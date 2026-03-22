package handlers

import (
	"bytes"
	"context"
	"encoding/json"
	"encoding/xml"
	"fmt"
	"io"
	"log"
	"net/http"
	"net/url"
	"strings"
	"time"

	"cloud.google.com/go/firestore"
	"github.com/gin-gonic/gin"
	"github.com/google/uuid"
	"google.golang.org/api/iterator"

	"module-a/models"
	"module-a/services"
)

// ============================================================================
// MESSAGING HANDLER — Buyer Messages / Helpdesk
//
// Full marketplace integration:
//   Amazon: SP-API Messaging API (messaging/v1)
//   eBay:   Trading API GetMemberMessages + AddMemberMessageRTQ
//   Temu:   No API — redirect link shown in UI
//
// Routes:
//   GET    /api/v1/messages                  List conversations
//   GET    /api/v1/messages/unread-count     Unread count (nav badge)
//   GET    /api/v1/messages/canned           List canned responses
//   POST   /api/v1/messages/canned           Create canned response
//   PUT    /api/v1/messages/canned/:id       Update canned response
//   DELETE /api/v1/messages/canned/:id       Delete canned response
//   POST   /api/v1/messages/sync             Pull new messages from all channels
//   POST   /api/v1/messages                  Create manual conversation
//   GET    /api/v1/messages/:id              Get conversation + messages
//   POST   /api/v1/messages/:id/reply        Send reply to buyer
//   POST   /api/v1/messages/:id/resolve      Mark conversation resolved
//   POST   /api/v1/messages/:id/read         Mark conversation read
// ============================================================================

type MessagingHandler struct {
	client             *firestore.Client
	marketplaceService *services.MarketplaceService
	notifier           *services.MessagingNotifier
	aiAgent            interface {
		ProcessConversationBackground(tenantID, convID string)
	}
}

func NewMessagingHandler(client *firestore.Client, marketplaceService *services.MarketplaceService) *MessagingHandler {
	return &MessagingHandler{
		client:             client,
		marketplaceService: marketplaceService,
		notifier:           services.NewMessagingNotifier(client),
	}
}

// SetAIAgent injects the AI agent after construction (avoids circular dependency).
func (h *MessagingHandler) SetAIAgent(agent interface {
	ProcessConversationBackground(tenantID, convID string)
}) {
	h.aiAgent = agent
}

// ─── Collection helpers ───────────────────────────────────────────────────────

func (h *MessagingHandler) convCol(tenantID string) *firestore.CollectionRef {
	return h.client.Collection(fmt.Sprintf("tenants/%s/conversations", tenantID))
}

func (h *MessagingHandler) convDoc(tenantID, convID string) *firestore.DocumentRef {
	return h.convCol(tenantID).Doc(convID)
}

func (h *MessagingHandler) msgCol(tenantID, convID string) *firestore.CollectionRef {
	return h.convDoc(tenantID, convID).Collection("messages")
}

func (h *MessagingHandler) cannedCol(tenantID string) *firestore.CollectionRef {
	return h.client.Collection(fmt.Sprintf("tenants/%s/canned_responses", tenantID))
}

// ============================================================================
// LIST CONVERSATIONS  GET /api/v1/messages
// ============================================================================

func (h *MessagingHandler) ListConversations(c *gin.Context) {
	tenantID := c.GetString("tenant_id")
	ctx := c.Request.Context()

	q := h.convCol(tenantID).Query
	if status := c.Query("status"); status != "" {
		q = q.Where("status", "==", status)
	}
	if channel := c.Query("channel"); channel != "" {
		q = q.Where("channel", "==", channel)
	}
	if c.Query("unread") == "true" {
		q = q.Where("unread", "==", true)
	}
	q = q.OrderBy("last_message_at", firestore.Desc).Limit(100)

	iter := q.Documents(ctx)
	var conversations []models.Conversation
	for {
		doc, err := iter.Next()
		if err == iterator.Done {
			break
		}
		if err != nil {
			c.JSON(http.StatusInternalServerError, gin.H{"error": err.Error()})
			return
		}
		var conv models.Conversation
		if err := doc.DataTo(&conv); err == nil {
			conversations = append(conversations, conv)
		}
	}
	if conversations == nil {
		conversations = []models.Conversation{}
	}
	unread := 0
	for _, conv := range conversations {
		if conv.Unread {
			unread++
		}
	}
	c.JSON(http.StatusOK, gin.H{"conversations": conversations, "total": len(conversations), "unread": unread})
}

// ============================================================================
// UNREAD COUNT  GET /api/v1/messages/unread-count
// ============================================================================

func (h *MessagingHandler) UnreadCount(c *gin.Context) {
	tenantID := c.GetString("tenant_id")
	ctx := c.Request.Context()

	iter := h.convCol(tenantID).Where("unread", "==", true).Documents(ctx)
	count := 0
	for {
		_, err := iter.Next()
		if err == iterator.Done {
			break
		}
		if err != nil {
			break
		}
		count++
	}
	c.JSON(http.StatusOK, gin.H{"unread": count})
}

// ============================================================================
// GET CONVERSATION  GET /api/v1/messages/:id
// ============================================================================

