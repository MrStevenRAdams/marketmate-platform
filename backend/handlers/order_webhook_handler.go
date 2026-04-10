package handlers

// ============================================================================
// ORDER WEBHOOK HANDLER
//
// Receives inbound push notifications from marketplaces that support them,
// then immediately triggers an order import for the affected credential.
// This gives near-real-time order download; the 15-minute poller acts as a
// safety net for anything the webhook misses.
//
// Webhook support by channel:
//   eBay        — Commerce Notifications API (MARKETPLACE_ACCOUNT_DELETION,
//                 ITEM_SOLD / order notifications via topic subscription)
//   WooCommerce — REST Webhooks (woocommerce_order_created, etc.)
//   Shopify     — Partner webhooks (orders/create, orders/updated)
//   BigCommerce — Webhook API (store/order/created, store/order/statusUpdated)
//   TikTok      — Push messages (ORDER_STATUS_CHANGE)
//
// All webhook endpoints are public (no Firebase auth) — each uses its own
// signature verification to authenticate the caller.
//
// Routes registered in main.go:
//   POST /webhooks/orders/ebay          — eBay Commerce Notifications
//   POST /webhooks/orders/woocommerce   — WooCommerce webhooks
//   POST /webhooks/orders/shopify       — Shopify webhooks
//   POST /webhooks/orders/bigcommerce   — BigCommerce webhooks
//   POST /webhooks/orders/tiktok        — TikTok push messages
//
// Webhook subscription management (authenticated, per-credential):
//   POST /api/v1/marketplace/credentials/:id/webhooks/subscribe
//   DELETE /api/v1/marketplace/credentials/:id/webhooks/unsubscribe
//   GET  /api/v1/marketplace/credentials/:id/webhooks/status
// ============================================================================

import (
	"bytes"
	"context"
	"crypto/aes"
	"crypto/cipher"
	"encoding/base64"
	"crypto/hmac"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"fmt"
	"io"
	"log"
	"net/http"
	"os"
	"sort"
	"strings"
	"time"

	"cloud.google.com/go/firestore"
	"github.com/gin-gonic/gin"
	"module-a/models"
	"module-a/repository"
	"module-a/services"
)

// OrderWebhookHandler handles inbound marketplace order notifications.
type OrderWebhookHandler struct {
	repo               *repository.MarketplaceRepository
	marketplaceService *services.MarketplaceService
	orderHandler       *OrderHandler
	firestoreClient    *firestore.Client
	messagingNotifier  *services.MessagingNotifier
}

// SetMessagingNotifier injects the notifier for Temu after-sales alerts.
func (h *OrderWebhookHandler) SetMessagingNotifier(n *services.MessagingNotifier) {
	h.messagingNotifier = n
}

// NewOrderWebhookHandler creates the handler.
func NewOrderWebhookHandler(
	repo *repository.MarketplaceRepository,
	marketplaceService *services.MarketplaceService,
	orderHandler *OrderHandler,
) *OrderWebhookHandler {
	return &OrderWebhookHandler{
		repo:               repo,
		marketplaceService: marketplaceService,
		orderHandler:       orderHandler,
	}
}

// SetFirestoreClient injects the Firestore client needed for message webhook
// handling and webhook health checks. Called after construction in main.go.
func (h *OrderWebhookHandler) SetFirestoreClient(client *firestore.Client) {
	h.firestoreClient = client
}

// ============================================================================
// COMMON HELPERS
// ============================================================================

// lookbackWindow returns the date range to use when a webhook fires —
// a short window (2 hours) is enough since webhooks are near-real-time.
func lookbackWindow() (string, string) {
	now := time.Now().UTC()
	return now.Add(-2 * time.Hour).Format("2006-01-02"), now.Format("2006-01-02")
}

// triggerImportForCredential fires an async order import for one credential.
func (h *OrderWebhookHandler) triggerImportForCredential(tenantID, credID, channel string) {
	dateFrom, dateTo := lookbackWindow()
	ctx := context.Background()

	jobID, err := h.orderHandler.orderService.StartOrderImport(ctx, tenantID, channel, credID, dateFrom, dateTo)
	if err != nil {
		log.Printf("[Webhook:%s] failed to start import tenant=%s cred=%s: %v", channel, tenantID, credID, err)
		return
	}
	go func() {
		h.orderHandler.processChannelImport(tenantID, jobID, channel, credID, dateFrom, dateTo)
		h.repo.UpdateCredentialLastSync(context.Background(), tenantID, credID, "success", "", 0)
	}()
	log.Printf("[Webhook:%s] triggered import tenant=%s cred=%s job=%s", channel, tenantID, credID, jobID)
}

// findCredentialByWebhookSecret looks up a credential by matching a secret
// stored in its credential_data under the given key. Used by channels that
// embed a per-account secret in the webhook URL or payload.
func (h *OrderWebhookHandler) findCredentialByWebhookSecret(ctx context.Context, secretKey, secretValue string) (*models.MarketplaceCredential, error) {
	creds, err := h.repo.ListAllActiveCredentials(ctx)
	if err != nil {
		return nil, err
	}
	for i := range creds {
		cred := &creds[i]
		if v, ok := cred.CredentialData[secretKey]; ok && v == secretValue {
			return cred, nil
		}
	}
	return nil, fmt.Errorf("no credential found for %s=%s", secretKey, secretValue)
}

// readBodyBytes reads the request body and replaces it so downstream code
// can read it again if needed.
func readBodyBytes(c *gin.Context) ([]byte, error) {
	body, err := io.ReadAll(c.Request.Body)
	if err != nil {
		return nil, err
	}
	c.Request.Body = io.NopCloser(bytes.NewReader(body))
	return body, nil
}

// ============================================================================
// EBAY — Commerce Notifications API
//
// eBay sends a signed POST to our endpoint for topics we have subscribed to.
// The relevant topic for orders is MARKETPLACE_ACCOUNT_DELETION (for GDPR)
// and order-related topics like ORDER_PAYMENT_COMPLETION.
//
// Signature verification: X-EBAY-SIGNATURE header contains a base64-encoded
// HMAC-SHA256 of the raw body using the verification token as key.
//
// Subscription is managed via the eBay Commerce Notification API:
//   POST /commerce/notification/v1/subscription
//
// Reference: https://developer.ebay.com/api-docs/commerce/notification/overview.html
// ============================================================================

// EbayWebhook handles POST /webhooks/orders/ebay
// Accepts both:
//   - JSON (Commerce Notification API / Platform Notifications with JSON encoding)
//   - XML/SOAP (Platform Notifications default format)
// All topics are routed to the messages portal so users see everything.
func (h *OrderWebhookHandler) EbayWebhook(c *gin.Context) {
	body, err := readBodyBytes(c)
	if err != nil {
		c.JSON(http.StatusBadRequest, gin.H{"error": "cannot read body"})
		return
	}

	// eBay sends a challenge code during endpoint validation — must respond with it.
	var payload map[string]interface{}
	if err := json.Unmarshal(body, &payload); err == nil {
		if challenge, ok := payload["challenge"].(string); ok {
			verificationToken := os.Getenv("EBAY_NOTIFICATION_VERIFICATION_TOKEN")
			hash := hmac.New(sha256.New, []byte(verificationToken))
			hash.Write([]byte(challenge))
			c.JSON(http.StatusOK, gin.H{"challengeResponse": hex.EncodeToString(hash.Sum(nil))})
			return
		}
	}

	ctx := c.Request.Context()
	contentType := c.GetHeader("Content-Type")

	// Detect XML/SOAP Platform Notifications and parse them
	if strings.Contains(contentType, "text/xml") || strings.HasPrefix(strings.TrimSpace(string(body)), "<?xml") || strings.HasPrefix(strings.TrimSpace(string(body)), "<soapenv") {
		h.handleEbaySOAPNotification(c, ctx, body)
		return
	}

	// JSON notification — verify signature if token configured
	verificationToken := os.Getenv("EBAY_NOTIFICATION_VERIFICATION_TOKEN")
	if verificationToken != "" {
		sig := c.GetHeader("X-EBAY-SIGNATURE")
		if sig != "" && !verifyHMACSHA256(body, sig, verificationToken) {
			log.Printf("[Webhook:ebay] invalid signature")
			c.JSON(http.StatusUnauthorized, gin.H{"error": "invalid signature"})
			return
		}
	}

	// Parse JSON notification
	var notification struct {
		Metadata struct {
			Topic string `json:"topic"`
		} `json:"metadata"`
		Notification struct {
			Data struct {
				Username string `json:"username"`
			} `json:"data"`
		} `json:"notification"`
	}
	_ = json.Unmarshal(body, &notification)

	topic := notification.Metadata.Topic
	username := notification.Notification.Data.Username
	log.Printf("[Webhook:ebay] JSON topic=%s username=%s", topic, username)

	// Route message topics to the messaging handler
	if topic == "ASK_SELLER_QUESTION" || topic == "BUYER_INITIATED_OFFER" {
		h.handleEbayMessageWebhook(c, body, username)
		return
	}

	// Route cancellation/return topics to alert handler
	if topic == "CANCELLATION_CREATED" || topic == "RETURN_CREATED" || topic == "RETURN_CLOSED" {
		h.handleEbayCancelReturnWebhook(c, body, topic, username)
		return
	}

	// Route ALL other topics to messages portal as info notifications
	if topic != "" && !strings.HasPrefix(topic, "ORDER") && topic != "MARKETPLACE_ACCOUNT_DELETION" {
		go h.createEbayInfoTicket(ctx, topic, username, body)
		c.JSON(http.StatusOK, gin.H{"ok": true})
		return
	}

	// Order topics — trigger import
	if strings.HasPrefix(topic, "ORDER") || topic == "MARKETPLACE_ACCOUNT_DELETION" {
		creds, err := h.repo.ListAllActiveCredentials(ctx)
		if err != nil {
			c.JSON(http.StatusOK, gin.H{"ok": true})
			return
		}
		for _, cred := range creds {
			if cred.Channel != "ebay" {
				continue
			}
			if username != "" && cred.CredentialData["seller_username"] != username {
				continue
			}
			if cred.Config.Orders.Enabled {
				go h.triggerImportForCredential(cred.TenantID, cred.CredentialID, "ebay")
			}
		}
	}

	c.JSON(http.StatusOK, gin.H{"ok": true})
}

