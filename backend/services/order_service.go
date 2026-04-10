package services

import (
	"context"
	"fmt"
	"log"
	"strconv"
	"strings"
	"time"

	"cloud.google.com/go/firestore"
	"google.golang.org/api/iterator"
	"google.golang.org/grpc/codes"
	"google.golang.org/grpc/status"

	"module-a/models"
	"module-a/repository"
)

type OrderService struct {
	repo        *repository.FirestoreRepository
	taskService *TaskService   // optional — nil in dev/when Cloud Tasks not configured
	piiService  *PIIService    // PII encryption — always set, may be in passthrough mode
	templateSvc *TemplateService // optional — fires automated email triggers on new orders
}

func NewOrderService(repo *repository.FirestoreRepository, piiService *PIIService) *OrderService {
	return &OrderService{repo: repo, piiService: piiService}
}

// SetTaskService injects the Cloud Tasks service after construction.
// Called from main.go only when Cloud Tasks initialises successfully.
// If not set, workflow processing must be triggered manually via:
//   POST /api/v1/orders/:id/process-workflows
func (s *OrderService) SetTaskService(ts *TaskService) {
	s.taskService = ts
}

// SetTemplateService injects the TemplateService for automated email triggers.
// When set, CreateOrder fires an order_confirmation email for every genuinely new order.
func (s *OrderService) SetTemplateService(ts *TemplateService) {
	s.templateSvc = ts
}

type OrderListOptions struct {
	Status    string
	Channel   string
	Limit     string
	Offset    string
	SortBy    string
	SortOrder string
	// Multi-column sort (Task 4) — if set, takes precedence over SortBy/SortOrder
	SortFields []SortField
	// Full-text search (Task 5)
	FreeTextSearch string
	// Date range filter (Task 7)
	ReceivedDateFrom    string
	ReceivedDateTo      string
	DespatchByDateFrom  string
	DespatchByDateTo    string
	DeliveryDateFrom    string
	DeliveryDateTo      string
	// Shipping / destination filters (Task 8)
	ShippingService string
	Carrier         string
	DestinationCountry string
	PackagingType   string
	// Folder filter (Fix 1B)
	FolderID string
}

// SortField defines a single sort column and direction
type SortField struct {
	Field     string
	Direction string // "asc" | "desc"
}

