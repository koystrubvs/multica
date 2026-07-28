package handler

// client_billing_period.go — billing cycles for agency client billing
// (phase 2, migration 121).
//
// A period is one invoicing cycle of a project, derived from the project's
// billing config (anchor_day + period_months). Periods are created lazily:
// the first confirm (or the /periods/current endpoint) materializes the cycle
// that covers today. Confirmed charges attach to the open period; closing the
// period freezes total_rub for the invoice (phase 3 pushes it to Elba).
//
// Closing a period early is allowed and means "everything new goes to the
// next cycle": the successor period starts at the closed period's planned
// ends_on even when that date is still in the future.
//
// Budget alerts: in mode=budget the period's confirmed total is compared to
// budget_rub on every confirm; crossing 25/50/75/100% files an inbox
// notification to every owner/admin exactly once per threshold
// (last_alert_percent is a monotonic watermark). mode=subscription uses
// fair_use_rub the same way.

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"log/slog"
	"net/http"
	"time"

	"github.com/go-chi/chi/v5"
	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgtype"

	"github.com/multica-ai/multica/server/internal/util"
	db "github.com/multica-ai/multica/server/pkg/db/generated"
	"github.com/multica-ai/multica/server/pkg/protocol"
)

var billingAlertThresholds = []int{100, 75, 50, 25}

// periodBoundsFor computes the cycle that covers `today` from scratch (used
// only when the project has no periods yet; afterwards cycles chain from the
// previous period's ends_on). anchorDay is 1..28 so AddDate never overflows.
func periodBoundsFor(periodMonths, anchorDay int, today time.Time) (time.Time, time.Time) {
	y, m, _ := today.Date()
	start := time.Date(y, m, anchorDay, 0, 0, 0, 0, time.UTC)
	if today.Before(start) {
		start = start.AddDate(0, -periodMonths, 0)
	}
	return start, start.AddDate(0, periodMonths, 0)
}

// nextPeriodBounds chains the successor cycle from a previous period's end,
// fast-forwarding whole cycles if more than one has fully elapsed. When the
// previous end is still in the future (early manual close), the successor
// keeps that future start — new work bills into the next cycle by design.
func nextPeriodBounds(prevEnd time.Time, periodMonths int, today time.Time) (time.Time, time.Time) {
	start := prevEnd
	for !today.Before(start.AddDate(0, periodMonths, 0)) {
		start = start.AddDate(0, periodMonths, 0)
	}
	return start, start.AddDate(0, periodMonths, 0)
}

// highestCrossedThreshold returns the highest alert threshold that `percent`
// has reached and that is above the already-alerted watermark, or 0.
func highestCrossedThreshold(percent, watermark int) int {
	for _, th := range billingAlertThresholds {
		if percent >= th && th > watermark {
			return th
		}
	}
	return 0
}

// ensureOpenBillingPeriod returns the open period covering today, creating
// (and auto-closing expired predecessors) as needed.
func (h *Handler) ensureOpenBillingPeriod(ctx context.Context, cfg db.GetClientBillingConfigRow, projectID, workspaceID pgtype.UUID) (db.GetOpenClientBillingPeriodRow, error) {
	today := time.Now().UTC().Truncate(24 * time.Hour)
	pm := int(cfg.PeriodMonths)
	if pm < 1 {
		pm = 1
	}

	var start, end time.Time
	periods, err := h.Queries.ListClientBillingPeriods(ctx, projectID)
	if err != nil {
		return db.GetOpenClientBillingPeriodRow{}, err
	}
	if len(periods) > 0 {
		latest := periods[0]
		if latest.Status == "open" {
			if today.Before(latest.EndsOn.Time) {
				return db.GetOpenClientBillingPeriodRow(latest), nil
			}
			// The open period has elapsed — mark it ready for human review
			// (Business billing runs) and chain the next open cycle for new work.
			total, sumErr := h.Queries.SumConfirmedChargesInPeriod(ctx, latest.ID)
			if sumErr != nil {
				return db.GetOpenClientBillingPeriodRow{}, sumErr
			}
			if _, readyErr := h.Queries.MarkClientBillingPeriodReady(ctx, db.MarkClientBillingPeriodReadyParams{
				ID: latest.ID, TotalRub: total,
			}); readyErr != nil && !errors.Is(readyErr, pgx.ErrNoRows) {
				return db.GetOpenClientBillingPeriodRow{}, readyErr
			}
			slog.Info("billing: marked elapsed period ready for review",
				"period_id", uuidToString(latest.ID), "total_rub", total)
		}
		start, end = nextPeriodBounds(latest.EndsOn.Time, pm, today)
	} else {
		start, end = periodBoundsFor(pm, int(cfg.AnchorDay), today)
	}

	created, err := h.Queries.CreateClientBillingPeriod(ctx, db.CreateClientBillingPeriodParams{
		ProjectID:   projectID,
		WorkspaceID: workspaceID,
		StartsOn:    pgtype.Date{Time: start, Valid: true},
		EndsOn:      pgtype.Date{Time: end, Valid: true},
	})
	if err != nil {
		return db.GetOpenClientBillingPeriodRow{}, err
	}
	return db.GetOpenClientBillingPeriodRow(created), nil
}

