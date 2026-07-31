package handler

// client_billing_metering.go — client billing v2: metering (migration 134,
// spec §10 in multica/AGENCY_BILLING_SPEC.md).
//
// v1 froze one price snapshot per issue on the done-transition. v2 treats the
// charge table as a LEDGER: an issue accrues usage in any status, and the
// sweep bills the delta between its cumulative token counters and the sum of
// its previous charge snapshots (void rows advance that watermark too, so an
// excluded delta never resurfaces). The sweep runs on demand and automatically
// inside period close; the human review happens on the period's charge list
// BEFORE close (void the junk, adjust the special deals), and close
// auto-confirms whatever drafts remain.
//
// GET /issues/{id}/billing/cost is the live-cost surface for the issue card:
// cumulative usage priced at the CURRENT knobs, billed/unbilled split, the
// charge ledger, and — for the workspace OWNER ONLY — the internal cache-
// discounted cost (the agency margin must never leak to clients).
//
// Disputes: a client (member or SitePing guest) contests an issue's accrued
// cost with a mandatory reason; owner/admin resolve keep / exclude / adjust.
// Open disputes block period close.

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"log/slog"
	"math"
	"net/http"

	"github.com/go-chi/chi/v5"
	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgtype"

	"github.com/multica-ai/multica/server/internal/util"
	obsmetrics "github.com/multica-ai/multica/server/internal/metrics"
	db "github.com/multica-ai/multica/server/pkg/db/generated"
)

// --- usage arithmetic ---

type billingUsageKey struct{ provider, model string }

// addChargeUsage accumulates one charge's usage snapshot (the JSONB audit
// trail) into a per-(provider,model) token baseline.
func addChargeUsage(dst map[billingUsageKey]*billingUsageLine, raw []byte) {
	var lines []billingUsageLine
	if err := json.Unmarshal(raw, &lines); err != nil {
		slog.Warn("billing: unparsable charge usage snapshot skipped", "error", err)
		return
	}
	for _, l := range lines {
		k := billingUsageKey{l.Provider, l.Model}
		acc, ok := dst[k]
		if !ok {
			acc = &billingUsageLine{Provider: l.Provider, Model: l.Model}
			dst[k] = acc
		}
		acc.InputTokens += l.InputTokens
		acc.OutputTokens += l.OutputTokens
		acc.CacheReadTokens += l.CacheReadTokens
		acc.CacheWriteTokens += l.CacheWriteTokens
	}
}

func clampSub(cur, billed int64) int64 {
	if d := cur - billed; d > 0 {
		return d
	}
	return 0
}

// subtractBaseline returns the unbilled usage delta: cumulative counters minus
// the charge-ledger baseline, clamped at zero per token class.
func subtractBaseline(rows []db.ListIssueUsageByModelRow, baseline map[billingUsageKey]*billingUsageLine) []db.ListIssueUsageByModelRow {
	out := make([]db.ListIssueUsageByModelRow, 0, len(rows))
	for _, r := range rows {
		b := baseline[billingUsageKey{r.Provider, r.Model}]
		if b != nil {
			r.InputTokens = clampSub(r.InputTokens, b.InputTokens)
			r.OutputTokens = clampSub(r.OutputTokens, b.OutputTokens)
			r.CacheReadTokens = clampSub(r.CacheReadTokens, b.CacheReadTokens)
			r.CacheWriteTokens = clampSub(r.CacheWriteTokens, b.CacheWriteTokens)
		}
		if r.InputTokens+r.OutputTokens+r.CacheReadTokens+r.CacheWriteTokens > 0 {
			out = append(out, r)
		}
	}
	return out
}

// computeInternalUsageUSD prices usage at the ACTUAL provider tariffs — cache
// reads/writes at their discounted rates. This is the agency's cost side and
// is exposed to the workspace owner only.
func computeInternalUsageUSD(rows []db.ListIssueUsageByModelRow) float64 {
	var total float64
	for _, r := range rows {
		if price, ok := obsmetrics.PriceForModelAlias(r.Model); ok {
			total += float64(r.InputTokens)*price.InputPerM/1_000_000 +
				float64(r.CacheReadTokens)*price.CacheReadPerM/1_000_000 +
				float64(r.CacheWriteTokens)*price.CacheWritePerM/1_000_000 +
				float64(r.OutputTokens)*price.OutputPerM/1_000_000
		}
	}
	return total
}