// ListOrders retrieves orders with filtering and pagination
func (s *OrderService) ListOrders(ctx context.Context, tenantID string, opts OrderListOptions) ([]models.Order, int, error) {
	client := s.repo.GetClient()
	baseCollection := client.Collection("tenants").Doc(tenantID).Collection("orders")

	var q firestore.Query = baseCollection.Query

	if opts.Status != "" {
		q = q.Where("status", "==", opts.Status)
	}
	if opts.Channel != "" {
		q = q.Where("channel", "==", opts.Channel)
	}
	// Task 8: shipping / destination filters
	if opts.ShippingService != "" {
		q = q.Where("shipping_service", "==", opts.ShippingService)
	}
	if opts.Carrier != "" {
		q = q.Where("carrier", "==", opts.Carrier)
	}
	if opts.DestinationCountry != "" {
		q = q.Where("shipping_address.country", "==", opts.DestinationCountry)
	}
	if opts.PackagingType != "" {
		q = q.Where("packaging_type", "==", opts.PackagingType)
	}
	// Fix 1B: folder filter
	if opts.FolderID != "" {
		q = q.Where("folder_id", "==", opts.FolderID)
	}

	// Task 7: date range filters
	if opts.ReceivedDateFrom != "" {
		if t, err := time.Parse("2006-01-02", opts.ReceivedDateFrom); err == nil {
			q = q.Where("order_date", ">=", t.Format(time.RFC3339))
		}
	}
	if opts.ReceivedDateTo != "" {
		if t, err := time.Parse("2006-01-02", opts.ReceivedDateTo); err == nil {
			q = q.Where("order_date", "<=", t.Add(24*time.Hour).Format(time.RFC3339))
		}
	}
	if opts.DespatchByDateFrom != "" {
		if t, err := time.Parse("2006-01-02", opts.DespatchByDateFrom); err == nil {
			q = q.Where("despatch_by_date", ">=", t.Format(time.RFC3339))
		}
	}
	if opts.DespatchByDateTo != "" {
		if t, err := time.Parse("2006-01-02", opts.DespatchByDateTo); err == nil {
			q = q.Where("despatch_by_date", "<=", t.Add(24*time.Hour).Format(time.RFC3339))
		}
	}
	if opts.DeliveryDateFrom != "" {
		if t, err := time.Parse("2006-01-02", opts.DeliveryDateFrom); err == nil {
			q = q.Where("scheduled_delivery_date", ">=", t.Format(time.RFC3339))
		}
	}
	if opts.DeliveryDateTo != "" {
		if t, err := time.Parse("2006-01-02", opts.DeliveryDateTo); err == nil {
			q = q.Where("scheduled_delivery_date", "<=", t.Add(24*time.Hour).Format(time.RFC3339))
		}
	}

	// Task 4: multi-column sort
	if len(opts.SortFields) > 0 {
		for _, sf := range opts.SortFields {
			dir := firestore.Desc
			if strings.ToLower(sf.Direction) == "asc" {
				dir = firestore.Asc
			}
			q = q.OrderBy(sf.Field, dir)
		}
	} else if opts.SortBy != "" {
		direction := firestore.Desc
		if opts.SortOrder == "asc" {
			direction = firestore.Asc
		}
		q = q.OrderBy(opts.SortBy, direction)
	}

	limit, _ := strconv.Atoi(opts.Limit)
	if limit <= 0 {
		limit = 50
	}
	q = q.Limit(limit)

	offset, _ := strconv.Atoi(opts.Offset)
	if offset > 0 {
		q = q.Offset(offset)
	}

	iter := q.Documents(ctx)
	defer iter.Stop()

	var orders []models.Order
	for {
		doc, err := iter.Next()
		if err == iterator.Done {
			break
		}
		if err != nil {
			return nil, 0, err
		}

		var order models.Order
		if err := doc.DataTo(&order); err != nil {
			log.Printf("Error unmarshaling order: %v", err)
			continue
		}
		// Decrypt PII for display
		if order.PIIEncrypted && s.piiService != nil {
			ef := EncryptedOrderFields{
				CustomerEnc:  order.CustomerEnc,
				ShippingEnc:  order.ShippingEnc,
				BillingEnc:   order.BillingEnc,
				PIIEncrypted: order.PIIEncrypted,
			}
			if decrypted, err := s.piiService.DecryptOrder(order, ef); err == nil {
				order = decrypted
			}
		}

		// Task 5: free-text search post-filter (order_id, external_order_id, sku)
		if opts.FreeTextSearch != "" {
			q := strings.ToLower(opts.FreeTextSearch)
			matched := strings.Contains(strings.ToLower(order.OrderID), q) ||
				strings.Contains(strings.ToLower(order.ExternalOrderID), q)
			if !matched {
				// Check SKUs in line items (in memory — lines fetched separately if needed)
				for _, line := range order.Lines {
					if strings.Contains(strings.ToLower(line.SKU), q) ||
						strings.Contains(strings.ToLower(line.Title), q) {
						matched = true
						break
					}
				}
			}
			if !matched {
				continue
			}
		}

		orders = append(orders, order)
	}

	total := len(orders)
	return orders, total, nil
}

// GetOrder retrieves a single order by ID
func (s *OrderService) GetOrder(ctx context.Context, tenantID, orderID string) (*models.Order, error) {
	client := s.repo.GetClient()
	doc, err := client.Collection("tenants").Doc(tenantID).Collection("orders").Doc(orderID).Get(ctx)
	if err != nil {
		return nil, fmt.Errorf("order not found")
	}

	var order models.Order
	if err := doc.DataTo(&order); err != nil {
		return nil, err
	}

	return &order, nil
}