// afterChargeConfirmed attaches a freshly confirmed charge to the open
// period, refreshes the period total and fires budget / fair-use alerts.
// Best-effort: a failure here must not fail the confirm itself.
func (h *Handler) afterChargeConfirmed(ctx context.Context, charge db.GetClientBillingChargeByIssueRow, actorID string) {
	cfg, err := h.Queries.GetClientBillingConfig(ctx, charge.ProjectID)
	if err != nil {
		slog.Warn("billing: period attach skipped, config lookup failed",
			"charge_id", uuidToString(charge.ID), "error", err)
		return
	}
	period, err := h.ensureOpenBillingPeriod(ctx, cfg, charge.ProjectID, charge.WorkspaceID)
	if err != nil {
		slog.Warn("billing: period ensure failed", "charge_id", uuidToString(charge.ID), "error", err)
		return
	}
	if err := h.Queries.AttachChargeToPeriod(ctx, db.AttachChargeToPeriodParams{
		ChargeID: charge.ID, PeriodID: period.ID,
	}); err != nil {
		slog.Warn("billing: charge attach failed", "charge_id", uuidToString(charge.ID), "error", err)
		return
	}
	total, err := h.Queries.SumConfirmedChargesInPeriod(ctx, period.ID)
	if err != nil {
		slog.Warn("billing: period sum failed", "period_id", uuidToString(period.ID), "error", err)
		return
	}
	if err := h.Queries.UpdateClientBillingPeriodTotal(ctx, db.UpdateClientBillingPeriodTotalParams{
		ID: period.ID, TotalRub: total,
	}); err != nil {
		slog.Warn("billing: period total update failed", "period_id", uuidToString(period.ID), "error", err)
	}

	limit := billingAlertLimit(cfg)
	if limit <= 0 {
		return
	}
	percent := int(total / limit * 100)
	th := highestCrossedThreshold(percent, int(period.LastAlertPercent))
	if th == 0 {
		return
	}
	if err := h.Queries.SetClientBillingPeriodAlertPercent(ctx, db.SetClientBillingPeriodAlertPercentParams{
		ID: period.ID, Percent: int32(th),
	}); err != nil {
		slog.Warn("billing: alert watermark update failed", "period_id", uuidToString(period.ID), "error", err)
		return
	}
	h.notifyBillingThreshold(ctx, cfg, period, charge.WorkspaceID, th, percent, total, limit, actorID)
}

// recalcPeriodTotal refreshes total_rub after a confirmed charge was voided.
func (h *Handler) recalcPeriodTotal(ctx context.Context, periodID pgtype.UUID) {
	if !periodID.Valid {
		return
	}
	total, err := h.Queries.SumConfirmedChargesInPeriod(ctx, periodID)
	if err != nil {
		return
	}
	_ = h.Queries.UpdateClientBillingPeriodTotal(ctx, db.UpdateClientBillingPeriodTotalParams{
		ID: periodID, TotalRub: total,
	})
}

// billingAlertLimit picks the alertable cap for the config's mode.
func billingAlertLimit(cfg db.GetClientBillingConfigRow) float64 {
	switch cfg.Mode {
	case "budget":
		return cfg.BudgetRub
	case "subscription":
		return cfg.FairUseRub
	default:
		return 0
	}
}

