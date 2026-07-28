package handler

import (
	"bytes"
	"context"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"math"
	"net/http"
	"strings"
	"time"

	"github.com/google/uuid"
	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgtype"
	"github.com/jackc/pgx/v5/pgxpool"
)

const (
	modulbankDefaultBaseURL          = "https://api.modulbank.ru/v1"
	modulbankDefaultLookbackDays     = 14
	modulbankDefaultFullSyncInterval = 24 * time.Hour
	modulbankPageSize                = 50
)

type ModulbankSyncConfig struct {
	Token            string
	BusinessID       string
	BaseURL          string
	LookbackDays     int
	FullSyncInterval time.Duration
	HTTPClient       *http.Client
}

type ModulbankSyncer struct {
	pool             *pgxpool.Pool
	token            string
	businessID       string
	baseURL          string
	lookbackDays     int
	fullSyncInterval time.Duration
	httpClient       *http.Client
}

type ModulbankSyncResult struct {
	Mode              string
	Accounts          int
	Fetched           int
	Active            int
	Inserted          int
	Updated           int
	Voided            int
	Invalid           int
	MissingVoided     int64
	LegacyVoided      int64
	MatchesMigrated   int64
	UnmigratedMatches int64
}

func (r ModulbankSyncResult) schedulerResult() (int64, map[string]any) {
	rows := int64(r.Inserted + r.Updated + r.Voided)
	return rows, map[string]any{
		"mode":               r.Mode,
		"accounts":           r.Accounts,
		"fetched":            r.Fetched,
		"active":             r.Active,
		"inserted":           r.Inserted,
		"updated":            r.Updated,
		"voided":             r.Voided,
		"invalid":            r.Invalid,
		"missing_voided":     r.MissingVoided,
		"legacy_voided":      r.LegacyVoided,
		"matches_migrated":   r.MatchesMigrated,
		"unmigrated_matches": r.UnmigratedMatches,
	}
}

func NewModulbankSyncer(pool *pgxpool.Pool, cfg ModulbankSyncConfig) (*ModulbankSyncer, error) {
	if pool == nil {
		return nil, errors.New("modulbank sync: database pool is required")
	}
	token := strings.TrimSpace(cfg.Token)
	if token == "" {
		return nil, errors.New("modulbank sync: MODULBANK_API_TOKEN is required")
	}
	businessID := strings.TrimSpace(cfg.BusinessID)
	if _, err := uuid.Parse(businessID); err != nil {
		return nil, errors.New("modulbank sync: MODULBANK_BUSINESS_ID must be a UUID")
	}
	baseURL := strings.TrimRight(strings.TrimSpace(cfg.BaseURL), "/")
	if baseURL == "" {
		baseURL = modulbankDefaultBaseURL
	}
	lookbackDays := cfg.LookbackDays
	if lookbackDays <= 0 {
		lookbackDays = modulbankDefaultLookbackDays
	}
	fullSyncInterval := cfg.FullSyncInterval
	if fullSyncInterval <= 0 {
		fullSyncInterval = modulbankDefaultFullSyncInterval
	}
	client := cfg.HTTPClient
	if client == nil {
		client = &http.Client{Timeout: 30 * time.Second}
	}
	return &ModulbankSyncer{
		pool:             pool,
		token:            token,
		businessID:       businessID,
		baseURL:          baseURL,
		lookbackDays:     lookbackDays,
		fullSyncInterval: fullSyncInterval,
		httpClient:       client,
	}, nil
}

// Run implements scheduler.ModulbankSyncRunner without making the scheduler
// package depend on handler-specific result types.
func (s *ModulbankSyncer) Run(ctx context.Context) (int64, map[string]any, error) {
	result, err := s.Sync(ctx)
	if err != nil {
		return 0, nil, err
	}
	rows, metadata := result.schedulerResult()
	return rows, metadata, nil
}

type modulbankCompany struct {
	BankAccounts []modulbankAccount `json:"bankAccounts"`
}