func (h *MessagingHandler) GetConversation(c *gin.Context) {
	tenantID := c.GetString("tenant_id")
	convID := c.Param("id")
	ctx := c.Request.Context()

	doc, err := h.convDoc(tenantID, convID).Get(ctx)
	if err != nil {
		c.JSON(http.StatusNotFound, gin.H{"error": "Conversation not found"})
		return
	}
	var conv models.Conversation
	if err := doc.DataTo(&conv); err != nil {
		c.JSON(http.StatusInternalServerError, gin.H{"error": err.Error()})
		return
	}

	msgIter := h.msgCol(tenantID, convID).OrderBy("sent_at", firestore.Asc).Documents(ctx)
	var messages []models.Message
	for {
		mdoc, err := msgIter.Next()
		if err == iterator.Done {
			break
		}
		if err != nil {
			break
		}
		var msg models.Message
		if err := mdoc.DataTo(&msg); err == nil {
			messages = append(messages, msg)
		}
	}
	if messages == nil {
		messages = []models.Message{}
	}

	if conv.Unread {
		go func() {
			h.convDoc(tenantID, convID).Update(ctx, []firestore.Update{
				{Path: "unread", Value: false},
				{Path: "updated_at", Value: time.Now()},
			})
		}()
	}

	c.JSON(http.StatusOK, gin.H{"conversation": conv, "messages": messages})
}

// ============================================================================
// REPLY  POST /api/v1/messages/:id/reply
// Sends via the correct marketplace API, then stores locally.
// ============================================================================

func (h *MessagingHandler) Reply(c *gin.Context) {
	tenantID := c.GetString("tenant_id")
	convID := c.Param("id")
	ctx := c.Request.Context()

	var req struct {
		Body string `json:"body" binding:"required"`
	}
	if err := c.ShouldBindJSON(&req); err != nil {
		c.JSON(http.StatusBadRequest, gin.H{"error": "body is required"})
		return
	}

	doc, err := h.convDoc(tenantID, convID).Get(ctx)
	if err != nil {
		c.JSON(http.StatusNotFound, gin.H{"error": "Conversation not found"})
		return
	}
	var conv models.Conversation
	if err := doc.DataTo(&conv); err != nil {
		c.JSON(http.StatusInternalServerError, gin.H{"error": err.Error()})
		return
	}

	if conv.Channel == "temu" {
		c.JSON(http.StatusUnprocessableEntity, gin.H{
			"error":    "Temu does not support external messaging. Please reply via Temu Seller Centre.",
			"temu_url": "https://seller.temu.com",
		})
		return
	}

	// Load credentials
	cred, err := h.marketplaceService.GetCredential(ctx, tenantID, conv.ChannelAccountID)
	if err != nil {
		c.JSON(http.StatusInternalServerError, gin.H{"error": fmt.Sprintf("Failed to load credentials: %v", err)})
		return
	}
	mergedCreds, err := h.marketplaceService.GetFullCredentials(ctx, cred)
	if err != nil {
		c.JSON(http.StatusInternalServerError, gin.H{"error": fmt.Sprintf("Failed to decrypt credentials: %v", err)})
		return
	}

	userID := c.GetString("user_id")
	if userID == "" {
		userID = "staff"
	}

	var sendErr error
	switch conv.Channel {
	case "amazon", "amazonnew":
		sendErr = h.sendAmazonMessage(ctx, mergedCreds, cred.MarketplaceID, conv.MarketplaceThreadID, req.Body)
	case "ebay":
		sendErr = h.sendEbayMessage(ctx, mergedCreds, conv.MarketplaceThreadID, conv.Customer.BuyerID, req.Body)
	}

	if sendErr != nil {
		log.Printf("[Messaging] Failed to send via %s: %v", conv.Channel, sendErr)
		c.JSON(http.StatusBadGateway, gin.H{"error": fmt.Sprintf("Marketplace send failed: %v", sendErr)})
		return
	}

	now := time.Now()
	msgID := uuid.New().String()
	preview := req.Body
	if len(preview) > 100 {
		preview = preview[:100] + "…"
	}
	msg := models.Message{
		MessageID:      msgID,
		ConversationID: convID,
		Direction:      models.MsgDirectionOutbound,
		Body:           req.Body,
		SentBy:         userID,
		SentAt:         now,
		ReadAt:         &now,
	}
	if _, err := h.msgCol(tenantID, convID).Doc(msgID).Set(ctx, msg); err != nil {
		c.JSON(http.StatusInternalServerError, gin.H{"error": "Message sent but failed to store locally"})
		return
	}

	h.convDoc(tenantID, convID).Update(ctx, []firestore.Update{
		{Path: "status", Value: models.ConvStatusPendingReply},
		{Path: "last_message_at", Value: now},
		{Path: "last_message_preview", Value: "You: " + preview},
		{Path: "unread", Value: false},
		{Path: "updated_at", Value: now},
	})

	c.JSON(http.StatusOK, gin.H{"ok": true, "message": msg})
}

// ============================================================================
// RESOLVE  POST /api/v1/messages/:id/resolve
// ============================================================================

func (h *MessagingHandler) Resolve(c *gin.Context) {
	tenantID := c.GetString("tenant_id")
	convID := c.Param("id")
	ctx := c.Request.Context()

	now := time.Now()
	if _, err := h.convDoc(tenantID, convID).Update(ctx, []firestore.Update{
		{Path: "status", Value: models.ConvStatusResolved},
		{Path: "unread", Value: false},
		{Path: "updated_at", Value: now},
	}); err != nil {
		c.JSON(http.StatusInternalServerError, gin.H{"error": err.Error()})
		return
	}
	c.JSON(http.StatusOK, gin.H{"ok": true, "status": models.ConvStatusResolved})
}

// ============================================================================
// MARK READ  POST /api/v1/messages/:id/read
// ============================================================================

func (h *MessagingHandler) MarkRead(c *gin.Context) {
	tenantID := c.GetString("tenant_id")
	convID := c.Param("id")
	ctx := c.Request.Context()
	h.convDoc(tenantID, convID).Update(ctx, []firestore.Update{
		{Path: "unread", Value: false},
		{Path: "updated_at", Value: time.Now()},
	})
	c.JSON(http.StatusOK, gin.H{"ok": true})
}

// ============================================================================
// SYNC  POST /api/v1/messages/sync
// Pulls new messages from all connected Amazon and eBay accounts.
// ============================================================================