// GetOrderLines retrieves all line items for an order
func (s *OrderService) GetOrderLines(ctx context.Context, tenantID, orderID string) ([]models.OrderLine, error) {
	client := s.repo.GetClient()
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
			log.Printf("Error unmarshaling order line: %v", err)
			continue
		}
		lines = append(lines, line)
	}

	return lines, nil
}

// UpdateOrderStatus updates the status of an order
func (s *OrderService) UpdateOrderStatus(ctx context.Context, tenantID, orderID, status, subStatus, notes string) error {
	client := s.repo.GetClient()
	updates := []firestore.Update{
		{Path: "status", Value: status},
		{Path: "updated_at", Value: time.Now().Format(time.RFC3339)},
	}

	if subStatus != "" {
		updates = append(updates, firestore.Update{Path: "sub_status", Value: subStatus})
	}
	if notes != "" {
		updates = append(updates, firestore.Update{Path: "internal_notes", Value: notes})
	}

	_, err := client.Collection("tenants").Doc(tenantID).Collection("orders").Doc(orderID).Update(ctx, updates)
	if err != nil {
		return err
	}

	// Release stock reservations when order is despatched or cancelled
	// so the freed stock is available for other channels immediately.
	switch status {
	case "fulfilled", "despatched", "shipped", "cancelled", "refunded":
		reason := "cancellation"
		if status == "fulfilled" || status == "despatched" || status == "shipped" {
			reason = "despatch"
		}
		releaseOrderReservations(ctx, client, tenantID, orderID, reason)
	}

	return nil
}

// releaseOrderReservations marks all active stock reservations for an order as released.
// Inlined here to avoid a circular dependency between services and handlers packages.
func releaseOrderReservations(ctx context.Context, client *firestore.Client, tenantID, orderID, reason string) {
	iter := client.Collection("tenants").Doc(tenantID).
		Collection("stock_reservations").
		Where("order_id", "==", orderID).
		Where("status", "==", "active").
		Documents(ctx)

	now := time.Now()
	for {
		doc, err := iter.Next()
		if err == iterator.Done {
			break
		}
		if err != nil {
			break
		}
		doc.Ref.Update(ctx, []firestore.Update{
			{Path: "status", Value: "released"},
			{Path: "released_at", Value: now},
			{Path: "released_by", Value: reason},
		})
	}
	iter.Stop()
}

