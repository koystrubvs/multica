package handler

// client_billing_contractor.go — contractor-level billing settings and
// consolidated per-contractor invoicing (migration 202).
//
// One Elba contractor can span several projects (e.g. «Долголетие» =
// spina.spb.ru + plastika.me). Each project keeps metering its own charges,
// but the invoice is issued for the contractor as a whole: one счёт + акт that
// covers every project's closed period for the same billing cycle, with the
// subscription cap applied to the combined delivered value rather than per
// project. This mirrors the single-period mechanics in
// client_billing_period.go (pushPeriodToElba) — same Elba client, same
// «Скидка по абонементу» discount trick — but sourced from a contractor batch.

import (
	"encoding/json"
	"errors"
	"fmt"
	"log/slog"
	"math"
	"net/http"
	"sort"
	"time"

	"github.com/go-chi/chi/v5"
	"github.com/jackc/pgx/v5/pgtype"

	db "github.com/multica-ai/multica/server/pkg/db/generated"
)

// --- JSON shaping ---

type clientBillingContractorConfigJSON struct {
	WorkspaceID        string             `json:"workspace_id"`
	ElbaContractorID   string             `json:"elba_contractor_id"`
	Name               *string            `json:"name"`
	Mode               string             `json:"mode"`
	SubscriptionFeeRub float64            `json:"subscription_fee_rub"`
	ElbaBankAccountID  *string            `json:"elba_bank_account_id"`
	CreatedAt          pgtype.Timestamptz `json:"created_at"`
	UpdatedAt          pgtype.Timestamptz `json:"updated_at"`
}

func contractorConfigJSON(row db.ListClientBillingContractorConfigsRow) clientBillingContractorConfigJSON {
	return clientBillingContractorConfigJSON{
		WorkspaceID:        uuidToString(row.WorkspaceID),
		ElbaContractorID:   row.ElbaContractorID,
		Name:               textToPtr(row.Name),
		Mode:               row.Mode,
		SubscriptionFeeRub: row.SubscriptionFeeRub,
		ElbaBankAccountID:  textToPtr(row.ElbaBankAccountID),
		CreatedAt:          row.CreatedAt,
		UpdatedAt:          row.UpdatedAt,
	}
}

// --- Contractor config CRUD ---

// ListContractorBillingConfigs returns the workspace's contractor-level
// billing settings (mode + subscription fee per Elba contractor).
func (h *Handler) ListContractorBillingConfigs(w http.ResponseWriter, r *http.Request) {
	wsID := h.resolveWorkspaceID(r)
	wsUUID, ok := parseUUIDOrBadRequest(w, wsID, "workspace id")
	if !ok {
		return
	}
	if !h.requireBillingStaff(w, r, wsID) {
		return
	}
	rows, err := h.Queries.ListClientBillingContractorConfigs(r.Context(), wsUUID)
	if err != nil {
		writeError(w, http.StatusInternalServerError, "failed to list contractor billing configs")
		return
	}
	out := make([]clientBillingContractorConfigJSON, 0, len(rows))
	for _, row := range rows {
		out = append(out, contractorConfigJSON(row))
	}
	writeJSON(w, http.StatusOK, out)
}

type contractorBillingConfigRequest struct {
	ElbaContractorID   string   `json:"elba_contractor_id"`
	Name               *string  `json:"name"`
	Mode               *string  `json:"mode"`
	SubscriptionFeeRub *float64 `json:"subscription_fee_rub"`
	ElbaBankAccountID  *string  `json:"elba_bank_account_id"`
}