type modulbankAccount struct {
	ID            string `json:"id"`
	AccountNumber string `json:"accountNumber"`
}

type modulbankOperation struct {
	ID                          string  `json:"id"`
	Status                      string  `json:"status"`
	Category                    string  `json:"category"`
	ContragentName              string  `json:"contragentName"`
	ContragentINN               string  `json:"contragentInn"`
	ContragentKPP               string  `json:"contragentKpp"`
	ContragentBankAccountNumber string  `json:"contragentBankAccountNumber"`
	Currency                    string  `json:"currency"`
	Amount                      float64 `json:"amount"`
	BankAccountNumber           string  `json:"bankAccountNumber"`
	PaymentPurpose              string  `json:"paymentPurpose"`
	Executed                    string  `json:"executed"`
	Created                     string  `json:"created"`
	DocNumber                   string  `json:"docNumber"`
	AbsID                       string  `json:"absId"`
	ModDate                     string  `json:"modDate"`
}

func (s *ModulbankSyncer) doJSON(ctx context.Context, method, path string, payload any, out any) error {
	var body io.Reader
	if payload != nil {
		raw, err := json.Marshal(payload)
		if err != nil {
			return err
		}
		body = bytes.NewReader(raw)
	}
	req, err := http.NewRequestWithContext(ctx, method, s.baseURL+path, body)
	if err != nil {
		return err
	}
	req.Header.Set("Authorization", "Bearer "+s.token)
	req.Header.Set("Accept", "application/json")
	req.Header.Set("Content-Type", "application/json")
	resp, err := s.httpClient.Do(req)
	if err != nil {
		return err
	}
	defer resp.Body.Close()
	raw, readErr := io.ReadAll(io.LimitReader(resp.Body, 16<<20))
	if readErr != nil {
		return readErr
	}
	if resp.StatusCode < 200 || resp.StatusCode >= 300 {
		message := strings.TrimSpace(string(raw))
		if len(message) > 300 {
			message = message[:300]
		}
		return fmt.Errorf("modulbank %s %s: HTTP %d: %s", method, path, resp.StatusCode, message)
	}
	if out == nil || len(raw) == 0 {
		return nil
	}
	if err := json.Unmarshal(raw, out); err != nil {
		return fmt.Errorf("modulbank %s %s: invalid JSON: %w", method, path, err)
	}
	return nil
}

func (s *ModulbankSyncer) accounts(ctx context.Context) ([]modulbankAccount, error) {
	var companies []modulbankCompany
	if err := s.doJSON(ctx, http.MethodPost, "/account-info", map[string]any{}, &companies); err != nil {
		return nil, err
	}
	accounts := make([]modulbankAccount, 0)
	seen := map[string]bool{}
	for _, company := range companies {
		for _, account := range company.BankAccounts {
			account.ID = strings.TrimSpace(account.ID)
			if account.ID == "" || seen[account.ID] {
				continue
			}
			seen[account.ID] = true
			accounts = append(accounts, account)
		}
	}
	if len(accounts) == 0 {
		return nil, errors.New("modulbank account-info returned no accounts")
	}
	return accounts, nil
}

func (s *ModulbankSyncer) operations(ctx context.Context, accountID string, from *time.Time) ([]modulbankOperation, error) {
	operations := make([]modulbankOperation, 0)
	for skip := 0; ; skip += modulbankPageSize {
		payload := map[string]any{"skip": skip, "records": modulbankPageSize}
		if from != nil {
			payload["from"] = from.Format("2006-01-02")
		}
		var page []modulbankOperation
		path := "/operation-history/" + accountID
		if err := s.doJSON(ctx, http.MethodPost, path, payload, &page); err != nil {
			return nil, err
		}
		operations = append(operations, page...)
		if len(page) < modulbankPageSize {
			break
		}
		if len(operations) > 100000 {
			return nil, errors.New("modulbank operation history exceeded safety limit")
		}
	}
	return operations, nil
}

