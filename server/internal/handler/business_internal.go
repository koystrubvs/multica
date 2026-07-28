package handler

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"net/http"
	"strconv"
	"strings"
	"time"

	"github.com/go-chi/chi/v5"
	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgtype"
	"github.com/multica-ai/multica/server/internal/featureflags"
	"github.com/multica-ai/multica/server/internal/middleware"
	"github.com/multica-ai/multica/server/pkg/featureflag"
)

const businessJSONBodyLimit = 1 << 20

type BusinessSnapshotResponse struct {
	Clients             json.RawMessage `json:"clients"`
	Aliases             json.RawMessage `json:"aliases"`
	Payers              json.RawMessage `json:"payers"`
	Projects            json.RawMessage `json:"projects"`
	AvailableWorkspaces json.RawMessage `json:"available_workspaces"`
	AvailableProjects   json.RawMessage `json:"available_projects"`
	Counterparties      json.RawMessage `json:"counterparties"`
	Agreements          json.RawMessage `json:"agreements"`
	Receivables         json.RawMessage `json:"receivables"`
	BankImports         json.RawMessage `json:"bank_imports"`
	Transactions        json.RawMessage `json:"transactions"`
	Matches             json.RawMessage `json:"matches"`
	CompanyCosts        json.RawMessage `json:"company_costs"`
	RecurringCosts      json.RawMessage `json:"recurring_costs"`
	Workers             json.RawMessage `json:"workers"`
	Policies            json.RawMessage `json:"policies"`
	ClientRequests      json.RawMessage `json:"client_requests"`
	TaskEconomics       json.RawMessage `json:"task_economics"`
	TaskParticipants    json.RawMessage `json:"task_participants"`
	ReceivableTasks     json.RawMessage `json:"receivable_tasks"`
	Accruals            json.RawMessage `json:"accruals"`
	QualityCases        json.RawMessage `json:"quality_cases"`
	AccrualAdjustments  json.RawMessage `json:"accrual_adjustments"`
	ReserveLedger       json.RawMessage `json:"reserve_ledger"`
	PayoutBatches       json.RawMessage `json:"payout_batches"`
	PayoutItems         json.RawMessage `json:"payout_items"`
	BankOutbox          json.RawMessage `json:"bank_outbox"`
	BillingCandidates   json.RawMessage `json:"billing_candidates"`
	BillingMonthTotals  json.RawMessage `json:"billing_month_client_totals"`
	BillingTasks        json.RawMessage `json:"billing_tasks"`
	Contracts           json.RawMessage `json:"contracts"`
	GeneratedAt         string          `json:"generated_at"`
}

type BusinessDashboardResponse struct {
	Month                  string `json:"month"`
	ExpectedRUB            string `json:"expected_rub"`
	InvoicedRUB            string `json:"invoiced_rub"`
	ReceivablePaidRUB      string `json:"receivable_paid_rub"`
	OverdueRUB             string `json:"overdue_rub"`
	NotInvoicedRUB         string `json:"not_invoiced_rub"`
	BankClientIncomeRUB    string `json:"bank_client_income_rub"`
	VitmaxTransitRUB       string `json:"vitmax_transit_rub"`
	TransferRUB            string `json:"transfer_rub"`
	UnknownInboundRUB      string `json:"unknown_inbound_rub"`
	TaskValueRUB           string `json:"task_value_rub"`
	ParticipantAccruedRUB  string `json:"participant_accrued_rub"`
	CompanyTargetPoolRUB   string `json:"company_target_pool_rub"`
	OwnerTargetMarginRUB   string `json:"owner_target_margin_rub"`
	CompanyCostsRUB        string `json:"company_costs_rub"`
	PayableRUB             string `json:"payable_rub"`
	PaidToWorkersRUB       string `json:"paid_to_workers_rub"`
	ReserveBalanceRUB      string `json:"reserve_balance_rub"`
	ReserveObligationRUB   string `json:"reserve_obligation_rub"`
	ReserveDeficitRUB      string `json:"reserve_deficit_rub"`
	OwnerNetIncomeRUB      string `json:"owner_net_income_rub"`
	OwnerIncomeTargetRUB   string `json:"owner_income_target_rub"`
	OwnerTargetProgressPct string `json:"owner_target_progress_pct"`
	UnmatchedCount         int64  `json:"unmatched_count"`
	OverdueCount           int64  `json:"overdue_count"`
	GeneratedAt            string `json:"generated_at"`
}

// RequireBusinessFeature keeps each rollout surface independently reversible.
func RequireBusinessFeature(flags *featureflag.Service, key string) func(http.Handler) http.Handler {
	return func(next http.Handler) http.Handler {
		return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
			if !featureflags.BusinessFeatureEnabled(r.Context(), flags, key) {
				writeError(w, http.StatusNotFound, "not found")
				return
			}
			next.ServeHTTP(w, r)
		})
	}
}

func decodeBusinessJSON(w http.ResponseWriter, r *http.Request, dst any) bool {
	r.Body = http.MaxBytesReader(w, r.Body, businessJSONBodyLimit)
	decoder := json.NewDecoder(r.Body)
	decoder.DisallowUnknownFields()
	if err := decoder.Decode(dst); err != nil {
		writeError(w, http.StatusBadRequest, "invalid JSON body")
		return false
	}
	if err := decoder.Decode(&struct{}{}); !errors.Is(err, io.EOF) {
		writeError(w, http.StatusBadRequest, "request body must contain one JSON object")
		return false
	}
	return true
}

func businessRequestIDs(w http.ResponseWriter, r *http.Request) (string, string, bool) {
	businessID := middleware.BusinessIDFromContext(r.Context())
	if businessID == "" {
		businessID = chi.URLParam(r, "businessId")
	}
	if _, ok := parseUUIDOrBadRequest(w, businessID, "business_id"); !ok {
		return "", "", false
	}
	userID, ok := requireUserID(w, r)
	if !ok {
		return "", "", false
	}
	return businessID, userID, true
}

func queryBusinessJSON(ctx context.Context, db dbExecutor, query string, args ...any) (json.RawMessage, error) {
	var raw []byte
	if err := db.QueryRow(ctx, query, args...).Scan(&raw); err != nil {
		return nil, err
	}
	if len(raw) == 0 {
		return json.RawMessage("[]"), nil
	}
	return json.RawMessage(raw), nil
}

func queryBusinessRowJSON(ctx context.Context, row pgx.Row) (json.RawMessage, error) {
	var raw []byte
	if err := row.Scan(&raw); err != nil {
		return nil, err
	}
	return json.RawMessage(raw), nil
}

func (h *Handler) insertBusinessAudit(
	ctx context.Context,
	tx pgx.Tx,
	businessID, actorUserID, action, entityType, entityID, reason string,
	beforeData, afterData json.RawMessage,
) error {
	requestID := randomID()
	_, err := tx.Exec(ctx, `
		INSERT INTO business_audit_event (
			business_id, actor_user_id, actor_type, action, entity_type,
			entity_id, request_id, reason, before_data, after_data
		) VALUES ($1, $2, 'user', $3, $4, NULLIF($5, '')::uuid, $6, NULLIF($7, ''), $8, $9)
	`, businessID, actorUserID, action, entityType, entityID, requestID, reason,
		nullableJSON(beforeData), nullableJSON(afterData))
	return err
}

func nullableJSON(value json.RawMessage) any {
	if len(value) == 0 || string(value) == "null" {
		return nil
	}
	return []byte(value)
}