// notifyBillingThreshold writes a billing_budget_alert inbox item to every
// owner/admin and pushes it over the realtime bus.
func (h *Handler) notifyBillingThreshold(ctx context.Context, cfg db.GetClientBillingConfigRow, period db.GetOpenClientBillingPeriodRow, workspaceID pgtype.UUID, threshold, percent int, total, limit float64, actorID string) {
	project, err := h.Queries.GetProjectInWorkspace(ctx, db.GetProjectInWorkspaceParams{
		ID: cfg.ProjectID, WorkspaceID: workspaceID,
	})
	if err != nil {
		slog.Warn("billing: alert project lookup failed", "project_id", uuidToString(cfg.ProjectID), "error", err)
		return
	}
	members, err := h.Queries.ListMembers(ctx, workspaceID)
	if err != nil {
		slog.Warn("billing: alert member list failed", "error", err)
		return
	}
	severity := "attention"
	if threshold >= 100 {
		severity = "action_required"
	}
	limitLabel := "budget"
	if cfg.Mode == "subscription" {
		limitLabel = "fair-use cap"
	}
	details, _ := json.Marshal(map[string]any{
		"project_id": uuidToString(cfg.ProjectID),
		"period_id":  uuidToString(period.ID),
		"threshold":  threshold,
		"percent":    percent,
		"total_rub":  total,
		"limit_rub":  limit,
		"mode":       cfg.Mode,
	})
	title := fmt.Sprintf("%s: %d%% of the billing %s", project.Title, threshold, limitLabel)
	body := fmt.Sprintf("Confirmed work this period: %.0f RUB of %.0f RUB (%d%%).", total, limit, percent)

	actorUUID, _ := util.ParseUUID(actorID)
	for _, m := range members {
		if m.Role != "owner" && m.Role != "admin" {
			continue
		}
		item, err := h.Queries.CreateInboxItem(ctx, db.CreateInboxItemParams{
			WorkspaceID:   workspaceID,
			RecipientType: "member",
			RecipientID:   m.UserID,
			Type:          "billing_budget_alert",
			Severity:      severity,
			IssueID:       pgtype.UUID{},
			Title:         title,
			Body:          pgtype.Text{String: body, Valid: true},
			ActorType:     pgtype.Text{String: "member", Valid: true},
			ActorID:       actorUUID,
			Details:       details,
		})
		if err != nil {
			slog.Warn("billing: alert inbox write failed", "member_id", uuidToString(m.ID), "error", err)
			continue
		}
		h.publishBillingInboxItem(item, uuidToString(workspaceID), actorID)
	}
	slog.Info("billing: threshold alert sent",
		"project_id", uuidToString(cfg.ProjectID), "threshold", threshold, "percent", percent)
}

// publishBillingInboxItem mirrors TaskService.publishQuickCreateInbox's wire
// shape so the web inbox picks the item up live.
func (h *Handler) publishBillingInboxItem(item db.InboxItem, workspaceID, actorID string) {
	resp := map[string]any{
		"id":             util.UUIDToString(item.ID),
		"workspace_id":   util.UUIDToString(item.WorkspaceID),
		"recipient_type": item.RecipientType,
		"recipient_id":   util.UUIDToString(item.RecipientID),
		"type":           item.Type,
		"severity":       item.Severity,
		"issue_id":       util.UUIDToPtr(item.IssueID),
		"title":          item.Title,
		"body":           util.TextToPtr(item.Body),
		"read":           item.Read,
		"archived":       item.Archived,
		"created_at":     util.TimestampToString(item.CreatedAt),
		"actor_type":     util.TextToPtr(item.ActorType),
		"actor_id":       util.UUIDToPtr(item.ActorID),
		"details":        json.RawMessage(item.Details),
	}
	h.publish(protocol.EventInboxNew, workspaceID, "member", actorID, map[string]any{"item": resp})
}

// --- HTTP handlers ---

// ListProjectBillingPeriods returns the project's billing periods, newest first.
func (h *Handler) ListProjectBillingPeriods(w http.ResponseWriter, r *http.Request) {
	project, ok := h.loadProjectForBilling(w, r)
	if !ok {
		return
	}
	periods, err := h.Queries.ListClientBillingPeriods(r.Context(), project.ID)
	if err != nil {
		writeError(w, http.StatusInternalServerError, "failed to list billing periods")
		return
	}
	writeJSON(w, http.StatusOK, periods)
}

