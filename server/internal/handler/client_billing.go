package handler

// client_billing.go — agency-side client billing (Phase 1, migration 120).
//
// Domain: what CLIENTS owe the agency for done issues. Entirely separate from
// cloud_billing.go (what this instance owes the Multica cloud for compute).
//
// Pricing model (multica/AGENCY_BILLING_SPEC.md in the ops repo):
//
//	nocache_usd = (input + cache_read + cache_write) x InputPerM/1M
//	            + output x OutputPerM/1M
//	price_rub   = round_up(max(nocache_usd x fx x markup, min_price), rounding)
//
// All input-class tokens are billed at the full input list price — the cache
// discount the agency actually receives from the provider is its margin and
// is deliberately not passed through to the client.
//
// A charge is a frozen snapshot taken when an issue transitions to `done`
// (hooked from UpdateIssue / BatchUpdateIssues). Usage, USD totals, the
// effective FX rate and the markup are captured as-of that moment and never
// recomputed: issued invoices must not drift when prices or rates change.

import (
	"context"
	"encoding/json"
	"errors"
	"log/slog"
	"math"
	"net/http"
	"time"

	"github.com/go-chi/chi/v5"
	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgtype"

	obsmetrics "github.com/multica-ai/multica/server/internal/metrics"
	db "github.com/multica-ai/multica/server/pkg/db/generated"
)

// billingUsageLine is one (provider, model) slice of an issue's token usage,
// priced at no-cache list prices. Serialized into client_billing_charge.usage
// as the audit trail behind the snapshot total.
type billingUsageLine struct {
	Provider         string  `json:"provider"`
	Model            string  `json:"model"`
	InputTokens      int64   `json:"input_tokens"`
	OutputTokens     int64   `json:"output_tokens"`
	CacheReadTokens  int64   `json:"cache_read_tokens"`
	CacheWriteTokens int64   `json:"cache_write_tokens"`
	NocacheUsd       float64 `json:"nocache_usd"`
	// Priced is false when the model is missing from the price table; its
	// tokens then contribute $0 and the line is kept for visibility.
	Priced bool `json:"priced"`
}

// computeBillingUsage prices per-model usage rows at no-cache list prices.
// Returns the priced lines, the USD total, and the total token count (used to
// skip charge creation for issues with no agent work at all).
func computeBillingUsage(rows []db.ListIssueUsageByModelRow) ([]billingUsageLine, float64, int64) {
	lines := make([]billingUsageLine, 0, len(rows))
	var totalUSD float64
	var totalTokens int64
	for _, r := range rows {
		line := billingUsageLine{
			Provider:         r.Provider,
			Model:            r.Model,
			InputTokens:      r.InputTokens,
			OutputTokens:     r.OutputTokens,
			CacheReadTokens:  r.CacheReadTokens,
			CacheWriteTokens: r.CacheWriteTokens,
		}
		totalTokens += r.InputTokens + r.OutputTokens + r.CacheReadTokens + r.CacheWriteTokens
		if price, ok := obsmetrics.PriceForModelAlias(r.Model); ok {
			inputClass := r.InputTokens + r.CacheReadTokens + r.CacheWriteTokens
			line.NocacheUsd = float64(inputClass)*price.InputPerM/1_000_000 +
				float64(r.OutputTokens)*price.OutputPerM/1_000_000
			line.Priced = true
			totalUSD += line.NocacheUsd
		}
		lines = append(lines, line)
	}
	return lines, totalUSD, totalTokens
}

// computePriceRub applies markup, the per-task floor and ceil-rounding to a
// USD no-cache total. rounding <= 0 disables the rounding step.
func computePriceRub(nocacheUSD, fxRate, markup, minPrice, rounding float64) float64 {
	raw := nocacheUSD * fxRate * markup
	if raw < minPrice {
		raw = minPrice
	}
	if rounding > 0 {
		// 1e-9 guards against float artifacts bumping an exact multiple up a
		// whole extra rounding step (e.g. 1500.0000000001 -> 1550).
		raw = math.Ceil(raw/rounding-1e-9) * rounding
	}
	return math.Round(raw*100) / 100
}

