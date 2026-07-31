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
// Since migration 134 (metering v2, see client_billing_metering.go) a charge
// is one "issue × period" line created by the SWEEP — not a done-transition
// snapshot. Usage, USD totals, the effective FX rate and the markup are still
// frozen as-of creation and never recomputed: issued invoices must not drift
// when prices or rates change.

import (
	"context"
	"encoding/json"
	"errors"
	"io"
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

// billingPricing is the fully resolved set of pricing knobs used for a
// charge: project override when set, else workspace default, else the
// hardcoded fallbacks (which match migration 122's workspace defaults).
type billingPricing struct {
	Markup          float64 `json:"markup"`
	MinPriceRub     float64 `json:"min_price_rub"`
	RoundingRub     float64 `json:"rounding_rub"`
	FxMarkupPercent float64 `json:"fx_markup_percent"`
}

var defaultBillingPricing = billingPricing{Markup: 3.0, MinPriceRub: 500, RoundingRub: 50, FxMarkupPercent: 5.0}

// resolveBillingPricing applies the inheritance chain:
// project override -> workspace default -> hardcoded fallback.
func (h *Handler) resolveBillingPricing(ctx context.Context, cfg db.GetClientBillingConfigRow, workspaceID pgtype.UUID) billingPricing {
	p := defaultBillingPricing
	if ws, err := h.Queries.GetClientBillingWorkspaceConfig(ctx, workspaceID); err == nil {
		p = billingPricing{ws.Markup, ws.MinPriceRub, ws.RoundingRub, ws.FxMarkupPercent}
	}
	if cfg.MarkupSet {
		p.Markup = cfg.Markup
	}
	if cfg.MinPriceRubSet {
		p.MinPriceRub = cfg.MinPriceRub
	}
	if cfg.RoundingRubSet {
		p.RoundingRub = cfg.RoundingRub
	}
	if cfg.FxMarkupPercentSet {
		p.FxMarkupPercent = cfg.FxMarkupPercent
	}
	return p
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

// The v1 done-transition hook (maybeCreateBillingChargeForIssue) is gone:
// since migration 134 billing is METERED — issues accrue usage in any status
// and the sweep (client_billing_metering.go) bills the unbilled delta on
// demand and at period close.

// requireBillingEditor gates billing mutations (config writes, charge
// confirm/void/adjust) to owner and admin.
//
// It used to admit "any member except guests", which was written before the
// agency had employees in the workspace: ordinary members could edit the
// markup, close periods and issue real invoices in Elba. Money now follows the
// role — see callerIsBillingStaff.
//
// Returns the acting user's UUID, or ok=false after writing the error.
func (h *Handler) requireBillingEditor(w http.ResponseWriter, r *http.Request, workspaceID string) (pgtype.UUID, bool) {
	member, ok := h.requireBillingStaffMember(w, r, workspaceID)
	if !ok {
		return pgtype.UUID{}, false
	}
	return member.UserID, true
}

// callerIsBillingStaff reports whether the requester may see money: rouble
// prices, the agency markup, charges, invoices, and raw USD compute cost.
// Owner and admin qualify; ordinary members and guests do not.
//
// Money is gated on the ROLE, while project visibility is gated separately on
// the member's project scope — two independent axes on purpose. An admin
// restricted to a few projects still sees agency-wide money, so `admin` is
// handed out deliberately, not as a seniority badge.
//
// The role is resolved from requestUserID, i.e. the server-set X-User-ID
// header. For a task token that is the runtime OWNER, so an agent dispatched
// by an ordinary member would pass this check — the RequireHumanActor
// middleware on the billing routes is what stops that, not this function.
func (h *Handler) callerIsBillingStaff(r *http.Request, workspaceID string) bool {
	member, err := h.getWorkspaceMember(r.Context(), requestUserID(r), workspaceID)
	return err == nil && (member.Role == "owner" || member.Role == "admin")
}

// requireBillingStaffMember is callerIsBillingStaff plus the 403, returning the
// resolved member for handlers that need the actor id.
func (h *Handler) requireBillingStaffMember(w http.ResponseWriter, r *http.Request, workspaceID string) (db.Member, bool) {
	member, err := h.getWorkspaceMember(r.Context(), requestUserID(r), workspaceID)
	if err != nil {
		writeError(w, http.StatusForbidden, "billing requires workspace membership")
		return db.Member{}, false
	}
	if member.Role != "owner" && member.Role != "admin" {
		writeError(w, http.StatusForbidden, "billing requires owner or admin role")
		return db.Member{}, false
	}
	return member, true
}

// requireBillingStaff is the void-returning form for handlers that only need
// the gate.
func (h *Handler) requireBillingStaff(w http.ResponseWriter, r *http.Request, workspaceID string) bool {
	_, ok := h.requireBillingStaffMember(w, r, workspaceID)
	return ok
}

// --- JSON shaping ---

// clientBillingChargeJSON re-types the snapshot's raw JSONB bytes so they
// serialize as the JSON array they are — a bare []byte field would be
// marshaled as base64 by encoding/json. The outer Usage (shallower depth)
// wins over the embedded row's field of the same JSON name.
type clientBillingChargeJSON struct {
	db.GetClientBillingChargeByIssueRow
	Usage json.RawMessage `json:"usage"`
}

func chargeJSON(row db.GetClientBillingChargeByIssueRow) clientBillingChargeJSON {
	return clientBillingChargeJSON{row, json.RawMessage(row.Usage)}
}

type clientBillingChargeListJSON struct {
	db.ListClientBillingChargesByProjectRow
	Usage json.RawMessage `json:"usage"`
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
	ElbaContractorID   *string  `json:"elba_contractor_id"`
	ElbaBankAccountID  *string  `json:"elba_bank_account_id"`
}

// clientBillingConfigJSON is the API shape of a project billing config.
// Pricing knobs are pointers: null = inherited from the workspace; the
// resolved values the snapshot will actually use are in `effective`.
type clientBillingConfigJSON struct {
	ProjectID          string             `json:"project_id"`
	Enabled            bool               `json:"enabled"`
	Mode               string             `json:"mode"`
	Markup             *float64           `json:"markup"`
	MinPriceRub        *float64           `json:"min_price_rub"`
	RoundingRub        *float64           `json:"rounding_rub"`
	FxMarkupPercent    *float64           `json:"fx_markup_percent"`
	BudgetRub          float64            `json:"budget_rub"`
	SubscriptionFeeRub float64            `json:"subscription_fee_rub"`
	FairUseRub         float64            `json:"fair_use_rub"`
	PeriodMonths       int32              `json:"period_months"`
	AnchorDay          int32              `json:"anchor_day"`
	ElbaContractorID   *string            `json:"elba_contractor_id"`
	ElbaBankAccountID  *string            `json:"elba_bank_account_id"`
	Effective          billingPricing     `json:"effective"`
	CreatedAt          pgtype.Timestamptz `json:"created_at"`
	UpdatedAt          pgtype.Timestamptz `json:"updated_at"`
}

func optFloat(set bool, v float64) *float64 {
	if !set {
		return nil
	}
	return &v
}

func (h *Handler) billingConfigJSON(ctx context.Context, cfg db.GetClientBillingConfigRow, workspaceID pgtype.UUID) clientBillingConfigJSON {
	return clientBillingConfigJSON{
		ProjectID:          uuidToString(cfg.ProjectID),
		Enabled:            cfg.Enabled,
		Mode:               cfg.Mode,
		Markup:             optFloat(cfg.MarkupSet, cfg.Markup),
		MinPriceRub:        optFloat(cfg.MinPriceRubSet, cfg.MinPriceRub),
		RoundingRub:        optFloat(cfg.RoundingRubSet, cfg.RoundingRub),
		FxMarkupPercent:    optFloat(cfg.FxMarkupPercentSet, cfg.FxMarkupPercent),
		BudgetRub:          cfg.BudgetRub,
		SubscriptionFeeRub: cfg.SubscriptionFeeRub,
		FairUseRub:         cfg.FairUseRub,
		PeriodMonths:       cfg.PeriodMonths,
		AnchorDay:          cfg.AnchorDay,
		ElbaContractorID:   textToPtr(cfg.ElbaContractorID),
		ElbaBankAccountID:  textToPtr(cfg.ElbaBankAccountID),
		Effective:          h.resolveBillingPricing(ctx, cfg, workspaceID),
		CreatedAt:          cfg.CreatedAt,
		UpdatedAt:          cfg.UpdatedAt,
	}
}

// loadProjectForBilling resolves {id} -> project within the caller's
// workspace and gates the caller to billing staff (owner/admin).
//
// The gate lives here rather than in each handler because every one of this
// function's callers is a money endpoint — project billing config, periods,
// sweeps, disputes, contractor wiring. Reads were previously ungated, so any
// member could read the agency markup and the Elba contractor of any project.
// A 403 (not 404) is correct: the member can legitimately see the project,
// they just may not see its money.
func (h *Handler) loadProjectForBilling(w http.ResponseWriter, r *http.Request) (db.Project, bool) {
	idUUID, ok := parseUUIDOrBadRequest(w, chi.URLParam(r, "id"), "project id")
	if !ok {
		return db.Project{}, false
	}
	workspaceID := h.resolveWorkspaceID(r)
	wsUUID, ok := parseUUIDOrBadRequest(w, workspaceID, "workspace id")
	if !ok {
		return db.Project{}, false
	}
	if !h.requireBillingStaff(w, r, workspaceID) {
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
	writeJSON(w, http.StatusOK, h.billingConfigJSON(r.Context(), cfg, project.WorkspaceID))
}

// PutProjectBillingConfig creates or updates the project's billing config.
// Pricing knobs are tri-state: omitted = keep current, explicit null = reset
// to "inherit from workspace", a number = project-level override.
func (h *Handler) PutProjectBillingConfig(w http.ResponseWriter, r *http.Request) {
	project, ok := h.loadProjectForBilling(w, r)
	if !ok {
		return
	}
	if _, ok := h.requireBillingEditor(w, r, uuidToString(project.WorkspaceID)); !ok {
		return
	}
	bodyBytes, err := io.ReadAll(r.Body)
	if err != nil {
		writeError(w, http.StatusBadRequest, "failed to read request body")
		return
	}
	var req clientBillingConfigRequest
	if err := json.Unmarshal(bodyBytes, &req); err != nil {
		writeError(w, http.StatusBadRequest, "invalid request body")
		return
	}
	var rawFields map[string]json.RawMessage
	_ = json.Unmarshal(bodyBytes, &rawFields)
	fieldNull := func(key string) bool {
		raw, ok := rawFields[key]
		return ok && string(raw) == "null"
	}

	// Start from the current config; a fresh config inherits everything.
	params := db.UpsertClientBillingConfigParams{
		ProjectID:    project.ID,
		Enabled:      true,
		Mode:         "postpaid",
		PeriodMonths: 1,
		AnchorDay:    1,
	}
	if cur, err := h.Queries.GetClientBillingConfig(r.Context(), project.ID); err == nil {
		params.Enabled = cur.Enabled
		params.Mode = cur.Mode
		params.PeriodMonths = cur.PeriodMonths
		params.AnchorDay = cur.AnchorDay
		params.Markup = pgtype.Float8{Float64: cur.Markup, Valid: cur.MarkupSet}
		params.MinPriceRub = pgtype.Float8{Float64: cur.MinPriceRub, Valid: cur.MinPriceRubSet}
		params.RoundingRub = pgtype.Float8{Float64: cur.RoundingRub, Valid: cur.RoundingRubSet}
		params.FxMarkupPercent = pgtype.Float8{Float64: cur.FxMarkupPercent, Valid: cur.FxMarkupPercentSet}
		params.BudgetRub = pgtype.Float8{Float64: cur.BudgetRub, Valid: cur.BudgetRub > 0}
		params.SubscriptionFeeRub = pgtype.Float8{Float64: cur.SubscriptionFeeRub, Valid: cur.SubscriptionFeeRub > 0}
		params.FairUseRub = pgtype.Float8{Float64: cur.FairUseRub, Valid: cur.FairUseRub > 0}
		params.ElbaContractorID = cur.ElbaContractorID
		params.ElbaBankAccountID = cur.ElbaBankAccountID
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

	// Tri-state pricing overrides.
	switch {
	case fieldNull("markup"):
		params.Markup = pgtype.Float8{}
	case req.Markup != nil:
		if *req.Markup <= 0 {
			writeError(w, http.StatusBadRequest, "markup must be positive")
			return
		}
		params.Markup = pgtype.Float8{Float64: *req.Markup, Valid: true}
	}
	switch {
	case fieldNull("min_price_rub"):
		params.MinPriceRub = pgtype.Float8{}
	case req.MinPriceRub != nil:
		if *req.MinPriceRub < 0 {
			writeError(w, http.StatusBadRequest, "min_price_rub must be >= 0")
			return
		}
		params.MinPriceRub = pgtype.Float8{Float64: *req.MinPriceRub, Valid: true}
	}
	switch {
	case fieldNull("rounding_rub"):
		params.RoundingRub = pgtype.Float8{}
	case req.RoundingRub != nil:
		if *req.RoundingRub < 0 {
			writeError(w, http.StatusBadRequest, "rounding_rub must be >= 0")
			return
		}
		params.RoundingRub = pgtype.Float8{Float64: *req.RoundingRub, Valid: true}
	}
	switch {
	case fieldNull("fx_markup_percent"):
		params.FxMarkupPercent = pgtype.Float8{}
	case req.FxMarkupPercent != nil:
		params.FxMarkupPercent = pgtype.Float8{Float64: *req.FxMarkupPercent, Valid: true}
	}

	if req.BudgetRub != nil || fieldNull("budget_rub") {
		v := 0.0
		if req.BudgetRub != nil {
			v = *req.BudgetRub
		}
		params.BudgetRub = pgtype.Float8{Float64: v, Valid: v > 0}
	}
	if req.SubscriptionFeeRub != nil || fieldNull("subscription_fee_rub") {
		v := 0.0
		if req.SubscriptionFeeRub != nil {
			v = *req.SubscriptionFeeRub
		}
		params.SubscriptionFeeRub = pgtype.Float8{Float64: v, Valid: v > 0}
	}
	if req.FairUseRub != nil || fieldNull("fair_use_rub") {
		v := 0.0
		if req.FairUseRub != nil {
			v = *req.FairUseRub
		}
		params.FairUseRub = pgtype.Float8{Float64: v, Valid: v > 0}
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
	if fieldNull("elba_contractor_id") {
		params.ElbaContractorID = pgtype.Text{}
	} else if req.ElbaContractorID != nil {
		params.ElbaContractorID = pgtype.Text{String: *req.ElbaContractorID, Valid: *req.ElbaContractorID != ""}
	}
	if fieldNull("elba_bank_account_id") {
		params.ElbaBankAccountID = pgtype.Text{}
	} else if req.ElbaBankAccountID != nil {
		params.ElbaBankAccountID = pgtype.Text{String: *req.ElbaBankAccountID, Valid: *req.ElbaBankAccountID != ""}
	}

	cfg, err := h.Queries.UpsertClientBillingConfig(r.Context(), params)
	if err != nil {
		slog.Error("billing: config upsert failed", "project_id", uuidToString(project.ID), "error", err)
		writeError(w, http.StatusInternalServerError, "failed to save billing config")
		return
	}
	slog.Info("billing: config saved", "project_id", uuidToString(project.ID), "mode", cfg.Mode)
	writeJSON(w, http.StatusOK, h.billingConfigJSON(r.Context(), db.GetClientBillingConfigRow(cfg), project.WorkspaceID))
}

// --- Workspace billing defaults ---

type clientBillingWorkspaceConfigRequest struct {
	Markup            *float64 `json:"markup"`
	MinPriceRub       *float64 `json:"min_price_rub"`
	RoundingRub       *float64 `json:"rounding_rub"`
	FxMarkupPercent   *float64 `json:"fx_markup_percent"`
	ElbaOrgID         *string  `json:"elba_org_id"`
	ElbaBankAccountID *string  `json:"elba_bank_account_id"`
}

type clientBillingWorkspaceConfigJSON struct {
	WorkspaceID       string   `json:"workspace_id"`
	Markup            float64  `json:"markup"`
	MinPriceRub       float64  `json:"min_price_rub"`
	RoundingRub       float64  `json:"rounding_rub"`
	FxMarkupPercent   float64  `json:"fx_markup_percent"`
	ElbaOrgID         *string  `json:"elba_org_id"`
	ElbaBankAccountID *string  `json:"elba_bank_account_id"`
	// Exists is false when the workspace has never saved defaults and the
	// values above are the hardcoded fallbacks.
	Exists bool `json:"exists"`
}

// requireBillingAdmin gates workspace-level billing settings to owner/admin.
func (h *Handler) requireBillingAdmin(w http.ResponseWriter, r *http.Request, workspaceID string) bool {
	member, err := h.getWorkspaceMember(r.Context(), requestUserID(r), workspaceID)
	if err != nil || (member.Role != "owner" && member.Role != "admin") {
		writeError(w, http.StatusForbidden, "workspace billing settings require owner or admin role")
		return false
	}
	return true
}

// GetWorkspaceBillingConfig returns the workspace pricing defaults (the
// hardcoded fallbacks when never saved — see `exists`).
func (h *Handler) GetWorkspaceBillingConfig(w http.ResponseWriter, r *http.Request) {
	wsID := h.resolveWorkspaceID(r)
	wsUUID, ok := parseUUIDOrBadRequest(w, wsID, "workspace id")
	if !ok {
		return
	}
	if !h.requireBillingStaff(w, r, wsID) {
		return
	}
	out := clientBillingWorkspaceConfigJSON{
		WorkspaceID:     wsID,
		Markup:          defaultBillingPricing.Markup,
		MinPriceRub:     defaultBillingPricing.MinPriceRub,
		RoundingRub:     defaultBillingPricing.RoundingRub,
		FxMarkupPercent: defaultBillingPricing.FxMarkupPercent,
	}
	if cfg, err := h.Queries.GetClientBillingWorkspaceConfig(r.Context(), wsUUID); err == nil {
		out.Markup = cfg.Markup
		out.MinPriceRub = cfg.MinPriceRub
		out.RoundingRub = cfg.RoundingRub
		out.FxMarkupPercent = cfg.FxMarkupPercent
		out.ElbaOrgID = textToPtr(cfg.ElbaOrgID)
		out.ElbaBankAccountID = textToPtr(cfg.ElbaBankAccountID)
		out.Exists = true
	}
	writeJSON(w, http.StatusOK, out)
}

// PutWorkspaceBillingConfig saves the workspace pricing defaults + Elba
// organization wiring. Owner/admin only.
func (h *Handler) PutWorkspaceBillingConfig(w http.ResponseWriter, r *http.Request) {
	wsID := h.resolveWorkspaceID(r)
	wsUUID, ok := parseUUIDOrBadRequest(w, wsID, "workspace id")
	if !ok {
		return
	}
	if !h.requireBillingAdmin(w, r, wsID) {
		return
	}
	var req clientBillingWorkspaceConfigRequest
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
		writeError(w, http.StatusBadRequest, "invalid request body")
		return
	}

	params := db.UpsertClientBillingWorkspaceConfigParams{
		WorkspaceID:     wsUUID,
		Markup:          defaultBillingPricing.Markup,
		MinPriceRub:     defaultBillingPricing.MinPriceRub,
		RoundingRub:     defaultBillingPricing.RoundingRub,
		FxMarkupPercent: defaultBillingPricing.FxMarkupPercent,
	}
	if cur, err := h.Queries.GetClientBillingWorkspaceConfig(r.Context(), wsUUID); err == nil {
		params.Markup = cur.Markup
		params.MinPriceRub = cur.MinPriceRub
		params.RoundingRub = cur.RoundingRub
		params.FxMarkupPercent = cur.FxMarkupPercent
		params.ElbaOrgID = cur.ElbaOrgID
		params.ElbaBankAccountID = cur.ElbaBankAccountID
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
	if req.ElbaOrgID != nil {
		params.ElbaOrgID = pgtype.Text{String: *req.ElbaOrgID, Valid: *req.ElbaOrgID != ""}
	}
	if req.ElbaBankAccountID != nil {
		params.ElbaBankAccountID = pgtype.Text{String: *req.ElbaBankAccountID, Valid: *req.ElbaBankAccountID != ""}
	}

	cfg, err := h.Queries.UpsertClientBillingWorkspaceConfig(r.Context(), params)
	if err != nil {
		slog.Error("billing: workspace config upsert failed", "workspace_id", wsID, "error", err)
		writeError(w, http.StatusInternalServerError, "failed to save workspace billing config")
		return
	}
	writeJSON(w, http.StatusOK, clientBillingWorkspaceConfigJSON{
		WorkspaceID:       wsID,
		Markup:            cfg.Markup,
		MinPriceRub:       cfg.MinPriceRub,
		RoundingRub:       cfg.RoundingRub,
		FxMarkupPercent:   cfg.FxMarkupPercent,
		ElbaOrgID:         textToPtr(cfg.ElbaOrgID),
		ElbaBankAccountID: textToPtr(cfg.ElbaBankAccountID),
		Exists:            true,
	})
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
	resp := make([]clientBillingChargeListJSON, 0, len(charges))
	for _, c := range charges {
		resp = append(resp, clientBillingChargeListJSON{c, json.RawMessage(c.Usage)})
	}
	writeJSON(w, http.StatusOK, resp)
}

// --- Issue billing charge ---

// GetIssueBillingCharge returns the issue's price snapshot (404 when none).
// Billing staff only — the snapshot is the client's price.
func (h *Handler) GetIssueBillingCharge(w http.ResponseWriter, r *http.Request) {
	issue, ok := h.loadIssueForUser(w, r, chi.URLParam(r, "id"))
	if !ok {
		return
	}
	if !h.requireBillingStaff(w, r, uuidToString(issue.WorkspaceID)) {
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
	writeJSON(w, http.StatusOK, chargeJSON(charge))
}

// ConfirmIssueBillingCharge moves a draft charge to confirmed, recording the
// acting user. Confirmed charges are what billing periods aggregate.
func (h *Handler) ConfirmIssueBillingCharge(w http.ResponseWriter, r *http.Request) {
	issue, ok := h.loadIssueForUser(w, r, chi.URLParam(r, "id"))
	if !ok {
		return
	}
	chargeUUID, ok := parseUUIDOrBadRequest(w, chi.URLParam(r, "chargeId"), "charge id")
	if !ok {
		return
	}
	userUUID, ok := h.requireBillingEditor(w, r, uuidToString(issue.WorkspaceID))
	if !ok {
		return
	}
	charge, err := h.Queries.ConfirmClientBillingCharge(r.Context(), db.ConfirmClientBillingChargeParams{
		ChargeID: chargeUUID,
		IssueID:  issue.ID,
		UserID:   userUUID,
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
	// Phase 2: attach to the open billing period, refresh its total and fire
	// budget / fair-use threshold alerts. Best-effort by design.
	h.afterChargeConfirmed(r.Context(), db.GetClientBillingChargeByIssueRow(charge), requestUserID(r))
	writeJSON(w, http.StatusOK, chargeJSON(db.GetClientBillingChargeByIssueRow(charge)))
}

// VoidIssueBillingCharge cancels a charge (draft or confirmed) so it never
// reaches an invoice. The row is kept for audit.
func (h *Handler) VoidIssueBillingCharge(w http.ResponseWriter, r *http.Request) {
	issue, ok := h.loadIssueForUser(w, r, chi.URLParam(r, "id"))
	if !ok {
		return
	}
	chargeUUID, ok := parseUUIDOrBadRequest(w, chi.URLParam(r, "chargeId"), "charge id")
	if !ok {
		return
	}
	if _, ok := h.requireBillingEditor(w, r, uuidToString(issue.WorkspaceID)); !ok {
		return
	}
	charge, err := h.Queries.VoidClientBillingCharge(r.Context(), db.VoidClientBillingChargeParams{
		ChargeID: chargeUUID,
		IssueID:  issue.ID,
	})
	if errors.Is(err, pgx.ErrNoRows) {
		writeError(w, http.StatusConflict, "charge is already void (or does not exist)")
		return
	}
	if err != nil {
		writeError(w, http.StatusInternalServerError, "failed to void charge")
		return
	}
	// A voided charge that was already attached must leave the period total.
	h.recalcPeriodTotal(r.Context(), charge.PeriodID)
	writeJSON(w, http.StatusOK, chargeJSON(db.GetClientBillingChargeByIssueRow(charge)))
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
	chargeUUID, ok := parseUUIDOrBadRequest(w, chi.URLParam(r, "chargeId"), "charge id")
	if !ok {
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
		ChargeID: chargeUUID,
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
	writeJSON(w, http.StatusOK, chargeJSON(db.GetClientBillingChargeByIssueRow(charge)))
}
