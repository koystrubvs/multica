package handler

import (
	"bytes"
	"context"
	"crypto/sha256"
	"encoding/csv"
	"encoding/hex"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"log/slog"
	"math"
	"net/http"
	"path/filepath"
	"sort"
	"strconv"
	"strings"
	"time"
	"unicode"

	"github.com/go-chi/chi/v5"
	"github.com/jackc/pgx/v5"
	"github.com/xuri/excelize/v2"
)

const businessBankFileLimit = 20 << 20

type businessBankRow struct {
	Account      string
	BookedOn     time.Time
	Counterparty string
	INN          string
	Inflow       int64
	Outflow      int64
	Purpose      string
	Raw          map[string]string
}

type businessBankImportResult struct {
	BatchID         string `json:"batch_id"`
	Status          string `json:"status"`
	RowsTotal       int    `json:"rows_total"`
	RowsInserted    int    `json:"rows_inserted"`
	RowsDuplicate   int    `json:"rows_duplicate"`
	RowsInvalid     int    `json:"rows_invalid"`
	AlreadyImported bool   `json:"already_imported"`
}

func (h *Handler) ImportBusinessBankFile(w http.ResponseWriter, r *http.Request) {
	businessID, userID, ok := businessRequestIDs(w, r)
	if !ok {
		return
	}
	r.Body = http.MaxBytesReader(w, r.Body, businessBankFileLimit+(1<<20))
	if err := r.ParseMultipartForm(businessBankFileLimit); err != nil {
		writeError(w, http.StatusBadRequest, "bank file must be a multipart upload up to 20 MB")
		return
	}
	file, header, err := r.FormFile("file")
	if err != nil {
		writeError(w, http.StatusBadRequest, "file is required")
		return
	}
	defer file.Close()
	content, err := io.ReadAll(io.LimitReader(file, businessBankFileLimit+1))
	if err != nil || len(content) == 0 || len(content) > businessBankFileLimit {
		writeError(w, http.StatusBadRequest, "bank file is empty, unreadable, or too large")
		return
	}

	ext := strings.ToLower(filepath.Ext(header.Filename))
	var rows []businessBankRow
	var source string
	switch ext {
	case ".csv":
		source = "modulbank_csv"
		rows, err = parseBusinessBankCSV(content)
	case ".xlsx":
		source = "modulbank_xlsx"
		rows, err = parseBusinessBankXLSX(content)
	default:
		err = fmt.Errorf("only .csv and .xlsx are supported")
	}
	if err != nil {
		writeError(w, http.StatusBadRequest, err.Error())
		return
	}

	digest := sha256.Sum256(content)
	sha := hex.EncodeToString(digest[:])
	result, err := h.persistBusinessBankImport(r.Context(), businessID, userID, source, header.Filename, sha, rows)
	if err != nil {
		slog.Error("business bank import failed", "business_id", businessID, "filename", header.Filename, "error", err)
		writeError(w, http.StatusInternalServerError, "failed to import bank file")
		return
	}
	status := http.StatusCreated
	if result.AlreadyImported {
		status = http.StatusOK
	}
	writeJSON(w, status, result)
}

func parseBusinessBankCSV(content []byte) ([]businessBankRow, error) {
	reader := csv.NewReader(bytes.NewReader(bytes.TrimPrefix(content, []byte{0xef, 0xbb, 0xbf})))
	reader.Comma = ';'
	reader.FieldsPerRecord = -1
	records, err := reader.ReadAll()
	if err != nil {
		return nil, fmt.Errorf("invalid CSV file")
	}
	return parseBusinessBankRecords(records)
}

func parseBusinessBankXLSX(content []byte) ([]businessBankRow, error) {
	book, err := excelize.OpenReader(bytes.NewReader(content))
	if err != nil {
		return nil, fmt.Errorf("invalid XLSX file")
	}
	defer book.Close()
	sheets := book.GetSheetList()
	if len(sheets) == 0 {
		return nil, fmt.Errorf("XLSX file has no worksheets")
	}
	records, err := book.GetRows(sheets[0])
	if err != nil {
		return nil, fmt.Errorf("failed to read XLSX rows")
	}
	return parseBusinessBankRecords(records)
}

func parseBusinessBankRecords(records [][]string) ([]businessBankRow, error) {
	if len(records) < 2 {
		return nil, fmt.Errorf("bank file has no transactions")
	}
	headers := make([]string, len(records[0]))
	for i, value := range records[0] {
		headers[i] = normalizeBusinessBankHeader(value)
	}
	columns := map[string]int{}
	for i, header := range headers {
		if canonical := canonicalBusinessBankHeader(header); canonical != "" {
			columns[canonical] = i
		}
	}
	for _, required := range []string{"date", "counterparty", "inflow", "outflow"} {
		if _, ok := columns[required]; !ok {
			return nil, fmt.Errorf("bank file is missing the %s column", required)
		}
	}
	rows := make([]businessBankRow, 0, len(records)-1)
	for _, record := range records[1:] {
		if businessBankRecordBlank(record) {
			continue
		}
		raw := map[string]string{}
		for i, value := range record {
			if i < len(headers) && headers[i] != "" {
				raw[headers[i]] = strings.TrimSpace(value)
			}
		}
		bookedOn, dateErr := parseBusinessBankDate(businessBankCell(record, columns["date"]))
		inflow, inErr := parseBusinessBankMoney(businessBankCell(record, columns["inflow"]))
		outflow, outErr := parseBusinessBankMoney(businessBankCell(record, columns["outflow"]))
		if dateErr != nil || inErr != nil || outErr != nil || (inflow <= 0) == (outflow <= 0) {
			rows = append(rows, businessBankRow{Raw: raw})
			continue
		}
		rows = append(rows, businessBankRow{
			Account:      businessBankCell(record, columns["account"]),
			BookedOn:     bookedOn,
			Counterparty: strings.TrimSpace(businessBankCell(record, columns["counterparty"])),
			INN:          digitsOnly(businessBankCell(record, columns["inn"])),
			Inflow:       inflow,
			Outflow:      outflow,
			Purpose:      strings.TrimSpace(businessBankCell(record, columns["purpose"])),
			Raw:          raw,
		})
	}
	return rows, nil
}

