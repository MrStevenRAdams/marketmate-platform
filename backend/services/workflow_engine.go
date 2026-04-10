package services

import (
	"context"
	"fmt"
	"log"
	"math"
	"strings"
	"time"

	"cloud.google.com/go/firestore"
	"github.com/google/uuid"
	"google.golang.org/api/iterator"

	"module-a/models"
	"module-a/repository"
)

// ============================================================================
// WORKFLOW ENGINE
// ============================================================================

type WorkflowEngine struct {
	repo *repository.FirestoreRepository
}

func NewWorkflowEngine(repo *repository.FirestoreRepository) *WorkflowEngine {
	return &WorkflowEngine{repo: repo}
}

// ProcessOrder is the main entry point. Called after an order is saved.
// Loads all active workflows, evaluates each against the order in priority order,
// executes the first matching workflow's actions, and records the execution.
func (e *WorkflowEngine) ProcessOrder(ctx context.Context, tenantID, orderID string) (*WorkflowEngineResult, error) {
	start := time.Now()

	// Load the order
	order, err := e.getOrder(ctx, tenantID, orderID)
	if err != nil {
		return nil, fmt.Errorf("failed to load order %s: %w", orderID, err)
	}

	// Load order lines (needed for SKU, weight, item count conditions)
	lines, err := e.getOrderLines(ctx, tenantID, orderID)
	if err != nil {
		log.Printf("[workflow] Warning: could not load order lines for %s: %v", orderID, err)
		lines = []models.OrderLine{} // Continue with empty lines rather than abort
	}

	// Load active workflows, sorted by priority descending
	workflows, err := e.loadActiveWorkflows(ctx, tenantID, models.TriggerTypeOrderImported)
	if err != nil {
		return nil, fmt.Errorf("failed to load workflows: %w", err)
	}

	execution := &models.WorkflowExecution{
		ExecutionID:     uuid.New().String(),
		TenantID:        tenantID,
		OrderID:         orderID,
		TriggerType:     models.TriggerTypeOrderImported,
		WorkflowResults: []models.WorkflowEvalResult{},
		Status:          models.ExecutionStatusNoMatch,
		TriggeredAt:     start,
	}

	var matchedWorkflow *models.Workflow
	var matchedResult *WorkflowEngineResult

	// Evaluate each workflow in priority order
	for i := range workflows {
		wf := &workflows[i]

		evalResult, matched := e.evaluateWorkflow(wf, order, lines)
		execution.WorkflowResults = append(execution.WorkflowResults, evalResult)

		if matched {
			matchedWorkflow = wf
			execution.MatchedWorkflowID = wf.WorkflowID
			execution.MatchedWorkflowName = wf.Name
			break // First match wins
		}
	}

	// Execute actions if we found a match
	if matchedWorkflow != nil {
		if matchedWorkflow.Settings.TestMode {
			execution.Status = models.ExecutionStatusMatchedTestMode
			matchedResult = &WorkflowEngineResult{
				Matched:      true,
				TestMode:     true,
				WorkflowID:   matchedWorkflow.WorkflowID,
				WorkflowName: matchedWorkflow.Name,
			}
		} else {
			actionResults, execResult, execErr := e.executeActions(ctx, tenantID, matchedWorkflow, order, lines)
			execution.ActionResults = actionResults

			if execErr != nil {
				execution.Status = models.ExecutionStatusFailed
				execution.Error = execErr.Error()
				log.Printf("[workflow] Execution failed for order %s workflow %s: %v", orderID, matchedWorkflow.WorkflowID, execErr)
			} else {
				execution.Status = models.ExecutionStatusMatchedExecuted
			}
			matchedResult = execResult
		}

		// Update workflow stats
		e.updateWorkflowStats(ctx, tenantID, matchedWorkflow.WorkflowID, execution.Status)
	}

	// Finalise execution record
	now := time.Now()
	execution.CompletedAt = &now
	execution.DurationMs = time.Since(start).Milliseconds()

	// Save execution record (non-fatal if it fails)
	if err := e.saveExecution(ctx, execution); err != nil {
		log.Printf("[workflow] Warning: failed to save execution record for order %s: %v", orderID, err)
	}

	if matchedResult == nil {
		matchedResult = &WorkflowEngineResult{
			Matched:     false,
			ExecutionID: execution.ExecutionID,
		}
	}
	matchedResult.ExecutionID = execution.ExecutionID
	matchedResult.DurationMs = execution.DurationMs

	return matchedResult, nil
}

// WorkflowEngineResult is returned to the caller after processing
type WorkflowEngineResult struct {
	Matched      bool
	TestMode     bool
	WorkflowID   string
	WorkflowName string
	ExecutionID  string
	DurationMs   int64
	// Fulfilment decisions made by actions
	FulfilmentSourceID string
	CarrierID          string
	ServiceCode        string
	Tags               []string
	OnHold             bool
	HoldReason         string
}

// ============================================================================
// WORKFLOW LOADING
// ============================================================================

