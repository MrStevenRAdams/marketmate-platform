package handlers

// ============================================================================
// AMAZON SP-API NOTIFICATIONS HANDLER
// ============================================================================
// Implements the APPLICATION destination type for Amazon SP-API Notifications.
//
// Flow:
//   1. On credential connect: call RegisterAmazonMessagingWebhook to:
//      a. Create an APPLICATION destination (POST /notifications/v1/destinations)
//      b. Subscribe to MESSAGING_NEW_MESSAGE_NOTIFICATION
//         (POST /notifications/v1/subscriptions/{notificationType})
//      c. Store destination_id + subscription_id in credential_data
//
//   2. Amazon creates a managed SNS topic and sends an HTTP confirmation
//      request to our endpoint. We handle it here:
//      GET /webhooks/messages/amazon?Type=SubscriptionConfirmation&...
//
//   3. On message: Amazon SNS POSTs JSON to:
//      POST /webhooks/messages/amazon
//      We verify the SNS message signature, extract the notification,
//      find the matching credential, and upsert the conversation + message
//      in Firestore.
//
//   4. Deregister: call UnregisterAmazonMessagingWebhook on credential removal.
//
// This approach requires NO AWS account — Amazon manages the SNS topic.
// The endpoint must be publicly reachable (it is — Cloud Run public URL).
//
// Routes (registered in main.go — no auth middleware, SNS-verified):
//   GET  /webhooks/messages/amazon   — SNS subscription confirmation
//   POST /webhooks/messages/amazon   — SNS message delivery
// ============================================================================