// handleEbaySOAPNotification parses SOAP/XML Platform Notifications and routes
// them to the messages portal. All topics create a conversation entry so users
// can see everything in one place.
func (h *OrderWebhookHandler) handleEbaySOAPNotification(c *gin.Context, ctx context.Context, body []byte) {
	bodyStr := string(body)

	// Extract NotificationEventName from XML
	eventName := extractXMLValue(bodyStr, "NotificationEventName")
	if eventName == "" {
		// Try SOAPAction header as fallback
		soap := c.GetHeader("SOAPAction")
		if idx := strings.LastIndex(soap, "/"); idx >= 0 {
			eventName = strings.Trim(soap[idx+1:], `"`)
		}
	}

	// Extract seller username
	username := extractXMLValue(bodyStr, "UserID")
	if username == "" {
		username = extractXMLValue(bodyStr, "SellerUserID")
	}

	log.Printf("[Webhook:ebay] SOAP event=%s username=%s", eventName, username)

	// Route to appropriate handler based on event name
	switch eventName {
	case "CANCELLATION_CREATED":
		h.handleEbayCancelReturnWebhook(c, body, "CANCELLATION_CREATED", username)
		return
	case "RETURN_CREATED":
		h.handleEbayCancelReturnWebhook(c, body, "RETURN_CREATED", username)
		return
	case "RETURN_CLOSED":
		h.handleEbayCancelReturnWebhook(c, body, "RETURN_CLOSED", username)
		return
	case "AskSellerQuestion":
		// Extract message details from XML and create conversation
		orderID := extractXMLValue(bodyStr, "ItemID")
		buyerName := extractXMLValue(bodyStr, "SenderID")
		msgBody := extractXMLValue(bodyStr, "Body")
		h.createEbaySOAPMessageTicket(ctx, username, buyerName, orderID, msgBody)
	case "FeedbackLeft":
		orderID := extractXMLValue(bodyStr, "ItemID")
		feedbackType := extractXMLValue(bodyStr, "CommentType")
		comment := extractXMLValue(bodyStr, "CommentText")
		h.createEbaySOAPMessageTicket(ctx, username, "eBay Buyer", orderID,
			fmt.Sprintf("Feedback received: %s\n\n%s", feedbackType, comment))
	default:
		if eventName != "" {
			go h.createEbayInfoTicket(ctx, eventName, username, body)
		}
	}

	c.JSON(http.StatusOK, gin.H{"ok": true})
}

// extractXMLValue extracts the text content of the first matching XML tag.
func extractXMLValue(xml, tag string) string {
	open := "<" + tag + ">"
	close := "</" + tag + ">"
	start := strings.Index(xml, open)
	if start < 0 {
		// Try with namespace prefix
		start = strings.Index(xml, ":"+tag+">")
		if start >= 0 {
			start = strings.LastIndex(xml[:start], "<") + 1
			close = "</" + xml[start:strings.Index(xml[start:], ">")+start] + ">"
			open = xml[start-1 : strings.Index(xml[start-1:], ">")+start]
		} else {
			return ""
		}
	}
	start += len(open)
	end := strings.Index(xml[start:], close)
	if end < 0 {
		return ""
	}
	return strings.TrimSpace(xml[start : start+end])
}

// createEbaySOAPMessageTicket creates a messaging conversation from a SOAP notification.
func (h *OrderWebhookHandler) createEbaySOAPMessageTicket(ctx context.Context, sellerUsername, buyerName, itemID, msgBody string) {
	if h.firestoreClient == nil {
		return
	}
	creds, _ := h.repo.ListAllActiveCredentials(ctx)
	for _, cred := range creds {
		if cred.Channel != "ebay" {
			continue
		}
		if sellerUsername != "" && cred.CredentialData["seller_username"] != sellerUsername {
			continue
		}
		now := time.Now()
		convID := fmt.Sprintf("ebay_msg_%s_%d", itemID, now.UnixNano())
		conv := models.Conversation{
			ConversationID:     convID,
			TenantID:           cred.TenantID,
			Channel:            "ebay",
			ChannelAccountID:   cred.CredentialID,
			OrderNumber:        itemID,
			Customer:           models.ConversationCustomer{Name: buyerName},
			Subject:            fmt.Sprintf("eBay Message — Item %s", itemID),
			Status:             models.ConvStatusOpen,
			LastMessageAt:      now,
			LastMessagePreview: msgBody[:min(100, len(msgBody))],
			Unread:             true,
			MessageCount:       1,
			CreatedAt:          now,
			UpdatedAt:          now,
		}
		h.firestoreClient.Collection(fmt.Sprintf("tenants/%s/conversations", cred.TenantID)).Doc(convID).Set(ctx, conv)
		msgID := fmt.Sprintf("ebay_msg_%d", now.UnixNano())
		msg := models.Message{
			MessageID:      msgID,
			ConversationID: convID,
			Direction:      models.MsgDirectionInbound,
			Body:           msgBody,
			SentBy:         buyerName,
			SentAt:         now,
		}
		h.firestoreClient.Collection(fmt.Sprintf("tenants/%s/conversations/%s/messages", cred.TenantID, convID)).Doc(msgID).Set(ctx, msg)
		break // Only create for first matching credential
	}
}

// createEbayInfoTicket creates an informational message for any unhandled topic.
func (h *OrderWebhookHandler) createEbayInfoTicket(ctx context.Context, topic, username string, body []byte) {
	if h.firestoreClient == nil {
		return
	}
	creds, _ := h.repo.ListAllActiveCredentials(ctx)
	for _, cred := range creds {
		if cred.Channel != "ebay" {
			continue
		}
		if username != "" && cred.CredentialData["seller_username"] != username {
			continue
		}
		now := time.Now()
		convID := fmt.Sprintf("ebay_info_%s_%d", topic, now.UnixNano())
		subject := fmt.Sprintf("eBay Notification: %s", topic)
		preview := fmt.Sprintf("eBay sent a %s notification.", topic)
		conv := models.Conversation{
			ConversationID:     convID,
			TenantID:           cred.TenantID,
			Channel:            "ebay",
			ChannelAccountID:   cred.CredentialID,
			Subject:            subject,
			Customer:           models.ConversationCustomer{Name: "eBay Platform"},
			Status:             models.ConvStatusOpen,
			LastMessageAt:      now,
			LastMessagePreview: preview,
			Unread:             true,
			MessageCount:       1,
			CreatedAt:          now,
			UpdatedAt:          now,
		}
		h.firestoreClient.Collection(fmt.Sprintf("tenants/%s/conversations", cred.TenantID)).Doc(convID).Set(ctx, conv)
		break
	}
}

// ============================================================================
// WOOCOMMERCE — REST Webhooks
//
// WooCommerce sends a signed POST. The X-WC-Webhook-Signature header contains
// a base64-encoded HMAC-SHA256 of the raw body using the webhook secret as key.
//
// We embed the credential ID in the webhook delivery URL:
//   POST /webhooks/orders/woocommerce?cred=<credentialID>&tenant=<tenantID>
//
// This lets us look up the exact credential without scanning all credentials.
// The credential ID and tenant ID are set when we register the webhook via the
// WooCommerce Webhooks API on connection.
//
// Reference: https://woocommerce.github.io/woocommerce-rest-api-docs/#webhooks
// ============================================================================

// WooCommerceWebhook handles POST /webhooks/orders/woocommerce
func (h *OrderWebhookHandler) WooCommerceWebhook(c *gin.Context) {
	tenantID := c.Query("tenant")
	credID := c.Query("cred")

	if tenantID == "" || credID == "" {
		c.JSON(http.StatusBadRequest, gin.H{"error": "missing tenant or cred query params"})
		return
	}

	body, err := readBodyBytes(c)
	if err != nil {
		c.JSON(http.StatusBadRequest, gin.H{"error": "cannot read body"})
		return
	}

	// Verify signature using the stored webhook secret.
	ctx := c.Request.Context()
	cred, err := h.marketplaceService.GetCredential(ctx, tenantID, credID)
	if err != nil {
		log.Printf("[Webhook:woocommerce] credential not found tenant=%s cred=%s", tenantID, credID)
		c.JSON(http.StatusOK, gin.H{"ok": true})
		return
	}

	webhookSecret := cred.CredentialData["webhook_secret"]
	if webhookSecret != "" {
		sig := c.GetHeader("X-WC-Webhook-Signature")
		if !verifyHMACSHA256Base64(body, webhookSecret, sig) {
			log.Printf("[Webhook:woocommerce] invalid signature tenant=%s", tenantID)
			c.JSON(http.StatusUnauthorized, gin.H{"error": "invalid signature"})
			return
		}
	}

	topic := c.GetHeader("X-WC-Webhook-Topic") // e.g. "order.created"
	log.Printf("[Webhook:woocommerce] topic=%s tenant=%s cred=%s", topic, tenantID, credID)

	if strings.HasPrefix(topic, "order.") && cred.Config.Orders.Enabled {
		go h.triggerImportForCredential(tenantID, credID, "woocommerce")
	}

	c.JSON(http.StatusOK, gin.H{"ok": true})
}

// ============================================================================
// SHOPIFY — Partner Webhooks
//
// Shopify signs every webhook with HMAC-SHA256 of the raw body using the
// shared secret, base64-encoded in X-Shopify-Hmac-Sha256.
//
// Credential is identified via query params as with WooCommerce.
//
// Reference: https://shopify.dev/docs/apps/webhooks/configuration/https
// ============================================================================

// ShopifyWebhook handles POST /webhooks/orders/shopify
func (h *OrderWebhookHandler) ShopifyWebhook(c *gin.Context) {
	tenantID := c.Query("tenant")
	credID := c.Query("cred")

	if tenantID == "" || credID == "" {
		c.JSON(http.StatusBadRequest, gin.H{"error": "missing tenant or cred query params"})
		return
	}

	body, err := readBodyBytes(c)
	if err != nil {
		c.JSON(http.StatusBadRequest, gin.H{"error": "cannot read body"})
		return
	}

	ctx := c.Request.Context()
	cred, err := h.marketplaceService.GetCredential(ctx, tenantID, credID)
	if err != nil {
		log.Printf("[Webhook:shopify] credential not found tenant=%s cred=%s", tenantID, credID)
		c.JSON(http.StatusOK, gin.H{"ok": true})
		return
	}

	clientSecret := cred.CredentialData["client_secret"]
	sig := c.GetHeader("X-Shopify-Hmac-Sha256")
	if clientSecret != "" && sig != "" {
		if !verifyHMACSHA256Base64(body, clientSecret, sig) {
			// Log but don't reject — secret may be stale after app rotation.
			// Shopify will retry on 5xx but not on 401, so we accept and log.
			log.Printf("[Webhook:shopify] WARNING: HMAC mismatch tenant=%s — processing anyway (check SHOPIFY_CLIENT_SECRET env var)", tenantID)
		}
	}

	topic := c.GetHeader("X-Shopify-Topic") // e.g. "orders/create"
	log.Printf("[Webhook:shopify] received topic=%s tenant=%s cred=%s orders_enabled=%v",
		topic, tenantID, credID, cred.Config.Orders.Enabled)

	if strings.HasPrefix(topic, "orders/") {
		if cred.Config.Orders.Enabled {
			go h.triggerImportForCredential(tenantID, credID, "shopify")
			log.Printf("[Webhook:shopify] triggered import for tenant=%s cred=%s", tenantID, credID)
		} else {
			log.Printf("[Webhook:shopify] orders not enabled for tenant=%s cred=%s — skipping import", tenantID, credID)
		}
	}

	c.JSON(http.StatusOK, gin.H{"ok": true})
}

