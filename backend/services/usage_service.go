package services

import (
	"context"
	"fmt"
	"log"
	"sync"
	"time"

	"cloud.google.com/go/firestore"
	"github.com/google/uuid"
	"google.golang.org/grpc/codes"
	"google.golang.org/grpc/status"

	"module-a/models"
)

// ============================================================================
// USAGE METERING SERVICE
// ============================================================================
// Implements the dual-write pattern: every usage event atomically writes to
// the immutable audit log AND debits the credit ledger in a single Firestore
// transaction. If the tenant has insufficient credits the transaction is
// aborted and ErrQuotaExceeded is returned — nothing is charged.
//
// Credit rates are cached in memory and refreshed every 5 minutes from
// /system/credit_rates so changes take effect without redeployment.
// ============================================================================

var ErrQuotaExceeded = fmt.Errorf("credit quota exceeded for this billing period")

type UsageService struct {
	client *firestore.Client

	// Rate cache
	mu          sync.RWMutex
	cachedRates *models.CreditRates
	ratesExpiry time.Time
}

func NewUsageService(client *firestore.Client) *UsageService {
	return &UsageService{client: client}
}

// ============================================================================
// RECORD USAGE — primary entry point for all handlers
// ============================================================================

// RecordUsage atomically debits credits from the tenant's ledger and writes
// an immutable audit log entry. Returns ErrQuotaExceeded if the tenant would
// exceed their monthly credit allocation.
//
// For Premium/Enterprise tenants (nil CreditsAllocated), usage is always
// recorded but never blocked.
//
// This should be called as a fire-and-forget goroutine for non-critical paths:
//   go usageSvc.RecordUsage(ctx, tenantID, event)
// For paths where quota enforcement matters (AI generation), call directly.
func (s *UsageService) RecordUsage(ctx context.Context, tenantID string, event models.UsageEvent) error {
	rates, err := s.getRates(ctx)
	if err != nil {
		// Rates fetch failure should not break the main request — log and use defaults
		log.Printf("[usage] failed to fetch credit rates, using defaults: %v", err)
		defaults := models.DefaultCreditRates()
		rates = &defaults
	}

	credits := s.computeCredits(event, rates)
	period := currentPeriod()
	eventID := "evt_" + uuid.New().String()

	ledgerRef := s.client.Collection("tenants").Doc(tenantID).
		Collection("credit_ledger").Doc(period)
	auditRef := s.client.Collection("tenants").Doc(tenantID).
		Collection("audit_log").Doc(eventID)

	return s.client.RunTransaction(ctx, func(ctx context.Context, tx *firestore.Transaction) error {
		// ── 1. Read current ledger ──────────────────────────────────────────
		ledgerSnap, err := tx.Get(ledgerRef)

		var ledger models.CreditLedger
		if err != nil {
			if status.Code(err) == codes.NotFound {
				// First usage this period — initialise the ledger
				ledger, err = s.initLedger(ctx, tenantID, period)
				if err != nil {
					return fmt.Errorf("failed to initialise ledger: %w", err)
				}
			} else {
				return fmt.Errorf("failed to read ledger: %w", err)
			}
		} else {
			if err := ledgerSnap.DataTo(&ledger); err != nil {
				return fmt.Errorf("failed to decode ledger: %w", err)
			}
		}

		// ── 2. Quota check (starter plans only) ────────────────────────────
		balanceBefore := float64(0)
		if ledger.CreditsRemaining != nil {
			balanceBefore = *ledger.CreditsRemaining
			if balanceBefore-credits < 0 {
				return ErrQuotaExceeded
			}
		} else {
			// Premium/Enterprise — use credits_used as "balance before"
			balanceBefore = ledger.CreditsUsed
		}

		balanceAfter := balanceBefore - credits
		if ledger.CreditsRemaining == nil {
			// For unlimited plans, balance_after is just credits_used+this
			balanceAfter = ledger.CreditsUsed + credits
		}

		// ── 3. Write immutable audit log entry ─────────────────────────────
		auditEntry := models.AuditLogEntry{
			EventID:        eventID,
			TenantID:       tenantID,
			Type:           event.Type,
			SubType:        event.SubType,
			Quantity:       event.Quantity,
			Unit:           string(event.Type),
			CreditsCharged: credits,
			RateApplied:    s.rateForType(event.Type, rates),
			Actor:          event.Actor,
			UserID:         event.UserID,
			Endpoint:       event.Endpoint,
			Marketplace:    event.Marketplace,
			Metadata:       event.Metadata,
			LedgerPeriod:   period,
			BalanceBefore:  balanceBefore,
			BalanceAfter:   balanceAfter,
			OccurredAt:     time.Now().UTC(),
			// Deliberately NO updated_at — immutable
		}
		if err := tx.Create(auditRef, auditEntry); err != nil {
			return fmt.Errorf("failed to write audit entry: %w", err)
		}

		// ── 4. Update ledger balance ────────────────────────────────────────
		updates := []firestore.Update{
			{Path: "credits_used", Value: firestore.Increment(credits)},
			{Path: "updated_at", Value: time.Now().UTC()},
			// Operational counters
			{Path: "api_calls_total", Value: firestore.Increment(event.OrderCount)},
		}

		// Only decrement credits_remaining for capped plans
		if ledger.CreditsRemaining != nil {
			updates = append(updates, firestore.Update{
				Path:  "credits_remaining",
				Value: firestore.Increment(-credits),
			})
			// Check if we just crossed the warning threshold (90%)
			if ledger.CreditsAllocated != nil {
				pctUsed := (ledger.CreditsUsed + credits) / *ledger.CreditsAllocated
				if pctUsed >= 0.9 && ledger.WarningSentAt == nil {
					// Flag for warning email — background job will pick this up
					now := time.Now().UTC()
					updates = append(updates, firestore.Update{
						Path:  "warning_sent_at",
						Value: now,
					})
				}
				if balanceAfter <= 0 {
					updates = append(updates, firestore.Update{
						Path:  "status",
						Value: models.LedgerQuotaExceeded,
					}, firestore.Update{
						Path:  "quota_exceeded_at",
						Value: time.Now().UTC(),
					})
				}
			}
		}

		// Per-type counters for breakdown
		switch event.Type {
		case models.UsageAITokens:
			updates = append(updates, firestore.Update{Path: "breakdown.ai_tokens", Value: firestore.Increment(credits)})
		case models.UsageAPICall:
			updates = append(updates, firestore.Update{Path: "breakdown.api_calls", Value: firestore.Increment(credits)})
			updates = append(updates, firestore.Update{Path: "api_calls_total", Value: firestore.Increment(1)})
		case models.UsageOrderSync:
			updates = append(updates, firestore.Update{Path: "breakdown.order_syncs", Value: firestore.Increment(credits)})
			updates = append(updates, firestore.Update{Path: "orders_processed", Value: firestore.Increment(event.OrderCount)})
			updates = append(updates, firestore.Update{Path: "gmv_total_gbp", Value: firestore.Increment(event.GMVValue)})
		case models.UsageListingPublish:
			updates = append(updates, firestore.Update{Path: "breakdown.listing_publish", Value: firestore.Increment(credits)})
			updates = append(updates, firestore.Update{Path: "listings_published", Value: firestore.Increment(1)})
		case models.UsageShipmentLabel:
			updates = append(updates, firestore.Update{Path: "breakdown.shipment_labels", Value: firestore.Increment(credits)})
			updates = append(updates, firestore.Update{Path: "labels_generated", Value: firestore.Increment(1)})
		case models.UsageDataExport:
			updates = append(updates, firestore.Update{Path: "breakdown.data_exports", Value: firestore.Increment(credits)})
		}

		return tx.Update(ledgerRef, updates)
	})
}

