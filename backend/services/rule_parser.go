package services

import (
	"fmt"
	"regexp"
	"strconv"
	"strings"

	"module-a/models"
)

// ============================================================================
// LEXER
// ============================================================================

type tokenKind string

const (
	tokWHEN    tokenKind = "WHEN"
	tokTHEN    tokenKind = "THEN"
	tokAND     tokenKind = "AND"
	tokOR      tokenKind = "OR"
	tokNOT     tokenKind = "NOT"
	tokIN      tokenKind = "IN"
	tokMATCHES tokenKind = "MATCHES"
	tokIF      tokenKind = "IF"
	tokEQ      tokenKind = "=="
	tokNEQ     tokenKind = "!="
	tokGTE     tokenKind = ">="
	tokLTE     tokenKind = "<="
	tokGT      tokenKind = ">"
	tokLT      tokenKind = "<"
	tokLBRACK  tokenKind = "["
	tokRBRACK  tokenKind = "]"
	tokLPAREN  tokenKind = "("
	tokRPAREN  tokenKind = ")"
	tokCOMMA   tokenKind = ","
	tokSTRING  tokenKind = "STRING"
	tokNUMBER  tokenKind = "NUMBER"
	tokIDENT   tokenKind = "IDENT"
	tokCOMMENT tokenKind = "COMMENT"
	tokNEWLINE tokenKind = "NEWLINE"
	tokEOF     tokenKind = "EOF"
)

type token struct {
	kind tokenKind
	val  string
	line int
	col  int
}

func tokenise(src string) ([]token, []models.ValidationError) {
	var tokens []token
	var errs []models.ValidationError

	lines := strings.Split(src, "\n")
	for lineNum, line := range lines {
		lineNum++ // 1-indexed
		i := 0
		runes := []rune(line)
		for i < len(runes) {
			// Skip whitespace (not newline — we've already split)
			if runes[i] == ' ' || runes[i] == '\t' || runes[i] == '\r' {
				i++
				continue
			}
			col := i + 1 // 1-indexed

			// Comment
			if runes[i] == '#' {
				comment := string(runes[i:])
				tokens = append(tokens, token{tokCOMMENT, strings.TrimSpace(comment[1:]), lineNum, col})
				break
			}

			// String literal
			if runes[i] == '"' {
				j := i + 1
				for j < len(runes) && runes[j] != '"' {
					j++
				}
				if j >= len(runes) {
					errs = append(errs, models.ValidationError{Line: lineNum, Column: col, Message: "unterminated string literal", Severity: "error"})
					break
				}
				tokens = append(tokens, token{tokSTRING, string(runes[i+1 : j]), lineNum, col})
				i = j + 1
				continue
			}

			// Multi-char operators
			if i+1 < len(runes) {
				two := string(runes[i : i+2])
				switch two {
				case "==":
					tokens = append(tokens, token{tokEQ, "==", lineNum, col})
					i += 2
					continue
				case "!=":
					tokens = append(tokens, token{tokNEQ, "!=", lineNum, col})
					i += 2
					continue
				case ">=":
					tokens = append(tokens, token{tokGTE, ">=", lineNum, col})
					i += 2
					continue
				case "<=":
					tokens = append(tokens, token{tokLTE, "<=", lineNum, col})
					i += 2
					continue
				}
			}

			// Single-char operators / punctuation
			switch runes[i] {
			case '>':
				tokens = append(tokens, token{tokGT, ">", lineNum, col})
				i++
				continue
			case '<':
				tokens = append(tokens, token{tokLT, "<", lineNum, col})
				i++
				continue
			case '[':
				tokens = append(tokens, token{tokLBRACK, "[", lineNum, col})
				i++
				continue
			case ']':
				tokens = append(tokens, token{tokRBRACK, "]", lineNum, col})
				i++
				continue
			case '(':
				tokens = append(tokens, token{tokLPAREN, "(", lineNum, col})
				i++
				continue
			case ')':
				tokens = append(tokens, token{tokRPAREN, ")", lineNum, col})
				i++
				continue
			case ',':
				tokens = append(tokens, token{tokCOMMA, ",", lineNum, col})
				i++
				continue
			}

			// Number (int or float)
			if runes[i] >= '0' && runes[i] <= '9' || (runes[i] == '-' && i+1 < len(runes) && runes[i+1] >= '0' && runes[i+1] <= '9') {
				j := i
				if runes[j] == '-' {
					j++
				}
				for j < len(runes) && (runes[j] >= '0' && runes[j] <= '9' || runes[j] == '.') {
					j++
				}
				tokens = append(tokens, token{tokNUMBER, string(runes[i:j]), lineNum, col})
				i = j
				continue
			}

			// Identifier / keyword
			if isIdentStart(runes[i]) {
				j := i
				for j < len(runes) && isIdentPart(runes[j]) {
					j++
				}
				word := string(runes[i:j])
				kind := classifyKeyword(word)
				tokens = append(tokens, token{kind, word, lineNum, col})
				i = j
				continue
			}

			errs = append(errs, models.ValidationError{
				Line: lineNum, Column: col,
				Message:  fmt.Sprintf("unexpected character: %q", runes[i]),
				Severity: "error",
			})
			i++
		}

		tokens = append(tokens, token{tokNEWLINE, "", lineNum, len(runes) + 1})
	}

	tokens = append(tokens, token{tokEOF, "", len(lines) + 1, 1})
	return tokens, errs
}