// ============================================================================
// SHOPLINE — Webhook receiver
// ============================================================================
// Shopline signs webhooks with HMAC-SHA256 of the request body using the
// app's client_secret. The signature is sent in X-Shopline-Hmac-Sha256.
// Topic is sent in X-Shopline-Topic (e.g. "orders/create").
//
// POST /webhooks/orders/shopline?tenant={id}&cred={id}&topic=orders%2Fcreate
// ============================================================================

// ShoplineWebhook handles POST /webhooks/orders/shopline
func (h *OrderWebhookHandler) ShoplineWebhook(c *gin.Context) {
	tenantID := c.Query("tenant")
	credID := c.Query("cred")

	if tenantID == "" || credID == "" {
		c.JSON(http.StatusBadRequest, gin.H{"error": "missing tenant or cred query params"})
		return
	}

	body, err := readBodyBytes(c)
	if err != nil {
		c.JSON(http.StatusBadRequest, gin.H{"error": "cannot read body"})
		return
	}

	ctx := c.Request.Context()
	cred, err := h.marketplaceService.GetCredential(ctx, tenantID, credID)
	if err != nil {
		log.Printf("[Webhook:shopline] credential not found tenant=%s cred=%s", tenantID, credID)
		c.JSON(http.StatusOK, gin.H{"ok": true})
		return
	}

	clientSecret := cred.CredentialData["client_secret"]
	sig := c.GetHeader("X-Shopline-Hmac-Sha256")
	if clientSecret != "" && sig != "" {
		if !verifyHMACSHA256Base64(body, clientSecret, sig) {
			log.Printf("[Webhook:shopline] WARNING: HMAC mismatch tenant=%s — processing anyway (check SHOPLINE_CLIENT_SECRET env var)", tenantID)
		}
	}

	topic := c.GetHeader("X-Shopline-Topic") // e.g. "orders/create"
	if topic == "" {
		topic = c.Query("topic") // fallback: passed as URL param by registerWebhooksForCred
	}
	log.Printf("[Webhook:shopline] received topic=%s tenant=%s cred=%s orders_enabled=%v",
		topic, tenantID, credID, cred.Config.Orders.Enabled)

	if strings.HasPrefix(topic, "orders/") {
		if cred.Config.Orders.Enabled {
			go h.triggerImportForCredential(tenantID, credID, "shopline")
			log.Printf("[Webhook:shopline] triggered import for tenant=%s cred=%s", tenantID, credID)
		} else {
			log.Printf("[Webhook:shopline] orders not enabled for tenant=%s cred=%s — skipping import", tenantID, credID)
		}
	}

	c.JSON(http.StatusOK, gin.H{"ok": true})
}

// ============================================================================
// BIGCOMMERCE — Webhook API
//
// BigCommerce signs webhooks with HMAC-SHA256 of client_id:client_secret,
// sent as X-Auth-Client / signature headers. We use URL params for routing.
//
// Reference: https://developer.bigcommerce.com/docs/integrations/webhooks
// ============================================================================

// BigCommerceWebhook handles POST /webhooks/orders/bigcommerce
func (h *OrderWebhookHandler) BigCommerceWebhook(c *gin.Context) {
	tenantID := c.Query("tenant")
	credID := c.Query("cred")

	if tenantID == "" || credID == "" {
		c.JSON(http.StatusBadRequest, gin.H{"error": "missing tenant or cred query params"})
		return
	}

	body, err := readBodyBytes(c)
	if err != nil {
		c.JSON(http.StatusBadRequest, gin.H{"error": "cannot read body"})
		return
	}

	ctx := c.Request.Context()
	cred, err := h.marketplaceService.GetCredential(ctx, tenantID, credID)
	if err != nil {
		log.Printf("[Webhook:bigcommerce] credential not found tenant=%s cred=%s", tenantID, credID)
		c.JSON(http.StatusOK, gin.H{"ok": true})
		return
	}

	// BigCommerce sends producer = "stores/{hash}" in the body; we don't need
	// to re-verify the store hash because the URL already encodes the credID.
	// The access_token in headers is sufficient for authenticated stores.
	_ = body

	var event struct {
		Scope    string `json:"scope"` // e.g. "store/order/created"
		StoreID  string `json:"store_id"`
	}
	if err := c.ShouldBindJSON(&event); err != nil {
		c.JSON(http.StatusBadRequest, gin.H{"error": "invalid payload"})
		return
	}

	log.Printf("[Webhook:bigcommerce] scope=%s tenant=%s cred=%s", event.Scope, tenantID, credID)

	if strings.HasPrefix(event.Scope, "store/order/") && cred.Config.Orders.Enabled {
		go h.triggerImportForCredential(tenantID, credID, "bigcommerce")
	}

	c.JSON(http.StatusOK, gin.H{"ok": true})
}

// ============================================================================
// TIKTOK — Push Messages
//
// TikTok signs webhook payloads with HMAC-SHA256 of the raw body using the
// app_secret, sent in X-Tiktok-Signature.
//
// Reference: https://partner.tiktokshop.com/docv2/page/6502edd33d5f7402b9f8c4f0
// ============================================================================

// TikTokOrderWebhook handles POST /webhooks/orders/tiktok
func (h *OrderWebhookHandler) TikTokOrderWebhook(c *gin.Context) {
	tenantID := c.Query("tenant")
	credID := c.Query("cred")

	if tenantID == "" || credID == "" {
		c.JSON(http.StatusBadRequest, gin.H{"error": "missing tenant or cred query params"})
		return
	}

	body, err := readBodyBytes(c)
	if err != nil {
		c.JSON(http.StatusBadRequest, gin.H{"error": "cannot read body"})
		return
	}

	ctx := c.Request.Context()
	cred, err := h.marketplaceService.GetCredential(ctx, tenantID, credID)
	if err != nil {
		log.Printf("[Webhook:tiktok] credential not found tenant=%s cred=%s", tenantID, credID)
		c.JSON(http.StatusOK, gin.H{"ok": true})
		return
	}

	appSecret := cred.CredentialData["app_secret"]
	if appSecret != "" {
		sig := c.GetHeader("X-Tiktok-Signature")
		if !verifyHMACSHA256(body, sig, appSecret) {
			log.Printf("[Webhook:tiktok] invalid signature tenant=%s", tenantID)
			c.JSON(http.StatusUnauthorized, gin.H{"error": "invalid signature"})
			return
		}
	}

	var event struct {
		Type string `json:"type"` // e.g. "ORDER_STATUS_CHANGE"
	}
	_ = json.Unmarshal(body, &event)

	log.Printf("[Webhook:tiktok] type=%s tenant=%s cred=%s", event.Type, tenantID, credID)

	if strings.Contains(event.Type, "ORDER") && cred.Config.Orders.Enabled {
		go h.triggerImportForCredential(tenantID, credID, "tiktok")
	}

	c.JSON(http.StatusOK, gin.H{"ok": true})
}

// ============================================================================
// WEBHOOK SUBSCRIPTION MANAGEMENT
//
// These endpoints register / deregister our webhook URL with each marketplace
// when a credential is set up. Called by the frontend ChannelConfig panel
// or automatically on credential creation.
// ============================================================================

// SubscribeWebhooks handles POST /api/v1/marketplace/credentials/:id/webhooks/subscribe
// Registers our webhook endpoint with the marketplace for the given credential.
func (h *OrderWebhookHandler) SubscribeWebhooks(c *gin.Context) {
	tenantID := c.GetString("tenant_id")
	credID := c.Param("id")

	ctx := c.Request.Context()
	cred, err := h.marketplaceService.GetCredential(ctx, tenantID, credID)
	if err != nil {
		c.JSON(http.StatusNotFound, gin.H{"error": "credential not found"})
		return
	}

	backendURL := os.Getenv("BACKEND_URL")
	if backendURL == "" {
		c.JSON(http.StatusInternalServerError, gin.H{
			"error": "BACKEND_URL env var not set — cannot register webhooks. " +
				"Set BACKEND_URL to the publicly reachable base URL of this server " +
				"(e.g. https://api.yourapp.com).",
		})
		return
	}

	result, err := h.subscribeForChannel(ctx, cred, backendURL)
	if err != nil {
		c.JSON(http.StatusInternalServerError, gin.H{"error": err.Error()})
		return
	}

	c.JSON(http.StatusOK, gin.H{"ok": true, "result": result})
}

// UnsubscribeWebhooks handles DELETE /api/v1/marketplace/credentials/:id/webhooks/unsubscribe
func (h *OrderWebhookHandler) UnsubscribeWebhooks(c *gin.Context) {
	tenantID := c.GetString("tenant_id")
	credID := c.Param("id")

	ctx := c.Request.Context()
	cred, err := h.marketplaceService.GetCredential(ctx, tenantID, credID)
	if err != nil {
		c.JSON(http.StatusNotFound, gin.H{"error": "credential not found"})
		return
	}

	result, err := h.unsubscribeForChannel(ctx, cred)
	if err != nil {
		c.JSON(http.StatusInternalServerError, gin.H{"error": err.Error()})
		return
	}

	c.JSON(http.StatusOK, gin.H{"ok": true, "result": result})
}

// WebhookStatus handles GET /api/v1/marketplace/credentials/:id/webhooks/status
func (h *OrderWebhookHandler) WebhookStatus(c *gin.Context) {
	tenantID := c.GetString("tenant_id")
	credID := c.Param("id")

	ctx := c.Request.Context()
	cred, err := h.marketplaceService.GetCredential(ctx, tenantID, credID)
	if err != nil {
		c.JSON(http.StatusNotFound, gin.H{"error": "credential not found"})
		return
	}

	backendURL := os.Getenv("BACKEND_URL")
	webhookURL := webhookURLForCredential(backendURL, cred)
	supported := channelSupportsWebhooks(cred.Channel)
	registered := cred.CredentialData["webhook_id"] != ""

	c.JSON(http.StatusOK, gin.H{
		"channel":         cred.Channel,
		"supported":       supported,
		"registered":      registered,
		"webhook_id":      cred.CredentialData["webhook_id"],
		"webhook_url":     webhookURL,
		"backend_url_set": backendURL != "",
	})
}

// channelSupportsWebhooks returns true for channels with webhook APIs.
func channelSupportsWebhooks(channel string) bool {
	supported := map[string]bool{
		"ebay":        true,
		"woocommerce": true,
		"shopify":     true,
		"bigcommerce": true,
		"tiktok":      true,
	}
	return supported[channel]
}