// issueBilledBaseline loads the charge-ledger watermark for one issue.
func (h *Handler) issueBilledBaseline(ctx context.Context, issueID pgtype.UUID) (map[billingUsageKey]*billingUsageLine, error) {
	usages, err := h.Queries.ListChargeUsageByIssue(ctx, issueID)
	if err != nil {
		return nil, err
	}
	baseline := make(map[billingUsageKey]*billingUsageLine)
	for _, raw := range usages {
		addChargeUsage(baseline, raw)
	}
	return baseline, nil
}

// --- live cost (issue card) ---

type issueBillingChargeSlimJSON struct {
	ID             string             `json:"id"`
	PeriodID       *string            `json:"period_id"`
	PriceRub       float64            `json:"price_rub"`
	NocacheUsd     float64            `json:"nocache_usd"`
	Status         string             `json:"status"`
	Source         string             `json:"source"`
	AdjustedReason *string            `json:"adjusted_reason"`
	CreatedAt      pgtype.Timestamptz `json:"created_at"`
}

type issueBillingInternalJSON struct {
	Usd float64 `json:"usd"`
	Rub float64 `json:"rub"`
}

type clientBillingDisputeJSON struct {
	ID                string             `json:"id"`
	IssueID           string             `json:"issue_id"`
	ProjectID         string             `json:"project_id"`
	OpenedByType      string             `json:"opened_by_type"`
	OpenedBy          *string            `json:"opened_by"`
	Reason            string             `json:"reason"`
	Status            string             `json:"status"`
	Resolution        *string            `json:"resolution"`
	ResolutionComment *string            `json:"resolution_comment"`
	ResolvedBy        *string            `json:"resolved_by"`
	ResolvedAt        pgtype.Timestamptz `json:"resolved_at"`
	CreatedAt         pgtype.Timestamptz `json:"created_at"`
	IssueTitle        string             `json:"issue_title,omitempty"`
}

func disputeJSON(d db.ClientBillingDispute) clientBillingDisputeJSON {
	return clientBillingDisputeJSON{
		ID:                uuidToString(d.ID),
		IssueID:           uuidToString(d.IssueID),
		ProjectID:         uuidToString(d.ProjectID),
		OpenedByType:      d.OpenedByType,
		OpenedBy:          util.UUIDToPtr(d.OpenedBy),
		Reason:            d.Reason,
		Status:            d.Status,
		Resolution:        textToPtr(d.Resolution),
		ResolutionComment: textToPtr(d.ResolutionComment),
		ResolvedBy:        util.UUIDToPtr(d.ResolvedBy),
		ResolvedAt:        d.ResolvedAt,
		CreatedAt:         d.CreatedAt,
	}
}

type issueBillingCostJSON struct {
	BillingEnabled bool               `json:"billing_enabled"`
	Usage          []billingUsageLine `json:"usage"`
	TotalTokens    int64              `json:"total_tokens"`
	// Cumulative no-cache USD and its RUB estimate at the CURRENT knobs (the
	// frozen numbers live in the charges below).
	NocacheUsd  float64 `json:"nocache_usd"`
	FxRate      float64 `json:"fx_rate"`
	Markup      float64 `json:"markup"`
	EstimateRub float64 `json:"estimate_rub"`
	// Billed = sum of non-void charge prices; unbilled = delta past the ledger
	// watermark, priced like the next sweep would.
	BilledRub           float64                      `json:"billed_rub"`
	UnbilledNocacheUsd  float64                      `json:"unbilled_nocache_usd"`
	UnbilledEstimateRub float64                      `json:"unbilled_estimate_rub"`
	Charges             []issueBillingChargeSlimJSON `json:"charges"`
	Dispute             *clientBillingDisputeJSON    `json:"dispute,omitempty"`
	// Owner-only: the agency's actual cache-discounted cost. Never shown to
	// members/guests — this is the margin.
	Internal *issueBillingInternalJSON `json:"internal,omitempty"`
}