func (h *Handler) GetBusinessSnapshot(w http.ResponseWriter, r *http.Request) {
	businessID, _, ok := businessRequestIDs(w, r)
	if !ok {
		return
	}

	queries := []struct {
		dst   *json.RawMessage
		query string
	}{
		{query: `SELECT COALESCE(jsonb_agg(to_jsonb(q) ORDER BY q.canonical_name), '[]'::jsonb) FROM (SELECT * FROM business_client WHERE business_id = $1 ORDER BY canonical_name) q`},
		{query: `SELECT COALESCE(jsonb_agg(to_jsonb(q) ORDER BY q.value), '[]'::jsonb) FROM (SELECT * FROM business_client_alias WHERE business_id = $1 ORDER BY value) q`},
		{query: `SELECT COALESCE(jsonb_agg(to_jsonb(q) ORDER BY q.name), '[]'::jsonb) FROM (SELECT * FROM business_client_payer WHERE business_id = $1 ORDER BY name) q`},
		{query: `SELECT COALESCE(jsonb_agg(to_jsonb(q) ORDER BY q.project_title), '[]'::jsonb) FROM (
			SELECT bcp.*, p.title AS project_title, p.status AS project_status, p.project_type,
			       w.name AS workspace_name, w.slug AS workspace_slug,
			       bc.canonical_name AS client_name,
			       cb.enabled AS billing_enabled, cb.mode AS billing_mode,
			       cb.elba_contractor_id AS billing_contractor_id, cb.subscription_fee_rub AS billing_subscription_fee_rub
			FROM business_client_project bcp
			JOIN project p ON p.id = bcp.project_id AND p.workspace_id = bcp.workspace_id
			JOIN workspace w ON w.id = bcp.workspace_id
			JOIN business_client bc ON bc.id = bcp.client_id AND bc.business_id = bcp.business_id
			LEFT JOIN client_billing_config cb ON cb.project_id = bcp.project_id
			WHERE bcp.business_id = $1 ORDER BY p.title
		) q`},
		{query: `SELECT COALESCE(jsonb_agg(to_jsonb(q) ORDER BY q.workspace_name), '[]'::jsonb) FROM (
			SELECT w.id AS workspace_id, w.name AS workspace_name, w.slug AS workspace_slug
			FROM business_workspace bw
			JOIN workspace w ON w.id = bw.workspace_id
			WHERE bw.business_id = $1 AND bw.kind <> 'archive' AND w.slug <> 'vitmax'
			ORDER BY w.name
		) q`},
		{query: `SELECT COALESCE(jsonb_agg(to_jsonb(q) ORDER BY q.workspace_name, q.project_title), '[]'::jsonb) FROM (
			SELECT p.id AS project_id, p.title AS project_title, p.status AS project_status, p.project_type,
			       w.id AS workspace_id, w.name AS workspace_name, w.slug AS workspace_slug
			FROM business_workspace bw
			JOIN workspace w ON w.id = bw.workspace_id
			JOIN project p ON p.workspace_id = w.id
			WHERE bw.business_id = $1 AND bw.kind <> 'archive' AND w.slug <> 'vitmax' AND p.status <> 'archived'
			ORDER BY w.name, p.title
		) q`},
		{query: `SELECT COALESCE(jsonb_agg(to_jsonb(q) ORDER BY q.classification, q.name), '[]'::jsonb) FROM (SELECT * FROM business_counterparty_classification WHERE business_id = $1 ORDER BY classification, name) q`},
		{query: `SELECT COALESCE(jsonb_agg(to_jsonb(q) ORDER BY q.status, q.name), '[]'::jsonb) FROM (
			SELECT ba.*, bc.canonical_name AS client_name, p.title AS project_title
			FROM business_agreement ba
			JOIN business_client bc ON bc.id = ba.client_id AND bc.business_id = ba.business_id
			LEFT JOIN project p ON p.id = ba.project_id
			WHERE ba.business_id = $1 ORDER BY ba.status, ba.name
		) q`},
		{query: `SELECT COALESCE(jsonb_agg(to_jsonb(q) ORDER BY q.due_on NULLS LAST, q.period_key), '[]'::jsonb) FROM (
			SELECT br.*, bc.canonical_name AS client_name, p.title AS project_title,
			       CASE WHEN br.status IN ('expected','invoiced','partially_paid','overdue')
			                  AND GREATEST(br.planned_amount_rub - br.paid_amount_rub, 0) > 0
			                  AND br.due_on < (now() AT TIME ZONE 'Asia/Yekaterinburg')::date THEN true ELSE false END AS is_overdue
			FROM business_receivable br
			JOIN business_client bc ON bc.id = br.client_id AND bc.business_id = br.business_id
			LEFT JOIN project p ON p.id = br.project_id
			WHERE br.business_id = $1 ORDER BY br.due_on NULLS LAST, br.period_key
		) q`},
		{query: `SELECT COALESCE(jsonb_agg(to_jsonb(q) ORDER BY q.created_at DESC), '[]'::jsonb) FROM (SELECT * FROM business_bank_import_batch WHERE business_id = $1 ORDER BY created_at DESC LIMIT 50) q`},
		{query: `SELECT COALESCE(jsonb_agg(to_jsonb(q) ORDER BY q.booked_on DESC, q.created_at DESC), '[]'::jsonb) FROM (
			SELECT t.*, EXISTS (
				SELECT 1 FROM business_transaction_match m
				WHERE m.business_id = t.business_id AND m.transaction_id = t.id AND m.status = 'confirmed'
			) AS is_matched
			FROM business_bank_transaction t
			WHERE t.business_id = $1 AND t.voided_at IS NULL
			ORDER BY t.booked_on DESC, t.created_at DESC LIMIT 5000
		) q`},
		{query: `SELECT COALESCE(jsonb_agg(to_jsonb(q) ORDER BY q.created_at DESC), '[]'::jsonb) FROM (SELECT * FROM business_transaction_match WHERE business_id = $1 ORDER BY created_at DESC LIMIT 500) q`},
		{query: `SELECT COALESCE(jsonb_agg(to_jsonb(q) ORDER BY q.incurred_on DESC), '[]'::jsonb) FROM (SELECT * FROM business_company_cost WHERE business_id = $1 ORDER BY incurred_on DESC LIMIT 500) q`},
		{query: `SELECT COALESCE(jsonb_agg(to_jsonb(q) ORDER BY q.status, q.name), '[]'::jsonb) FROM (SELECT * FROM business_recurring_cost WHERE business_id = $1 ORDER BY status, name) q`},
		{query: `SELECT COALESCE(jsonb_agg(to_jsonb(q) ORDER BY q.name), '[]'::jsonb) FROM (SELECT * FROM business_worker WHERE business_id = $1 ORDER BY name) q`},
		{query: `SELECT COALESCE(jsonb_agg(to_jsonb(q) ORDER BY q.effective_from DESC, q.pool), '[]'::jsonb) FROM (SELECT * FROM business_compensation_policy WHERE business_id = $1 ORDER BY effective_from DESC, pool) q`},
		{query: `SELECT COALESCE(jsonb_agg(to_jsonb(q) ORDER BY q.received_at DESC), '[]'::jsonb) FROM (
			SELECT bcr.*, bc.canonical_name AS client_name, p.title AS project_title, i.title AS issue_title
			FROM business_client_request bcr
			JOIN business_client bc ON bc.id = bcr.client_id AND bc.business_id = bcr.business_id
			LEFT JOIN project p ON p.id = bcr.project_id
			LEFT JOIN issue i ON i.id = bcr.linked_issue_id AND i.workspace_id = bcr.workspace_id
			WHERE bcr.business_id = $1 ORDER BY bcr.received_at DESC LIMIT 500
		) q`},
		{query: `SELECT COALESCE(jsonb_agg(to_jsonb(q) ORDER BY q.created_at DESC), '[]'::jsonb) FROM (
			SELECT bte.*, i.title AS issue_title, i.number AS issue_number, p.title AS project_title, bc.canonical_name AS client_name
			FROM business_task_economics bte
			JOIN issue i ON i.id = bte.issue_id AND i.workspace_id = bte.workspace_id
			JOIN project p ON p.id = bte.project_id AND p.workspace_id = bte.workspace_id
			LEFT JOIN business_client bc ON bc.id = bte.client_id AND bc.business_id = bte.business_id
			WHERE bte.business_id = $1 ORDER BY bte.created_at DESC LIMIT 500
		) q`},
		{query: `SELECT COALESCE(jsonb_agg(to_jsonb(q) ORDER BY q.created_at), '[]'::jsonb) FROM (
			SELECT btp.*, bw.name AS worker_name
			FROM business_task_participant btp JOIN business_worker bw ON bw.id = btp.worker_id AND bw.business_id = btp.business_id
			WHERE btp.business_id = $1 ORDER BY btp.created_at
		) q`},
		{query: `SELECT COALESCE(jsonb_agg(to_jsonb(q) ORDER BY q.created_at), '[]'::jsonb) FROM (SELECT * FROM business_receivable_task WHERE business_id = $1 ORDER BY created_at) q`},
		{query: `SELECT COALESCE(jsonb_agg(to_jsonb(q) ORDER BY q.created_at DESC), '[]'::jsonb) FROM (
			SELECT ba.*, bw.name AS worker_name,
			       COALESCE((SELECT sum(adj.amount_rub) FROM business_accrual_adjustment adj WHERE adj.business_id = ba.business_id AND adj.accrual_id = ba.id), 0) AS adjustment_rub
			FROM business_accrual ba JOIN business_worker bw ON bw.id = ba.worker_id AND bw.business_id = ba.business_id
			WHERE ba.business_id = $1 ORDER BY ba.created_at DESC LIMIT 1000
		) q`},
		{query: `SELECT COALESCE(jsonb_agg(to_jsonb(q) ORDER BY q.created_at DESC), '[]'::jsonb) FROM (SELECT * FROM business_quality_case WHERE business_id = $1 ORDER BY created_at DESC LIMIT 500) q`},
		{query: `SELECT COALESCE(jsonb_agg(to_jsonb(q) ORDER BY q.created_at DESC), '[]'::jsonb) FROM (SELECT * FROM business_accrual_adjustment WHERE business_id = $1 ORDER BY created_at DESC LIMIT 500) q`},
		{query: `SELECT COALESCE(jsonb_agg(to_jsonb(q) ORDER BY q.occurred_at DESC), '[]'::jsonb) FROM (SELECT * FROM business_reserve_ledger WHERE business_id = $1 ORDER BY occurred_at DESC LIMIT 1000) q`},
		{query: `SELECT COALESCE(jsonb_agg(to_jsonb(q) ORDER BY q.created_at DESC), '[]'::jsonb) FROM (SELECT * FROM business_payout_batch WHERE business_id = $1 ORDER BY created_at DESC LIMIT 200) q`},
		{query: `SELECT COALESCE(jsonb_agg(to_jsonb(q) ORDER BY q.created_at DESC), '[]'::jsonb) FROM (
			SELECT bpi.*, bw.name AS worker_name FROM business_payout_item bpi
			JOIN business_worker bw ON bw.id = bpi.worker_id AND bw.business_id = bpi.business_id
			WHERE bpi.business_id = $1 ORDER BY bpi.created_at DESC LIMIT 1000
		) q`},
		{query: `SELECT COALESCE(jsonb_agg(to_jsonb(q) ORDER BY q.created_at DESC), '[]'::jsonb) FROM (SELECT * FROM business_bank_outbox WHERE business_id = $1 ORDER BY created_at DESC LIMIT 200) q`},
		{query: `SELECT COALESCE(jsonb_agg(to_jsonb(q) ORDER BY q.created_at DESC), '[]'::jsonb) FROM (
			SELECT c.id, c.issue_id, i.title AS issue_title, i.number AS issue_number, c.project_id, p.title AS project_title,
			       c.workspace_id, c.price_rub, c.status, c.created_at, bcp.client_id, bc.canonical_name AS client_name,
			       COALESCE(bcp.service_type, 'development') AS service_type
			FROM client_billing_charge c
			JOIN issue i ON i.id = c.issue_id AND i.workspace_id = c.workspace_id
			JOIN project p ON p.id = c.project_id AND p.workspace_id = c.workspace_id
			JOIN business_workspace bw ON bw.workspace_id = c.workspace_id AND bw.business_id = $1
			LEFT JOIN business_client_project bcp ON bcp.project_id = c.project_id AND bcp.business_id = $1
			LEFT JOIN business_client bc ON bc.id = bcp.client_id AND bc.business_id = $1
			WHERE c.status <> 'void'
			  AND NOT EXISTS (SELECT 1 FROM business_task_economics e WHERE e.business_id = $1 AND e.issue_id = c.issue_id AND e.status <> 'superseded')
			ORDER BY c.created_at DESC LIMIT 200
		) q`},
		{query: `SELECT COALESCE(jsonb_agg(to_jsonb(q) ORDER BY q.month, q.client_id), '[]'::jsonb) FROM (
			SELECT bcp.client_id, to_char(COALESCE(per.ends_on - 1, c.created_at::date), 'YYYY-MM') AS month,
			       sum(c.price_rub) AS billed_rub, count(DISTINCT c.issue_id) AS issue_count
			FROM client_billing_charge c
			LEFT JOIN client_billing_period per ON per.id = c.period_id
			JOIN business_client_project bcp ON bcp.project_id = c.project_id AND bcp.business_id = $1
			WHERE c.status <> 'void'
			GROUP BY bcp.client_id, to_char(COALESCE(per.ends_on - 1, c.created_at::date), 'YYYY-MM')
		) q`},
		{query: `SELECT COALESCE(jsonb_agg(to_jsonb(q) ORDER BY q.month DESC, q.client_name, q.issue_title), '[]'::jsonb) FROM (
			SELECT bcp.client_id, bc.canonical_name AS client_name,
			       to_char(COALESCE(per.ends_on - 1, c.created_at::date), 'YYYY-MM') AS month,
			       c.issue_id, i.title AS issue_title, i.number AS issue_number,
			       c.project_id, p.title AS project_title, c.price_rub, c.status
			FROM client_billing_charge c
			LEFT JOIN client_billing_period per ON per.id = c.period_id
			JOIN issue i ON i.id = c.issue_id AND i.workspace_id = c.workspace_id
			JOIN project p ON p.id = c.project_id AND p.workspace_id = c.workspace_id
			JOIN business_client_project bcp ON bcp.project_id = c.project_id AND bcp.business_id = $1
			JOIN business_client bc ON bc.id = bcp.client_id AND bc.business_id = $1
			WHERE c.status <> 'void'
			ORDER BY month DESC, bc.canonical_name, i.title LIMIT 5000
		) q`},
		{query: `SELECT COALESCE(jsonb_agg(to_jsonb(q) ORDER BY q.created_at DESC), '[]'::jsonb) FROM (
			SELECT bct.*, a.filename AS file_name, a.content_type AS file_content_type, a.size_bytes AS file_size
			FROM business_contract bct
			LEFT JOIN attachment a ON a.id = bct.attachment_id
			WHERE bct.business_id = $1 ORDER BY bct.created_at DESC
		) q`},
	}

	response := BusinessSnapshotResponse{GeneratedAt: time.Now().UTC().Format(time.RFC3339)}
	destinations := []*json.RawMessage{
		&response.Clients, &response.Aliases, &response.Payers, &response.Projects,
		&response.AvailableWorkspaces, &response.AvailableProjects,
		&response.Counterparties, &response.Agreements, &response.Receivables, &response.BankImports,
		&response.Transactions, &response.Matches, &response.CompanyCosts, &response.RecurringCosts,
		&response.Workers,
		&response.Policies, &response.ClientRequests, &response.TaskEconomics, &response.TaskParticipants,
		&response.ReceivableTasks, &response.Accruals, &response.QualityCases, &response.AccrualAdjustments,
		&response.ReserveLedger, &response.PayoutBatches, &response.PayoutItems, &response.BankOutbox,
		&response.BillingCandidates, &response.BillingMonthTotals, &response.BillingTasks,
		&response.Contracts,
	}
	for index := range queries {
		queries[index].dst = destinations[index]
		raw, err := queryBusinessJSON(r.Context(), h.DB, queries[index].query, businessID)
		if err != nil {
			writeError(w, http.StatusInternalServerError, "failed to load business snapshot")
			return
		}
		*queries[index].dst = raw
	}
	writeJSON(w, http.StatusOK, response)
}