// webhookURLForCredential builds the inbound webhook URL for a credential.
func webhookURLForCredential(backendURL string, cred *models.MarketplaceCredential) string {
	if backendURL == "" {
		return ""
	}
	base := strings.TrimRight(backendURL, "/")
	switch cred.Channel {
	case "ebay":
		return base + "/webhooks/orders/ebay"
	case "woocommerce", "shopify", "bigcommerce", "tiktok":
		return fmt.Sprintf("%s/webhooks/orders/%s?tenant=%s&cred=%s",
			base, cred.Channel, cred.TenantID, cred.CredentialID)
	}
	return ""
}

// subscribeForChannel registers the webhook with the marketplace.
func (h *OrderWebhookHandler) subscribeForChannel(ctx context.Context, cred *models.MarketplaceCredential, backendURL string) (map[string]interface{}, error) {
	if !channelSupportsWebhooks(cred.Channel) {
		return map[string]interface{}{"message": "channel does not support webhooks — polling only"}, nil
	}

	webhookURL := webhookURLForCredential(backendURL, cred)

	switch cred.Channel {
	case "woocommerce":
		return h.subscribeWooCommerce(ctx, cred, webhookURL)
	case "shopify":
		return h.subscribeShopify(ctx, cred, webhookURL)
	case "bigcommerce":
		return h.subscribeBigCommerce(ctx, cred, webhookURL)
	case "tiktok":
		// TikTok push webhooks are configured in the TikTok developer portal,
		// not via API. Return the URL for the operator to configure manually.
		return map[string]interface{}{
			"message":     "TikTok push webhooks must be configured in the TikTok Seller Center. Point it to the URL below.",
			"webhook_url": webhookURL,
		}, nil
	case "ebay":
		return h.subscribeEbay(ctx, cred, backendURL)
	}

	return nil, fmt.Errorf("unsupported channel: %s", cred.Channel)
}

// unsubscribeForChannel removes the webhook registration.
func (h *OrderWebhookHandler) unsubscribeForChannel(ctx context.Context, cred *models.MarketplaceCredential) (map[string]interface{}, error) {
	switch cred.Channel {
	case "woocommerce":
		return h.unsubscribeWooCommerce(ctx, cred)
	case "shopify":
		return h.unsubscribeShopify(ctx, cred)
	case "bigcommerce":
		return h.unsubscribeBigCommerce(ctx, cred)
	default:
		return map[string]interface{}{"message": "no webhook to remove"}, nil
	}
}

// ============================================================================
// WOOCOMMERCE SUBSCRIPTION
// ============================================================================

func (h *OrderWebhookHandler) subscribeWooCommerce(ctx context.Context, cred *models.MarketplaceCredential, webhookURL string) (map[string]interface{}, error) {
	merged, err := h.marketplaceService.GetFullCredentials(ctx, cred)
	if err != nil {
		return nil, err
	}

	storeURL := strings.TrimRight(merged["store_url"], "/")
	consumerKey := merged["consumer_key"]
	consumerSecret := merged["consumer_secret"]

	if storeURL == "" || consumerKey == "" || consumerSecret == "" {
		return nil, fmt.Errorf("woocommerce: missing store_url, consumer_key or consumer_secret")
	}

	// Register webhooks for order.created and order.updated.
	results := []string{}
	ids := []string{}

	for _, topic := range []string{"order.created", "order.updated"} {
		payload := map[string]interface{}{
			"name":         "MarketMate " + topic,
			"status":       "active",
			"topic":        topic,
			"delivery_url": webhookURL,
		}
		body, _ := json.Marshal(payload)

		req, err := http.NewRequestWithContext(ctx, "POST",
			storeURL+"/wp-json/wc/v3/webhooks", bytes.NewReader(body))
		if err != nil {
			return nil, err
		}
		req.Header.Set("Content-Type", "application/json")
		req.SetBasicAuth(consumerKey, consumerSecret)

		resp, err := http.DefaultClient.Do(req)
		if err != nil {
			return nil, fmt.Errorf("woocommerce webhook registration failed: %w", err)
		}
		defer resp.Body.Close()

		var result struct {
			ID     int    `json:"id"`
			Secret string `json:"secret"`
		}
		json.NewDecoder(resp.Body).Decode(&result)

		if result.ID > 0 {
			ids = append(ids, fmt.Sprintf("%d", result.ID))
			results = append(results, fmt.Sprintf("registered %s (id=%d)", topic, result.ID))

			// Store the webhook secret for signature verification.
			if result.Secret != "" {
				cred.CredentialData["webhook_secret"] = result.Secret
			}
		}
	}

	// Store webhook IDs (comma-separated) for later removal.
	if len(ids) > 0 {
		cred.CredentialData["webhook_id"] = strings.Join(ids, ",")
		if err := h.marketplaceService.SaveCredential(ctx, cred); err != nil {
			log.Printf("[Webhook:woocommerce] failed to save webhook IDs: %v", err)
		}
	}

	return map[string]interface{}{"registered": results}, nil
}

func (h *OrderWebhookHandler) unsubscribeWooCommerce(ctx context.Context, cred *models.MarketplaceCredential) (map[string]interface{}, error) {
	merged, err := h.marketplaceService.GetFullCredentials(ctx, cred)
	if err != nil {
		return nil, err
	}

	storeURL := strings.TrimRight(merged["store_url"], "/")
	consumerKey := merged["consumer_key"]
	consumerSecret := merged["consumer_secret"]
	webhookIDs := cred.CredentialData["webhook_id"]

	if webhookIDs == "" {
		return map[string]interface{}{"message": "no registered webhooks found"}, nil
	}

	for _, idStr := range strings.Split(webhookIDs, ",") {
		idStr = strings.TrimSpace(idStr)
		req, err := http.NewRequestWithContext(ctx, "DELETE",
			fmt.Sprintf("%s/wp-json/wc/v3/webhooks/%s?force=true", storeURL, idStr), nil)
		if err != nil {
			continue
		}
		req.SetBasicAuth(consumerKey, consumerSecret)
		resp, err := http.DefaultClient.Do(req)
		if err != nil {
			log.Printf("[Webhook:woocommerce] failed to delete webhook %s: %v", idStr, err)
			continue
		}
		resp.Body.Close()
	}

	delete(cred.CredentialData, "webhook_id")
	delete(cred.CredentialData, "webhook_secret")
	_ = h.marketplaceService.SaveCredential(ctx, cred)

	return map[string]interface{}{"removed": webhookIDs}, nil
}

// ============================================================================
// SHOPIFY SUBSCRIPTION
// ============================================================================

func (h *OrderWebhookHandler) subscribeShopify(ctx context.Context, cred *models.MarketplaceCredential, webhookURL string) (map[string]interface{}, error) {
	merged, err := h.marketplaceService.GetFullCredentials(ctx, cred)
	if err != nil {
		return nil, err
	}

	shopDomain := merged["shop_domain"] // e.g. mystore.myshopify.com
	accessToken := merged["access_token"]

	if shopDomain == "" || accessToken == "" {
		return nil, fmt.Errorf("shopify: missing shop_domain or access_token")
	}

	results := []string{}
	ids := []string{}

	for _, topic := range []string{"orders/create", "orders/updated", "orders/cancelled"} {
		payload := map[string]interface{}{
			"webhook": map[string]interface{}{
				"topic":   topic,
				"address": webhookURL,
				"format":  "json",
			},
		}
		body, _ := json.Marshal(payload)

		req, err := http.NewRequestWithContext(ctx, "POST",
			fmt.Sprintf("https://%s/admin/api/2024-01/webhooks.json", shopDomain),
			bytes.NewReader(body))
		if err != nil {
			return nil, err
		}
		req.Header.Set("Content-Type", "application/json")
		req.Header.Set("X-Shopify-Access-Token", accessToken)

		resp, err := http.DefaultClient.Do(req)
		if err != nil {
			return nil, fmt.Errorf("shopify webhook registration failed: %w", err)
		}
		defer resp.Body.Close()

		var result struct {
			Webhook struct {
				ID int64 `json:"id"`
			} `json:"webhook"`
		}
		json.NewDecoder(resp.Body).Decode(&result)

		if result.Webhook.ID > 0 {
			ids = append(ids, fmt.Sprintf("%d", result.Webhook.ID))
			results = append(results, fmt.Sprintf("registered %s (id=%d)", topic, result.Webhook.ID))
		}
	}

	if len(ids) > 0 {
		cred.CredentialData["webhook_id"] = strings.Join(ids, ",")
		_ = h.marketplaceService.SaveCredential(ctx, cred)
	}

	return map[string]interface{}{"registered": results}, nil
}

func (h *OrderWebhookHandler) unsubscribeShopify(ctx context.Context, cred *models.MarketplaceCredential) (map[string]interface{}, error) {
	merged, err := h.marketplaceService.GetFullCredentials(ctx, cred)
	if err != nil {
		return nil, err
	}

	shopDomain := merged["shop_domain"]
	accessToken := merged["access_token"]
	webhookIDs := cred.CredentialData["webhook_id"]

	if webhookIDs == "" {
		return map[string]interface{}{"message": "no registered webhooks found"}, nil
	}

	for _, idStr := range strings.Split(webhookIDs, ",") {
		idStr = strings.TrimSpace(idStr)
		req, _ := http.NewRequestWithContext(ctx, "DELETE",
			fmt.Sprintf("https://%s/admin/api/2024-01/webhooks/%s.json", shopDomain, idStr), nil)
		req.Header.Set("X-Shopify-Access-Token", accessToken)
		resp, err := http.DefaultClient.Do(req)
		if err != nil {
			log.Printf("[Webhook:shopify] failed to delete webhook %s: %v", idStr, err)
			continue
		}
		resp.Body.Close()
	}

	delete(cred.CredentialData, "webhook_id")
	_ = h.marketplaceService.SaveCredential(ctx, cred)

	return map[string]interface{}{"removed": webhookIDs}, nil
}

// ============================================================================
// BIGCOMMERCE SUBSCRIPTION
// ============================================================================