func isIdentStart(r rune) bool {
	return (r >= 'a' && r <= 'z') || (r >= 'A' && r <= 'Z') || r == '_'
}

func isIdentPart(r rune) bool {
	return isIdentStart(r) || (r >= '0' && r <= '9') || r == '.' || r == '-'
}

func classifyKeyword(w string) tokenKind {
	switch strings.ToUpper(w) {
	case "WHEN":
		return tokWHEN
	case "THEN":
		return tokTHEN
	case "AND":
		return tokAND
	case "OR":
		return tokOR
	case "NOT":
		return tokNOT
	case "IN":
		return tokIN
	case "MATCHES":
		return tokMATCHES
	case "IF":
		return tokIF
	}
	return tokIDENT
}

// ============================================================================
// PARSER
// ============================================================================

// RuleParser parses DSL source into an AST
type RuleParser struct {
	tokens  []token
	pos     int
	errors  []models.ValidationError
	warnings []models.ValidationError
}

func NewRuleParser() *RuleParser {
	return &RuleParser{}
}

// Parse converts source text to a RuleScript AST.
// Returns the AST and any parse errors (warnings are embedded in the result).
func (p *RuleParser) Parse(src string) (*models.RuleScript, []models.ValidationError, []models.ValidationError) {
	toks, lexErrs := tokenise(src)
	p.tokens = toks
	p.pos = 0
	p.errors = append(p.errors, lexErrs...)
	p.warnings = nil

	script := &models.RuleScript{}
	lastComment := ""

	for !p.is(tokEOF) {
		// Skip blank lines
		for p.is(tokNEWLINE) {
			p.advance()
		}
		if p.is(tokEOF) {
			break
		}

		// Capture comment as rule name
		if p.is(tokCOMMENT) {
			lastComment = p.cur().val
			p.advance()
			continue
		}

		if p.is(tokWHEN) {
			block, ok := p.parseRuleBlock(lastComment)
			lastComment = ""
			if ok {
				script.Rules = append(script.Rules, block)
			}
		} else {
			p.errorf("expected WHEN keyword or comment, got %q", p.cur().val)
			p.skipToNextWHEN()
		}
	}

	return script, p.errors, p.warnings
}

func (p *RuleParser) parseRuleBlock(name string) (models.RuleBlock, bool) {
	block := models.RuleBlock{Name: name}
	whenTok := p.cur()
	block.LineNumber = whenTok.line

	p.expect(tokWHEN)
	p.skipNewlines()

	cond, ok := p.parseOr()
	if !ok {
		return block, false
	}
	block.Condition = cond

	p.skipNewlines()
	if !p.expect(tokTHEN) {
		return block, false
	}
	p.skipNewlines()

	for !p.is(tokWHEN) && !p.is(tokEOF) && !p.is(tokCOMMENT) {
		if p.is(tokNEWLINE) {
			p.advance()
			continue
		}
		action, ok := p.parseAction()
		if !ok {
			p.skipToNextNewlineOrWHEN()
			continue
		}
		block.Actions = append(block.Actions, action)
		p.skipNewlines()
	}

	return block, true
}

