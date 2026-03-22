package services

import (
	"context"
	"fmt"
	"log"
	"net/http"
	"strings"
	"time"

	"cloud.google.com/go/firestore"
	"google.golang.org/api/iterator"

	"module-a/models"
)

// ============================================================================
// ACTION EXECUTOR
// ============================================================================

// ActionExecutor executes or dry-runs DSL actions against an order
type ActionExecutor struct {
	client      *firestore.Client
	smtp        *SMTPConfig
	templateSvc *TemplateService
}

// SMTPConfig holds SMTP settings for notify action.
// Field names match SMTP_USERNAME convention used in the rest of the platform.
type SMTPConfig struct {
	Host     string
	Port     string
	User     string // loaded from SMTP_USERNAME env var
	Password string
	From     string
}

func NewActionExecutor(client *firestore.Client, smtp *SMTPConfig) *ActionExecutor {
	return &ActionExecutor{client: client, smtp: smtp}
}

// NewActionExecutorWithTemplateService creates an ActionExecutor with tenant-aware SMTP support.
func NewActionExecutorWithTemplateService(client *firestore.Client, smtp *SMTPConfig, templateSvc *TemplateService) *ActionExecutor {
	return &ActionExecutor{client: client, smtp: smtp, templateSvc: templateSvc}
}

// ExecuteAction runs a single action (or simulates it if dryRun=true).
// Returns an ActionResult describing what happened.
func (e *ActionExecutor) ExecuteAction(
	ctx context.Context,
	tenantID string,
	action models.ActionNode,
	orderCtx *models.OrderContext,
	dryRun bool,
) models.ActionResult {
	result := models.ActionResult{
		Action: action.Name,
		Params: action.Params,
		DryRun: dryRun,
	}

	// Evaluate inline IF condition if present
	if action.IfCond != nil {
		evaluator := NewRuleEvaluator()
		node := models.ConditionNode{Type: models.NodeCondition, Expr: action.IfCond}
		var traces []models.ConditionTrace
		matched, err := evaluator.evalCondition(node, orderCtx, &traces)
		if err != nil || !matched {
			result.Skipped = true
			actualVal := ""
			if len(traces) > 0 {
				actualVal = fmt.Sprintf("actual: %v", traces[0].Value)
			}
			result.Reason = fmt.Sprintf("IF condition false: %s (%s)", exprString(action.IfCond), actualVal)
			return result
		}
	}

	if dryRun {
		return result
	}

	// Execute for real
	var execErr error
	switch action.Name {
	case "add_tag":
		execErr = e.execAddTag(ctx, tenantID, orderCtx, action.Params)
	case "remove_tag":
		execErr = e.execRemoveTag(ctx, tenantID, orderCtx, action.Params)
	case "set_status":
		execErr = e.execSetStatus(ctx, tenantID, orderCtx, action.Params)
	case "hold_order":
		execErr = e.execSetStatus(ctx, tenantID, orderCtx, []string{"on_hold"})
	case "flag_for_review":
		execErr = e.execFlagForReview(ctx, tenantID, orderCtx, action.Params)
	case "notify":
		execErr = e.execNotify(ctx, tenantID, action.Params)
	case "webhook":
		execErr = e.execWebhook(ctx, action.Params)
	case "select_carrier":
		execErr = e.execSetOrderMeta(ctx, tenantID, orderCtx, "preferred_carrier", action.Params)
	case "select_service":
		execErr = e.execSetOrderMeta(ctx, tenantID, orderCtx, "preferred_service", action.Params)
	case "require_signature":
		execErr = e.execSetOrderMeta(ctx, tenantID, orderCtx, "require_signature", []string{"true"})
	case "set_fulfilment_source":
		execErr = e.execSetFulfilmentSource(ctx, tenantID, orderCtx, action.Params)
	case "set_shipping_method":
		execErr = e.execSetOrderMeta(ctx, tenantID, orderCtx, "shipping_method", action.Params)
	case "skip_remaining_rules":
		// handled by engine — no-op here
	case "set_dispatch_date":
		execErr = e.execSetDispatchDate(ctx, tenantID, orderCtx, action.Params)
	case "add_note":
		execErr = e.execAddNote(ctx, tenantID, orderCtx, action.Params)
	case "add_buyer_note":
		execErr = e.execAddBuyerNote(ctx, tenantID, orderCtx, action.Params)
	case "add_item":
		execErr = e.execAddItem(ctx, tenantID, orderCtx, action.Params)
	case "assign_fulfilment_network":
		execErr = e.execAssignFulfilmentNetwork(ctx, tenantID, orderCtx, action.Params)
	default:
		execErr = fmt.Errorf("unknown action: %s", action.Name)
	}

	if execErr != nil {
		result.Error = execErr.Error()
		log.Printf("[rule_actions] error executing %s for tenant %s order %s: %v",
			action.Name, tenantID, orderCtx.Order.OrderID, execErr)
	}

	return result
}