func (e *WorkflowEngine) loadActiveWorkflows(ctx context.Context, tenantID, triggerType string) ([]models.Workflow, error) {
	client := e.repo.GetClient()

	iter := client.Collection("tenants").Doc(tenantID).Collection("workflows").
		Where("status", "==", models.WorkflowStatusActive).
		OrderBy("priority", firestore.Desc).
		Documents(ctx)
	defer iter.Stop()

	var workflows []models.Workflow
	for {
		doc, err := iter.Next()
		if err == iterator.Done {
			break
		}
		if err != nil {
			return nil, err
		}

		var wf models.Workflow
		if err := doc.DataTo(&wf); err != nil {
			log.Printf("[workflow] Failed to unmarshal workflow %s: %v", doc.Ref.ID, err)
			continue
		}

		// Filter by trigger type
		if wf.Trigger.Type == triggerType || wf.Trigger.Type == "" {
			workflows = append(workflows, wf)
		}
	}

	return workflows, nil
}

// ============================================================================
// CONDITION EVALUATION
// ============================================================================

func (e *WorkflowEngine) evaluateWorkflow(wf *models.Workflow, order *models.Order, lines []models.OrderLine) (models.WorkflowEvalResult, bool) {
	result := models.WorkflowEvalResult{
		WorkflowID:   wf.WorkflowID,
		WorkflowName: wf.Name,
		Priority:     wf.Priority,
		Conditions:   make([]models.ConditionEvalResult, 0, len(wf.Conditions)),
	}

	// Check trigger channel filter
	if len(wf.Trigger.Channels) > 0 {
		channelMatch := false
		for _, ch := range wf.Trigger.Channels {
			if strings.EqualFold(ch, order.Channel) {
				channelMatch = true
				break
			}
		}
		if !channelMatch {
			result.Matched = false
			return result, false
		}
	}

	// Evaluate each condition (AND logic — all must match)
	allMatch := true
	for _, cond := range wf.Conditions {
		condResult := e.evaluateCondition(cond, order, lines)
		result.Conditions = append(result.Conditions, condResult)

		if !condResult.Matched {
			allMatch = false
			// Continue evaluating remaining conditions so the UI can show
			// which ones passed and which failed — don't break early
		}
	}

	result.Matched = allMatch
	return result, allMatch
}

func (e *WorkflowEngine) evaluateCondition(cond models.WorkflowCondition, order *models.Order, lines []models.OrderLine) models.ConditionEvalResult {
	result := models.ConditionEvalResult{
		Type:  cond.Type,
		Label: cond.Label,
	}

	var matched bool
	var actualValue interface{}
	var reason string

	switch cond.Type {
	case models.ConditionTypeGeography:
		matched, actualValue, reason = evalGeography(cond, order)

	case models.ConditionTypeOrderValue:
		matched, actualValue, reason = evalOrderValue(cond, order)

	case models.ConditionTypeWeight:
		matched, actualValue, reason = evalWeight(cond, order, lines)

	case models.ConditionTypeItemCount:
		matched, actualValue, reason = evalItemCount(cond, order, lines)

	case models.ConditionTypeSKU:
		matched, actualValue, reason = evalSKU(cond, lines)

	case models.ConditionTypeChannel:
		matched, actualValue, reason = evalChannel(cond, order)

	case models.ConditionTypeTag:
		matched, actualValue, reason = evalTag(cond, order)

	case models.ConditionTypeFulfilmentType:
		matched, actualValue, reason = evalFulfilmentType(cond, lines)

	case models.ConditionTypeTime:
		matched, actualValue, reason = evalTime(cond, order)

	case models.ConditionTypePaymentStatus:
		matched = strings.EqualFold(order.PaymentStatus, cond.PaymentStatusValue)
		actualValue = order.PaymentStatus
		reason = fmt.Sprintf("payment_status is %q", order.PaymentStatus)

	case models.ConditionTypeCustomer:
		matched, actualValue, reason = evalCustomer(cond, order)

	default:
		matched = false
		reason = fmt.Sprintf("unknown condition type: %s", cond.Type)
	}

	// Apply negation
	if cond.Negate {
		matched = !matched
		if reason != "" {
			reason = "NOT(" + reason + ")"
		}
	}

	result.Matched = matched
	result.ActualValue = actualValue
	result.Reason = reason
	return result
}

// --- Individual condition evaluators ---

func evalGeography(cond models.WorkflowCondition, order *models.Order) (bool, interface{}, string) {
	var actual string
	switch cond.GeoField {
	case "country":
		actual = order.ShippingAddress.Country
	case "postcode_prefix":
		actual = order.ShippingAddress.PostalCode
	case "region", "state":
		actual = order.ShippingAddress.State
	case "city":
		actual = order.ShippingAddress.City
	default:
		actual = order.ShippingAddress.Country
	}

	actual = strings.TrimSpace(strings.ToUpper(actual))

	switch cond.GeoOperator {
	case "equals":
		return strings.EqualFold(actual, cond.GeoValue), actual,
			fmt.Sprintf("%s=%q matches %q", cond.GeoField, actual, cond.GeoValue)
	case "not_equals":
		return !strings.EqualFold(actual, cond.GeoValue), actual,
			fmt.Sprintf("%s=%q not equals %q", cond.GeoField, actual, cond.GeoValue)
	case "in":
		for _, v := range cond.GeoValues {
			if strings.EqualFold(actual, v) {
				return true, actual, fmt.Sprintf("%s=%q found in list", cond.GeoField, actual)
			}
		}
		return false, actual, fmt.Sprintf("%s=%q not in list", cond.GeoField, actual)
	case "not_in":
		for _, v := range cond.GeoValues {
			if strings.EqualFold(actual, v) {
				return false, actual, fmt.Sprintf("%s=%q is in exclusion list", cond.GeoField, actual)
			}
		}
		return true, actual, fmt.Sprintf("%s=%q not in exclusion list", cond.GeoField, actual)
	case "starts_with":
		return strings.HasPrefix(actual, strings.ToUpper(cond.GeoValue)), actual,
			fmt.Sprintf("%s=%q starts with %q", cond.GeoField, actual, cond.GeoValue)
	}
	return false, actual, "unknown geography operator"
}

