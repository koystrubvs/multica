package handler

import (
	"encoding/json"
	"errors"
	"net/http"
	"strings"
	"time"

	"github.com/go-chi/chi/v5"
	"github.com/jackc/pgx/v5"
)

type createBusinessWorkerRequest struct {
	UserID              *string `json:"user_id"`
	Name                string  `json:"name"`
	EngagementFormat    string  `json:"engagement_format"`
	RecipientExternalID *string `json:"recipient_external_id"`
	RecipientMask       *string `json:"recipient_mask"`
	Notes               *string `json:"notes"`
}

func (h *Handler) CreateBusinessWorker(w http.ResponseWriter, r *http.Request) {
	businessID, userID, ok := businessRequestIDs(w, r)
	if !ok {
		return
	}
	var request createBusinessWorkerRequest
	if !decodeBusinessJSON(w, r, &request) {
		return
	}
	request.Name = strings.TrimSpace(request.Name)
	if request.Name == "" || !containsBusinessString([]string{"employee", "self_employed", "individual_contractor", "vendor_person"}, request.EngagementFormat) {
		writeError(w, http.StatusBadRequest, "name or engagement_format is invalid")
		return
	}
	if request.UserID != nil && *request.UserID != "" {
		if _, valid := parseUUIDOrBadRequest(w, *request.UserID, "user_id"); !valid {
			return
		}
	}
	tx, err := h.TxStarter.Begin(r.Context())
	if err != nil {
		writeError(w, 500, "failed to create worker")
		return
	}
	defer tx.Rollback(r.Context())
	if request.UserID != nil && *request.UserID != "" {
		var member bool
		_ = tx.QueryRow(r.Context(), `
			SELECT EXISTS(SELECT 1 FROM business_workspace bw JOIN member wm ON wm.workspace_id=bw.workspace_id WHERE bw.business_id=$1 AND wm.user_id=$2)
		`, businessID, *request.UserID).Scan(&member)
		if !member {
			writeError(w, 400, "worker user is not a member of a business workspace")
			return
		}
	}
	var id string
	err = tx.QueryRow(r.Context(), `
		INSERT INTO business_worker (business_id,user_id,name,engagement_format,recipient_external_id,recipient_mask,notes)
		VALUES ($1,NULLIF($2,'')::uuid,$3,$4,NULLIF($5,''),NULLIF($6,''),NULLIF($7,''))
		ON CONFLICT (business_id,user_id) WHERE user_id IS NOT NULL
		DO UPDATE SET name=EXCLUDED.name,engagement_format=EXCLUDED.engagement_format,
		  recipient_external_id=EXCLUDED.recipient_external_id,recipient_mask=EXCLUDED.recipient_mask,
		  notes=EXCLUDED.notes,status='active',updated_at=now()
		RETURNING id::text
	`, businessID, stringValue(request.UserID), request.Name, request.EngagementFormat,
		stringValue(request.RecipientExternalID), stringValue(request.RecipientMask), stringValue(request.Notes)).Scan(&id)
	if err != nil {
		writeError(w, 500, "failed to create worker")
		return
	}
	if err := h.insertBusinessAudit(r.Context(), tx, businessID, userID, "worker.upsert", "business_worker", id, "worker registry", nil, nil); err != nil {
		writeError(w, 500, "failed to create worker")
		return
	}
	if err := tx.Commit(r.Context()); err != nil {
		writeError(w, 500, "failed to create worker")
		return
	}
	writeJSON(w, http.StatusCreated, map[string]string{"id": id})
}

type createBusinessClientRequestInput struct {
	ClientID         string  `json:"client_id"`
	WorkspaceID      string  `json:"workspace_id"`
	ProjectID        *string `json:"project_id"`
	Channel          string  `json:"channel"`
	ExternalRef      *string `json:"external_ref"`
	IdempotencyKey   string  `json:"idempotency_key"`
	Summary          string  `json:"summary"`
	ReceivedAt       string  `json:"received_at"`
	TriageDueAt      *string `json:"triage_due_at"`
	LinkedIssueID    *string `json:"linked_issue_id"`
	PMWorkerID       *string `json:"pm_worker_id"`
	Status           string  `json:"status"`
	EscalationReason *string `json:"escalation_reason"`
}