func (h *Handler) GetBusinessDashboard(w http.ResponseWriter, r *http.Request) {
	businessID, _, ok := businessRequestIDs(w, r)
	if !ok {
		return
	}
	var month, monthStart, monthEnd string
	if year := r.URL.Query().Get("year"); year != "" {
		parsedYear, err := strconv.Atoi(year)
		if err != nil || parsedYear < 2000 || parsedYear > 2100 {
			writeError(w, http.StatusBadRequest, "year must use YYYY")
			return
		}
		month = year
		monthStart = fmt.Sprintf("%04d-01-01", parsedYear)
		monthEnd = fmt.Sprintf("%04d-01-01", parsedYear+1)
	} else {
		var monthOK bool
		month, monthStart, monthEnd, monthOK = parseBusinessMonth(w, r.URL.Query().Get("month"))
		if !monthOK {
			return
		}
	}
	workspaceID := r.URL.Query().Get("workspace_id")
	clientID := r.URL.Query().Get("client_id")
	projectID := r.URL.Query().Get("project_id")
	serviceType := r.URL.Query().Get("service_type")
	for field, value := range map[string]string{"workspace_id": workspaceID, "client_id": clientID, "project_id": projectID} {
		if value != "" {
			if _, valid := parseUUIDOrBadRequest(w, value, field); !valid {
				return
			}
		}
	}
	if serviceType != "" && !containsBusinessString([]string{"development", "support", "seo", "content", "internal"}, serviceType) {
		writeError(w, http.StatusBadRequest, "invalid service_type")
		return
	}

	row := h.DB.QueryRow(r.Context(), businessDashboardSQL,
		businessID, monthStart, monthEnd, workspaceID, clientID, projectID, serviceType)
	response := BusinessDashboardResponse{Month: month, GeneratedAt: time.Now().UTC().Format(time.RFC3339)}
	if err := row.Scan(
		&response.ExpectedRUB,
		&response.InvoicedRUB,
		&response.ReceivablePaidRUB,
		&response.OverdueRUB,
		&response.NotInvoicedRUB,
		&response.BankClientIncomeRUB,
		&response.VitmaxTransitRUB,
		&response.TransferRUB,
		&response.UnknownInboundRUB,
		&response.TaskValueRUB,
		&response.ParticipantAccruedRUB,
		&response.CompanyTargetPoolRUB,
		&response.OwnerTargetMarginRUB,
		&response.CompanyCostsRUB,
		&response.PayableRUB,
		&response.PaidToWorkersRUB,
		&response.ReserveBalanceRUB,
		&response.ReserveObligationRUB,
		&response.ReserveDeficitRUB,
		&response.OwnerNetIncomeRUB,
		&response.OwnerIncomeTargetRUB,
		&response.OwnerTargetProgressPct,
		&response.UnmatchedCount,
		&response.OverdueCount,
	); err != nil {
		writeError(w, http.StatusInternalServerError, "failed to load business dashboard")
		return
	}
	writeJSON(w, http.StatusOK, response)
}

const businessDashboardSQL = `
WITH scope AS (
	SELECT $1::uuid AS business_id, $2::date AS month_start, $3::date AS month_end,
	       NULLIF($4, '')::uuid AS workspace_id, NULLIF($5, '')::uuid AS client_id,
	       NULLIF($6, '')::uuid AS project_id, NULLIF($7, '')::text AS service_type
),
scoped_receivable AS (
	SELECT r.* FROM business_receivable r, scope s
	WHERE r.business_id = s.business_id AND r.period_start < s.month_end AND r.period_end >= s.month_start
	  AND (s.client_id IS NULL OR r.client_id = s.client_id)
	  AND (s.project_id IS NULL OR r.project_id = s.project_id)
	  AND (s.workspace_id IS NULL OR EXISTS (
		SELECT 1 FROM business_client_project cp WHERE cp.business_id = r.business_id
		AND cp.project_id = r.project_id AND cp.workspace_id = s.workspace_id
	  ))
	  AND (s.service_type IS NULL OR EXISTS (
		SELECT 1 FROM business_agreement a WHERE a.id = r.agreement_id AND a.business_id = r.business_id
		AND a.service_type = s.service_type
	  ))
),
scoped_economics AS (
	SELECT e.* FROM business_task_economics e, scope s
	WHERE e.business_id = s.business_id AND e.status = 'accepted'
	  AND e.accepted_at >= s.month_start::timestamptz AND e.accepted_at < s.month_end::timestamptz
	  AND (s.workspace_id IS NULL OR e.workspace_id = s.workspace_id)
	  AND (s.client_id IS NULL OR e.client_id = s.client_id)
	  AND (s.project_id IS NULL OR e.project_id = s.project_id)
	  AND (s.service_type IS NULL OR e.service_type = s.service_type)
),
scoped_transactions AS (
	SELECT t.* FROM business_bank_transaction t, scope s
	WHERE t.business_id = s.business_id AND t.voided_at IS NULL
	  AND t.booked_on >= s.month_start AND t.booked_on < s.month_end
	  AND (
		s.workspace_id IS NULL AND s.client_id IS NULL AND s.project_id IS NULL AND s.service_type IS NULL
		OR EXISTS (
			SELECT 1 FROM business_transaction_match m
			JOIN business_receivable r ON m.target_type = 'receivable' AND r.id = m.target_id AND r.business_id = m.business_id
			LEFT JOIN business_client_project cp ON cp.business_id = r.business_id AND cp.project_id = r.project_id
			LEFT JOIN business_agreement a ON a.id = r.agreement_id AND a.business_id = r.business_id
			WHERE m.business_id = t.business_id AND m.transaction_id = t.id AND m.status = 'confirmed'
			  AND (s.workspace_id IS NULL OR cp.workspace_id = s.workspace_id)
			  AND (s.client_id IS NULL OR r.client_id = s.client_id)
			  AND (s.project_id IS NULL OR r.project_id = s.project_id)
			  AND (s.service_type IS NULL OR a.service_type = s.service_type)
		)
	  )
),
accrual_totals AS (
	SELECT a.*,
	       a.original_amount_rub + COALESCE((SELECT sum(x.amount_rub) FROM business_accrual_adjustment x WHERE x.business_id = a.business_id AND x.accrual_id = a.id), 0) AS adjusted_amount_rub
	FROM business_accrual a
	WHERE a.business_id = $1::uuid AND EXISTS (SELECT 1 FROM scoped_economics e WHERE e.id = a.task_economics_id)
),
period_months AS (
	SELECT generate_series(
		date_trunc('month', s.month_start::date),
		date_trunc('month', (s.month_end::date - 1)),
		interval '1 month'
	)::date AS month_start
	FROM scope s
),
recurring_charges AS (
	SELECT rc.amount, rc.currency,
	       make_date(
		       EXTRACT(year FROM pm.month_start)::int,
		       EXTRACT(month FROM pm.month_start)::int,
		       LEAST(rc.charge_day::int, EXTRACT(day FROM (pm.month_start + interval '1 month - 1 day'))::int)
	       ) AS charge_on
	FROM business_recurring_cost rc
	CROSS JOIN period_months pm
	CROSS JOIN scope s
	WHERE rc.business_id = s.business_id
	  AND rc.status = 'active'
	  AND rc.starts_on < (pm.month_start + interval '1 month')::date
	  AND (rc.ends_on IS NULL OR rc.ends_on >= pm.month_start)
	  AND (rc.frequency = 'monthly' OR EXTRACT(month FROM rc.starts_on) = EXTRACT(month FROM pm.month_start))
	  AND s.workspace_id IS NULL AND s.client_id IS NULL
	  AND s.project_id IS NULL AND s.service_type IS NULL
),
recurring_cost_total AS (
	SELECT round(COALESCE(sum(
		CASE WHEN currency = 'RUB' THEN amount ELSE amount * COALESCE((
			SELECT usd_rub FROM fx_rate_daily
			WHERE date <= charge_on ORDER BY date DESC LIMIT 1
		), 90) END
	), 0) / 100, 0) * 100 AS amount_rub
	FROM recurring_charges
),
reserve AS (
	SELECT COALESCE(sum(amount_rub), 0) AS balance FROM business_reserve_ledger WHERE business_id = $1::uuid
),
metrics AS (
	SELECT
		COALESCE((SELECT sum(planned_amount_rub) FROM scoped_receivable WHERE status = 'expected'), 0) AS expected,
		COALESCE((SELECT sum(GREATEST(planned_amount_rub - paid_amount_rub, 0)) FROM scoped_receivable WHERE status IN ('invoiced','partially_paid','overdue')), 0) AS invoiced,
		COALESCE((SELECT sum(paid_amount_rub) FROM scoped_receivable), 0) AS receivable_paid,
		COALESCE((SELECT sum(GREATEST(planned_amount_rub - paid_amount_rub, 0)) FROM scoped_receivable WHERE status NOT IN ('paid','skipped','written_off') AND due_on < (now() AT TIME ZONE 'Asia/Yekaterinburg')::date), 0) AS overdue,
		COALESCE((SELECT sum(planned_amount_rub) FROM scoped_receivable WHERE status = 'expected' AND invoice_on <= (now() AT TIME ZONE 'Asia/Yekaterinburg')::date), 0) AS not_invoiced,
		COALESCE((SELECT sum(amount_rub) FROM scoped_transactions WHERE direction = 'inbound' AND classification = 'client_income'), 0) AS bank_income,
		COALESCE((SELECT sum(amount_rub) FROM scoped_transactions WHERE classification = 'vitmax_transit'), 0) AS vitmax_transit,
		COALESCE((SELECT sum(amount_rub) FROM scoped_transactions WHERE classification = 'transfer'), 0) AS transfer,
		COALESCE((SELECT sum(t.amount_rub) FROM scoped_transactions t
			WHERE t.direction = 'inbound' AND t.classification NOT IN ('transfer','vitmax_transit')
			  AND NOT EXISTS (SELECT 1 FROM business_transaction_match m WHERE m.business_id = t.business_id AND m.transaction_id = t.id AND m.status = 'confirmed')), 0) AS unknown_inbound,
		COALESCE((SELECT sum(service_value_rub) FROM scoped_economics), 0) AS task_value,
		COALESCE((SELECT sum(adjusted_amount_rub) FROM accrual_totals), 0) AS participant_accrued,
		COALESCE((SELECT sum(c.amount_rub) FROM business_company_cost c, scope s WHERE c.business_id = s.business_id AND c.voided_at IS NULL AND c.incurred_on >= s.month_start AND c.incurred_on < s.month_end AND (s.workspace_id IS NULL OR c.workspace_id = s.workspace_id) AND (s.client_id IS NULL OR c.client_id = s.client_id) AND (s.project_id IS NULL OR c.project_id = s.project_id)), 0)
			+ (SELECT amount_rub FROM recurring_cost_total) AS company_costs,
		COALESCE((SELECT sum(GREATEST(LEAST(adjusted_amount_rub, funded_rub + reserve_funded_rub) - paid_rub, 0)) FROM accrual_totals WHERE status IN ('partially_payable','payable','in_payout')), 0) AS payable,
		COALESCE((SELECT sum(paid_rub) FROM accrual_totals), 0) AS paid_workers,
		(SELECT balance FROM reserve) AS reserve_balance,
		COALESCE((SELECT sum(GREATEST(adjusted_amount_rub - funded_rub - reserve_funded_rub, 0)) FROM accrual_totals WHERE reserve_due_on <= (now() AT TIME ZONE 'Asia/Yekaterinburg')::date), 0) AS reserve_obligation,
		(SELECT monthly_owner_income_target_rub FROM business_account WHERE id = $1::uuid) AS owner_target,
		(SELECT count(*) FROM scoped_transactions t WHERE t.direction = 'inbound' AND t.classification NOT IN ('transfer','vitmax_transit') AND NOT EXISTS (SELECT 1 FROM business_transaction_match m WHERE m.business_id = t.business_id AND m.transaction_id = t.id AND m.status = 'confirmed')) AS unmatched_count,
		(SELECT count(*) FROM scoped_receivable WHERE status NOT IN ('paid','skipped','written_off')
			AND GREATEST(planned_amount_rub - paid_amount_rub, 0) > 0
			AND due_on < (now() AT TIME ZONE 'Asia/Yekaterinburg')::date) AS overdue_count
)
SELECT
	expected::text, invoiced::text, receivable_paid::text, overdue::text, not_invoiced::text,
	bank_income::text, vitmax_transit::text, transfer::text, unknown_inbound::text,
	task_value::text, participant_accrued::text, round(task_value * 0.15, 2)::text,
	round(task_value * 0.50, 2)::text, company_costs::text, payable::text, paid_workers::text,
	reserve_balance::text, reserve_obligation::text,
	GREATEST(reserve_obligation - GREATEST(reserve_balance, 0), 0)::text,
	(bank_income - company_costs - paid_workers)::text, owner_target::text,
	CASE WHEN owner_target > 0 THEN round((bank_income - company_costs - paid_workers) / owner_target * 100, 2) ELSE 0 END::text,
	unmatched_count, overdue_count
FROM metrics`

