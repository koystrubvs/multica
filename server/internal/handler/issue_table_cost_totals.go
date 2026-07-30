package handler

// issue_table_cost_totals.go — per-column money line for the kanban board.
//
// The board header shows, next to the issue count, what the issues in that
// column are worth to the client. The number has to answer "what will we
// invoice for this pile", so it is built from exactly the same parts as the
// issue card's live cost (GET /issues/{id}/billing/cost): no-cache USD per
// (provider, model) -> RUB at the project's fx markup -> the project's markup,
// minimum price and rounding.
//
// Two consequences shape this file:
//
//   - The price is a PER-ISSUE function (minimum price and rounding apply to
//     each issue, and the markup comes from that issue's project), so a column
//     total is the sum of per-issue prices — never one markup applied to the
//     column's summed tokens. Hence the query returns one row per
//     (group, issue, provider, model) and the folding happens here.
//   - Filters must match what the user is looking at, so the request carries
//     the same query/group payload as the table endpoints and is compiled by
//     the same compiler. No third copy of the filter grammar.
//
// Owner-only, enforced here rather than hidden in the client: the agency's
// pricing is not something a member or a SitePing guest may read.

import (
	"fmt"
	"log/slog"
	"math"
	"net/http"

	"github.com/multica-ai/multica/server/internal/util"
	db "github.com/multica-ai/multica/server/pkg/db/generated"
)

type issueCostTotalsRequest struct {
	Query issueTableQuerySpec `json:"query"`
	Group issueTableGroupSpec `json:"group"`
}

type issueCostTotalsGroupJSON struct {
	Key      string  `json:"key"`
	Issues   int64   `json:"issues"`
	Tokens   int64   `json:"tokens"`
	PriceRub float64 `json:"price_rub"`
}

// One issue's own price, for the card that shows it. Emitted from the same
// pass that folds the group totals — the per-issue price has to be computed
// anyway, so the board gets both from one request instead of asking
// /issues/{id}/billing/cost once per visible card.
type issueCostTotalsIssueJSON struct {
	ID       string  `json:"id"`
	Tokens   int64   `json:"tokens"`
	PriceRub float64 `json:"price_rub"`
}

type issueCostTotalsJSON struct {
	Groups []issueCostTotalsGroupJSON `json:"groups"`
	// Only issues that cost something: unbilled and untouched issues would
	// otherwise pad an owner-only payload with zeroes.
	Issues []issueCostTotalsIssueJSON `json:"issues"`
	Total  issueCostTotalsGroupJSON   `json:"total"`
}

// costTotalsIssue accumulates one issue's usage rows before pricing.
type costTotalsIssue struct {
	groupKey  string
	projectID string
	usage     []db.ListIssueUsageByModelRow
}

// costTotalsPricing is one project's resolved knobs. fx already carries that
// project's fx markup, mirroring GetIssueBillingCost.
type costTotalsPricing struct {
	pricing billingPricing
	fx      float64
}