func (h *Handler) CreateBusinessClientRequest(w http.ResponseWriter, r *http.Request) {
	businessID, userID, ok := businessRequestIDs(w, r)
	if !ok {
		return
	}
	var request createBusinessClientRequestInput
	if !decodeBusinessJSON(w, r, &request) {
		return
	}
	if _, valid := parseUUIDOrBadRequest(w, request.ClientID, "client_id"); !valid {
		return
	}
	if _, valid := parseUUIDOrBadRequest(w, request.WorkspaceID, "workspace_id"); !valid {
		return
	}
	if request.Status == "" {
		request.Status = "new"
	}
	if !containsBusinessString([]string{"siteping", "email", "messenger", "meeting", "manual"}, request.Channel) ||
		!containsBusinessString([]string{"new", "triaged", "linked", "closed", "missed", "escalated"}, request.Status) ||
		strings.TrimSpace(request.Summary) == "" || strings.TrimSpace(request.IdempotencyKey) == "" {
		writeError(w, 400, "client request fields are invalid")
		return
	}
	receivedAt, err := time.Parse(time.RFC3339, request.ReceivedAt)
	if err != nil {
		writeError(w, 400, "received_at must be RFC3339")
		return
	}
	var triageDue any
	if request.TriageDueAt != nil && *request.TriageDueAt != "" {
		parsed, err := time.Parse(time.RFC3339, *request.TriageDueAt)
		if err != nil || parsed.Before(receivedAt) {
			writeError(w, 400, "triage_due_at is invalid")
			return
		}
		triageDue = parsed
	}
	if containsBusinessString([]string{"missed", "escalated"}, request.Status) && strings.TrimSpace(stringValue(request.EscalationReason)) == "" {
		writeError(w, 400, "escalation_reason is required")
		return
	}
	tx, err := h.TxStarter.Begin(r.Context())
	if err != nil {
		writeError(w, 500, "failed to create client request")
		return
	}
	defer tx.Rollback(r.Context())
	if !businessEntityExists(r.Context(), tx, "business_client", request.ClientID, businessID) || !businessWorkspaceExists(r.Context(), tx, businessID, request.WorkspaceID) {
		writeError(w, 400, "client or workspace is outside this business")
		return
	}
	if request.ProjectID != nil && *request.ProjectID != "" && !nativeBusinessProjectExists(r.Context(), tx, businessID, request.WorkspaceID, *request.ProjectID) {
		writeError(w, 400, "project is outside this workspace")
		return
	}
	if request.LinkedIssueID != nil && *request.LinkedIssueID != "" {
		if request.ProjectID == nil || !nativeBusinessIssueExists(r.Context(), tx, businessID, request.WorkspaceID, *request.ProjectID, *request.LinkedIssueID) {
			writeError(w, 400, "issue is outside this project")
			return
		}
	}
	var id string
	err = tx.QueryRow(r.Context(), `
		INSERT INTO business_client_request (business_id,client_id,workspace_id,project_id,channel,external_ref,idempotency_key,summary,received_at,triage_due_at,linked_issue_id,pm_worker_id,status,client_escalated_at,escalation_reason)
		VALUES ($1,$2,$3,NULLIF($4,'')::uuid,$5,NULLIF($6,''),$7,$8,$9,$10,NULLIF($11,'')::uuid,NULLIF($12,'')::uuid,$13,CASE WHEN $13 IN ('missed','escalated') THEN now() END,NULLIF($14,''))
		ON CONFLICT (business_id,idempotency_key) DO UPDATE SET updated_at=now()
		RETURNING id::text
	`, businessID, request.ClientID, request.WorkspaceID, stringValue(request.ProjectID), request.Channel,
		stringValue(request.ExternalRef), request.IdempotencyKey, strings.TrimSpace(request.Summary), receivedAt,
		triageDue, stringValue(request.LinkedIssueID), stringValue(request.PMWorkerID), request.Status, stringValue(request.EscalationReason)).Scan(&id)
	if err != nil {
		writeError(w, 500, "failed to create client request")
		return
	}
	if err := h.insertBusinessAudit(r.Context(), tx, businessID, userID, "client_request.upsert", "business_client_request", id, "client request", nil, nil); err != nil {
		writeError(w, 500, "failed to create client request")
		return
	}
	if err := tx.Commit(r.Context()); err != nil {
		writeError(w, 500, "failed to create client request")
		return
	}
	writeJSON(w, http.StatusCreated, map[string]string{"id": id})
}

type businessParticipantInput struct {
	WorkerID string  `json:"worker_id"`
	Role     string  `json:"role"`
	Pool     string  `json:"pool"`
	Weight   *string `json:"weight"`
	Percent  string  `json:"percent"`
}

type createBusinessTaskEconomicsRequest struct {
	WorkspaceID            string                     `json:"workspace_id"`
	ProjectID              string                     `json:"project_id"`
	IssueID                string                     `json:"issue_id"`
	ClientID               *string                    `json:"client_id"`
	ClientRequestID        *string                    `json:"client_request_id"`
	ServiceType            string                     `json:"service_type"`
	ServiceValueRUB        string                     `json:"service_value_rub"`
	Source                 string                     `json:"source"`
	BillingDisposition     string                     `json:"billing_disposition"`
	ClientPriceSnapshotRUB *string                    `json:"client_price_snapshot_rub"`
	InternalAICostRUB      *string                    `json:"internal_ai_cost_rub"`
	IdempotencyKey         string                     `json:"idempotency_key"`
	Participants           []businessParticipantInput `json:"participants"`
}