// UpsertContractorBillingConfig saves the mode + subscription fee for one
// contractor. Owner/admin only (same gate as the workspace pricing defaults).
func (h *Handler) UpsertContractorBillingConfig(w http.ResponseWriter, r *http.Request) {
	wsID := h.resolveWorkspaceID(r)
	wsUUID, ok := parseUUIDOrBadRequest(w, wsID, "workspace id")
	if !ok {
		return
	}
	if !h.requireBillingAdmin(w, r, wsID) {
		return
	}
	var req contractorBillingConfigRequest
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
		writeError(w, http.StatusBadRequest, "invalid request body")
		return
	}
	if req.ElbaContractorID == "" {
		writeError(w, http.StatusBadRequest, "elba_contractor_id is required")
		return
	}
	mode := "postpaid"
	if req.Mode != nil {
		if *req.Mode != "postpaid" && *req.Mode != "subscription" {
			writeError(w, http.StatusBadRequest, "mode must be postpaid or subscription")
			return
		}
		mode = *req.Mode
	}
	params := db.UpsertClientBillingContractorConfigParams{
		WorkspaceID:      wsUUID,
		ElbaContractorID: req.ElbaContractorID,
		Mode:             mode,
	}
	if req.Name != nil {
		params.Name = pgtype.Text{String: *req.Name, Valid: *req.Name != ""}
	}
	if req.SubscriptionFeeRub != nil {
		if *req.SubscriptionFeeRub < 0 {
			writeError(w, http.StatusBadRequest, "subscription_fee_rub must be >= 0")
			return
		}
		params.SubscriptionFeeRub = pgtype.Float8{Float64: *req.SubscriptionFeeRub, Valid: true}
	}
	if req.ElbaBankAccountID != nil {
		params.ElbaBankAccountID = pgtype.Text{String: *req.ElbaBankAccountID, Valid: *req.ElbaBankAccountID != ""}
	}
	cfg, err := h.Queries.UpsertClientBillingContractorConfig(r.Context(), params)
	if err != nil {
		slog.Error("billing: contractor config upsert failed", "workspace_id", wsID, "contractor_id", req.ElbaContractorID, "error", err)
		writeError(w, http.StatusInternalServerError, "failed to save contractor billing config")
		return
	}
	writeJSON(w, http.StatusOK, contractorConfigJSON(db.ListClientBillingContractorConfigsRow(cfg)))
}

// --- Invoiceable period groups (UI) ---

type contractorPeriodGroupProjectJSON struct {
	PeriodID     string  `json:"period_id"`
	ProjectID    string  `json:"project_id"`
	ProjectTitle string  `json:"project_title"`
	TotalRub     float64 `json:"total_rub"`
}

type contractorPeriodGroupJSON struct {
	ElbaContractorID string                             `json:"elba_contractor_id"`
	StartsOn         string                             `json:"starts_on"`
	EndsOn           string                             `json:"ends_on"`
	TotalRub         float64                            `json:"total_rub"`
	Projects         []contractorPeriodGroupProjectJSON `json:"projects"`
}

// ListInvoiceableContractorGroups returns the workspace's closed, uninvoiced
// periods grouped by (contractor, cycle) — one group per «Выставить счёт»
// button. Groups with a single project still show up; the UI can offer the
// consolidated endpoint uniformly.
func (h *Handler) ListInvoiceableContractorGroups(w http.ResponseWriter, r *http.Request) {
	wsID := h.resolveWorkspaceID(r)
	wsUUID, ok := parseUUIDOrBadRequest(w, wsID, "workspace id")
	if !ok {
		return
	}
	if !h.requireBillingStaff(w, r, wsID) {
		return
	}
	rows, err := h.Queries.ListInvoiceableClosedPeriodsInWorkspace(r.Context(), wsUUID)
	if err != nil {
		writeError(w, http.StatusInternalServerError, "failed to list invoiceable periods")
		return
	}
	// Group by contractor id + exact cycle. Keep insertion order (the query
	// already sorts by starts_on DESC, then project title) for stable output.
	type groupKey struct{ contractor, start, end string }
	index := make(map[groupKey]int)
	groups := make([]contractorPeriodGroupJSON, 0)
	for _, row := range rows {
		start := row.StartsOn.Time.Format("2006-01-02")
		end := row.EndsOn.Time.Format("2006-01-02")
		key := groupKey{row.ElbaContractorID.String, start, end}
		i, seen := index[key]
		if !seen {
			i = len(groups)
			index[key] = i
			groups = append(groups, contractorPeriodGroupJSON{
				ElbaContractorID: row.ElbaContractorID.String,
				StartsOn:         start,
				EndsOn:           end,
			})
		}
		groups[i].Projects = append(groups[i].Projects, contractorPeriodGroupProjectJSON{
			PeriodID:     uuidToString(row.ID),
			ProjectID:    uuidToString(row.ProjectID),
			ProjectTitle: row.ProjectTitle,
			TotalRub:     row.TotalRub,
		})
		groups[i].TotalRub += row.TotalRub
	}
	writeJSON(w, http.StatusOK, groups)
}

// --- Consolidated invoicing ---