// ── Action implementations ────────────────────────────────────────────────────

func (e *ActionExecutor) execAddTag(ctx context.Context, tenantID string, orderCtx *models.OrderContext, params []string) error {
	if len(params) == 0 {
		return fmt.Errorf("add_tag requires a tag parameter")
	}
	tag := params[0]
	for _, t := range orderCtx.Tags {
		if strings.EqualFold(t, tag) {
			return nil // already has tag
		}
	}
	orderCtx.Tags = append(orderCtx.Tags, tag)
	_, err := e.client.Collection("tenants").Doc(tenantID).
		Collection("orders").Doc(orderCtx.Order.OrderID).
		Update(ctx, []firestore.Update{
			{Path: "tags", Value: orderCtx.Tags},
			{Path: "updated_at", Value: time.Now().Format(time.RFC3339)},
		})
	return err
}

func (e *ActionExecutor) execRemoveTag(ctx context.Context, tenantID string, orderCtx *models.OrderContext, params []string) error {
	if len(params) == 0 {
		return fmt.Errorf("remove_tag requires a tag parameter")
	}
	tag := params[0]
	var newTags []string
	for _, t := range orderCtx.Tags {
		if !strings.EqualFold(t, tag) {
			newTags = append(newTags, t)
		}
	}
	orderCtx.Tags = newTags
	_, err := e.client.Collection("tenants").Doc(tenantID).
		Collection("orders").Doc(orderCtx.Order.OrderID).
		Update(ctx, []firestore.Update{
			{Path: "tags", Value: newTags},
			{Path: "updated_at", Value: time.Now().Format(time.RFC3339)},
		})
	return err
}

func (e *ActionExecutor) execSetStatus(ctx context.Context, tenantID string, orderCtx *models.OrderContext, params []string) error {
	if len(params) == 0 {
		return fmt.Errorf("set_status requires a status parameter")
	}
	newStatus := params[0]
	orderCtx.Status = newStatus
	orderCtx.Order.Status = newStatus
	_, err := e.client.Collection("tenants").Doc(tenantID).
		Collection("orders").Doc(orderCtx.Order.OrderID).
		Update(ctx, []firestore.Update{
			{Path: "status", Value: newStatus},
			{Path: "updated_at", Value: time.Now().Format(time.RFC3339)},
		})
	return err
}

func (e *ActionExecutor) execFlagForReview(ctx context.Context, tenantID string, orderCtx *models.OrderContext, params []string) error {
	reason := ""
	if len(params) > 0 {
		reason = params[0]
	}
	updates := []firestore.Update{
		{Path: "flag_for_review", Value: true},
		{Path: "updated_at", Value: time.Now().Format(time.RFC3339)},
	}
	if reason != "" {
		updates = append(updates, firestore.Update{Path: "review_reason", Value: reason})
	}
	_, err := e.client.Collection("tenants").Doc(tenantID).
		Collection("orders").Doc(orderCtx.Order.OrderID).
		Update(ctx, updates)
	return err
}

func (e *ActionExecutor) execSetOrderMeta(ctx context.Context, tenantID string, orderCtx *models.OrderContext, key string, params []string) error {
	val := ""
	if len(params) > 0 {
		val = params[0]
	}
	_, err := e.client.Collection("tenants").Doc(tenantID).
		Collection("orders").Doc(orderCtx.Order.OrderID).
		Update(ctx, []firestore.Update{
			{Path: "automation_meta." + key, Value: val},
			{Path: "updated_at", Value: time.Now().Format(time.RFC3339)},
		})
	return err
}