func (h *Handler) CreateBusinessTaskEconomics(w http.ResponseWriter, r *http.Request) {
	businessID, userID, ok := businessRequestIDs(w, r)
	if !ok {
		return
	}
	var request createBusinessTaskEconomicsRequest
	if !decodeBusinessJSON(w, r, &request) {
		return
	}
	for field, value := range map[string]string{"workspace_id": request.WorkspaceID, "project_id": request.ProjectID, "issue_id": request.IssueID} {
		if _, valid := parseUUIDOrBadRequest(w, value, field); !valid {
			return
		}
	}
	if request.Source == "" {
		request.Source = "manual_override"
	}
	if request.BillingDisposition == "" {
		request.BillingDisposition = "normal"
	}
	if !containsBusinessString([]string{"development", "support", "seo", "content", "internal"}, request.ServiceType) ||
		!containsBusinessString([]string{"billing_estimate", "charge_snapshot", "manual_override"}, request.Source) ||
		!containsBusinessString([]string{"normal", "warranty", "service_recovery", "non_billable"}, request.BillingDisposition) ||
		strings.TrimSpace(request.IdempotencyKey) == "" || len(request.Participants) == 0 {
		writeError(w, 400, "task economics fields are invalid")
		return
	}
	serviceValue, err := parseBusinessBankMoney(request.ServiceValueRUB)
	if err != nil || serviceValue < 0 {
		writeError(w, 400, "service_value_rub is invalid")
		return
	}
	pmPercent, executionPercent := int64(0), int64(0)
	seen := map[string]bool{}
	type parsedParticipant struct {
		businessParticipantInput
		PercentBasisPoints int64
		AmountCents        int64
	}
	parsedParticipants := make([]parsedParticipant, 0, len(request.Participants))
	for _, participant := range request.Participants {
		if _, valid := parseUUIDOrBadRequest(w, participant.WorkerID, "worker_id"); !valid {
			return
		}
		if !containsBusinessString([]string{"pm", "executor", "reviewer", "seo", "content", "copywriter", "designer", "domain_reviewer"}, participant.Role) ||
			!containsBusinessString([]string{"pm", "execution"}, participant.Pool) {
			writeError(w, 400, "participant role or pool is invalid")
			return
		}
		key := participant.WorkerID + ":" + participant.Role + ":" + participant.Pool
		if seen[key] {
			writeError(w, 409, "participant is duplicated")
			return
		}
		seen[key] = true
		percent, err := parseBusinessPercent(participant.Percent)
		if err != nil || percent < 0 {
			writeError(w, 400, "participant percent is invalid")
			return
		}
		if participant.Pool == "pm" {
			pmPercent += percent
		} else {
			executionPercent += percent
		}
		parsedParticipants = append(parsedParticipants, parsedParticipant{participant, percent, serviceValue * percent / 1_000_000})
	}
	maxExecution, maxTotal := int64(250000), int64(350000)
	if containsBusinessString([]string{"seo", "content"}, request.ServiceType) {
		maxExecution, maxTotal = 350000, 450000
	}
	if pmPercent > 100000 || executionPercent > maxExecution || pmPercent+executionPercent > maxTotal {
		writeError(w, 409, "participant pools exceed the approved compensation limits")
		return
	}

	tx, err := h.TxStarter.Begin(r.Context())
	if err != nil {
		writeError(w, 500, "failed to create task economics")
		return
	}
	defer tx.Rollback(r.Context())
	if !nativeBusinessIssueExists(r.Context(), tx, businessID, request.WorkspaceID, request.ProjectID, request.IssueID) {
		writeError(w, 400, "issue is outside this business project")
		return
	}
	if request.ClientID != nil && *request.ClientID != "" {
		var mapped bool
		_ = tx.QueryRow(r.Context(), `SELECT EXISTS(SELECT 1 FROM business_client_project WHERE business_id=$1 AND client_id=$2 AND workspace_id=$3 AND project_id=$4)`, businessID, *request.ClientID, request.WorkspaceID, request.ProjectID).Scan(&mapped)
		if !mapped {
			writeError(w, 400, "client is not mapped to this project")
			return
		}
	}
	pmEligible, pmReason := true, ""
	if request.BillingDisposition != "normal" {
		pmEligible, pmReason = false, "billing_disposition:"+request.BillingDisposition
	}
	if request.ClientRequestID != nil && *request.ClientRequestID != "" {
		var status string
		if err := tx.QueryRow(r.Context(), `SELECT status FROM business_client_request WHERE business_id=$1 AND id=$2`, businessID, *request.ClientRequestID).Scan(&status); err != nil {
			writeError(w, 400, "client_request_id is invalid")
			return
		}
		if containsBusinessString([]string{"missed", "escalated"}, status) {
			pmEligible, pmReason = false, "request_"+status
		}
	}
	if !pmEligible && pmPercent > 0 {
		writeError(w, 409, "PM pool is not eligible for this task")
		return
	}
	for _, participant := range parsedParticipants {
		if !businessEntityExists(r.Context(), tx, "business_worker", participant.WorkerID, businessID) {
			writeError(w, 400, "participant worker is outside this business")
			return
		}
	}
	policy, _ := json.Marshal(map[string]any{"pm_max_percent": 10, "execution_max_percent": float64(maxExecution) / 10000, "total_max_percent": float64(maxTotal) / 10000, "holdback_percent": 0})
	var economicsID string
	err = tx.QueryRow(r.Context(), `
		INSERT INTO business_task_economics (business_id,workspace_id,project_id,issue_id,client_id,client_request_id,service_type,service_value_rub,source,billing_disposition,client_price_snapshot_rub,internal_ai_cost_rub,policy_snapshot,pm_eligible,pm_ineligible_reason,idempotency_key)
		VALUES ($1,$2,$3,$4,NULLIF($5,'')::uuid,NULLIF($6,'')::uuid,$7,$8::numeric/100,$9,$10,NULLIF($11,'')::numeric,NULLIF($12,'')::numeric,$13,$14,NULLIF($15,''),$16)
		ON CONFLICT (business_id,idempotency_key) DO UPDATE SET updated_at=now()
		RETURNING id::text
	`, businessID, request.WorkspaceID, request.ProjectID, request.IssueID, stringValue(request.ClientID), stringValue(request.ClientRequestID), request.ServiceType,
		serviceValue, request.Source, request.BillingDisposition, stringValue(request.ClientPriceSnapshotRUB), stringValue(request.InternalAICostRUB), policy, pmEligible, pmReason, request.IdempotencyKey).Scan(&economicsID)
	if err != nil {
		writeError(w, 500, "failed to create task economics")
		return
	}
	for _, participant := range parsedParticipants {
		weight := "1"
		if participant.Weight != nil && *participant.Weight != "" {
			weight = *participant.Weight
		}
		if _, err := tx.Exec(r.Context(), `
			INSERT INTO business_task_participant (business_id,task_economics_id,worker_id,role,pool,weight,percent,amount_rub,participation_confirmed,confirmed_by,confirmed_at)
			VALUES ($1,$2,$3,$4,$5,$6::numeric,$7::numeric/10000,$8::numeric/100,true,$9,now())
			ON CONFLICT (task_economics_id,worker_id,role) DO UPDATE SET pool=EXCLUDED.pool,weight=EXCLUDED.weight,percent=EXCLUDED.percent,amount_rub=EXCLUDED.amount_rub,updated_at=now()
		`, businessID, economicsID, participant.WorkerID, participant.Role, participant.Pool, weight, participant.PercentBasisPoints, participant.AmountCents, userID); err != nil {
			writeError(w, 500, "failed to save task participants")
			return
		}
	}
	if err := h.insertBusinessAudit(r.Context(), tx, businessID, userID, "task_economics.draft", "business_task_economics", economicsID, "economics shadow draft", nil, nil); err != nil {
		writeError(w, 500, "failed to create task economics")
		return
	}
	if err := tx.Commit(r.Context()); err != nil {
		writeError(w, 500, "failed to create task economics")
		return
	}
	writeJSON(w, http.StatusCreated, map[string]string{"id": economicsID})
}