// parseOr: handles OR at lowest precedence
func (p *RuleParser) parseOr() (models.ConditionNode, bool) {
	left, ok := p.parseAnd()
	if !ok {
		return models.ConditionNode{}, false
	}

	for p.is(tokOR) {
		p.advance()
		p.skipNewlines()
		right, ok := p.parseAnd()
		if !ok {
			return left, false
		}
		left = models.ConditionNode{
			Type:  models.NodeOr,
			Left:  &models.ConditionNode{Type: left.Type, Left: left.Left, Right: left.Right, Expr: left.Expr},
			Right: &models.ConditionNode{Type: right.Type, Left: right.Left, Right: right.Right, Expr: right.Expr},
		}
	}
	return left, true
}

// parseAnd: handles AND at higher precedence than OR
func (p *RuleParser) parseAnd() (models.ConditionNode, bool) {
	left, ok := p.parseAtom()
	if !ok {
		return models.ConditionNode{}, false
	}

	for p.is(tokAND) {
		p.advance()
		p.skipNewlines()
		right, ok := p.parseAtom()
		if !ok {
			return left, false
		}
		left = models.ConditionNode{
			Type:  models.NodeAnd,
			Left:  &models.ConditionNode{Type: left.Type, Left: left.Left, Right: left.Right, Expr: left.Expr},
			Right: &models.ConditionNode{Type: right.Type, Left: right.Left, Right: right.Right, Expr: right.Expr},
		}
	}
	return left, true
}

// parseAtom: single condition expression
func (p *RuleParser) parseAtom() (models.ConditionNode, bool) {
	t := p.cur()

	if t.kind != tokIDENT {
		p.errorf("expected field name, got %q at line %d col %d", t.val, t.line, t.col)
		return models.ConditionNode{}, false
	}

	field := t.val
	p.advance()

	// Check for function-style: order.has_tag("x") or order.sku_in_order("x")
	if p.is(tokLPAREN) {
		p.advance()
		arg := ""
		if p.is(tokSTRING) {
			arg = p.cur().val
			p.advance()
		}
		if !p.expect(tokRPAREN) {
			return models.ConditionNode{}, false
		}
		expr := &models.ExprNode{
			Field:    field,
			Operator: "FUNC",
			Value:    arg,
			LineNum:  t.line,
			ColNum:   t.col,
		}
		return models.ConditionNode{Type: models.NodeCondition, Expr: expr}, true
	}

	// Standard operator
	op, ok := p.parseOperator()
	if !ok {
		return models.ConditionNode{}, false
	}

	// Value or array
	val, ok := p.parseValue(op)
	if !ok {
		return models.ConditionNode{}, false
	}

	expr := &models.ExprNode{
		Field:   field,
		Operator: op,
		Value:   val,
		LineNum: t.line,
		ColNum:  t.col,
	}

	// Validate field name
	if !isKnownField(field) {
		suggestion := suggestField(field)
		msg := fmt.Sprintf("Unknown field %q.", field)
		if suggestion != "" {
			msg += fmt.Sprintf(" Did you mean %q?", suggestion)
		}
		p.errors = append(p.errors, models.ValidationError{Line: t.line, Column: t.col, Message: msg, Severity: "error"})
	}

	return models.ConditionNode{Type: models.NodeCondition, Expr: expr}, true
}

func (p *RuleParser) parseOperator() (string, bool) {
	t := p.cur()
	switch t.kind {
	case tokEQ:
		p.advance()
		return "==", true
	case tokNEQ:
		p.advance()
		return "!=", true
	case tokGT:
		p.advance()
		return ">", true
	case tokGTE:
		p.advance()
		return ">=", true
	case tokLT:
		p.advance()
		return "<", true
	case tokLTE:
		p.advance()
		return "<=", true
	case tokIN:
		p.advance()
		return "IN", true
	case tokNOT:
		p.advance()
		if p.is(tokIN) {
			p.advance()
			return "NOT IN", true
		}
		if p.is(tokMATCHES) {
			p.advance()
			return "NOT MATCHES", true
		}
		p.errorf("expected IN or MATCHES after NOT, got %q", p.cur().val)
		return "", false
	case tokMATCHES:
		p.advance()
		return "MATCHES", true
	default:
		p.errorf("expected operator, got %q at line %d col %d", t.val, t.line, t.col)
		return "", false
	}
}

func (p *RuleParser) parseValue(op string) (string, bool) {
	switch op {
	case "IN", "NOT IN":
		return p.parseArray()
	default:
		t := p.cur()
		switch t.kind {
		case tokSTRING:
			p.advance()
			return `"` + t.val + `"`, true
		case tokNUMBER:
			p.advance()
			return t.val, true
		case tokIDENT:
			// boolean-ish identifiers: true, false
			if t.val == "true" || t.val == "false" {
				p.advance()
				return t.val, true
			}
			p.errorf("expected string or number value, got %q at line %d col %d", t.val, t.line, t.col)
			return "", false
		default:
			p.errorf("expected value, got %q at line %d col %d", t.val, t.line, t.col)
			return "", false
		}
	}
}