// RecordUsageAsync fires RecordUsage in a background goroutine for non-blocking
// paths where quota is not enforced (e.g. API call logging).
// Errors are logged but do not surface to the caller.
func (s *UsageService) RecordUsageAsync(tenantID string, event models.UsageEvent) {
	go func() {
		ctx, cancel := context.WithTimeout(context.Background(), 10*time.Second)
		defer cancel()
		if err := s.RecordUsage(ctx, tenantID, event); err != nil {
			if err != ErrQuotaExceeded {
				log.Printf("[usage] async record failed tenant=%s type=%s: %v", tenantID, event.Type, err)
			}
		}
	}()
}

// ============================================================================
// LEDGER INITIALISATION
// ============================================================================

// initLedger creates a new ledger document for the given tenant + period.
// Called within a transaction when no ledger exists for the current month.
func (s *UsageService) initLedger(ctx context.Context, tenantID, period string) (models.CreditLedger, error) {
	// Fetch tenant to get plan
	tenantDoc, err := s.client.Collection("tenants").Doc(tenantID).Get(ctx)
	if err != nil {
		return models.CreditLedger{}, fmt.Errorf("tenant not found: %w", err)
	}
	var tenant models.Tenant
	if err := tenantDoc.DataTo(&tenant); err != nil {
		return models.CreditLedger{}, err
	}

	// Fetch plan definition
	planDoc, err := s.client.Collection("system").Doc("plans").
		Collection("plans").Doc(string(tenant.PlanID)).Get(ctx)

	var plan models.Plan
	if err != nil || planDoc == nil {
		// Default to starter_s if plan not found
		credits := int64(10000)
		plan = models.Plan{PlanID: models.PlanStarterS, CreditsPerMonth: &credits}
	} else {
		planDoc.DataTo(&plan)
	}

	// Check for plan overrides (custom credit limit)
	overrideDoc, _ := s.client.Collection("tenants").Doc(tenantID).
		Collection("plan_overrides").Doc("current").Get(ctx)
	var override models.PlanOverride
	if overrideDoc != nil && overrideDoc.Exists() {
		overrideDoc.DataTo(&override)
	}

	// Determine credit allocation
	now := time.Now().UTC()
	periodStart, periodEnd := periodBounds(period)

	ledger := models.CreditLedger{
		TenantID:    tenantID,
		Period:      period,
		PlanID:      tenant.PlanID,
		Status:      models.LedgerActive,
		PeriodStart: periodStart,
		PeriodEnd:   periodEnd,
		CreatedAt:   now,
		UpdatedAt:   now,
	}

	// Set credit allocation based on plan
	if plan.CreditsPerMonth != nil {
		allocated := float64(*plan.CreditsPerMonth)

		// Plan override takes precedence
		if override.CustomCreditLimit != nil {
			allocated = float64(*override.CustomCreditLimit)
		}

		remaining := allocated
		ledger.CreditsAllocated = &allocated
		ledger.CreditsRemaining = &remaining
	}
	// For premium/enterprise: CreditsAllocated stays nil (unlimited)

	return ledger, nil
}