// GetIssueBillingCost is the live-cost endpoint behind the issue card's
// billing block. Works for issues in ANY status.
func (h *Handler) GetIssueBillingCost(w http.ResponseWriter, r *http.Request) {
	issue, ok := h.loadIssueForUser(w, r, chi.URLParam(r, "id"))
	if !ok {
		return
	}
	// Billing staff only. This payload is the money: rouble estimate, markup,
	// FX, the charge ledger. Employees keep their per-issue TOKEN counts —
	// those come from GET /api/issues/{id}/usage, which is a different
	// endpoint and stays open to members.
	if !h.requireBillingStaff(w, r, uuidToString(issue.WorkspaceID)) {
		return
	}
	out := issueBillingCostJSON{Usage: []billingUsageLine{}, Charges: []issueBillingChargeSlimJSON{}}

	var cfg db.GetClientBillingConfigRow
	if issue.ProjectID.Valid {
		if c, err := h.Queries.GetClientBillingConfig(r.Context(), issue.ProjectID); err == nil && c.Enabled {
			cfg = c
			out.BillingEnabled = true
		}
	}
	rows, err := h.Queries.ListIssueUsageByModel(r.Context(), issue.ID)
	if err != nil {
		writeError(w, http.StatusInternalServerError, "failed to load issue usage")
		return
	}
	lines, totalUSD, totalTokens := computeBillingUsage(rows)
	out.Usage = lines
	out.TotalTokens = totalTokens
	out.NocacheUsd = totalUSD

	pricing := h.resolveBillingPricing(r.Context(), cfg, issue.WorkspaceID)
	fx := h.latestFxRubPerUsd(r.Context()) * (1 + pricing.FxMarkupPercent/100)
	fx = math.Round(fx*10000) / 10000
	out.FxRate = fx
	out.Markup = pricing.Markup
	if totalTokens > 0 {
		out.EstimateRub = computePriceRub(totalUSD, fx, pricing.Markup, pricing.MinPriceRub, pricing.RoundingRub)
	}

	charges, err := h.Queries.ListClientBillingChargesByIssue(r.Context(), issue.ID)
	if err != nil {
		writeError(w, http.StatusInternalServerError, "failed to load issue charges")
		return
	}
	baseline := make(map[billingUsageKey]*billingUsageLine)
	for _, c := range charges {
		addChargeUsage(baseline, c.Usage)
		if c.Status != "void" {
			out.BilledRub += c.PriceRub
		}
		out.Charges = append(out.Charges, issueBillingChargeSlimJSON{
			ID:             uuidToString(c.ID),
			PeriodID:       util.UUIDToPtr(c.PeriodID),
			PriceRub:       c.PriceRub,
			NocacheUsd:     c.NocacheUsd,
			Status:         c.Status,
			Source:         c.Source,
			AdjustedReason: textToPtr(c.AdjustedReason),
			CreatedAt:      c.CreatedAt,
		})
	}
	delta := subtractBaseline(rows, baseline)
	_, unbilledUSD, unbilledTokens := computeBillingUsage(delta)
	out.UnbilledNocacheUsd = unbilledUSD
	if unbilledTokens > 0 && out.BillingEnabled {
		out.UnbilledEstimateRub = computePriceRub(unbilledUSD, fx, pricing.Markup, pricing.MinPriceRub, pricing.RoundingRub)
	}

	if d, err := h.Queries.GetOpenClientBillingDisputeByIssue(r.Context(), issue.ID); err == nil {
		dj := disputeJSON(d)
		out.Dispute = &dj
	}

	// Owner-only internal cost: raw CBR rate (no client markup) — this is what
	// the tokens actually cost us.
	if member, err := h.getWorkspaceMember(r.Context(), requestUserID(r), uuidToString(issue.WorkspaceID)); err == nil && member.Role == "owner" {
		internalUSD := computeInternalUsageUSD(rows)
		baseFx := h.latestFxRubPerUsd(r.Context())
		out.Internal = &issueBillingInternalJSON{
			Usd: math.Round(internalUSD*10000) / 10000,
			Rub: math.Round(internalUSD*baseFx*100) / 100,
		}
	}

	writeJSON(w, http.StatusOK, out)
}