func (s *ModulbankSyncer) syncWindow(ctx context.Context, now time.Time) (bool, *time.Time, error) {
	var lastFull pgtype.Timestamptz
	if err := s.pool.QueryRow(ctx, `
		SELECT max(completed_at)
		FROM business_bank_import_batch
		WHERE business_id=$1 AND source='modulbank_api' AND status='completed'
		  AND raw_metadata->>'mode'='full'
	`, s.businessID).Scan(&lastFull); err != nil {
		return false, nil, err
	}
	full := !lastFull.Valid || now.Sub(lastFull.Time) >= s.fullSyncInterval
	if full {
		return true, nil, nil
	}
	var latest pgtype.Date
	if err := s.pool.QueryRow(ctx, `
		SELECT max(booked_on)
		FROM business_bank_transaction
		WHERE business_id=$1 AND source='modulbank_api' AND voided_at IS NULL
	`, s.businessID).Scan(&latest); err != nil {
		return false, nil, err
	}
	if !latest.Valid {
		return true, nil, nil
	}
	from := latest.Time.AddDate(0, 0, -s.lookbackDays)
	return false, &from, nil
}

func (s *ModulbankSyncer) Sync(ctx context.Context) (ModulbankSyncResult, error) {
	now := time.Now().UTC()
	full, from, err := s.syncWindow(ctx, now)
	if err != nil {
		return ModulbankSyncResult{}, fmt.Errorf("modulbank sync window: %w", err)
	}
	result := ModulbankSyncResult{Mode: "incremental"}
	if full {
		result.Mode = "full"
	}
	accounts, err := s.accounts(ctx)
	if err != nil {
		return result, err
	}
	result.Accounts = len(accounts)
	operations := make([]modulbankOperation, 0)
	for _, account := range accounts {
		page, fetchErr := s.operations(ctx, account.ID, from)
		if fetchErr != nil {
			return result, fetchErr
		}
		operations = append(operations, page...)
	}
	result.Fetched = len(operations)
	return s.persist(ctx, now, full, operations, result)
}