type acceptBusinessEconomicsRequest struct {
	Reason string `json:"reason"`
}

func (h *Handler) AcceptBusinessTaskEconomics(w http.ResponseWriter, r *http.Request) {
	businessID, userID, ok := businessRequestIDs(w, r)
	if !ok {
		return
	}
	economicsID := chi.URLParam(r, "economicsId")
	if _, valid := parseUUIDOrBadRequest(w, economicsID, "economics_id"); !valid {
		return
	}
	var request acceptBusinessEconomicsRequest
	if !decodeBusinessJSON(w, r, &request) {
		return
	}
	tx, err := h.TxStarter.Begin(r.Context())
	if err != nil {
		writeError(w, 500, "failed to accept economics")
		return
	}
	defer tx.Rollback(r.Context())
	var status string
	if err := tx.QueryRow(r.Context(), `SELECT status FROM business_task_economics WHERE business_id=$1 AND id=$2 FOR UPDATE`, businessID, economicsID).Scan(&status); errors.Is(err, pgx.ErrNoRows) {
		writeError(w, 404, "task economics not found")
		return
	} else if err != nil {
		writeError(w, 500, "failed to accept economics")
		return
	}
	if status == "accepted" {
		writeJSON(w, 200, map[string]string{"id": economicsID, "status": "accepted"})
		return
	}
	var participantCount int
	if err := tx.QueryRow(r.Context(), `SELECT count(*) FROM business_task_participant WHERE business_id=$1 AND task_economics_id=$2 AND participation_confirmed`, businessID, economicsID).Scan(&participantCount); err != nil || participantCount == 0 {
		writeError(w, 409, "task has no confirmed participants")
		return
	}
	if _, err := tx.Exec(r.Context(), `UPDATE business_task_economics SET status='accepted',accepted_at=now(),accepted_by=$3,updated_at=now() WHERE business_id=$1 AND id=$2`, businessID, economicsID, userID); err != nil {
		writeError(w, 500, "failed to accept economics")
		return
	}
	if _, err := tx.Exec(r.Context(), `
		INSERT INTO business_accrual (business_id,task_economics_id,participant_id,worker_id,role,policy_snapshot,original_amount_rub,holdback_percent,idempotency_key)
		SELECT p.business_id,p.task_economics_id,p.id,p.worker_id,p.role,e.policy_snapshot,p.amount_rub,0,'accept:'||e.id::text||':'||p.id::text
		FROM business_task_participant p JOIN business_task_economics e ON e.id=p.task_economics_id AND e.business_id=p.business_id
		WHERE p.business_id=$1 AND p.task_economics_id=$2 AND p.participation_confirmed
		ON CONFLICT (business_id,idempotency_key) DO NOTHING
	`, businessID, economicsID); err != nil {
		writeError(w, 500, "failed to create immutable accruals")
		return
	}
	if err := h.insertBusinessAudit(r.Context(), tx, businessID, userID, "task_economics.accept", "business_task_economics", economicsID, request.Reason, nil, nil); err != nil {
		writeError(w, 500, "failed to accept economics")
		return
	}
	if err := tx.Commit(r.Context()); err != nil {
		writeError(w, 500, "failed to accept economics")
		return
	}
	writeJSON(w, 200, map[string]string{"id": economicsID, "status": "accepted"})
}

type linkBusinessReceivableTaskRequest struct {
	ReceivableID      string `json:"receivable_id"`
	TaskEconomicsID   string `json:"task_economics_id"`
	AllocatedValueRUB string `json:"allocated_value_rub"`
}