// latestFxRubPerUsd returns the most recent CBR USD->RUB rate, lazily
// backfilling fx_rate_daily (same mechanism as GET /api/fx/daily). Falls back
// to fxFallbackRubPerUsd when both CBR and the table yield nothing.
func (h *Handler) latestFxRubPerUsd(ctx context.Context) float64 {
	now := time.Now().UTC()
	from := now.AddDate(0, 0, -14).Format(isoLayout)
	to := now.Format(isoLayout)
	if err := h.ensureFxRange(ctx, from, to); err != nil {
		slog.Warn("billing: fx backfill failed, using stored rates", "error", err)
	}
	stored, err := h.loadFxRange(ctx, from, to)
	if err != nil || len(stored) == 0 {
		slog.Warn("billing: no fx rates available, using fallback", "fallback", fxFallbackRubPerUsd)
		return fxFallbackRubPerUsd
	}
	return stored[len(stored)-1].UsdRub
}

// maybeCreateBillingChargeForIssue creates a draft price snapshot for an
// issue that just transitioned to `done`. Best-effort: billing must never
// fail the status update itself. No-ops when the issue has no project, the
// project has no enabled billing config, a charge already exists (reopen ->
// re-done keeps the original snapshot), or the issue has zero agent usage
// (nothing AI-priced to bill; a manual charge can be added in a later phase).
func (h *Handler) maybeCreateBillingChargeForIssue(ctx context.Context, issue db.Issue) {
	if !issue.ProjectID.Valid {
		return
	}
	cfg, err := h.Queries.GetClientBillingConfig(ctx, issue.ProjectID)
	if errors.Is(err, pgx.ErrNoRows) {
		return
	}
	if err != nil {
		slog.Warn("billing: config lookup failed", "issue_id", uuidToString(issue.ID), "error", err)
		return
	}
	if !cfg.Enabled {
		return
	}
	if _, err := h.Queries.GetClientBillingChargeByIssue(ctx, issue.ID); err == nil {
		return // snapshot already taken
	} else if !errors.Is(err, pgx.ErrNoRows) {
		slog.Warn("billing: charge lookup failed", "issue_id", uuidToString(issue.ID), "error", err)
		return
	}
	rows, err := h.Queries.ListIssueUsageByModel(ctx, issue.ID)
	if err != nil {
		slog.Warn("billing: usage lookup failed", "issue_id", uuidToString(issue.ID), "error", err)
		return
	}
	lines, totalUSD, totalTokens := computeBillingUsage(rows)
	if totalTokens == 0 {
		slog.Info("billing: skipping issue with no agent usage", "issue_id", uuidToString(issue.ID))
		return
	}
	fx := h.latestFxRubPerUsd(ctx) * (1 + cfg.FxMarkupPercent/100)
	fx = math.Round(fx*10000) / 10000
	price := computePriceRub(totalUSD, fx, cfg.Markup, cfg.MinPriceRub, cfg.RoundingRub)
	usageJSON, err := json.Marshal(lines)
	if err != nil {
		slog.Warn("billing: usage marshal failed", "issue_id", uuidToString(issue.ID), "error", err)
		return
	}
	charge, err := h.Queries.CreateClientBillingCharge(ctx, db.CreateClientBillingChargeParams{
		IssueID:     issue.ID,
		ProjectID:   issue.ProjectID,
		WorkspaceID: issue.WorkspaceID,
		Usage:       usageJSON,
		NocacheUsd:  totalUSD,
		FxRate:      fx,
		Markup:      cfg.Markup,
		PriceRub:    price,
	})
	if err != nil {
		// A unique-violation race with a concurrent done-transition is benign.
		slog.Warn("billing: charge insert failed", "issue_id", uuidToString(issue.ID), "error", err)
		return
	}
	slog.Info("billing: draft charge created",
		"issue_id", uuidToString(issue.ID),
		"charge_id", uuidToString(charge.ID),
		"nocache_usd", totalUSD, "fx_rate", fx, "price_rub", price)
}