// --- sweep ---

// sweepIssueDelta prices and inserts one issue's unbilled delta as a draft
// charge attached to periodID (pass an invalid UUID to leave it unattached).
// Returns created=false when the delta is empty.
func (h *Handler) sweepIssueDelta(ctx context.Context, issueID, projectID, workspaceID pgtype.UUID, delta []db.ListIssueUsageByModelRow, pricing billingPricing, fx float64, periodID pgtype.UUID) (db.CreateClientBillingChargeRow, bool, error) {
	lines, totalUSD, totalTokens := computeBillingUsage(delta)
	if totalTokens == 0 {
		return db.CreateClientBillingChargeRow{}, false, nil
	}
	price := computePriceRub(totalUSD, fx, pricing.Markup, pricing.MinPriceRub, pricing.RoundingRub)
	usageJSON, err := json.Marshal(lines)
	if err != nil {
		return db.CreateClientBillingChargeRow{}, false, err
	}
	charge, err := h.Queries.CreateClientBillingCharge(ctx, db.CreateClientBillingChargeParams{
		IssueID:     issueID,
		ProjectID:   projectID,
		WorkspaceID: workspaceID,
		Usage:       usageJSON,
		NocacheUsd:  totalUSD,
		FxRate:      fx,
		Markup:      pricing.Markup,
		PriceRub:    price,
		Source:      "sweep",
		PeriodID:    periodID,
	})
	if err != nil {
		return db.CreateClientBillingChargeRow{}, false, err
	}
	return charge, true, nil
}

// internalIssuesInProject returns the issues the workspace has marked as
// internal work. The client does not pay for those, so their usage never
// becomes a charge.
func (h *Handler) internalIssuesInProject(ctx context.Context, projectID pgtype.UUID) (map[string]bool, error) {
	rows, err := h.DB.Query(ctx, `
		SELECT i.id::text FROM issue i WHERE i.project_id = $1 AND `+issueMarkedInternalSQL, projectID)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	internal := make(map[string]bool)
	for rows.Next() {
		var id string
		if err := rows.Scan(&id); err != nil {
			return nil, err
		}
		internal[id] = true
	}
	return internal, rows.Err()
}

// confirmPeriodDrafts turns the period's drafts into the invoice line-up. The
// "Биллинг" property is filled in after an issue closes, so work swept before
// anyone marked it internal is voided here — this is the last point where the
// period total can still be corrected.
func (h *Handler) confirmPeriodDrafts(ctx context.Context, periodID, userID pgtype.UUID) (confirmed, voided int, err error) {
	tag, err := h.DB.Exec(ctx, `
		UPDATE client_billing_charge c
		SET status = 'void', adjusted_reason = 'внутренняя задача', updated_at = now()
		FROM issue i
		WHERE i.id = c.issue_id AND c.period_id = $1 AND c.status <> 'void' AND `+issueMarkedInternalSQL, periodID)
	if err != nil {
		return 0, 0, fmt.Errorf("void internal charges: %w", err)
	}
	voided = int(tag.RowsAffected())
	if voided > 0 {
		slog.Info("billing: internal charges voided before confirm",
			"period_id", uuidToString(periodID), "count", voided)
	}
	rows, err := h.Queries.ConfirmDraftChargesInPeriod(ctx, db.ConfirmDraftChargesInPeriodParams{
		UserID: userID, PeriodID: periodID,
	})
	if err != nil {
		return 0, voided, err
	}
	return len(rows), voided, nil
}