func (p *RuleParser) parseArray() (string, bool) {
	if !p.expect(tokLBRACK) {
		return "", false
	}

	var items []string
	for !p.is(tokRBRACK) && !p.is(tokEOF) {
		if len(items) > 0 {
			if !p.expect(tokCOMMA) {
				return "", false
			}
		}
		if p.is(tokSTRING) {
			items = append(items, `"`+p.cur().val+`"`)
			p.advance()
		} else if p.is(tokNUMBER) {
			items = append(items, p.cur().val)
			p.advance()
		} else {
			p.errorf("expected string or number in array, got %q", p.cur().val)
			return "", false
		}
	}
	if !p.expect(tokRBRACK) {
		return "", false
	}

	return "[" + strings.Join(items, ",") + "]", true
}

func (p *RuleParser) parseAction() (models.ActionNode, bool) {
	t := p.cur()
	if t.kind != tokIDENT {
		p.errorf("expected action name, got %q at line %d col %d", t.val, t.line, t.col)
		return models.ActionNode{}, false
	}

	name := t.val
	p.advance()

	if !p.expect(tokLPAREN) {
		return models.ActionNode{}, false
	}

	var params []string
	for !p.is(tokRPAREN) && !p.is(tokEOF) && !p.is(tokNEWLINE) {
		if len(params) > 0 {
			if !p.expect(tokCOMMA) {
				return models.ActionNode{}, false
			}
		}
		pt := p.cur()
		switch pt.kind {
		case tokSTRING:
			params = append(params, pt.val)
			p.advance()
		case tokNUMBER:
			params = append(params, pt.val)
			p.advance()
		case tokIDENT:
			params = append(params, pt.val)
			p.advance()
		default:
			p.errorf("unexpected token in action params: %q", pt.val)
			return models.ActionNode{}, false
		}
	}

	if !p.expect(tokRPAREN) {
		return models.ActionNode{}, false
	}

	// Validate action name
	if !isKnownAction(name) {
		p.errors = append(p.errors, models.ValidationError{
			Line: t.line, Column: t.col,
			Message:  fmt.Sprintf("Unknown action %q", name),
			Severity: "error",
		})
	}

	action := models.ActionNode{
		Name:    name,
		Params:  params,
		LineNum: t.line,
		ColNum:  t.col,
	}

	// Optional inline IF condition
	if p.is(tokIF) {
		p.advance()
		ifCond, ok := p.parseAtom()
		if ok && ifCond.Expr != nil {
			action.IfCond = ifCond.Expr
		}
	}

	// Emit warning if action requires SMTP
	if (name == "notify") && !isKnownAction(name) {
		p.warnings = append(p.warnings, models.ValidationError{
			Line: t.line, Column: t.col,
			Message:  "Action 'notify' requires SMTP to be configured",
			Severity: "warning",
		})
	}

	return action, true
}

// ============================================================================
// HELPERS
// ============================================================================

func (p *RuleParser) cur() token {
	for p.pos < len(p.tokens) && p.tokens[p.pos].kind == tokNEWLINE {
		// peek through newlines only in specific contexts
		// caller uses skipNewlines() explicitly
		break
	}
	if p.pos >= len(p.tokens) {
		return token{tokEOF, "", 0, 0}
	}
	return p.tokens[p.pos]
}

func (p *RuleParser) is(k tokenKind) bool {
	return p.cur().kind == k
}

func (p *RuleParser) advance() token {
	t := p.cur()
	p.pos++
	return t
}

func (p *RuleParser) expect(k tokenKind) bool {
	t := p.cur()
	if t.kind != k {
		p.errorf("expected %s, got %q at line %d col %d", k, t.val, t.line, t.col)
		return false
	}
	p.advance()
	return true
}

func (p *RuleParser) skipNewlines() {
	for p.pos < len(p.tokens) && p.tokens[p.pos].kind == tokNEWLINE {
		p.pos++
	}
}

func (p *RuleParser) skipToNextWHEN() {
	for !p.is(tokWHEN) && !p.is(tokEOF) {
		p.advance()
	}
}

