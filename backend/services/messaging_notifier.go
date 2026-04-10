package services

// ============================================================================
// MESSAGING NOTIFIER
// ============================================================================
// Sends assignment notifications to team members via:
//   - Email (SMTP, same config as template emails)
//   - WhatsApp (Twilio sandbox / production)
//   - SMS (Twilio)
//
// Credentials sourced from environment variables (set on Cloud Run):
//   TWILIO_ACCOUNT_SID  — from Secret Manager marketmate-twilio-account-sid
//   TWILIO_AUTH_TOKEN   — from Secret Manager marketmate-twilio-auth-token
//   TWILIO_FROM         — e.g. "whatsapp:+14155238886" or "+14155238886" for SMS
//   SMTP_HOST / SMTP_PORT / SMTP_USERNAME / SMTP_PASSWORD / SMTP_FROM
// ============================================================================

import (
	"context"
	"crypto/hmac"
	"crypto/sha256"
	"crypto/tls"
	"encoding/base64"
	"encoding/hex"
	"encoding/json"
	"fmt"
	"io"
	"log"
	"net/http"
	"net/smtp"
	"net/url"
	"os"
	"strings"
	"time"

	"cloud.google.com/go/firestore"
	"google.golang.org/api/iterator"

	"module-a/models"
)

type MessagingNotifier struct {
	client *firestore.Client
}

func NewMessagingNotifier(client *firestore.Client) *MessagingNotifier {
	return &MessagingNotifier{client: client}
}

// ── Member lookup ─────────────────────────────────────────────────────────────

type AssignableMember struct {
	MembershipID string `json:"membership_id"`
	DisplayName  string `json:"display_name"`
	Email        string `json:"email"`
	AvatarURL    string `json:"avatar_url,omitempty"`
	// NotifPrefs from UserMembership
	NotifEmail    string   `json:"notif_email,omitempty"`
	NotifPhone    string   `json:"notif_phone,omitempty"`
	NotifChannels []string `json:"notif_channels,omitempty"`
}

// GetAssignableMembers returns all active members for a tenant with their
// notification preferences. Used to populate the assignee picker in the UI.
func (n *MessagingNotifier) GetAssignableMembers(ctx context.Context, tenantID string) ([]AssignableMember, error) {
	iter := n.client.Collection("user_memberships").
		Where("tenant_id", "==", tenantID).
		Where("status", "==", "active").
		Documents(ctx)
	defer iter.Stop()

	var members []AssignableMember
	for {
		snap, err := iter.Next()
		if err == iterator.Done {
			break
		}
		if err != nil {
			return nil, err
		}

		var m models.UserMembership
		if err := snap.DataTo(&m); err != nil {
			continue
		}

		// Load user profile for email + display name
		userSnap, err := n.client.Collection("users").Doc(m.UserID).Get(ctx)
		if err != nil {
			continue
		}
		userData := userSnap.Data()
		email, _ := userData["email"].(string)
		displayName, _ := userData["display_name"].(string)
		avatarURL, _ := userData["avatar_url"].(string)
		if displayName == "" {
			displayName = m.DisplayNameHint
		}
		if displayName == "" {
			displayName = email
		}

		member := AssignableMember{
			MembershipID: m.MembershipID,
			DisplayName:  displayName,
			Email:        email,
			AvatarURL:    avatarURL,
		}

		if m.MessagingNotifPrefs != nil {
			member.NotifEmail = m.MessagingNotifPrefs.Email
			member.NotifPhone = m.MessagingNotifPrefs.Phone
			member.NotifChannels = m.MessagingNotifPrefs.Channels
		}
		// Default notification channel to email if none set
		if len(member.NotifChannels) == 0 {
			member.NotifChannels = []string{"email"}
		}
		// Default notif email to account email
		if member.NotifEmail == "" {
			member.NotifEmail = email
		}

		members = append(members, member)
	}
	return members, nil
}

// GetMember returns a single member by membership_id.
func (n *MessagingNotifier) GetMember(ctx context.Context, tenantID, membershipID string) (*AssignableMember, error) {
	members, err := n.GetAssignableMembers(ctx, tenantID)
	if err != nil {
		return nil, err
	}
	for _, m := range members {
		if m.MembershipID == membershipID {
			return &m, nil
		}
	}
	return nil, fmt.Errorf("member %s not found", membershipID)
}

// ── Notification dispatch ─────────────────────────────────────────────────────