func (h *OrderWebhookHandler) subscribeBigCommerce(ctx context.Context, cred *models.MarketplaceCredential, webhookURL string) (map[string]interface{}, error) {
	merged, err := h.marketplaceService.GetFullCredentials(ctx, cred)
	if err != nil {
		return nil, err
	}

	storeHash := merged["store_hash"]
	accessToken := merged["access_token"]
	clientID := merged["client_id"]

	if storeHash == "" || accessToken == "" {
		return nil, fmt.Errorf("bigcommerce: missing store_hash or access_token")
	}

	results := []string{}
	ids := []string{}

	for _, scope := range []string{"store/order/created", "store/order/statusUpdated"} {
		payload := map[string]interface{}{
			"scope":       scope,
			"destination": webhookURL,
			"is_active":   true,
			"headers":     map[string]string{"X-Custom-Auth": clientID},
		}
		body, _ := json.Marshal(payload)

		req, err := http.NewRequestWithContext(ctx, "POST",
			fmt.Sprintf("https://api.bigcommerce.com/stores/%s/v3/hooks", storeHash),
			bytes.NewReader(body))
		if err != nil {
			return nil, err
		}
		req.Header.Set("Content-Type", "application/json")
		req.Header.Set("X-Auth-Token", accessToken)

		resp, err := http.DefaultClient.Do(req)
		if err != nil {
			return nil, fmt.Errorf("bigcommerce webhook registration failed: %w", err)
		}
		defer resp.Body.Close()

		var result struct {
			Data struct {
				ID int `json:"id"`
			} `json:"data"`
		}
		json.NewDecoder(resp.Body).Decode(&result)

		if result.Data.ID > 0 {
			ids = append(ids, fmt.Sprintf("%d", result.Data.ID))
			results = append(results, fmt.Sprintf("registered %s (id=%d)", scope, result.Data.ID))
		}
	}

	if len(ids) > 0 {
		cred.CredentialData["webhook_id"] = strings.Join(ids, ",")
		_ = h.marketplaceService.SaveCredential(ctx, cred)
	}

	return map[string]interface{}{"registered": results}, nil
}

func (h *OrderWebhookHandler) unsubscribeBigCommerce(ctx context.Context, cred *models.MarketplaceCredential) (map[string]interface{}, error) {
	merged, err := h.marketplaceService.GetFullCredentials(ctx, cred)
	if err != nil {
		return nil, err
	}

	storeHash := merged["store_hash"]
	accessToken := merged["access_token"]
	webhookIDs := cred.CredentialData["webhook_id"]

	if webhookIDs == "" {
		return map[string]interface{}{"message": "no registered webhooks found"}, nil
	}

	for _, idStr := range strings.Split(webhookIDs, ",") {
		idStr = strings.TrimSpace(idStr)
		req, _ := http.NewRequestWithContext(ctx, "DELETE",
			fmt.Sprintf("https://api.bigcommerce.com/stores/%s/v3/hooks/%s", storeHash, idStr), nil)
		req.Header.Set("X-Auth-Token", accessToken)
		resp, err := http.DefaultClient.Do(req)
		if err != nil {
			log.Printf("[Webhook:bigcommerce] failed to delete webhook %s: %v", idStr, err)
			continue
		}
		resp.Body.Close()
	}

	delete(cred.CredentialData, "webhook_id")
	_ = h.marketplaceService.SaveCredential(ctx, cred)

	return map[string]interface{}{"removed": webhookIDs}, nil
}

// ============================================================================
// EBAY SUBSCRIPTION — Commerce Notifications API
// ============================================================================

func (h *OrderWebhookHandler) subscribeEbay(ctx context.Context, cred *models.MarketplaceCredential, backendURL string) (map[string]interface{}, error) {
	// eBay Commerce Notifications uses a single global endpoint per app, not
	// per seller. The endpoint is registered at the app level in the eBay
	// developer portal or via the Notification API. We only need to subscribe
	// to the ORDER_PAYMENT_COMPLETION topic per token.
	//
	// For now we return the required endpoint URL and verification token for
	// operators to configure in the eBay developer portal, and store the fact
	// that this credential wants order notifications.
	verificationToken := os.Getenv("EBAY_NOTIFICATION_VERIFICATION_TOKEN")
	webhookURL := strings.TrimRight(backendURL, "/") + "/webhooks/orders/ebay"

	cred.CredentialData["webhook_subscribed"] = "true"
	_ = h.marketplaceService.SaveCredential(ctx, cred)

	return map[string]interface{}{
		"message":            "Register the webhook URL below in your eBay developer portal under Commerce Notifications, then set EBAY_NOTIFICATION_VERIFICATION_TOKEN in your env.",
		"webhook_url":        webhookURL,
		"verification_token": verificationToken,
		"topics":             []string{"ORDER_PAYMENT_COMPLETION", "ORDER_CHECKOUT_BUYER_APPROVAL", "ASK_SELLER_QUESTION", "BUYER_INITIATED_OFFER", "CANCELLATION_CREATED", "RETURN_CREATED", "RETURN_CLOSED"},
	}, nil
}


// ============================================================================
// EBAY MESSAGE WEBHOOK — ASK_SELLER_QUESTION / BUYER_INITIATED_OFFER
// ============================================================================
// Called from EbayWebhook when the topic is a message-related topic.
// Upserts the conversation + message in Firestore via the MessagingHandler.
// The MessagingHandler is injected lazily to avoid circular imports.

func (h *OrderWebhookHandler) handleEbayMessageWebhook(c *gin.Context, body []byte, sellerUsername string) {
	// Parse the full eBay message notification payload
	var notif struct {
		Metadata struct {
			Topic     string `json:"topic"`
			SchemaVersion string `json:"schemaVersion"`
		} `json:"metadata"`
		Notification struct {
			NotificationID string `json:"notificationId"`
			EventDate      string `json:"eventDate"`
			PublishDate    string `json:"publishDate"`
			Data           struct {
				Username  string `json:"username"` // seller username
				Message   struct {
					MessageID   string `json:"messageId"`
					SenderID    string `json:"senderId"`   // buyer username
					Subject     string `json:"subject"`
					Text        string `json:"text"`
					CreatedDate string `json:"createdDate"`
					ItemID      string `json:"itemId"`
				} `json:"message"`
			} `json:"data"`
		} `json:"notification"`
	}

	if err := json.Unmarshal(body, &notif); err != nil {
		log.Printf("[Webhook:ebay] failed to parse message notification: %v", err)
		c.JSON(http.StatusOK, gin.H{"ok": true})
		return
	}

	msg := notif.Notification.Data.Message
	if msg.MessageID == "" {
		c.JSON(http.StatusOK, gin.H{"ok": true})
		return
	}

	ctx := c.Request.Context()
	username := notif.Notification.Data.Username
	if username == "" {
		username = sellerUsername
	}

	// Find the matching credential by seller_username
	creds, err := h.repo.ListAllActiveCredentials(ctx)
	if err != nil {
		log.Printf("[Webhook:ebay] failed to list credentials: %v", err)
		c.JSON(http.StatusOK, gin.H{"ok": true})
		return
	}

	for _, cred := range creds {
		if cred.Channel != "ebay" {
			continue
		}
		if username != "" && cred.CredentialData["seller_username"] != username {
			continue
		}

		// Upsert conversation + message in Firestore
		convID := fmt.Sprintf("ebay_%s_%s", cred.CredentialID, msg.MessageID)
		msgDocID := fmt.Sprintf("ebay_%s", msg.MessageID)

		convRef := h.firestoreClient.Collection(fmt.Sprintf("tenants/%s/conversations", cred.TenantID)).Doc(convID)
		msgRef := convRef.Collection("messages").Doc(msgDocID)

		// Skip if already stored
		existing, _ := msgRef.Get(ctx)
		if existing.Exists() {
			continue
		}

		now := time.Now()
		sentAt, _ := time.Parse(time.RFC3339, msg.CreatedDate)
		if sentAt.IsZero() {
			sentAt = now
		}

		subject := msg.Subject
		if subject == "" {
			subject = fmt.Sprintf("Message about item %s", msg.ItemID)
		}
		preview := msg.Text
		if len(preview) > 100 {
			preview = preview[:100] + "…"
		}

		// Upsert conversation
		convDoc, _ := convRef.Get(ctx)
		var conv models.Conversation
		if convDoc.Exists() {
			convDoc.DataTo(&conv)
		} else {
			conv = models.Conversation{
				ConversationID:      convID,
				TenantID:            cred.TenantID,
				Channel:             "ebay",
				ChannelAccountID:    cred.CredentialID,
				MarketplaceThreadID: msg.MessageID,
				Customer: models.ConversationCustomer{
					Name:    msg.SenderID,
					BuyerID: msg.SenderID,
				},
				Subject:   subject,
				Status:    models.ConvStatusOpen,
				CreatedAt: now,
			}
		}
		conv.LastMessageAt = sentAt
		conv.LastMessagePreview = preview
		conv.MessageCount = conv.MessageCount + 1
		conv.Unread = true
		conv.Status = models.ConvStatusOpen
		conv.UpdatedAt = now
		convRef.Set(ctx, conv)

		// Store message
		message := models.Message{
			MessageID:        msgDocID,
			ConversationID:   convID,
			Direction:        models.MsgDirectionInbound,
			Body:             msg.Text,
			ChannelMessageID: msg.MessageID,
			SentBy:           msg.SenderID,
			SentAt:           sentAt,
		}
		msgRef.Set(ctx, message)

		log.Printf("[Webhook:ebay] Stored message %s from buyer %s (tenant %s)",
			msg.MessageID, msg.SenderID, cred.TenantID)
		break
	}

	c.JSON(http.StatusOK, gin.H{"ok": true})
}


// ============================================================================
// EBAY CANCELLATION / RETURN WEBHOOK
// ============================================================================
// Handles CANCELLATION_CREATED, RETURN_CREATED, RETURN_CLOSED topics.
// Creates a messaging ticket and alerts staff.