// contractorInvoiceGroup is one project's confirmed-charge lines within the
// cycle. Kept as a named type so the flattening + discount logic is a pure,
// testable function (buildContractorInvoiceItems).
type contractorInvoiceGroup struct {
	ProjectTitle string
	Items        []elbaDocItem
}

// discountedKopecks mirrors Elba's per-line computation:
// round(price * (1 - discount/100)) to the kopeck, with the discount pinned to
// 2 decimal places (hundredths of a percent).
func discountedKopecks(priceRub, discountPct float64) int64 {
	p := int64(math.Round(priceRub * 100))
	c := int64(math.Round(discountPct * 100)) // hundredths of a percent
	if c < 0 {
		c = 0
	}
	if c > 10000 {
		c = 10000
	}
	return (p*(10000-c) + 5000) / 10000
}

// discountedTotalRub sums the per-line discounted totals the way Elba will.
func discountedTotalRub(prices, discountsPct []float64) float64 {
	var kop int64
	for i := range prices {
		kop += discountedKopecks(prices[i], discountsPct[i])
	}
	return float64(kop) / 100
}

// distributeSubscriptionDiscounts computes a per-line discount percent (max 2
// decimals) so the sum of the discounted line totals equals feeRub, capping a
// subscription invoice at the fixed fee WITHOUT a negative line (forbidden by
// the Elba v1 contract). Returns one percent per price, each in [0,100].
//
// Elba discounts each line as round(price*(1-pct/100)); a 2-decimal percent
// moves a large line in coarse steps, so a single balancing line cannot hit an
// arbitrary target. Leading lines take a uniform percent and the last two are
// searched jointly (outward from that uniform value) for the combination whose
// discounted sum lands on feeRub to the kopeck — the general form of the
// hand-picked pair in the reference invoice. Falls back to the closest
// achievable split when an exact hit is impossible.
func distributeSubscriptionDiscounts(prices []float64, feeRub float64) []float64 {
	n := len(prices)
	out := make([]float64, n)
	if n == 0 {
		return out
	}
	P := make([]int64, n)
	var total int64
	for i, p := range prices {
		P[i] = int64(math.Round(p * 100))
		total += P[i]
	}
	F := int64(math.Round(feeRub * 100))
	if F <= 0 || F >= total {
		return out // nothing to cap
	}

	disc := func(p, c int64) int64 { return (p*(10000-c) + 5000) / 10000 }
	// hundredths-of-percent that discounts price p down to ~target.
	solveC := func(p, target int64) int64 {
		if p <= 0 {
			return 0
		}
		c := int64(math.Round(float64(p-target) * 10000 / float64(p)))
		if c < 0 {
			c = 0
		}
		if c > 10000 {
			c = 10000
		}
		return c
	}

	cs := make([]int64, n)
	if n == 1 {
		cs[0] = solveC(P[0], F)
		out[0] = float64(cs[0]) / 100
		return out
	}

	uniformC := solveC(total, F)
	var fixed int64
	for i := 0; i < n-2; i++ {
		cs[i] = uniformC
		fixed += disc(P[i], uniformC)
	}
	r2 := F - fixed // the last two lines must sum to this after discount
	a, b := n-2, n-1
	bestErr := int64(1) << 62
	bestCa, bestCb := uniformC, uniformC
	for delta := int64(0); delta <= 10000; delta++ {
		for _, ca := range [2]int64{uniformC + delta, uniformC - delta} {
			if ca < 0 || ca > 10000 {
				continue
			}
			ta := disc(P[a], ca)
			cb := solveC(P[b], r2-ta)
			tb := disc(P[b], cb)
			err := (ta + tb) - r2
			if err < 0 {
				err = -err
			}
			if err < bestErr {
				bestErr, bestCa, bestCb = err, ca, cb
			}
		}
		if bestErr == 0 {
			break
		}
	}
	cs[a], cs[b] = bestCa, bestCb
	for i := range cs {
		out[i] = float64(cs[i]) / 100
	}
	return out
}