// NotifyAssignment sends an assignment notification to a team member.
// It respects the member's configured notification channels.
func (n *MessagingNotifier) NotifyAssignment(
	ctx context.Context,
	member *AssignableMember,
	conv *models.Conversation,
	assignedByName string,
) {
	if member == nil {
		return
	}

	subject := fmt.Sprintf("Message assigned to you — %s (%s)",
		conv.Customer.Name, conv.Channel)

	body := fmt.Sprintf(
		"Hi %s,\n\nA buyer message has been assigned to you.\n\n"+
			"Customer: %s\n"+
			"Channel: %s\n"+
			"Order: %s\n"+
			"Subject: %s\n"+
			"Preview: %s\n\n"+
			"Assigned by: %s\n\n"+
			"Please reply within Amazon's 24-hour SLA window.\n\n"+
			"Open MarketMate → Messages to respond.",
		member.DisplayName,
		conv.Customer.Name,
		strings.ToUpper(conv.Channel),
		conv.OrderNumber,
		conv.Subject,
		conv.LastMessagePreview,
		assignedByName,
	)

	channels := member.NotifChannels
	if len(channels) == 0 {
		channels = []string{"email"}
	}

	for _, ch := range channels {
		switch ch {
		case "email":
			if member.NotifEmail != "" {
				if err := n.sendEmail(member.NotifEmail, member.DisplayName, subject, body); err != nil {
					log.Printf("[MessagingNotifier] Email failed to %s: %v", member.NotifEmail, err)
				} else {
					log.Printf("[MessagingNotifier] Email sent to %s", member.NotifEmail)
				}
			}
		case "whatsapp":
			if member.NotifPhone != "" {
				to := member.NotifPhone
				if !strings.HasPrefix(to, "whatsapp:") {
					to = "whatsapp:" + to
				}
				msg := fmt.Sprintf("📬 *MarketMate*: Message assigned to you\n\nCustomer: %s\nChannel: %s\nOrder: %s\nPreview: %s\n\nAssigned by: %s\n\nPlease respond within 24h.",
					conv.Customer.Name, strings.ToUpper(conv.Channel),
					conv.OrderNumber, conv.LastMessagePreview, assignedByName)
				if err := n.sendTwilio(to, msg); err != nil {
					log.Printf("[MessagingNotifier] WhatsApp failed to %s: %v", member.NotifPhone, err)
				} else {
					log.Printf("[MessagingNotifier] WhatsApp sent to %s", member.NotifPhone)
				}
			}
		case "sms":
			if member.NotifPhone != "" {
				to := member.NotifPhone
				// Ensure no whatsapp: prefix for SMS
				to = strings.TrimPrefix(to, "whatsapp:")
				// Generate a deep link so staff can tap to open the exact conversation
				deepLink := n.generateMobileLink(conv.ConversationID, conv.TenantID)
				var msg string
				if deepLink != "" {
					msg = fmt.Sprintf("MarketMate: New message from %s (Order: %s)\nPreview: %s\n\nReply here: %s",
						conv.Customer.Name, conv.OrderNumber,
						truncate(conv.LastMessagePreview, 80), deepLink)
				} else {
					msg = fmt.Sprintf("MarketMate: Message assigned to you. Customer: %s, Order: %s. Please respond within 24h.",
						conv.Customer.Name, conv.OrderNumber)
				}
				if err := n.sendTwilio(to, msg); err != nil {
					log.Printf("[MessagingNotifier] SMS failed to %s: %v", member.NotifPhone, err)
				} else {
					log.Printf("[MessagingNotifier] SMS sent to %s", member.NotifPhone)
				}
			}
		}
	}
}

// ── Email (SMTP) ──────────────────────────────────────────────────────────────

func (n *MessagingNotifier) sendEmail(toEmail, toName, subject, body string) error {
	host := os.Getenv("SMTP_HOST")
	port := os.Getenv("SMTP_PORT")
	username := os.Getenv("SMTP_USERNAME")
	password := os.Getenv("SMTP_PASSWORD")
	from := os.Getenv("SMTP_FROM")
	fromName := os.Getenv("SMTP_FROM_NAME")

	if host == "" {
		return fmt.Errorf("SMTP not configured")
	}
	if port == "" {
		port = "587"
	}
	if fromName == "" {
		fromName = "MarketMate"
	}

	// Sanitise email headers to prevent SMTP header injection (G707)
	// Strip any CR/LF characters from values that go into email headers
	sanitiseHeader := func(s string) string {
		s = strings.ReplaceAll(s, "\r", "")
		s = strings.ReplaceAll(s, "\n", "")
		return s
	}
	toEmailSafe := sanitiseHeader(toEmail)
	toNameSafe  := sanitiseHeader(toName)
	fromSafe    := sanitiseHeader(from)
	fromNameSafe := sanitiseHeader(fromName)
	subjectSafe  := sanitiseHeader(subject)

	to := fmt.Sprintf("%s <%s>", toNameSafe, toEmailSafe)
	fromAddr := fmt.Sprintf("%s <%s>", fromNameSafe, fromSafe)

	headers := fmt.Sprintf(
		"From: %s\r\nTo: %s\r\nSubject: %s\r\nContent-Type: text/plain; charset=UTF-8\r\nDate: %s\r\n\r\n",
		fromAddr, to, subjectSafe, time.Now().Format(time.RFC1123Z),
	)
	msg := []byte(headers + body)

	addr := fmt.Sprintf("%s:%s", host, port)
	tlsCfg := &tls.Config{ServerName: host}

	// Try implicit TLS first (port 465)
	conn, err := tls.Dial("tcp", addr, tlsCfg)
	if err == nil {
		client, err := smtp.NewClient(conn, host)
		if err != nil {
			return err
		}
		defer client.Close()
		if username != "" {
			auth := smtp.PlainAuth("", username, password, host)
			if err := client.Auth(auth); err != nil {
				return err
			}
		}
		if err := client.Mail(fromSafe); err != nil {
			return err
		}
		if err := client.Rcpt(toEmailSafe); err != nil {
			return err
		}
		w, err := client.Data()
		if err != nil {
			return err
		}
		if _, err := w.Write(msg); err != nil {
			return err
		}
		return w.Close()
	}

	// Fall back to STARTTLS (port 587) — connect plain, upgrade to TLS, then auth
	client, err := smtp.Dial(addr)
	if err != nil {
		return fmt.Errorf("SMTP dial failed: %w", err)
	}
	defer client.Close()

	if err := client.StartTLS(tlsCfg); err != nil {
		return fmt.Errorf("STARTTLS failed: %w", err)
	}

	if username != "" {
		auth := smtp.PlainAuth("", username, password, host)
		if err := client.Auth(auth); err != nil {
			return fmt.Errorf("SMTP auth failed: %w", err)
		}
	}

	if err := client.Mail(fromSafe); err != nil {
		return err
	}
	if err := client.Rcpt(toEmailSafe); err != nil {
		return err
	}
	w, err := client.Data()
	if err != nil {
		return err
	}
	if _, err := w.Write(msg); err != nil {
		return err
	}
	return w.Close()
}