// CreateOrder saves an order to Firestore.
//
// DUPLICATE DETECTION: Checks for an existing order with the same
// external_order_id + channel_account_id before writing. If found,
// returns the existing order ID and skips the write — this is not
// an error, it is normal behaviour during re-imports and retries.
//
// Returns (orderID, isNew, error).
// isNew == false means the order already existed; caller should skip
// line item processing to avoid duplicating lines.
func (s *OrderService) CreateOrder(ctx context.Context, tenantID string, order *models.Order) (string, bool, error) {
	client := s.repo.GetClient()

	// --- Duplicate detection ---
	// Only check if we have an external order ID to match on.
	if order.ExternalOrderID != "" && order.ChannelAccountID != "" {
		existing, err := s.findExistingOrder(ctx, tenantID, order.ExternalOrderID, order.ChannelAccountID)
		if err != nil {
			// Log but don't fail — better to risk a duplicate than to
			// silently drop a real order due to a query error.
			log.Printf("Warning: duplicate check failed for %s/%s: %v — proceeding with write",
				order.Channel, order.ExternalOrderID, err)
		} else if existing != "" {
			// Order already in Firestore. Return the existing ID.
			return existing, false, nil
		}
	}

	// --- Generate internal order ID if not already set ---
	if order.OrderID == "" {
		order.OrderID = fmt.Sprintf("ord_%d", time.Now().UnixNano())
	}

	order.TenantID = tenantID
	now := time.Now().Format(time.RFC3339)
	order.CreatedAt = now
	order.UpdatedAt = now
	order.ImportedAt = now

	// Default status for new imported orders
	if order.Status == "" {
		order.Status = "imported"
	}

	// Capture plain-text customer email before PII encryption mutates the struct.
	// Used below to fire the order_confirmation automated email trigger.
	plainEmail := order.Customer.Email
	plainName  := order.Customer.Name

	// Encrypt PII fields before writing to Firestore
	if s.piiService != nil {
		sanitised, ef, encErr := s.piiService.EncryptOrder(*order)
		if encErr != nil {
			log.Printf("Warning: PII encryption failed for order %s: %v — writing plaintext", order.OrderID, encErr)
		} else {
			*order = sanitised
			order.CustomerEnc   = ef.CustomerEnc
			order.ShippingEnc   = ef.ShippingEnc
			order.BillingEnc    = ef.BillingEnc
			order.EmailToken    = ef.EmailToken
			order.NameToken     = ef.NameToken
			order.PostcodeToken = ef.PostcodeToken
			order.PhoneToken    = ef.PhoneToken
			order.PIIEncrypted  = ef.PIIEncrypted
		}
	}

	_, err := client.Collection("tenants").Doc(tenantID).Collection("orders").Doc(order.OrderID).Set(ctx, order)
	if err != nil {
		return "", false, err
	}

	// Enqueue workflow processing asynchronously via Cloud Tasks.
	// Fire-and-forget: a failure to enqueue must not roll back the order write.
	// The operator can manually trigger POST /api/v1/orders/:id/process-workflows.
	if s.taskService != nil {
		if enqErr := s.taskService.EnqueueWorkflowProcessing(ctx, tenantID, order.OrderID); enqErr != nil {
			log.Printf("Warning: failed to enqueue workflow task for order %s: %v — trigger manually if needed",
				order.OrderID, enqErr)
		}
	}

	// Fire order_confirmation automated email trigger for genuinely new orders.
	if s.templateSvc != nil && plainEmail != "" {
		// Build a minimal order value with plain-text customer details for the email renderer.
		// The stored order may have PII encrypted at this point.
		emailOrder := *order
		emailOrder.Customer.Email = plainEmail
		emailOrder.Customer.Name  = plainName
		go s.templateSvc.SendEventEmail(context.Background(), tenantID, models.TriggerEventOrderConfirmation, &emailOrder)
	}

	return order.OrderID, true, nil
}

// findExistingOrder queries for an order by external ID and channel account.
// Returns the internal order ID if found, or "" if not found.
func (s *OrderService) findExistingOrder(ctx context.Context, tenantID, externalOrderID, channelAccountID string) (string, error) {
	client := s.repo.GetClient()

	iter := client.Collection("tenants").Doc(tenantID).Collection("orders").
		Where("external_order_id", "==", externalOrderID).
		Where("channel_account_id", "==", channelAccountID).
		Limit(1).
		Documents(ctx)
	defer iter.Stop()

	doc, err := iter.Next()
	if err == iterator.Done {
		return "", nil // Not found — this is a new order
	}
	if err != nil {
		return "", err
	}

	return doc.Ref.ID, nil
}

// CreateOrderLine creates a line item for an order.
// Safe to call multiple times — uses line_id as document key so
// re-imports won't duplicate lines.
func (s *OrderService) CreateOrderLine(ctx context.Context, tenantID, orderID string, line *models.OrderLine) error {
	client := s.repo.GetClient()

	if line.LineID == "" {
		line.LineID = fmt.Sprintf("line_%d", time.Now().UnixNano())
	}

	_, err := client.Collection("tenants").Doc(tenantID).
		Collection("orders").Doc(orderID).
		Collection("lines").Doc(line.LineID).
		Set(ctx, line)
	return err
}

// OrderExists checks if an order document exists in Firestore by internal ID.
func (s *OrderService) OrderExists(ctx context.Context, tenantID, orderID string) (bool, error) {
	client := s.repo.GetClient()
	doc, err := client.Collection("tenants").Doc(tenantID).Collection("orders").Doc(orderID).Get(ctx)
	if err != nil {
		if status.Code(err) == codes.NotFound {
			return false, nil
		}
		return false, err
	}
	return doc.Exists(), nil
}

