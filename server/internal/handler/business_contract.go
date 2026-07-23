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

var businessContractStatuses = []string{"draft", "active", "expired", "terminated"}

type createBusinessContractRequest struct {
	Number       string  `json:"number"`
	Subject      string  `json:"subject"`
	AmountRUB    *string `json:"amount_rub"`
	StartsOn     *string `json:"starts_on"`
	EndsOn       *string `json:"ends_on"`
	Status       string  `json:"status"`
	AttachmentID *string `json:"attachment_id"`
	Notes        *string `json:"notes"`
}

// validBusinessContractDate accepts empty (cleared) or a YYYY-MM-DD date.
func validBusinessContractDate(value *string) bool {
	if value == nil || strings.TrimSpace(*value) == "" {
		return true
	}
	_, err := time.Parse("2006-01-02", strings.TrimSpace(*value))
	return err == nil
}

// CreateBusinessContract attaches a contract (structured metadata + an optional
// uploaded file, referenced by attachment_id) to a client. Owner-only via the
// route group; the file itself is uploaded first through /api/upload-file.
func (h *Handler) CreateBusinessContract(w http.ResponseWriter, r *http.Request) {
	businessID, userID, ok := businessRequestIDs(w, r)
	if !ok {
		return
	}
	clientID := chi.URLParam(r, "clientId")
	if _, valid := parseUUIDOrBadRequest(w, clientID, "client_id"); !valid {
		return
	}
	var request createBusinessContractRequest
	if !decodeBusinessJSON(w, r, &request) {
		return
	}
	if request.Status == "" {
		request.Status = "active"
	}
	if !containsBusinessString(businessContractStatuses, request.Status) {
		writeError(w, http.StatusBadRequest, "invalid status")
		return
	}
	if request.AmountRUB != nil && strings.TrimSpace(*request.AmountRUB) != "" && !validBusinessMoney(strings.TrimSpace(*request.AmountRUB)) {
		writeError(w, http.StatusBadRequest, "invalid amount_rub")
		return
	}
	if !validBusinessContractDate(request.StartsOn) || !validBusinessContractDate(request.EndsOn) {
		writeError(w, http.StatusBadRequest, "dates must use YYYY-MM-DD")
		return
	}
	if request.AttachmentID != nil && strings.TrimSpace(*request.AttachmentID) != "" {
		if _, valid := parseUUIDOrBadRequest(w, strings.TrimSpace(*request.AttachmentID), "attachment_id"); !valid {
			return
		}
	}

	tx, err := h.TxStarter.Begin(r.Context())
	if err != nil {
		writeError(w, http.StatusInternalServerError, "failed to create contract")
		return
	}
	defer tx.Rollback(r.Context())

	// The client must belong to this business.
	var exists bool
	if err := tx.QueryRow(r.Context(), `SELECT EXISTS(SELECT 1 FROM business_client WHERE id = $1 AND business_id = $2)`, clientID, businessID).Scan(&exists); err != nil || !exists {
		writeError(w, http.StatusNotFound, "client not found")
		return
	}
	// A referenced attachment must exist (FK also enforces this, but a clean 400 beats a 500).
	if request.AttachmentID != nil && strings.TrimSpace(*request.AttachmentID) != "" {
		var attExists bool
		if err := tx.QueryRow(r.Context(), `SELECT EXISTS(SELECT 1 FROM attachment WHERE id = $1)`, strings.TrimSpace(*request.AttachmentID)).Scan(&attExists); err != nil || !attExists {
			writeError(w, http.StatusBadRequest, "invalid attachment_id")
			return
		}
	}

	raw, err := queryBusinessRowJSON(r.Context(), tx.QueryRow(r.Context(), `
		INSERT INTO business_contract (business_id, client_id, number, subject, amount_rub, starts_on, ends_on, status, attachment_id, notes)
		VALUES ($1, $2, $3, $4, NULLIF($5, '')::numeric, NULLIF($6, '')::date, NULLIF($7, '')::date, $8, NULLIF($9, '')::uuid, $10)
		RETURNING to_jsonb(business_contract)
	`, businessID, clientID, strings.TrimSpace(request.Number), strings.TrimSpace(request.Subject),
		stringValue(request.AmountRUB), stringValue(request.StartsOn), stringValue(request.EndsOn),
		request.Status, stringValue(request.AttachmentID), request.Notes))
	if err != nil {
		writeError(w, http.StatusInternalServerError, "failed to create contract")
		return
	}
	var created struct {
		ID string `json:"id"`
	}
	if err := json.Unmarshal(raw, &created); err != nil {
		writeError(w, http.StatusInternalServerError, "failed to encode contract")
		return
	}
	if err := h.insertBusinessAudit(r.Context(), tx, businessID, userID, "contract.created", "business_contract", created.ID, "", nil, raw); err != nil {
		writeError(w, http.StatusInternalServerError, "failed to audit contract")
		return
	}
	if err := tx.Commit(r.Context()); err != nil {
		writeError(w, http.StatusInternalServerError, "failed to create contract")
		return
	}
	writeJSON(w, http.StatusCreated, raw)
}

type updateBusinessContractRequest struct {
	Number       *string `json:"number"`
	Subject      *string `json:"subject"`
	AmountRUB    *string `json:"amount_rub"`
	StartsOn     *string `json:"starts_on"`
	EndsOn       *string `json:"ends_on"`
	Status       *string `json:"status"`
	AttachmentID *string `json:"attachment_id"`
	Notes        *string `json:"notes"`
}