func (s *ModulbankSyncer) persist(ctx context.Context, now time.Time, full bool, operations []modulbankOperation, result ModulbankSyncResult) (ModulbankSyncResult, error) {
	tx, err := s.pool.Begin(ctx)
	if err != nil {
		return result, err
	}
	defer tx.Rollback(ctx)

	// Auto-match records who confirmed the link; the sync runs unattended, so it
	// acts as the business owner exactly like the scheduled import batch does.
	var ownerUserID string
	if err := tx.QueryRow(ctx, `SELECT owner_user_id::text FROM business_account WHERE id=$1`, s.businessID).Scan(&ownerUserID); err != nil {
		if errors.Is(err, pgx.ErrNoRows) {
			return result, errors.New("modulbank sync: configured business account does not exist")
		}
		return result, err
	}

	var batchID *string
	if full {
		id, batchErr := s.upsertFullSyncBatch(ctx, tx, now, len(operations))
		if batchErr != nil {
			return result, batchErr
		}
		batchID = &id
	}

	for _, operation := range operations {
		mapped, active, mapErr := mapModulbankOperation(operation)
		if mapErr != nil {
			result.Invalid++
			continue
		}
		dedupKey := "modulbank-api:" + operation.ID
		if !active {
			command, voidErr := tx.Exec(ctx, `
				UPDATE business_bank_transaction
				SET voided_at=COALESCE(voided_at,now()), raw_payload=$3, updated_at=now()
				WHERE business_id=$1 AND source='modulbank_api' AND dedup_key=$2
			`, s.businessID, dedupKey, mapped.raw)
			if voidErr != nil {
				return result, voidErr
			}
			result.Voided += int(command.RowsAffected())
			continue
		}
		result.Active++
		classification, confidence := classifyBusinessBankRow(ctx, tx, s.businessID, mapped.row, mapped.direction)
		var existingID string
		existingErr := tx.QueryRow(ctx, `
			SELECT id::text FROM business_bank_transaction
			WHERE business_id=$1 AND dedup_key=$2
		`, s.businessID, dedupKey).Scan(&existingID)
		if existingErr != nil && !errors.Is(existingErr, pgx.ErrNoRows) {
			return result, existingErr
		}
		var transactionID string
		err = tx.QueryRow(ctx, `
			INSERT INTO business_bank_transaction (
				business_id, import_batch_id, source, external_id, dedup_key, booked_on,
				direction, amount_rub, currency, account_mask, counterparty_name,
				counterparty_inn, counterparty_kpp, counterparty_account_mask, purpose,
				classification, classification_confidence, raw_payload, voided_at
			) VALUES (
				$1,NULLIF($2,'')::uuid,'modulbank_api',$3,$4,$5,$6,$7::numeric/100,'RUB',$8,$9,
				NULLIF($10,''),NULLIF($11,''),NULLIF($12,''),NULLIF($13,''),$14,$15,$16,NULL
			)
			ON CONFLICT (business_id,dedup_key) DO UPDATE SET
				import_batch_id=COALESCE(EXCLUDED.import_batch_id,business_bank_transaction.import_batch_id),
				external_id=EXCLUDED.external_id, booked_on=EXCLUDED.booked_on,
				direction=EXCLUDED.direction, amount_rub=EXCLUDED.amount_rub,
				account_mask=EXCLUDED.account_mask, counterparty_name=EXCLUDED.counterparty_name,
				counterparty_inn=EXCLUDED.counterparty_inn, counterparty_kpp=EXCLUDED.counterparty_kpp,
				counterparty_account_mask=EXCLUDED.counterparty_account_mask, purpose=EXCLUDED.purpose,
				classification=CASE WHEN business_bank_transaction.classification_confidence='confirmed'
					THEN business_bank_transaction.classification ELSE EXCLUDED.classification END,
				classification_confidence=CASE WHEN business_bank_transaction.classification_confidence='confirmed'
					THEN business_bank_transaction.classification_confidence ELSE EXCLUDED.classification_confidence END,
				raw_payload=EXCLUDED.raw_payload, voided_at=NULL, updated_at=now()
			RETURNING id::text
		`, s.businessID, stringValue(batchID), operation.ID, dedupKey, dateOnly(mapped.bookedOn), mapped.direction,
			mapped.amountCents, maskBusinessAccount(mapped.row.Account), mapped.row.Counterparty, mapped.row.INN,
			operation.ContragentKPP, maskBusinessAccount(operation.ContragentBankAccountNumber), mapped.row.Purpose,
			classification, confidence, mapped.raw).Scan(&transactionID)
		if err != nil {
			return result, err
		}
		if errors.Is(existingErr, pgx.ErrNoRows) {
			result.Inserted++
		} else {
			result.Updated++
		}
		legacyDedup := businessBankDedupKey(mapped.row, mapped.direction, mapped.amountCents)
		migrated, migrateErr := tx.Exec(ctx, `
			UPDATE business_transaction_match m
			SET transaction_id=$3, updated_at=now()
			FROM business_bank_transaction legacy
			WHERE legacy.business_id=$1 AND legacy.source IN ('modulbank_csv','modulbank_xlsx')
			  AND legacy.dedup_key=$2 AND m.business_id=$1 AND m.transaction_id=legacy.id
		`, s.businessID, legacyDedup, transactionID)
		if migrateErr != nil {
			return result, migrateErr
		}
		result.MatchesMigrated += migrated.RowsAffected()

		// Receipts pulled from the bank API have to settle receivables the same
		// way an imported statement does. Without this the money reached the
		// ledger but every receivable stayed open until someone matched it by
		// hand. Runs after the legacy migration so an already-linked operation is
		// left alone by the auto-matcher's own guard.
		if err := autoMatchBankTransactionToReceivable(ctx, tx, s.businessID, ownerUserID, transactionID); err != nil {
			return result, err
		}
	}

	if full {
		seenDedupKeys := make([]string, 0, len(operations))
		for _, operation := range operations {
			if operationID := strings.TrimSpace(operation.ID); operationID != "" {
				seenDedupKeys = append(seenDedupKeys, "modulbank-api:"+operationID)
			}
		}
		if len(seenDedupKeys) == 0 {
			var existing int64
			if err := tx.QueryRow(ctx, `
				SELECT count(*) FROM business_bank_transaction
				WHERE business_id=$1 AND source='modulbank_api' AND voided_at IS NULL
			`, s.businessID).Scan(&existing); err != nil {
				return result, err
			}
			if existing > 0 {
				return result, errors.New("modulbank full sync returned no operation ids; refusing to void existing API history")
			}
		} else {
			missing, missingErr := tx.Exec(ctx, `
				UPDATE business_bank_transaction
				SET voided_at=COALESCE(voided_at,now()), updated_at=now()
				WHERE business_id=$1 AND source='modulbank_api' AND voided_at IS NULL
				  AND NOT (dedup_key = ANY($2::text[]))
			`, s.businessID, seenDedupKeys)
			if missingErr != nil {
				return result, missingErr
			}
			result.MissingVoided = missing.RowsAffected()
			result.Voided += int(result.MissingVoided)
		}
		legacy, legacyErr := tx.Exec(ctx, `
			UPDATE business_bank_transaction
			SET voided_at=COALESCE(voided_at,now()), updated_at=now()
			WHERE business_id=$1 AND source IN ('modulbank_csv','modulbank_xlsx') AND voided_at IS NULL
		`, s.businessID)
		if legacyErr != nil {
			return result, legacyErr
		}
		result.LegacyVoided = legacy.RowsAffected()
		if err := tx.QueryRow(ctx, `
			SELECT count(*)
			FROM business_transaction_match m
			JOIN business_bank_transaction t ON t.id=m.transaction_id AND t.business_id=m.business_id
			WHERE m.business_id=$1 AND t.source IN ('modulbank_csv','modulbank_xlsx') AND t.voided_at IS NOT NULL
		`, s.businessID).Scan(&result.UnmigratedMatches); err != nil {
			return result, err
		}
		if err := s.finishFullSyncBatch(ctx, tx, *batchID, result); err != nil {
			return result, err
		}
	}
	if err := tx.Commit(ctx); err != nil {
		return result, err
	}
	return result, nil
}