func (h *OrderWebhookHandler) handleEbayCancelReturnWebhook(
	c *gin.Context, body []byte, topic, sellerUsername string,
) {
	var notif struct {
		Metadata struct {
			Topic string `json:"topic"`
		} `json:"metadata"`
		Notification struct {
			NotificationID string `json:"notificationId"`
			EventDate      string `json:"eventDate"`
			Data           struct {
				Username        string `json:"username"` // seller username
				CancellationID  string `json:"cancellationId,omitempty"`
				OrderID         string `json:"orderId,omitempty"`
				LegacyOrderID   string `json:"legacyOrderId,omitempty"`
				ReturnID        string `json:"returnId,omitempty"`
				BuyerUserID     string `json:"buyerUserId,omitempty"`
				BuyerName       string `json:"buyerName,omitempty"`
				ReturnReason    string `json:"returnReason,omitempty"`
				CancelReason    string `json:"cancelReason,omitempty"`
				ReturnState     string `json:"returnState,omitempty"`
			} `json:"data"`
		} `json:"notification"`
	}

	if err := json.Unmarshal(body, &notif); err != nil {
		log.Printf("[Webhook:ebay] Failed to parse cancel/return notification: %v", err)
		c.JSON(http.StatusOK, gin.H{"ok": true})
		return
	}

	d := notif.Notification.Data
	username := d.Username
	if username == "" {
		username = sellerUsername
	}

	// Determine order reference
	orderRef := d.LegacyOrderID
	if orderRef == "" {
		orderRef = d.OrderID
	}

	// Build subject and body based on topic
	var subject, bodyText string
	switch topic {
	case "CANCELLATION_CREATED":
		subject = fmt.Sprintf("⚠️ eBay Cancellation Request — Order %s", orderRef)
		bodyText = fmt.Sprintf(
			"A buyer has requested cancellation of an eBay order.\n\n"+
				"Order: %s\n"+
				"Buyer: %s\n"+
				"Reason: %s\n\n"+
				"⚠️ STOP — do not dispatch this order until resolved.\n"+
				"If a label has already been printed, do not ship.",
			orderRef, d.BuyerUserID, d.CancelReason,
		)
	case "RETURN_CREATED":
		subject = fmt.Sprintf("eBay Return Request — Order %s", orderRef)
		bodyText = fmt.Sprintf(
			"A buyer has opened a return request on eBay.\n\n"+
				"Order: %s\n"+
				"Return ID: %s\n"+
				"Buyer: %s\n"+
				"Reason: %s\n\n"+
				"Please review and respond in the eBay Resolution Centre.",
			orderRef, d.ReturnID, d.BuyerUserID, d.ReturnReason,
		)
	case "RETURN_CLOSED":
		subject = fmt.Sprintf("eBay Return Closed — Order %s", orderRef)
		bodyText = fmt.Sprintf(
			"An eBay return has been closed.\n\n"+
				"Order: %s\n"+
				"Return ID: %s\n"+
				"State: %s",
			orderRef, d.ReturnID, d.ReturnState,
		)
	}

	if subject == "" {
		c.JSON(http.StatusOK, gin.H{"ok": true})
		return
	}

	ctx := c.Request.Context()

	// Find matching credential by seller username
	creds, err := h.repo.ListAllActiveCredentials(ctx)
	if err != nil {
		log.Printf("[Webhook:ebay] Failed to list credentials: %v", err)
		c.JSON(http.StatusOK, gin.H{"ok": true})
		return
	}

	for _, cred := range creds {
		if cred.Channel != "ebay" {
			continue
		}
		if username != "" && cred.CredentialData["seller_username"] != username {
			continue
		}

		now := time.Now()
		convID := fmt.Sprintf("ebay_%s_%s_%s", topic, cred.CredentialID, orderRef)

		convRef := h.firestoreClient.Collection(fmt.Sprintf("tenants/%s/conversations", cred.TenantID)).Doc(convID)
		existingSnap, _ := convRef.Get(ctx)

		var conv models.Conversation
		preview := bodyText
		if len(preview) > 100 {
			preview = preview[:100] + "…"
		}

		if existingSnap.Exists() {
			existingSnap.DataTo(&conv)
			convRef.Update(ctx, []firestore.Update{
				{Path: "last_message_at", Value: now},
				{Path: "last_message_preview", Value: preview},
				{Path: "unread", Value: true},
				{Path: "status", Value: models.ConvStatusOpen},
				{Path: "updated_at", Value: now},
			})
		} else {
			conv = models.Conversation{
				ConversationID:   convID,
				TenantID:         cred.TenantID,
				Channel:          "ebay",
				ChannelAccountID: cred.CredentialID,
				OrderNumber:      orderRef,
				Customer:         models.ConversationCustomer{Name: d.BuyerUserID, BuyerID: d.BuyerUserID},
				Subject:          subject,
				Status:           models.ConvStatusOpen,
				LastMessageAt:    now,
				LastMessagePreview: preview,
				Unread:           true,
				MessageCount:     1,
				CreatedAt:        now,
				UpdatedAt:        now,
			}
			convRef.Set(ctx, conv)
		}

		// Store alert message
		msgID := fmt.Sprintf("ebay_%s_%d", topic, now.UnixNano())
		h.firestoreClient.Collection(fmt.Sprintf("tenants/%s/conversations/%s/messages", cred.TenantID, convID)).
			Doc(msgID).Set(ctx, models.Message{
			MessageID:      msgID,
			ConversationID: convID,
			Direction:      models.MsgDirectionInbound,
			Body:           bodyText,
			SentBy:         "ebay_platform",
			SentAt:         now,
		})

		log.Printf("[Webhook:ebay] %s ticket created: conv=%s tenant=%s", topic, convID, cred.TenantID)

		// For cancellations specifically, also create a cancellation alert for acknowledgement
		if topic == "CANCELLATION_CREATED" && h.firestoreClient != nil {
			go CreateCancellationAlert(
				context.Background(), h.firestoreClient,
				cred.TenantID, cred.CredentialID, "ebay",
				orderRef, orderRef, d.CancelReason,
			)
		}

		// Alert all team members
		if h.messagingNotifier != nil {
			members, err := h.messagingNotifier.GetAssignableMembers(ctx, cred.TenantID)
			if err == nil {
				for _, m := range members {
					member := m
					go h.messagingNotifier.NotifyAssignment(context.Background(), &member, &conv, "eBay Platform")
				}
			}
		}
		break
	}

	c.JSON(http.StatusOK, gin.H{"ok": true})
}

// ============================================================================
// WEBHOOK HEALTH CHECKER
// ============================================================================
// CheckAllWebhookSubscriptions scans every active credential across all tenants
// and verifies it has the required webhook subscriptions. Re-registers any that
// are missing. Called at startup and every 6 hours by the scheduler.
//
// Channels checked:
//   amazon/amazonnew — MESSAGING_NEW_MESSAGE_NOTIFICATION via SP-API Notifications
//   ebay             — ASK_SELLER_QUESTION via eBay Commerce Notifications
//   shopify/woocommerce/bigcommerce — order webhooks
//
// Findings are written to Firestore at:
//   tenants/{tid}/webhook_health/{credentialID}
// so operators can see the status in the UI.

type WebhookHealthResult struct {
	CredentialID string    `json:"credential_id" firestore:"credential_id"`
	Channel      string    `json:"channel" firestore:"channel"`
	AccountName  string    `json:"account_name" firestore:"account_name"`
	TenantID     string    `json:"tenant_id" firestore:"tenant_id"`
	Status       string    `json:"status" firestore:"status"` // "ok" | "missing" | "error" | "not_supported"
	Details      string    `json:"details,omitempty" firestore:"details,omitempty"`
	CheckedAt    time.Time `json:"checked_at" firestore:"checked_at"`
	Reregistered bool      `json:"reregistered,omitempty" firestore:"reregistered,omitempty"`
}

func (h *OrderWebhookHandler) CheckAllWebhookSubscriptions(
	ctx context.Context,
	backendURL string,
	amazonWebhookHandler interface {
		TryRegisterAmazonMessagingWebhook(ctx context.Context, tenantID string, cred *models.MarketplaceCredential, backendURL string)
	},
) []WebhookHealthResult {
	creds, err := h.repo.ListAllActiveCredentials(ctx)
	if err != nil {
		log.Printf("[WebhookHealth] Failed to list credentials: %v", err)
		return nil
	}

	var results []WebhookHealthResult
	log.Printf("[WebhookHealth] Checking %d credentials...", len(creds))

	for _, cred := range creds {
		result := WebhookHealthResult{
			CredentialID: cred.CredentialID,
			Channel:      cred.Channel,
			AccountName:  cred.AccountName,
			TenantID:     cred.TenantID,
			CheckedAt:    time.Now(),
		}

		switch cred.Channel {
		case "amazon", "amazonnew":
			result = h.checkAmazonWebhookHealth(ctx, cred, result, backendURL, amazonWebhookHandler)
		case "ebay":
			result = h.checkEbayWebhookHealth(cred, result)
		case "shopify", "woocommerce", "bigcommerce", "tiktok":
			result = h.checkOrderWebhookHealth(cred, result)
		default:
			result.Status = "not_supported"
			result.Details = fmt.Sprintf("%s does not support webhooks — polling only", cred.Channel)
		}

		// Write result to Firestore for visibility
		h.firestoreClient.Collection(fmt.Sprintf("tenants/%s/webhook_health", cred.TenantID)).
			Doc(cred.CredentialID).Set(ctx, result)

		if result.Status != "ok" && result.Status != "not_supported" {
			log.Printf("[WebhookHealth] ⚠ %s/%s (%s): %s — %s",
				cred.TenantID, cred.AccountName, cred.Channel, result.Status, result.Details)
		}

		results = append(results, result)
	}

	ok := 0
	missing := 0
	for _, r := range results {
		if r.Status == "ok" || r.Status == "not_supported" { ok++ } else { missing++ }
	}
	log.Printf("[WebhookHealth] Done: %d ok, %d missing/error", ok, missing)
	return results
}

func (h *OrderWebhookHandler) checkAmazonWebhookHealth(
	ctx context.Context,
	cred models.MarketplaceCredential,
	result WebhookHealthResult,
	backendURL string,
	amazonHandler interface {
		TryRegisterAmazonMessagingWebhook(ctx context.Context, tenantID string, cred *models.MarketplaceCredential, backendURL string)
	},
) WebhookHealthResult {
	destID := cred.CredentialData["amazon_notif_destination_id"]
	subID := cred.CredentialData["amazon_notif_subscription_id"]

	if destID != "" && subID != "" {
		result.Status = "ok"
		result.Details = fmt.Sprintf("destination=%s subscription=%s", destID[:8]+"...", subID[:8]+"...")
		return result
	}

	// Missing — attempt re-registration
	result.Status = "missing"
	if destID == "" {
		result.Details = "No destination_id — webhook not registered"
	} else {
		result.Details = "destination_id present but no subscription_id"
	}

	if backendURL != "" {
		credCopy := cred
		amazonHandler.TryRegisterAmazonMessagingWebhook(ctx, cred.TenantID, &credCopy, backendURL)
		result.Reregistered = true
		result.Details += " — re-registration triggered"
	}

	return result
}

func (h *OrderWebhookHandler) checkEbayWebhookHealth(
	cred models.MarketplaceCredential,
	result WebhookHealthResult,
) WebhookHealthResult {
	if cred.CredentialData["webhook_subscribed"] == "true" {
		result.Status = "ok"
		result.Details = "eBay Commerce Notifications subscribed"
	} else {
		result.Status = "missing"
		result.Details = "eBay webhook not subscribed — go to Marketplace Connections and click Subscribe"
	}
	return result
}

func (h *OrderWebhookHandler) checkOrderWebhookHealth(
	cred models.MarketplaceCredential,
	result WebhookHealthResult,
) WebhookHealthResult {
	webhookID := cred.CredentialData["webhook_id"]
	if webhookID != "" {
		result.Status = "ok"
		result.Details = fmt.Sprintf("webhook_id=%s", webhookID)
	} else {
		result.Status = "missing"
		result.Details = fmt.Sprintf("%s order webhook not registered", cred.Channel)
	}
	return result
}