// EnsureLedger creates the ledger for the current period if it doesn't exist.
// Called at the start of each billing period by a Cloud Scheduler job,
// but also called lazily by RecordUsage as a fallback.
func (s *UsageService) EnsureLedger(ctx context.Context, tenantID string) error {
	period := currentPeriod()
	ledgerRef := s.client.Collection("tenants").Doc(tenantID).
		Collection("credit_ledger").Doc(period)

	snap, err := ledgerRef.Get(ctx)
	if err == nil && snap.Exists() {
		return nil // Already exists
	}

	ledger, err := s.initLedger(ctx, tenantID, period)
	if err != nil {
		return err
	}

	_, err = ledgerRef.Set(ctx, ledger)
	return err
}

// ============================================================================
// LEDGER QUERIES
// ============================================================================

// GetCurrentLedger returns the ledger for the current billing period.
func (s *UsageService) GetCurrentLedger(ctx context.Context, tenantID string) (*models.CreditLedger, error) {
	period := currentPeriod()
	snap, err := s.client.Collection("tenants").Doc(tenantID).
		Collection("credit_ledger").Doc(period).Get(ctx)
	if err != nil {
		if status.Code(err) == codes.NotFound {
			return nil, nil
		}
		return nil, err
	}
	var ledger models.CreditLedger
	if err := snap.DataTo(&ledger); err != nil {
		return nil, err
	}
	return &ledger, nil
}

// GetLedger returns the ledger for a specific period ("2026-02").
func (s *UsageService) GetLedger(ctx context.Context, tenantID, period string) (*models.CreditLedger, error) {
	snap, err := s.client.Collection("tenants").Doc(tenantID).
		Collection("credit_ledger").Doc(period).Get(ctx)
	if err != nil {
		if status.Code(err) == codes.NotFound {
			return nil, nil
		}
		return nil, err
	}
	var ledger models.CreditLedger
	if err := snap.DataTo(&ledger); err != nil {
		return nil, err
	}
	return &ledger, nil
}