// requireBillingEditor gates billing mutations (config writes, charge
// confirm/void/adjust) to real workspace staff: any member except guests.
// Returns the acting user's UUID, or ok=false after writing the error.
func (h *Handler) requireBillingEditor(w http.ResponseWriter, r *http.Request, workspaceID string) (pgtype.UUID, bool) {
	userID := requestUserID(r)
	member, err := h.getWorkspaceMember(r.Context(), userID, workspaceID)
	if err != nil {
		writeError(w, http.StatusForbidden, "billing requires workspace membership")
		return pgtype.UUID{}, false
	}
	if member.Role == "guest" {
		writeError(w, http.StatusForbidden, "guests cannot manage billing")
		return pgtype.UUID{}, false
	}
	return member.UserID, true
}

// --- Project billing config ---

type clientBillingConfigRequest struct {
	Enabled            *bool    `json:"enabled"`
	Mode               *string  `json:"mode"`
	Markup             *float64 `json:"markup"`
	MinPriceRub        *float64 `json:"min_price_rub"`
	RoundingRub        *float64 `json:"rounding_rub"`
	FxMarkupPercent    *float64 `json:"fx_markup_percent"`
	BudgetRub          *float64 `json:"budget_rub"`
	SubscriptionFeeRub *float64 `json:"subscription_fee_rub"`
	FairUseRub         *float64 `json:"fair_use_rub"`
	PeriodMonths       *int32   `json:"period_months"`
	AnchorDay          *int32   `json:"anchor_day"`
}

// loadProjectForBilling resolves {id} -> project within the caller's
// workspace, mirroring GetProject's access model.
func (h *Handler) loadProjectForBilling(w http.ResponseWriter, r *http.Request) (db.Project, bool) {
	idUUID, ok := parseUUIDOrBadRequest(w, chi.URLParam(r, "id"), "project id")
	if !ok {
		return db.Project{}, false
	}
	wsUUID, ok := parseUUIDOrBadRequest(w, h.resolveWorkspaceID(r), "workspace id")
	if !ok {
		return db.Project{}, false
	}
	project, err := h.Queries.GetProjectInWorkspace(r.Context(), db.GetProjectInWorkspaceParams{
		ID: idUUID, WorkspaceID: wsUUID,
	})
	if err != nil {
		writeError(w, http.StatusNotFound, "project not found")
		return db.Project{}, false
	}
	return project, true
}

// GetProjectBillingConfig returns the project's billing config, or 404 when
// billing was never configured for it.
func (h *Handler) GetProjectBillingConfig(w http.ResponseWriter, r *http.Request) {
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
	writeJSON(w, http.StatusOK, cfg)
}