type businessSeriesPoint struct {
	Month             string `json:"month"`
	ExpectedRUB       string `json:"expected_rub"`
	ReceivablePaidRUB string `json:"receivable_paid_rub"`
	BankIncomeRUB     string `json:"bank_income_rub"`
	VitmaxTransitRUB  string `json:"vitmax_transit_rub"`
	UnknownInboundRUB string `json:"unknown_inbound_rub"`
}

const businessSeriesSQL = `
WITH buckets AS (
	SELECT CASE WHEN $4::text = 'day'
		THEN $2::date + g.n
		ELSE (date_trunc('month', $2::date) + make_interval(months => g.n))::date
	END AS bucket_start
	FROM generate_series(0, $3::int - 1) AS g(n)
), ranged AS (
	SELECT bucket_start,
	       CASE WHEN $4::text = 'day' THEN bucket_start + 1 ELSE (bucket_start + interval '1 month')::date END AS bucket_end
	FROM buckets
)
SELECT CASE WHEN $4::text = 'day' THEN to_char(b.bucket_start, 'YYYY-MM-DD') ELSE to_char(b.bucket_start, 'YYYY-MM') END,
	COALESCE((SELECT sum(r.planned_amount_rub) FROM business_receivable r WHERE r.business_id = $1 AND r.status NOT IN ('skipped','written_off') AND COALESCE(r.due_on, r.invoice_on, r.period_start) >= b.bucket_start AND COALESCE(r.due_on, r.invoice_on, r.period_start) < b.bucket_end), 0)::text,
	COALESCE((SELECT sum(r.paid_amount_rub) FROM business_receivable r WHERE r.business_id = $1 AND r.status NOT IN ('skipped','written_off') AND COALESCE(r.due_on, r.invoice_on, r.period_start) >= b.bucket_start AND COALESCE(r.due_on, r.invoice_on, r.period_start) < b.bucket_end), 0)::text,
	COALESCE((SELECT sum(t.amount_rub) FROM business_bank_transaction t WHERE t.business_id = $1 AND t.voided_at IS NULL AND t.direction = 'inbound' AND t.classification = 'client_income' AND t.booked_on >= b.bucket_start AND t.booked_on < b.bucket_end), 0)::text,
	COALESCE((SELECT sum(t.amount_rub) FROM business_bank_transaction t WHERE t.business_id = $1 AND t.voided_at IS NULL AND t.classification = 'vitmax_transit' AND t.booked_on >= b.bucket_start AND t.booked_on < b.bucket_end), 0)::text,
	COALESCE((SELECT sum(t.amount_rub) FROM business_bank_transaction t WHERE t.business_id = $1 AND t.voided_at IS NULL AND t.direction = 'inbound' AND t.classification = 'unknown' AND t.booked_on >= b.bucket_start AND t.booked_on < b.bucket_end), 0)::text
FROM ranged b ORDER BY b.bucket_start`

func (h *Handler) GetBusinessDashboardSeries(w http.ResponseWriter, r *http.Request) {
	businessID, _, ok := businessRequestIDs(w, r)
	if !ok {
		return
	}
	location, err := time.LoadLocation("Asia/Yekaterinburg")
	if err != nil {
		location = time.FixedZone("Asia/Yekaterinburg", 5*60*60)
	}
	granularity := r.URL.Query().Get("granularity")
	if granularity == "" {
		granularity = "month"
	}
	if granularity != "month" && granularity != "day" {
		writeError(w, http.StatusBadRequest, "granularity must be month or day")
		return
	}
	from := r.URL.Query().Get("from")
	if from == "" {
		from = time.Now().In(location).Format("2006-01-02")
	}
	parsed, err := time.ParseInLocation("2006-01-02", from, location)
	if err != nil {
		parsed, err = time.ParseInLocation("2006-01", from, location)
		if err != nil {
			writeError(w, http.StatusBadRequest, "from must use YYYY-MM or YYYY-MM-DD")
			return
		}
	}
	periods := 12
	rawPeriods := r.URL.Query().Get("periods")
	if rawPeriods == "" {
		rawPeriods = r.URL.Query().Get("months")
	}
	if rawPeriods != "" {
		value, err := strconv.Atoi(rawPeriods)
		if err != nil || value < 1 || value > 366 {
			writeError(w, http.StatusBadRequest, "periods must be between 1 and 366")
			return
		}
		periods = value
	}
	rows, err := h.DB.Query(r.Context(), businessSeriesSQL, businessID, parsed.Format("2006-01-02"), periods, granularity)
	if err != nil {
		writeError(w, http.StatusInternalServerError, "failed to load business series")
		return
	}
	defer rows.Close()
	points := make([]businessSeriesPoint, 0, periods)
	for rows.Next() {
		var point businessSeriesPoint
		if err := rows.Scan(&point.Month, &point.ExpectedRUB, &point.ReceivablePaidRUB, &point.BankIncomeRUB, &point.VitmaxTransitRUB, &point.UnknownInboundRUB); err != nil {
			writeError(w, http.StatusInternalServerError, "failed to load business series")
			return
		}
		points = append(points, point)
	}
	if rows.Err() != nil {
		writeError(w, http.StatusInternalServerError, "failed to load business series")
		return
	}
	writeJSON(w, http.StatusOK, map[string]any{"from": parsed.Format("2006-01-02"), "months": periods, "periods": periods, "granularity": granularity, "points": points})
}

type updateBusinessAgreementRequest struct {
	Name           *string `json:"name"`
	ServiceType    *string `json:"service_type"`
	Model          *string `json:"model"`
	AmountRUB      *string `json:"amount_rub"`
	HourlyRateRUB  *string `json:"hourly_rate_rub"`
	CapRUB         *string `json:"cap_rub"`
	InvoiceDay     *string `json:"invoice_day"`
	DueDays        *string `json:"due_days"`
	PaymentChannel *string `json:"payment_channel"`
	Status         *string `json:"status"`
	IsEstimate     *bool   `json:"is_estimate"`
	NeedsReview    *bool   `json:"needs_review"`
}

func validBusinessMoney(value string) bool {
	amount, err := strconv.ParseFloat(value, 64)
	return err == nil && amount >= 0 && amount < 1e12
}

func (h *Handler) UpdateBusinessAgreement(w http.ResponseWriter, r *http.Request) {
	businessID, userID, ok := businessRequestIDs(w, r)
	if !ok {
		return
	}
	agreementID := chi.URLParam(r, "agreementId")
	if _, valid := parseUUIDOrBadRequest(w, agreementID, "agreement_id"); !valid {
		return
	}
	var request updateBusinessAgreementRequest
	if !decodeBusinessJSON(w, r, &request) {
		return
	}
	if request.Name != nil && strings.TrimSpace(*request.Name) == "" {
		writeError(w, http.StatusBadRequest, "name must not be empty")
		return
	}
	if request.ServiceType != nil && !containsBusinessString([]string{"development", "support", "seo", "content", "internal"}, *request.ServiceType) {
		writeError(w, http.StatusBadRequest, "invalid service_type")
		return
	}
	if request.Model != nil && !containsBusinessString([]string{"fixed", "cap", "time_material", "project"}, *request.Model) {
		writeError(w, http.StatusBadRequest, "invalid model")
		return
	}
	if request.PaymentChannel != nil && !containsBusinessString([]string{"bank", "personal_card"}, *request.PaymentChannel) {
		writeError(w, http.StatusBadRequest, "invalid payment_channel")
		return
	}
	if request.Status != nil && !containsBusinessString([]string{"active", "archived"}, *request.Status) {
		writeError(w, http.StatusBadRequest, "invalid status")
		return
	}
	for _, money := range []*string{request.AmountRUB, request.HourlyRateRUB, request.CapRUB} {
		if money != nil && strings.TrimSpace(*money) != "" && !validBusinessMoney(strings.TrimSpace(*money)) {
			writeError(w, http.StatusBadRequest, "invalid amount")
			return
		}
	}
	for _, day := range []*string{request.InvoiceDay, request.DueDays} {
		if day != nil && strings.TrimSpace(*day) != "" {
			value, err := strconv.Atoi(strings.TrimSpace(*day))
			if err != nil || value < 0 || value > 31 {
				writeError(w, http.StatusBadRequest, "invalid day value")
				return
			}
		}
	}

	tx, err := h.TxStarter.Begin(r.Context())
	if err != nil {
		writeError(w, http.StatusInternalServerError, "failed to update agreement")
		return
	}
	defer tx.Rollback(r.Context())

	var (
		name, serviceType, model, paymentChannel, status string
		amount                                           string
		hourly, cap, invoiceDay, dueDays                 *string
		isEstimate, needsReview                          bool
	)
	err = tx.QueryRow(r.Context(), `
		SELECT name, service_type, model, COALESCE(amount_rub::text, ''), hourly_rate_rub::text, cap_rub::text,
		       invoice_day::text, due_days::text, payment_channel, status, is_estimate, needs_review
		FROM business_agreement WHERE business_id = $1 AND id = $2 FOR UPDATE
	`, businessID, agreementID).Scan(&name, &serviceType, &model, &amount, &hourly, &cap, &invoiceDay, &dueDays, &paymentChannel, &status, &isEstimate, &needsReview)
	if errors.Is(err, pgx.ErrNoRows) {
		writeError(w, http.StatusNotFound, "agreement not found")
		return
	}
	if err != nil {
		writeError(w, http.StatusInternalServerError, "failed to update agreement")
		return
	}

	mergeNullable := func(current *string, incoming *string) *string {
		if incoming == nil {
			return current
		}
		trimmed := strings.TrimSpace(*incoming)
		if trimmed == "" {
			return nil
		}
		return &trimmed
	}
	if request.Name != nil {
		name = strings.TrimSpace(*request.Name)
	}
	if request.ServiceType != nil {
		serviceType = *request.ServiceType
	}
	if request.Model != nil {
		model = *request.Model
	}
	if request.PaymentChannel != nil {
		paymentChannel = *request.PaymentChannel
	}
	if request.Status != nil {
		status = *request.Status
	}
	if request.AmountRUB != nil && strings.TrimSpace(*request.AmountRUB) != "" {
		amount = strings.TrimSpace(*request.AmountRUB)
	}
	hourly = mergeNullable(hourly, request.HourlyRateRUB)
	cap = mergeNullable(cap, request.CapRUB)
	invoiceDay = mergeNullable(invoiceDay, request.InvoiceDay)
	dueDays = mergeNullable(dueDays, request.DueDays)
	if request.IsEstimate != nil {
		isEstimate = *request.IsEstimate
	}
	if request.NeedsReview != nil {
		needsReview = *request.NeedsReview
	}

	if _, err := tx.Exec(r.Context(), `
		UPDATE business_agreement SET
			name = $3, service_type = $4, model = $5, amount_rub = NULLIF($6, '')::numeric,
			hourly_rate_rub = $7::numeric, cap_rub = $8::numeric, invoice_day = $9::int, due_days = COALESCE($10::int, due_days),
			payment_channel = $11, status = $12, is_estimate = $13, needs_review = $14, updated_at = now()
		WHERE business_id = $1 AND id = $2
	`, businessID, agreementID, name, serviceType, model, amount, hourly, cap, invoiceDay, dueDays, paymentChannel, status, isEstimate, needsReview); err != nil {
		writeError(w, http.StatusInternalServerError, "failed to update agreement")
		return
	}
	if err := h.insertBusinessAudit(r.Context(), tx, businessID, userID, "agreement.update", "business_agreement", agreementID, "owner edit", nil, nil); err != nil {
		writeError(w, http.StatusInternalServerError, "failed to update agreement")
		return
	}
	if err := tx.Commit(r.Context()); err != nil {
		writeError(w, http.StatusInternalServerError, "failed to update agreement")
		return
	}
	writeJSON(w, http.StatusOK, map[string]string{"id": agreementID})
}