// CheckQuota returns whether the tenant has credits remaining.
// For premium/enterprise (unlimited) always returns true.
func (s *UsageService) CheckQuota(ctx context.Context, tenantID string) (bool, error) {
	ledger, err := s.GetCurrentLedger(ctx, tenantID)
	if err != nil {
		return true, err // Fail open — don't block on ledger errors
	}
	if ledger == nil {
		return true, nil // No ledger yet — new tenant
	}
	if ledger.CreditsRemaining == nil {
		return true, nil // Unlimited plan
	}
	return *ledger.CreditsRemaining > 0, nil
}

// ============================================================================
// AUDIT LOG QUERIES
// ============================================================================

type AuditLogFilter struct {
	Type      models.UsageEventType
	SubType   string
	StartTime time.Time
	EndTime   time.Time
	Limit     int
}

// GetAuditLog returns audit log entries for a tenant, with optional filtering.
// Results are ordered by occurred_at descending (most recent first).
func (s *UsageService) GetAuditLog(ctx context.Context, tenantID string, filter AuditLogFilter) ([]models.AuditLogEntry, error) {
	limit := filter.Limit
	if limit <= 0 || limit > 500 {
		limit = 100
	}

	q := s.client.Collection("tenants").Doc(tenantID).
		Collection("audit_log").
		OrderBy("occurred_at", firestore.Desc).
		Limit(limit)

	if filter.Type != "" {
		q = q.Where("type", "==", string(filter.Type))
	}
	if !filter.StartTime.IsZero() {
		q = q.Where("occurred_at", ">=", filter.StartTime)
	}
	if !filter.EndTime.IsZero() {
		q = q.Where("occurred_at", "<=", filter.EndTime)
	}

	iter := q.Documents(ctx)
	defer iter.Stop()

	var entries []models.AuditLogEntry
	for {
		snap, err := iter.Next()
		if err != nil {
			break
		}
		var entry models.AuditLogEntry
		if err := snap.DataTo(&entry); err != nil {
			continue
		}
		entries = append(entries, entry)
	}
	return entries, nil
}

// ============================================================================
// CREDIT RATE MANAGEMENT
// ============================================================================

// getRates returns cached credit rates, refreshing from Firestore every 5 min
func (s *UsageService) getRates(ctx context.Context) (*models.CreditRates, error) {
	s.mu.RLock()
	if s.cachedRates != nil && time.Now().Before(s.ratesExpiry) {
		rates := s.cachedRates
		s.mu.RUnlock()
		return rates, nil
	}
	s.mu.RUnlock()

	// Cache miss — fetch from Firestore
	s.mu.Lock()
	defer s.mu.Unlock()

	snap, err := s.client.Collection("system").Doc("credit_rates").Get(ctx)
	if err != nil {
		if status.Code(err) == codes.NotFound {
			// No config doc — seed defaults and return them
			defaults := models.DefaultCreditRates()
			s.cachedRates = &defaults
			s.ratesExpiry = time.Now().Add(5 * time.Minute)
			go s.seedDefaultRates(defaults)
			return s.cachedRates, nil
		}
		return nil, err
	}

	var rates models.CreditRates
	if err := snap.DataTo(&rates); err != nil {
		return nil, err
	}
	s.cachedRates = &rates
	s.ratesExpiry = time.Now().Add(5 * time.Minute)
	return s.cachedRates, nil
}

// UpdateRates persists new credit rates to Firestore and invalidates the cache.
func (s *UsageService) UpdateRates(ctx context.Context, rates models.CreditRates) error {
	rates.UpdatedAt = time.Now().UTC()
	_, err := s.client.Collection("system").Doc("credit_rates").Set(ctx, rates)
	if err != nil {
		return err
	}
	// Invalidate cache
	s.mu.Lock()
	s.cachedRates = nil
	s.mu.Unlock()
	return nil
}

func (s *UsageService) seedDefaultRates(rates models.CreditRates) {
	ctx, cancel := context.WithTimeout(context.Background(), 10*time.Second)
	defer cancel()
	rates.UpdatedAt = time.Now().UTC()
	rates.UpdatedBy = "system"
	s.client.Collection("system").Doc("credit_rates").Set(ctx, rates)
}

// ============================================================================
// BILLING CALCULATION
// ============================================================================