func evalOrderValue(cond models.WorkflowCondition, order *models.Order) (bool, interface{}, string) {
	var amount float64
	switch cond.ValueField {
	case "subtotal":
		amount = order.Totals.Subtotal.Amount
	case "shipping_paid":
		amount = order.Totals.Shipping.Amount
	default: // grand_total
		amount = order.Totals.GrandTotal.Amount
	}

	switch cond.ValueOperator {
	case "gt":
		return amount > cond.ValueAmount, amount, fmt.Sprintf("%.2f > %.2f", amount, cond.ValueAmount)
	case "gte":
		return amount >= cond.ValueAmount, amount, fmt.Sprintf("%.2f >= %.2f", amount, cond.ValueAmount)
	case "lt":
		return amount < cond.ValueAmount, amount, fmt.Sprintf("%.2f < %.2f", amount, cond.ValueAmount)
	case "lte":
		return amount <= cond.ValueAmount, amount, fmt.Sprintf("%.2f <= %.2f", amount, cond.ValueAmount)
	case "eq":
		return math.Abs(amount-cond.ValueAmount) < 0.001, amount, fmt.Sprintf("%.2f == %.2f", amount, cond.ValueAmount)
	case "between":
		return amount >= cond.ValueMin && amount <= cond.ValueMax, amount,
			fmt.Sprintf("%.2f between %.2f-%.2f", amount, cond.ValueMin, cond.ValueMax)
	}
	return false, amount, "unknown value operator"
}

func evalWeight(cond models.WorkflowCondition, order *models.Order, lines []models.OrderLine) (bool, interface{}, string) {
	// Calculate total weight from order lines
	// Note: weight comes from product records, which may not be on the order line itself.
	// For now we sum line quantities as a proxy — real weight lookup happens via product service.
	// TODO: join with product records for actual weight.
	totalWeight := 0.0
	for _, line := range lines {
		// Use quantity as placeholder — real implementation joins product
		totalWeight += float64(line.Quantity) * 0.5 // 500g default per unit
	}

	switch cond.WeightOperator {
	case "gt":
		return totalWeight > cond.WeightValue, totalWeight, fmt.Sprintf("%.2fkg > %.2fkg", totalWeight, cond.WeightValue)
	case "gte":
		return totalWeight >= cond.WeightValue, totalWeight, fmt.Sprintf("%.2fkg >= %.2fkg", totalWeight, cond.WeightValue)
	case "lt":
		return totalWeight < cond.WeightValue, totalWeight, fmt.Sprintf("%.2fkg < %.2fkg", totalWeight, cond.WeightValue)
	case "lte":
		return totalWeight <= cond.WeightValue, totalWeight, fmt.Sprintf("%.2fkg <= %.2fkg", totalWeight, cond.WeightValue)
	case "between":
		return totalWeight >= cond.WeightMin && totalWeight <= cond.WeightMax, totalWeight,
			fmt.Sprintf("%.2fkg between %.2f-%.2fkg", totalWeight, cond.WeightMin, cond.WeightMax)
	}
	return false, totalWeight, "unknown weight operator"
}

func evalItemCount(cond models.WorkflowCondition, order *models.Order, lines []models.OrderLine) (bool, interface{}, string) {
	var count int
	switch cond.ItemCountField {
	case "distinct_skus":
		seen := map[string]bool{}
		for _, l := range lines {
			seen[l.SKU] = true
		}
		count = len(seen)
	case "line_count":
		count = len(lines)
	default: // total_quantity
		for _, l := range lines {
			count += l.Quantity
		}
	}

	switch cond.ItemCountOperator {
	case "gt":
		return count > cond.ItemCountValue, count, fmt.Sprintf("%d > %d", count, cond.ItemCountValue)
	case "gte":
		return count >= cond.ItemCountValue, count, fmt.Sprintf("%d >= %d", count, cond.ItemCountValue)
	case "lt":
		return count < cond.ItemCountValue, count, fmt.Sprintf("%d < %d", count, cond.ItemCountValue)
	case "lte":
		return count <= cond.ItemCountValue, count, fmt.Sprintf("%d <= %d", count, cond.ItemCountValue)
	case "eq":
		return count == cond.ItemCountValue, count, fmt.Sprintf("%d == %d", count, cond.ItemCountValue)
	case "between":
		return count >= cond.ItemCountMin && count <= cond.ItemCountMax, count,
			fmt.Sprintf("%d between %d-%d", count, cond.ItemCountMin, cond.ItemCountMax)
	}
	return false, count, "unknown item_count operator"
}