func (s *ModulbankSyncer) upsertFullSyncBatch(ctx context.Context, tx pgx.Tx, now time.Time, rows int) (string, error) {
	idempotencyKey := "modulbank-api-full:" + now.Format("2006-01-02")
	digest := sha256.Sum256([]byte(idempotencyKey))
	fileHash := hex.EncodeToString(digest[:])
	metadata, _ := json.Marshal(map[string]any{"mode": "full"})
	var id string
	err := tx.QueryRow(ctx, `
		INSERT INTO business_bank_import_batch (
			business_id, source, filename, file_sha256, idempotency_key, status,
			rows_total, imported_by, raw_metadata
		)
		SELECT $1,'modulbank_api',$2,$3,$4,'processing',$5,owner_user_id,$6
		FROM business_account WHERE id=$1
		ON CONFLICT (business_id,idempotency_key) DO UPDATE SET
			status='processing', rows_total=EXCLUDED.rows_total, rows_inserted=0,
			rows_duplicate=0, rows_invalid=0, raw_metadata=EXCLUDED.raw_metadata,
			error_message=NULL, completed_at=NULL, voided_at=NULL, updated_at=now()
		RETURNING id::text
	`, s.businessID, "modulbank-api-full-"+now.Format("2006-01-02"), fileHash, idempotencyKey, rows, metadata).Scan(&id)
	if errors.Is(err, pgx.ErrNoRows) {
		return "", errors.New("modulbank sync: configured business account does not exist")
	}
	return id, err
}