func (h *Handler) LinkBusinessReceivableTask(w http.ResponseWriter, r *http.Request) {
	businessID, userID, ok := businessRequestIDs(w, r)
	if !ok {
		return
	}
	var request linkBusinessReceivableTaskRequest
	if !decodeBusinessJSON(w, r, &request) {
		return
	}
	for field, value := range map[string]string{"receivable_id": request.ReceivableID, "task_economics_id": request.TaskEconomicsID} {
		if _, valid := parseUUIDOrBadRequest(w, value, field); !valid {
			return
		}
	}
	allocated, err := parseBusinessBankMoney(request.AllocatedValueRUB)
	if err != nil || allocated <= 0 {
		writeError(w, 400, "allocated_value_rub is invalid")
		return
	}
	tx, err := h.TxStarter.Begin(r.Context())
	if err != nil {
		writeError(w, 500, "failed to link receivable")
		return
	}
	defer tx.Rollback(r.Context())
	var serviceValue int64
	if err := tx.QueryRow(r.Context(), `SELECT round(service_value_rub*100)::bigint FROM business_task_economics WHERE business_id=$1 AND id=$2 AND status='accepted' FOR UPDATE`, businessID, request.TaskEconomicsID).Scan(&serviceValue); err != nil {
		writeError(w, 400, "accepted task economics not found")
		return
	}
	var dueOn *time.Time
	if err := tx.QueryRow(r.Context(), `SELECT due_on FROM business_receivable WHERE business_id=$1 AND id=$2 AND status NOT IN ('skipped','written_off')`, businessID, request.ReceivableID).Scan(&dueOn); err != nil {
		writeError(w, 400, "receivable not found")
		return
	}
	var already int64
	_ = tx.QueryRow(r.Context(), `SELECT COALESCE(round(sum(allocated_value_rub)*100),0)::bigint FROM business_receivable_task WHERE business_id=$1 AND task_economics_id=$2`, businessID, request.TaskEconomicsID).Scan(&already)
	if already+allocated > serviceValue {
		writeError(w, 409, "receivable allocations exceed task service value")
		return
	}
	var id string
	err = tx.QueryRow(r.Context(), `
		INSERT INTO business_receivable_task (business_id,receivable_id,task_economics_id,service_value_rub,allocated_value_rub)
		VALUES ($1,$2,$3,$4::numeric/100,$5::numeric/100)
		ON CONFLICT (receivable_id,task_economics_id) DO UPDATE SET allocated_value_rub=EXCLUDED.allocated_value_rub,updated_at=now()
		RETURNING id::text
	`, businessID, request.ReceivableID, request.TaskEconomicsID, serviceValue, allocated).Scan(&id)
	if err != nil {
		writeError(w, 500, "failed to link receivable")
		return
	}
	if dueOn != nil {
		_, _ = tx.Exec(r.Context(), `UPDATE business_accrual SET reserve_due_on=$3::date+30,updated_at=now() WHERE business_id=$1 AND task_economics_id=$2 AND reserve_due_on IS NULL`, businessID, request.TaskEconomicsID, dateOnly(*dueOn))
	}
	if err := recalculateBusinessReceivableFunding(r.Context(), tx, businessID, request.ReceivableID); err != nil {
		writeError(w, 500, "failed to update task funding")
		return
	}
	if err := h.insertBusinessAudit(r.Context(), tx, businessID, userID, "receivable_task.link", "business_receivable_task", id, "funding source", nil, nil); err != nil {
		writeError(w, 500, "failed to link receivable")
		return
	}
	if err := tx.Commit(r.Context()); err != nil {
		writeError(w, 500, "failed to link receivable")
		return
	}
	writeJSON(w, 201, map[string]string{"id": id})
}

type createReserveEntryRequest struct {
	EntryType      string  `json:"entry_type"`
	AmountRUB      string  `json:"amount_rub"`
	AccrualID      *string `json:"accrual_id"`
	PayoutBatchID  *string `json:"payout_batch_id"`
	Reason         string  `json:"reason"`
	IdempotencyKey string  `json:"idempotency_key"`
}

func (h *Handler) CreateBusinessReserveEntry(w http.ResponseWriter, r *http.Request) {
	businessID, userID, ok := businessRequestIDs(w, r)
	if !ok {
		return
	}
	var request createReserveEntryRequest
	if !decodeBusinessJSON(w, r, &request) {
		return
	}
	amount, err := parseSignedBusinessMoney(request.AmountRUB)
	if err != nil || amount == 0 || !containsBusinessString([]string{"contribution", "allocation", "release", "correction"}, request.EntryType) || strings.TrimSpace(request.Reason) == "" || strings.TrimSpace(request.IdempotencyKey) == "" {
		writeError(w, 400, "reserve entry fields are invalid")
		return
	}
	if request.EntryType == "contribution" && amount < 0 || request.EntryType == "allocation" && amount > 0 {
		writeError(w, 400, "contributions are positive and allocations are negative")
		return
	}
	tx, err := h.TxStarter.Begin(r.Context())
	if err != nil {
		writeError(w, 500, "failed to create reserve entry")
		return
	}
	defer tx.Rollback(r.Context())
	if request.AccrualID != nil && *request.AccrualID != "" && !businessEntityExists(r.Context(), tx, "business_accrual", *request.AccrualID, businessID) {
		writeError(w, 400, "accrual is outside this business")
		return
	}
	var balance int64
	_ = tx.QueryRow(r.Context(), `SELECT COALESCE(round(sum(amount_rub)*100),0)::bigint FROM business_reserve_ledger WHERE business_id=$1`, businessID).Scan(&balance)
	if balance+amount < 0 {
		writeError(w, 409, "reserve balance is insufficient")
		return
	}
	var id string
	err = tx.QueryRow(r.Context(), `INSERT INTO business_reserve_ledger (business_id,entry_type,amount_rub,accrual_id,payout_batch_id,reason,actor_user_id,idempotency_key) VALUES ($1,$2,$3::numeric/100,NULLIF($4,'')::uuid,NULLIF($5,'')::uuid,$6,$7,$8) ON CONFLICT (business_id,idempotency_key) DO UPDATE SET idempotency_key=EXCLUDED.idempotency_key RETURNING id::text`, businessID, request.EntryType, amount, stringValue(request.AccrualID), stringValue(request.PayoutBatchID), strings.TrimSpace(request.Reason), userID, request.IdempotencyKey).Scan(&id)
	if err != nil {
		writeError(w, 500, "failed to create reserve entry")
		return
	}
	if request.EntryType == "allocation" && request.AccrualID != nil {
		allocation := -amount
		_, err = tx.Exec(r.Context(), `UPDATE business_accrual SET reserve_funded_rub=LEAST(original_amount_rub-funded_rub,reserve_funded_rub+$3::numeric/100),status=CASE WHEN funded_rub+reserve_funded_rub+$3::numeric/100>=original_amount_rub THEN 'payable' ELSE 'partially_payable' END,payable_at=CASE WHEN funded_rub+reserve_funded_rub+$3::numeric/100>=original_amount_rub THEN COALESCE(payable_at,now()) END,updated_at=now() WHERE business_id=$1 AND id=$2 AND status NOT IN ('in_payout','paid')`, businessID, *request.AccrualID, allocation)
		if err != nil {
			writeError(w, 500, "failed to allocate reserve")
			return
		}
	}
	if err := h.insertBusinessAudit(r.Context(), tx, businessID, userID, "reserve.entry", "business_reserve_ledger", id, request.Reason, nil, nil); err != nil {
		writeError(w, 500, "failed to create reserve entry")
		return
	}
	if err := tx.Commit(r.Context()); err != nil {
		writeError(w, 500, "failed to create reserve entry")
		return
	}
	writeJSON(w, 201, map[string]string{"id": id})
}