func evalSKU(cond models.WorkflowCondition, lines []models.OrderLine) (bool, interface{}, string) {
	matchLine := func(sku string) bool {
		switch cond.SKUOperator {
		case "equals":
			return strings.EqualFold(sku, cond.SKUValue)
		case "starts_with":
			return strings.HasPrefix(strings.ToUpper(sku), strings.ToUpper(cond.SKUValue))
		case "contains":
			return strings.Contains(strings.ToUpper(sku), strings.ToUpper(cond.SKUValue))
		case "in":
			for _, v := range cond.SKUValues {
				if strings.EqualFold(sku, v) {
					return true
				}
			}
			return false
		case "not_in":
			for _, v := range cond.SKUValues {
				if strings.EqualFold(sku, v) {
					return false
				}
			}
			return true
		}
		return false
	}

	skus := make([]string, 0, len(lines))
	for _, l := range lines {
		skus = append(skus, l.SKU)
	}

	scope := cond.SKUScope
	if scope == "" {
		scope = "any_line"
	}

	switch scope {
	case "all_lines":
		for _, l := range lines {
			if !matchLine(l.SKU) {
				return false, skus, "not all lines matched SKU condition"
			}
		}
		return true, skus, "all lines matched SKU condition"
	default: // any_line
		for _, l := range lines {
			if matchLine(l.SKU) {
				return true, skus, fmt.Sprintf("SKU %q matched condition", l.SKU)
			}
		}
		return false, skus, "no lines matched SKU condition"
	}
}

func evalChannel(cond models.WorkflowCondition, order *models.Order) (bool, interface{}, string) {
	actual := strings.ToLower(order.Channel)
	switch cond.ChannelOperator {
	case "equals":
		return strings.EqualFold(actual, cond.ChannelValue), actual,
			fmt.Sprintf("channel=%q", actual)
	case "in":
		for _, v := range cond.ChannelValues {
			if strings.EqualFold(actual, v) {
				return true, actual, fmt.Sprintf("channel=%q in list", actual)
			}
		}
		return false, actual, fmt.Sprintf("channel=%q not in list", actual)
	case "not_in":
		for _, v := range cond.ChannelValues {
			if strings.EqualFold(actual, v) {
				return false, actual, fmt.Sprintf("channel=%q in exclusion list", actual)
			}
		}
		return true, actual, fmt.Sprintf("channel=%q not excluded", actual)
	}
	return false, actual, "unknown channel operator"
}

func evalTag(cond models.WorkflowCondition, order *models.Order) (bool, interface{}, string) {
	tagSet := map[string]bool{}
	for _, t := range order.Tags {
		tagSet[strings.ToLower(t)] = true
	}

	switch cond.TagOperator {
	case "has":
		for _, v := range cond.TagValues {
			if tagSet[strings.ToLower(v)] {
				return true, order.Tags, fmt.Sprintf("has tag %q", v)
			}
		}
		return false, order.Tags, "tag not found"
	case "not_has":
		for _, v := range cond.TagValues {
			if tagSet[strings.ToLower(v)] {
				return false, order.Tags, fmt.Sprintf("has excluded tag %q", v)
			}
		}
		return true, order.Tags, "none of the excluded tags present"
	case "has_any":
		for _, v := range cond.TagValues {
			if tagSet[strings.ToLower(v)] {
				return true, order.Tags, fmt.Sprintf("has tag %q", v)
			}
		}
		return false, order.Tags, "none of the required tags present"
	case "has_all":
		for _, v := range cond.TagValues {
			if !tagSet[strings.ToLower(v)] {
				return false, order.Tags, fmt.Sprintf("missing required tag %q", v)
			}
		}
		return true, order.Tags, "all required tags present"
	}
	return false, order.Tags, "unknown tag operator"
}

func evalFulfilmentType(cond models.WorkflowCondition, lines []models.OrderLine) (bool, interface{}, string) {
	types := make([]string, 0, len(lines))
	for _, l := range lines {
		types = append(types, l.FulfilmentType)
	}

	switch cond.FulfilmentTypeOperator {
	case "all_are":
		for _, l := range lines {
			if !strings.EqualFold(l.FulfilmentType, cond.FulfilmentTypeValue) {
				return false, types, fmt.Sprintf("line %s is %q not %q", l.LineID, l.FulfilmentType, cond.FulfilmentTypeValue)
			}
		}
		return true, types, "all lines are " + cond.FulfilmentTypeValue
	case "any_are":
		for _, l := range lines {
			if strings.EqualFold(l.FulfilmentType, cond.FulfilmentTypeValue) {
				return true, types, fmt.Sprintf("line %s is %q", l.LineID, l.FulfilmentType)
			}
		}
		return false, types, "no lines are " + cond.FulfilmentTypeValue
	case "none_are":
		for _, l := range lines {
			if strings.EqualFold(l.FulfilmentType, cond.FulfilmentTypeValue) {
				return false, types, fmt.Sprintf("line %s is %q", l.LineID, l.FulfilmentType)
			}
		}
		return true, types, "no lines are " + cond.FulfilmentTypeValue
	}
	return false, types, "unknown fulfilment_type operator"
}