import (
	"bytes"
	"context"
	"crypto/x509"
	"encoding/base64"
	"encoding/json"
	"encoding/pem"
	"fmt"
	"io"
	"log"
	"net/http"
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
// HANDLER STRUCT
// ============================================================================

type AmazonMessagingWebhookHandler struct {
	client             *firestore.Client
	marketplaceService *services.MarketplaceService
	messagingHandler   *MessagingHandler
	notifier           *services.MessagingNotifier
}

func NewAmazonMessagingWebhookHandler(
	client *firestore.Client,
	marketplaceService *services.MarketplaceService,
	messagingHandler *MessagingHandler,
) *AmazonMessagingWebhookHandler {
	return &AmazonMessagingWebhookHandler{
		client:             client,
		marketplaceService: marketplaceService,
		messagingHandler:   messagingHandler,
		notifier:           services.NewMessagingNotifier(client),
	}
}

// ============================================================================
// REGISTRATION — called when an amazon/amazonnew credential is saved
// ============================================================================

// RegisterAmazonMessagingWebhook creates an APPLICATION destination in SP-API
// Notifications and subscribes to MESSAGING_NEW_MESSAGE_NOTIFICATION.
// The backendURL is the public base URL of this server (from BACKEND_URL env var).
func (h *AmazonMessagingWebhookHandler) RegisterAmazonMessagingWebhook(
	ctx context.Context,
	tenantID string,
	cred *models.MarketplaceCredential,
	backendURL string,
) error {
	mergedCreds, err := h.marketplaceService.GetFullCredentials(ctx, cred)
	if err != nil {
		return fmt.Errorf("get credentials: %w", err)
	}

	accessToken, err := h.messagingHandler.amazonLWAToken(mergedCreds)
	if err != nil {
		return fmt.Errorf("LWA auth: %w", err)
	}

	endpoint := h.messagingHandler.amazonMessagingEndpoint(mergedCreds["region"])
	webhookURL := strings.TrimRight(backendURL, "/") + "/webhooks/messages/amazon"

	// ── Step 1: Create APPLICATION destination ───────────────────────────────
	// Check if we already have a destination_id stored
	existingDestID := cred.CredentialData["amazon_notif_destination_id"]
	destID := existingDestID

	if destID == "" {
		destPayload := map[string]interface{}{
			"name": fmt.Sprintf("MarketMate-%s", cred.CredentialID[:8]),
			"deliveryChannel": map[string]interface{}{
				"eventBridgeApiDestination": nil,
				"eventBridgeConfiguration":  nil,
				// APPLICATION type: Amazon manages an SNS topic and POSTs to our URL
				"applicationInfo": map[string]interface{}{
					"url": webhookURL,
				},
			},
		}

		destBody, _ := json.Marshal(destPayload)
		req, _ := http.NewRequestWithContext(ctx, "POST",
			endpoint+"/notifications/v1/destinations",
			bytes.NewReader(destBody))
		req.Header.Set("x-amz-access-token", accessToken)
		req.Header.Set("Content-Type", "application/json")

		client := &http.Client{Timeout: 20 * time.Second}
		resp, err := client.Do(req)
		if err != nil {
			return fmt.Errorf("create destination: %w", err)
		}
		respBody, _ := io.ReadAll(resp.Body)
		resp.Body.Close()

		if resp.StatusCode != 200 && resp.StatusCode != 201 {
			return fmt.Errorf("create destination SP-API %d: %s", resp.StatusCode, string(respBody))
		}

		var destResult struct {
			Payload struct {
				DestinationID string `json:"destinationId"`
			} `json:"payload"`
		}
		if err := json.Unmarshal(respBody, &destResult); err != nil {
			return fmt.Errorf("parse destination response: %w", err)
		}
		destID = destResult.Payload.DestinationID
		log.Printf("[AmazonNotif] Created destination %s for credential %s", destID, cred.CredentialID)
	}

	// ── Step 2: Subscribe to notification types ─────────────────────────────
	// We subscribe to multiple notification types using the same destination.
	// Each type gets its own subscription_id stored in credential_data.
	notifTypes := []struct {
		notifType  string
		storeKey   string
	}{
		{"MESSAGING_NEW_MESSAGE_NOTIFICATION", "amazon_notif_subscription_id"},
		{"ORDER_CHANGE", "amazon_notif_order_change_sub_id"},
	}

	for _, nt := range notifTypes {
		existingSubID := cred.CredentialData[nt.storeKey]
		if existingSubID != "" {
			log.Printf("[AmazonNotif] Subscription %s already registered for %s", nt.notifType, cred.CredentialID)
			continue
		}
		subPayload := map[string]interface{}{
			"payloadVersion": "1.0",
			"destinationId":  destID,
		}
		subBody, _ := json.Marshal(subPayload)
		req, _ := http.NewRequestWithContext(ctx, "POST",
			fmt.Sprintf("%s/notifications/v1/subscriptions/%s", endpoint, nt.notifType),
			bytes.NewReader(subBody))
		req.Header.Set("x-amz-access-token", accessToken)
		req.Header.Set("Content-Type", "application/json")

		client := &http.Client{Timeout: 20 * time.Second}
		resp, err := client.Do(req)
		if err != nil {
			log.Printf("[AmazonNotif] WARNING: failed to subscribe to %s: %v", nt.notifType, err)
			continue
		}
		respBody, _ := io.ReadAll(resp.Body)
		resp.Body.Close()

		if resp.StatusCode == 409 {
			log.Printf("[AmazonNotif] Subscription %s already exists for %s", nt.notifType, cred.CredentialID)
			cred.CredentialData[nt.storeKey] = "existing"
		} else if resp.StatusCode != 200 && resp.StatusCode != 201 {
			log.Printf("[AmazonNotif] WARNING: subscription %s returned %d: %s", nt.notifType, resp.StatusCode, string(respBody))
		} else {
			var subResult struct {
				Payload struct {
					SubscriptionID string `json:"subscriptionId"`
				} `json:"payload"`
			}
			if err := json.Unmarshal(respBody, &subResult); err == nil && subResult.Payload.SubscriptionID != "" {
				cred.CredentialData[nt.storeKey] = subResult.Payload.SubscriptionID
			}
			log.Printf("[AmazonNotif] Subscribed to %s, sub=%s", nt.notifType, cred.CredentialData[nt.storeKey])
		}
	}

	// Keep subID for backwards compat field
	subID := cred.CredentialData["amazon_notif_subscription_id"]

	// ── Step 3: Persist IDs in credential_data ───────────────────────────────
	if cred.CredentialData == nil {
		cred.CredentialData = map[string]string{}
	}
	cred.CredentialData["amazon_notif_destination_id"] = destID
	cred.CredentialData["amazon_notif_subscription_id"] = subID
	cred.CredentialData["amazon_notif_webhook_url"] = webhookURL

	if err := h.marketplaceService.SaveCredential(ctx, cred); err != nil {
		log.Printf("[AmazonNotif] WARNING: saved subscription but failed to persist IDs: %v", err)
	}

	return nil
}

// UnregisterAmazonMessagingWebhook deletes the subscription and destination.
func (h *AmazonMessagingWebhookHandler) UnregisterAmazonMessagingWebhook(
	ctx context.Context,
	cred *models.MarketplaceCredential,
) error {
	mergedCreds, err := h.marketplaceService.GetFullCredentials(ctx, cred)
	if err != nil {
		return fmt.Errorf("get credentials: %w", err)
	}

	accessToken, err := h.messagingHandler.amazonLWAToken(mergedCreds)
	if err != nil {
		return fmt.Errorf("LWA auth: %w", err)
	}

	endpoint := h.messagingHandler.amazonMessagingEndpoint(mergedCreds["region"])
	client := &http.Client{Timeout: 20 * time.Second}

	// Delete subscription
	if subID := cred.CredentialData["amazon_notif_subscription_id"]; subID != "" {
		req, _ := http.NewRequestWithContext(ctx, "DELETE",
			fmt.Sprintf("%s/notifications/v1/subscriptions/MESSAGING_NEW_MESSAGE_NOTIFICATION/%s", endpoint, subID),
			nil)
		req.Header.Set("x-amz-access-token", accessToken)
		resp, err := client.Do(req)
		if err == nil {
			resp.Body.Close()
			log.Printf("[AmazonNotif] Deleted subscription %s", subID)
		}
	}

	// Delete destination
	if destID := cred.CredentialData["amazon_notif_destination_id"]; destID != "" {
		req, _ := http.NewRequestWithContext(ctx, "DELETE",
			fmt.Sprintf("%s/notifications/v1/destinations/%s", endpoint, destID),
			nil)
		req.Header.Set("x-amz-access-token", accessToken)
		resp, err := client.Do(req)
		if err == nil {
			resp.Body.Close()
			log.Printf("[AmazonNotif] Deleted destination %s", destID)
		}
	}

	return nil
}

// ============================================================================
// WEBHOOK ENDPOINT — SNS Confirmation + Message Delivery
// ============================================================================

// HandleAmazonMessagingWebhook handles both:
//   GET  /webhooks/messages/amazon — SNS SubscriptionConfirmation
//   POST /webhooks/messages/amazon — SNS Notification (message delivery)
func (h *AmazonMessagingWebhookHandler) HandleAmazonMessagingWebhook(c *gin.Context) {
	// Read body for POST; for GET the confirmation URL is in query params
	var bodyBytes []byte
	if c.Request.Method == "POST" {
		var err error
		bodyBytes, err = io.ReadAll(c.Request.Body)
		if err != nil {
			c.JSON(http.StatusBadRequest, gin.H{"error": "cannot read body"})
			return
		}
	}

	// SNS sends the message type in the x-amz-sns-message-type header
	msgType := c.GetHeader("x-amz-sns-message-type")
	if msgType == "" {
		// Some SNS deliveries put type in body JSON
		if len(bodyBytes) > 0 {
			var probe struct {
				Type string `json:"Type"`
			}
			json.Unmarshal(bodyBytes, &probe)
			msgType = probe.Type
		}
	}

	log.Printf("[AmazonNotif] Webhook received: method=%s type=%s", c.Request.Method, msgType)

	switch msgType {
	case "SubscriptionConfirmation":
		h.handleSNSConfirmation(c, bodyBytes)
	case "Notification":
		h.handleSNSNotification(c, bodyBytes)
	case "UnsubscribeConfirmation":
		log.Printf("[AmazonNotif] Received UnsubscribeConfirmation — ignoring")
		c.JSON(http.StatusOK, gin.H{"ok": true})
	default:
		// Return 200 to avoid SNS retry storms for unknown types
		log.Printf("[AmazonNotif] Unknown SNS message type: %q", msgType)
		c.JSON(http.StatusOK, gin.H{"ok": true})
	}
}

// ── SNS Subscription Confirmation ────────────────────────────────────────────

type snsMessage struct {
	Type             string `json:"Type"`
	MessageID        string `json:"MessageId"`
	Token            string `json:"Token"`
	TopicArn         string `json:"TopicArn"`
	Message          string `json:"Message"`
	SubscribeURL     string `json:"SubscribeURL"`
	Timestamp        string `json:"Timestamp"`
	SignatureVersion string `json:"SignatureVersion"`
	Signature        string `json:"Signature"`
	SigningCertURL   string `json:"SigningCertURL"`
}

func (h *AmazonMessagingWebhookHandler) handleSNSConfirmation(c *gin.Context, body []byte) {
	var msg snsMessage
	if err := json.Unmarshal(body, &msg); err != nil {
		c.JSON(http.StatusBadRequest, gin.H{"error": "invalid SNS confirmation body"})
		return
	}

	if msg.SubscribeURL == "" {
		c.JSON(http.StatusBadRequest, gin.H{"error": "missing SubscribeURL"})
		return
	}

	// Verify the URL is genuinely from Amazon SNS
	if !strings.Contains(msg.SubscribeURL, "amazonaws.com") {
		log.Printf("[AmazonNotif] WARNING: suspicious SubscribeURL: %s", msg.SubscribeURL)
		c.JSON(http.StatusBadRequest, gin.H{"error": "invalid SubscribeURL domain"})
		return
	}

	// Confirm the subscription by GETting the SubscribeURL
	client := &http.Client{Timeout: 15 * time.Second}
	resp, err := client.Get(msg.SubscribeURL)
	if err != nil {
		log.Printf("[AmazonNotif] Failed to confirm SNS subscription: %v", err)
		c.JSON(http.StatusInternalServerError, gin.H{"error": "subscription confirmation failed"})
		return
	}
	defer resp.Body.Close()

	log.Printf("[AmazonNotif] SNS subscription confirmed for topic %s (HTTP %d)", msg.TopicArn, resp.StatusCode)
	c.JSON(http.StatusOK, gin.H{"ok": true, "confirmed": true})
}

// ── SNS Notification (actual message delivery) ────────────────────────────────

func (h *AmazonMessagingWebhookHandler) handleSNSNotification(c *gin.Context, body []byte) {
	var snsMsg snsMessage
	if err := json.Unmarshal(body, &snsMsg); err != nil {
		log.Printf("[AmazonNotif] Failed to parse SNS notification: %v", err)
		c.JSON(http.StatusBadRequest, gin.H{"error": "invalid SNS notification"})
		return
	}

	// Verify SNS signature to ensure this is genuinely from Amazon
	if err := h.verifySNSSignature(snsMsg); err != nil {
		log.Printf("[AmazonNotif] SNS signature verification failed: %v", err)
		c.JSON(http.StatusUnauthorized, gin.H{"error": "signature verification failed"})
		return
	}

	// The SNS Message field contains the actual SP-API notification as JSON
	var notif struct {
		NotificationType string `json:"NotificationType"`
		EventTime        string `json:"EventTime"`
		Payload          struct {
			// MESSAGING_NEW_MESSAGE_NOTIFICATION
			BuyerSellerMessagingNotification *struct {
				AmazonOrderID string `json:"amazonOrderId"`
				BuyerInfo     struct {
					BuyerName string `json:"buyerName"`
				} `json:"buyerInfo"`
				Message struct {
					MessageID     string `json:"messageId"`
					Text          string `json:"text"`
					SentDate      string `json:"sentDate"`
					FromRole      string `json:"fromRole"` // "BUYER" or "SELLER"
					MarketplaceID string `json:"marketplaceId"`
				} `json:"message"`
			} `json:"BuyerSellerMessagingNotification"`
			// ORDER_CHANGE
			OrderChangeNotification *struct {
				AmazonOrderID   string `json:"amazonOrderId"`
				OrderChangeType string `json:"orderChangeType"` // BUYER_REQUESTED_CANCEL, ORDER_STATUS_CHANGE etc
				BuyerInfo       struct {
					BuyerName string `json:"buyerName"`
				} `json:"buyerInfo"`
				OrderStatus string `json:"orderStatus"`
				Summary     string `json:"summary,omitempty"`
			} `json:"OrderChangeNotification"`
		} `json:"payload"`
		// SP-API wraps in sellerId for routing
		SellerID string `json:"sellerId"`
	}

	if err := json.Unmarshal([]byte(snsMsg.Message), &notif); err != nil {
		log.Printf("[AmazonNotif] Failed to parse notification payload: %v", err)
		// Return 200 anyway to prevent SNS retries for malformed payloads
		c.JSON(http.StatusOK, gin.H{"ok": true})
		return
	}

	ctx := c.Request.Context()

	switch notif.NotificationType {
	case "MESSAGING_NEW_MESSAGE_NOTIFICATION":
		msgNotif := notif.Payload.BuyerSellerMessagingNotification
		if msgNotif == nil {
			break
		}
		log.Printf("[AmazonNotif] New message for order %s from %s (seller %s)",
			msgNotif.AmazonOrderID, msgNotif.Message.FromRole, notif.SellerID)
		if err := h.storeMessageNotification(ctx, notif.SellerID, msgNotif.Message.MarketplaceID, msgNotif); err != nil {
			log.Printf("[AmazonNotif] Failed to store message: %v", err)
		}

	case "ORDER_CHANGE":
		orderNotif := notif.Payload.OrderChangeNotification
		if orderNotif == nil {
			break
		}
		log.Printf("[AmazonNotif] ORDER_CHANGE for order %s type=%s (seller %s)",
			orderNotif.AmazonOrderID, orderNotif.OrderChangeType, notif.SellerID)
		if orderNotif.OrderChangeType == "BUYER_REQUESTED_CANCEL" {
			h.handleAmazonCancelRequest(ctx, notif.SellerID, orderNotif)
		}

	default:
		log.Printf("[AmazonNotif] Ignoring notification type: %s", notif.NotificationType)
	}

	c.JSON(http.StatusOK, gin.H{"ok": true})
}

// storeMessageNotification finds the tenant/credential matching the seller ID
// and upserts the conversation + message in Firestore.
func (h *AmazonMessagingWebhookHandler) storeMessageNotification(
	ctx context.Context,
	sellerID string,
	marketplaceID string,
	msgNotif *struct {
		AmazonOrderID string `json:"amazonOrderId"`
		BuyerInfo     struct {
			BuyerName string `json:"buyerName"`
		} `json:"buyerInfo"`
		Message struct {
			MessageID     string `json:"messageId"`
			Text          string `json:"text"`
			SentDate      string `json:"sentDate"`
			FromRole      string `json:"fromRole"`
			MarketplaceID string `json:"marketplaceId"`
		} `json:"message"`
	},
) error {
	// Scan all tenants for a credential matching this seller_id
	tenantIter := h.client.Collection("tenants").Documents(ctx)
	defer tenantIter.Stop()

	for {
		tenantDoc, err := tenantIter.Next()
		if err == iterator.Done {
			break
		}
		if err != nil {
			return fmt.Errorf("iterate tenants: %w", err)
		}
		tenantID := tenantDoc.Ref.ID

		// Check credentials for this tenant
		credIter := h.client.Collection("tenants").Doc(tenantID).
			Collection("marketplace_credentials").
			Where("active", "==", true).
			Documents(ctx)

		found := false
		for {
			credDoc, err := credIter.Next()
			if err == iterator.Done {
				break
			}
			if err != nil {
				break
			}

			var cred models.MarketplaceCredential
			if err := credDoc.DataTo(&cred); err != nil {
				continue
			}
			if cred.Channel != "amazon" && cred.Channel != "amazonnew" {
				continue
			}

			// Match by seller_id stored in credential_data
			credSellerID := cred.CredentialData["seller_id"]
			if credSellerID == "" {
				// Try nested credential_data structure
				credSellerID = cred.CredentialData["seller_id"]
			}
			if credSellerID != sellerID {
				continue
			}

			// Found the matching credential — store the message
			found = true
			credIter.Stop()
			if err := h.upsertConversationAndMessage(ctx, tenantID, &cred, msgNotif); err != nil {
				return err
			}
			break
		}
		if found {
			break
		}
	}
	return nil
}

// upsertConversationAndMessage creates or updates the conversation and adds
// the new message to Firestore.
func (h *AmazonMessagingWebhookHandler) upsertConversationAndMessage(
	ctx context.Context,
	tenantID string,
	cred *models.MarketplaceCredential,
	msgNotif *struct {
		AmazonOrderID string `json:"amazonOrderId"`
		BuyerInfo     struct {
			BuyerName string `json:"buyerName"`
		} `json:"buyerInfo"`
		Message struct {
			MessageID     string `json:"messageId"`
			Text          string `json:"text"`
			SentDate      string `json:"sentDate"`
			FromRole      string `json:"fromRole"`
			MarketplaceID string `json:"marketplaceId"`
		} `json:"message"`
	},
) error {
	convID := fmt.Sprintf("amz_%s_%s", cred.CredentialID, msgNotif.AmazonOrderID)
	msgID := fmt.Sprintf("amz_%s", msgNotif.Message.MessageID)

	convRef := h.messagingHandler.convDoc(tenantID, convID)
	msgRef := h.messagingHandler.msgCol(tenantID, convID).Doc(msgID)

	// Skip if message already stored (idempotent)
	existing, _ := msgRef.Get(ctx)
	if existing.Exists() {
		log.Printf("[AmazonNotif] Message %s already stored — skipping", msgID)
		return nil
	}

	now := time.Now()

	sentAt, _ := time.Parse(time.RFC3339, msgNotif.Message.SentDate)
	if sentAt.IsZero() {
		sentAt = now
	}

	direction := models.MsgDirectionInbound
	if msgNotif.Message.FromRole == "SELLER" {
		direction = models.MsgDirectionOutbound
	}

	// Upsert conversation
	convDoc, _ := convRef.Get(ctx)
	var conv models.Conversation
	if convDoc.Exists() {
		convDoc.DataTo(&conv)
	} else {
		conv = models.Conversation{
			ConversationID:      convID,
			TenantID:            tenantID,
			Channel:             cred.Channel,
			ChannelAccountID:    cred.CredentialID,
			MarketplaceThreadID: msgNotif.AmazonOrderID,
			OrderNumber:         msgNotif.AmazonOrderID,
			Customer: models.ConversationCustomer{
				Name: msgNotif.BuyerInfo.BuyerName,
			},
			Subject:   fmt.Sprintf("Order %s", msgNotif.AmazonOrderID),
			Status:    models.ConvStatusOpen,
			CreatedAt: now,
		}
	}

	preview := msgNotif.Message.Text
	if len(preview) > 100 {
		preview = preview[:100] + "…"
	}
	conv.LastMessageAt = sentAt
	conv.LastMessagePreview = preview
	conv.MessageCount = conv.MessageCount + 1
	conv.UpdatedAt = now
	if direction == models.MsgDirectionInbound {
		conv.Unread = true
		conv.Status = models.ConvStatusOpen
	}

	if _, err := convRef.Set(ctx, conv); err != nil {
		return fmt.Errorf("upsert conversation: %w", err)
	}

	// Store message
	msg := models.Message{
		MessageID:        msgID,
		ConversationID:   convID,
		Direction:        direction,
		Body:             msgNotif.Message.Text,
		ChannelMessageID: msgNotif.Message.MessageID,
		SentAt:           sentAt,
	}
	if _, err := msgRef.Set(ctx, msg); err != nil {
		return fmt.Errorf("store message: %w", err)
	}

	log.Printf("[AmazonNotif] Stored message %s for order %s (tenant %s, direction %s)",
		msgID, msgNotif.AmazonOrderID, tenantID, direction)
	return nil
}


// ============================================================================
// AMAZON CANCEL REQUEST HANDLER
// ============================================================================
// Creates a messaging ticket and alerts staff when a buyer requests
// cancellation of an Amazon order via ORDER_CHANGE notification.

func (h *AmazonMessagingWebhookHandler) handleAmazonCancelRequest(
	ctx context.Context,
	sellerID string,
	orderNotif *struct {
		AmazonOrderID   string `json:"amazonOrderId"`
		OrderChangeType string `json:"orderChangeType"`
		BuyerInfo       struct {
			BuyerName string `json:"buyerName"`
		} `json:"buyerInfo"`
		OrderStatus string `json:"orderStatus"`
		Summary     string `json:"summary,omitempty"`
	},
) {
	// Find tenant + credential by seller_id
	tenantID, credID := h.findTenantBySellerID(ctx, sellerID)
	if tenantID == "" {
		log.Printf("[AmazonNotif] Cancel request: could not find tenant for seller %s order %s", sellerID, orderNotif.AmazonOrderID)
		return
	}

	now := time.Now()
	convID := fmt.Sprintf("amz_cancel_%s_%s", credID, orderNotif.AmazonOrderID)

	subject := fmt.Sprintf("⚠️ Amazon Cancellation Request — Order %s", orderNotif.AmazonOrderID)
	bodyText := fmt.Sprintf(
		"A buyer has requested cancellation of an Amazon order.\n\n"+
			"Order: %s\n"+
			"Buyer: %s\n"+
			"Order Status: %s\n\n"+
			"⚠️ STOP — do not dispatch this order until resolved.\n"+
			"If a label has already been printed, do not ship.\n"+
			"Go to Messages to respond to the buyer.",
		orderNotif.AmazonOrderID,
		orderNotif.BuyerInfo.BuyerName,
		orderNotif.OrderStatus,
	)

	// Upsert conversation
	convRef := h.messagingHandler.convDoc(tenantID, convID)
	existingSnap, _ := convRef.Get(ctx)

	var conv models.Conversation
	if existingSnap.Exists() {
		existingSnap.DataTo(&conv)
		convRef.Update(ctx, []firestore.Update{
			{Path: "last_message_at", Value: now},
			{Path: "last_message_preview", Value: bodyText[:min(100, len(bodyText))]},
			{Path: "unread", Value: true},
			{Path: "status", Value: models.ConvStatusOpen},
			{Path: "updated_at", Value: now},
		})
	} else {
		conv = models.Conversation{
			ConversationID:      convID,
			TenantID:            tenantID,
			Channel:             "amazon",
			ChannelAccountID:    credID,
			MarketplaceThreadID: orderNotif.AmazonOrderID,
			OrderNumber:         orderNotif.AmazonOrderID,
			Customer:            models.ConversationCustomer{Name: orderNotif.BuyerInfo.BuyerName},
			Subject:             subject,
			Status:              models.ConvStatusOpen,
			LastMessageAt:       now,
			LastMessagePreview:  bodyText[:min(100, len(bodyText))],
			Unread:              true,
			MessageCount:        1,
			CreatedAt:           now,
			UpdatedAt:           now,
		}
		convRef.Set(ctx, conv)
	}

	// Store alert message
	msgID := fmt.Sprintf("amz_cancel_%d", now.UnixNano())
	h.messagingHandler.msgCol(tenantID, convID).Doc(msgID).Set(ctx, models.Message{
		MessageID:      msgID,
		ConversationID: convID,
		Direction:      models.MsgDirectionInbound,
		Body:           bodyText,
		SentBy:         "amazon_platform",
		SentAt:         now,
	})

	log.Printf("[AmazonNotif] Cancel ticket created: conv=%s tenant=%s order=%s",
		convID, tenantID, orderNotif.AmazonOrderID)

	// Alert all team members
	if h.notifier != nil {
		members, err := h.notifier.GetAssignableMembers(ctx, tenantID)
		if err == nil {
			for _, m := range members {
				member := m
				go h.notifier.NotifyAssignment(context.Background(), &member, &conv, "Amazon Platform")
			}
		}
	}
}

// findTenantBySellerID scans all active amazon/amazonnew credentials to find
// the tenant matching the given seller_id.
func (h *AmazonMessagingWebhookHandler) findTenantBySellerID(ctx context.Context, sellerID string) (tenantID, credID string) {
	tenantIter := h.client.Collection("tenants").Documents(ctx)
	defer tenantIter.Stop()
	for {
		tenantDoc, err := tenantIter.Next()
		if err != nil {
			break
		}
		tid := tenantDoc.Ref.ID
		credIter := h.client.Collection("tenants").Doc(tid).
			Collection("marketplace_credentials").
			Where("active", "==", true).
			Documents(ctx)
		for {
			credDoc, err := credIter.Next()
			if err != nil {
				break
			}
			var cred models.MarketplaceCredential
			if err := credDoc.DataTo(&cred); err != nil {
				continue
			}
			if cred.Channel != "amazon" && cred.Channel != "amazonnew" {
				continue
			}
			if cred.CredentialData["seller_id"] == sellerID {
				credIter.Stop()
				return tid, cred.CredentialID
			}
		}
		credIter.Stop()
	}
	return "", ""
}


// ============================================================================
// SNS SIGNATURE VERIFICATION
// ============================================================================
// Verifies the SNS message signature to ensure it genuinely came from Amazon.
// Amazon signs messages with an RSA private key; the cert is at SigningCertURL.
// Reference: https://docs.aws.amazon.com/sns/latest/dg/sns-verify-signature-of-message.html

func (h *AmazonMessagingWebhookHandler) verifySNSSignature(msg snsMessage) error {
	// Only trust certs from Amazon's SNS certificate URLs
	certURL := msg.SigningCertURL
	if !strings.HasPrefix(certURL, "https://sns.") || !strings.Contains(certURL, ".amazonaws.com/") {
		return fmt.Errorf("untrusted SigningCertURL: %s", certURL)
	}

	// Fetch the signing certificate
	certResp, err := http.Get(certURL)
	if err != nil {
		return fmt.Errorf("fetch signing cert: %w", err)
	}
	defer certResp.Body.Close()
	certPEM, err := io.ReadAll(certResp.Body)
	if err != nil {
		return fmt.Errorf("read signing cert: %w", err)
	}

	block, _ := pem.Decode(certPEM)
	if block == nil {
		return fmt.Errorf("invalid PEM cert")
	}
	cert, err := x509.ParseCertificate(block.Bytes)
	if err != nil {
		return fmt.Errorf("parse cert: %w", err)
	}

	// Build the string to sign (field order depends on message type)
	var signStr string
	if msg.Type == "Notification" {
		signStr = fmt.Sprintf("Message\n%s\nMessageId\n%s\nTimestamp\n%s\nTopicArn\n%s\nType\n%s\n",
			msg.Message, msg.MessageID, msg.Timestamp, msg.TopicArn, msg.Type)
	} else {
		// SubscriptionConfirmation / UnsubscribeConfirmation
		signStr = fmt.Sprintf("Message\n%s\nMessageId\n%s\nSubscribeURL\n%s\nTimestamp\n%s\nToken\n%s\nTopicArn\n%s\nType\n%s\n",
			msg.Message, msg.MessageID, msg.SubscribeURL, msg.Timestamp, msg.Token, msg.TopicArn, msg.Type)
	}

	// Decode the base64 signature
	sig, err := base64.StdEncoding.DecodeString(msg.Signature)
	if err != nil {
		return fmt.Errorf("decode signature: %w", err)
	}

	// Verify using the cert's public key
	if err := cert.CheckSignature(
		x509.SHA1WithRSA,
		[]byte(signStr),
		sig,
	); err != nil {
		return fmt.Errorf("signature invalid: %w", err)
	}

	return nil
}

// ============================================================================
// REGISTER ON CREDENTIAL SAVE — helper called from marketplace handler
// ============================================================================

// TryRegisterAmazonMessagingWebhook attempts to register the webhook for a
// credential. Logs errors but does not fail the credential save operation.
func (h *AmazonMessagingWebhookHandler) TryRegisterAmazonMessagingWebhook(
	ctx context.Context,
	tenantID string,
	cred *models.MarketplaceCredential,
	backendURL string,
) {
	if backendURL == "" {
		log.Printf("[AmazonNotif] BACKEND_URL not set — skipping webhook registration for %s", cred.CredentialID)
		return
	}
	if cred.Channel != "amazon" && cred.Channel != "amazonnew" {
		return
	}

	go func() {
		bgCtx := context.Background()
		if err := h.RegisterAmazonMessagingWebhook(bgCtx, tenantID, cred, backendURL); err != nil {
			log.Printf("[AmazonNotif] WARNING: webhook registration failed for %s/%s: %v",
				tenantID, cred.CredentialID, err)
		} else {
			log.Printf("[AmazonNotif] Webhook registered for %s/%s", tenantID, cred.CredentialID)
		}
	}()
}

// RegisterAllExistingCredentials registers webhooks for all existing
// amazon/amazonnew credentials that don't already have a destination_id.
// Called once at startup.
func (h *AmazonMessagingWebhookHandler) RegisterAllExistingCredentials(
	ctx context.Context,
	backendURL string,
) {
	if backendURL == "" {
		log.Println("[AmazonNotif] BACKEND_URL not set — skipping bulk webhook registration")
		return
	}

	tenantIter := h.client.Collection("tenants").Documents(ctx)
	defer tenantIter.Stop()

	registered := 0
	skipped := 0

	for {
		tenantDoc, err := tenantIter.Next()
		if err == iterator.Done {
			break
		}
		if err != nil {
			log.Printf("[AmazonNotif] Error iterating tenants: %v", err)
			break
		}
		tenantID := tenantDoc.Ref.ID

		credIter := h.client.Collection("tenants").Doc(tenantID).
			Collection("marketplace_credentials").
			Where("active", "==", true).
			Documents(ctx)

		for {
			credDoc, err := credIter.Next()
			if err == iterator.Done {
				break
			}
			if err != nil {
				break
			}

			var cred models.MarketplaceCredential
			if err := credDoc.DataTo(&cred); err != nil {
				continue
			}
			if cred.Channel != "amazon" && cred.Channel != "amazonnew" {
				continue
			}
			// Already registered
			if cred.CredentialData["amazon_notif_destination_id"] != "" {
				skipped++
				continue
			}

			// Register in background goroutine
			credCopy := cred
			go func(tid string, c models.MarketplaceCredential) {
				bgCtx := context.Background()
				if err := h.RegisterAmazonMessagingWebhook(bgCtx, tid, &c, backendURL); err != nil {
					log.Printf("[AmazonNotif] Startup registration failed %s/%s: %v", tid, c.CredentialID, err)
				} else {
					log.Printf("[AmazonNotif] Startup registration OK %s/%s", tid, c.CredentialID)
				}
			}(tenantID, credCopy)
			registered++
		}
		credIter.Stop()
	}

	log.Printf("[AmazonNotif] Startup registration: %d queued, %d already registered", registered, skipped)
}

// ── Helpers ───────────────────────────────────────────────────────────────────

func newUUID() string {
	return uuid.New().String()
}