// GetProjectBillingCurrentPeriod materializes (if needed) and returns the
// cycle covering today, together with live progress numbers for the UI.
func (h *Handler) GetProjectBillingCurrentPeriod(w http.ResponseWriter, r *http.Request) {
	project, ok := h.loadProjectForBilling(w, r)
	if !ok {
		return
	}
	cfg, err := h.Queries.GetClientBillingConfig(r.Context(), project.ID)
	if errors.Is(err, pgx.ErrNoRows) {
		writeError(w, http.StatusNotFound, "billing is not configured for this project")
		return
	}
	if err != nil {
		writeError(w, http.StatusInternalServerError, "failed to load billing config")
		return
	}
	period, err := h.ensureOpenBillingPeriod(r.Context(), cfg, project.ID, project.WorkspaceID)
	if err != nil {
		slog.Error("billing: current period ensure failed", "project_id", uuidToString(project.ID), "error", err)
		writeError(w, http.StatusInternalServerError, "failed to resolve current billing period")
		return
	}
	total, err := h.Queries.SumConfirmedChargesInPeriod(r.Context(), period.ID)
	if err != nil {
		writeError(w, http.StatusInternalServerError, "failed to sum period charges")
		return
	}
	draftCount, _ := h.Queries.CountDraftChargesInProject(r.Context(), project.ID)
	limit := billingAlertLimit(cfg)
	percent := 0
	if limit > 0 {
		percent = int(total / limit * 100)
	}
	writeJSON(w, http.StatusOK, map[string]any{
		"period":          period,
		"confirmed_total": total,
		"draft_count":     draftCount,
		"limit_rub":       limit,
		"percent":         percent,
	})
}

// loadPeriodForProject resolves {periodId} within the {id} project, mirroring
// the access model of the other billing endpoints.
func (h *Handler) loadPeriodForProject(w http.ResponseWriter, r *http.Request, projectID pgtype.UUID) (db.GetClientBillingPeriodInProjectRow, bool) {
	periodUUID, ok := parseUUIDOrBadRequest(w, chi.URLParam(r, "periodId"), "period id")
	if !ok {
		return db.GetClientBillingPeriodInProjectRow{}, false
	}
	period, err := h.Queries.GetClientBillingPeriodInProject(r.Context(), db.GetClientBillingPeriodInProjectParams{
		ID: periodUUID, ProjectID: projectID,
	})
	if err != nil {
		writeError(w, http.StatusNotFound, "billing period not found")
		return db.GetClientBillingPeriodInProjectRow{}, false
	}
	return period, true
}

// ListBillingPeriodCharges returns the charges attached to one period.
func (h *Handler) ListBillingPeriodCharges(w http.ResponseWriter, r *http.Request) {
	project, ok := h.loadProjectForBilling(w, r)
	if !ok {
		return
	}
	period, ok := h.loadPeriodForProject(w, r, project.ID)
	if !ok {
		return
	}
	charges, err := h.Queries.ListClientBillingChargesByPeriod(r.Context(), period.ID)
	if err != nil {
		writeError(w, http.StatusInternalServerError, "failed to list period charges")
		return
	}
	resp := make([]clientBillingChargeListJSON, 0, len(charges))
	for _, c := range charges {
		resp = append(resp, clientBillingChargeListJSON{db.ListClientBillingChargesByProjectRow(c), json.RawMessage(c.Usage)})
	}
	writeJSON(w, http.StatusOK, resp)
}

// CloseBillingPeriod freezes the open period. Metering (v2): open disputes
// block the close; the leftovers sweep runs so nothing accrued escapes the
// cycle; every draft still attached is auto-confirmed (the human review —
// void/adjust — happens on the period charge list BEFORE close).
func (h *Handler) CloseBillingPeriod(w http.ResponseWriter, r *http.Request) {
	project, ok := h.loadProjectForBilling(w, r)
	if !ok {
		return
	}
	userUUID, ok := h.requireBillingEditor(w, r, uuidToString(project.WorkspaceID))
	if !ok {
		return
	}
	period, ok := h.loadPeriodForProject(w, r, project.ID)
	if !ok {
		return
	}
	if open, err := h.Queries.CountOpenClientBillingDisputesInProject(r.Context(), project.ID); err == nil && open > 0 {
		writeError(w, http.StatusConflict, fmt.Sprintf("resolve %d open billing dispute(s) before closing the period", open))
		return
	}
	// Sweep the unbilled tail into this cycle, then auto-confirm the drafts.
	if cfg, err := h.Queries.GetClientBillingConfig(r.Context(), project.ID); err == nil && cfg.Enabled && period.Status == "open" {
		if _, err := h.sweepProjectBilling(r.Context(), project, cfg); err != nil {
			slog.Error("billing: close-time sweep failed", "period_id", uuidToString(period.ID), "error", err)
			writeError(w, http.StatusInternalServerError, "sweep failed: "+err.Error())
			return
		}
	}
	if confirmed, err := h.Queries.ConfirmDraftChargesInPeriod(r.Context(), db.ConfirmDraftChargesInPeriodParams{
		PeriodID: period.ID, UserID: userUUID,
	}); err != nil {
		writeError(w, http.StatusInternalServerError, "failed to confirm period drafts")
		return
	} else if len(confirmed) > 0 {
		slog.Info("billing: drafts auto-confirmed at close", "period_id", uuidToString(period.ID), "count", len(confirmed))
	}
	total, err := h.Queries.SumConfirmedChargesInPeriod(r.Context(), period.ID)
	if err != nil {
		writeError(w, http.StatusInternalServerError, "failed to sum period charges")
		return
	}
	closed, err := h.Queries.CloseClientBillingPeriod(r.Context(), db.CloseClientBillingPeriodParams{
		ID: period.ID, TotalRub: total,
	})
	if errors.Is(err, pgx.ErrNoRows) {
		writeError(w, http.StatusConflict, "only open periods can be closed")
		return
	}
	if err != nil {
		writeError(w, http.StatusInternalServerError, "failed to close billing period")
		return
	}
	slog.Info("billing: period closed", "period_id", uuidToString(closed.ID), "total_rub", closed.TotalRub)

	// Closing no longer auto-pushes to Elba. Human review happens in Business
	// billing runs (`ready` → confirm), which closes then calls /invoice (or
	// the package confirm endpoint). Retry remains on POST .../invoice.
	writeJSON(w, http.StatusOK, map[string]any{"period": closed})
}