func evalTime(cond models.WorkflowCondition, order *models.Order) (bool, interface{}, string) {
	tz := cond.TimeTimezone
	if tz == "" {
		tz = "Europe/London"
	}
	loc, err := time.LoadLocation(tz)
	if err != nil {
		loc = time.UTC
	}

	now := time.Now().In(loc)

	switch cond.TimeField {
	case "current_time":
		// Compare current wall-clock time
		switch cond.TimeOperator {
		case "before":
			cutoff, err := time.ParseInLocation("15:04", cond.TimeValue, loc)
			if err != nil {
				return false, now.Format("15:04"), "invalid time format"
			}
			cutoffToday := time.Date(now.Year(), now.Month(), now.Day(), cutoff.Hour(), cutoff.Minute(), 0, 0, loc)
			return now.Before(cutoffToday), now.Format("15:04"),
				fmt.Sprintf("current time %s before cutoff %s", now.Format("15:04"), cond.TimeValue)
		case "after":
			cutoff, err := time.ParseInLocation("15:04", cond.TimeValue, loc)
			if err != nil {
				return false, now.Format("15:04"), "invalid time format"
			}
			cutoffToday := time.Date(now.Year(), now.Month(), now.Day(), cutoff.Hour(), cutoff.Minute(), 0, 0, loc)
			return now.After(cutoffToday), now.Format("15:04"),
				fmt.Sprintf("current time %s after cutoff %s", now.Format("15:04"), cond.TimeValue)
		}
	}

	return false, now.Format(time.RFC3339), "unsupported time condition"
}

func evalCustomer(cond models.WorkflowCondition, order *models.Order) (bool, interface{}, string) {
	var actual string
	switch cond.CustomerField {
	case "name":
		actual = order.Customer.Name
	default: // email
		actual = order.Customer.Email
	}

	switch cond.CustomerOperator {
	case "equals":
		return strings.EqualFold(actual, cond.CustomerValue), actual,
			fmt.Sprintf("customer.%s=%q", cond.CustomerField, actual)
	case "contains":
		return strings.Contains(strings.ToLower(actual), strings.ToLower(cond.CustomerValue)), actual,
			fmt.Sprintf("customer.%s contains %q", cond.CustomerField, cond.CustomerValue)
	}
	return false, actual, "unknown customer operator"
}

// ============================================================================
// ACTION EXECUTION
// ============================================================================