func (h *Handler) persistBusinessBankImport(ctx context.Context, businessID, userID, source, filename, sha string, rows []businessBankRow) (businessBankImportResult, error) {
	result := businessBankImportResult{Status: "completed", RowsTotal: len(rows)}
	var existingStatus string
	err := h.DB.QueryRow(ctx, `
		SELECT id::text, status, rows_total, rows_inserted, rows_duplicate, rows_invalid
		FROM business_bank_import_batch
		WHERE business_id = $1 AND file_sha256 = $2 AND voided_at IS NULL
	`, businessID, sha).Scan(&result.BatchID, &existingStatus, &result.RowsTotal, &result.RowsInserted, &result.RowsDuplicate, &result.RowsInvalid)
	if err == nil {
		result.Status = existingStatus
		result.AlreadyImported = true
		return result, nil
	}
	if err != pgx.ErrNoRows {
		return result, err
	}

	tx, err := h.TxStarter.Begin(ctx)
	if err != nil {
		return result, err
	}
	defer tx.Rollback(ctx)
	metadata, _ := json.Marshal(map[string]any{"parser": "business_bank_v1", "rows": len(rows)})
	if err := tx.QueryRow(ctx, `
		INSERT INTO business_bank_import_batch (
			business_id, source, filename, file_sha256, idempotency_key, status,
			rows_total, imported_by, raw_metadata
		) VALUES ($1,$2,$3,$4,$5,'processing',$6,$7,$8)
		RETURNING id::text
	`, businessID, source, strings.TrimSpace(filename), sha, "bank-file:"+sha, len(rows), userID, metadata).Scan(&result.BatchID); err != nil {
		return result, err
	}

	for _, row := range rows {
		if row.BookedOn.IsZero() || row.Counterparty == "" || (row.Inflow <= 0) == (row.Outflow <= 0) {
			result.RowsInvalid++
			continue
		}
		direction, amount := "inbound", row.Inflow
		if row.Outflow > 0 {
			direction, amount = "outbound", row.Outflow
		}
		classification, confidence := classifyBusinessBankRow(ctx, tx, businessID, row, direction)
		dedup := businessBankDedupKey(row, direction, amount)
		raw, _ := json.Marshal(row.Raw)
		var transactionID string
		insertErr := tx.QueryRow(ctx, `
			INSERT INTO business_bank_transaction (
				business_id, import_batch_id, source, dedup_key, booked_on, direction,
				amount_rub, account_mask, counterparty_name, counterparty_inn, purpose,
				classification, classification_confidence, raw_payload
			) VALUES ($1,$2,$3,$4,$5,$6,$7::numeric/100,$8,$9,NULLIF($10,''),NULLIF($11,''),$12,$13,$14)
			ON CONFLICT (business_id, dedup_key) WHERE voided_at IS NULL DO NOTHING
			RETURNING id::text
		`, businessID, result.BatchID, source, dedup, dateOnly(row.BookedOn), direction, amount,
			maskBusinessAccount(row.Account), row.Counterparty, row.INN, row.Purpose, classification, confidence, raw).Scan(&transactionID)
		if errors.Is(insertErr, pgx.ErrNoRows) {
			result.RowsDuplicate++
			continue
		}
		if insertErr != nil {
			return result, insertErr
		}
		result.RowsInserted++
		if err := autoMatchBankTransactionToReceivable(ctx, tx, businessID, userID, transactionID); err != nil {
			return result, err
		}
	}
	if _, err := tx.Exec(ctx, `
		UPDATE business_bank_import_batch
		SET status='completed', rows_inserted=$2, rows_duplicate=$3, rows_invalid=$4,
		    completed_at=now(), updated_at=now()
		WHERE business_id=$1 AND id=$5
	`, businessID, result.RowsInserted, result.RowsDuplicate, result.RowsInvalid, result.BatchID); err != nil {
		return result, err
	}
	resultJSON, _ := json.Marshal(result)
	if err := h.insertBusinessAudit(ctx, tx, businessID, userID, "bank.import", "business_bank_import_batch", result.BatchID, "bank file import", nil, resultJSON); err != nil {
		return result, err
	}
	if err := tx.Commit(ctx); err != nil {
		return result, err
	}
	return result, nil
}

func classifyBusinessBankRow(ctx context.Context, tx pgx.Tx, businessID string, row businessBankRow, direction string) (string, string) {
	vitmaxINNs := map[string]bool{"7724010662": true, "5611054231": true, "9203005794": true, "6674136513": true}
	if vitmaxINNs[row.INN] {
		return "vitmax_transit", "confirmed"
	}
	lower := strings.ToLower(row.Counterparty + " " + row.Purpose)
	if row.INN == "662607078800" || strings.Contains(lower, "между своими счет") || strings.Contains(lower, "перевод собственных средств") {
		return "transfer", "confirmed"
	}
	if direction == "outbound" && (strings.Contains(lower, "налог") || strings.Contains(lower, "фнс") || strings.Contains(lower, "страхов") || strings.Contains(lower, "взнос")) {
		return "tax", "suggested"
	}
	if direction == "outbound" && (strings.Contains(lower, "комисси") || strings.Contains(lower, "обслуживан")) {
		return "service", "suggested"
	}
	var classification, confidence string
	err := tx.QueryRow(ctx, `
		SELECT classification, confidence
		FROM business_counterparty_classification
		WHERE business_id=$1 AND source='bank' AND confidence='confirmed'
		  AND (external_id=$2 OR (NULLIF($3,'') IS NOT NULL AND inn=$3))
		ORDER BY updated_at DESC LIMIT 1
	`, businessID, businessBankCounterpartyKey(row), row.INN).Scan(&classification, &confidence)
	if err == nil {
		if mapped, mappedConfidence, ok := businessCounterpartyTransactionClassification(classification, direction); ok {
			return mapped, mappedConfidence
		}
	}
	if direction == "inbound" {
		var exists bool
		_ = tx.QueryRow(ctx, `
			SELECT EXISTS(
				SELECT 1 FROM business_client_payer
				WHERE business_id=$1 AND status='active'
				  AND (lower(name)=lower($2) OR (NULLIF($3,'') IS NOT NULL AND inn=$3))
			)
		`, businessID, row.Counterparty, row.INN).Scan(&exists)
		if exists {
			return "client_income", "confirmed"
		}
	}
	return "unknown", "unresolved"
}

type createBusinessBankTransactionRequest struct {
	BookedOn         string  `json:"booked_on"`
	Direction        string  `json:"direction"`
	AmountRUB        string  `json:"amount_rub"`
	CounterpartyName string  `json:"counterparty_name"`
	CounterpartyINN  *string `json:"counterparty_inn"`
	Purpose          *string `json:"purpose"`
	Classification   string  `json:"classification"`
	IdempotencyKey   string  `json:"idempotency_key"`
}

func (h *Handler) CreateBusinessBankTransaction(w http.ResponseWriter, r *http.Request) {
	businessID, userID, ok := businessRequestIDs(w, r)
	if !ok {
		return
	}
	var request createBusinessBankTransactionRequest
	if !decodeBusinessJSON(w, r, &request) {
		return
	}
	bookedOn, err := time.Parse("2006-01-02", request.BookedOn)
	if err != nil || !containsBusinessString([]string{"inbound", "outbound"}, request.Direction) || strings.TrimSpace(request.CounterpartyName) == "" {
		writeError(w, http.StatusBadRequest, "booked_on, direction and counterparty_name are invalid")
		return
	}
	amount, err := parseBusinessBankMoney(request.AmountRUB)
	if err != nil || amount <= 0 || strings.TrimSpace(request.IdempotencyKey) == "" {
		writeError(w, http.StatusBadRequest, "positive amount_rub and idempotency_key are required")
		return
	}
	if request.Classification == "" {
		request.Classification = "unknown"
	}
	if !containsBusinessString([]string{"client_income", "payroll", "tax", "service", "transfer", "owner_draw", "vitmax_transit", "unknown"}, request.Classification) {
		writeError(w, http.StatusBadRequest, "classification is invalid")
		return
	}
	tx, err := h.TxStarter.Begin(r.Context())
	if err != nil {
		writeError(w, 500, "failed to create transaction")
		return
	}
	defer tx.Rollback(r.Context())
	var id string
	err = tx.QueryRow(r.Context(), `
		INSERT INTO business_bank_transaction (
			business_id, source, dedup_key, booked_on, direction, amount_rub,
			counterparty_name, counterparty_inn, purpose, classification, classification_confidence,
			raw_payload
		) VALUES ($1,'manual',$2,$3,$4,$5::numeric/100,$6,NULLIF($7,''),NULLIF($8,''),$9,'confirmed',jsonb_build_object('created_by',$10::text))
		ON CONFLICT (business_id,dedup_key) WHERE voided_at IS NULL DO UPDATE SET updated_at=now()
		RETURNING id::text
	`, businessID, request.IdempotencyKey, dateOnly(bookedOn), request.Direction, amount,
		strings.TrimSpace(request.CounterpartyName), stringValue(request.CounterpartyINN), stringValue(request.Purpose), request.Classification, userID).Scan(&id)
	if err != nil {
		writeError(w, 500, "failed to create transaction")
		return
	}
	if err := h.insertBusinessAudit(r.Context(), tx, businessID, userID, "bank.transaction.create", "business_bank_transaction", id, "manual bank transaction", nil, nil); err != nil {
		writeError(w, 500, "failed to create transaction")
		return
	}
	if err := autoMatchBankTransactionToReceivable(r.Context(), tx, businessID, userID, id); err != nil {
		writeError(w, 500, "failed to auto-match transaction")
		return
	}
	if err := tx.Commit(r.Context()); err != nil {
		writeError(w, 500, "failed to create transaction")
		return
	}
	writeJSON(w, http.StatusCreated, map[string]string{"id": id})
}