type updateBusinessReceivableRequest struct {
	PlannedAmountRUB *string `json:"planned_amount_rub"`
	InvoiceOn        *string `json:"invoice_on"`
	DueOn            *string `json:"due_on"`
	Status           *string `json:"status"`
	NeedsReview      *bool   `json:"needs_review"`
	Notes            *string `json:"notes"`
}

func (h *Handler) UpdateBusinessReceivable(w http.ResponseWriter, r *http.Request) {
	businessID, userID, ok := businessRequestIDs(w, r)
	if !ok {
		return
	}
	receivableID := chi.URLParam(r, "receivableId")
	if _, valid := parseUUIDOrBadRequest(w, receivableID, "receivable_id"); !valid {
		return
	}
	var request updateBusinessReceivableRequest
	if !decodeBusinessJSON(w, r, &request) {
		return
	}
	if request.PlannedAmountRUB != nil && !validBusinessMoney(strings.TrimSpace(*request.PlannedAmountRUB)) {
		writeError(w, http.StatusBadRequest, "invalid planned_amount_rub")
		return
	}
	if request.Status != nil && !containsBusinessString([]string{"expected", "invoiced", "skipped", "written_off"}, *request.Status) {
		writeError(w, http.StatusBadRequest, "invalid status")
		return
	}
	for _, day := range []*string{request.InvoiceOn, request.DueOn} {
		if day != nil && strings.TrimSpace(*day) != "" {
			if _, err := time.Parse("2006-01-02", strings.TrimSpace(*day)); err != nil {
				writeError(w, http.StatusBadRequest, "dates must use YYYY-MM-DD")
				return
			}
		}
	}

	tx, err := h.TxStarter.Begin(r.Context())
	if err != nil {
		writeError(w, http.StatusInternalServerError, "failed to update receivable")
		return
	}
	defer tx.Rollback(r.Context())

	var (
		planned, status         string
		invoiceOn, dueOn, notes *string
		needsReview             bool
	)
	err = tx.QueryRow(r.Context(), `
		SELECT planned_amount_rub::text, invoice_on::text, due_on::text, status, needs_review, notes
		FROM business_receivable WHERE business_id = $1 AND id = $2 FOR UPDATE
	`, businessID, receivableID).Scan(&planned, &invoiceOn, &dueOn, &status, &needsReview, &notes)
	if errors.Is(err, pgx.ErrNoRows) {
		writeError(w, http.StatusNotFound, "receivable not found")
		return
	}
	if err != nil {
		writeError(w, http.StatusInternalServerError, "failed to update receivable")
		return
	}
	if status == "paid" || status == "partially_paid" {
		if request.Status != nil || request.PlannedAmountRUB != nil {
			writeError(w, http.StatusConflict, "paid receivables can only change notes and review flag")
			return
		}
	}

	mergeNullable := func(current *string, incoming *string) *string {
		if incoming == nil {
			return current
		}
		trimmed := strings.TrimSpace(*incoming)
		if trimmed == "" {
			return nil
		}
		return &trimmed
	}
	if request.PlannedAmountRUB != nil {
		planned = strings.TrimSpace(*request.PlannedAmountRUB)
	}
	if request.Status != nil {
		status = *request.Status
	}
	if request.NeedsReview != nil {
		needsReview = *request.NeedsReview
	}
	invoiceOn = mergeNullable(invoiceOn, request.InvoiceOn)
	dueOn = mergeNullable(dueOn, request.DueOn)
	notes = mergeNullable(notes, request.Notes)

	if _, err := tx.Exec(r.Context(), `
		UPDATE business_receivable SET
			planned_amount_rub = $3::numeric, invoice_on = $4::date, due_on = $5::date,
			status = $6, needs_review = $7, notes = $8, updated_at = now()
		WHERE business_id = $1 AND id = $2
	`, businessID, receivableID, planned, invoiceOn, dueOn, status, needsReview, notes); err != nil {
		writeError(w, http.StatusInternalServerError, "failed to update receivable")
		return
	}
	if err := h.insertBusinessAudit(r.Context(), tx, businessID, userID, "receivable.update", "business_receivable", receivableID, "owner edit", nil, nil); err != nil {
		writeError(w, http.StatusInternalServerError, "failed to update receivable")
		return
	}
	if err := tx.Commit(r.Context()); err != nil {
		writeError(w, http.StatusInternalServerError, "failed to update receivable")
		return
	}
	writeJSON(w, http.StatusOK, map[string]string{"id": receivableID})
}

func parseBusinessMonth(w http.ResponseWriter, value string) (string, string, string, bool) {
	location, err := time.LoadLocation("Asia/Yekaterinburg")
	if err != nil {
		location = time.FixedZone("Asia/Yekaterinburg", 5*60*60)
	}
	if value == "" {
		value = time.Now().In(location).Format("2006-01")
	}
	parsed, err := time.ParseInLocation("2006-01", value, location)
	if err != nil {
		writeError(w, http.StatusBadRequest, "month must use YYYY-MM")
		return "", "", "", false
	}
	return value, parsed.Format("2006-01-02"), parsed.AddDate(0, 1, 0).Format("2006-01-02"), true
}

type createBusinessClientRequest struct {
	CanonicalName         string  `json:"canonical_name"`
	Status                string  `json:"status"`
	ManagerUserID         *string `json:"manager_user_id"`
	PrimaryPaymentChannel string  `json:"primary_payment_channel"`
	Notes                 *string `json:"notes"`
}

func (h *Handler) CreateBusinessClient(w http.ResponseWriter, r *http.Request) {
	businessID, userID, ok := businessRequestIDs(w, r)
	if !ok {
		return
	}
	var request createBusinessClientRequest
	if !decodeBusinessJSON(w, r, &request) {
		return
	}
	request.CanonicalName = strings.TrimSpace(request.CanonicalName)
	if request.Status == "" {
		request.Status = "prospect"
	}
	if request.PrimaryPaymentChannel == "" {
		request.PrimaryPaymentChannel = "bank"
	}
	if request.CanonicalName == "" {
		writeError(w, http.StatusBadRequest, "canonical_name is required")
		return
	}
	if request.ManagerUserID != nil {
		if _, valid := parseUUIDOrBadRequest(w, *request.ManagerUserID, "manager_user_id"); !valid {
			return
		}
	}
	tx, err := h.TxStarter.Begin(r.Context())
	if err != nil {
		writeError(w, http.StatusInternalServerError, "failed to start transaction")
		return
	}
	defer tx.Rollback(r.Context())
	raw, err := queryBusinessRowJSON(r.Context(), tx.QueryRow(r.Context(), `
		INSERT INTO business_client (business_id, canonical_name, status, manager_user_id, primary_payment_channel, notes)
		VALUES ($1, $2, $3, NULLIF($4, '')::uuid, $5, $6)
		RETURNING to_jsonb(business_client)
	`, businessID, request.CanonicalName, request.Status, stringValue(request.ManagerUserID), request.PrimaryPaymentChannel, request.Notes))
	if isUniqueViolation(err) {
		writeError(w, http.StatusConflict, "client already exists")
		return
	}
	if err != nil {
		writeError(w, http.StatusBadRequest, "failed to create client")
		return
	}
	var created struct {
		ID string `json:"id"`
	}
	if err := json.Unmarshal(raw, &created); err != nil {
		writeError(w, http.StatusInternalServerError, "failed to encode client")
		return
	}
	if err := h.insertBusinessAudit(r.Context(), tx, businessID, userID, "client.created", "business_client", created.ID, "", nil, raw); err != nil {
		writeError(w, http.StatusInternalServerError, "failed to audit client")
		return
	}
	if err := tx.Commit(r.Context()); err != nil {
		writeError(w, http.StatusInternalServerError, "failed to create client")
		return
	}
	writeJSON(w, http.StatusCreated, raw)
}

type updateBusinessClientRequest struct {
	CanonicalName         *string `json:"canonical_name"`
	Status                *string `json:"status"`
	ManagerUserID         *string `json:"manager_user_id"`
	PrimaryPaymentChannel *string `json:"primary_payment_channel"`
	Notes                 *string `json:"notes"`
}

func (h *Handler) UpdateBusinessClient(w http.ResponseWriter, r *http.Request) {
	businessID, userID, ok := businessRequestIDs(w, r)
	if !ok {
		return
	}
	clientID := chi.URLParam(r, "clientId")
	if _, valid := parseUUIDOrBadRequest(w, clientID, "client_id"); !valid {
		return
	}
	var request updateBusinessClientRequest
	if !decodeBusinessJSON(w, r, &request) {
		return
	}
	if request.CanonicalName != nil {
		trimmed := strings.TrimSpace(*request.CanonicalName)
		if trimmed == "" {
			writeError(w, http.StatusBadRequest, "canonical_name cannot be empty")
			return
		}
		request.CanonicalName = &trimmed
	}
	if request.ManagerUserID != nil && *request.ManagerUserID != "" {
		if _, valid := parseUUIDOrBadRequest(w, *request.ManagerUserID, "manager_user_id"); !valid {
			return
		}
	}
	tx, err := h.TxStarter.Begin(r.Context())
	if err != nil {
		writeError(w, http.StatusInternalServerError, "failed to start transaction")
		return
	}
	defer tx.Rollback(r.Context())
	before, err := queryBusinessRowJSON(r.Context(), tx.QueryRow(r.Context(), `SELECT to_jsonb(c) FROM business_client c WHERE id = $1 AND business_id = $2 FOR UPDATE`, clientID, businessID))
	if errors.Is(err, pgx.ErrNoRows) {
		writeError(w, http.StatusNotFound, "client not found")
		return
	}
	if err != nil {
		writeError(w, http.StatusInternalServerError, "failed to load client")
		return
	}
	after, err := queryBusinessRowJSON(r.Context(), tx.QueryRow(r.Context(), `
		UPDATE business_client SET
			canonical_name = COALESCE($3, canonical_name),
			status = COALESCE($4, status),
			manager_user_id = CASE WHEN $5::text IS NULL THEN manager_user_id ELSE NULLIF($5, '')::uuid END,
			primary_payment_channel = COALESCE($6, primary_payment_channel),
			notes = CASE WHEN $7::text IS NULL THEN notes ELSE NULLIF($7, '') END,
			archived_at = CASE WHEN COALESCE($4, status) = 'lost' THEN COALESCE(archived_at, now()) ELSE NULL END,
			updated_at = now()
		WHERE id = $1 AND business_id = $2
		RETURNING to_jsonb(business_client)
	`, clientID, businessID, request.CanonicalName, request.Status, nullableString(request.ManagerUserID), request.PrimaryPaymentChannel, nullableString(request.Notes)))
	if isUniqueViolation(err) {
		writeError(w, http.StatusConflict, "client name already exists")
		return
	}
	if err != nil {
		writeError(w, http.StatusBadRequest, "failed to update client")
		return
	}
	if err := h.insertBusinessAudit(r.Context(), tx, businessID, userID, "client.updated", "business_client", clientID, "", before, after); err != nil {
		writeError(w, http.StatusInternalServerError, "failed to audit client")
		return
	}
	if err := tx.Commit(r.Context()); err != nil {
		writeError(w, http.StatusInternalServerError, "failed to update client")
		return
	}
	writeJSON(w, http.StatusOK, after)
}