func (h *MessagingHandler) Sync(c *gin.Context) {
	tenantID := c.GetString("tenant_id")
	ctx := c.Request.Context()

	credentials, err := h.marketplaceService.ListCredentials(ctx, tenantID)
	if err != nil {
		c.JSON(http.StatusInternalServerError, gin.H{"error": err.Error()})
		return
	}

	totalSynced := 0
	var syncErrors []string

	for _, cred := range credentials {
		if !cred.Active {
			continue
		}
		mergedCreds, err := h.marketplaceService.GetFullCredentials(ctx, &cred)
		if err != nil {
			syncErrors = append(syncErrors, fmt.Sprintf("%s (%s): credential error: %v", cred.Channel, cred.AccountName, err))
			continue
		}

		switch cred.Channel {
		case "amazon", "amazonnew":
			synced, err := h.syncAmazonMessages(ctx, tenantID, cred.CredentialID, cred.MarketplaceID, mergedCreds)
			if err != nil {
				syncErrors = append(syncErrors, fmt.Sprintf("%s (%s): %v", cred.Channel, cred.AccountName, err))
			} else {
				totalSynced += synced
			}
		case "ebay":
			synced, err := h.syncEbayMessages(ctx, tenantID, cred.CredentialID, mergedCreds)
			if err != nil {
				syncErrors = append(syncErrors, fmt.Sprintf("ebay (%s): %v", cred.AccountName, err))
			} else {
				totalSynced += synced
			}
		}
	}

	resp := gin.H{"ok": true, "synced": totalSynced}
	if len(syncErrors) > 0 {
		resp["errors"] = syncErrors
	}
	c.JSON(http.StatusOK, resp)
}

// ============================================================================
// CREATE CONVERSATION (manual)  POST /api/v1/messages
// ============================================================================

func (h *MessagingHandler) CreateConversation(c *gin.Context) {
	tenantID := c.GetString("tenant_id")
	ctx := c.Request.Context()

	var req struct {
		Channel     string                      `json:"channel"`
		OrderID     string                      `json:"order_id"`
		OrderNumber string                      `json:"order_number"`
		Customer    models.ConversationCustomer `json:"customer"`
		Subject     string                      `json:"subject"`
		Body        string                      `json:"body"`
	}
	if err := c.ShouldBindJSON(&req); err != nil {
		c.JSON(http.StatusBadRequest, gin.H{"error": err.Error()})
		return
	}

	now := time.Now()
	convID := uuid.New().String()
	preview := req.Body
	if len(preview) > 100 {
		preview = preview[:100] + "…"
	}
	conv := models.Conversation{
		ConversationID:     convID,
		TenantID:           tenantID,
		Channel:            req.Channel,
		OrderID:            req.OrderID,
		OrderNumber:        req.OrderNumber,
		Customer:           req.Customer,
		Subject:            req.Subject,
		Status:             models.ConvStatusOpen,
		LastMessageAt:      now,
		LastMessagePreview: preview,
		Unread:             false,
		MessageCount:       1,
		CreatedAt:          now,
		UpdatedAt:          now,
	}
	if _, err := h.convDoc(tenantID, convID).Set(ctx, conv); err != nil {
		c.JSON(http.StatusInternalServerError, gin.H{"error": err.Error()})
		return
	}
	if req.Body != "" {
		userID := c.GetString("user_id")
		if userID == "" {
			userID = "staff"
		}
		msgID := uuid.New().String()
		msg := models.Message{
			MessageID:      msgID,
			ConversationID: convID,
			Direction:      models.MsgDirectionOutbound,
			Body:           req.Body,
			SentBy:         userID,
			SentAt:         now,
			ReadAt:         &now,
		}
		h.msgCol(tenantID, convID).Doc(msgID).Set(ctx, msg)
	}
	c.JSON(http.StatusCreated, gin.H{"conversation": conv})
}

// ============================================================================
// CANNED RESPONSES
// ============================================================================

func (h *MessagingHandler) ListCannedResponses(c *gin.Context) {
	tenantID := c.GetString("tenant_id")
	ctx := c.Request.Context()
	iter := h.cannedCol(tenantID).OrderBy("title", firestore.Asc).Documents(ctx)
	var responses []models.CannedResponse
	for {
		doc, err := iter.Next()
		if err == iterator.Done {
			break
		}
		if err != nil {
			break
		}
		var cr models.CannedResponse
		if err := doc.DataTo(&cr); err == nil {
			responses = append(responses, cr)
		}
	}
	if responses == nil {
		responses = []models.CannedResponse{}
	}
	c.JSON(http.StatusOK, gin.H{"canned_responses": responses})
}

func (h *MessagingHandler) CreateCannedResponse(c *gin.Context) {
	tenantID := c.GetString("tenant_id")
	ctx := c.Request.Context()
	var req models.CannedResponse
	if err := c.ShouldBindJSON(&req); err != nil {
		c.JSON(http.StatusBadRequest, gin.H{"error": err.Error()})
		return
	}
	if req.ID == "" {
		req.ID = uuid.New().String()
	}
	req.TenantID = tenantID
	if _, err := h.cannedCol(tenantID).Doc(req.ID).Set(ctx, req); err != nil {
		c.JSON(http.StatusInternalServerError, gin.H{"error": err.Error()})
		return
	}
	c.JSON(http.StatusCreated, gin.H{"canned_response": req})
}