type classifyBusinessTransactionRequest struct {
	Classification string `json:"classification"`
	Confidence     string `json:"confidence"`
	Reason         string `json:"reason"`
}

func (h *Handler) ClassifyBusinessBankTransaction(w http.ResponseWriter, r *http.Request) {
	businessID, userID, ok := businessRequestIDs(w, r)
	if !ok {
		return
	}
	transactionID := chi.URLParam(r, "transactionId")
	if _, ok := parseUUIDOrBadRequest(w, transactionID, "transaction_id"); !ok {
		return
	}
	var request classifyBusinessTransactionRequest
	if !decodeBusinessJSON(w, r, &request) {
		return
	}
	if !containsBusinessString([]string{"client_income", "payroll", "tax", "service", "transfer", "owner_draw", "vitmax_transit", "unknown"}, request.Classification) ||
		!containsBusinessString([]string{"confirmed", "suggested", "unresolved"}, request.Confidence) {
		writeError(w, 400, "classification or confidence is invalid")
		return
	}
	tx, err := h.TxStarter.Begin(r.Context())
	if err != nil {
		writeError(w, 500, "failed to classify transaction")
		return
	}
	defer tx.Rollback(r.Context())
	command, err := tx.Exec(r.Context(), `UPDATE business_bank_transaction SET classification=$3,classification_confidence=$4,updated_at=now() WHERE business_id=$1 AND id=$2 AND voided_at IS NULL`, businessID, transactionID, request.Classification, request.Confidence)
	if err != nil {
		writeError(w, 500, "failed to classify transaction")
		return
	}
	if command.RowsAffected() == 0 {
		writeError(w, 404, "transaction not found")
		return
	}
	if err := h.insertBusinessAudit(r.Context(), tx, businessID, userID, "bank.transaction.classify", "business_bank_transaction", transactionID, request.Reason, nil, nil); err != nil {
		writeError(w, 500, "failed to classify transaction")
		return
	}
	if err := tx.Commit(r.Context()); err != nil {
		writeError(w, 500, "failed to classify transaction")
		return
	}
	w.WriteHeader(http.StatusNoContent)
}

type resolveBusinessBankCounterpartyRequest struct {
	TransactionID  string  `json:"transaction_id"`
	Classification string  `json:"classification"`
	ClientID       *string `json:"client_id"`
	WorkerID       *string `json:"worker_id"`
	Reason         string  `json:"reason"`
}

func businessCounterpartyTransactionClassification(classification, direction string) (string, string, bool) {
	switch classification {
	case "client_payer":
		if direction == "inbound" {
			return "client_income", "confirmed", true
		}
	case "worker_payee":
		if direction == "outbound" {
			return "payroll", "confirmed", true
		}
	case "vendor":
		return "service", "confirmed", true
	case "transit", "ignored":
		return "transfer", "confirmed", true
	}
	return "", "", false
}