func (p *RuleParser) skipToNextNewlineOrWHEN() {
	for !p.is(tokNEWLINE) && !p.is(tokWHEN) && !p.is(tokEOF) {
		p.advance()
	}
}

func (p *RuleParser) errorf(format string, args ...interface{}) {
	t := p.cur()
	p.errors = append(p.errors, models.ValidationError{
		Line:     t.line,
		Column:   t.col,
		Message:  fmt.Sprintf(format, args...),
		Severity: "error",
	})
}

// ============================================================================
// KNOWN FIELDS AND ACTIONS
// ============================================================================

var knownFields = map[string]string{
	"order.channel":          "string",
	"order.total_gbp":        "number",
	"order.weight_grams":     "number",
	"order.item_count":       "number",
	"order.shipping_country": "string",
	"order.shipping_postcode": "string",
	"order.shipping_city":    "string",
	"order.status":           "string",
	"order.payment_method":   "string",
	"order.payment_status":   "string",
	"order.tags":             "[]string",
	"order.customer_email":   "string",
	"order.placed_hour":      "number",
	"order.placed_date":      "string",
	"order.has_tag":          "func(string) bool",
	"order.sku_in_order":     "func(string) bool",
	"line.sku":               "string",
	"line.quantity":          "number",
	"line.title":             "string",
}

var knownActions = map[string]string{
	"select_carrier":        "select_carrier(name: string)",
	"select_service":        "select_service(code: string)",
	"require_signature":     "require_signature()",
	"add_tag":               "add_tag(tag: string)",
	"remove_tag":            "remove_tag(tag: string)",
	"set_status":            "set_status(status: string)",
	"notify":                "notify(email: string, message: string)",
	"webhook":               "webhook(url: string, method: string)",
	"set_fulfilment_source": "set_fulfilment_source(id: string)",
	"hold_order":            "hold_order(reason: string)",
	"flag_for_review":       "flag_for_review(reason: string)",
	"set_shipping_method":   "set_shipping_method(name: string)",
	"skip_remaining_rules":  "skip_remaining_rules()",
	"set_dispatch_date":     "set_dispatch_date(days: number, time: string)",
	"add_note":              `add_note("note text")`,
	"add_buyer_note":        `add_buyer_note("note text")`,
	"add_item":              `add_item(sku: string, qty: number)`,
	"assign_fulfilment_network": `assign_fulfilment_network(networkName: string)`,
}

func isKnownField(f string) bool {
	// Strip function call suffix for function-style checks
	base := strings.Split(f, "(")[0]
	_, ok := knownFields[base]
	return ok
}

func isKnownAction(name string) bool {
	_, ok := knownActions[name]
	return ok
}

// suggestField returns the closest known field name using simple edit distance
func suggestField(field string) string {
	best := ""
	bestDist := 999
	for k := range knownFields {
		d := editDistance(field, k)
		if d < bestDist && d <= 4 {
			bestDist = d
			best = k
		}
	}
	return best
}

func editDistance(a, b string) int {
	la, lb := len(a), len(b)
	dp := make([][]int, la+1)
	for i := range dp {
		dp[i] = make([]int, lb+1)
		dp[i][0] = i
	}
	for j := 0; j <= lb; j++ {
		dp[0][j] = j
	}
	for i := 1; i <= la; i++ {
		for j := 1; j <= lb; j++ {
			if a[i-1] == b[j-1] {
				dp[i][j] = dp[i-1][j-1]
			} else {
				dp[i][j] = 1 + min3(dp[i-1][j], dp[i][j-1], dp[i-1][j-1])
			}
		}
	}
	return dp[la][lb]
}

func min3(a, b, c int) int {
	if a < b {
		if a < c {
			return a
		}
		return c
	}
	if b < c {
		return b
	}
	return c
}

// ============================================================================
// FIELD / ACTION METADATA (for /automation/fields and /automation/actions)
// ============================================================================

type FieldMeta struct {
	Field       string `json:"field"`
	Type        string `json:"type"`
	Description string `json:"description"`
}

type ActionMeta struct {
	Name        string `json:"name"`
	Signature   string `json:"signature"`
	Description string `json:"description"`
}