// ComputeBill calculates the bill amount for a closed ledger period.
// Returns the GBP amount owed for the period.
func (s *UsageService) ComputeBill(ctx context.Context, tenantID, period string) (float64, error) {
	ledger, err := s.GetLedger(ctx, tenantID, period)
	if err != nil || ledger == nil {
		return 0, err
	}

	// Fetch plan
	tenantDoc, err := s.client.Collection("tenants").Doc(tenantID).Get(ctx)
	if err != nil {
		return 0, err
	}
	var tenant models.Tenant
	tenantDoc.DataTo(&tenant)

	planDoc, err := s.client.Collection("system").Doc("plans").
		Collection("plans").Doc(string(tenant.PlanID)).Get(ctx)

	var plan models.Plan
	if err == nil {
		planDoc.DataTo(&plan)
	}

	// Check for overrides
	overrideDoc, _ := s.client.Collection("tenants").Doc(tenantID).
		Collection("plan_overrides").Doc("current").Get(ctx)
	var override models.PlanOverride
	if overrideDoc != nil && overrideDoc.Exists() {
		overrideDoc.DataTo(&override)
	}

	return s.calculateBillAmount(ledger, &plan, &override), nil
}

func (s *UsageService) calculateBillAmount(ledger *models.CreditLedger, plan *models.Plan, override *models.PlanOverride) float64 {
	baseGBP := plan.PriceGBP
	if override.MonthlyBaseGBP != nil {
		baseGBP = *override.MonthlyBaseGBP
	}

	switch plan.BillingModel {
	case models.BillingModelCredits:
		// Starter — flat monthly fee (overages not charged, just blocked)
		return baseGBP

	case models.BillingModelPerOrder:
		perOrder := plan.PerOrderGBP
		if perOrder == nil {
			v := 0.10
			perOrder = &v
		}
		if override.PerOrderGBP != nil {
			perOrder = override.PerOrderGBP
		}
		orderCharge := float64(ledger.OrdersProcessed) * *perOrder
		return baseGBP + orderCharge

	case models.BillingModelGMV:
		gmvPct := plan.GMVPercent
		if gmvPct == nil {
			v := 1.0
			gmvPct = &v
		}
		if override.GMVPercent != nil {
			gmvPct = override.GMVPercent
		}
		gmvCharge := ledger.GMVTotalGBP * (*gmvPct / 100.0)
		return baseGBP + gmvCharge
	}

	return baseGBP
}

// ============================================================================
// HELPERS
// ============================================================================

func (s *UsageService) computeCredits(event models.UsageEvent, rates *models.CreditRates) float64 {
	switch event.Type {
	case models.UsageAITokens:
		return (event.Quantity / 1000.0) * rates.AITokensPer1k
	case models.UsageAPICall:
		return event.Quantity * rates.APICall
	case models.UsageOrderSync:
		return event.Quantity * rates.OrderSync
	case models.UsageListingPublish:
		return event.Quantity * rates.ListingPublish
	case models.UsageShipmentLabel:
		return event.Quantity * rates.ShipmentLabel
	case models.UsageDataExport:
		return event.Quantity * rates.DataExport
	}
	return 0
}

func (s *UsageService) rateForType(t models.UsageEventType, rates *models.CreditRates) float64 {
	switch t {
	case models.UsageAITokens:
		return rates.AITokensPer1k
	case models.UsageAPICall:
		return rates.APICall
	case models.UsageOrderSync:
		return rates.OrderSync
	case models.UsageListingPublish:
		return rates.ListingPublish
	case models.UsageShipmentLabel:
		return rates.ShipmentLabel
	case models.UsageDataExport:
		return rates.DataExport
	}
	return 0
}

// currentPeriod returns the billing period identifier for now ("2026-02")
func currentPeriod() string {
	return time.Now().UTC().Format("2006-01")
}

// periodBounds returns the start and end timestamps for a period string
func periodBounds(period string) (time.Time, time.Time) {
	t, err := time.Parse("2006-01", period)
	if err != nil {
		t = time.Now().UTC()
	}
	start := time.Date(t.Year(), t.Month(), 1, 0, 0, 0, 0, time.UTC)
	end := start.AddDate(0, 1, 0).Add(-time.Nanosecond)
	return start, end
}
