package handler

import (
	"errors"
	"fmt"
	"net/http"
	"strings"
	"time"

	"github.com/go-chi/chi/v5"
	"github.com/jackc/pgx/v5"
)

type createBusinessRecurringCostRequest struct {
	Name      string  `json:"name"`
	Category  string  `json:"category"`
	Amount    string  `json:"amount"`
	Currency  string  `json:"currency"`
	Frequency string  `json:"frequency"`
	ChargeDay int16   `json:"charge_day"`
	StartsOn  string  `json:"starts_on"`
	EndsOn    *string `json:"ends_on"`
	Notes     *string `json:"notes"`
}

type updateBusinessRecurringCostRequest struct {
	Name      *string `json:"name"`
	Category  *string `json:"category"`
	Amount    *string `json:"amount"`
	Currency  *string `json:"currency"`
	Frequency *string `json:"frequency"`
	ChargeDay *int16  `json:"charge_day"`
	StartsOn  *string `json:"starts_on"`
	EndsOn    *string `json:"ends_on"`
	Notes     *string `json:"notes"`
	Status    *string `json:"status"`
}

type businessRecurringCostFields struct {
	Name      string
	Category  string
	Amount    string
	Currency  string
	Frequency string
	ChargeDay int16
	StartsOn  string
	EndsOn    string
	Notes     string
	Status    string
}

func normalizeBusinessRecurringMonth(value string) (string, error) {
	parsed, err := time.Parse("2006-01-02", strings.TrimSpace(value))
	if err != nil || parsed.Day() != 1 {
		return "", errors.New("month must use the first day")
	}
	return parsed.Format("2006-01-02"), nil
}

func normalizeBusinessRecurringAmount(value string) (string, error) {
	cents, err := parseBusinessBankMoney(strings.TrimSpace(value))
	if err != nil || cents <= 0 {
		return "", errors.New("amount must be positive")
	}
	return fmt.Sprintf("%d.%02d", cents/100, cents%100), nil
}

func (fields *businessRecurringCostFields) normalizeAndValidate() error {
	fields.Name = strings.TrimSpace(fields.Name)
	fields.Category = strings.TrimSpace(fields.Category)
	fields.Currency = strings.ToUpper(strings.TrimSpace(fields.Currency))
	fields.Frequency = strings.ToLower(strings.TrimSpace(fields.Frequency))
	fields.Notes = strings.TrimSpace(fields.Notes)
	fields.Status = strings.TrimSpace(fields.Status)
	if fields.Name == "" || len(fields.Name) > 200 {
		return errors.New("invalid name")
	}
	if !containsBusinessString([]string{"tax", "bank", "ai", "service", "infrastructure", "contractor", "other"}, fields.Category) {
		return errors.New("invalid category")
	}
	amount, err := normalizeBusinessRecurringAmount(fields.Amount)
	if err != nil {
		return err
	}
	fields.Amount = amount
	if !containsBusinessString([]string{"RUB", "USD"}, fields.Currency) {
		return errors.New("invalid currency")
	}
	if fields.Frequency == "" {
		fields.Frequency = "monthly"
	}
	if !containsBusinessString([]string{"monthly", "yearly"}, fields.Frequency) {
		return errors.New("invalid frequency")
	}
	if fields.ChargeDay < 1 || fields.ChargeDay > 31 {
		return errors.New("invalid charge day")
	}
	startsOn, err := normalizeBusinessRecurringMonth(fields.StartsOn)
	if err != nil {
		return err
	}
	fields.StartsOn = startsOn
	if fields.EndsOn != "" {
		endsOn, err := normalizeBusinessRecurringMonth(fields.EndsOn)
		if err != nil || endsOn < startsOn {
			return errors.New("invalid end month")
		}
		fields.EndsOn = endsOn
	}
	if !containsBusinessString([]string{"active", "paused", "archived"}, fields.Status) {
		return errors.New("invalid status")
	}
	if len(fields.Notes) > 2000 {
		return errors.New("notes are too long")
	}
	return nil
}

func (h *Handler) CreateBusinessRecurringCost(w http.ResponseWriter, r *http.Request) {
	businessID, userID, ok := businessRequestIDs(w, r)
	if !ok {
		return
	}
	var request createBusinessRecurringCostRequest
	if !decodeBusinessJSON(w, r, &request) {
		return
	}
	fields := businessRecurringCostFields{
		Name:      request.Name,
		Category:  request.Category,
		Amount:    request.Amount,
		Currency:  request.Currency,
		Frequency: request.Frequency,
		ChargeDay: request.ChargeDay,
		StartsOn:  request.StartsOn,
		EndsOn:    stringValue(request.EndsOn),
		Notes:     stringValue(request.Notes),
		Status:    "active",
	}
	if err := fields.normalizeAndValidate(); err != nil {
		writeError(w, http.StatusBadRequest, "recurring cost fields are invalid")
		return
	}

	tx, err := h.TxStarter.Begin(r.Context())
	if err != nil {
		writeError(w, http.StatusInternalServerError, "failed to create recurring cost")
		return
	}
	defer tx.Rollback(r.Context())
	var id string
	err = tx.QueryRow(r.Context(), `
		INSERT INTO business_recurring_cost (
			business_id, name, category, amount, currency, frequency, charge_day,
			starts_on, ends_on, notes, status, created_by
		) VALUES ($1, $2, $3, $4::numeric, $5, $6, $7, $8::date, NULLIF($9, '')::date,
		          NULLIF($10, ''), 'active', $11)
		RETURNING id::text
	`, businessID, fields.Name, fields.Category, fields.Amount, fields.Currency,
		fields.Frequency, fields.ChargeDay, fields.StartsOn, fields.EndsOn, fields.Notes, userID).Scan(&id)
	if err != nil {
		writeError(w, http.StatusInternalServerError, "failed to create recurring cost")
		return
	}
	afterData, err := queryBusinessRowJSON(r.Context(), tx.QueryRow(r.Context(),
		`SELECT to_jsonb(q) FROM (SELECT * FROM business_recurring_cost WHERE business_id = $1 AND id = $2) q`, businessID, id))
	if err != nil || h.insertBusinessAudit(r.Context(), tx, businessID, userID,
		"recurring_cost.create", "business_recurring_cost", id, "recurring company cost", nil, afterData) != nil {
		writeError(w, http.StatusInternalServerError, "failed to create recurring cost")
		return
	}
	if err := tx.Commit(r.Context()); err != nil {
		writeError(w, http.StatusInternalServerError, "failed to create recurring cost")
		return
	}
	writeJSON(w, http.StatusCreated, map[string]string{"id": id})
}