// buildContractorInvoiceItems flattens the per-project charge groups into one
// warehouse-line list. In subscription mode, when the delivered value exceeds
// the fixed fee, it caps the bill by distributing a per-line discount percent
// so the discounted total equals the fee (Elba v1 forbids negative lines). The
// same line list feeds both the счёт and the акт: line prices show the full
// delivered value (grossTotal) while the discounts bring the payable total to
// the fee.
//
// Returns the lines, the payable bill total (after discounts), grossTotal (sum
// of the full line prices, i.e. the акт's headline value) and whether any
// discount was applied.
func buildContractorInvoiceItems(groups []contractorInvoiceGroup, mode string, subscriptionFeeRub float64) (items []elbaDocItem, billTotal, grossTotal float64, withDiscount bool) {
	for _, g := range groups {
		for _, it := range g.Items {
			items = append(items, it)
			grossTotal += it.Price
		}
	}
	billTotal = grossTotal
	if mode == "subscription" && subscriptionFeeRub > 0 && grossTotal > subscriptionFeeRub {
		prices := make([]float64, len(items))
		for i := range items {
			prices[i] = items[i].Price
		}
		pcts := distributeSubscriptionDiscounts(prices, subscriptionFeeRub)
		for i := range items {
			items[i].Discount = pcts[i]
		}
		billTotal = discountedTotalRub(prices, pcts)
		withDiscount = true
	}
	return items, billTotal, grossTotal, withDiscount
}

type contractorInvoiceRequest struct {
	StartsOn string `json:"starts_on"` // YYYY-MM-DD
	EndsOn   string `json:"ends_on"`   // YYYY-MM-DD
}