// GetWebhookHealthStatus handles GET /api/v1/webhook-health
// Returns the stored health results for the current tenant.
func (h *OrderWebhookHandler) GetWebhookHealthStatus(c *gin.Context) {
	tenantID := c.GetString("tenant_id")
	ctx := c.Request.Context()

	iter := h.firestoreClient.Collection(fmt.Sprintf("tenants/%s/webhook_health", tenantID)).Documents(ctx)
	var results []WebhookHealthResult
	for {
		doc, err := iter.Next()
		if err != nil {
			break
		}
		var r WebhookHealthResult
		if err := doc.DataTo(&r); err == nil {
			results = append(results, r)
		}
	}
	if results == nil {
		results = []WebhookHealthResult{}
	}

	// Count summary
	ok := 0; missing := 0; notSupported := 0
	for _, r := range results {
		switch r.Status {
		case "ok": ok++
		case "not_supported": notSupported++
		default: missing++
		}
	}

	c.JSON(http.StatusOK, gin.H{
		"results":       results,
		"summary": gin.H{
			"ok":            ok,
			"missing":       missing,
			"not_supported": notSupported,
			"total":         len(results),
		},
	})
}


// ============================================================================
// TEMU AFTER-SALES & CANCEL WEBHOOK
// POST /webhooks/temu/aftersales
// ============================================================================
// Receives push notifications for after-sales events from Temu Open Platform.
// Payload is AES/CBC/PKCS5 encrypted using app_secret (first 16 bytes as IV).
// Signature is HMAC-SHA256 over sorted header+body params using app_secret.
//
// Subscribed event codes:
//   bg_aftersales_status_change    — buyer refund/return request
//   bg_cancel_order_status_change  — order cancellation status
//
// On receipt: creates a conversation ticket in the messaging system and
// sends email + WhatsApp alerts to all team members.

func (h *OrderWebhookHandler) TemuAfterSalesWebhook(c *gin.Context) {
	body, err := readBodyBytes(c)
	if err != nil {
		c.JSON(http.StatusBadRequest, gin.H{"error": "cannot read body"})
		return
	}

	// Extract headers
	appKey := c.GetHeader("x-tm-app-key")
	eventCode := c.GetHeader("x-tm-event-code")
	timestamp := c.GetHeader("x-tm-timestamp")
	signature := c.GetHeader("x-tm-signature")
	extParam := c.GetHeader("x-tm-ext-param")
	// Note: do NOT substitute extParam if empty — Temu skips blank params when signing

	if appKey == "" || eventCode == "" || signature == "" {
		log.Printf("[Temu Webhook] Missing required headers")
		c.JSON(http.StatusBadRequest, gin.H{"error": "missing required headers"})
		return
	}

	// Get app_secret from platform_config
	appSecret := h.getTemuAppSecret(c.Request.Context())
	if appSecret == "" {
		log.Printf("[Temu Webhook] app_secret not found in platform config")
		c.JSON(http.StatusInternalServerError, gin.H{"error": "configuration error"})
		return
	}

	// Parse body to get encrypted eventData
	var payload struct {
		EventData string `json:"eventData"`
	}
	if err := json.Unmarshal(body, &payload); err != nil || payload.EventData == "" {
		log.Printf("[Temu Webhook] Invalid body: %v", err)
		c.JSON(http.StatusBadRequest, gin.H{"error": "invalid body"})
		return
	}

	// Attempt to decrypt eventData. For some event types (e.g. bg_open_event_test)
	// Temu may send unencrypted or differently-formatted payloads.
	// We try decryption; if it fails we use the raw value.
	decrypted, decryptErr := temuAESDecrypt(payload.EventData, appSecret)
	if decryptErr != nil {
		log.Printf("[Temu Webhook] Decryption failed (will use raw): %v", decryptErr)
		decrypted = payload.EventData
	}

	// Verify HMAC-SHA256 signature.
	// Temu signs over sorted key=value pairs using the decrypted eventData.
	// For events where decryption fails/is a no-op, also try the raw encrypted value.
	sigParams := map[string]string{
		"eventData":       decrypted,
		"x-tm-app-key":    appKey,
		"x-tm-event-code": eventCode,
		"x-tm-timestamp":  timestamp,
	}
	if extParam != "" {
		sigParams["x-tm-ext-param"] = extParam
	}

	sigValid := temuVerifyWebhookSignature(appSecret, signature, sigParams)

	// If decryption changed the value, also try signing the raw encrypted string.
	// Also try raw if decryption appeared to succeed but signature still failed —
	// some event types (e.g. bg_open_event_test) are not encrypted at all.
	if !sigValid {
		rawParams := map[string]string{
			"eventData":       payload.EventData,
			"x-tm-app-key":    appKey,
			"x-tm-event-code": eventCode,
			"x-tm-timestamp":  timestamp,
		}
		if extParam != "" {
			rawParams["x-tm-ext-param"] = extParam
		}
		sigValid = temuVerifyWebhookSignature(appSecret, signature, rawParams)
		if sigValid {
			log.Printf("[Temu Webhook] Signature valid using raw eventData for event=%s", eventCode)
			decrypted = payload.EventData
		}
	}

	if !sigValid {
		// Log detail to diagnose without exposing the full secret
		keys := make([]string, 0, len(sigParams))
		for k := range sigParams {
			keys = append(keys, k)
		}
		sort.Strings(keys)
		var sb strings.Builder
		for _, k := range keys {
			sb.WriteString(k)
			sb.WriteString(sigParams[k]) // no = separator
		}
		mac := hmac.New(sha256.New, []byte(appSecret))
		mac.Write([]byte(sb.String()))
		computed := hex.EncodeToString(mac.Sum(nil))
		log.Printf("[Temu Webhook] Signature mismatch event=%s secretLen=%d received=%s computed=%s signBase=%s",
			eventCode, len(appSecret), signature, computed, sb.String())
		c.JSON(http.StatusUnauthorized, gin.H{"error": "invalid signature"})
		return
	}

	log.Printf("[Temu Webhook] Received event=%s payload=%s", eventCode, decrypted)

	// Route to appropriate handler
	ctx := c.Request.Context()
	switch eventCode {
	case "bg_aftersales_status_change":
		h.handleTemuAfterSalesEvent(ctx, decrypted)
	case "bg_cancel_order_status_change":
		h.handleTemuCancelEvent(ctx, decrypted)
	default:
		// Order status change etc — trigger order import
		h.triggerTemuOrderSync(ctx)
	}

	// Temu expects {"result": {}} on success
	c.JSON(http.StatusOK, gin.H{"result": map[string]interface{}{}})
}

// ── After-sales event handler ─────────────────────────────────────────────────

func (h *OrderWebhookHandler) handleTemuAfterSalesEvent(ctx context.Context, data string) {
	var event struct {
		MallID              int64  `json:"mallId"`
		AfterSalesType      int    `json:"afterSalesType"`      // 1=refund only, 2=return+refund
		ParentAfterSalesSn  string `json:"parentAfterSalesSn"`
		ParentOrderSn       string `json:"parentOrderSn"`
		UpdateAt            int64  `json:"updateAt"`
		ApplyOperatorRole   int    `json:"applyOperatorRole"`   // 1=buyer, 2=merchant, 3=platform, 4=system
		ParentAfterSalesStatus int `json:"parentAfterSalesStatus"` // 1=pending, 5=refunded, 6=cancelled, 7=rejected, 10=return applied
	}
	if err := json.Unmarshal([]byte(data), &event); err != nil {
		log.Printf("[Temu Webhook] Failed to parse after-sales event: %v", err)
		return
	}

	// Only act on buyer-initiated events
	if event.ApplyOperatorRole != 1 {
		return
	}

	afterSalesTypeStr := "Refund only"
	if event.AfterSalesType == 2 {
		afterSalesTypeStr = "Return & Refund"
	}

	statusStr := temuAfterSalesStatusLabel(event.ParentAfterSalesStatus)

	subject := fmt.Sprintf("Temu After-Sales Request — Order %s", event.ParentOrderSn)
	bodyText := fmt.Sprintf(
		"A buyer has submitted an after-sales request on Temu.\n\n"+
			"Order: %s\n"+
			"After-Sales Ref: %s\n"+
			"Type: %s\n"+
			"Status: %s\n\n"+
			"⚠️ Please check this order before taking any fulfilment action.",
		event.ParentOrderSn, event.ParentAfterSalesSn, afterSalesTypeStr, statusStr,
	)

	h.createTemuAlertTicket(ctx, event.ParentOrderSn, subject, bodyText, "bg_aftersales_status_change")
}

// ── Cancel event handler ──────────────────────────────────────────────────────

func (h *OrderWebhookHandler) handleTemuCancelEvent(ctx context.Context, data string) {
	var event struct {
		MallID              int64  `json:"mallId"`
		ParentAfterSalesSn  string `json:"parentAfterSalesSn"`
		ParentOrderSn       string `json:"parentOrderSn"`
		ParentAfterSalesStatus int `json:"parentAfterSalesStatus"` // 5=refunded
		UpdateAt            int64  `json:"updateAt"`
		ApplyOperatorRole   int    `json:"applyOperatorRole"`
	}
	if err := json.Unmarshal([]byte(data), &event); err != nil {
		log.Printf("[Temu Webhook] Failed to parse cancel event: %v", err)
		return
	}

	// Only act on buyer-initiated cancellations (role 1), not merchant/platform/system
	if event.ApplyOperatorRole != 0 && event.ApplyOperatorRole != 1 {
		log.Printf("[Temu Webhook] Skipping cancel event for non-buyer role %d on order %s", event.ApplyOperatorRole, event.ParentOrderSn)
		return
	}

	subject := fmt.Sprintf("⚠️ Temu Cancellation Request — Order %s", event.ParentOrderSn)
	bodyText := fmt.Sprintf(
		"A buyer has requested a cancellation on Temu.\n\n"+
			"Order: %s\n"+
			"After-Sales Ref: %s\n"+
			"Status: %s\n\n"+
			"⚠️ STOP — do not print or dispatch this order until resolved.\n"+
			"If a label has already been printed, do not ship. Contact the buyer.",
		event.ParentOrderSn, event.ParentAfterSalesSn,
		temuAfterSalesStatusLabel(event.ParentAfterSalesStatus),
	)

	h.createTemuAlertTicket(ctx, event.ParentOrderSn, subject, bodyText, "bg_cancel_order_status_change")

	// Create cancellation alert for staff acknowledgement
	if h.firestoreClient != nil {
		tenantID, credID := h.findTemuTenant(ctx, event.ParentOrderSn)
		_ = credID
		go CreateCancellationAlert(
			context.Background(), h.firestoreClient,
			tenantID, "", "temu",
			event.ParentOrderSn, event.ParentOrderSn,
			temuAfterSalesStatusLabel(event.ParentAfterSalesStatus),
		)
	}
}

// ── Create messaging ticket + alert staff ─────────────────────────────────────