type createBusinessPayerRequest struct {
	WorkspaceID      *string `json:"workspace_id"`
	ElbaOrgID        *string `json:"elba_org_id"`
	ElbaContractorID *string `json:"elba_contractor_id"`
	Name             string  `json:"name"`
	INN              *string `json:"inn"`
	KPP              *string `json:"kpp"`
	Status           string  `json:"status"`
	PaymentChannel   string  `json:"payment_channel"`
	Notes            *string `json:"notes"`
}

func (h *Handler) CreateBusinessPayer(w http.ResponseWriter, r *http.Request) {
	businessID, userID, ok := businessRequestIDs(w, r)
	if !ok {
		return
	}
	clientID := chi.URLParam(r, "clientId")
	if _, valid := parseUUIDOrBadRequest(w, clientID, "client_id"); !valid {
		return
	}
	var request createBusinessPayerRequest
	if !decodeBusinessJSON(w, r, &request) {
		return
	}
	request.Name = strings.TrimSpace(request.Name)
	if request.Name == "" {
		writeError(w, http.StatusBadRequest, "name is required")
		return
	}
	if request.Status == "" {
		request.Status = "active"
	}
	if request.PaymentChannel == "" {
		request.PaymentChannel = "bank"
	}
	if request.WorkspaceID != nil && *request.WorkspaceID != "" {
		if _, valid := parseUUIDOrBadRequest(w, *request.WorkspaceID, "workspace_id"); !valid {
			return
		}
	}
	tx, err := h.TxStarter.Begin(r.Context())
	if err != nil {
		writeError(w, http.StatusInternalServerError, "failed to start transaction")
		return
	}
	defer tx.Rollback(r.Context())
	if !businessEntityExists(r.Context(), tx, "business_client", clientID, businessID) {
		writeError(w, http.StatusNotFound, "client not found")
		return
	}
	if request.WorkspaceID != nil && *request.WorkspaceID != "" && !businessWorkspaceExists(r.Context(), tx, businessID, *request.WorkspaceID) {
		writeError(w, http.StatusBadRequest, "workspace is outside business registry")
		return
	}
	raw, err := queryBusinessRowJSON(r.Context(), tx.QueryRow(r.Context(), `
		INSERT INTO business_client_payer (
			business_id, client_id, workspace_id, elba_org_id, elba_contractor_id,
			name, inn, kpp, status, payment_channel, notes
		) VALUES ($1, $2, NULLIF($3, '')::uuid, $4, $5, $6, $7, $8, $9, $10, $11)
		RETURNING to_jsonb(business_client_payer)
	`, businessID, clientID, stringValue(request.WorkspaceID), request.ElbaOrgID, request.ElbaContractorID,
		request.Name, request.INN, request.KPP, request.Status, request.PaymentChannel, request.Notes))
	if isUniqueViolation(err) {
		writeError(w, http.StatusConflict, "payer already exists")
		return
	}
	if err != nil {
		writeError(w, http.StatusBadRequest, "failed to create payer")
		return
	}
	var created struct {
		ID string `json:"id"`
	}
	_ = json.Unmarshal(raw, &created)
	if err := h.insertBusinessAudit(r.Context(), tx, businessID, userID, "payer.created", "business_client_payer", created.ID, "", nil, raw); err != nil {
		writeError(w, http.StatusInternalServerError, "failed to audit payer")
		return
	}
	if err := tx.Commit(r.Context()); err != nil {
		writeError(w, http.StatusInternalServerError, "failed to create payer")
		return
	}
	writeJSON(w, http.StatusCreated, raw)
}

type updateBusinessPayerRequest struct {
	Name             *string `json:"name"`
	INN              *string `json:"inn"`
	KPP              *string `json:"kpp"`
	Status           *string `json:"status"`
	PaymentChannel   *string `json:"payment_channel"`
	Notes            *string `json:"notes"`
	ElbaOrgID        *string `json:"elba_org_id"`
	ElbaContractorID *string `json:"elba_contractor_id"`
	// When true and the payer ends up with a contractor, every project mapped
	// to the payer's client gets that contractor in client_billing_config —
	// existing configs are updated, missing ones are created disabled so the
	// link is ready the moment billing is switched on.
	ApplyToProjects bool `json:"apply_contractor_to_projects"`
}

func (h *Handler) UpdateBusinessPayer(w http.ResponseWriter, r *http.Request) {
	businessID, userID, ok := businessRequestIDs(w, r)
	if !ok {
		return
	}
	payerID := chi.URLParam(r, "payerId")
	if _, valid := parseUUIDOrBadRequest(w, payerID, "payer_id"); !valid {
		return
	}
	var request updateBusinessPayerRequest
	if !decodeBusinessJSON(w, r, &request) {
		return
	}
	if request.Name != nil {
		trimmed := strings.TrimSpace(*request.Name)
		if trimmed == "" {
			writeError(w, http.StatusBadRequest, "name cannot be empty")
			return
		}
		request.Name = &trimmed
	}
	if request.Status != nil && !containsBusinessString([]string{"active", "inactive", "needs_review"}, *request.Status) {
		writeError(w, http.StatusBadRequest, "invalid status")
		return
	}
	if request.PaymentChannel != nil && !containsBusinessString([]string{"bank", "personal_card", "cash", "other"}, *request.PaymentChannel) {
		writeError(w, http.StatusBadRequest, "invalid payment_channel")
		return
	}
	tx, err := h.TxStarter.Begin(r.Context())
	if err != nil {
		writeError(w, http.StatusInternalServerError, "failed to start transaction")
		return
	}
	defer tx.Rollback(r.Context())
	before, err := queryBusinessRowJSON(r.Context(), tx.QueryRow(r.Context(), `SELECT to_jsonb(p) FROM business_client_payer p WHERE id = $1 AND business_id = $2 FOR UPDATE`, payerID, businessID))
	if errors.Is(err, pgx.ErrNoRows) {
		writeError(w, http.StatusNotFound, "payer not found")
		return
	}
	if err != nil {
		writeError(w, http.StatusInternalServerError, "failed to load payer")
		return
	}
	after, err := queryBusinessRowJSON(r.Context(), tx.QueryRow(r.Context(), `
		UPDATE business_client_payer SET
			name = COALESCE($3, name),
			inn = CASE WHEN $4::text IS NULL THEN inn ELSE NULLIF($4, '') END,
			kpp = CASE WHEN $5::text IS NULL THEN kpp ELSE NULLIF($5, '') END,
			status = COALESCE($6, status),
			payment_channel = COALESCE($7, payment_channel),
			notes = CASE WHEN $8::text IS NULL THEN notes ELSE NULLIF($8, '') END,
			elba_org_id = CASE WHEN $9::text IS NULL THEN elba_org_id ELSE NULLIF($9, '') END,
			elba_contractor_id = CASE WHEN $10::text IS NULL THEN elba_contractor_id ELSE NULLIF($10, '') END,
			updated_at = now()
		WHERE id = $1 AND business_id = $2
		RETURNING to_jsonb(business_client_payer)
	`, payerID, businessID, request.Name, nullableString(request.INN), nullableString(request.KPP),
		request.Status, request.PaymentChannel, nullableString(request.Notes),
		nullableString(request.ElbaOrgID), nullableString(request.ElbaContractorID)))
	if err != nil {
		writeError(w, http.StatusBadRequest, "failed to update payer")
		return
	}
	if err := h.insertBusinessAudit(r.Context(), tx, businessID, userID, "payer.updated", "business_client_payer", payerID, "", before, after); err != nil {
		writeError(w, http.StatusInternalServerError, "failed to audit payer")
		return
	}
	var billingUpdated, billingCreated int64
	if request.ApplyToProjects {
		var payerRow struct {
			ClientID         string  `json:"client_id"`
			ElbaContractorID *string `json:"elba_contractor_id"`
		}
		_ = json.Unmarshal(after, &payerRow)
		if payerRow.ElbaContractorID != nil && *payerRow.ElbaContractorID != "" && payerRow.ClientID != "" {
			updateTag, err := tx.Exec(r.Context(), `
				UPDATE client_billing_config cb SET elba_contractor_id = $3, updated_at = now()
				FROM business_client_project bcp
				WHERE bcp.business_id = $1 AND bcp.client_id = $2 AND cb.project_id = bcp.project_id
				  AND cb.elba_contractor_id IS DISTINCT FROM $3
			`, businessID, payerRow.ClientID, *payerRow.ElbaContractorID)
			if err != nil {
				writeError(w, http.StatusInternalServerError, "failed to link contractor to projects")
				return
			}
			billingUpdated = updateTag.RowsAffected()
			insertTag, err := tx.Exec(r.Context(), `
				INSERT INTO client_billing_config (project_id, enabled, elba_contractor_id)
				SELECT bcp.project_id, false, $3
				FROM business_client_project bcp
				WHERE bcp.business_id = $1 AND bcp.client_id = $2
				  AND NOT EXISTS (SELECT 1 FROM client_billing_config cb WHERE cb.project_id = bcp.project_id)
			`, businessID, payerRow.ClientID, *payerRow.ElbaContractorID)
			if err != nil {
				writeError(w, http.StatusInternalServerError, "failed to link contractor to projects")
				return
			}
			billingCreated = insertTag.RowsAffected()
			applied, _ := json.Marshal(map[string]any{
				"elba_contractor_id": *payerRow.ElbaContractorID,
				"billing_updated":    billingUpdated,
				"billing_created":    billingCreated,
			})
			if err := h.insertBusinessAudit(r.Context(), tx, businessID, userID, "payer.contractor_applied", "business_client_payer", payerID, "", nil, applied); err != nil {
				writeError(w, http.StatusInternalServerError, "failed to audit payer")
				return
			}
		}
	}
	if err := tx.Commit(r.Context()); err != nil {
		writeError(w, http.StatusInternalServerError, "failed to update payer")
		return
	}
	writeJSON(w, http.StatusOK, map[string]any{
		"payer":           json.RawMessage(after),
		"billing_updated": billingUpdated,
		"billing_created": billingCreated,
	})
}

type mapBusinessProjectRequest struct {
	WorkspaceID   string  `json:"workspace_id"`
	ProjectID     string  `json:"project_id"`
	// ServiceType is optional and ignored when the project has project_type set.
	// Kept for older clients; the canonical source is project.project_type.
	ServiceType   string  `json:"service_type"`
	Billable      *bool   `json:"billable"`
	PortalVisible *bool   `json:"portal_visible"`
	Notes         *string `json:"notes"`
}