// ListIssueCostTotals returns the client price and token count of the issues
// matching the request, grouped the way the board groups them.
func (h *Handler) ListIssueCostTotals(w http.ResponseWriter, r *http.Request) {
	if h.DB == nil {
		writeError(w, http.StatusInternalServerError, "database is unavailable")
		return
	}
	if _, ok := h.requireWorkspaceRole(w, r, h.resolveWorkspaceID(r), "workspace not found", "owner"); !ok {
		return
	}
	var request issueCostTotalsRequest
	if !decodeIssueTableJSON(w, r, &request) {
		return
	}
	r, cancel := withIssueTableQueryTimeout(r)
	defer cancel()

	compiled, ok := h.compileIssueTableQuery(w, r, request.Query)
	if !ok {
		return
	}
	group, ok := h.resolveIssueTableGroup(w, r, compiled.workspaceID, request.Group, false)
	if !ok {
		return
	}

	ctx := r.Context()
	// Internal work is excluded for the same reason it never reaches an
	// invoice (see issueMarkedInternalSQL): this line is the client price, and
	// our own infrastructure fixes are not billed to anyone.
	query := fmt.Sprintf(`
		SELECT %s AS group_key,
		       i.id::text                        AS issue_id,
		       COALESCE(i.project_id::text, '')  AS project_id,
		       tu.provider,
		       tu.model,
		       COALESCE(SUM(tu.input_tokens), 0)::bigint       AS input_tokens,
		       COALESCE(SUM(tu.output_tokens), 0)::bigint      AS output_tokens,
		       COALESCE(SUM(tu.cache_read_tokens), 0)::bigint  AS cache_read_tokens,
		       COALESCE(SUM(tu.cache_write_tokens), 0)::bigint AS cache_write_tokens
		  FROM issue i
		  JOIN agent_task_queue atq ON atq.issue_id = i.id
		  JOIN task_usage tu ON tu.task_id = atq.id
		 WHERE %s
		   AND NOT %s
		 GROUP BY 1, 2, 3, 4, 5`, group.groupExpr, compiled.where, issueMarkedInternalSQL)

	rows, err := h.DB.Query(ctx, query, compiled.args...)
	if err != nil {
		slog.Warn("ListIssueCostTotals query failed", "error", err)
		writeIssueTableQueryFailure(w, r, "failed to aggregate issue cost")
		return
	}
	defer rows.Close()

	issues := make(map[string]*costTotalsIssue)
	order := make([]string, 0)
	for rows.Next() {
		var groupKey, issueID, projectID string
		var line db.ListIssueUsageByModelRow
		if err := rows.Scan(
			&groupKey, &issueID, &projectID,
			&line.Provider, &line.Model,
			&line.InputTokens, &line.OutputTokens,
			&line.CacheReadTokens, &line.CacheWriteTokens,
		); err != nil {
			slog.Warn("ListIssueCostTotals scan failed", "error", err)
			writeIssueTableQueryFailure(w, r, "failed to aggregate issue cost")
			return
		}
		acc, seen := issues[issueID]
		if !seen {
			acc = &costTotalsIssue{groupKey: groupKey, projectID: projectID}
			issues[issueID] = acc
			order = append(order, issueID)
		}
		acc.usage = append(acc.usage, line)
	}
	if err := rows.Err(); err != nil {
		slog.Warn("ListIssueCostTotals iteration failed", "error", err)
		writeIssueTableQueryFailure(w, r, "failed to aggregate issue cost")
		return
	}

	baseFx := h.latestFxRubPerUsd(ctx)
	pricingByProject := make(map[string]costTotalsPricing)
	resolvePricing := func(projectID string) costTotalsPricing {
		if cached, ok := pricingByProject[projectID]; ok {
			return cached
		}
		var cfg db.GetClientBillingConfigRow
		if projectID != "" {
			if id, err := util.ParseUUID(projectID); err == nil {
				if c, err := h.Queries.GetClientBillingConfig(ctx, id); err == nil && c.Enabled {
					cfg = c
				}
			}
		}
		p := h.resolveBillingPricing(ctx, cfg, compiled.workspaceID)
		fx := baseFx * (1 + p.FxMarkupPercent/100)
		resolved := costTotalsPricing{pricing: p, fx: math.Round(fx*10000) / 10000}
		pricingByProject[projectID] = resolved
		return resolved
	}

	totals := make(map[string]*issueCostTotalsGroupJSON)
	groupOrder := make([]string, 0)
	perIssue := make([]issueCostTotalsIssueJSON, 0, len(order))
	var overall issueCostTotalsGroupJSON
	for _, issueID := range order {
		acc := issues[issueID]
		_, usd, tokens := computeBillingUsage(acc.usage)
		if tokens == 0 {
			continue
		}
		p := resolvePricing(acc.projectID)
		price := computePriceRub(usd, p.fx, p.pricing.Markup, p.pricing.MinPriceRub, p.pricing.RoundingRub)

		bucket, seen := totals[acc.groupKey]
		if !seen {
			bucket = &issueCostTotalsGroupJSON{Key: acc.groupKey}
			totals[acc.groupKey] = bucket
			groupOrder = append(groupOrder, acc.groupKey)
		}
		bucket.Issues++
		bucket.Tokens += tokens
		bucket.PriceRub += price

		overall.Issues++
		overall.Tokens += tokens
		overall.PriceRub += price

		perIssue = append(perIssue, issueCostTotalsIssueJSON{
			ID:       issueID,
			Tokens:   tokens,
			PriceRub: price,
		})
	}

	out := issueCostTotalsJSON{
		Groups: make([]issueCostTotalsGroupJSON, 0, len(groupOrder)),
		Issues: perIssue,
	}
	for _, key := range groupOrder {
		bucket := totals[key]
		bucket.PriceRub = math.Round(bucket.PriceRub*100) / 100
		out.Groups = append(out.Groups, *bucket)
	}
	overall.PriceRub = math.Round(overall.PriceRub*100) / 100
	out.Total = overall

	writeJSON(w, http.StatusOK, out)
}
