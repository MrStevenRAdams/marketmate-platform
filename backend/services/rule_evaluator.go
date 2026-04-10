package services

import (
	"fmt"
	"regexp"
	"strings"
	"time"

	"module-a/models"
)

// ============================================================================
// RULE EVALUATOR
// ============================================================================

// RuleEvaluator walks an AST and evaluates conditions against an OrderContext
type RuleEvaluator struct{}

func NewRuleEvaluator() *RuleEvaluator {
	return &RuleEvaluator{}
}

// EvaluateRule evaluates a single RuleBlock against an OrderContext.
// Returns matched flag, a slice of condition traces, and any error.
func (e *RuleEvaluator) EvaluateRule(block models.RuleBlock, ctx *models.OrderContext) (bool, []models.ConditionTrace, error) {
	var traces []models.ConditionTrace
	matched, err := e.evalCondition(block.Condition, ctx, &traces)
	return matched, traces, err
}

func (e *RuleEvaluator) evalCondition(node models.ConditionNode, ctx *models.OrderContext, traces *[]models.ConditionTrace) (bool, error) {
	switch node.Type {
	case models.NodeAnd:
		leftResult, err := e.evalCondition(*node.Left, ctx, traces)
		if err != nil {
			return false, err
		}
		rightResult, err := e.evalCondition(*node.Right, ctx, traces)
		if err != nil {
			return false, err
		}
		return leftResult && rightResult, nil

	case models.NodeOr:
		leftResult, err := e.evalCondition(*node.Left, ctx, traces)
		if err != nil {
			return false, err
		}
		rightResult, err := e.evalCondition(*node.Right, ctx, traces)
		if err != nil {
			return false, err
		}
		return leftResult || rightResult, nil

	case models.NodeCondition:
		if node.Expr == nil {
			return false, fmt.Errorf("nil expression node")
		}
		result, actualVal, err := e.evalExpr(node.Expr, ctx)
		if err != nil {
			return false, err
		}
		trace := models.ConditionTrace{
			Expression: exprString(node.Expr),
			Result:     result,
			Value:      actualVal,
		}
		*traces = append(*traces, trace)
		return result, nil

	default:
		return false, fmt.Errorf("unknown condition node type: %s", node.Type)
	}
}

func (e *RuleEvaluator) evalExpr(expr *models.ExprNode, ctx *models.OrderContext) (bool, interface{}, error) {
	if expr.Operator == "FUNC" {
		return e.evalFunc(expr, ctx)
	}

	fieldVal, err := resolveField(expr.Field, ctx)
	if err != nil {
		return false, nil, err
	}

	return applyOperator(expr.Operator, fieldVal, expr.Value)
}

func (e *RuleEvaluator) evalFunc(expr *models.ExprNode, ctx *models.OrderContext) (bool, interface{}, error) {
	switch expr.Field {
	case "order.has_tag":
		for _, tag := range ctx.Tags {
			if strings.EqualFold(tag, expr.Value) {
				return true, ctx.Tags, nil
			}
		}
		return false, ctx.Tags, nil

	case "order.sku_in_order":
		for _, sku := range ctx.AllSKUs {
			if strings.EqualFold(sku, expr.Value) {
				return true, ctx.AllSKUs, nil
			}
		}
		return false, ctx.AllSKUs, nil

	default:
		return false, nil, fmt.Errorf("unknown function: %s", expr.Field)
	}
}

// resolveField extracts the field value from an OrderContext
func resolveField(field string, ctx *models.OrderContext) (interface{}, error) {
	switch field {
	case "order.channel":
		return ctx.Channel, nil
	case "order.total_gbp":
		return ctx.TotalGBP, nil
	case "order.weight_grams":
		return ctx.WeightGrams, nil
	case "order.item_count":
		return float64(ctx.ItemCount), nil
	case "order.shipping_country":
		return ctx.ShippingCountry, nil
	case "order.shipping_postcode":
		return ctx.ShippingPostcode, nil
	case "order.shipping_city":
		return ctx.ShippingCity, nil
	case "order.status":
		return ctx.Status, nil
	case "order.payment_method":
		return ctx.PaymentMethod, nil
	case "order.payment_status":
		return ctx.PaymentStatus, nil
	case "order.tags":
		return ctx.Tags, nil
	case "order.customer_email":
		return ctx.CustomerEmail, nil
	case "order.placed_hour":
		return float64(ctx.PlacedHour), nil
	case "order.placed_date":
		return ctx.PlacedDate, nil
	case "line.sku":
		return ctx.LineSKU, nil
	case "line.quantity":
		return float64(ctx.LineQuantity), nil
	case "line.title":
		return ctx.LineTitle, nil
	default:
		return nil, fmt.Errorf("unknown field: %q", field)
	}
}

// applyOperator compares actualVal against rawRHS using the operator
func applyOperator(op string, actual interface{}, rawRHS string) (bool, interface{}, error) {
	switch op {
	case "==":
		return compareEq(actual, rawRHS), actual, nil
	case "!=":
		return !compareEq(actual, rawRHS), actual, nil
	case ">":
		return compareNum(actual, rawRHS, ">")
	case ">=":
		return compareNum(actual, rawRHS, ">=")
	case "<":
		return compareNum(actual, rawRHS, "<")
	case "<=":
		return compareNum(actual, rawRHS, "<=")
	case "IN":
		result, err := compareIn(actual, rawRHS)
		return result, actual, err
	case "NOT IN":
		result, err := compareIn(actual, rawRHS)
		return !result, actual, err
	case "MATCHES":
		result, err := compareRegex(actual, rawRHS)
		return result, actual, err
	case "NOT MATCHES":
		result, err := compareRegex(actual, rawRHS)
		return !result, actual, err
	default:
		return false, actual, fmt.Errorf("unknown operator: %q", op)
	}
}