// StartOrderImport initiates an order import job record
func (s *OrderService) StartOrderImport(ctx context.Context, tenantID, channel, channelAccountID, dateFrom, dateTo string) (string, error) {
	client := s.repo.GetClient()

	jobID := fmt.Sprintf("order_import_%s_%d", channel, time.Now().Unix())

	job := map[string]interface{}{
		"job_id":             jobID,
		"tenant_id":          tenantID,
		"type":               "order_import",
		"channel":            channel,
		"channel_account_id": channelAccountID,
		"date_from":          dateFrom,
		"date_to":            dateTo,
		"status":             "pending",
		"orders_imported":    0,
		"orders_failed":      0,
		"orders_skipped":     0, // New: tracks duplicates skipped
		"created_at":         time.Now().Format(time.RFC3339),
		"updated_at":         time.Now().Format(time.RFC3339),
	}

	_, err := client.Collection("tenants").Doc(tenantID).
		Collection("import_jobs").Doc(jobID).
		Set(ctx, job)
	if err != nil {
		return "", err
	}

	return jobID, nil
}

// UpdateImportJobStatus updates an import job's progress counters
func (s *OrderService) UpdateImportJobStatus(ctx context.Context, tenantID, jobID, jobStatus string, ordersImported, ordersFailed int, errors []string) error {
	client := s.repo.GetClient()

	updates := []firestore.Update{
		{Path: "status", Value: jobStatus},
		{Path: "orders_imported", Value: ordersImported},
		{Path: "orders_failed", Value: ordersFailed},
		{Path: "updated_at", Value: time.Now().Format(time.RFC3339)},
	}

	if len(errors) > 0 {
		updates = append(updates, firestore.Update{Path: "errors", Value: errors})
	}

	if jobStatus == "running" {
		updates = append(updates, firestore.Update{Path: "started_at", Value: time.Now().Format(time.RFC3339)})
	}
	if jobStatus == "completed" || jobStatus == "failed" {
		updates = append(updates, firestore.Update{Path: "completed_at", Value: time.Now().Format(time.RFC3339)})
	}

	_, err := client.Collection("tenants").Doc(tenantID).
		Collection("import_jobs").Doc(jobID).
		Update(ctx, updates)
	return err
}

// GetImportJob retrieves an import job by ID
func (s *OrderService) GetImportJob(ctx context.Context, tenantID, jobID string) (map[string]interface{}, error) {
	client := s.repo.GetClient()
	doc, err := client.Collection("tenants").Doc(tenantID).
		Collection("import_jobs").Doc(jobID).Get(ctx)
	if err != nil {
		return nil, fmt.Errorf("import job not found")
	}
	return doc.Data(), nil
}

// ListImportJobs retrieves recent import jobs for a tenant
func (s *OrderService) ListImportJobs(ctx context.Context, tenantID string) ([]map[string]interface{}, error) {
	client := s.repo.GetClient()
	iter := client.Collection("tenants").Doc(tenantID).
		Collection("import_jobs").
		OrderBy("created_at", firestore.Desc).
		Limit(50).
		Documents(ctx)
	defer iter.Stop()

	var jobs []map[string]interface{}
	for {
		doc, err := iter.Next()
		if err == iterator.Done {
			break
		}
		if err != nil {
			return nil, err
		}
		jobs = append(jobs, doc.Data())
	}
	return jobs, nil
}