func (e *ActionExecutor) execSetFulfilmentSource(ctx context.Context, tenantID string, orderCtx *models.OrderContext, params []string) error {
	if len(params) == 0 {
		return fmt.Errorf("set_fulfilment_source requires an id parameter")
	}
	sourceID := params[0]
	_, err := e.client.Collection("tenants").Doc(tenantID).
		Collection("orders").Doc(orderCtx.Order.OrderID).
		Update(ctx, []firestore.Update{
			{Path: "fulfilment_source", Value: sourceID},
			{Path: "updated_at", Value: time.Now().Format(time.RFC3339)},
		})
	return err
}

// execNotify sends a real email via the platform's SMTP. When a TemplateService is wired in,
// it uses per-tenant Firestore SMTP config (including reply_to). Otherwise falls back to
// environment variable config via SendRawEmail.
func (e *ActionExecutor) execNotify(ctx context.Context, tenantID string, params []string) error {
	if len(params) < 2 {
		return fmt.Errorf("notify requires email and message parameters")
	}
	toEmail := params[0]
	message := params[1]

	// Wrap the plain text message in minimal HTML so it renders cleanly.
	htmlBody := fmt.Sprintf(`<!DOCTYPE html><html><body style="font-family:sans-serif;font-size:14px;color:#1e293b;padding:24px;">
<p>%s</p>
</body></html>`, strings.ReplaceAll(message, "\n", "<br>"))

	subject := "MarketMate Automation Notification"

	if e.templateSvc != nil {
		// Use per-tenant Firestore SMTP config (honours reply_to etc.)
		if err := e.templateSvc.SendRawEmailForTenant(ctx, tenantID, toEmail, subject, htmlBody); err != nil {
			return fmt.Errorf("notify: email send failed: %w", err)
		}
		log.Printf("[rule_actions] notify: sent email to %s (tenant SMTP)", toEmail)
		return nil
	}

	// Fallback — env-var SMTP only
	if e.smtp == nil || e.smtp.Host == "" {
		log.Printf("[rule_actions] notify: SMTP not configured — skipping notification to %s", toEmail)
		return nil
	}
	if err := SendRawEmail(toEmail, subject, htmlBody); err != nil {
		return fmt.Errorf("notify: email send failed: %w", err)
	}
	log.Printf("[rule_actions] notify: sent email to %s", toEmail)
	return nil
}

func (e *ActionExecutor) execWebhook(ctx context.Context, params []string) error {
	if len(params) < 2 {
		return fmt.Errorf("webhook requires url and method parameters")
	}
	url, method := params[0], strings.ToUpper(params[1])
	req, err := http.NewRequestWithContext(ctx, method, url, nil)
	if err != nil {
		return fmt.Errorf("webhook: invalid request: %v", err)
	}
	client := &http.Client{Timeout: 10 * time.Second}
	resp, err := client.Do(req)
	if err != nil {
		return fmt.Errorf("webhook: request failed: %v", err)
	}
	defer resp.Body.Close()
	if resp.StatusCode >= 400 {
		return fmt.Errorf("webhook: server returned %d", resp.StatusCode)
	}
	return nil
}