type createQualityCaseRequest struct {
	IssueID         string  `json:"issue_id"`
	TaskEconomicsID *string `json:"task_economics_id"`
	Severity        string  `json:"severity"`
	Summary         string  `json:"summary"`
}

func (h *Handler) CreateBusinessQualityCase(w http.ResponseWriter, r *http.Request) {
	businessID, userID, ok := businessRequestIDs(w, r)
	if !ok {
		return
	}
	var request createQualityCaseRequest
	if !decodeBusinessJSON(w, r, &request) {
		return
	}
	if _, valid := parseUUIDOrBadRequest(w, request.IssueID, "issue_id"); !valid {
		return
	}
	if !containsBusinessString([]string{"critical", "major", "minor", "cosmetic"}, request.Severity) || strings.TrimSpace(request.Summary) == "" {
		writeError(w, 400, "quality case fields are invalid")
		return
	}
	tx, err := h.TxStarter.Begin(r.Context())
	if err != nil {
		writeError(w, 500, "failed to create quality case")
		return
	}
	defer tx.Rollback(r.Context())
	var id string
	err = tx.QueryRow(r.Context(), `INSERT INTO business_quality_case (business_id,issue_id,task_economics_id,severity,summary,created_by) VALUES ($1,$2,NULLIF($3,'')::uuid,$4,$5,$6) RETURNING id::text`, businessID, request.IssueID, stringValue(request.TaskEconomicsID), request.Severity, strings.TrimSpace(request.Summary), userID).Scan(&id)
	if err != nil {
		writeError(w, 500, "failed to create quality case")
		return
	}
	if err := h.insertBusinessAudit(r.Context(), tx, businessID, userID, "quality_case.create", "business_quality_case", id, "manual quality case; no automatic penalty", nil, nil); err != nil {
		writeError(w, 500, "failed to create quality case")
		return
	}
	if err := tx.Commit(r.Context()); err != nil {
		writeError(w, 500, "failed to create quality case")
		return
	}
	writeJSON(w, 201, map[string]string{"id": id})
}

type createAccrualAdjustmentRequest struct {
	AccrualID      string  `json:"accrual_id"`
	QualityCaseID  *string `json:"quality_case_id"`
	AmountRUB      string  `json:"amount_rub"`
	Reason         string  `json:"reason"`
	DecisionRef    *string `json:"decision_ref"`
	Notes          string  `json:"notes"`
	IdempotencyKey string  `json:"idempotency_key"`
}

func (h *Handler) CreateBusinessAccrualAdjustment(w http.ResponseWriter, r *http.Request) {
	businessID, userID, ok := businessRequestIDs(w, r)
	if !ok {
		return
	}
	var request createAccrualAdjustmentRequest
	if !decodeBusinessJSON(w, r, &request) {
		return
	}
	if _, valid := parseUUIDOrBadRequest(w, request.AccrualID, "accrual_id"); !valid {
		return
	}
	amount, err := parseSignedBusinessMoney(request.AmountRUB)
	if err != nil || amount == 0 || !containsBusinessString([]string{"quality_case", "owner_correction"}, request.Reason) || strings.TrimSpace(request.Notes) == "" || strings.TrimSpace(request.IdempotencyKey) == "" {
		writeError(w, 400, "adjustment fields are invalid")
		return
	}
	if request.Reason == "quality_case" && (request.QualityCaseID == nil || *request.QualityCaseID == "") {
		writeError(w, 400, "quality_case_id is required")
		return
	}
	tx, err := h.TxStarter.Begin(r.Context())
	if err != nil {
		writeError(w, 500, "failed to create adjustment")
		return
	}
	defer tx.Rollback(r.Context())
	if !businessEntityExists(r.Context(), tx, "business_accrual", request.AccrualID, businessID) {
		writeError(w, 404, "accrual not found")
		return
	}
	var id string
	err = tx.QueryRow(r.Context(), `INSERT INTO business_accrual_adjustment (business_id,accrual_id,quality_case_id,amount_rub,reason,decision_ref,notes,actor_user_id,idempotency_key) VALUES ($1,$2,NULLIF($3,'')::uuid,$4::numeric/100,$5,NULLIF($6,''),$7,$8,$9) ON CONFLICT (business_id,idempotency_key) DO UPDATE SET idempotency_key=EXCLUDED.idempotency_key RETURNING id::text`, businessID, request.AccrualID, stringValue(request.QualityCaseID), amount, request.Reason, stringValue(request.DecisionRef), strings.TrimSpace(request.Notes), userID, request.IdempotencyKey).Scan(&id)
	if err != nil {
		writeError(w, 500, "failed to create adjustment")
		return
	}
	if err := h.insertBusinessAudit(r.Context(), tx, businessID, userID, "accrual.adjustment", "business_accrual_adjustment", id, request.Notes, nil, nil); err != nil {
		writeError(w, 500, "failed to create adjustment")
		return
	}
	if err := tx.Commit(r.Context()); err != nil {
		writeError(w, 500, "failed to create adjustment")
		return
	}
	writeJSON(w, 201, map[string]string{"id": id})
}

type buildBusinessPayoutRequest struct {
	PeriodKey      string `json:"period_key"`
	IdempotencyKey string `json:"idempotency_key"`
}