// sweepProjectBilling walks every issue of the project (ANY status), bills the
// unbilled usage delta into draft charges attached to the open period, and
// returns them. Prices and the FX rate freeze at sweep time.
func (h *Handler) sweepProjectBilling(ctx context.Context, project db.Project, cfg db.GetClientBillingConfigRow) ([]db.CreateClientBillingChargeRow, error) {
	period, err := h.ensureOpenBillingPeriod(ctx, cfg, project.ID, project.WorkspaceID)
	if err != nil {
		return nil, fmt.Errorf("ensure open period: %w", err)
	}
	projRows, err := h.Queries.ListProjectIssueUsageByModel(ctx, project.ID)
	if err != nil {
		return nil, fmt.Errorf("project usage: %w", err)
	}
	chargeUsages, err := h.Queries.ListChargeUsageByProject(ctx, project.ID)
	if err != nil {
		return nil, fmt.Errorf("charge ledger: %w", err)
	}
	internal, err := h.internalIssuesInProject(ctx, project.ID)
	if err != nil {
		return nil, fmt.Errorf("internal issues: %w", err)
	}

	baselines := make(map[string]map[billingUsageKey]*billingUsageLine)
	for _, cu := range chargeUsages {
		key := uuidToString(cu.IssueID)
		if baselines[key] == nil {
			baselines[key] = make(map[billingUsageKey]*billingUsageLine)
		}
		addChargeUsage(baselines[key], cu.Usage)
	}

	byIssue := make(map[string][]db.ListIssueUsageByModelRow)
	issueUUIDs := make(map[string]pgtype.UUID)
	for _, r := range projRows {
		key := uuidToString(r.IssueID)
		issueUUIDs[key] = r.IssueID
		byIssue[key] = append(byIssue[key], db.ListIssueUsageByModelRow{
			Provider:         r.Provider,
			Model:            r.Model,
			InputTokens:      r.InputTokens,
			OutputTokens:     r.OutputTokens,
			CacheReadTokens:  r.CacheReadTokens,
			CacheWriteTokens: r.CacheWriteTokens,
		})
	}

	pricing := h.resolveBillingPricing(ctx, cfg, project.WorkspaceID)
	fx := h.latestFxRubPerUsd(ctx) * (1 + pricing.FxMarkupPercent/100)
	fx = math.Round(fx*10000) / 10000

	created := []db.CreateClientBillingChargeRow{}
	skipped := 0
	for key, rows := range byIssue {
		if internal[key] {
			skipped++
			continue
		}
		baseline := baselines[key]
		if baseline == nil {
			baseline = map[billingUsageKey]*billingUsageLine{}
		}
		delta := subtractBaseline(rows, baseline)
		charge, ok, err := h.sweepIssueDelta(ctx, issueUUIDs[key], project.ID, project.WorkspaceID, delta, pricing, fx, period.ID)
		if err != nil {
			return created, fmt.Errorf("sweep issue %s: %w", key, err)
		}
		if ok {
			created = append(created, charge)
		}
	}
	slog.Info("billing: sweep completed", "project_id", uuidToString(project.ID),
		"period_id", uuidToString(period.ID), "charges_created", len(created), "internal_skipped", skipped)
	return created, nil
}

// SweepProjectBilling handles POST /projects/{id}/billing/sweep.
func (h *Handler) SweepProjectBilling(w http.ResponseWriter, r *http.Request) {
	project, ok := h.loadProjectForBilling(w, r)
	if !ok {
		return
	}
	if _, ok := h.requireBillingEditor(w, r, uuidToString(project.WorkspaceID)); !ok {
		return
	}
	cfg, err := h.Queries.GetClientBillingConfig(r.Context(), project.ID)
	if errors.Is(err, pgx.ErrNoRows) || (err == nil && !cfg.Enabled) {
		writeError(w, http.StatusConflict, "billing is not enabled for this project")
		return
	}
	if err != nil {
		writeError(w, http.StatusInternalServerError, "failed to load billing config")
		return
	}
	created, err := h.sweepProjectBilling(r.Context(), project, cfg)
	if err != nil {
		slog.Error("billing: sweep failed", "project_id", uuidToString(project.ID), "error", err)
		writeError(w, http.StatusInternalServerError, "sweep failed: "+err.Error())
		return
	}
	writeJSON(w, http.StatusOK, map[string]any{"created": len(created)})
}

// --- disputes ---

type openDisputeRequest struct {
	Reason string `json:"reason"`
}