// buildSKUMaps returns two maps derived from the tenant's order line items:
//   - orderSKUs: orderID → []sku  (all SKUs seen on that order's lines)
//   - allSKUs:   set of every unique SKU across all orders
//
// Lines are stored as a sub-collection "lines" under each order document.
// We use a CollectionGroup query and filter by path prefix so we make exactly
// one Firestore round-trip regardless of order count.
func (s *OrderService) buildSKUMaps(ctx context.Context, tenantID string) (orderSKUs map[string][]string, allSKUs map[string]struct{}) {
	client := s.repo.GetClient()
	orderSKUs = make(map[string][]string)
	allSKUs = make(map[string]struct{})

	ordersRef := client.Collection("tenants").Doc(tenantID).Collection("orders")
	tenantOrdersPath := ordersRef.Path // full path: "projects/.../tenants/{tenantID}/orders"

	linesIter := client.CollectionGroup("lines").Documents(ctx)
	defer linesIter.Stop()

	for {
		doc, err := linesIter.Next()
		if err == iterator.Done {
			break
		}
		if err != nil {
			break
		}
		// doc.Ref.Parent is the "lines" collection ref; doc.Ref.Parent.Parent is the order doc ref
		orderRef := doc.Ref.Parent.Parent
		if orderRef == nil {
			continue
		}
		// Only include lines that belong to this tenant's orders
		if !strings.HasPrefix(orderRef.Path, tenantOrdersPath+"/") {
			continue
		}
		orderID := orderRef.ID

		data := doc.Data()
		sku, _ := data["sku"].(string)
		if sku == "" {
			continue
		}
		orderSKUs[orderID] = append(orderSKUs[orderID], sku)
		allSKUs[sku] = struct{}{}
	}
	return orderSKUs, allSKUs
}

// buildProductTypeMap returns a sku → product_type map for all products in the tenant catalogue.
func (s *OrderService) buildProductTypeMap(ctx context.Context, tenantID string) map[string]string {
	client := s.repo.GetClient()
	skuTypeMap := make(map[string]string)

	prodIter := client.Collection("tenants").Doc(tenantID).Collection("products").Documents(ctx)
	defer prodIter.Stop()

	for {
		doc, err := prodIter.Next()
		if err == iterator.Done {
			break
		}
		if err != nil {
			break
		}
		data := doc.Data()
		sku, _ := data["sku"].(string)
		ptype, _ := data["product_type"].(string)
		if sku != "" {
			skuTypeMap[sku] = ptype
		}
	}
	return skuTypeMap
}

// GetOrderStats returns aggregate counts by status, plus unlinked_items_count and composite_items_count.
func (s *OrderService) GetOrderStats(ctx context.Context, tenantID string) (map[string]interface{}, error) {
	client := s.repo.GetClient()

	stats := map[string]interface{}{
		"total":                 0,
		"imported":              0,
		"processing":            0,
		"on_hold":               0,
		"ready_to_fulfil":       0,
		"fulfilled":             0,
		"cancelled":             0,
		"unlinked_items_count":  0,
		"composite_items_count": 0,
	}

	// --- Pass 1: scan orders for status counts ---
	iter := client.Collection("tenants").Doc(tenantID).Collection("orders").Documents(ctx)
	defer iter.Stop()

	for {
		doc, err := iter.Next()
		if err == iterator.Done {
			break
		}
		if err != nil {
			return stats, nil
		}
		var order models.Order
		if err := doc.DataTo(&order); err != nil {
			continue
		}
		stats["total"] = stats["total"].(int) + 1
		if count, ok := stats[order.Status]; ok {
			stats[order.Status] = count.(int) + 1
		}
	}

	// --- Pass 2: build orderID → []sku map via CollectionGroup on "lines" ---
	orderSKUs, allSKUs := s.buildSKUMaps(ctx, tenantID)

	if len(allSKUs) == 0 {
		return stats, nil
	}

	// --- Pass 3: build sku → product_type map ---
	skuTypeMap := s.buildProductTypeMap(ctx, tenantID)

	// --- Pass 4: classify orders ---
	unlinkedCount := 0
	compositeCount := 0
	for _, skus := range orderSKUs {
		hasUnlinked := false
		hasComposite := false
		for _, sku := range skus {
			ptype, exists := skuTypeMap[sku]
			if !exists {
				hasUnlinked = true
			}
			if ptype == "bundle" {
				hasComposite = true
			}
		}
		if hasUnlinked {
			unlinkedCount++
		}
		if hasComposite {
			compositeCount++
		}
	}
	stats["unlinked_items_count"] = unlinkedCount
	stats["composite_items_count"] = compositeCount

	return stats, nil
}