// PutProjectBillingConfig creates or updates the project's billing config.
// Omitted fields keep their current value (or the documented default when the
// config is being created).
func (h *Handler) PutProjectBillingConfig(w http.ResponseWriter, r *http.Request) {
	project, ok := h.loadProjectForBilling(w, r)
	if !ok {
		return
	}
	if _, ok := h.requireBillingEditor(w, r, uuidToString(project.WorkspaceID)); !ok {
		return
	}
	var req clientBillingConfigRequest
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
		writeError(w, http.StatusBadRequest, "invalid request body")
		return
	}

	// Start from current config or defaults (mirrors migration 120 DEFAULTs).
	params := db.UpsertClientBillingConfigParams{
		ProjectID:       project.ID,
		Enabled:         true,
		Mode:            "postpaid",
		Markup:          3.0,
		MinPriceRub:     500,
		RoundingRub:     50,
		FxMarkupPercent: 5.0,
		PeriodMonths:    1,
		AnchorDay:       1,
	}
	if cur, err := h.Queries.GetClientBillingConfig(r.Context(), project.ID); err == nil {
		params.Enabled = cur.Enabled
		params.Mode = cur.Mode
		params.Markup = cur.Markup
		params.MinPriceRub = cur.MinPriceRub
		params.RoundingRub = cur.RoundingRub
		params.FxMarkupPercent = cur.FxMarkupPercent
		params.PeriodMonths = cur.PeriodMonths
		params.AnchorDay = cur.AnchorDay
		if cur.BudgetRub > 0 {
			params.BudgetRub = pgtype.Float8{Float64: cur.BudgetRub, Valid: true}
		}
		if cur.SubscriptionFeeRub > 0 {
			params.SubscriptionFeeRub = pgtype.Float8{Float64: cur.SubscriptionFeeRub, Valid: true}
		}
		if cur.FairUseRub > 0 {
			params.FairUseRub = pgtype.Float8{Float64: cur.FairUseRub, Valid: true}
		}
	}

	if req.Enabled != nil {
		params.Enabled = *req.Enabled
	}
	if req.Mode != nil {
		switch *req.Mode {
		case "postpaid", "budget", "subscription":
			params.Mode = *req.Mode
		default:
			writeError(w, http.StatusBadRequest, "mode must be one of: postpaid, budget, subscription")
			return
		}
	}
	if req.Markup != nil {
		if *req.Markup <= 0 {
			writeError(w, http.StatusBadRequest, "markup must be positive")
			return
		}
		params.Markup = *req.Markup
	}
	if req.MinPriceRub != nil {
		if *req.MinPriceRub < 0 {
			writeError(w, http.StatusBadRequest, "min_price_rub must be >= 0")
			return
		}
		params.MinPriceRub = *req.MinPriceRub
	}
	if req.RoundingRub != nil {
		if *req.RoundingRub < 0 {
			writeError(w, http.StatusBadRequest, "rounding_rub must be >= 0")
			return
		}
		params.RoundingRub = *req.RoundingRub
	}
	if req.FxMarkupPercent != nil {
		params.FxMarkupPercent = *req.FxMarkupPercent
	}
	if req.BudgetRub != nil {
		params.BudgetRub = pgtype.Float8{Float64: *req.BudgetRub, Valid: *req.BudgetRub > 0}
	}
	if req.SubscriptionFeeRub != nil {
		params.SubscriptionFeeRub = pgtype.Float8{Float64: *req.SubscriptionFeeRub, Valid: *req.SubscriptionFeeRub > 0}
	}
	if req.FairUseRub != nil {
		params.FairUseRub = pgtype.Float8{Float64: *req.FairUseRub, Valid: *req.FairUseRub > 0}
	}
	if req.PeriodMonths != nil {
		if *req.PeriodMonths < 1 || *req.PeriodMonths > 12 {
			writeError(w, http.StatusBadRequest, "period_months must be between 1 and 12")
			return
		}
		params.PeriodMonths = *req.PeriodMonths
	}
	if req.AnchorDay != nil {
		if *req.AnchorDay < 1 || *req.AnchorDay > 28 {
			writeError(w, http.StatusBadRequest, "anchor_day must be between 1 and 28")
			return
		}
		params.AnchorDay = *req.AnchorDay
	}

	cfg, err := h.Queries.UpsertClientBillingConfig(r.Context(), params)
	if err != nil {
		slog.Error("billing: config upsert failed", "project_id", uuidToString(project.ID), "error", err)
		writeError(w, http.StatusInternalServerError, "failed to save billing config")
		return
	}
	slog.Info("billing: config saved", "project_id", uuidToString(project.ID), "mode", cfg.Mode, "markup", cfg.Markup)
	writeJSON(w, http.StatusOK, cfg)
}

// ListProjectBillingCharges returns the project's charges, optionally
// filtered by ?status=draft|confirmed|void.
func (h *Handler) ListProjectBillingCharges(w http.ResponseWriter, r *http.Request) {
	project, ok := h.loadProjectForBilling(w, r)
	if !ok {
		return
	}
	status := r.URL.Query().Get("status")
	var statusArg pgtype.Text
	switch status {
	case "":
		// no filter
	case "draft", "confirmed", "void":
		statusArg = pgtype.Text{String: status, Valid: true}
	default:
		writeError(w, http.StatusBadRequest, "status must be one of: draft, confirmed, void")
		return
	}
	charges, err := h.Queries.ListClientBillingChargesByProject(r.Context(), db.ListClientBillingChargesByProjectParams{
		ProjectID: project.ID,
		Status:    statusArg,
	})
	if err != nil {
		writeError(w, http.StatusInternalServerError, "failed to list charges")
		return
	}
	writeJSON(w, http.StatusOK, charges)
}

// --- Issue billing charge ---