// InvoiceContractorPeriod issues ONE счёт + акт in Elba covering every
// project's closed period for the given contractor and cycle, then flips all
// of those periods to `invoiced` with the shared Elba document ids. Charges
// are grouped by project (line names prefixed with the project title) and, in
// subscription mode, capped at the contractor's fixed fee.
func (h *Handler) InvoiceContractorPeriod(w http.ResponseWriter, r *http.Request) {
	wsID := h.resolveWorkspaceID(r)
	wsUUID, ok := parseUUIDOrBadRequest(w, wsID, "workspace id")
	if !ok {
		return
	}
	if _, ok := h.requireBillingEditor(w, r, wsID); !ok {
		return
	}
	contractorID := chi.URLParam(r, "contractorId")
	if contractorID == "" {
		writeError(w, http.StatusBadRequest, "contractor id is required")
		return
	}
	var req contractorInvoiceRequest
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
		writeError(w, http.StatusBadRequest, "invalid request body")
		return
	}
	start, err := time.Parse("2006-01-02", req.StartsOn)
	if err != nil {
		writeError(w, http.StatusBadRequest, "starts_on must be YYYY-MM-DD")
		return
	}
	end, err := time.Parse("2006-01-02", req.EndsOn)
	if err != nil {
		writeError(w, http.StatusBadRequest, "ends_on must be YYYY-MM-DD")
		return
	}

	// Elba org must be wired at workspace level; the API key must be present.
	wsCfg, err := h.Queries.GetClientBillingWorkspaceConfig(r.Context(), wsUUID)
	if err != nil || !wsCfg.ElbaOrgID.Valid {
		writeError(w, http.StatusBadRequest, "link an Elba organization in workspace billing settings first")
		return
	}
	client, err := newElbaClient()
	if errors.Is(err, errElbaNotConfigured) {
		writeError(w, http.StatusServiceUnavailable, "Elba integration is not configured (set ELBA_API_KEY)")
		return
	}
	if err != nil {
		writeError(w, http.StatusInternalServerError, "failed to initialize Elba client")
		return
	}

	periods, err := h.Queries.ListClosedPeriodsForContractorCycle(r.Context(), db.ListClosedPeriodsForContractorCycleParams{
		WorkspaceID:      wsUUID,
		ElbaContractorID: pgtype.Text{String: contractorID, Valid: true},
		StartsOn:         pgtype.Date{Time: start, Valid: true},
		EndsOn:           pgtype.Date{Time: end, Valid: true},
	})
	if err != nil {
		writeError(w, http.StatusInternalServerError, "failed to load contractor periods")
		return
	}
	if len(periods) == 0 {
		writeError(w, http.StatusConflict, "no closed, uninvoiced periods for this contractor and cycle")
		return
	}

	// Group confirmed charges by project, prefixing each line with the project
	// title so the consolidated bill stays readable per project.
	groups := make([]contractorInvoiceGroup, 0, len(periods))
	for _, p := range periods {
		charges, cerr := h.Queries.ListClientBillingChargesByPeriod(r.Context(), p.ID)
		if cerr != nil {
			writeError(w, http.StatusInternalServerError, "failed to load period charges")
			return
		}
		g := contractorInvoiceGroup{ProjectTitle: p.ProjectTitle}
		for _, c := range charges {
			if c.Status != "confirmed" {
				continue
			}
			g.Items = append(g.Items, elbaDocItem{
				ProductName: fmt.Sprintf("%s: %s", p.ProjectTitle, c.IssueTitle),
				Quantity:    1,
				Price:       c.PriceRub,
				UnitName:    "усл",
			})
		}
		if len(g.Items) > 0 {
			groups = append(groups, g)
		}
	}
	// Deterministic project ordering (the query already orders by title, but be
	// explicit for the grouped output).
	sort.SliceStable(groups, func(i, j int) bool { return groups[i].ProjectTitle < groups[j].ProjectTitle })

	// Contractor mode/fee: a missing contractor config means plain postpaid.
	mode := "postpaid"
	var feeRub float64
	var bankAccountID string
	if cc, ccErr := h.Queries.GetClientBillingContractorConfig(r.Context(), db.GetClientBillingContractorConfigParams{
		WorkspaceID: wsUUID, ElbaContractorID: contractorID,
	}); ccErr == nil {
		mode = cc.Mode
		feeRub = cc.SubscriptionFeeRub
		if cc.ElbaBankAccountID.Valid {
			bankAccountID = cc.ElbaBankAccountID.String
		}
	}

	items, billTotal, grossTotal, withDiscount := buildContractorInvoiceItems(groups, mode, feeRub)
	if len(items) == 0 {
		writeError(w, http.StatusConflict, "the contractor's periods have no confirmed charges to invoice")
		return
	}

	opts := elbaDocOptions{
		Date: time.Now().UTC().Format("2006-01-02"),
		Comment: fmt.Sprintf("Консолидированный счёт по контрагенту за период %s — %s",
			start.Format("02.01.2006"), end.Format("02.01.2006")),
		WithDiscount: withDiscount,
	}
	if bankAccountID != "" {
		opts.BankAccountID = bankAccountID
	} else if wsCfg.ElbaBankAccountID.Valid {
		opts.BankAccountID = wsCfg.ElbaBankAccountID.String
	}

	orgID := wsCfg.ElbaOrgID.String
	billID, err := client.CreateBill(r.Context(), orgID, contractorID, items, opts)
	if err != nil {
		writeError(w, http.StatusBadGateway, fmt.Sprintf("create bill: %v", err))
		return
	}
	actID, actErr := client.CreateAct(r.Context(), orgID, contractorID, items, opts)
	if actErr != nil {
		// The bill exists — record it anyway so a retry doesn't duplicate it.
		slog.Error("billing: contractor act creation failed after bill",
			"workspace_id", wsID, "contractor_id", contractorID, "bill_id", billID, "error", actErr)
	}

	// Stamp every period in the batch with the shared documents (closed -> invoiced).
	invoicedIDs := make([]string, 0, len(periods))
	for _, p := range periods {
		if _, uerr := h.Queries.SetClientBillingPeriodElba(r.Context(), db.SetClientBillingPeriodElbaParams{
			ID:        p.ID,
			InvoiceID: pgtype.Text{String: billID, Valid: billID != ""},
			ActID:     pgtype.Text{String: actID, Valid: actID != ""},
		}); uerr != nil {
			slog.Error("billing: contractor period elba stamp failed",
				"period_id", uuidToString(p.ID), "bill_id", billID, "error", uerr)
			continue
		}
		invoicedIDs = append(invoicedIDs, uuidToString(p.ID))
	}

	slog.Info("billing: contractor invoiced in Elba",
		"workspace_id", wsID, "contractor_id", contractorID, "bill_id", billID, "act_id", actID,
		"gross_rub", grossTotal, "bill_rub", billTotal, "periods", len(invoicedIDs))

	resp := map[string]any{
		"contractor_id": contractorID,
		"starts_on":     req.StartsOn,
		"ends_on":       req.EndsOn,
		"mode":          mode,
		"bill_id":       billID,
		"act_id":        actID,
		"gross_rub":     grossTotal,
		"bill_rub":      billTotal,
		"period_ids":    invoicedIDs,
		"period_count":  len(invoicedIDs),
	}
	if actErr != nil {
		resp["act_error"] = actErr.Error()
	}
	writeJSON(w, http.StatusOK, resp)
}