func (h *MessagingHandler) UpdateCannedResponse(c *gin.Context) {
	tenantID := c.GetString("tenant_id")
	id := c.Param("id")
	ctx := c.Request.Context()
	var req models.CannedResponse
	if err := c.ShouldBindJSON(&req); err != nil {
		c.JSON(http.StatusBadRequest, gin.H{"error": err.Error()})
		return
	}
	req.ID = id
	req.TenantID = tenantID
	if _, err := h.cannedCol(tenantID).Doc(id).Set(ctx, req); err != nil {
		c.JSON(http.StatusInternalServerError, gin.H{"error": err.Error()})
		return
	}
	c.JSON(http.StatusOK, gin.H{"canned_response": req})
}

func (h *MessagingHandler) DeleteCannedResponse(c *gin.Context) {
	tenantID := c.GetString("tenant_id")
	id := c.Param("id")
	ctx := c.Request.Context()
	if _, err := h.cannedCol(tenantID).Doc(id).Delete(ctx); err != nil {
		c.JSON(http.StatusInternalServerError, gin.H{"error": err.Error()})
		return
	}
	c.JSON(http.StatusOK, gin.H{"ok": true})
}


// ============================================================================
// SYNC ALL TENANTS (scheduled background sync)
// Called by the in-process scheduler every 30 minutes.
// ============================================================================

func (h *MessagingHandler) SyncAllTenants(ctx context.Context) {
	iter := h.client.Collection("tenants").Documents(ctx)
	for {
		doc, err := iter.Next()
		if err != nil {
			break
		}
		tenantID := doc.Ref.ID
		go func(tid string) {
			tctx := context.Background()
			credentials, err := h.marketplaceService.ListCredentials(tctx, tid)
			if err != nil {
				log.Printf("[Messaging] SyncAllTenants: credentials error for %s: %v", tid, err)
				return
			}
			for _, cred := range credentials {
				if !cred.Active {
					continue
				}
				if cred.Channel != "amazon" && cred.Channel != "amazonnew" && cred.Channel != "ebay" {
					continue
				}
				mergedCreds, err := h.marketplaceService.GetFullCredentials(tctx, &cred)
				if err != nil {
					log.Printf("[Messaging] SyncAllTenants: credential error %s/%s: %v", tid, cred.AccountName, err)
					continue
				}
				var synced int
				switch cred.Channel {
				case "amazon", "amazonnew":
					synced, err = h.syncAmazonMessages(tctx, tid, cred.CredentialID, cred.MarketplaceID, mergedCreds)
				case "ebay":
					synced, err = h.syncEbayMessages(tctx, tid, cred.CredentialID, mergedCreds)
				}
				if err != nil {
					log.Printf("[Messaging] SyncAllTenants: sync error %s/%s: %v", tid, cred.AccountName, err)
				} else if synced > 0 {
					log.Printf("[Messaging] SyncAllTenants: %d new messages for %s/%s", synced, tid, cred.AccountName)
				}
			}
		}(tenantID)
	}
	iter.Stop()
}


// ============================================================================
// ASSIGN CONVERSATION  POST /api/v1/messages/:id/assign
// Assigns a conversation to a team member and sends a notification.
// ============================================================================

func (h *MessagingHandler) Assign(c *gin.Context) {
	tenantID := c.GetString("tenant_id")
	convID := c.Param("id")
	ctx := c.Request.Context()

	var req struct {
		MembershipID string `json:"membership_id"` // empty = unassign
	}
	if err := c.ShouldBindJSON(&req); err != nil {
		c.JSON(http.StatusBadRequest, gin.H{"error": err.Error()})
		return
	}

	// Load conversation
	doc, err := h.convDoc(tenantID, convID).Get(ctx)
	if err != nil {
		c.JSON(http.StatusNotFound, gin.H{"error": "Conversation not found"})
		return
	}
	var conv models.Conversation
	if err := doc.DataTo(&conv); err != nil {
		c.JSON(http.StatusInternalServerError, gin.H{"error": err.Error()})
		return
	}

	now := time.Now()
	assignerID := c.GetString("user_id")
	assignerName := c.GetString("display_name")
	if assignerName == "" {
		assignerName = "A team member"
	}

	// Unassign
	if req.MembershipID == "" {
		h.convDoc(tenantID, convID).Update(ctx, []firestore.Update{
			{Path: "assigned_to", Value: firestore.Delete},
			{Path: "updated_at", Value: now},
		})
		c.JSON(http.StatusOK, gin.H{"ok": true, "assigned_to": nil})
		return
	}

	// Look up member
	member, err := h.notifier.GetMember(ctx, tenantID, req.MembershipID)
	if err != nil {
		c.JSON(http.StatusNotFound, gin.H{"error": "Team member not found"})
		return
	}

	assignedTo := models.AssignedTo{
		MembershipID: member.MembershipID,
		DisplayName:  member.DisplayName,
		Email:        member.Email,
		AssignedAt:   now,
		AssignedBy:   assignerID,
	}

	h.convDoc(tenantID, convID).Update(ctx, []firestore.Update{
		{Path: "assigned_to", Value: assignedTo},
		{Path: "updated_at", Value: now},
	})

	// Send notification asynchronously
	conv.AssignedTo = &assignedTo
	go h.notifier.NotifyAssignment(context.Background(), member, &conv, assignerName)

	c.JSON(http.StatusOK, gin.H{"ok": true, "assigned_to": assignedTo})
}

// ============================================================================
// LIST ASSIGNABLE MEMBERS  GET /api/v1/messages/members
// Returns team members the current tenant can assign conversations to.
// ============================================================================

func (h *MessagingHandler) ListMembers(c *gin.Context) {
	tenantID := c.GetString("tenant_id")
	ctx := c.Request.Context()

	members, err := h.notifier.GetAssignableMembers(ctx, tenantID)
	if err != nil {
		c.JSON(http.StatusInternalServerError, gin.H{"error": err.Error()})
		return
	}
	if members == nil {
		members = []services.AssignableMember{}
	}
	c.JSON(http.StatusOK, gin.H{"members": members})
}