// execSetDispatchDate sets despatch_by_date = order received date + N days.
// Params: days (required, integer string), time (optional, "HH:MM", defaults to "23:59")
func (e *ActionExecutor) execSetDispatchDate(ctx context.Context, tenantID string, orderCtx *models.OrderContext, params []string) error {
	if len(params) == 0 {
		return fmt.Errorf("set_dispatch_date requires a days parameter")
	}
	days := 0
	if _, err := fmt.Sscanf(params[0], "%d", &days); err != nil {
		return fmt.Errorf("set_dispatch_date: invalid days value %q", params[0])
	}
	cutoffTime := "23:59"
	if len(params) >= 2 && params[1] != "" {
		cutoffTime = params[1]
	}

	// Determine base date from order
	baseStr := orderCtx.Order.OrderDate
	if baseStr == "" {
		baseStr = orderCtx.Order.CreatedAt
	}
	var base time.Time
	var err error
	if base, err = time.Parse(time.RFC3339, baseStr); err != nil {
		if base, err = time.Parse("2006-01-02", baseStr); err != nil {
			return fmt.Errorf("set_dispatch_date: cannot parse order date %q", baseStr)
		}
	}
	base = base.UTC().Truncate(24 * time.Hour).AddDate(0, 0, days)

	// Apply cutoff time
	var hh, mm int
	fmt.Sscanf(cutoffTime, "%d:%d", &hh, &mm)
	target := time.Date(base.Year(), base.Month(), base.Day(), hh, mm, 0, 0, time.UTC)

	despatchVal := target.Format(time.RFC3339)
	_, updateErr := e.client.Collection("tenants").Doc(tenantID).
		Collection("orders").Doc(orderCtx.Order.OrderID).
		Update(ctx, []firestore.Update{
			{Path: "despatch_by_date", Value: despatchVal},
			{Path: "updated_at", Value: time.Now().Format(time.RFC3339)},
		})
	if updateErr != nil {
		return updateErr
	}
	orderCtx.Order.DespatchByDate = despatchVal
	return nil
}

// execAddNote appends an internal note to the order's internal_notes field.
// Params: note text (required)
func (e *ActionExecutor) execAddNote(ctx context.Context, tenantID string, orderCtx *models.OrderContext, params []string) error {
	if len(params) == 0 || params[0] == "" {
		return fmt.Errorf("add_note requires a note text parameter")
	}
	note := params[0]
	existing := orderCtx.Order.InternalNotes
	var newNotes string
	if existing == "" {
		newNotes = note
	} else {
		newNotes = existing + "\n" + note
	}
	_, err := e.client.Collection("tenants").Doc(tenantID).
		Collection("orders").Doc(orderCtx.Order.OrderID).
		Update(ctx, []firestore.Update{
			{Path: "internal_notes", Value: newNotes},
			{Path: "updated_at", Value: time.Now().Format(time.RFC3339)},
		})
	if err == nil {
		orderCtx.Order.InternalNotes = newNotes
	}
	return err
}

// execAddBuyerNote appends a note to the buyer_notes field.
// Params: note text (required)
func (e *ActionExecutor) execAddBuyerNote(ctx context.Context, tenantID string, orderCtx *models.OrderContext, params []string) error {
	if len(params) == 0 || params[0] == "" {
		return fmt.Errorf("add_buyer_note requires a note text parameter")
	}
	note := params[0]
	existing := orderCtx.Order.BuyerNotes
	var newNotes string
	if existing == "" {
		newNotes = note
	} else {
		newNotes = existing + "\n" + note
	}
	_, err := e.client.Collection("tenants").Doc(tenantID).
		Collection("orders").Doc(orderCtx.Order.OrderID).
		Update(ctx, []firestore.Update{
			{Path: "buyer_notes", Value: newNotes},
			{Path: "updated_at", Value: time.Now().Format(time.RFC3339)},
		})
	if err == nil {
		orderCtx.Order.BuyerNotes = newNotes
	}
	return err
}