func GetFieldMetadata() []FieldMeta {
	return []FieldMeta{
		{Field: "order.channel", Type: "string", Description: `Sales channel: "amazon", "ebay", "temu"`},
		{Field: "order.total_gbp", Type: "number", Description: "Grand total in GBP"},
		{Field: "order.weight_grams", Type: "number", Description: "Total order weight in grams"},
		{Field: "order.item_count", Type: "number", Description: "Number of line items"},
		{Field: "order.shipping_country", Type: "string", Description: "ISO 2-letter destination country code"},
		{Field: "order.shipping_postcode", Type: "string", Description: "Destination postcode"},
		{Field: "order.shipping_city", Type: "string", Description: "Destination city"},
		{Field: "order.status", Type: "string", Description: "Current order status"},
		{Field: "order.payment_method", Type: "string", Description: "Payment method"},
		{Field: "order.payment_status", Type: "string", Description: "Payment status"},
		{Field: "order.tags", Type: "[]string", Description: "Applied tags — use IN operator"},
		{Field: "order.customer_email", Type: "string", Description: "Customer email address"},
		{Field: "order.placed_hour", Type: "number", Description: "Hour of day the order was placed (0-23, UTC)"},
		{Field: "order.placed_date", Type: "string", Description: "Date the order was placed, YYYY-MM-DD"},
		{Field: "order.has_tag(x)", Type: "bool", Description: "Returns true if the order has the specified tag"},
		{Field: "order.sku_in_order(x)", Type: "bool", Description: "Returns true if any line item has this SKU"},
		{Field: "line.sku", Type: "string", Description: "Primary line item SKU"},
		{Field: "line.quantity", Type: "number", Description: "Primary line item quantity"},
		{Field: "line.title", Type: "string", Description: "Primary line item title"},
	}
}

func GetActionMetadata() []ActionMeta {
	return []ActionMeta{
		{Name: "select_carrier", Signature: `select_carrier(name: string)`, Description: "Set preferred carrier for this shipment"},
		{Name: "select_service", Signature: `select_service(code: string)`, Description: "Set preferred carrier service code"},
		{Name: "require_signature", Signature: `require_signature()`, Description: "Flag shipment as requiring signature on delivery"},
		{Name: "add_tag", Signature: `add_tag(tag: string)`, Description: "Add a tag to the order"},
		{Name: "remove_tag", Signature: `remove_tag(tag: string)`, Description: "Remove a tag from the order"},
		{Name: "set_status", Signature: `set_status(status: string)`, Description: "Update the order status"},
		{Name: "notify", Signature: `notify(email: string, message: string)`, Description: "Send an email notification (requires SMTP)"},
		{Name: "webhook", Signature: `webhook(url: string, method: string)`, Description: "Fire an outbound webhook"},
		{Name: "set_fulfilment_source", Signature: `set_fulfilment_source(id: string)`, Description: "Assign a fulfilment source"},
		{Name: "hold_order", Signature: `hold_order(reason: string)`, Description: "Place the order on hold"},
		{Name: "flag_for_review", Signature: `flag_for_review(reason: string)`, Description: "Flag for manual review"},
		{Name: "set_shipping_method", Signature: `set_shipping_method(name: string)`, Description: "Override display shipping method name"},
		{Name: "skip_remaining_rules", Signature: `skip_remaining_rules()`, Description: "Stop evaluating further rules in this set"},
		{Name: "set_dispatch_date", Signature: `set_dispatch_date(days: number, time: "HH:MM")`, Description: "Set the despatch-by date to N days after order receipt, with optional cutoff time (e.g. set_dispatch_date(1, \"14:00\"))"},
		{Name: "add_note", Signature: `add_note("note text")`, Description: "Append an internal note to the order (visible to staff only)"},
		{Name: "add_buyer_note", Signature: `add_buyer_note("note text")`, Description: "Add a note visible to the buyer"},
		{Name: "add_item", Signature: `add_item(sku: "SKU", qty: 1)`, Description: "Add a product line to the order (e.g. free gift or service item); marks line with is_auto_added"},
		{Name: "assign_fulfilment_network", Signature: `assign_fulfilment_network(networkName: string)`, Description: "Resolve and assign the best fulfilment source from a named network (priority waterfall routing)"},
	}
}

// ============================================================================
// REGEX COMPILE HELPER
// ============================================================================

func CompileRegex(pattern string) (*regexp.Regexp, error) {
	return regexp.Compile(pattern)
}

// ParseNumber safely parses a raw value string to float64
func ParseNumber(s string) (float64, error) {
	// strip surrounding quotes if present
	s = strings.Trim(s, `"`)
	return strconv.ParseFloat(s, 64)
}