// businessServiceTypeFromProject maps project.project_type onto
// business_client_project.service_type. Transit projects are internal for
// billing economics; unclassified projects default to development.
func businessServiceTypeFromProject(projectType string) string {
	switch strings.TrimSpace(projectType) {
	case "support", "seo", "development":
		return projectType
	case "transit":
		return "internal"
	case "content", "internal":
		return projectType
	default:
		return "development"
	}
}

func (h *Handler) MapBusinessClientProject(w http.ResponseWriter, r *http.Request) {
	businessID, userID, ok := businessRequestIDs(w, r)
	if !ok {
		return
	}
	clientID := chi.URLParam(r, "clientId")
	if _, valid := parseUUIDOrBadRequest(w, clientID, "client_id"); !valid {
		return
	}
	var request mapBusinessProjectRequest
	if !decodeBusinessJSON(w, r, &request) {
		return
	}
	if _, valid := parseUUIDOrBadRequest(w, request.WorkspaceID, "workspace_id"); !valid {
		return
	}
	if _, valid := parseUUIDOrBadRequest(w, request.ProjectID, "project_id"); !valid {
		return
	}
	billable := true
	if request.Billable != nil {
		billable = *request.Billable
	}
	portalVisible := false
	if request.PortalVisible != nil {
		portalVisible = *request.PortalVisible
	}
	tx, err := h.TxStarter.Begin(r.Context())
	if err != nil {
		writeError(w, http.StatusInternalServerError, "failed to start transaction")
		return
	}
	defer tx.Rollback(r.Context())
	if !businessEntityExists(r.Context(), tx, "business_client", clientID, businessID) {
		writeError(w, http.StatusNotFound, "client not found")
		return
	}
	if !nativeBusinessProjectExists(r.Context(), tx, businessID, request.WorkspaceID, request.ProjectID) {
		writeError(w, http.StatusBadRequest, "project is outside business workspace registry")
		return
	}
	var projectType pgtype.Text
	if err := tx.QueryRow(r.Context(), `
		SELECT project_type FROM project WHERE id = $1 AND workspace_id = $2
	`, request.ProjectID, request.WorkspaceID).Scan(&projectType); err != nil {
		writeError(w, http.StatusBadRequest, "project not found")
		return
	}
	serviceType := businessServiceTypeFromProject("")
	if projectType.Valid && projectType.String != "" {
		serviceType = businessServiceTypeFromProject(projectType.String)
	} else if containsBusinessString([]string{"development", "support", "seo", "content", "internal"}, request.ServiceType) {
		serviceType = request.ServiceType
	}
	var before json.RawMessage
	before, _ = queryBusinessRowJSON(r.Context(), tx.QueryRow(r.Context(), `SELECT to_jsonb(x) FROM business_client_project x WHERE project_id = $1 FOR UPDATE`, request.ProjectID))
	raw, err := queryBusinessRowJSON(r.Context(), tx.QueryRow(r.Context(), `
		INSERT INTO business_client_project (business_id, client_id, workspace_id, project_id, service_type, billable, portal_visible, notes)
		VALUES ($1, $2, $3, $4, $5, $6, $7, $8)
		ON CONFLICT (project_id) DO UPDATE SET
			business_id = EXCLUDED.business_id, client_id = EXCLUDED.client_id,
			workspace_id = EXCLUDED.workspace_id, service_type = EXCLUDED.service_type,
			billable = EXCLUDED.billable, portal_visible = EXCLUDED.portal_visible,
			notes = EXCLUDED.notes, updated_at = now()
		RETURNING to_jsonb(business_client_project)
	`, businessID, clientID, request.WorkspaceID, request.ProjectID, serviceType, billable, portalVisible, request.Notes))
	if err != nil {
		writeError(w, http.StatusBadRequest, "failed to map project")
		return
	}
	var mapped struct {
		ID string `json:"id"`
	}
	_ = json.Unmarshal(raw, &mapped)
	if err := h.insertBusinessAudit(r.Context(), tx, businessID, userID, "client_project.mapped", "business_client_project", mapped.ID, "", before, raw); err != nil {
		writeError(w, http.StatusInternalServerError, "failed to audit project mapping")
		return
	}
	if err := tx.Commit(r.Context()); err != nil {
		writeError(w, http.StatusInternalServerError, "failed to map project")
		return
	}
	writeJSON(w, http.StatusOK, raw)
}

type classifyBusinessCounterpartyRequest struct {
	Classification string  `json:"classification"`
	ClientID       *string `json:"client_id"`
	WorkerID       *string `json:"worker_id"`
	Confidence     string  `json:"confidence"`
	Reason         string  `json:"reason"`
}

func (h *Handler) ClassifyBusinessCounterparty(w http.ResponseWriter, r *http.Request) {
	businessID, userID, ok := businessRequestIDs(w, r)
	if !ok {
		return
	}
	classificationID := chi.URLParam(r, "classificationId")
	if _, valid := parseUUIDOrBadRequest(w, classificationID, "classification_id"); !valid {
		return
	}
	var request classifyBusinessCounterpartyRequest
	if !decodeBusinessJSON(w, r, &request) {
		return
	}
	if request.Confidence == "" {
		request.Confidence = "confirmed"
	}
	if request.Classification == "client_payer" && (request.ClientID == nil || *request.ClientID == "") {
		writeError(w, http.StatusBadRequest, "client_id is required for client_payer")
		return
	}
	if request.Classification == "worker_payee" && (request.WorkerID == nil || *request.WorkerID == "") {
		writeError(w, http.StatusBadRequest, "worker_id is required for worker_payee")
		return
	}
	tx, err := h.TxStarter.Begin(r.Context())
	if err != nil {
		writeError(w, http.StatusInternalServerError, "failed to start transaction")
		return
	}
	defer tx.Rollback(r.Context())
	before, err := queryBusinessRowJSON(r.Context(), tx.QueryRow(r.Context(), `SELECT to_jsonb(x) FROM business_counterparty_classification x WHERE id = $1 AND business_id = $2 FOR UPDATE`, classificationID, businessID))
	if errors.Is(err, pgx.ErrNoRows) {
		writeError(w, http.StatusNotFound, "counterparty not found")
		return
	}
	if err != nil {
		writeError(w, http.StatusInternalServerError, "failed to load counterparty")
		return
	}
	if request.ClientID != nil && *request.ClientID != "" {
		if _, valid := parseUUIDOrBadRequest(w, *request.ClientID, "client_id"); !valid || !businessEntityExists(r.Context(), tx, "business_client", *request.ClientID, businessID) {
			if valid {
				writeError(w, http.StatusBadRequest, "client is outside business")
			}
			return
		}
	}
	if request.WorkerID != nil && *request.WorkerID != "" {
		if _, valid := parseUUIDOrBadRequest(w, *request.WorkerID, "worker_id"); !valid || !businessEntityExists(r.Context(), tx, "business_worker", *request.WorkerID, businessID) {
			if valid {
				writeError(w, http.StatusBadRequest, "worker is outside business")
			}
			return
		}
	}
	after, err := queryBusinessRowJSON(r.Context(), tx.QueryRow(r.Context(), `
		UPDATE business_counterparty_classification SET classification = $3,
			client_id = NULLIF($4, '')::uuid, worker_id = NULLIF($5, '')::uuid,
			confidence = $6, reason = NULLIF($7, ''), classified_by = $8,
			classified_at = now(), updated_at = now()
		WHERE id = $1 AND business_id = $2 RETURNING to_jsonb(business_counterparty_classification)
	`, classificationID, businessID, request.Classification, stringValue(request.ClientID), stringValue(request.WorkerID), request.Confidence, request.Reason, userID))
	if err != nil {
		writeError(w, http.StatusBadRequest, "failed to classify counterparty")
		return
	}
	if err := h.insertBusinessAudit(r.Context(), tx, businessID, userID, "counterparty.classified", "business_counterparty_classification", classificationID, request.Reason, before, after); err != nil {
		writeError(w, http.StatusInternalServerError, "failed to audit classification")
		return
	}
	if err := tx.Commit(r.Context()); err != nil {
		writeError(w, http.StatusInternalServerError, "failed to classify counterparty")
		return
	}
	writeJSON(w, http.StatusOK, after)
}

type createBusinessAgreementRequest struct {
	ClientID       string          `json:"client_id"`
	ProjectID      *string         `json:"project_id"`
	ServiceType    string          `json:"service_type"`
	AgreementKey   string          `json:"agreement_key"`
	Version        int32           `json:"version"`
	Name           string          `json:"name"`
	Model          string          `json:"model"`
	AmountRUB      *string         `json:"amount_rub"`
	HourlyRateRUB  *string         `json:"hourly_rate_rub"`
	CapRUB         *string         `json:"cap_rub"`
	InvoiceDay     *int32          `json:"invoice_day"`
	DueDays        *int32          `json:"due_days"`
	PeriodMonths   *int32          `json:"period_months"`
	PaymentChannel string          `json:"payment_channel"`
	EffectiveFrom  string          `json:"effective_from"`
	EffectiveTo    *string         `json:"effective_to"`
	Status         string          `json:"status"`
	IsEstimate     bool            `json:"is_estimate"`
	NeedsReview    bool            `json:"needs_review"`
	Terms          json.RawMessage `json:"terms"`
}

func (h *Handler) CreateBusinessAgreement(w http.ResponseWriter, r *http.Request) {
	businessID, userID, ok := businessRequestIDs(w, r)
	if !ok {
		return
	}
	var request createBusinessAgreementRequest
	if !decodeBusinessJSON(w, r, &request) {
		return
	}
	if _, valid := parseUUIDOrBadRequest(w, request.ClientID, "client_id"); !valid {
		return
	}
	if request.ProjectID != nil && *request.ProjectID != "" {
		if _, valid := parseUUIDOrBadRequest(w, *request.ProjectID, "project_id"); !valid {
			return
		}
	}
	if _, err := time.Parse("2006-01-02", request.EffectiveFrom); err != nil {
		writeError(w, http.StatusBadRequest, "effective_from must use YYYY-MM-DD")
		return
	}
	if request.EffectiveTo != nil && *request.EffectiveTo != "" {
		if _, err := time.Parse("2006-01-02", *request.EffectiveTo); err != nil {
			writeError(w, http.StatusBadRequest, "effective_to must use YYYY-MM-DD")
			return
		}
	}
	if request.Version == 0 {
		request.Version = 1
	}
	if request.DueDays == nil {
		value := int32(7)
		request.DueDays = &value
	}
	if request.PeriodMonths == nil {
		value := int32(1)
		request.PeriodMonths = &value
	}
	if request.PaymentChannel == "" {
		request.PaymentChannel = "bank"
	}
	if request.Status == "" {
		request.Status = "draft"
	}
	if len(request.Terms) == 0 {
		request.Terms = json.RawMessage("{}")
	}
	tx, err := h.TxStarter.Begin(r.Context())
	if err != nil {
		writeError(w, http.StatusInternalServerError, "failed to start transaction")
		return
	}
	defer tx.Rollback(r.Context())
	if !businessEntityExists(r.Context(), tx, "business_client", request.ClientID, businessID) {
		writeError(w, http.StatusBadRequest, "client is outside business")
		return
	}
	if request.ProjectID != nil && *request.ProjectID != "" {
		var mapped bool
		_ = tx.QueryRow(r.Context(), `SELECT EXISTS(SELECT 1 FROM business_client_project WHERE business_id = $1 AND client_id = $2 AND project_id = $3)`, businessID, request.ClientID, *request.ProjectID).Scan(&mapped)
		if !mapped {
			writeError(w, http.StatusBadRequest, "project is not mapped to client")
			return
		}
	}
	raw, err := queryBusinessRowJSON(r.Context(), tx.QueryRow(r.Context(), `
		INSERT INTO business_agreement (
			business_id, client_id, project_id, service_type, agreement_key, version, name, model,
			amount_rub, hourly_rate_rub, cap_rub, invoice_day, due_days, period_months,
			payment_channel, effective_from, effective_to, status, is_estimate, needs_review, terms, created_by
		) VALUES ($1, $2, NULLIF($3, '')::uuid, $4, $5, $6, $7, $8,
			NULLIF($9, '')::numeric, NULLIF($10, '')::numeric, NULLIF($11, '')::numeric,
			$12, $13, $14, $15, $16::date, NULLIF($17, '')::date, $18, $19, $20, $21, $22)
		RETURNING to_jsonb(business_agreement)
	`, businessID, request.ClientID, stringValue(request.ProjectID), request.ServiceType, strings.TrimSpace(request.AgreementKey), request.Version,
		strings.TrimSpace(request.Name), request.Model, stringValue(request.AmountRUB), stringValue(request.HourlyRateRUB), stringValue(request.CapRUB),
		request.InvoiceDay, request.DueDays, request.PeriodMonths, request.PaymentChannel, request.EffectiveFrom, stringValue(request.EffectiveTo),
		request.Status, request.IsEstimate, request.NeedsReview, []byte(request.Terms), userID))
	if isUniqueViolation(err) {
		writeError(w, http.StatusConflict, "agreement version already exists")
		return
	}
	if err != nil {
		writeError(w, http.StatusBadRequest, "failed to create agreement")
		return
	}
	var created struct {
		ID string `json:"id"`
	}
	_ = json.Unmarshal(raw, &created)
	if err := h.insertBusinessAudit(r.Context(), tx, businessID, userID, "agreement.created", "business_agreement", created.ID, "", nil, raw); err != nil {
		writeError(w, http.StatusInternalServerError, "failed to audit agreement")
		return
	}
	if err := tx.Commit(r.Context()); err != nil {
		writeError(w, http.StatusInternalServerError, "failed to create agreement")
		return
	}
	writeJSON(w, http.StatusCreated, raw)
}