func compareEq(actual interface{}, rawRHS string) bool {
	rhs := stripQuotes(rawRHS)
	switch v := actual.(type) {
	case string:
		return strings.EqualFold(v, rhs)
	case float64:
		rhsF, err := ParseNumber(rhs)
		if err != nil {
			return false
		}
		return v == rhsF
	case bool:
		return fmt.Sprintf("%v", v) == rhs
	}
	return false
}

func compareNum(actual interface{}, rawRHS, op string) (bool, interface{}, error) {
	var lhs float64
	switch v := actual.(type) {
	case float64:
		lhs = v
	case int:
		lhs = float64(v)
	default:
		return false, actual, fmt.Errorf("cannot apply numeric operator to %T", actual)
	}
	rhs, err := ParseNumber(rawRHS)
	if err != nil {
		return false, actual, fmt.Errorf("invalid number %q: %v", rawRHS, err)
	}
	var result bool
	switch op {
	case ">":
		result = lhs > rhs
	case ">=":
		result = lhs >= rhs
	case "<":
		result = lhs < rhs
	case "<=":
		result = lhs <= rhs
	}
	return result, actual, nil
}

func compareIn(actual interface{}, rawRHS string) (bool, error) {
	items := parseArrayLiteral(rawRHS)
	switch v := actual.(type) {
	case string:
		for _, item := range items {
			if strings.EqualFold(v, item) {
				return true, nil
			}
		}
		return false, nil
	case []string:
		for _, av := range v {
			for _, item := range items {
				if strings.EqualFold(av, item) {
					return true, nil
				}
			}
		}
		return false, nil
	default:
		return false, fmt.Errorf("IN operator not supported for %T", actual)
	}
}

func compareRegex(actual interface{}, rawRHS string) (bool, error) {
	pattern := stripQuotes(rawRHS)
	re, err := regexp.Compile(pattern)
	if err != nil {
		return false, fmt.Errorf("invalid regex %q: %v", pattern, err)
	}
	switch v := actual.(type) {
	case string:
		return re.MatchString(v), nil
	default:
		return false, fmt.Errorf("MATCHES operator requires string field, got %T", actual)
	}
}

// ============================================================================
// HELPERS
// ============================================================================

func stripQuotes(s string) string {
	return strings.Trim(s, `"`)
}

func parseArrayLiteral(raw string) []string {
	raw = strings.TrimSpace(raw)
	raw = strings.TrimPrefix(raw, "[")
	raw = strings.TrimSuffix(raw, "]")
	if raw == "" {
		return nil
	}
	parts := strings.Split(raw, ",")
	var result []string
	for _, p := range parts {
		result = append(result, stripQuotes(strings.TrimSpace(p)))
	}
	return result
}

func exprString(expr *models.ExprNode) string {
	if expr.Operator == "FUNC" {
		return fmt.Sprintf(`%s("%s")`, expr.Field, expr.Value)
	}
	return fmt.Sprintf("%s %s %s", expr.Field, expr.Operator, expr.Value)
}

// BuildOrderContext derives an OrderContext from an Order + its lines.
//
// NOTE — order.weight_grams:
// Weight is not yet stored on OrderLine. Until you add a WeightGrams field
// to models.OrderLine and populate it during channel imports, this field
// will always be 0 and any rule using order.weight_grams will never match.
// To fix: add `WeightGrams float64` to OrderLine, populate it in each
// channel import handler, then replace the placeholder below.
func BuildOrderContext(order *models.Order, lines []models.OrderLine) *models.OrderContext {
	ctx := &models.OrderContext{
		Channel:          order.Channel,
		TotalGBP:         order.Totals.GrandTotal.Amount, // TODO: convert if currency != GBP
		ShippingCostGBP:  order.Totals.Shipping.Amount,
		ShippingCountry:  order.ShippingAddress.Country,
		ShippingPostcode: order.ShippingAddress.PostalCode,
		ShippingCity:     order.ShippingAddress.City,
		Status:           order.Status,
		PaymentMethod:    order.PaymentMethod,
		PaymentStatus:    order.PaymentStatus,
		Tags:             order.Tags,
		CustomerEmail:    order.Customer.Email,
		ItemCount:        len(lines),
		Order:            order,
		Lines:            lines,
	}

	// Weight placeholder — see NOTE above
	var totalWeight float64
	for i, l := range lines {
		ctx.AllSKUs = append(ctx.AllSKUs, l.SKU)
		// totalWeight += l.WeightGrams  ← uncomment once field exists on OrderLine
		_ = totalWeight
		if i == 0 {
			ctx.LineSKU = l.SKU
			ctx.LineQuantity = l.Quantity
			ctx.LineTitle = l.Title
		}
	}
	ctx.WeightGrams = totalWeight

	// Fix 2B: populate placed_hour and placed_date from order date
	dateStr := order.OrderDate
	if dateStr == "" {
		dateStr = order.CreatedAt
	}
	if t, err := time.Parse(time.RFC3339, dateStr); err == nil {
		ctx.PlacedHour = t.UTC().Hour()
		ctx.PlacedDate = t.UTC().Format("2006-01-02")
	} else if t, err := time.Parse("2006-01-02", dateStr); err == nil {
		ctx.PlacedHour = 0
		ctx.PlacedDate = t.Format("2006-01-02")
	}

	return ctx
}