func (h *Handler) ResolveBusinessBankCounterparty(w http.ResponseWriter, r *http.Request) {
	businessID, userID, ok := businessRequestIDs(w, r)
	if !ok {
		return
	}
	var request resolveBusinessBankCounterpartyRequest
	if !decodeBusinessJSON(w, r, &request) {
		return
	}
	if _, valid := parseUUIDOrBadRequest(w, request.TransactionID, "transaction_id"); !valid {
		return
	}
	if !containsBusinessString([]string{"client_payer", "worker_payee", "vendor", "transit", "ignored"}, request.Classification) {
		writeError(w, http.StatusBadRequest, "counterparty classification is invalid")
		return
	}
	if request.Classification == "client_payer" && (request.ClientID == nil || *request.ClientID == "") {
		writeError(w, http.StatusBadRequest, "client_id is required for client_payer")
		return
	}
	if request.Classification == "worker_payee" && (request.WorkerID == nil || *request.WorkerID == "") {
		writeError(w, http.StatusBadRequest, "worker_id is required for worker_payee")
		return
	}
	if request.Classification != "client_payer" {
		request.ClientID = nil
	}
	if request.Classification != "worker_payee" {
		request.WorkerID = nil
	}
	if request.Reason == "" {
		request.Reason = "manual bank counterparty resolution"
	}

	tx, err := h.TxStarter.Begin(r.Context())
	if err != nil {
		writeError(w, http.StatusInternalServerError, "failed to start transaction")
		return
	}
	defer tx.Rollback(r.Context())

	var counterpartyName, counterpartyINN, direction string
	err = tx.QueryRow(r.Context(), `
		SELECT counterparty_name, COALESCE(counterparty_inn, ''), direction
		FROM business_bank_transaction
		WHERE business_id=$1 AND id=$2 AND voided_at IS NULL
		FOR UPDATE
	`, businessID, request.TransactionID).Scan(&counterpartyName, &counterpartyINN, &direction)
	if err == pgx.ErrNoRows {
		writeError(w, http.StatusNotFound, "transaction not found")
		return
	}
	if err != nil {
		writeError(w, http.StatusInternalServerError, "failed to load transaction")
		return
	}
	transactionClassification, transactionConfidence, applicable := businessCounterpartyTransactionClassification(request.Classification, direction)
	if !applicable {
		writeError(w, http.StatusBadRequest, "counterparty classification does not match transaction direction")
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

	externalID := businessBankCounterpartyKey(businessBankRow{Counterparty: counterpartyName, INN: counterpartyINN})
	var before json.RawMessage
	before, err = queryBusinessRowJSON(r.Context(), tx.QueryRow(r.Context(), `
		SELECT to_jsonb(c) FROM business_counterparty_classification c
		WHERE business_id=$1 AND source='bank' AND external_id=$2
		FOR UPDATE
	`, businessID, externalID))
	if err != nil && err != pgx.ErrNoRows {
		writeError(w, http.StatusInternalServerError, "failed to load counterparty rule")
		return
	}
	if err == pgx.ErrNoRows {
		before = nil
	}

	var ruleID string
	var after json.RawMessage
	err = tx.QueryRow(r.Context(), `
		INSERT INTO business_counterparty_classification (
			business_id, source, external_id, name, inn, classification,
			client_id, worker_id, confidence, reason, classified_by, classified_at
		) VALUES ($1, 'bank', $2, $3, NULLIF($4, ''), $5, NULLIF($6, '')::uuid,
			NULLIF($7, '')::uuid, 'confirmed', $8, $9, now())
		ON CONFLICT (business_id, source, external_id) DO UPDATE SET
			name=EXCLUDED.name, inn=EXCLUDED.inn, classification=EXCLUDED.classification,
			client_id=EXCLUDED.client_id, worker_id=EXCLUDED.worker_id,
			confidence='confirmed', reason=EXCLUDED.reason,
			classified_by=EXCLUDED.classified_by, classified_at=now(), updated_at=now()
		RETURNING id::text, to_jsonb(business_counterparty_classification)
	`, businessID, externalID, strings.TrimSpace(counterpartyName), counterpartyINN,
		request.Classification, stringValue(request.ClientID), stringValue(request.WorkerID),
		request.Reason, userID).Scan(&ruleID, &after)
	if err != nil {
		writeError(w, http.StatusBadRequest, "failed to save counterparty rule")
		return
	}

	command, err := tx.Exec(r.Context(), `
		UPDATE business_bank_transaction
		SET classification=$3, classification_confidence=$4, updated_at=now()
		WHERE business_id=$1 AND voided_at IS NULL AND classification='unknown'
		  AND direction=$5
		  AND (
			(NULLIF($6, '') IS NOT NULL AND counterparty_inn=$6)
			OR (NULLIF($6, '') IS NULL AND COALESCE(counterparty_inn, '')='' AND lower(btrim(counterparty_name))=lower(btrim($2)))
		  )
	`, businessID, counterpartyName, transactionClassification, transactionConfidence, direction, counterpartyINN)
	if err != nil {
		writeError(w, http.StatusInternalServerError, "failed to classify counterparty transactions")
		return
	}
	response := map[string]any{"rule": json.RawMessage(after), "updated_transactions": command.RowsAffected()}
	auditAfter, _ := json.Marshal(response)
	if err := h.insertBusinessAudit(r.Context(), tx, businessID, userID, "bank.counterparty.resolve", "business_counterparty_classification", ruleID, request.Reason, before, auditAfter); err != nil {
		writeError(w, http.StatusInternalServerError, "failed to audit counterparty resolution")
		return
	}
	if err := tx.Commit(r.Context()); err != nil {
		writeError(w, http.StatusInternalServerError, "failed to resolve counterparty")
		return
	}
	writeJSON(w, http.StatusOK, response)
}

type createBusinessMatchRequest struct {
	TargetType     string  `json:"target_type"`
	TargetID       string  `json:"target_id"`
	AmountRUB      string  `json:"amount_rub"`
	Status         string  `json:"status"`
	IdempotencyKey string  `json:"idempotency_key"`
	Notes          *string `json:"notes"`
}

func (h *Handler) CreateBusinessTransactionMatch(w http.ResponseWriter, r *http.Request) {
	businessID, userID, ok := businessRequestIDs(w, r)
	if !ok {
		return
	}
	transactionID := chi.URLParam(r, "transactionId")
	if _, ok := parseUUIDOrBadRequest(w, transactionID, "transaction_id"); !ok {
		return
	}
	var request createBusinessMatchRequest
	if !decodeBusinessJSON(w, r, &request) {
		return
	}
	if _, ok := parseUUIDOrBadRequest(w, request.TargetID, "target_id"); !ok {
		return
	}
	amount, err := parseBusinessBankMoney(request.AmountRUB)
	if err != nil || amount <= 0 || !containsBusinessString([]string{"receivable", "billing_period", "payout", "company_cost"}, request.TargetType) || strings.TrimSpace(request.IdempotencyKey) == "" {
		writeError(w, 400, "match fields are invalid")
		return
	}
	if request.Status == "" {
		request.Status = "confirmed"
	}
	if !containsBusinessString([]string{"suggested", "confirmed"}, request.Status) {
		writeError(w, 400, "status is invalid")
		return
	}
	tx, err := h.TxStarter.Begin(r.Context())
	if err != nil {
		writeError(w, 500, "failed to create match")
		return
	}
	defer tx.Rollback(r.Context())
	var transactionAmount int64
	var direction string
	if err := tx.QueryRow(r.Context(), `SELECT round(amount_rub*100)::bigint,direction FROM business_bank_transaction WHERE business_id=$1 AND id=$2 AND voided_at IS NULL FOR UPDATE`, businessID, transactionID).Scan(&transactionAmount, &direction); err != nil {
		writeError(w, 404, "transaction not found")
		return
	}
	if (containsBusinessString([]string{"receivable", "billing_period"}, request.TargetType) && direction != "inbound") ||
		(containsBusinessString([]string{"payout", "company_cost"}, request.TargetType) && direction != "outbound") {
		writeError(w, 409, "transaction direction does not match the target")
		return
	}
	var matched int64
	if err := tx.QueryRow(r.Context(), `SELECT COALESCE(round(sum(amount_rub)*100),0)::bigint FROM business_transaction_match WHERE business_id=$1 AND transaction_id=$2 AND status IN ('suggested','confirmed')`, businessID, transactionID).Scan(&matched); err != nil || matched+amount > transactionAmount {
		writeError(w, 409, "match total exceeds transaction amount")
		return
	}
	if !businessMatchTargetExists(r.Context(), tx, businessID, request.TargetType, request.TargetID) {
		writeError(w, 400, "match target is outside this business")
		return
	}
	var id string
	confirmed := request.Status == "confirmed"
	err = tx.QueryRow(r.Context(), `INSERT INTO business_transaction_match (business_id,transaction_id,target_type,target_id,amount_rub,status,suggested_by,confirmed_by,confirmed_at,idempotency_key,notes) VALUES ($1,$2,$3,$4,$5::numeric/100,$6,$7,CASE WHEN $8 THEN $7::uuid END,CASE WHEN $8 THEN now() END,$9,NULLIF($10,'')) ON CONFLICT (business_id,idempotency_key) DO UPDATE SET updated_at=now() RETURNING id::text`, businessID, transactionID, request.TargetType, request.TargetID, amount, request.Status, userID, confirmed, request.IdempotencyKey, stringValue(request.Notes)).Scan(&id)
	if err != nil {
		writeError(w, 500, "failed to create match")
		return
	}
	if confirmed && request.TargetType == "receivable" {
		if err := recalculateBusinessReceivableFunding(r.Context(), tx, businessID, request.TargetID); err != nil {
			writeError(w, 500, "failed to update receivable funding")
			return
		}
	}
	if confirmed && request.TargetType == "payout" {
		if err := recalculateBusinessPayoutPayment(r.Context(), tx, businessID, request.TargetID); err != nil {
			writeError(w, 500, "failed to update payout payment")
			return
		}
	}
	if err := h.insertBusinessAudit(r.Context(), tx, businessID, userID, "bank.match.create", "business_transaction_match", id, "bank reconciliation", nil, nil); err != nil {
		writeError(w, 500, "failed to create match")
		return
	}
	if err := tx.Commit(r.Context()); err != nil {
		writeError(w, 500, "failed to create match")
		return
	}
	writeJSON(w, http.StatusCreated, map[string]string{"id": id})
}

// A receivable only reaches 'paid' through a confirmed bank match, which leaves
// money that never touches the business account — personal card, cash — with no
// way to be recorded. This settles a receivable the owner points at directly:
// the receipt is written into the money ledger and matched in one transaction.
// Auto-match is deliberately bypassed, because the target is chosen rather than
// guessed from the amount.
type recordBusinessReceivablePaymentRequest struct {
	AmountRUB      string  `json:"amount_rub"`
	ReceivedOn     string  `json:"received_on"`
	PaymentChannel string  `json:"payment_channel"`
	Notes          *string `json:"notes"`
	IdempotencyKey string  `json:"idempotency_key"`
}

// Settlement guards stay pure so the rules that protect a receivable from being
// overpaid are testable without a database.
func receivablePaymentBlocker(status string, remaining, amount int64) (int, string) {
	switch {
	case containsBusinessString([]string{"skipped", "written_off"}, status):
		return http.StatusConflict, "skipped and written-off receivables cannot take payments"
	case remaining <= 0:
		return http.StatusConflict, "receivable is already fully paid"
	case amount > remaining:
		return http.StatusConflict, "amount exceeds the outstanding balance"
	}
	return 0, ""
}

// The ledger records the channel the money actually arrived through, so a card
// transfer is never reported as bank turnover.
func receivablePaymentSource(channel string) string {
	if channel == "personal_card" {
		return "personal_card"
	}
	return "manual"
}

func receivablePaymentPurpose(notes, agreementName, periodKey string) string {
	if trimmed := strings.TrimSpace(notes); trimmed != "" {
		return trimmed
	}
	if trimmed := strings.TrimSpace(agreementName); trimmed != "" {
		return trimmed + " · " + periodKey
	}
	return periodKey
}

func (h *Handler) RecordBusinessReceivablePayment(w http.ResponseWriter, r *http.Request) {
	businessID, userID, ok := businessRequestIDs(w, r)
	if !ok {
		return
	}
	receivableID := chi.URLParam(r, "receivableId")
	if _, ok := parseUUIDOrBadRequest(w, receivableID, "receivable_id"); !ok {
		return
	}
	var request recordBusinessReceivablePaymentRequest
	if !decodeBusinessJSON(w, r, &request) {
		return
	}
	receivedOn, err := time.Parse("2006-01-02", strings.TrimSpace(request.ReceivedOn))
	if err != nil {
		writeError(w, http.StatusBadRequest, "received_on must use YYYY-MM-DD")
		return
	}
	amount, err := parseBusinessBankMoney(request.AmountRUB)
	if err != nil || amount <= 0 {
		writeError(w, http.StatusBadRequest, "amount_rub must be positive")
		return
	}
	if strings.TrimSpace(request.IdempotencyKey) == "" {
		writeError(w, http.StatusBadRequest, "idempotency_key is required")
		return
	}
	if request.PaymentChannel == "" {
		request.PaymentChannel = "bank"
	}
	if !containsBusinessString([]string{"bank", "personal_card", "cash", "other"}, request.PaymentChannel) {
		writeError(w, http.StatusBadRequest, "payment_channel is invalid")
		return
	}

	tx, err := h.TxStarter.Begin(r.Context())
	if err != nil {
		writeError(w, http.StatusInternalServerError, "failed to record payment")
		return
	}
	defer tx.Rollback(r.Context())

	var (
		status, periodKey, clientName string
		agreementName                 *string
		remaining                     int64
	)
	err = tx.QueryRow(r.Context(), `
		SELECT r.status, r.period_key,
		       round((r.planned_amount_rub - r.paid_amount_rub) * 100)::bigint,
		       c.canonical_name, a.name
		FROM business_receivable r
		JOIN business_client c ON c.business_id = r.business_id AND c.id = r.client_id
		LEFT JOIN business_agreement a ON a.business_id = r.business_id AND a.id = r.agreement_id
		WHERE r.business_id = $1 AND r.id = $2
		FOR UPDATE OF r
	`, businessID, receivableID).Scan(&status, &periodKey, &remaining, &clientName, &agreementName)
	if errors.Is(err, pgx.ErrNoRows) {
		writeError(w, http.StatusNotFound, "receivable not found")
		return
	}
	if err != nil {
		writeError(w, http.StatusInternalServerError, "failed to record payment")
		return
	}
	if code, reason := receivablePaymentBlocker(status, remaining, amount); code != 0 {
		writeError(w, code, reason)
		return
	}

	source := receivablePaymentSource(request.PaymentChannel)
	purpose := receivablePaymentPurpose(stringValue(request.Notes), stringValue(agreementName), periodKey)
	// One key namespaces both rows: a retried request re-reads the same ledger
	// entry and the same match instead of paying the receivable twice.
	key := "receivable-payment:" + strings.TrimSpace(request.IdempotencyKey)

	var transactionID string
	err = tx.QueryRow(r.Context(), `
		INSERT INTO business_bank_transaction (
			business_id, source, dedup_key, booked_on, direction, amount_rub,
			counterparty_name, purpose, classification, classification_confidence, raw_payload
		) VALUES ($1,$2,$3,$4,'inbound',$5::numeric/100,$6,NULLIF($7,''),'client_income','confirmed',
			jsonb_build_object('created_by',$8::text,'receivable_id',$9::text,'payment_channel',$10::text))
		ON CONFLICT (business_id,dedup_key) WHERE voided_at IS NULL DO UPDATE SET updated_at=now()
		RETURNING id::text
	`, businessID, source, key, dateOnly(receivedOn), amount, clientName, purpose, userID, receivableID, request.PaymentChannel).Scan(&transactionID)
	if err != nil {
		writeError(w, http.StatusInternalServerError, "failed to record payment")
		return
	}

	var matchID string
	err = tx.QueryRow(r.Context(), `
		INSERT INTO business_transaction_match (
			business_id, transaction_id, target_type, target_id, amount_rub, status,
			suggested_by, confirmed_by, confirmed_at, idempotency_key, notes
		) VALUES ($1,$2,'receivable',$3,$4::numeric/100,'confirmed',$5,$5,now(),$6,'receivable payment')
		ON CONFLICT (business_id,idempotency_key) DO UPDATE SET updated_at=now()
		RETURNING id::text
	`, businessID, transactionID, receivableID, amount, userID, key).Scan(&matchID)
	if err != nil {
		writeError(w, http.StatusInternalServerError, "failed to record payment")
		return
	}
	if err := recalculateBusinessReceivableFunding(r.Context(), tx, businessID, receivableID); err != nil {
		writeError(w, http.StatusInternalServerError, "failed to update receivable funding")
		return
	}
	if err := h.insertBusinessAudit(r.Context(), tx, businessID, userID, "receivable.payment.record", "business_receivable", receivableID, "manual payment", nil, nil); err != nil {
		writeError(w, http.StatusInternalServerError, "failed to record payment")
		return
	}
	if err := tx.Commit(r.Context()); err != nil {
		writeError(w, http.StatusInternalServerError, "failed to record payment")
		return
	}
	writeJSON(w, http.StatusCreated, map[string]string{
		"receivable_id":  receivableID,
		"transaction_id": transactionID,
		"match_id":       matchID,
	})
}

type createBusinessCostRequest struct {
	TransactionID *string `json:"transaction_id"`
	Category      string  `json:"category"`
	AmountRUB     string  `json:"amount_rub"`
	WorkspaceID   *string `json:"workspace_id"`
	ClientID      *string `json:"client_id"`
	ProjectID     *string `json:"project_id"`
	IncurredOn    string  `json:"incurred_on"`
	Notes         *string `json:"notes"`
}

func (h *Handler) CreateBusinessCompanyCost(w http.ResponseWriter, r *http.Request) {
	businessID, userID, ok := businessRequestIDs(w, r)
	if !ok {
		return
	}
	var request createBusinessCostRequest
	if !decodeBusinessJSON(w, r, &request) {
		return
	}
	amount, err := parseBusinessBankMoney(request.AmountRUB)
	incurred, derr := time.Parse("2006-01-02", request.IncurredOn)
	if err != nil || derr != nil || amount <= 0 || !containsBusinessString([]string{"tax", "bank", "ai", "service", "infrastructure", "contractor", "other"}, request.Category) {
		writeError(w, 400, "cost fields are invalid")
		return
	}
	tx, err := h.TxStarter.Begin(r.Context())
	if err != nil {
		writeError(w, 500, "failed to create cost")
		return
	}
	defer tx.Rollback(r.Context())
	if request.WorkspaceID != nil && !businessWorkspaceExists(r.Context(), tx, businessID, *request.WorkspaceID) {
		writeError(w, 400, "workspace is outside this business")
		return
	}
	var id string
	err = tx.QueryRow(r.Context(), `INSERT INTO business_company_cost (business_id,transaction_id,category,amount_rub,workspace_id,client_id,project_id,incurred_on,notes,created_by) VALUES ($1,NULLIF($2,'')::uuid,$3,$4::numeric/100,NULLIF($5,'')::uuid,NULLIF($6,'')::uuid,NULLIF($7,'')::uuid,$8,NULLIF($9,''),$10) RETURNING id::text`, businessID, stringValue(request.TransactionID), request.Category, amount, stringValue(request.WorkspaceID), stringValue(request.ClientID), stringValue(request.ProjectID), dateOnly(incurred), stringValue(request.Notes), userID).Scan(&id)
	if err != nil {
		writeError(w, 500, "failed to create cost")
		return
	}
	if err := h.insertBusinessAudit(r.Context(), tx, businessID, userID, "cost.create", "business_company_cost", id, "company cost", nil, nil); err != nil {
		writeError(w, 500, "failed to create cost")
		return
	}
	if err := tx.Commit(r.Context()); err != nil {
		writeError(w, 500, "failed to create cost")
		return
	}
	writeJSON(w, 201, map[string]string{"id": id})
}

func businessMatchTargetExists(ctx context.Context, tx pgx.Tx, businessID, targetType, targetID string) bool {
	queries := map[string]string{
		"receivable":     `SELECT EXISTS(SELECT 1 FROM business_receivable WHERE business_id=$1 AND id=$2 AND status NOT IN ('skipped','written_off'))`,
		"billing_period": `SELECT EXISTS(SELECT 1 FROM client_billing_period cbp JOIN client_billing_config cbc ON cbc.id=cbp.client_billing_config_id JOIN business_workspace bw ON bw.workspace_id=cbc.workspace_id WHERE bw.business_id=$1 AND cbp.id=$2)`,
		"payout":         `SELECT EXISTS(SELECT 1 FROM business_payout_batch WHERE business_id=$1 AND id=$2)`,
		"company_cost":   `SELECT EXISTS(SELECT 1 FROM business_company_cost WHERE business_id=$1 AND id=$2 AND voided_at IS NULL)`,
	}
	var exists bool
	q, ok := queries[targetType]
	if !ok {
		return false
	}
	return tx.QueryRow(ctx, q, businessID, targetID).Scan(&exists) == nil && exists
}

func recalculateBusinessReceivableFunding(ctx context.Context, tx pgx.Tx, businessID, receivableID string) error {
	_, err := tx.Exec(ctx, `
		WITH funding AS (
			SELECT COALESCE(sum(amount_rub),0) AS paid FROM business_transaction_match
			WHERE business_id=$1 AND target_type='receivable' AND target_id=$2 AND status='confirmed'
		), updated AS (
			UPDATE business_receivable r SET paid_amount_rub=least(r.planned_amount_rub,f.paid),
				status=CASE
					WHEN f.paid <= 0 THEN r.status
					WHEN f.paid >= r.planned_amount_rub THEN 'paid'
					WHEN EXISTS (
						SELECT 1 FROM business_agreement a
						WHERE a.id = r.agreement_id
						  AND a.model = 'cap'
						  AND f.paid >= round(r.planned_amount_rub * 0.95, 2)
					) THEN 'paid'
					ELSE 'partially_paid'
				END,
				updated_at=now()
			FROM funding f WHERE r.business_id=$1 AND r.id=$2 RETURNING r.planned_amount_rub,r.paid_amount_rub
		), task_funding AS (
			UPDATE business_receivable_task rt SET funded_rub=least(rt.allocated_value_rub,
				CASE WHEN u.planned_amount_rub=0 THEN 0 ELSE u.paid_amount_rub*rt.allocated_value_rub/u.planned_amount_rub END),updated_at=now()
			FROM updated u WHERE rt.business_id=$1 AND rt.receivable_id=$2 RETURNING rt.task_economics_id,rt.service_value_rub,rt.funded_rub
		), ratios AS (
			SELECT task_economics_id,LEAST(1,COALESCE(sum(funded_rub)/NULLIF(max(service_value_rub),0),0)) ratio FROM task_funding GROUP BY task_economics_id
		)
		UPDATE business_accrual a SET funded_rub=round(a.original_amount_rub*r.ratio,2),
			status=CASE WHEN r.ratio>=1 THEN 'payable' WHEN r.ratio>0 THEN 'partially_payable' ELSE 'accrued' END,
			client_funded_at=CASE WHEN r.ratio>0 THEN COALESCE(a.client_funded_at,now()) END,
			payable_at=CASE WHEN r.ratio>=1 THEN COALESCE(a.payable_at,now()) END,updated_at=now()
		FROM ratios r WHERE a.business_id=$1 AND a.task_economics_id=r.task_economics_id AND a.status NOT IN ('in_payout','paid')
	`, businessID, receivableID)
	return err
}

type autoMatchReceivableCandidate struct {
	ID        string
	Remaining int64 // kopecks
	DueOn     *time.Time
	PeriodKey string
}

// pickAutoMatchReceivable chooses a unique best open receivable for an inbound
// payment and returns how much of the payment it takes. Score is
// |remaining - amount|; ties on the best score are refused.
//
// A payment larger than the receivable settles it in full and leaves the change
// unattributed: clients whose transfer bundles our fee with money we pass on to
// someone else (13 000 arrives, 7 000 is ours) would otherwise stay unmatched
// forever. Receivables the payment fits into are still preferred, so an exact
// payment never gets consumed by a smaller open period.
func pickAutoMatchReceivable(amountKopecks int64, candidates []autoMatchReceivableCandidate) (string, int64, bool) {
	if amountKopecks <= 0 {
		return "", 0, false
	}
	eligible := make([]autoMatchReceivableCandidate, 0, len(candidates))
	for _, candidate := range candidates {
		if candidate.Remaining > 0 {
			eligible = append(eligible, candidate)
		}
	}
	if len(eligible) == 0 {
		return "", 0, false
	}
	fits := func(candidate autoMatchReceivableCandidate) bool { return candidate.Remaining >= amountKopecks }
	sort.SliceStable(eligible, func(i, j int) bool {
		if fits(eligible[i]) != fits(eligible[j]) {
			return fits(eligible[i])
		}
		di := absInt64(eligible[i].Remaining - amountKopecks)
		dj := absInt64(eligible[j].Remaining - amountKopecks)
		if di != dj {
			return di < dj
		}
		diDue, djDue := eligible[i].DueOn, eligible[j].DueOn
		switch {
		case diDue == nil && djDue != nil:
			return false
		case diDue != nil && djDue == nil:
			return true
		case diDue != nil && djDue != nil && !diDue.Equal(*djDue):
			return diDue.Before(*djDue)
		}
		return eligible[i].PeriodKey < eligible[j].PeriodKey
	})
	// The list is already ordered by closeness, then due date, then period. Equal
	// remaining amounts are the normal case for a recurring monthly agreement, so
	// refusing on that alone left every such client permanently unmatched; the
	// oldest open debt is what an accountant applies a receipt to. Only a true
	// coin flip — same amount, same due date, same period — is left to a human.
	if len(eligible) > 1 && sameAutoMatchRank(eligible[0], eligible[1], amountKopecks) {
		return "", 0, false
	}
	return eligible[0].ID, minInt64(amountKopecks, eligible[0].Remaining), true
}

func minInt64(a, b int64) int64 {
	if a < b {
		return a
	}
	return b
}

func sameAutoMatchRank(first, second autoMatchReceivableCandidate, amountKopecks int64) bool {
	if absInt64(first.Remaining-amountKopecks) != absInt64(second.Remaining-amountKopecks) {
		return false
	}
	switch {
	case first.DueOn == nil && second.DueOn != nil:
		return false
	case first.DueOn != nil && second.DueOn == nil:
		return false
	case first.DueOn != nil && second.DueOn != nil && !first.DueOn.Equal(*second.DueOn):
		return false
	}
	return first.PeriodKey == second.PeriodKey
}

func absInt64(value int64) int64 {
	if value < 0 {
		return -value
	}
	return value
}

func businessReceivablePaidStatus(plannedRub, paidRub float64, agreementModel string) string {
	if paidRub <= 0 {
		return ""
	}
	if paidRub+1e-9 >= plannedRub {
		return "paid"
	}
	if agreementModel == "cap" && paidRub+1e-9 >= math.Round(plannedRub*0.95*100)/100 {
		return "paid"
	}
	return "partially_paid"
}

func resolveClientIDForBankAutoMatch(ctx context.Context, tx pgx.Tx, businessID, counterpartyName, counterpartyINN string) (string, bool) {
	inn := digitsOnly(counterpartyINN)
	name := strings.TrimSpace(counterpartyName)
	if inn != "" {
		var clientID string
		err := tx.QueryRow(ctx, `
			SELECT client_id::text
			FROM business_client_payer
			WHERE business_id = $1 AND status = 'active' AND inn = $2
			ORDER BY updated_at DESC
			LIMIT 1
		`, businessID, inn).Scan(&clientID)
		if err == nil && clientID != "" {
			return clientID, true
		}
	}
	if name == "" {
		return "", false
	}
	var clientID string
	err := tx.QueryRow(ctx, `
		SELECT client_id::text
		FROM business_client_payer
		WHERE business_id = $1 AND status = 'active' AND lower(btrim(name)) = lower(btrim($2))
		ORDER BY updated_at DESC
		LIMIT 1
	`, businessID, name).Scan(&clientID)
	if err != nil || clientID == "" {
		return "", false
	}
	return clientID, true
}

func autoMatchBankTransactionToReceivable(ctx context.Context, tx pgx.Tx, businessID, userID, transactionID string) error {
	var (
		direction, classification, counterpartyName string
		counterpartyINN                             *string
		purpose                                     string
		amountKopecks                               int64
		bookedOn                                    time.Time
	)
	err := tx.QueryRow(ctx, `
		SELECT direction, classification, counterparty_name, counterparty_inn,
		       COALESCE(purpose, ''), round(amount_rub * 100)::bigint, booked_on
		FROM business_bank_transaction
		WHERE business_id = $1 AND id = $2 AND voided_at IS NULL
	`, businessID, transactionID).Scan(&direction, &classification, &counterpartyName, &counterpartyINN, &purpose, &amountKopecks, &bookedOn)
	if err != nil {
		return err
	}
	if direction != "inbound" || classification != "client_income" || amountKopecks <= 0 {
		return nil
	}

	var alreadyMatched int64
	if err := tx.QueryRow(ctx, `
		SELECT COALESCE(round(sum(amount_rub) * 100), 0)::bigint
		FROM business_transaction_match
		WHERE business_id = $1 AND transaction_id = $2 AND status IN ('suggested', 'confirmed')
	`, businessID, transactionID).Scan(&alreadyMatched); err != nil {
		return err
	}
	if alreadyMatched > 0 {
		return nil
	}

	clientID, ok := resolveClientIDForBankAutoMatch(ctx, tx, businessID, counterpartyName, stringValue(counterpartyINN))
	if !ok {
		return nil
	}

	// An invoice named in the purpose says which document the money answers.
	// That beats every inference below it, which is why it runs first: both
	// wrong settlements this matcher has made on real money — a June payment
	// closing a July line, and one closing an agreement that did not exist on
	// the day it was paid — named their invoice in plain text.
	if ref, refOK := parseInvoiceReference(purpose); refOK {
		receivableID, takenKopecks, found, ferr := findReceivableByInvoiceNumber(ctx, tx, businessID, clientID, ref, amountKopecks)
		if ferr != nil {
			return ferr
		}
		if found {
			return insertBankReceivableMatch(ctx, tx, businessID, userID, transactionID, receivableID,
				takenKopecks, "auto bank match by invoice "+ref.Number)
		}
	}

	// Money can arrive late for a period that is already over, but it cannot
	// arrive long before the work: without this bound a payment from years ago
	// settles a current period, which is how a 2022 receipt once "paid" a 2026
	// month. Two weeks of slack cover prepayment for the period ahead.
	//
	// A month of slack was too generous. Monthly plan lines only start in July,
	// so every May and June payment had nowhere of its own to land and the
	// window pulled it onto a July or August line: five wrong settlements worth
	// 88 950 ₽. Real prepayments cluster at one to ten days early, the wrong
	// ones at twenty-seven to thirty-one, so two weeks separates them cleanly.
	//
	// The slack alone is not enough, because it is blind to when the deal was
	// struck: money for Innovatis site work on 26 June kept landing on the July
	// line of an SEO agreement signed on 27 July — five days early by the period
	// bound, a month before the agreement existed. Skipping those leaves the
	// payment for a human instead of settling the wrong line, and receivables
	// with no agreement keep their old eligibility.
	rows, err := tx.Query(ctx, `
		SELECT r.id::text,
		       round((r.planned_amount_rub - r.paid_amount_rub) * 100)::bigint,
		       r.due_on,
		       r.period_key
		FROM business_receivable r
		WHERE r.business_id = $1
		  AND r.client_id = $2::uuid
		  AND r.status IN ('expected', 'invoiced', 'overdue', 'partially_paid')
		  AND (r.planned_amount_rub - r.paid_amount_rub) > 0
		  AND $3::date >= r.period_start - INTERVAL '14 days'
		  AND NOT EXISTS (
		      SELECT 1 FROM business_agreement a
		      WHERE a.id = r.agreement_id AND a.effective_from > $3::date
		  )
	`, businessID, clientID, bookedOn.Format("2006-01-02"))
	if err != nil {
		return err
	}
	defer rows.Close()

	candidates := make([]autoMatchReceivableCandidate, 0)
	for rows.Next() {
		var candidate autoMatchReceivableCandidate
		if err := rows.Scan(&candidate.ID, &candidate.Remaining, &candidate.DueOn, &candidate.PeriodKey); err != nil {
			return err
		}
		candidates = append(candidates, candidate)
	}
	if err := rows.Err(); err != nil {
		return err
	}

	receivableID, matchedKopecks, ok := pickAutoMatchReceivable(amountKopecks, candidates)
	if !ok {
		return nil
	}
	return insertBankReceivableMatch(ctx, tx, businessID, userID, transactionID, receivableID, matchedKopecks, "auto bank match")
}

func insertBankReceivableMatch(ctx context.Context, tx pgx.Tx, businessID, userID, transactionID, receivableID string, amountKopecks int64, notes string) error {
	idempotencyKey := "auto-bank:" + transactionID + ":" + receivableID
	var matchID string
	err := tx.QueryRow(ctx, `
		INSERT INTO business_transaction_match (
			business_id, transaction_id, target_type, target_id, amount_rub, status,
			suggested_by, confirmed_by, confirmed_at, idempotency_key, notes
		) VALUES (
			$1, $2, 'receivable', $3, $4::numeric / 100, 'confirmed',
			$5, $5, now(), $6, $7
		)
		ON CONFLICT (business_id, idempotency_key) DO UPDATE SET updated_at = now()
		RETURNING id::text
	`, businessID, transactionID, receivableID, amountKopecks, userID, idempotencyKey, notes).Scan(&matchID)
	if err != nil {
		return err
	}
	return recalculateBusinessReceivableFunding(ctx, tx, businessID, receivableID)
}

// findReceivableByInvoiceNumber locates the one open plan line that carries the
// invoice the payment names. Scoped to the client because a number is not an
// identifier on its own — Elba restarts numbering every year, and № 16 exists
// in 2023, 2025 and 2026 across three clients. When the purpose names a date
// and the line records one, they must agree; a line invoiced before this change
// has no date recorded and matches on the number alone.
//
// Two candidates mean the number is ambiguous for this client, and the payment
// is left for a human rather than guessed at.
func findReceivableByInvoiceNumber(ctx context.Context, tx pgx.Tx, businessID, clientID string, ref invoiceReference, amountKopecks int64) (string, int64, bool, error) {
	invoiceDate := ""
	if !ref.Date.IsZero() {
		invoiceDate = ref.Date.Format("2006-01-02")
	}
	rows, err := tx.Query(ctx, `
		SELECT r.id::text, round((r.planned_amount_rub - r.paid_amount_rub) * 100)::bigint
		FROM business_receivable r
		WHERE r.business_id = $1
		  AND r.client_id = $2::uuid
		  AND r.elba_invoice_number = $3
		  AND r.status IN ('expected', 'invoiced', 'overdue', 'partially_paid')
		  AND (r.planned_amount_rub - r.paid_amount_rub) > 0
		  AND (
		        NULLIF($4, '')::date IS NULL
		     OR r.elba_invoice_date IS NULL
		     OR r.elba_invoice_date = NULLIF($4, '')::date
		  )
	`, businessID, clientID, ref.Number, invoiceDate)
	if err != nil {
		return "", 0, false, err
	}
	defer rows.Close()
	var (
		receivableID string
		remaining    int64
		matches      int
	)
	for rows.Next() {
		var id string
		var rem int64
		if err := rows.Scan(&id, &rem); err != nil {
			return "", 0, false, err
		}
		matches++
		receivableID, remaining = id, rem
	}
	if err := rows.Err(); err != nil {
		return "", 0, false, err
	}
	if matches != 1 {
		return "", 0, false, nil
	}
	return receivableID, minInt64(amountKopecks, remaining), true, nil
}

func recalculateBusinessPayoutPayment(ctx context.Context, tx pgx.Tx, businessID, payoutID string) error {
	var total, matched int64
	if err := tx.QueryRow(ctx, `SELECT round(total_rub*100)::bigint FROM business_payout_batch WHERE business_id=$1 AND id=$2 FOR UPDATE`, businessID, payoutID).Scan(&total); err != nil {
		return err
	}
	if err := tx.QueryRow(ctx, `SELECT COALESCE(round(sum(amount_rub)*100),0)::bigint FROM business_transaction_match WHERE business_id=$1 AND target_type='payout' AND target_id=$2 AND status='confirmed'`, businessID, payoutID).Scan(&matched); err != nil {
		return err
	}
	if total <= 0 || matched < total {
		return nil
	}
	if _, err := tx.Exec(ctx, `
		WITH paid_items AS (
			UPDATE business_payout_item SET status='paid',updated_at=now()
			WHERE business_id=$1 AND payout_batch_id=$2 AND status='submitted'
			RETURNING accrual_id,amount_rub
		)
		UPDATE business_accrual a SET paid_rub=LEAST(a.original_amount_rub,a.paid_rub+i.amount_rub),
			status='paid',paid_at=now(),updated_at=now()
		FROM paid_items i WHERE a.business_id=$1 AND a.id=i.accrual_id
	`, businessID, payoutID); err != nil {
		return err
	}
	_, err := tx.Exec(ctx, `UPDATE business_payout_batch SET status='paid',paid_at=now(),updated_at=now() WHERE business_id=$1 AND id=$2 AND status='submitted'`, businessID, payoutID)
	return err
}

func normalizeBusinessBankHeader(value string) string {
	value = strings.TrimSpace(strings.ToLower(strings.TrimPrefix(value, "\ufeff")))
	return strings.Join(strings.Fields(value), " ")
}
func canonicalBusinessBankHeader(value string) string {
	compact := strings.NewReplacer(" ", "", "_", "", "-", "").Replace(value)
	switch compact {
	case "account", "счет", "счёт", "номерсчета", "расчетныйсчет":
		return "account"
	case "date", "дата", "датапроводки", "датаоперации":
		return "date"
	case "counterparty", "контрагент", "наименованиеконтрагента", "плательщик", "получатель":
		return "counterparty"
	case "inn", "инн", "иннконтрагента":
		return "inn"
	case "inflow", "приход", "поступление", "кредит":
		return "inflow"
	case "outflow", "расход", "списание", "дебет":
		return "outflow"
	case "purpose", "назначение", "назначениеплатежа":
		return "purpose"
	}
	return ""
}
func businessBankCell(record []string, index int) string {
	if index < 0 || index >= len(record) {
		return ""
	}
	return strings.TrimSpace(record[index])
}
func businessBankRecordBlank(record []string) bool {
	for _, v := range record {
		if strings.TrimSpace(v) != "" {
			return false
		}
	}
	return true
}
func parseBusinessBankDate(value string) (time.Time, error) {
	value = strings.TrimSpace(value)
	for _, layout := range []string{"2006-01-02", "02.01.2006", "02/01/2006", "2006-01-02 15:04:05", "02.01.2006 15:04:05"} {
		if parsed, err := time.Parse(layout, value); err == nil {
			return parsed, nil
		}
	}
	return time.Time{}, fmt.Errorf("invalid date")
}
func parseBusinessBankMoney(value string) (int64, error) {
	clean := strings.NewReplacer("\u00a0", "", " ", "", "₽", "", "руб.", "", "руб", "", ",", ".").Replace(strings.TrimSpace(value))
	if clean == "" || clean == "-" {
		return 0, nil
	}
	number, err := strconv.ParseFloat(clean, 64)
	if err != nil {
		return 0, err
	}
	if number < 0 {
		number = -number
	}
	return int64(number*100 + 0.5), nil
}
func digitsOnly(value string) string {
	return strings.Map(func(r rune) rune {
		if unicode.IsDigit(r) {
			return r
		}
		return -1
	}, value)
}
func maskBusinessAccount(value string) string {
	value = digitsOnly(value)
	if len(value) <= 8 {
		return value
	}
	return value[:4] + "…" + value[len(value)-4:]
}
func businessBankCounterpartyKey(row businessBankRow) string {
	if row.INN != "" {
		return "inn:" + row.INN
	}
	return "name:" + strings.ToLower(strings.Join(strings.Fields(row.Counterparty), " "))
}
func businessBankDedupKey(row businessBankRow, direction string, amount int64) string {
	normalized := fmt.Sprintf("%s|%s|%d|%s|%s|%s|%s", row.BookedOn.Format("2006-01-02"), direction, amount, strings.ToLower(strings.Join(strings.Fields(row.Counterparty), " ")), row.INN, strings.ToLower(strings.Join(strings.Fields(row.Purpose), " ")), digitsOnly(row.Account))
	digest := sha256.Sum256([]byte(normalized))
	return hex.EncodeToString(digest[:])
}