// UpdateBusinessContract edits contract metadata. Nullable text/date/amount
// fields clear when an empty string is sent and stay put when omitted.
func (h *Handler) UpdateBusinessContract(w http.ResponseWriter, r *http.Request) {
	businessID, userID, ok := businessRequestIDs(w, r)
	if !ok {
		return
	}
	contractID := chi.URLParam(r, "contractId")
	if _, valid := parseUUIDOrBadRequest(w, contractID, "contract_id"); !valid {
		return
	}
	var request updateBusinessContractRequest
	if !decodeBusinessJSON(w, r, &request) {
		return
	}
	if request.Status != nil && !containsBusinessString(businessContractStatuses, *request.Status) {
		writeError(w, http.StatusBadRequest, "invalid status")
		return
	}
	if request.AmountRUB != nil && strings.TrimSpace(*request.AmountRUB) != "" && !validBusinessMoney(strings.TrimSpace(*request.AmountRUB)) {
		writeError(w, http.StatusBadRequest, "invalid amount_rub")
		return
	}
	if !validBusinessContractDate(request.StartsOn) || !validBusinessContractDate(request.EndsOn) {
		writeError(w, http.StatusBadRequest, "dates must use YYYY-MM-DD")
		return
	}

	tx, err := h.TxStarter.Begin(r.Context())
	if err != nil {
		writeError(w, http.StatusInternalServerError, "failed to update contract")
		return
	}
	defer tx.Rollback(r.Context())

	var (
		number, subject, status  string
		amount, starts, ends     *string
		attachmentID, notes      *string
	)
	err = tx.QueryRow(r.Context(), `
		SELECT number, subject, amount_rub::text, starts_on::text, ends_on::text, status, attachment_id::text, notes
		FROM business_contract WHERE id = $1 AND business_id = $2 FOR UPDATE
	`, contractID, businessID).Scan(&number, &subject, &amount, &starts, &ends, &status, &attachmentID, &notes)
	if errors.Is(err, pgx.ErrNoRows) {
		writeError(w, http.StatusNotFound, "contract not found")
		return
	}
	if err != nil {
		writeError(w, http.StatusInternalServerError, "failed to update contract")
		return
	}

	mergeText := func(current string, incoming *string) string {
		if incoming == nil {
			return current
		}
		return strings.TrimSpace(*incoming)
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
	number = mergeText(number, request.Number)
	subject = mergeText(subject, request.Subject)
	if request.Status != nil {
		status = *request.Status
	}
	amount = mergeNullable(amount, request.AmountRUB)
	starts = mergeNullable(starts, request.StartsOn)
	ends = mergeNullable(ends, request.EndsOn)
	attachmentID = mergeNullable(attachmentID, request.AttachmentID)
	notes = mergeNullable(notes, request.Notes)

	updatedRaw, err := queryBusinessRowJSON(r.Context(), tx.QueryRow(r.Context(), `
		UPDATE business_contract
		SET number = $3, subject = $4, amount_rub = NULLIF($5, '')::numeric,
		    starts_on = NULLIF($6, '')::date, ends_on = NULLIF($7, '')::date,
		    status = $8, attachment_id = NULLIF($9, '')::uuid, notes = $10, updated_at = now()
		WHERE id = $1 AND business_id = $2
		RETURNING to_jsonb(business_contract)
	`, contractID, businessID, number, subject, stringValue(amount), stringValue(starts),
		stringValue(ends), status, stringValue(attachmentID), notes))
	if err != nil {
		writeError(w, http.StatusInternalServerError, "failed to update contract")
		return
	}
	if err := h.insertBusinessAudit(r.Context(), tx, businessID, userID, "contract.updated", "business_contract", contractID, "", nil, updatedRaw); err != nil {
		writeError(w, http.StatusInternalServerError, "failed to audit contract")
		return
	}
	if err := tx.Commit(r.Context()); err != nil {
		writeError(w, http.StatusInternalServerError, "failed to update contract")
		return
	}
	writeJSON(w, http.StatusOK, updatedRaw)
}

// DeleteBusinessContract removes a contract row. The underlying uploaded file
// (attachment) is left in place — attachments are shared infrastructure and are
// garbage-collected separately.
func (h *Handler) DeleteBusinessContract(w http.ResponseWriter, r *http.Request) {
	businessID, userID, ok := businessRequestIDs(w, r)
	if !ok {
		return
	}
	contractID := chi.URLParam(r, "contractId")
	if _, valid := parseUUIDOrBadRequest(w, contractID, "contract_id"); !valid {
		return
	}
	tx, err := h.TxStarter.Begin(r.Context())
	if err != nil {
		writeError(w, http.StatusInternalServerError, "failed to delete contract")
		return
	}
	defer tx.Rollback(r.Context())

	tag, err := tx.Exec(r.Context(), `DELETE FROM business_contract WHERE id = $1 AND business_id = $2`, contractID, businessID)
	if err != nil {
		writeError(w, http.StatusInternalServerError, "failed to delete contract")
		return
	}
	if tag.RowsAffected() == 0 {
		writeError(w, http.StatusNotFound, "contract not found")
		return
	}
	if err := h.insertBusinessAudit(r.Context(), tx, businessID, userID, "contract.deleted", "business_contract", contractID, "", nil, nil); err != nil {
		writeError(w, http.StatusInternalServerError, "failed to audit contract")
		return
	}
	if err := tx.Commit(r.Context()); err != nil {
		writeError(w, http.StatusInternalServerError, "failed to delete contract")
		return
	}
	writeJSON(w, http.StatusOK, map[string]bool{"ok": true})
}