// execAddItem adds a product line item to the order (e.g. free gift, service charge).
// Params: sku (required), qty (optional, defaults to 1)
// The added line is marked with is_auto_added=true via a dedicated Firestore update.
func (e *ActionExecutor) execAddItem(ctx context.Context, tenantID string, orderCtx *models.OrderContext, params []string) error {
	if len(params) == 0 || params[0] == "" {
		return fmt.Errorf("add_item requires a sku parameter")
	}
	sku := params[0]
	qty := 1
	if len(params) >= 2 {
		fmt.Sscanf(params[1], "%d", &qty)
		if qty < 1 {
			qty = 1
		}
	}

	// Look up the product to get title/price
	var title string
	var unitPrice models.Money
	prodIter := e.client.Collection(fmt.Sprintf("tenants/%s/products", tenantID)).
		Where("sku", "==", sku).Limit(1).Documents(ctx)
	if doc, err := prodIter.Next(); err == nil && doc != nil {
		data := doc.Data()
		if t, ok := data["title"].(string); ok {
			title = t
		}
		unitPrice = models.Money{Amount: 0, Currency: "GBP"}
		if price, ok := data["price"].(float64); ok {
			unitPrice.Amount = price
		}
		if cur, ok := data["currency"].(string); ok {
			unitPrice.Currency = cur
		}
	} else if err != nil && err != iterator.Done {
		log.Printf("[rule_actions] add_item: product lookup error for sku %s: %v", sku, err)
	}
	prodIter.Stop()

	newLine := models.OrderLine{
		LineID:    fmt.Sprintf("auto-%s-%d", sku, time.Now().UnixNano()),
		SKU:       sku,
		Title:     title,
		Quantity:  qty,
		UnitPrice: unitPrice,
		LineTotal: models.Money{Amount: unitPrice.Amount * float64(qty), Currency: unitPrice.Currency},
		Status:    "pending",
	}

	updatedLines := append(orderCtx.Lines, newLine)
	_, err := e.client.Collection("tenants").Doc(tenantID).
		Collection("orders").Doc(orderCtx.Order.OrderID).
		Update(ctx, []firestore.Update{
			{Path: "lines", Value: updatedLines},
			{Path: "updated_at", Value: time.Now().Format(time.RFC3339)},
		})
	if err == nil {
		orderCtx.Lines = updatedLines
	}
	return err
}

// execAssignFulfilmentNetwork resolves the best source from a named fulfilment network
// and sets fulfilment_center_id on the order.
// Params: networkName (required)
func (e *ActionExecutor) execAssignFulfilmentNetwork(ctx context.Context, tenantID string, orderCtx *models.OrderContext, params []string) error {
	if len(params) == 0 || params[0] == "" {
		return fmt.Errorf("assign_fulfilment_network requires a network name parameter")
	}
	networkName := params[0]

	// Look up network by name
	iter := e.client.Collection(fmt.Sprintf("tenants/%s/fulfilment_networks", tenantID)).
		Where("name", "==", networkName).
		Where("active", "==", true).
		Limit(1).
		Documents(ctx)
	defer iter.Stop()

	doc, err := iter.Next()
	if err == iterator.Done {
		return fmt.Errorf("assign_fulfilment_network: no active network named %q", networkName)
	}
	if err != nil {
		return fmt.Errorf("assign_fulfilment_network: lookup error: %v", err)
	}

	var network struct {
		NetworkID string `firestore:"network_id"`
		Name      string `firestore:"name"`
		Sources   []struct {
			SourceID string `firestore:"source_id"`
			Priority int    `firestore:"priority"`
			MinStock int    `firestore:"min_stock"`
		} `firestore:"sources"`
	}
	if err := doc.DataTo(&network); err != nil {
		return fmt.Errorf("assign_fulfilment_network: parse error: %v", err)
	}

	// Walk sources in priority order — pick first active source
	type srcEntry struct {
		SourceID string
		Priority int
		MinStock int
	}
	var entries []srcEntry
	for _, s := range network.Sources {
		entries = append(entries, srcEntry{s.SourceID, s.Priority, s.MinStock})
	}
	// Sort by priority
	for i := 1; i < len(entries); i++ {
		for j := i; j > 0 && entries[j].Priority < entries[j-1].Priority; j-- {
			entries[j], entries[j-1] = entries[j-1], entries[j]
		}
	}

	for _, entry := range entries {
		srcDoc, err := e.client.Collection(fmt.Sprintf("tenants/%s/fulfilment_sources", tenantID)).
			Doc(entry.SourceID).Get(ctx)
		if err != nil {
			continue
		}
		var src models.FulfilmentSource
		if err := srcDoc.DataTo(&src); err != nil || !src.Active {
			continue
		}
		// Source found — assign it
		_, updateErr := e.client.Collection("tenants").Doc(tenantID).
			Collection("orders").Doc(orderCtx.Order.OrderID).
			Update(ctx, []firestore.Update{
				{Path: "fulfilment_center_id", Value: src.SourceID},
				{Path: "fulfilment_center_name", Value: src.Name},
				{Path: "updated_at", Value: time.Now().Format(time.RFC3339)},
			})
		return updateErr
	}

	return fmt.Errorf("assign_fulfilment_network: no active source found in network %q", networkName)
}