// ============================================================================
// DELETE DRAFT  DELETE /api/v1/messages/:id/drafts/:draft_id
// Discards an AI-generated draft message.
// ============================================================================

func (h *MessagingHandler) DeleteDraft(c *gin.Context) {
	tenantID := c.GetString("tenant_id")
	convID := c.Param("id")
	draftID := c.Param("draft_id")
	ctx := c.Request.Context()

	if _, err := h.msgCol(tenantID, convID).Doc(draftID).Delete(ctx); err != nil {
		c.JSON(http.StatusInternalServerError, gin.H{"error": err.Error()})
		return
	}

	// Check if any other drafts remain; if not, clear has_ai_draft flag
	iter := h.msgCol(tenantID, convID).Where("direction", "==", "draft").Limit(1).Documents(ctx)
	defer iter.Stop()
	_, err := iter.Next()
	if err != nil {
		// No more drafts
		h.convDoc(tenantID, convID).Update(ctx, []firestore.Update{
			{Path: "has_ai_draft", Value: false},
			{Path: "updated_at", Value: time.Now()},
		})
	}

	c.JSON(http.StatusOK, gin.H{"ok": true})
}

// ============================================================================
// UPDATE NOTIFICATION PREFS  PUT /api/v1/messages/notif-prefs
// Saves a team member's notification preferences for message assignments.
// ============================================================================

func (h *MessagingHandler) UpdateNotifPrefs(c *gin.Context) {
	tenantID := c.GetString("tenant_id")
	userID := c.GetString("user_id")
	ctx := c.Request.Context()

	var prefs models.MessagingNotificationPrefs
	if err := c.ShouldBindJSON(&prefs); err != nil {
		c.JSON(http.StatusBadRequest, gin.H{"error": err.Error()})
		return
	}

	// Find the membership doc for this user+tenant
	iter := h.client.Collection("user_memberships").
		Where("tenant_id", "==", tenantID).
		Where("user_id", "==", userID).
		Limit(1).Documents(ctx)
	defer iter.Stop()

	snap, err := iter.Next()
	if err != nil {
		c.JSON(http.StatusNotFound, gin.H{"error": "Membership not found"})
		return
	}

	snap.Ref.Update(ctx, []firestore.Update{
		{Path: "messaging_notif_prefs", Value: prefs},
		{Path: "updated_at", Value: time.Now()},
	})

	c.JSON(http.StatusOK, gin.H{"ok": true, "prefs": prefs})
}

// ============================================================================
// AMAZON SP-API MESSAGING
// ============================================================================

// amazonMessagingEndpoint returns the SP-API endpoint for a region
func (h *MessagingHandler) amazonMessagingEndpoint(region string) string {
	endpoints := map[string]string{
		"EU": "https://sellingpartnerapi-eu.amazon.com",
		"NA": "https://sellingpartnerapi-na.amazon.com",
		"FE": "https://sellingpartnerapi-fe.amazon.com",
		"UK": "https://sellingpartnerapi-eu.amazon.com",
		"US": "https://sellingpartnerapi-na.amazon.com",
	}
	if ep, ok := endpoints[region]; ok {
		return ep
	}
	return "https://sellingpartnerapi-eu.amazon.com"
}

// amazonLWAToken gets a fresh LWA access token
func (h *MessagingHandler) amazonLWAToken(creds map[string]string) (string, error) {
	data := url.Values{}
	data.Set("grant_type", "refresh_token")
	data.Set("refresh_token", creds["refresh_token"])
	data.Set("client_id", creds["lwa_client_id"])
	data.Set("client_secret", creds["lwa_client_secret"])

	resp, err := http.PostForm("https://api.amazon.com/auth/o2/token", data)
	if err != nil {
		return "", err
	}
	defer resp.Body.Close()
	body, _ := io.ReadAll(resp.Body)
	if resp.StatusCode != 200 {
		return "", fmt.Errorf("LWA token error %d: %s", resp.StatusCode, string(body))
	}
	var tok struct {
		AccessToken string `json:"access_token"`
	}
	json.Unmarshal(body, &tok)
	return tok.AccessToken, nil
}