func (e *WorkflowEngine) executeActions(ctx context.Context, tenantID string, wf *models.Workflow, order *models.Order, lines []models.OrderLine) ([]models.ActionExecResult, *WorkflowEngineResult, error) {
	results := make([]models.ActionExecResult, 0, len(wf.Actions))
	engineResult := &WorkflowEngineResult{
		Matched:      true,
		WorkflowID:   wf.WorkflowID,
		WorkflowName: wf.Name,
	}

	for _, action := range wf.Actions {
		start := time.Now()
		actionResult := models.ActionExecResult{
			Type:  action.Type,
			Label: action.Label,
		}

		var execErr error
		switch action.Type {

		case models.ActionTypeAssignFulfilmentSource:
			execErr = e.actionAssignFulfilmentSource(ctx, tenantID, order, action, engineResult)

		case models.ActionTypeAssignCarrier:
			execErr = e.actionAssignCarrier(ctx, tenantID, order, action, engineResult)

		case models.ActionTypeAssignCarrierCheapest, models.ActionTypeAssignCarrierFastest:
			// These need rate shopping — stub for now, will be wired to carrier service
			actionResult.Result = fmt.Sprintf("%s: rate shopping not yet implemented, skipping", action.Type)
			actionResult.Success = true

		case models.ActionTypeSplitByFulfilmentType:
			execErr = e.actionSplitByFulfilmentType(ctx, tenantID, order, lines)

		case models.ActionTypeSetPriority:
			execErr = e.actionSetPriority(ctx, tenantID, order.OrderID, action.Priority)

		case models.ActionTypeAddTag:
			execErr = e.actionAddTag(ctx, tenantID, order, action.TagValue)
			if execErr == nil {
				engineResult.Tags = append(engineResult.Tags, action.TagValue)
			}

		case models.ActionTypeRemoveTag:
			execErr = e.actionRemoveTag(ctx, tenantID, order, action.TagValue)

		case models.ActionTypeHoldOrder:
			execErr = e.actionHoldOrder(ctx, tenantID, order.OrderID, action.HoldReason)
			if execErr == nil {
				engineResult.OnHold = true
				engineResult.HoldReason = action.HoldReason
			}

		case models.ActionTypeRequireSignature:
			// Stored on the pending shipment record when it's created
			actionResult.Result = "signature requirement flagged for shipment"
			actionResult.Success = true

		case models.ActionTypeSaturdayDelivery:
			actionResult.Result = "Saturday delivery flagged for shipment"
			actionResult.Success = true

		case models.ActionTypeSetInsurance:
			actionResult.Result = fmt.Sprintf("insurance value set to %.2f", action.InsuranceValue)
			actionResult.Success = true

		case models.ActionTypeSendNotification:
			// Stub — real implementation sends email/webhook
			actionResult.Result = fmt.Sprintf("notification queued to %s", action.NotificationEmail)
			actionResult.Success = true

		case models.ActionTypeMergeCheck:
			// Stub — merge check logic is complex, deferred
			actionResult.Result = "merge check: not yet implemented"
			actionResult.Success = true

		case models.ActionTypeAssignToPickwave:
			// Find or create a draft pickwave matching the action's config
			client := e.repo.GetClient()
			oid := order.OrderID

			pwName := action.PickwaveName
			if pwName == "" {
				pwName = "Auto Wave {date}"
			}
			// Replace placeholders
			pwName = strings.Replace(pwName, "{date}", time.Now().Format("2006-01-02"), 1)

			grouping := action.PickwaveGrouping
			if grouping == "" { grouping = "single_order" }
			sortBy := action.PickwaveSortBy
			if sortBy == "" { sortBy = "sku" }

			// Look for an existing open (draft) pickwave that matches this config and isn't full
			pwCol := client.Collection("tenants").Doc(tenantID).Collection("pickwaves")
			candidateIter := pwCol.
				Where("status", "==", "draft").
				Where("grouping", "==", grouping).
				OrderBy("created_at", firestore.Desc).
				Limit(5).
				Documents(ctx)

			var targetPW *firestore.DocumentSnapshot
			for {
				doc, err := candidateIter.Next()
				if err != nil { break }
				data := doc.Data()
				// Check capacity constraints
				orderCount, _ := data["order_count"].(int64)
				itemCount, _ := data["item_count"].(int64)
				if action.PickwaveMaxOrders > 0 && int(orderCount) >= action.PickwaveMaxOrders { continue }
				if action.PickwaveMaxItems > 0 && int(itemCount) >= action.PickwaveMaxItems { continue }
				// Check this order isn't already in the wave
				existingIDs, _ := data["order_ids"].([]interface{})
				alreadyIn := false
				for _, eid := range existingIDs {
					if eid == oid { alreadyIn = true; break }
				}
				if alreadyIn { continue }
				targetPW = doc
				break
			}
			candidateIter.Stop()

			if targetPW != nil {
				// Add order to existing pickwave
				pwID := targetPW.Ref.ID
				// Build pick lines from order
				var newLines []map[string]interface{}
				for _, line := range lines {
					newLines = append(newLines, map[string]interface{}{
						"id": "pwl_" + uuid.New().String(), "pickwave_id": pwID,
						"order_id": oid, "sku": line.SKU, "product_name": line.Title,
						"quantity": line.Quantity, "binrack_name": "",
						"status": "pending", "picked_quantity": 0,
					})
				}
				_, updateErr := pwCol.Doc(pwID).Update(ctx, []firestore.Update{
					{Path: "order_ids", Value: firestore.ArrayUnion(oid)},
					{Path: "order_count", Value: firestore.Increment(1)},
					{Path: "item_count", Value: firestore.Increment(len(newLines))},
					{Path: "updated_at", Value: time.Now().UTC()},
				})
				if updateErr != nil {
					actionResult.Error = "failed to add to pickwave: " + updateErr.Error()
				} else {
					// Append lines to the pickwave's lines array
					for _, nl := range newLines {
						pwCol.Doc(pwID).Update(ctx, []firestore.Update{
							{Path: "lines", Value: firestore.ArrayUnion(nl)},
						})
					}
					actionResult.Result = fmt.Sprintf("added to existing pickwave %s", pwID)
					actionResult.Success = true
				}
			} else {
				// Create new pickwave
				pwID := "pw_" + uuid.New().String()
				var pwLines []map[string]interface{}
				for _, line := range lines {
					pwLines = append(pwLines, map[string]interface{}{
						"id": "pwl_" + uuid.New().String(), "pickwave_id": pwID,
						"order_id": oid, "sku": line.SKU, "product_name": line.Title,
						"quantity": line.Quantity, "binrack_name": "",
						"status": "pending", "picked_quantity": 0,
					})
				}
				now := time.Now().UTC()
				newWave := map[string]interface{}{
					"id": pwID, "tenant_id": tenantID, "name": pwName,
					"status": "draft", "type": "multi_sku", "grouping": grouping,
					"assigned_user_id": action.PickwaveAssignUser,
					"max_orders": action.PickwaveMaxOrders, "max_items": action.PickwaveMaxItems,
					"sort_by": sortBy, "show_next_only": false,
					"order_ids": []string{oid}, "order_count": 1,
					"item_count": len(pwLines), "lines": pwLines,
					"created_at": now, "updated_at": now,
				}
				_, createErr := pwCol.Doc(pwID).Set(ctx, newWave)
				if createErr != nil {
					actionResult.Error = "failed to create pickwave: " + createErr.Error()
				} else {
					actionResult.Result = fmt.Sprintf("created new pickwave %s (%s)", pwName, pwID)
					actionResult.Success = true
				}
			}

		default:
			actionResult.Result = fmt.Sprintf("unknown action type: %s", action.Type)
			actionResult.Success = false
		}

		actionResult.DurationMs = time.Since(start).Milliseconds()

		if execErr != nil {
			actionResult.Success = false
			actionResult.Error = execErr.Error()
			results = append(results, actionResult)

			if wf.Settings.StopOnError {
				return results, engineResult, execErr
			}
			// Otherwise log and continue
			log.Printf("[workflow] Action %s failed for order %s: %v (continuing)", action.Type, order.OrderID, execErr)
		} else {
			if actionResult.Result == "" {
				actionResult.Result = "success"
			}
			actionResult.Success = true
			results = append(results, actionResult)
		}
	}

	return results, engineResult, nil
}