func (h *Handler) UpdateBusinessRecurringCost(w http.ResponseWriter, r *http.Request) {
	businessID, userID, ok := businessRequestIDs(w, r)
	if !ok {
		return
	}
	costID := chi.URLParam(r, "costId")
	if _, ok := parseUUIDOrBadRequest(w, costID, "cost_id"); !ok {
		return
	}
	var request updateBusinessRecurringCostRequest
	if !decodeBusinessJSON(w, r, &request) {
		return
	}

	tx, err := h.TxStarter.Begin(r.Context())
	if err != nil {
		writeError(w, http.StatusInternalServerError, "failed to update recurring cost")
		return
	}
	defer tx.Rollback(r.Context())
	var fields businessRecurringCostFields
	var endsOn, notes *string
	err = tx.QueryRow(r.Context(), `
		SELECT name, category, amount::text, currency, frequency, charge_day,
		       starts_on::text, ends_on::text, notes, status
		FROM business_recurring_cost
		WHERE business_id = $1 AND id = $2
		FOR UPDATE
	`, businessID, costID).Scan(&fields.Name, &fields.Category, &fields.Amount,
		&fields.Currency, &fields.Frequency, &fields.ChargeDay, &fields.StartsOn, &endsOn, &notes, &fields.Status)
	if errors.Is(err, pgx.ErrNoRows) {
		writeError(w, http.StatusNotFound, "recurring cost not found")
		return
	}
	if err != nil {
		writeError(w, http.StatusInternalServerError, "failed to update recurring cost")
		return
	}
	fields.EndsOn = stringValue(endsOn)
	fields.Notes = stringValue(notes)
	beforeData, err := queryBusinessRowJSON(r.Context(), tx.QueryRow(r.Context(),
		`SELECT to_jsonb(q) FROM (SELECT * FROM business_recurring_cost WHERE business_id = $1 AND id = $2) q`, businessID, costID))
	if err != nil {
		writeError(w, http.StatusInternalServerError, "failed to update recurring cost")
		return
	}

	if request.Name != nil {
		fields.Name = *request.Name
	}
	if request.Category != nil {
		fields.Category = *request.Category
	}
	if request.Amount != nil {
		fields.Amount = *request.Amount
	}
	if request.Currency != nil {
		fields.Currency = *request.Currency
	}
	if request.Frequency != nil {
		fields.Frequency = *request.Frequency
	}
	if request.ChargeDay != nil {
		fields.ChargeDay = *request.ChargeDay
	}
	if request.StartsOn != nil {
		fields.StartsOn = *request.StartsOn
	}
	if request.EndsOn != nil {
		fields.EndsOn = *request.EndsOn
	}
	if request.Notes != nil {
		fields.Notes = *request.Notes
	}
	if request.Status != nil {
		fields.Status = *request.Status
	}
	if err := fields.normalizeAndValidate(); err != nil {
		writeError(w, http.StatusBadRequest, "recurring cost fields are invalid")
		return
	}

	_, err = tx.Exec(r.Context(), `
		UPDATE business_recurring_cost
		SET name = $3, category = $4, amount = $5::numeric, currency = $6,
		    frequency = $7, charge_day = $8, starts_on = $9::date, ends_on = NULLIF($10, '')::date,
		    notes = NULLIF($11, ''), status = $12,
		    archived_at = CASE WHEN $12 = 'archived' THEN COALESCE(archived_at, now()) ELSE NULL END,
		    updated_at = now()
		WHERE business_id = $1 AND id = $2
	`, businessID, costID, fields.Name, fields.Category, fields.Amount, fields.Currency,
		fields.Frequency, fields.ChargeDay, fields.StartsOn, fields.EndsOn, fields.Notes, fields.Status)
	if err != nil {
		writeError(w, http.StatusInternalServerError, "failed to update recurring cost")
		return
	}
	afterData, err := queryBusinessRowJSON(r.Context(), tx.QueryRow(r.Context(),
		`SELECT to_jsonb(q) FROM (SELECT * FROM business_recurring_cost WHERE business_id = $1 AND id = $2) q`, businessID, costID))
	if err != nil || h.insertBusinessAudit(r.Context(), tx, businessID, userID,
		"recurring_cost.update", "business_recurring_cost", costID, "recurring company cost", beforeData, afterData) != nil {
		writeError(w, http.StatusInternalServerError, "failed to update recurring cost")
		return
	}
	if err := tx.Commit(r.Context()); err != nil {
		writeError(w, http.StatusInternalServerError, "failed to update recurring cost")
		return
	}
	writeJSON(w, http.StatusOK, map[string]string{"id": costID})
}