// syncAmazonMessages pulls buyer messages for recent orders via SP-API messaging API.
// Amazon's approach: for each order that has buyer-initiated messages, retrieve the thread.
func (h *MessagingHandler) syncAmazonMessages(ctx context.Context, tenantID, credentialID, marketplaceID string, creds map[string]string) (int, error) {
	accessToken, err := h.amazonLWAToken(creds)
	if err != nil {
		return 0, fmt.Errorf("LWA auth: %w", err)
	}

	endpoint := h.amazonMessagingEndpoint(creds["region"])
	httpClient := &http.Client{Timeout: 30 * time.Second}

	// Fetch orders from last 30 days to check for messages
	createdAfter := time.Now().AddDate(0, 0, -30).Format(time.RFC3339)
	ordersURL := fmt.Sprintf("%s/orders/v0/orders?MarketplaceIds=%s&CreatedAfter=%s&OrderStatuses=Unshipped,PartiallyShipped,Shipped,InvoiceUnconfirmed,Canceled,Unfulfillable",
		endpoint, url.QueryEscape(marketplaceID), url.QueryEscape(createdAfter))

	ordersReq, _ := http.NewRequestWithContext(ctx, "GET", ordersURL, nil)
	ordersReq.Header.Set("x-amz-access-token", accessToken)
	ordersResp, err := httpClient.Do(ordersReq)
	if err != nil {
		return 0, fmt.Errorf("orders fetch: %w", err)
	}
	defer ordersResp.Body.Close()
	ordersBody, _ := io.ReadAll(ordersResp.Body)
	if ordersResp.StatusCode != 200 {
		return 0, fmt.Errorf("orders API %d: %s", ordersResp.StatusCode, string(ordersBody))
	}

	var ordersResult struct {
		Payload struct {
			Orders []struct {
				AmazonOrderID string `json:"AmazonOrderId"`
				BuyerInfo     *struct {
					BuyerName  string `json:"BuyerName"`
					BuyerEmail string `json:"BuyerEmail"`
				} `json:"BuyerInfo"`
			} `json:"Orders"`
		} `json:"payload"`
	}
	json.Unmarshal(ordersBody, &ordersResult)

	synced := 0
	for _, order := range ordersResult.Payload.Orders {
		if order.AmazonOrderID == "" {
			continue
		}

		// Check if messaging actions are available for this order
		actionsURL := fmt.Sprintf("%s/messaging/v1/orders/%s", endpoint, order.AmazonOrderID)
		actionsReq, _ := http.NewRequestWithContext(ctx, "GET", actionsURL, nil)
		actionsReq.Header.Set("x-amz-access-token", accessToken)
		// MarketplaceIds is required
		q := actionsReq.URL.Query()
		q.Set("marketplaceIds", marketplaceID)
		actionsReq.URL.RawQuery = q.Encode()

		actionsResp, err := httpClient.Do(actionsReq)
		if err != nil {
			continue
		}
		actionsBody, _ := io.ReadAll(actionsResp.Body)
		actionsResp.Body.Close()

		// 200 means actions are available (messages exist or can be sent)
		if actionsResp.StatusCode != 200 {
			continue
		}

		var actionsResult struct {
			Links struct {
				GetMessaging struct {
					Href string `json:"href"`
				} `json:"GetMessaging"`
			} `json:"_links"`
		}
		json.Unmarshal(actionsBody, &actionsResult)

		// Fetch the actual buyer-seller messages
		msgsURL := fmt.Sprintf("%s/messaging/v1/orders/%s/messages", endpoint, order.AmazonOrderID)
		msgsReq, _ := http.NewRequestWithContext(ctx, "GET", msgsURL, nil)
		msgsReq.Header.Set("x-amz-access-token", accessToken)
		msgsQ := msgsReq.URL.Query()
		msgsQ.Set("marketplaceIds", marketplaceID)
		msgsReq.URL.RawQuery = msgsQ.Encode()

		msgsResp, err := httpClient.Do(msgsReq)
		if err != nil {
			continue
		}
		msgsBody, _ := io.ReadAll(msgsResp.Body)
		msgsResp.Body.Close()

		if msgsResp.StatusCode != 200 {
			continue
		}

		var msgsResult struct {
			Embedded struct {
				Messages []struct {
					Attributes struct {
						Text      string `json:"text"`
						SentDate  string `json:"sentDate"`
						Status    string `json:"status"`
						FromRole  string `json:"fromRole"`
						MessageID string `json:"messageId"`
					} `json:"attributes"`
				} `json:"messages"`
			} `json:"_embedded"`
		}
		json.Unmarshal(msgsBody, &msgsResult)

		if len(msgsResult.Embedded.Messages) == 0 {
			continue
		}

		// Upsert conversation
		convID := fmt.Sprintf("amz_%s_%s", credentialID, order.AmazonOrderID)
		customerName := ""
		if order.BuyerInfo != nil {
			customerName = order.BuyerInfo.BuyerName
		}

		now := time.Now()
		conv := models.Conversation{
			ConversationID:      convID,
			TenantID:            tenantID,
			Channel:             "amazon",
			ChannelAccountID:    credentialID,
			MarketplaceThreadID: order.AmazonOrderID,
			OrderNumber:         order.AmazonOrderID,
			Customer:            models.ConversationCustomer{Name: customerName},
			Subject:             fmt.Sprintf("Order %s", order.AmazonOrderID),
			Status:              models.ConvStatusOpen,
			LastMessageAt:       now,
			CreatedAt:           now,
			UpdatedAt:           now,
		}

		// Check if conversation already exists
		existingDoc, _ := h.convDoc(tenantID, convID).Get(ctx)
		if existingDoc.Exists() {
			existingDoc.DataTo(&conv)
		}

		newMessages := 0
		for _, m := range msgsResult.Embedded.Messages {
			msgID := fmt.Sprintf("amz_%s", m.Attributes.MessageID)
			// Check if already stored
			msgDocRef := h.msgCol(tenantID, convID).Doc(msgID)
			existingMsg, _ := msgDocRef.Get(ctx)
			if existingMsg.Exists() {
				continue
			}

			direction := models.MsgDirectionOutbound
			if m.Attributes.FromRole == "BUYER" {
				direction = models.MsgDirectionInbound
			}

			sentAt, _ := time.Parse(time.RFC3339, m.Attributes.SentDate)
			if sentAt.IsZero() {
				sentAt = time.Now()
			}

			msg := models.Message{
				MessageID:        msgID,
				ConversationID:   convID,
				Direction:        direction,
				Body:             m.Attributes.Text,
				ChannelMessageID: m.Attributes.MessageID,
				SentAt:           sentAt,
			}
			msgDocRef.Set(ctx, msg)

			conv.LastMessageAt = sentAt
			conv.LastMessagePreview = m.Attributes.Text
			if len(conv.LastMessagePreview) > 100 {
				conv.LastMessagePreview = conv.LastMessagePreview[:100] + "…"
			}
			newMessages++
			synced++
		}

		if newMessages > 0 {
			conv.Unread = true
			conv.MessageCount = conv.MessageCount + newMessages
		}
		conv.UpdatedAt = now
		h.convDoc(tenantID, convID).Set(ctx, conv)
		// Trigger AI agent background processing for new inbound messages
		if newMessages > 0 && h.aiAgent != nil {
			cid := convID
			go h.aiAgent.ProcessConversationBackground(tenantID, cid)
		}
	}

	log.Printf("[Messaging] Amazon sync complete for %s: %d new messages", tenantID, synced)
	return synced, nil
}