func (h *Handler) BuildBusinessPayoutBatch(w http.ResponseWriter, r *http.Request) {
	businessID, userID, ok := businessRequestIDs(w, r)
	if !ok {
		return
	}
	var request buildBusinessPayoutRequest
	if !decodeBusinessJSON(w, r, &request) {
		return
	}
	if strings.TrimSpace(request.PeriodKey) == "" || strings.TrimSpace(request.IdempotencyKey) == "" {
		writeError(w, 400, "period_key and idempotency_key are required")
		return
	}
	tx, err := h.TxStarter.Begin(r.Context())
	if err != nil {
		writeError(w, 500, "failed to build payout")
		return
	}
	defer tx.Rollback(r.Context())
	var batchID string
	err = tx.QueryRow(r.Context(), `INSERT INTO business_payout_batch (business_id,period_key,idempotency_key) VALUES ($1,$2,$3) ON CONFLICT (business_id,idempotency_key) DO UPDATE SET updated_at=now() RETURNING id::text`, businessID, strings.TrimSpace(request.PeriodKey), request.IdempotencyKey).Scan(&batchID)
	if err != nil {
		writeError(w, 500, "failed to build payout")
		return
	}
	_, err = tx.Exec(r.Context(), `
		WITH adjusted AS (SELECT a.*,a.original_amount_rub+COALESCE((SELECT sum(x.amount_rub) FROM business_accrual_adjustment x WHERE x.business_id=a.business_id AND x.accrual_id=a.id),0) adjusted FROM business_accrual a WHERE a.business_id=$1 AND a.status IN ('partially_payable','payable')),
		eligible AS (SELECT *,GREATEST(LEAST(adjusted,funded_rub+reserve_funded_rub)-paid_rub,0) due FROM adjusted)
		INSERT INTO business_payout_item (business_id,payout_batch_id,accrual_id,worker_id,amount_rub)
		SELECT business_id,$2,id,worker_id,due FROM eligible WHERE due>0
		ON CONFLICT (accrual_id) WHERE status IN ('pending','submitted') DO NOTHING
	`, businessID, batchID)
	if err != nil {
		writeError(w, 500, "failed to build payout")
		return
	}
	_, err = tx.Exec(r.Context(), `UPDATE business_payout_batch b SET total_rub=x.total,worker_count=x.workers,updated_at=now() FROM (SELECT COALESCE(sum(amount_rub),0) total,count(DISTINCT worker_id)::int workers FROM business_payout_item WHERE business_id=$1 AND payout_batch_id=$2 AND status='pending') x WHERE b.business_id=$1 AND b.id=$2`, businessID, batchID)
	if err != nil {
		writeError(w, 500, "failed to build payout")
		return
	}
	if err := h.insertBusinessAudit(r.Context(), tx, businessID, userID, "payout.build", "business_payout_batch", batchID, "draft payout batch", nil, nil); err != nil {
		writeError(w, 500, "failed to build payout")
		return
	}
	if err := tx.Commit(r.Context()); err != nil {
		writeError(w, 500, "failed to build payout")
		return
	}
	writeJSON(w, 201, map[string]string{"id": batchID, "status": "draft"})
}

func (h *Handler) ApproveBusinessPayoutBatch(w http.ResponseWriter, r *http.Request) {
	h.changeBusinessPayoutStatus(w, r, "draft", "approved", "payout.approve")
}

func (h *Handler) changeBusinessPayoutStatus(w http.ResponseWriter, r *http.Request, from, to, action string) {
	businessID, userID, ok := businessRequestIDs(w, r)
	if !ok {
		return
	}
	batchID := chi.URLParam(r, "batchId")
	if _, valid := parseUUIDOrBadRequest(w, batchID, "batch_id"); !valid {
		return
	}
	tx, err := h.TxStarter.Begin(r.Context())
	if err != nil {
		writeError(w, 500, "failed to update payout")
		return
	}
	defer tx.Rollback(r.Context())
	command, err := tx.Exec(r.Context(), `UPDATE business_payout_batch SET status=$3,approved_by=CASE WHEN $3='approved' THEN $4::uuid ELSE approved_by END,approved_at=CASE WHEN $3='approved' THEN now() ELSE approved_at END,updated_at=now() WHERE business_id=$1 AND id=$2 AND status=$5 AND total_rub>0`, businessID, batchID, to, userID, from)
	if err != nil {
		writeError(w, 500, "failed to update payout")
		return
	}
	if command.RowsAffected() == 0 {
		writeError(w, 409, "payout batch is empty or in the wrong state")
		return
	}
	if err := h.insertBusinessAudit(r.Context(), tx, businessID, userID, action, "business_payout_batch", batchID, "owner approval", nil, nil); err != nil {
		writeError(w, 500, "failed to update payout")
		return
	}
	if err := tx.Commit(r.Context()); err != nil {
		writeError(w, 500, "failed to update payout")
		return
	}
	writeJSON(w, 200, map[string]string{"id": batchID, "status": to})
}