// GetIssueBillingCharge returns the issue's price snapshot (404 when none).
func (h *Handler) GetIssueBillingCharge(w http.ResponseWriter, r *http.Request) {
	issue, ok := h.loadIssueForUser(w, r, chi.URLParam(r, "id"))
	if !ok {
		return
	}
	charge, err := h.Queries.GetClientBillingChargeByIssue(r.Context(), issue.ID)
	if errors.Is(err, pgx.ErrNoRows) {
		writeError(w, http.StatusNotFound, "no billing charge for this issue")
		return
	}
	if err != nil {
		writeError(w, http.StatusInternalServerError, "failed to load billing charge")
		return
	}
	writeJSON(w, http.StatusOK, charge)
}

// ConfirmIssueBillingCharge moves a draft charge to confirmed, recording the
// acting user. Confirmed charges are what billing periods aggregate.
func (h *Handler) ConfirmIssueBillingCharge(w http.ResponseWriter, r *http.Request) {
	issue, ok := h.loadIssueForUser(w, r, chi.URLParam(r, "id"))
	if !ok {
		return
	}
	userUUID, ok := h.requireBillingEditor(w, r, uuidToString(issue.WorkspaceID))
	if !ok {
		return
	}
	charge, err := h.Queries.ConfirmClientBillingCharge(r.Context(), db.ConfirmClientBillingChargeParams{
		IssueID: issue.ID,
		UserID:  userUUID,
	})
	if errors.Is(err, pgx.ErrNoRows) {
		writeError(w, http.StatusConflict, "charge is not in draft status (or does not exist)")
		return
	}
	if err != nil {
		writeError(w, http.StatusInternalServerError, "failed to confirm charge")
		return
	}
	slog.Info("billing: charge confirmed", "issue_id", uuidToString(issue.ID), "price_rub", charge.PriceRub)
	writeJSON(w, http.StatusOK, charge)
}

// VoidIssueBillingCharge cancels a charge (draft or confirmed) so it never
// reaches an invoice. The row is kept for audit.
func (h *Handler) VoidIssueBillingCharge(w http.ResponseWriter, r *http.Request) {
	issue, ok := h.loadIssueForUser(w, r, chi.URLParam(r, "id"))
	if !ok {
		return
	}
	if _, ok := h.requireBillingEditor(w, r, uuidToString(issue.WorkspaceID)); !ok {
		return
	}
	charge, err := h.Queries.VoidClientBillingCharge(r.Context(), issue.ID)
	if errors.Is(err, pgx.ErrNoRows) {
		writeError(w, http.StatusConflict, "charge is already void (or does not exist)")
		return
	}
	if err != nil {
		writeError(w, http.StatusInternalServerError, "failed to void charge")
		return
	}
	writeJSON(w, http.StatusOK, charge)
}

type adjustChargeRequest struct {
	PriceRub float64 `json:"price_rub"`
	Reason   string  `json:"reason"`
}

// AdjustIssueBillingCharge overrides a draft charge's price with a mandatory
// human-readable reason (special deals, goodwill discounts, ...).
func (h *Handler) AdjustIssueBillingCharge(w http.ResponseWriter, r *http.Request) {
	issue, ok := h.loadIssueForUser(w, r, chi.URLParam(r, "id"))
	if !ok {
		return
	}
	if _, ok := h.requireBillingEditor(w, r, uuidToString(issue.WorkspaceID)); !ok {
		return
	}
	var req adjustChargeRequest
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
		writeError(w, http.StatusBadRequest, "invalid request body")
		return
	}
	if req.PriceRub < 0 {
		writeError(w, http.StatusBadRequest, "price_rub must be >= 0")
		return
	}
	if req.Reason == "" {
		writeError(w, http.StatusBadRequest, "reason is required for a manual price override")
		return
	}
	charge, err := h.Queries.AdjustClientBillingCharge(r.Context(), db.AdjustClientBillingChargeParams{
		IssueID:  issue.ID,
		PriceRub: req.PriceRub,
		Reason:   pgtype.Text{String: req.Reason, Valid: true},
	})
	if errors.Is(err, pgx.ErrNoRows) {
		writeError(w, http.StatusConflict, "only draft charges can be adjusted")
		return
	}
	if err != nil {
		writeError(w, http.StatusInternalServerError, "failed to adjust charge")
		return
	}
	slog.Info("billing: charge adjusted", "issue_id", uuidToString(issue.ID), "price_rub", charge.PriceRub, "reason", req.Reason)
	writeJSON(w, http.StatusOK, charge)
}