// sendAmazonMessage sends a buyer-seller message via SP-API Messaging API
func (h *MessagingHandler) sendAmazonMessage(ctx context.Context, creds map[string]string, marketplaceID, amazonOrderID, body string) error {
	accessToken, err := h.amazonLWAToken(creds)
	if err != nil {
		return fmt.Errorf("LWA auth: %w", err)
	}

	endpoint := h.amazonMessagingEndpoint(creds["region"])
	apiURL := fmt.Sprintf("%s/messaging/v1/orders/%s/messages/buyerSellerMessages?marketplaceIds=%s",
		endpoint, amazonOrderID, url.QueryEscape(marketplaceID))

	payload := map[string]interface{}{
		"text": body,
	}
	payloadBytes, _ := json.Marshal(payload)

	req, err := http.NewRequestWithContext(ctx, "POST", apiURL, bytes.NewReader(payloadBytes))
	if err != nil {
		return err
	}
	req.Header.Set("x-amz-access-token", accessToken)
	req.Header.Set("Content-Type", "application/json")

	client := &http.Client{Timeout: 20 * time.Second}
	resp, err := client.Do(req)
	if err != nil {
		return err
	}
	defer resp.Body.Close()
	respBody, _ := io.ReadAll(resp.Body)

	// 201 Created = success for Amazon messaging
	if resp.StatusCode != 201 && resp.StatusCode != 200 {
		return fmt.Errorf("Amazon messaging API %d: %s", resp.StatusCode, string(respBody))
	}
	return nil
}

// ============================================================================
// EBAY TRADING API MESSAGING
// Uses the existing eBay client pattern with X-EBAY-API-IAF-TOKEN
// ============================================================================

// ebayRefreshToken refreshes the eBay OAuth access token
func (h *MessagingHandler) ebayRefreshToken(creds map[string]string) (string, error) {
	tokenURL := "https://api.ebay.com/identity/v1/oauth2/token"
	if creds["environment"] == "sandbox" {
		tokenURL = "https://api.sandbox.ebay.com/identity/v1/oauth2/token"
	}

	data := url.Values{}
	data.Set("grant_type", "refresh_token")
	data.Set("refresh_token", creds["refresh_token"])
	data.Set("scope", "https://api.ebay.com/oauth/api_scope/sell.fulfillment https://api.ebay.com/oauth/api_scope")

	req, err := http.NewRequest("POST", tokenURL, strings.NewReader(data.Encode()))
	if err != nil {
		return "", err
	}
	clientID := creds["client_id"]
	clientSecret := creds["client_secret"]
	req.SetBasicAuth(clientID, clientSecret)
	req.Header.Set("Content-Type", "application/x-www-form-urlencoded")

	resp, err := (&http.Client{Timeout: 15 * time.Second}).Do(req)
	if err != nil {
		return "", err
	}
	defer resp.Body.Close()
	body, _ := io.ReadAll(resp.Body)
	if resp.StatusCode != 200 {
		return "", fmt.Errorf("eBay token refresh %d: %s", resp.StatusCode, string(body))
	}
	var tok struct {
		AccessToken string `json:"access_token"`
	}
	json.Unmarshal(body, &tok)
	return tok.AccessToken, nil
}

// ebayTradingCall makes an authenticated eBay Trading API XML call
func (h *MessagingHandler) ebayTradingCall(accessToken, callName, xmlBody string, sandbox bool) ([]byte, error) {
	apiURL := "https://api.ebay.com/ws/api.dll"
	if sandbox {
		apiURL = "https://api.sandbox.ebay.com/ws/api.dll"
	}

	req, err := http.NewRequest("POST", apiURL, strings.NewReader(xmlBody))
	if err != nil {
		return nil, err
	}
	req.Header.Set("Content-Type", "text/xml")
	req.Header.Set("X-EBAY-API-IAF-TOKEN", accessToken)
	req.Header.Set("X-EBAY-API-CALL-NAME", callName)
	req.Header.Set("X-EBAY-API-SITEID", "3") // UK
	req.Header.Set("X-EBAY-API-COMPATIBILITY-LEVEL", "1225")

	resp, err := (&http.Client{Timeout: 30 * time.Second}).Do(req)
	if err != nil {
		return nil, err
	}
	defer resp.Body.Close()
	return io.ReadAll(resp.Body)
}