// ── Twilio (WhatsApp / SMS) ───────────────────────────────────────────────────

func (n *MessagingNotifier) sendTwilio(to, body string) error {
	sid := os.Getenv("TWILIO_ACCOUNT_SID")
	token := os.Getenv("TWILIO_AUTH_TOKEN")
	from := os.Getenv("TWILIO_FROM")

	if sid == "" || token == "" || from == "" {
		return fmt.Errorf("Twilio not configured (TWILIO_ACCOUNT_SID / TWILIO_AUTH_TOKEN / TWILIO_FROM)")
	}

	// For WhatsApp recipients, ensure from also has whatsapp: prefix
	if strings.HasPrefix(to, "whatsapp:") && !strings.HasPrefix(from, "whatsapp:") {
		from = "whatsapp:" + from
	}

	apiURL := fmt.Sprintf("https://api.twilio.com/2010-04-01/Accounts/%s/Messages.json", sid)
	data := url.Values{
		"From": {from},
		"To":   {to},
		"Body": {body},
	}

	creds := base64.StdEncoding.EncodeToString([]byte(sid + ":" + token))
	req, err := http.NewRequest("POST", apiURL, strings.NewReader(data.Encode()))
	if err != nil {
		return err
	}
	req.Header.Set("Authorization", "Basic "+creds)
	req.Header.Set("Content-Type", "application/x-www-form-urlencoded")

	client := &http.Client{Timeout: 15 * time.Second}
	resp, err := client.Do(req)
	if err != nil {
		return err
	}
	defer resp.Body.Close()
	respBody, _ := io.ReadAll(resp.Body)

	if resp.StatusCode < 200 || resp.StatusCode >= 300 {
		var twErr struct {
			Message string `json:"message"`
			Code    int    `json:"code"`
		}
		json.Unmarshal(respBody, &twErr)
		return fmt.Errorf("Twilio %d: %s", resp.StatusCode, twErr.Message)
	}
	return nil
}

// generateMobileLink creates a 24h signed deep link for a conversation.
func (n *MessagingNotifier) generateMobileLink(convID, tenantID string) string {
	if convID == "" || tenantID == "" {
		return ""
	}
	expiry := time.Now().Add(24 * time.Hour).Unix()
	secret := os.Getenv("CREDENTIAL_ENCRYPTION_KEY")
	if len(secret) < 16 {
		secret = "marketmate-mobile-link-secret-32"
	}
	payload := fmt.Sprintf("%s:%s:%d", convID, tenantID, expiry)
	mac := hmac.New(sha256.New, []byte(secret))
	mac.Write([]byte(payload))
	token := hex.EncodeToString(mac.Sum(nil))[:16]

	baseURL := os.Getenv("FRONTEND_URL")
	if baseURL == "" {
		baseURL = "https://e-lister-site-2026.web.app"
	}
	return fmt.Sprintf("%s/mobile/conversation/%s?tenant=%s&exp=%d&token=%s",
		baseURL, convID, tenantID, expiry, token)
}

// truncate shortens a string to maxLen chars, appending … if cut.
func truncate(s string, maxLen int) string {
	if len(s) <= maxLen {
		return s
	}
	return s[:maxLen] + "…"
}
