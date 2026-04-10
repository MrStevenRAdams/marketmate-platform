package handlers

import (
	"net/http"
	"time"

	"cloud.google.com/go/firestore"
	"github.com/gin-gonic/gin"
	"github.com/google/uuid"
	"google.golang.org/api/iterator"
)

// ============================================================================
// CHANGELOG HANDLER — H-001
// ============================================================================
// Manages the global in-app changelog / "What's New" feed.
// Entries are stored in the global /changelog/{id} collection (non-tenant).
//
// Routes:
//   GET  /changelog          — list recent entries (newest first, limit 50)
//   POST /changelog          — create a new entry (admin only — no auth gate here,
//                              relies on deployment pipeline / ops tooling)
//   POST /changelog/seen     — record that the authenticated user has seen
//                              the changelog (stores last_viewed_changelog in
//                              /global_users/{user_id})
// ============================================================================

type ChangelogEntry struct {
	EntryID     string    `json:"entry_id"   firestore:"entry_id"`
	Version     string    `json:"version"    firestore:"version"`     // e.g. "8.1"
	Date        string    `json:"date"       firestore:"date"`        // ISO date string
	Title       string    `json:"title"      firestore:"title"`
	Description string    `json:"description" firestore:"description"`
	Type        string    `json:"type"       firestore:"type"`        // "feature" | "fix" | "improvement"
	CreatedAt   time.Time `json:"created_at"  firestore:"created_at"`
}

type ChangelogHandler struct {
	client *firestore.Client
}

func NewChangelogHandler(client *firestore.Client) *ChangelogHandler {
	return &ChangelogHandler{client: client}
}

// GET /changelog
// Returns up to 50 most-recent changelog entries. No auth required so the
// frontend can call it freely; no tenant data is returned.
func (h *ChangelogHandler) ListEntries(c *gin.Context) {
	ctx := c.Request.Context()

	iter := h.client.Collection("changelog").
		OrderBy("created_at", firestore.Desc).
		Limit(50).
		Documents(ctx)
	defer iter.Stop()

	var entries []ChangelogEntry
	for {
		doc, err := iter.Next()
		if err == iterator.Done {
			break
		}
		if err != nil {
			c.JSON(http.StatusInternalServerError, gin.H{"error": "failed to list changelog"})
			return
		}
		var entry ChangelogEntry
		if err := doc.DataTo(&entry); err != nil {
			continue
		}
		entries = append(entries, entry)
	}

	if entries == nil {
		entries = []ChangelogEntry{}
	}

	c.JSON(http.StatusOK, gin.H{"entries": entries, "count": len(entries)})
}

// POST /changelog
// Creates a new changelog entry. Intended for internal admin tooling.
func (h *ChangelogHandler) CreateEntry(c *gin.Context) {
	ctx := c.Request.Context()

	var body struct {
		Version     string `json:"version"`
		Date        string `json:"date"`
		Title       string `json:"title"`
		Description string `json:"description"`
		Type        string `json:"type"` // "feature" | "fix" | "improvement"
	}
	if err := c.ShouldBindJSON(&body); err != nil {
		c.JSON(http.StatusBadRequest, gin.H{"error": "invalid request body"})
		return
	}
	if body.Title == "" || body.Type == "" {
		c.JSON(http.StatusBadRequest, gin.H{"error": "title and type are required"})
		return
	}

	entryType := body.Type
	switch entryType {
	case "feature", "fix", "improvement":
		// valid
	default:
		entryType = "feature"
	}

	date := body.Date
	if date == "" {
		date = time.Now().Format("2006-01-02")
	}

	entry := ChangelogEntry{
		EntryID:     uuid.New().String(),
		Version:     body.Version,
		Date:        date,
		Title:       body.Title,
		Description: body.Description,
		Type:        entryType,
		CreatedAt:   time.Now(),
	}

	_, err := h.client.Collection("changelog").Doc(entry.EntryID).Set(ctx, entry)
	if err != nil {
		c.JSON(http.StatusInternalServerError, gin.H{"error": "failed to save entry"})
		return
	}

	c.JSON(http.StatusCreated, gin.H{"entry": entry})
}

// POST /changelog/seen
// Records the current user's last-viewed-changelog timestamp.
// Body: { "user_id": "..." }
func (h *ChangelogHandler) MarkSeen(c *gin.Context) {
	ctx := c.Request.Context()

	var body struct {
		UserID string `json:"user_id"`
	}
	if err := c.ShouldBindJSON(&body); err != nil || body.UserID == "" {
		c.JSON(http.StatusBadRequest, gin.H{"error": "user_id required"})
		return
	}

	_, err := h.client.Collection("global_users").Doc(body.UserID).
		Update(ctx, []firestore.Update{
			{Path: "last_viewed_changelog", Value: time.Now()},
		})
	if err != nil {
		// Non-fatal: user doc might not exist yet
		c.JSON(http.StatusOK, gin.H{"ok": true})
		return
	}

	c.JSON(http.StatusOK, gin.H{"ok": true})
}