// --- Individual action implementations ---

func (e *WorkflowEngine) actionAssignFulfilmentSource(ctx context.Context, tenantID string, order *models.Order, action models.WorkflowAction, result *WorkflowEngineResult) error {
	client := e.repo.GetClient()

	sourceID := action.FulfilmentSourceID

	if sourceID == "" && action.FulfilmentStrategy == "default" {
		// Find the default fulfilment source
		iter := client.Collection("tenants").Doc(tenantID).Collection("fulfilment_sources").
			Where("default", "==", true).
			Where("active", "==", true).
			Limit(1).Documents(ctx)
		defer iter.Stop()

		doc, err := iter.Next()
		if err != nil {
			return fmt.Errorf("no default fulfilment source configured")
		}
		sourceID = doc.Ref.ID
	}

	if sourceID == "" {
		return fmt.Errorf("no fulfilment source specified in action")
	}

	// Write to order
	_, err := client.Collection("tenants").Doc(tenantID).Collection("orders").Doc(order.OrderID).
		Update(ctx, []firestore.Update{
			{Path: "fulfilment_source", Value: sourceID},
			{Path: "warehouse_id", Value: sourceID},
			{Path: "status", Value: "processing"},
			{Path: "updated_at", Value: time.Now().Format(time.RFC3339)},
		})
	if err != nil {
		return fmt.Errorf("failed to assign fulfilment source: %w", err)
	}

	result.FulfilmentSourceID = sourceID
	return nil
}

func (e *WorkflowEngine) actionAssignCarrier(ctx context.Context, tenantID string, order *models.Order, action models.WorkflowAction, result *WorkflowEngineResult) error {
	client := e.repo.GetClient()

	if action.CarrierID == "" {
		return fmt.Errorf("assign_carrier action missing carrier_id")
	}

	_, err := client.Collection("tenants").Doc(tenantID).Collection("orders").Doc(order.OrderID).
		Update(ctx, []firestore.Update{
			{Path: "assigned_carrier_id", Value: action.CarrierID},
			{Path: "assigned_service_code", Value: action.ServiceCode},
			{Path: "updated_at", Value: time.Now().Format(time.RFC3339)},
		})
	if err != nil {
		return fmt.Errorf("failed to assign carrier: %w", err)
	}

	result.CarrierID = action.CarrierID
	result.ServiceCode = action.ServiceCode
	return nil
}

func (e *WorkflowEngine) actionSplitByFulfilmentType(ctx context.Context, tenantID string, order *models.Order, lines []models.OrderLine) error {
	// Group lines by fulfilment type
	groups := map[string][]models.OrderLine{}
	for _, l := range lines {
		ft := l.FulfilmentType
		if ft == "" {
			ft = "stock"
		}
		groups[ft] = append(groups[ft], l)
	}

	if len(groups) <= 1 {
		// All lines are the same type — no split needed
		return nil
	}

	// Create a shipment record per group
	client := e.repo.GetClient()
	for ft, groupLines := range groups {
		lineIDs := make([]string, 0, len(groupLines))
		for _, l := range groupLines {
			lineIDs = append(lineIDs, l.LineID)
		}

		shipmentID := uuid.New().String()
		orderLines := map[string][]string{
			order.OrderID: lineIDs,
		}

		shipment := map[string]interface{}{
			"shipment_id":            shipmentID,
			"tenant_id":              tenantID,
			"order_ids":              []string{order.OrderID},
			"order_lines":            orderLines,
			"fulfilment_source_type": ft,
			"status":                 models.ShipmentStatusPlanned,
			"to_address": map[string]interface{}{
				"name":          order.Customer.Name,
				"address_line1": order.ShippingAddress.AddressLine1,
				"address_line2": order.ShippingAddress.AddressLine2,
				"city":          order.ShippingAddress.City,
				"postal_code":   order.ShippingAddress.PostalCode,
				"country":       order.ShippingAddress.Country,
			},
			"created_at": time.Now(),
			"updated_at": time.Now(),
		}

		_, err := client.Collection("tenants").Doc(tenantID).Collection("shipments").Doc(shipmentID).Set(ctx, shipment)
		if err != nil {
			return fmt.Errorf("failed to create shipment record for %s lines: %w", ft, err)
		}

		log.Printf("[workflow] Created shipment %s for %d %s lines of order %s", shipmentID, len(lineIDs), ft, order.OrderID)
	}

	// Update order to reference shipments and mark as split
	_, err := client.Collection("tenants").Doc(tenantID).Collection("orders").Doc(order.OrderID).
		Update(ctx, []firestore.Update{
			{Path: "sub_status", Value: "split"},
			{Path: "updated_at", Value: time.Now().Format(time.RFC3339)},
		})
	return err
}