// OpenIssueBillingDispute lets the client (member or guest) contest an issue's
// accrued cost. One open dispute per issue; owner/admin get an inbox item.
func (h *Handler) OpenIssueBillingDispute(w http.ResponseWriter, r *http.Request) {
	issue, ok := h.loadIssueForUser(w, r, chi.URLParam(r, "id"))
	if !ok {
		return
	}
	if !issue.ProjectID.Valid {
		writeError(w, http.StatusConflict, "issue has no project")
		return
	}
	var req openDisputeRequest
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil || req.Reason == "" {
		writeError(w, http.StatusBadRequest, "reason is required")
		return
	}
	wsID := uuidToString(issue.WorkspaceID)
	member, err := h.getWorkspaceMember(r.Context(), requestUserID(r), wsID)
	if err != nil {
		writeError(w, http.StatusForbidden, "workspace membership required")
		return
	}
	openedByType := "member"
	if member.Role == "guest" {
		openedByType = "guest"
	}
	created, err := h.Queries.CreateClientBillingDispute(r.Context(), db.CreateClientBillingDisputeParams{
		IssueID:      issue.ID,
		ProjectID:    issue.ProjectID,
		WorkspaceID:  issue.WorkspaceID,
		OpenedByType: openedByType,
		OpenedBy:     member.UserID,
		Reason:       req.Reason,
	})
	if err != nil {
		// unique partial index: one open dispute per issue
		writeError(w, http.StatusConflict, "an open dispute already exists for this issue")
		return
	}
	slog.Info("billing: dispute opened", "issue_id", uuidToString(issue.ID), "by", openedByType)
	h.notifyBillingDispute(r.Context(), issue, req.Reason, uuidToString(created.ID))
	writeJSON(w, http.StatusOK, disputeJSON(db.ClientBillingDispute(created)))
}

// notifyBillingDispute files a billing_dispute inbox item to every owner/admin.
func (h *Handler) notifyBillingDispute(ctx context.Context, issue db.Issue, reason, disputeID string) {
	members, err := h.Queries.ListMembers(ctx, issue.WorkspaceID)
	if err != nil {
		slog.Warn("billing: dispute notify member list failed", "error", err)
		return
	}
	details, _ := json.Marshal(map[string]any{
		"issue_id":   uuidToString(issue.ID),
		"project_id": uuidToString(issue.ProjectID),
		"dispute_id": disputeID,
		"reason":     reason,
	})
	title := fmt.Sprintf("Billing disputed: %s", issue.Title)
	for _, m := range members {
		if m.Role != "owner" && m.Role != "admin" {
			continue
		}
		item, err := h.Queries.CreateInboxItem(ctx, db.CreateInboxItemParams{
			WorkspaceID:   issue.WorkspaceID,
			RecipientType: "member",
			RecipientID:   m.UserID,
			Type:          "billing_dispute",
			Severity:      "action_required",
			IssueID:       issue.ID,
			Title:         title,
			Body:          pgtype.Text{String: reason, Valid: true},
			ActorType:     pgtype.Text{String: "member", Valid: true},
			Details:       details,
		})
		if err != nil {
			slog.Warn("billing: dispute inbox write failed", "member_id", uuidToString(m.ID), "error", err)
			continue
		}
		h.publishBillingInboxItem(item, uuidToString(issue.WorkspaceID), "")
	}
}