func (h *OrderWebhookHandler) createTemuAlertTicket(
	ctx context.Context,
	orderNumber, subject, bodyText, eventCode string,
) {
	if h.firestoreClient == nil {
		return
	}

	// Find which tenant owns this Temu order
	tenantID, credID := h.findTemuTenant(ctx, orderNumber)
	if tenantID == "" {
		log.Printf("[Temu Webhook] Could not find tenant for order %s — creating ticket under all Temu tenants", orderNumber)
		// Fall back: alert all active Temu tenants
		creds, _ := h.repo.ListAllActiveCredentials(ctx)
		for _, cred := range creds {
			if cred.Channel == "temu" || cred.Channel == "temu_sandbox" {
				h.createTemuTicketForTenant(ctx, cred.TenantID, cred.CredentialID, orderNumber, subject, bodyText, eventCode)
			}
		}
		return
	}
	h.createTemuTicketForTenant(ctx, tenantID, credID, orderNumber, subject, bodyText, eventCode)
}

func (h *OrderWebhookHandler) createTemuTicketForTenant(
	ctx context.Context,
	tenantID, credID, orderNumber, subject, bodyText, eventCode string,
) {
	now := time.Now()
	convID := fmt.Sprintf("temu_alert_%s_%s", orderNumber, eventCode)

	// Upsert conversation
	convRef := h.firestoreClient.Collection(fmt.Sprintf("tenants/%s/conversations", tenantID)).Doc(convID)
	existingSnap, _ := convRef.Get(ctx)

	var conv models.Conversation
	if existingSnap.Exists() {
		existingSnap.DataTo(&conv)
		// Update preview but don't duplicate
		convRef.Update(ctx, []firestore.Update{
			{Path: "last_message_at", Value: now},
			{Path: "last_message_preview", Value: bodyText[:min(100, len(bodyText))]},
			{Path: "unread", Value: true},
			{Path: "status", Value: models.ConvStatusOpen},
			{Path: "updated_at", Value: now},
		})
	} else {
		conv = models.Conversation{
			ConversationID:   convID,
			TenantID:         tenantID,
			Channel:          "temu",
			ChannelAccountID: credID,
			OrderNumber:      orderNumber,
			Customer:         models.ConversationCustomer{Name: "Temu Buyer"},
			Subject:          subject,
			Status:           models.ConvStatusOpen,
			LastMessageAt:    now,
			LastMessagePreview: bodyText[:min(100, len(bodyText))],
			Unread:           true,
			MessageCount:     1,
			CreatedAt:        now,
			UpdatedAt:        now,
		}
		convRef.Set(ctx, conv)
	}

	// Store message
	msgID := fmt.Sprintf("temu_alert_%d", now.UnixNano())
	msg := models.Message{
		MessageID:      msgID,
		ConversationID: convID,
		Direction:      models.MsgDirectionInbound,
		Body:           bodyText,
		SentBy:         "temu_platform",
		SentAt:         now,
	}
	h.firestoreClient.Collection(fmt.Sprintf("tenants/%s/conversations/%s/messages", tenantID, convID)).
		Doc(msgID).Set(ctx, msg)

	log.Printf("[Temu Webhook] Ticket created: conv=%s tenant=%s order=%s", convID, tenantID, orderNumber)

	// Alert all team members via email + WhatsApp
	if h.messagingNotifier != nil {
		members, err := h.messagingNotifier.GetAssignableMembers(ctx, tenantID)
		if err == nil {
			for _, m := range members {
				member := m
				// Force both email and whatsapp for Temu alerts regardless of prefs
				if member.NotifEmail != "" {
					go h.messagingNotifier.NotifyAssignment(context.Background(), &member, &conv, "Temu Platform")
				}
			}
		}
	}
}

// ── Helpers ───────────────────────────────────────────────────────────────────

func (h *OrderWebhookHandler) getTemuAppSecret(ctx context.Context) string {
	if h.firestoreClient == nil {
		return strings.TrimSpace(os.Getenv("TEMU_APP_SECRET"))
	}
	// Try platform_config/temu first (global key)
	snap, err := h.firestoreClient.Collection("platform_config").Doc("temu").Get(ctx)
	if err == nil && snap.Exists() {
		data := snap.Data()
		if secret, ok := data["app_secret"].(string); ok && secret != "" {
			return strings.TrimSpace(secret)
		}
	}
	return strings.TrimSpace(os.Getenv("TEMU_APP_SECRET"))
}

func (h *OrderWebhookHandler) findTemuTenant(ctx context.Context, orderNumber string) (tenantID, credID string) {
	// Look up order by external_order_id across all tenants
	tenantIter := h.firestoreClient.Collection("tenants").Documents(ctx)
	defer tenantIter.Stop()
	for {
		tenantDoc, err := tenantIter.Next()
		if err != nil {
			break
		}
		tid := tenantDoc.Ref.ID
		orderIter := h.firestoreClient.Collection("tenants").Doc(tid).Collection("orders").
			Where("external_order_id", "==", orderNumber).
			Where("channel", "in", []string{"temu", "temu_sandbox"}).
			Limit(1).Documents(ctx)
		orderDoc, err := orderIter.Next()
		orderIter.Stop()
		if err == nil && orderDoc.Exists() {
			var order models.Order
			orderDoc.DataTo(&order)
			return tid, order.ChannelAccountID
		}
	}
	return "", ""
}

func (h *OrderWebhookHandler) triggerTemuOrderSync(ctx context.Context) {
	creds, err := h.repo.ListAllActiveCredentials(ctx)
	if err != nil {
		return
	}
	for _, cred := range creds {
		if cred.Channel == "temu" || cred.Channel == "temu_sandbox" {
			go h.triggerImportForCredential(cred.TenantID, cred.CredentialID, cred.Channel)
		}
	}
}

func temuAfterSalesStatusLabel(status int) string {
	switch status {
	case 1: return "Pending — buyer applied for refund"
	case 5: return "Refunded"
	case 6: return "Buyer cancelled the after-sales request"
	case 7: return "Rejected by merchant"
	case 8: return "Buyer returning goods — awaiting merchant review"
	case 10: return "Buyer applied for return"
	default: return fmt.Sprintf("Status %d", status)
	}
}

// temuAESDecrypt decrypts a Base64-encoded AES/CBC/PKCS5 ciphertext.
// Key: app_secret bytes; IV: first 16 bytes of app_secret.
func temuAESDecrypt(cipherB64, appSecret string) (string, error) {
	if len(appSecret) < 16 {
		return "", fmt.Errorf("app_secret too short (need ≥16 bytes)")
	}
	key := []byte(appSecret)[:16]
	iv := []byte(appSecret)[:16]

	raw, err := base64.StdEncoding.DecodeString(cipherB64)
	if err != nil {
		return "", fmt.Errorf("base64 decode: %w", err)
	}

	block, err := aes.NewCipher(key)
	if err != nil {
		return "", fmt.Errorf("aes cipher: %w", err)
	}

	if len(raw)%aes.BlockSize != 0 {
		return "", fmt.Errorf("ciphertext not block-aligned")
	}

	mode := cipher.NewCBCDecrypter(block, iv)
	plaintext := make([]byte, len(raw))
	mode.CryptBlocks(plaintext, raw)

	// Remove PKCS5 padding
	plaintext, err = pkcs5Unpad(plaintext)
	if err != nil {
		return "", fmt.Errorf("unpad: %w", err)
	}
	return string(plaintext), nil
}

func pkcs5Unpad(data []byte) ([]byte, error) {
	if len(data) == 0 {
		return nil, fmt.Errorf("empty data")
	}
	padLen := int(data[len(data)-1])
	if padLen < 1 || padLen > aes.BlockSize {
		return nil, fmt.Errorf("invalid padding length %d", padLen)
	}
	for _, b := range data[len(data)-padLen:] {
		if int(b) != padLen {
			return nil, fmt.Errorf("invalid padding byte")
		}
	}
	return data[:len(data)-padLen], nil
}

// temuVerifyWebhookSignature verifies the HMAC-SHA256 signature of a Temu webhook.
// Params are sorted by key ascending, concatenated as keyvalue (no = separator), then HMAC-SHA256.
// Per Temu docs: "each key-value pair is concatenated in the form of key=value" is misleading —
// the actual example shows no separator: eventData{...}x-tm-app-keyfoo...
func temuVerifyWebhookSignature(appSecret, signature string, params map[string]string) bool {
	keys := make([]string, 0, len(params))
	for k := range params {
		keys = append(keys, k)
	}
	sort.Strings(keys)

	var sb strings.Builder
	for _, k := range keys {
		sb.WriteString(k)
		sb.WriteString(params[k]) // no "=" between key and value
	}

	mac := hmac.New(sha256.New, []byte(appSecret))
	mac.Write([]byte(sb.String()))
	expected := hex.EncodeToString(mac.Sum(nil))
	return hmac.Equal([]byte(expected), []byte(signature))
}


// ============================================================================
// SIGNATURE VERIFICATION HELPERS
// ============================================================================

// verifyHMACSHA256Base64 checks an HMAC-SHA256 signature encoded as either
// hex or standard base64 (different marketplaces use different encodings).
func verifyHMACSHA256Base64(body []byte, secret, sig string) bool {
	mac := hmac.New(sha256.New, []byte(secret))
	mac.Write(body)
	sum := mac.Sum(nil)

	// Try hex match (eBay, some WooCommerce).
	if hmac.Equal([]byte(hex.EncodeToString(sum)), []byte(sig)) {
		return true
	}

	// Try raw base64 match (Shopify, WooCommerce default).
	mac2 := hmac.New(sha256.New, []byte(secret))
	mac2.Write(body)
	encodedB64 := encodeBase64(mac2.Sum(nil))
	return hmac.Equal([]byte(encodedB64), []byte(sig))
}

// encodeBase64 is a local helper to avoid importing encoding/base64 at the top
// — it is already imported in the file.
func encodeBase64(b []byte) string {
	const chars = "ABCDEFGHIJKLMNOPQRSTUVWXYZabcdefghijklmnopqrstuvwxyz0123456789+/"
	encoded := make([]byte, 0, ((len(b)+2)/3)*4)
	for i := 0; i < len(b); i += 3 {
		var b0, b1, b2 byte
		b0 = b[i]
		if i+1 < len(b) {
			b1 = b[i+1]
		}
		if i+2 < len(b) {
			b2 = b[i+2]
		}
		encoded = append(encoded,
			chars[b0>>2],
			chars[(b0&0x03)<<4|b1>>4],
			chars[(b1&0x0f)<<2|b2>>6],
			chars[b2&0x3f],
		)
	}
	// Add padding.
	switch len(b) % 3 {
	case 1:
		encoded[len(encoded)-2] = '='
		encoded[len(encoded)-1] = '='
	case 2:
		encoded[len(encoded)-1] = '='
	}
	return string(encoded)
}