type generateBusinessReceivablesRequest struct {
	FromMonth string `json:"from_month"`
	Months    int32  `json:"months"`
}

func (h *Handler) GenerateBusinessReceivables(w http.ResponseWriter, r *http.Request) {
	businessID, userID, ok := businessRequestIDs(w, r)
	if !ok {
		return
	}
	var request generateBusinessReceivablesRequest
	if !decodeBusinessJSON(w, r, &request) {
		return
	}
	month, startString, _, valid := parseBusinessMonth(w, request.FromMonth)
	if !valid {
		return
	}
	if request.Months == 0 {
		request.Months = 3
	}
	if request.Months < 1 || request.Months > 24 {
		writeError(w, http.StatusBadRequest, "months must be between 1 and 24")
		return
	}
	start, _ := time.Parse("2006-01-02", startString)
	tx, err := h.TxStarter.Begin(r.Context())
	if err != nil {
		writeError(w, http.StatusInternalServerError, "failed to start transaction")
		return
	}
	defer tx.Rollback(r.Context())
	rows, err := tx.Query(r.Context(), `
		SELECT id::text, client_id::text, COALESCE(project_id::text, ''), model,
		       COALESCE(amount_rub::text, ''), COALESCE(cap_rub::text, ''),
		       invoice_day, due_days, period_months, effective_from, effective_to,
		       needs_review
		FROM business_agreement
		WHERE business_id = $1 AND status = 'active' AND period_months > 0 AND model <> 'project'
		ORDER BY agreement_key, version DESC
	`, businessID)
	if err != nil {
		writeError(w, http.StatusInternalServerError, "failed to list agreements")
		return
	}
	type agreementRow struct {
		ID, ClientID, ProjectID, Model, Amount, Cap string
		InvoiceDay                                  *int32
		DueDays, PeriodMonths                       int32
		EffectiveFrom                               time.Time
		EffectiveTo                                 *time.Time
		NeedsReview                                 bool
	}
	agreements := make([]agreementRow, 0)
	for rows.Next() {
		var row agreementRow
		if err := rows.Scan(&row.ID, &row.ClientID, &row.ProjectID, &row.Model, &row.Amount, &row.Cap,
			&row.InvoiceDay, &row.DueDays, &row.PeriodMonths, &row.EffectiveFrom, &row.EffectiveTo, &row.NeedsReview); err != nil {
			rows.Close()
			writeError(w, http.StatusInternalServerError, "failed to read agreements")
			return
		}
		agreements = append(agreements, row)
	}
	rows.Close()
	if err := rows.Err(); err != nil {
		writeError(w, http.StatusInternalServerError, "failed to read agreements")
		return
	}
	inserted := int64(0)
	for offset := int32(0); offset < request.Months; offset++ {
		periodStart := start.AddDate(0, int(offset), 0)
		periodEndExclusive := periodStart.AddDate(0, 1, 0)
		periodEnd := periodEndExclusive.AddDate(0, 0, -1)
		periodKey := periodStart.Format("2006-01")
		for _, agreement := range agreements {
			if periodEnd.Before(dateOnly(agreement.EffectiveFrom)) || (agreement.EffectiveTo != nil && periodStart.After(dateOnly(*agreement.EffectiveTo))) {
				continue
			}
			amount := agreement.Amount
			needsReview := agreement.NeedsReview
			source := "agreement"
			billingPeriodID := ""
			switch agreement.Model {
			case "cap":
				amount = agreement.Cap
			case "time_material":
				amount = ""
				if agreement.ProjectID != "" {
					var total string
					err := tx.QueryRow(r.Context(), `
						SELECT id::text, total_rub::text FROM client_billing_period
						WHERE project_id = $1 AND starts_on < $3::date AND ends_on > $2::date
						ORDER BY starts_on DESC LIMIT 1
					`, agreement.ProjectID, periodStart.Format("2006-01-02"), periodEndExclusive.Format("2006-01-02")).Scan(&billingPeriodID, &total)
					if err == nil {
						amount = total
						source = "billing_period"
						needsReview = false
					}
				}
			}
			if amount == "" {
				amount = "0"
				needsReview = true
			}
			invoiceOn := ""
			dueOn := ""
			if agreement.InvoiceDay != nil {
				day := int(*agreement.InvoiceDay)
				lastDay := periodEnd.Day()
				if day > lastDay {
					day = lastDay
				}
				invoiceDate := time.Date(periodStart.Year(), periodStart.Month(), day, 0, 0, 0, 0, time.UTC)
				invoiceOn = invoiceDate.Format("2006-01-02")
				dueOn = invoiceDate.AddDate(0, 0, int(agreement.DueDays)).Format("2006-01-02")
			}
			key := agreement.ID + ":" + periodKey
			command, err := tx.Exec(r.Context(), `
				INSERT INTO business_receivable (
					business_id, agreement_id, client_id, project_id, period_key,
					period_start, period_end, planned_amount_rub, source, invoice_on, due_on,
					client_billing_period_id, needs_review, idempotency_key
				) VALUES ($1, $2, $3, NULLIF($4, '')::uuid, $5, $6::date, $7::date,
					$8::numeric, $9, NULLIF($10, '')::date, NULLIF($11, '')::date,
					NULLIF($12, '')::uuid, $13, $14)
				ON CONFLICT (agreement_id, period_key) DO NOTHING
			`, businessID, agreement.ID, agreement.ClientID, agreement.ProjectID, periodKey,
				periodStart.Format("2006-01-02"), periodEnd.Format("2006-01-02"), amount, source,
				invoiceOn, dueOn, billingPeriodID, needsReview, key)
			if err != nil {
				writeError(w, http.StatusBadRequest, "failed to generate receivables")
				return
			}
			inserted += command.RowsAffected()
		}
	}
	_, err = tx.Exec(r.Context(), `
		UPDATE business_receivable SET status = 'overdue', updated_at = now()
		WHERE business_id = $1 AND status IN ('expected','invoiced','partially_paid')
		  AND due_on < (now() AT TIME ZONE 'Asia/Yekaterinburg')::date
		  AND paid_amount_rub < planned_amount_rub
	`, businessID)
	if err != nil {
		writeError(w, http.StatusInternalServerError, "failed to update overdue receivables")
		return
	}
	after, _ := json.Marshal(map[string]any{"from_month": month, "months": request.Months, "inserted": inserted})
	if err := h.insertBusinessAudit(r.Context(), tx, businessID, userID, "receivables.generated", "business_receivable", "", "", nil, after); err != nil {
		writeError(w, http.StatusInternalServerError, "failed to audit receivables")
		return
	}
	if err := tx.Commit(r.Context()); err != nil {
		writeError(w, http.StatusInternalServerError, "failed to generate receivables")
		return
	}
	writeJSON(w, http.StatusOK, map[string]any{"from_month": month, "months": request.Months, "inserted": inserted})
}

func businessEntityExists(ctx context.Context, tx pgx.Tx, table, id, businessID string) bool {
	allowed := map[string]bool{
		"business_client": true, "business_worker": true, "business_agreement": true,
		"business_receivable": true, "business_bank_transaction": true,
		"business_task_economics": true, "business_accrual": true,
		"business_quality_case": true, "business_payout_batch": true,
	}
	if !allowed[table] {
		return false
	}
	var exists bool
	query := fmt.Sprintf("SELECT EXISTS(SELECT 1 FROM %s WHERE id = $1 AND business_id = $2)", table)
	if err := tx.QueryRow(ctx, query, id, businessID).Scan(&exists); err != nil {
		return false
	}
	return exists
}

func businessWorkspaceExists(ctx context.Context, tx pgx.Tx, businessID, workspaceID string) bool {
	var exists bool
	err := tx.QueryRow(ctx, `SELECT EXISTS(SELECT 1 FROM business_workspace WHERE business_id = $1 AND workspace_id = $2 AND kind <> 'archive')`, businessID, workspaceID).Scan(&exists)
	return err == nil && exists
}

func nativeBusinessProjectExists(ctx context.Context, tx pgx.Tx, businessID, workspaceID, projectID string) bool {
	var exists bool
	err := tx.QueryRow(ctx, `
		SELECT EXISTS(
			SELECT 1 FROM business_workspace bw
			JOIN project p ON p.workspace_id = bw.workspace_id
			WHERE bw.business_id = $1 AND bw.workspace_id = $2 AND bw.kind <> 'archive'
			  AND p.id = $3 AND p.workspace_id = $2
		)
	`, businessID, workspaceID, projectID).Scan(&exists)
	return err == nil && exists
}

func nativeBusinessIssueExists(ctx context.Context, tx pgx.Tx, businessID, workspaceID, projectID, issueID string) bool {
	var exists bool
	err := tx.QueryRow(ctx, `
		SELECT EXISTS(
			SELECT 1 FROM business_workspace bw
			JOIN issue i ON i.workspace_id = bw.workspace_id
			WHERE bw.business_id = $1 AND bw.workspace_id = $2 AND bw.kind <> 'archive'
			  AND i.id = $4 AND i.workspace_id = $2 AND i.project_id = $3
		)
	`, businessID, workspaceID, projectID, issueID).Scan(&exists)
	return err == nil && exists
}

func stringValue(value *string) string {
	if value == nil {
		return ""
	}
	return *value
}

func nullableString(value *string) any {
	if value == nil {
		return nil
	}
	return *value
}

func containsBusinessString(values []string, value string) bool {
	for _, candidate := range values {
		if candidate == value {
			return true
		}
	}
	return false
}

func dateOnly(value time.Time) time.Time {
	return time.Date(value.Year(), value.Month(), value.Day(), 0, 0, 0, 0, time.UTC)
}