// ListProjectBillingDisputes returns the project's disputes (?status=open|resolved).
func (h *Handler) ListProjectBillingDisputes(w http.ResponseWriter, r *http.Request) {
	project, ok := h.loadProjectForBilling(w, r)
	if !ok {
		return
	}
	status := r.URL.Query().Get("status")
	var statusArg pgtype.Text
	switch status {
	case "":
	case "open", "resolved":
		statusArg = pgtype.Text{String: status, Valid: true}
	default:
		writeError(w, http.StatusBadRequest, "status must be open or resolved")
		return
	}
	rows, err := h.Queries.ListClientBillingDisputesByProject(r.Context(), db.ListClientBillingDisputesByProjectParams{
		ProjectID: project.ID,
		Status:    statusArg,
	})
	if err != nil {
		writeError(w, http.StatusInternalServerError, "failed to list disputes")
		return
	}
	resp := make([]clientBillingDisputeJSON, 0, len(rows))
	for _, d := range rows {
		j := disputeJSON(db.ClientBillingDispute{
			ID: d.ID, IssueID: d.IssueID, ProjectID: d.ProjectID, WorkspaceID: d.WorkspaceID,
			OpenedByType: d.OpenedByType, OpenedBy: d.OpenedBy, Reason: d.Reason, Status: d.Status,
			Resolution: d.Resolution, ResolutionComment: d.ResolutionComment,
			ResolvedBy: d.ResolvedBy, ResolvedAt: d.ResolvedAt, CreatedAt: d.CreatedAt,
		})
		j.IssueTitle = d.IssueTitle
		resp = append(resp, j)
	}
	writeJSON(w, http.StatusOK, resp)
}

type resolveDisputeRequest struct {
	Resolution string  `json:"resolution"` // keep | exclude | adjust
	Comment    string  `json:"comment"`
	PriceRub   float64 `json:"price_rub"` // adjust only
}

// ResolveProjectBillingDispute applies the owner's verdict:
//   - keep:    price stands, dispute closes with a comment back to the client;
//   - exclude: every pending draft of the issue is voided AND the remaining
//     unbilled delta is swept into an immediately-voided charge, so the
//     excluded tokens advance the watermark and never come back;
//   - adjust:  ensures a draft covering the current delta exists, then
//     overrides its price (comment is the mandatory audit trail).
func (h *Handler) ResolveProjectBillingDispute(w http.ResponseWriter, r *http.Request) {
	project, ok := h.loadProjectForBilling(w, r)
	if !ok {
		return
	}
	userUUID, ok := h.requireBillingEditor(w, r, uuidToString(project.WorkspaceID))
	if !ok {
		return
	}
	disputeUUID, ok := parseUUIDOrBadRequest(w, chi.URLParam(r, "disputeId"), "dispute id")
	if !ok {
		return
	}
	var req resolveDisputeRequest
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
		writeError(w, http.StatusBadRequest, "invalid request body")
		return
	}
	if req.Resolution != "keep" && req.Resolution != "exclude" && req.Resolution != "adjust" {
		writeError(w, http.StatusBadRequest, "resolution must be keep, exclude or adjust")
		return
	}
	if req.Resolution == "adjust" && (req.Comment == "" || req.PriceRub < 0) {
		writeError(w, http.StatusBadRequest, "adjust requires a comment and price_rub >= 0")
		return
	}

	resolved, err := h.Queries.ResolveClientBillingDispute(r.Context(), db.ResolveClientBillingDisputeParams{
		ID:         disputeUUID,
		Resolution: pgtype.Text{String: req.Resolution, Valid: true},
		Comment:    pgtype.Text{String: req.Comment, Valid: req.Comment != ""},
		ResolvedBy: userUUID,
	})
	if errors.Is(err, pgx.ErrNoRows) {
		writeError(w, http.StatusConflict, "dispute is not open (or does not exist)")
		return
	}
	if err != nil {
		writeError(w, http.StatusInternalServerError, "failed to resolve dispute")
		return
	}

	switch req.Resolution {
	case "exclude":
		if err := h.excludeIssueBilling(r.Context(), resolved.IssueID, project); err != nil {
			slog.Error("billing: dispute exclude failed", "dispute_id", uuidToString(resolved.ID), "error", err)
			writeError(w, http.StatusInternalServerError, "dispute resolved but exclusion failed: "+err.Error())
			return
		}
	case "adjust":
		if err := h.adjustIssueBilling(r.Context(), resolved.IssueID, project, req.PriceRub, req.Comment); err != nil {
			slog.Error("billing: dispute adjust failed", "dispute_id", uuidToString(resolved.ID), "error", err)
			writeError(w, http.StatusInternalServerError, "dispute resolved but adjust failed: "+err.Error())
			return
		}
	}
	slog.Info("billing: dispute resolved",
		"dispute_id", uuidToString(resolved.ID), "resolution", req.Resolution)
	writeJSON(w, http.StatusOK, disputeJSON(db.ClientBillingDispute(resolved)))
}