// GetOrdersBySpecialFilter returns orders matching a special filter: "unlinked" or "composite".
// It performs a full scan but is only called when the user explicitly clicks a badge filter.
func (s *OrderService) GetOrdersBySpecialFilter(ctx context.Context, tenantID, filter string) ([]models.Order, error) {
	client := s.repo.GetClient()

	// Build orderID → []sku and sku → product_type maps
	orderSKUs, allSKUs := s.buildSKUMaps(ctx, tenantID)
	if len(allSKUs) == 0 {
		return nil, nil
	}
	skuTypeMap := s.buildProductTypeMap(ctx, tenantID)

	// Determine which order IDs match the filter
	matchingIDs := make(map[string]struct{})
	for orderID, skus := range orderSKUs {
		for _, sku := range skus {
			ptype, exists := skuTypeMap[sku]
			if filter == "unlinked" && !exists {
				matchingIDs[orderID] = struct{}{}
				break
			}
			if filter == "composite" && ptype == "bundle" {
				matchingIDs[orderID] = struct{}{}
				break
			}
		}
	}

	if len(matchingIDs) == 0 {
		return nil, nil
	}

	// Fetch the matching orders
	var result []models.Order
	for orderID := range matchingIDs {
		doc, err := client.Collection("tenants").Doc(tenantID).Collection("orders").Doc(orderID).Get(ctx)
		if err != nil {
			continue
		}
		var order models.Order
		if err := doc.DataTo(&order); err != nil {
			continue
		}
		if order.PIIEncrypted && s.piiService != nil {
			ef := EncryptedOrderFields{
				CustomerEnc:  order.CustomerEnc,
				ShippingEnc:  order.ShippingEnc,
				BillingEnc:   order.BillingEnc,
				PIIEncrypted: order.PIIEncrypted,
			}
			if decrypted, err := s.piiService.DecryptOrder(order, ef); err == nil {
				order = decrypted
			}
		}
		result = append(result, order)
	}
	return result, nil
}

// BulkUpdateStatus updates status for multiple orders at once
func (s *OrderService) BulkUpdateStatus(ctx context.Context, tenantID string, orderIDs []string, orderStatus, subStatus string) (int, error) {
	client := s.repo.GetClient()
	updated := 0

	for _, orderID := range orderIDs {
		updates := []firestore.Update{
			{Path: "status", Value: orderStatus},
			{Path: "updated_at", Value: time.Now().Format(time.RFC3339)},
		}
		if subStatus != "" {
			updates = append(updates, firestore.Update{Path: "sub_status", Value: subStatus})
		}

		_, err := client.Collection("tenants").Doc(tenantID).Collection("orders").Doc(orderID).Update(ctx, updates)
		if err != nil {
			log.Printf("Failed to update order %s: %v", orderID, err)
			continue
		}
		updated++
	}

	return updated, nil
}

// SearchOrdersByPII searches for orders matching a PII token field.
// fieldName should be one of: "pii_email_token", "pii_name_token",
// "pii_postcode_token", "pii_phone_token"
func (s *OrderService) SearchOrdersByPII(ctx context.Context, tenantID, fieldName, rawValue string) ([]models.Order, error) {
	client := s.repo.GetClient()

	var token string
	if s.piiService != nil {
		token = s.piiService.SearchToken(rawValue)
	} else {
		return nil, fmt.Errorf("PII service not available")
	}

	iter := client.Collection("tenants").Doc(tenantID).Collection("orders").
		Where(fieldName, "==", token).
		Limit(50).
		Documents(ctx)
	defer iter.Stop()

	var orders []models.Order
	for {
		doc, err := iter.Next()
		if err == iterator.Done {
			break
		}
		if err != nil {
			return nil, err
		}
		var order models.Order
		if err := doc.DataTo(&order); err != nil {
			continue
		}
		if order.PIIEncrypted {
			ef := EncryptedOrderFields{
				CustomerEnc:  order.CustomerEnc,
				ShippingEnc:  order.ShippingEnc,
				BillingEnc:   order.BillingEnc,
				PIIEncrypted: order.PIIEncrypted,
			}
			if decrypted, err := s.piiService.DecryptOrder(order, ef); err == nil {
				order = decrypted
			}
		}
		orders = append(orders, order)
	}
	return orders, nil
}