// errElbaSkipped marks "not configured for this project" — a normal state,
// not an error to surface.
var errElbaSkipped = errors.New("elba push skipped: not configured")

// pushPeriodToElba creates the счёт + акт for a closed period and flips it
// to `invoiced`. One warehouse line per confirmed charge; in subscription
// mode a negative «Скидка по абонементу» line caps the bill at the fixed fee
// while still showing the full delivered value (same trick Plane used).
func (h *Handler) pushPeriodToElba(ctx context.Context, project db.Project, period db.GetClientBillingPeriodInProjectRow) (db.SetClientBillingPeriodElbaRow, error) {
	var zero db.SetClientBillingPeriodElbaRow
	cfg, err := h.Queries.GetClientBillingConfig(ctx, project.ID)
	if err != nil || !cfg.ElbaContractorID.Valid {
		return zero, errElbaSkipped
	}
	wsCfg, err := h.Queries.GetClientBillingWorkspaceConfig(ctx, project.WorkspaceID)
	if err != nil || !wsCfg.ElbaOrgID.Valid {
		return zero, errElbaSkipped
	}
	client, err := newElbaClient()
	if errors.Is(err, errElbaNotConfigured) {
		return zero, errElbaSkipped
	}
	if err != nil {
		return zero, err
	}

	charges, err := h.Queries.ListClientBillingChargesByPeriod(ctx, period.ID)
	if err != nil {
		return zero, err
	}
	items := make([]elbaDocItem, 0, len(charges))
	var total float64
	for _, c := range charges {
		if c.Status != "confirmed" {
			continue
		}
		items = append(items, elbaDocItem{ProductName: c.IssueTitle, Quantity: 1, Price: c.PriceRub, UnitName: "усл"})
		total += c.PriceRub
	}
	if len(items) == 0 {
		return zero, errors.New("period has no confirmed charges to invoice")
	}

	opts := elbaDocOptions{
		Date: time.Now().UTC().Format("2006-01-02"),
		Comment: fmt.Sprintf("%s: работы за период %s — %s", project.Title,
			period.StartsOn.Time.Format("02.01.2006"), period.EndsOn.Time.Format("02.01.2006")),
	}
	if cfg.ElbaBankAccountID.Valid {
		opts.BankAccountID = cfg.ElbaBankAccountID.String
	} else if wsCfg.ElbaBankAccountID.Valid {
		opts.BankAccountID = wsCfg.ElbaBankAccountID.String
	}
	// Subscription: cap the bill at the fixed fee via per-line discounts — the
	// Elba v1 contract forbids the old negative discount line. Line prices keep
	// showing the full delivered value; the discounts bring the payable to the fee.
	if cfg.Mode == "subscription" && cfg.SubscriptionFeeRub > 0 && total > cfg.SubscriptionFeeRub {
		prices := make([]float64, len(items))
		for i := range items {
			prices[i] = items[i].Price
		}
		pcts := distributeSubscriptionDiscounts(prices, cfg.SubscriptionFeeRub)
		for i := range items {
			items[i].Discount = pcts[i]
		}
		opts.WithDiscount = true
	}

	orgID := wsCfg.ElbaOrgID.String
	contractorID := cfg.ElbaContractorID.String
	billID, err := client.CreateBill(ctx, orgID, contractorID, items, opts)
	if err != nil {
		return zero, fmt.Errorf("create bill: %w", err)
	}
	actID, err := client.CreateAct(ctx, orgID, contractorID, items, opts)
	if err != nil {
		// The bill exists — still record it so a retry doesn't duplicate it.
		slog.Error("billing: elba act creation failed after bill", "period_id", uuidToString(period.ID), "bill_id", billID, "error", err)
	}
	updated, uerr := h.Queries.SetClientBillingPeriodElba(ctx, db.SetClientBillingPeriodElbaParams{
		ID:        period.ID,
		InvoiceID: pgtype.Text{String: billID, Valid: billID != ""},
		ActID:     pgtype.Text{String: actID, Valid: actID != ""},
	})
	if uerr != nil {
		return zero, fmt.Errorf("record elba documents: %w", uerr)
	}
	if err != nil {
		return updated, fmt.Errorf("act creation failed (bill %s created): %w", billID, err)
	}
	slog.Info("billing: period invoiced in Elba",
		"period_id", uuidToString(period.ID), "bill_id", billID, "act_id", actID, "total_rub", total)
	return updated, nil
}