// excludeIssueBilling voids the issue's pending drafts and burns the remaining
// unbilled delta into a void charge (watermark advance without billing).
func (h *Handler) excludeIssueBilling(ctx context.Context, issueID pgtype.UUID, project db.Project) error {
	voided, err := h.Queries.VoidDraftChargesByIssue(ctx, issueID)
	if err != nil {
		return fmt.Errorf("void drafts: %w", err)
	}
	for _, v := range voided {
		h.recalcPeriodTotal(ctx, v.PeriodID)
	}

	rows, err := h.Queries.ListIssueUsageByModel(ctx, issueID)
	if err != nil {
		return fmt.Errorf("issue usage: %w", err)
	}
	baseline, err := h.issueBilledBaseline(ctx, issueID)
	if err != nil {
		return fmt.Errorf("baseline: %w", err)
	}
	delta := subtractBaseline(rows, baseline)
	cfg, err := h.Queries.GetClientBillingConfig(ctx, project.ID)
	if err != nil {
		if errors.Is(err, pgx.ErrNoRows) {
			return nil // no billing config — nothing accrues anyway
		}
		return err
	}
	pricing := h.resolveBillingPricing(ctx, cfg, project.WorkspaceID)
	fx := h.latestFxRubPerUsd(ctx) * (1 + pricing.FxMarkupPercent/100)
	charge, created, err := h.sweepIssueDelta(ctx, issueID, project.ID, project.WorkspaceID, delta, pricing, fx, pgtype.UUID{})
	if err != nil {
		return fmt.Errorf("sweep delta: %w", err)
	}
	if created {
		if _, err := h.Queries.VoidClientBillingCharge(ctx, db.VoidClientBillingChargeParams{
			ChargeID: charge.ID, IssueID: issueID,
		}); err != nil {
			return fmt.Errorf("void swept delta: %w", err)
		}
	}
	return nil
}

// adjustIssueBilling makes sure a draft covering the current unbilled delta
// exists, then overrides its price.
func (h *Handler) adjustIssueBilling(ctx context.Context, issueID pgtype.UUID, project db.Project, priceRub float64, reason string) error {
	cfg, err := h.Queries.GetClientBillingConfig(ctx, project.ID)
	if err != nil {
		return fmt.Errorf("billing config: %w", err)
	}
	rows, err := h.Queries.ListIssueUsageByModel(ctx, issueID)
	if err != nil {
		return fmt.Errorf("issue usage: %w", err)
	}
	baseline, err := h.issueBilledBaseline(ctx, issueID)
	if err != nil {
		return fmt.Errorf("baseline: %w", err)
	}
	delta := subtractBaseline(rows, baseline)
	pricing := h.resolveBillingPricing(ctx, cfg, project.WorkspaceID)
	fx := h.latestFxRubPerUsd(ctx) * (1 + pricing.FxMarkupPercent/100)
	fx = math.Round(fx*10000) / 10000
	period, err := h.ensureOpenBillingPeriod(ctx, cfg, project.ID, project.WorkspaceID)
	if err != nil {
		return fmt.Errorf("ensure period: %w", err)
	}
	if _, _, err := h.sweepIssueDelta(ctx, issueID, project.ID, project.WorkspaceID, delta, pricing, fx, period.ID); err != nil {
		return fmt.Errorf("sweep delta: %w", err)
	}
	latest, err := h.Queries.GetClientBillingChargeByIssue(ctx, issueID)
	if err != nil {
		return fmt.Errorf("no charge to adjust: %w", err)
	}
	if latest.Status != "draft" {
		return errors.New("latest charge is not a draft")
	}
	if _, err := h.Queries.AdjustClientBillingCharge(ctx, db.AdjustClientBillingChargeParams{
		ChargeID: latest.ID,
		IssueID:  issueID,
		PriceRub: priceRub,
		Reason:   pgtype.Text{String: reason, Valid: true},
	}); err != nil {
		return fmt.Errorf("adjust: %w", err)
	}
	return nil
}