// syncEbayMessages pulls all member messages via Trading API GetMemberMessages
func (h *MessagingHandler) syncEbayMessages(ctx context.Context, tenantID, credentialID string, creds map[string]string) (int, error) {
	accessToken, err := h.ebayRefreshToken(creds)
	if err != nil {
		return 0, fmt.Errorf("eBay token: %w", err)
	}

	sandbox := creds["environment"] == "sandbox"
	startTime := time.Now().AddDate(0, 0, -30).Format("2006-01-02T15:04:05.000Z")

	xmlBody := fmt.Sprintf(`<?xml version="1.0" encoding="utf-8"?>
<GetMemberMessagesRequest xmlns="urn:ebay:apis:eBLBaseComponents">
  <ErrorLanguage>en_US</ErrorLanguage>
  <WarningLevel>Low</WarningLevel>
  <MailMessageType>All</MailMessageType>
  <MessageStatus>Unanswered</MessageStatus>
  <StartCreationTime>%s</StartCreationTime>
  <Pagination>
    <EntriesPerPage>50</EntriesPerPage>
    <PageNumber>1</PageNumber>
  </Pagination>
</GetMemberMessagesRequest>`, startTime)

	body, err := h.ebayTradingCall(accessToken, "GetMemberMessages", xmlBody, sandbox)
	if err != nil {
		return 0, fmt.Errorf("GetMemberMessages: %w", err)
	}

	// Parse XML response
	var resp struct {
		XMLName         xml.Name `xml:"GetMemberMessagesResponse"`
		Ack             string   `xml:"Ack"`
		MemberMessage   struct {
			MemberMessageExchange []struct {
				Item struct {
					ItemID string `xml:"ItemID"`
					Title  string `xml:"Title"`
				} `xml:"Item"`
				Question struct {
					MsgID        string `xml:"MessageID"`
					SenderID     string `xml:"SenderID"`
					Body         string `xml:"Body"`
					ItemArID     string `xml:"ItemArID"`
					CreationDate string `xml:"CreationDate"`
					Subject      string `xml:"Subject"`
				} `xml:"Question"`
			} `xml:"MemberMessageExchange"`
		} `xml:"MemberMessage"`
		Errors struct {
			ShortMessage string `xml:"ShortMessage"`
			LongMessage  string `xml:"LongMessage"`
		} `xml:"Errors"`
	}

	if err := xml.Unmarshal(body, &resp); err != nil {
		return 0, fmt.Errorf("parse GetMemberMessages: %w", err)
	}

	if resp.Ack != "Success" && resp.Ack != "Warning" {
		return 0, fmt.Errorf("eBay GetMemberMessages: %s — %s", resp.Errors.ShortMessage, resp.Errors.LongMessage)
	}

	synced := 0
	now := time.Now()

	for _, exchange := range resp.MemberMessage.MemberMessageExchange {
		q := exchange.Question
		if q.MsgID == "" {
			continue
		}

		convID := fmt.Sprintf("ebay_%s_%s", credentialID, q.MsgID)

		// Check if already stored
		existingConv, _ := h.convDoc(tenantID, convID).Get(ctx)
		if existingConv.Exists() {
			continue
		}

		sentAt, _ := time.Parse(time.RFC3339, q.CreationDate)
		if sentAt.IsZero() {
			sentAt = now
		}

		subject := q.Subject
		if subject == "" {
			subject = fmt.Sprintf("Message about item %s", exchange.Item.ItemID)
		}

		conv := models.Conversation{
			ConversationID:      convID,
			TenantID:            tenantID,
			Channel:             "ebay",
			ChannelAccountID:    credentialID,
			MarketplaceThreadID: q.MsgID,
			Customer: models.ConversationCustomer{
				Name:    q.SenderID,
				BuyerID: q.SenderID,
			},
			Subject:            subject,
			Status:             models.ConvStatusOpen,
			LastMessageAt:      sentAt,
			LastMessagePreview: q.Body,
			Unread:             true,
			MessageCount:       1,
			CreatedAt:          now,
			UpdatedAt:          now,
		}
		if len(conv.LastMessagePreview) > 100 {
			conv.LastMessagePreview = conv.LastMessagePreview[:100] + "…"
		}

		h.convDoc(tenantID, convID).Set(ctx, conv)

		msgID := fmt.Sprintf("ebay_%s", q.MsgID)
		msg := models.Message{
			MessageID:        msgID,
			ConversationID:   convID,
			Direction:        models.MsgDirectionInbound,
			Body:             q.Body,
			ChannelMessageID: q.MsgID,
			SentBy:           q.SenderID,
			SentAt:           sentAt,
		}
		h.msgCol(tenantID, convID).Doc(msgID).Set(ctx, msg)
		synced++
	}

	log.Printf("[Messaging] eBay sync complete for %s: %d new conversations", tenantID, synced)
	return synced, nil
}

// sendEbayMessage sends a reply to a buyer via Trading API AddMemberMessageRTQ
func (h *MessagingHandler) sendEbayMessage(ctx context.Context, creds map[string]string, parentMessageID, buyerID, body string) error {
	accessToken, err := h.ebayRefreshToken(creds)
	if err != nil {
		return fmt.Errorf("eBay token: %w", err)
	}

	sandbox := creds["environment"] == "sandbox"

	xmlBody := fmt.Sprintf(`<?xml version="1.0" encoding="utf-8"?>
<AddMemberMessageRTQRequest xmlns="urn:ebay:apis:eBLBaseComponents">
  <ErrorLanguage>en_US</ErrorLanguage>
  <WarningLevel>Low</WarningLevel>
  <MemberMessage>
    <Body>%s</Body>
    <RecipientID>%s</RecipientID>
    <ParentMessageID>%s</ParentMessageID>
  </MemberMessage>
</AddMemberMessageRTQRequest>`,
		xmlEscape(body), xmlEscape(buyerID), xmlEscape(parentMessageID))

	respBody, err := h.ebayTradingCall(accessToken, "AddMemberMessageRTQ", xmlBody, sandbox)
	if err != nil {
		return err
	}

	var resp struct {
		XMLName xml.Name `xml:"AddMemberMessageRTQResponse"`
		Ack     string   `xml:"Ack"`
		Errors  struct {
			ShortMessage string `xml:"ShortMessage"`
			LongMessage  string `xml:"LongMessage"`
		} `xml:"Errors"`
	}
	xml.Unmarshal(respBody, &resp)

	if resp.Ack != "Success" && resp.Ack != "Warning" {
		return fmt.Errorf("AddMemberMessageRTQ failed: %s — %s", resp.Errors.ShortMessage, resp.Errors.LongMessage)
	}
	return nil
}

// xmlEscape escapes special characters for XML
func xmlEscape(s string) string {
	s = strings.ReplaceAll(s, "&", "&amp;")
	s = strings.ReplaceAll(s, "<", "&lt;")
	s = strings.ReplaceAll(s, ">", "&gt;")
	s = strings.ReplaceAll(s, "\"", "&quot;")
	s = strings.ReplaceAll(s, "'", "&apos;")
	return s
}