// InvoiceBillingPeriod pushes a closed period to Elba on demand (retry path
// for failed auto-invoicing, or when the contractor was linked after close).
func (h *Handler) InvoiceBillingPeriod(w http.ResponseWriter, r *http.Request) {
	project, ok := h.loadProjectForBilling(w, r)
	if !ok {
		return
	}
	if _, ok := h.requireBillingEditor(w, r, uuidToString(project.WorkspaceID)); !ok {
		return
	}
	period, ok := h.loadPeriodForProject(w, r, project.ID)
	if !ok {
		return
	}
	if period.Status != "closed" {
		writeError(w, http.StatusConflict, "only closed periods can be invoiced")
		return
	}
	invoiced, err := h.pushPeriodToElba(r.Context(), project, period)
	if errors.Is(err, errElbaSkipped) {
		writeError(w, http.StatusBadRequest, "Elba is not configured: link a contractor in the project and an organization in workspace billing settings")
		return
	}
	if err != nil {
		writeError(w, http.StatusBadGateway, err.Error())
		return
	}
	writeJSON(w, http.StatusOK, map[string]any{"period": invoiced})
}

// ReopenBillingPeriod reverts a closed (not yet paid) period to open.
func (h *Handler) ReopenBillingPeriod(w http.ResponseWriter, r *http.Request) {
	project, ok := h.loadProjectForBilling(w, r)
	if !ok {
		return
	}
	if _, ok := h.requireBillingEditor(w, r, uuidToString(project.WorkspaceID)); !ok {
		return
	}
	period, ok := h.loadPeriodForProject(w, r, project.ID)
	if !ok {
		return
	}
	reopened, err := h.Queries.ReopenClientBillingPeriod(r.Context(), period.ID)
	if errors.Is(err, pgx.ErrNoRows) {
		writeError(w, http.StatusConflict, "only closed or invoiced periods can be reopened")
		return
	}
	if err != nil {
		writeError(w, http.StatusInternalServerError, "failed to reopen billing period")
		return
	}
	writeJSON(w, http.StatusOK, reopened)
}

// MarkBillingPeriodPaid marks a closed/invoiced period as paid by the client.
func (h *Handler) MarkBillingPeriodPaid(w http.ResponseWriter, r *http.Request) {
	project, ok := h.loadProjectForBilling(w, r)
	if !ok {
		return
	}
	if _, ok := h.requireBillingEditor(w, r, uuidToString(project.WorkspaceID)); !ok {
		return
	}
	period, ok := h.loadPeriodForProject(w, r, project.ID)
	if !ok {
		return
	}
	paid, err := h.Queries.MarkClientBillingPeriodPaid(r.Context(), period.ID)
	if errors.Is(err, pgx.ErrNoRows) {
		writeError(w, http.StatusConflict, "only closed or invoiced periods can be marked paid")
		return
	}
	if err != nil {
		writeError(w, http.StatusInternalServerError, "failed to mark billing period paid")
		return
	}
	slog.Info("billing: period marked paid", "period_id", uuidToString(paid.ID))
	writeJSON(w, http.StatusOK, paid)
}