func (h *Handler) SubmitBusinessPayoutDraft(w http.ResponseWriter, r *http.Request) {
	businessID, userID, ok := businessRequestIDs(w, r)
	if !ok {
		return
	}
	batchID := chi.URLParam(r, "batchId")
	if _, valid := parseUUIDOrBadRequest(w, batchID, "batch_id"); !valid {
		return
	}
	tx, err := h.TxStarter.Begin(r.Context())
	if err != nil {
		writeError(w, 500, "failed to submit payout draft")
		return
	}
	defer tx.Rollback(r.Context())
	var payload []byte
	err = tx.QueryRow(r.Context(), `SELECT jsonb_build_object('payout_batch_id',b.id,'period_key',b.period_key,'total_rub',b.total_rub,'items',COALESCE((SELECT jsonb_agg(jsonb_build_object('worker_id',i.worker_id,'amount_rub',i.amount_rub,'recipient_external_id',w.recipient_external_id,'recipient_mask',w.recipient_mask)) FROM business_payout_item i JOIN business_worker w ON w.id=i.worker_id WHERE i.payout_batch_id=b.id AND i.business_id=b.business_id AND i.status='pending'),'[]'::jsonb)) FROM business_payout_batch b WHERE b.business_id=$1 AND b.id=$2 AND b.status='approved' FOR UPDATE`, businessID, batchID).Scan(&payload)
	if errors.Is(err, pgx.ErrNoRows) {
		writeError(w, 409, "payout batch must be approved")
		return
	} else if err != nil {
		writeError(w, 500, "failed to submit payout draft")
		return
	}
	idempotency := "payout-draft:" + batchID
	var outboxID string
	err = tx.QueryRow(r.Context(), `INSERT INTO business_bank_outbox (business_id,aggregate_type,aggregate_id,event_type,payload,idempotency_key) VALUES ($1,'payout_batch',$2,'create_payout_draft',$3,$4) ON CONFLICT (business_id,idempotency_key) DO UPDATE SET updated_at=now() RETURNING id::text`, businessID, batchID, payload, idempotency).Scan(&outboxID)
	if err != nil {
		writeError(w, 500, "failed to queue payout draft")
		return
	}
	_, err = tx.Exec(r.Context(), `UPDATE business_payout_batch SET status='submitted',submitted_at=now(),updated_at=now() WHERE business_id=$1 AND id=$2`, businessID, batchID)
	if err != nil {
		writeError(w, 500, "failed to submit payout draft")
		return
	}
	if _, err = tx.Exec(r.Context(), `UPDATE business_payout_item SET status='submitted',updated_at=now() WHERE business_id=$1 AND payout_batch_id=$2 AND status='pending'`, businessID, batchID); err != nil {
		writeError(w, 500, "failed to submit payout draft")
		return
	}
	if _, err = tx.Exec(r.Context(), `UPDATE business_accrual a SET status='in_payout',updated_at=now() FROM business_payout_item i WHERE i.business_id=$1 AND i.payout_batch_id=$2 AND i.accrual_id=a.id AND a.business_id=$1`, businessID, batchID); err != nil {
		writeError(w, 500, "failed to submit payout draft")
		return
	}
	if err := h.insertBusinessAudit(r.Context(), tx, businessID, userID, "payout.submit_draft", "business_payout_batch", batchID, "bank draft only; signing remains in bank", nil, nil); err != nil {
		writeError(w, 500, "failed to submit payout draft")
		return
	}
	if err := tx.Commit(r.Context()); err != nil {
		writeError(w, 500, "failed to submit payout draft")
		return
	}
	writeJSON(w, 202, map[string]string{"id": batchID, "status": "submitted", "outbox_id": outboxID})
}

func (h *Handler) GetMyBusinessCompensation(w http.ResponseWriter, r *http.Request) {
	businessID, userID, ok := businessRequestIDs(w, r)
	if !ok {
		return
	}
	raw, err := queryBusinessJSON(r.Context(), h.DB, `
		SELECT jsonb_build_object('worker',to_jsonb(w),'summary',jsonb_build_object(
			'accrued_rub',COALESCE(sum(a.original_amount_rub),0),'funded_rub',COALESCE(sum(a.funded_rub+a.reserve_funded_rub),0),
			'payable_rub',COALESCE(sum(CASE WHEN a.status IN ('partially_payable','payable','in_payout') THEN GREATEST(LEAST(a.original_amount_rub,a.funded_rub+a.reserve_funded_rub)-a.paid_rub,0) ELSE 0 END),0),
			'paid_rub',COALESCE(sum(a.paid_rub),0)),'accruals',COALESCE(jsonb_agg(to_jsonb(a) ORDER BY a.created_at DESC) FILTER (WHERE a.id IS NOT NULL),'[]'::jsonb))
		FROM business_worker w LEFT JOIN business_accrual a ON a.business_id=w.business_id AND a.worker_id=w.id
		WHERE w.business_id=$1 AND w.user_id=$2 GROUP BY w.id
	`, businessID, userID)
	if errors.Is(err, pgx.ErrNoRows) {
		writeError(w, 404, "worker profile not found")
		return
	}
	if err != nil {
		writeError(w, 500, "failed to load compensation")
		return
	}
	writeJSON(w, 200, raw)
}

func parseBusinessPercent(value string) (int64, error)     { return parseFixedDecimal(value, 4) }
func parseSignedBusinessMoney(value string) (int64, error) { return parseFixedDecimal(value, 2) }
func parseFixedDecimal(value string, scale int) (int64, error) {
	value = strings.TrimSpace(strings.ReplaceAll(value, ",", "."))
	if value == "" {
		return 0, errors.New("empty")
	}
	negative := strings.HasPrefix(value, "-")
	value = strings.TrimPrefix(value, "-")
	parts := strings.Split(value, ".")
	if len(parts) > 2 {
		return 0, errors.New("invalid")
	}
	whole, err := parseDecimalDigits(parts[0])
	if err != nil {
		return 0, err
	}
	fraction := ""
	if len(parts) == 2 {
		fraction = parts[1]
	}
	if len(fraction) > scale {
		return 0, errors.New("too precise")
	}
	for len(fraction) < scale {
		fraction += "0"
	}
	fractionValue, err := parseDecimalDigits(fraction)
	if err != nil {
		return 0, err
	}
	factor := int64(1)
	for i := 0; i < scale; i++ {
		factor *= 10
	}
	result := whole*factor + fractionValue
	if negative {
		result = -result
	}
	return result, nil
}
func parseDecimalDigits(value string) (int64, error) {
	if value == "" {
		return 0, nil
	}
	var result int64
	for _, r := range value {
		if r < '0' || r > '9' {
			return 0, errors.New("invalid number")
		}
		result = result*10 + int64(r-'0')
	}
	return result, nil
}