func (e *WorkflowEngine) actionSetPriority(ctx context.Context, tenantID, orderID, priority string) error {
	client := e.repo.GetClient()
	_, err := client.Collection("tenants").Doc(tenantID).Collection("orders").Doc(orderID).
		Update(ctx, []firestore.Update{
			{Path: "priority", Value: priority},
			{Path: "updated_at", Value: time.Now().Format(time.RFC3339)},
		})
	return err
}

func (e *WorkflowEngine) actionAddTag(ctx context.Context, tenantID string, order *models.Order, tag string) error {
	client := e.repo.GetClient()

	// Avoid duplicate tags
	for _, t := range order.Tags {
		if strings.EqualFold(t, tag) {
			return nil
		}
	}

	newTags := append(order.Tags, tag)
	_, err := client.Collection("tenants").Doc(tenantID).Collection("orders").Doc(order.OrderID).
		Update(ctx, []firestore.Update{
			{Path: "tags", Value: newTags},
			{Path: "updated_at", Value: time.Now().Format(time.RFC3339)},
		})
	return err
}

func (e *WorkflowEngine) actionRemoveTag(ctx context.Context, tenantID string, order *models.Order, tag string) error {
	client := e.repo.GetClient()

	newTags := make([]string, 0, len(order.Tags))
	for _, t := range order.Tags {
		if !strings.EqualFold(t, tag) {
			newTags = append(newTags, t)
		}
	}

	_, err := client.Collection("tenants").Doc(tenantID).Collection("orders").Doc(order.OrderID).
		Update(ctx, []firestore.Update{
			{Path: "tags", Value: newTags},
			{Path: "updated_at", Value: time.Now().Format(time.RFC3339)},
		})
	return err
}

func (e *WorkflowEngine) actionHoldOrder(ctx context.Context, tenantID, orderID, reason string) error {
	client := e.repo.GetClient()
	_, err := client.Collection("tenants").Doc(tenantID).Collection("orders").Doc(orderID).
		Update(ctx, []firestore.Update{
			{Path: "status", Value: "on_hold"},
			{Path: "sub_status", Value: reason},
			{Path: "updated_at", Value: time.Now().Format(time.RFC3339)},
		})
	return err
}

// ============================================================================
// STATS & PERSISTENCE
// ============================================================================

func (e *WorkflowEngine) updateWorkflowStats(ctx context.Context, tenantID, workflowID, execStatus string) {
	client := e.repo.GetClient()
	ref := client.Collection("tenants").Doc(tenantID).Collection("workflows").Doc(workflowID)

	updates := []firestore.Update{
		{Path: "stats.total_evaluated", Value: firestore.Increment(1)},
		{Path: "last_executed_at", Value: time.Now()},
		{Path: "updated_at", Value: time.Now()},
	}

	if execStatus == models.ExecutionStatusMatchedExecuted || execStatus == models.ExecutionStatusMatchedTestMode {
		updates = append(updates, firestore.Update{Path: "stats.total_matched", Value: firestore.Increment(1)})
	}
	if execStatus == models.ExecutionStatusMatchedExecuted {
		updates = append(updates, firestore.Update{Path: "stats.total_executed", Value: firestore.Increment(1)})
	}
	if execStatus == models.ExecutionStatusFailed {
		updates = append(updates, firestore.Update{Path: "stats.total_failed", Value: firestore.Increment(1)})
	}

	if _, err := ref.Update(ctx, updates); err != nil {
		log.Printf("[workflow] Failed to update stats for workflow %s: %v", workflowID, err)
	}
}

func (e *WorkflowEngine) saveExecution(ctx context.Context, exec *models.WorkflowExecution) error {
	client := e.repo.GetClient()
	_, err := client.Collection("tenants").Doc(exec.TenantID).
		Collection("workflow_executions").Doc(exec.ExecutionID).
		Set(ctx, exec)
	return err
}

// ============================================================================
// HELPERS
// ============================================================================

func (e *WorkflowEngine) getOrder(ctx context.Context, tenantID, orderID string) (*models.Order, error) {
	client := e.repo.GetClient()
	doc, err := client.Collection("tenants").Doc(tenantID).Collection("orders").Doc(orderID).Get(ctx)
	if err != nil {
		return nil, err
	}
	var order models.Order
	if err := doc.DataTo(&order); err != nil {
		return nil, err
	}
	return &order, nil
}

func (e *WorkflowEngine) getOrderLines(ctx context.Context, tenantID, orderID string) ([]models.OrderLine, error) {
	client := e.repo.GetClient()
	iter := client.Collection("tenants").Doc(tenantID).Collection("orders").Doc(orderID).Collection("lines").Documents(ctx)
	defer iter.Stop()

	var lines []models.OrderLine
	for {
		doc, err := iter.Next()
		if err == iterator.Done {
			break
		}
		if err != nil {
			return nil, err
		}
		var line models.OrderLine
		if err := doc.DataTo(&line); err != nil {
			continue
		}
		lines = append(lines, line)
	}
	return lines, nil
}