func (s *ModulbankSyncer) finishFullSyncBatch(ctx context.Context, tx pgx.Tx, batchID string, result ModulbankSyncResult) error {
	metadata, _ := json.Marshal(map[string]any{
		"mode":               "full",
		"accounts":           result.Accounts,
		"fetched":            result.Fetched,
		"active":             result.Active,
		"missing_voided":     result.MissingVoided,
		"legacy_voided":      result.LegacyVoided,
		"matches_migrated":   result.MatchesMigrated,
		"unmigrated_matches": result.UnmigratedMatches,
	})
	_, err := tx.Exec(ctx, `
		UPDATE business_bank_import_batch
		SET status='completed', rows_inserted=$2, rows_duplicate=$3, rows_invalid=$4,
			raw_metadata=$5, completed_at=now(), updated_at=now()
		WHERE business_id=$1 AND id=$6
	`, s.businessID, result.Inserted, result.Updated, result.Invalid, metadata, batchID)
	return err
}

type mappedModulbankOperation struct {
	row         businessBankRow
	direction   string
	amountCents int64
	bookedOn    time.Time
	raw         []byte
}

func mapModulbankOperation(operation modulbankOperation) (mappedModulbankOperation, bool, error) {
	operation.ID = strings.TrimSpace(operation.ID)
	if operation.ID == "" {
		return mappedModulbankOperation{}, false, errors.New("missing operation id")
	}
	direction := ""
	switch strings.ToLower(strings.TrimSpace(operation.Category)) {
	case "debet":
		direction = "inbound"
	case "credit":
		direction = "outbound"
	default:
		return mappedModulbankOperation{}, false, errors.New("unknown operation category")
	}
	amountCents := int64(math.Round(math.Abs(operation.Amount) * 100))
	if amountCents <= 0 {
		return mappedModulbankOperation{}, false, errors.New("non-positive amount")
	}
	bookedOn, err := parseModulbankTime(operation.Executed)
	if err != nil {
		bookedOn, err = parseModulbankTime(operation.Created)
	}
	if err != nil {
		return mappedModulbankOperation{}, false, errors.New("missing execution date")
	}
	currency := strings.ToUpper(strings.TrimSpace(operation.Currency))
	if currency != "RUR" && currency != "RUB" {
		return mappedModulbankOperation{}, false, errors.New("unsupported currency")
	}
	counterparty := strings.TrimSpace(operation.ContragentName)
	if counterparty == "" {
		counterparty = "Без контрагента"
	}
	raw, err := json.Marshal(operation)
	if err != nil {
		return mappedModulbankOperation{}, false, err
	}
	row := businessBankRow{
		Account:      operation.BankAccountNumber,
		BookedOn:     bookedOn,
		Counterparty: counterparty,
		INN:          digitsOnly(operation.ContragentINN),
		Inflow:       0,
		Outflow:      0,
		Purpose:      strings.TrimSpace(operation.PaymentPurpose),
	}
	if direction == "inbound" {
		row.Inflow = amountCents
	} else {
		row.Outflow = amountCents
	}
	active := map[string]bool{
		"executed":        true,
		"received":        true,
		"payreceived":     true,
		"paid":            true,
		"payrollexecuted": true,
	}[strings.ToLower(strings.TrimSpace(operation.Status))]
	return mappedModulbankOperation{
		row:         row,
		direction:   direction,
		amountCents: amountCents,
		bookedOn:    bookedOn,
		raw:         raw,
	}, active, nil
}

func parseModulbankTime(value string) (time.Time, error) {
	value = strings.TrimSpace(value)
	for _, layout := range []string{time.RFC3339Nano, "2006-01-02T15:04:05", "2006-01-02"} {
		if parsed, err := time.Parse(layout, value); err == nil {
			return parsed, nil
		}
	}
	return time.Time{}, errors.New("invalid Modulbank time")
}
