package models

import "time"

// ============================================================================
// MESSAGING MODEL
// Collections:
//   tenants/{tenant_id}/conversations/{conversation_id}
//   tenants/{tenant_id}/conversations/{conversation_id}/messages/{message_id}
//   tenants/{tenant_id}/canned_responses/{id}
// ============================================================================

type Conversation struct {
	ConversationID      string    `json:"conversation_id" firestore:"conversation_id"`
	TenantID            string    `json:"tenant_id" firestore:"tenant_id"`
	Channel             string    `json:"channel" firestore:"channel"` // "amazon" | "ebay"
	ChannelAccountID    string    `json:"channel_account_id,omitempty" firestore:"channel_account_id,omitempty"`
	MarketplaceThreadID string    `json:"marketplace_thread_id,omitempty" firestore:"marketplace_thread_id,omitempty"`
	OrderID             string    `json:"order_id,omitempty" firestore:"order_id,omitempty"`
	OrderNumber         string    `json:"order_number,omitempty" firestore:"order_number,omitempty"`

	Customer ConversationCustomer `json:"customer" firestore:"customer"`

	Subject       string    `json:"subject,omitempty" firestore:"subject,omitempty"`
	Status        string    `json:"status" firestore:"status"` // "open" | "pending_reply" | "resolved"
	LastMessageAt time.Time `json:"last_message_at" firestore:"last_message_at"`
	Unread        bool      `json:"unread" firestore:"unread"`

	// Denormalised last message preview for list view
	LastMessagePreview string `json:"last_message_preview,omitempty" firestore:"last_message_preview,omitempty"`
	AssignedTo *AssignedTo `json:"assigned_to,omitempty" firestore:"assigned_to,omitempty"`
	MessageCount       int    `json:"message_count,omitempty" firestore:"message_count,omitempty"`

	CreatedAt time.Time `json:"created_at" firestore:"created_at"`
	UpdatedAt time.Time `json:"updated_at" firestore:"updated_at"`
}

type ConversationCustomer struct {
	Name    string `json:"name" firestore:"name"`
	BuyerID string `json:"buyer_id,omitempty" firestore:"buyer_id,omitempty"`
}

// AssignedTo identifies a team member a conversation is assigned to.
type AssignedTo struct {
	MembershipID string `json:"membership_id" firestore:"membership_id"`
	DisplayName  string `json:"display_name" firestore:"display_name"`
	Email        string `json:"email" firestore:"email"`
	AssignedAt   time.Time `json:"assigned_at" firestore:"assigned_at"`
	AssignedBy   string `json:"assigned_by,omitempty" firestore:"assigned_by,omitempty"` // user_id
}

// NotificationPrefs stores how a team member wants to be alerted.
// Stored on UserMembership.NotificationPrefs.
type MessagingNotificationPrefs struct {
	Email    string   `json:"email,omitempty" firestore:"email,omitempty"`       // override email (defaults to account email)
	Phone    string   `json:"phone,omitempty" firestore:"phone,omitempty"`       // E.164 e.g. +447880311499
	Channels []string `json:"channels,omitempty" firestore:"channels,omitempty"` // "email" | "whatsapp" | "sms"
}

type Message struct {
	MessageID        string    `json:"message_id" firestore:"message_id"`
	ConversationID   string    `json:"conversation_id" firestore:"conversation_id"`
	Direction        string    `json:"direction" firestore:"direction"` // "inbound" | "outbound"
	Body             string    `json:"body" firestore:"body"`
	ChannelMessageID string    `json:"channel_message_id,omitempty" firestore:"channel_message_id,omitempty"`
	SentBy           string    `json:"sent_by,omitempty" firestore:"sent_by,omitempty"`
	SentAt           time.Time `json:"sent_at" firestore:"sent_at"`
	ReadAt           *time.Time `json:"read_at,omitempty" firestore:"read_at,omitempty"`
}

type CannedResponse struct {
	ID       string   `json:"id" firestore:"id"`
	TenantID string   `json:"tenant_id" firestore:"tenant_id"`
	Title    string   `json:"title" firestore:"title"`
	Body     string   `json:"body" firestore:"body"`
	Channels []string `json:"channels,omitempty" firestore:"channels,omitempty"`
}

// ── Status constants ──────────────────────────────────────────────────────────

const (
	ConvStatusOpen         = "open"
	ConvStatusPendingReply = "pending_reply"
	ConvStatusResolved     = "resolved"

	MsgDirectionInbound  = "inbound"
	MsgDirectionOutbound = "outbound"
)
