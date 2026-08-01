package main

import (
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"mime/multipart"
	"net/http"
	"testing"
	"time"
)

func importBusinessCSV(t *testing.T, businessID, content string, want int) map[string]any {
	t.Helper()
	var body bytes.Buffer
	writer := multipart.NewWriter(&body)
	part, err := writer.CreateFormFile("file", "statement.csv")
	if err != nil {
		t.Fatal(err)
	}
	if _, err := part.Write([]byte(content)); err != nil {
		t.Fatal(err)
	}
	if err := writer.Close(); err != nil {
		t.Fatal(err)
	}
	req, err := http.NewRequest(http.MethodPost, testServer.URL+"/api/businesses/"+businessID+"/bank/imports", &body)
	if err != nil {
		t.Fatal(err)
	}
	req.Header.Set("Content-Type", writer.FormDataContentType())
	req.Header.Set("Authorization", "Bearer "+testToken)
	resp, err := http.DefaultClient.Do(req)
	if err != nil {
		t.Fatal(err)
	}
	defer resp.Body.Close()
	if resp.StatusCode != want {
		t.Fatalf("bank import: expected %d, got %d", want, resp.StatusCode)
	}
	var result map[string]any
	if err := json.NewDecoder(resp.Body).Decode(&result); err != nil {
		t.Fatal(err)
	}
	return result
}

func businessRequest(t *testing.T, method, businessID, path string, body any, want int) map[string]any {
	t.Helper()
	resp := authRequest(t, method, "/api/businesses/"+businessID+path, body)
	defer resp.Body.Close()
	if resp.StatusCode != want {
		var failure any
		_ = json.NewDecoder(resp.Body).Decode(&failure)
		t.Fatalf("%s %s: expected %d, got %d: %v", method, path, want, resp.StatusCode, failure)
	}
	if resp.StatusCode == http.StatusNoContent {
		return nil
	}
	var result map[string]any
	if err := json.NewDecoder(resp.Body).Decode(&result); err != nil {
		t.Fatalf("decode %s %s: %v", method, path, err)
	}
	return result
}

func cleanupBusinessIntegrationFixture(ctx context.Context, t *testing.T, businessID string) {
	t.Helper()
	for _, table := range []string{
		"business_bank_outbox", "business_payout_item", "business_payout_batch", "business_reserve_ledger",
		"business_accrual_adjustment", "business_quality_case", "business_accrual", "business_receivable_task",
		"business_task_participant", "business_task_economics", "business_client_request", "business_compensation_policy",
		"business_worker", "business_company_cost", "business_transaction_match", "business_bank_transaction",
		"business_bank_import_batch", "business_receivable", "business_agreement", "business_counterparty_classification",
		"business_client_project", "business_client_payer", "business_client_alias", "business_client",
		"business_audit_event", "business_workspace", "business_account_member",
	} {
		if _, err := testPool.Exec(ctx, fmt.Sprintf("DELETE FROM %s WHERE business_id=$1", table), businessID); err != nil {
			t.Errorf("cleanup %s: %v", table, err)
		}
	}
	if _, err := testPool.Exec(ctx, `DELETE FROM business_account WHERE id=$1`, businessID); err != nil {
		t.Errorf("cleanup business_account: %v", err)
	}
}

func TestBusinessSystemLifecycle(t *testing.T) {
	ctx := context.Background()
	var businessID, projectID, issueID string
	if err := testPool.QueryRow(ctx, `INSERT INTO business_account(name,owner_user_id) VALUES ('Integration Business',$1) RETURNING id`, testUserID).Scan(&businessID); err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { cleanupBusinessIntegrationFixture(context.Background(), t, businessID) })
	if _, err := testPool.Exec(ctx, `INSERT INTO business_account_member(business_id,user_id,role) VALUES ($1,$2,'owner')`, businessID, testUserID); err != nil {
		t.Fatal(err)
	}
	if _, err := testPool.Exec(ctx, `INSERT INTO business_workspace(business_id,workspace_id,kind) VALUES ($1,$2,'operational')`, businessID, testWorkspaceID); err != nil {
		t.Fatal(err)
	}
	if err := testPool.QueryRow(ctx, `INSERT INTO project(workspace_id,title,status) VALUES ($1,'Business lifecycle','in_progress') RETURNING id`, testWorkspaceID).Scan(&projectID); err != nil {
		t.Fatal(err)
	}
	if err := testPool.QueryRow(ctx, `INSERT INTO issue(workspace_id,title,creator_type,creator_id,project_id) VALUES ($1,'Billable lifecycle','member',$2,$3) RETURNING id`, testWorkspaceID, testUserID, projectID).Scan(&issueID); err != nil {
		t.Fatal(err)
	}

	list := authRequest(t, http.MethodGet, "/api/businesses/", nil)
	if list.StatusCode != http.StatusOK {
		list.Body.Close()
		t.Fatalf("list businesses: %d", list.StatusCode)
	}
	list.Body.Close()

	client := businessRequest(t, http.MethodPost, businessID, "/clients", map[string]any{
		"canonical_name": "Integration Client", "status": "active", "primary_payment_channel": "bank",
	}, http.StatusCreated)
	clientID := client["id"].(string)
	businessRequest(t, http.MethodPut, businessID, "/clients/"+clientID+"/projects", map[string]any{
		"workspace_id": testWorkspaceID, "project_id": projectID, "service_type": "development", "billable": true,
	}, http.StatusOK)
	businessRequest(t, http.MethodPost, businessID, "/clients/"+clientID+"/payers", map[string]any{
		"name": "Integration Payer", "inn": "1234567890", "status": "active", "payment_channel": "bank",
	}, http.StatusCreated)
	csv := "account;date;counterparty;inn;inflow;outflow;purpose\n" +
		"40817810000000000001;19.07.2026;УФК КБ85;7724010662;1000;;транзит VitMax\n" +
		"40817810000000000001;19.07.2026;Неизвестный плательщик;9999999999;500;;проверка inbox\n"
	firstImport := importBusinessCSV(t, businessID, csv, http.StatusCreated)
	if firstImport["rows_inserted"] != float64(2) {
		t.Fatalf("expected two imported rows, got %v", firstImport)
	}
	secondImport := importBusinessCSV(t, businessID, csv, http.StatusOK)
	if secondImport["already_imported"] != true {
		t.Fatalf("expected duplicate import to be idempotent, got %v", secondImport)
	}
	var importedClassification string
	if err := testPool.QueryRow(ctx, `SELECT classification FROM business_bank_transaction WHERE business_id=$1 AND source='modulbank_csv' AND counterparty_inn='7724010662'`, businessID).Scan(&importedClassification); err != nil {
		t.Fatal(err)
	}
	if importedClassification != "vitmax_transit" {
		t.Fatalf("expected VitMax transit classification, got %s", importedClassification)
	}

	month := time.Now().UTC().Format("2006-01")
	start := month + "-01"
	agreement := businessRequest(t, http.MethodPost, businessID, "/agreements", map[string]any{
		"client_id": clientID, "project_id": projectID, "service_type": "development",
		"agreement_key": "integration-fixed", "version": 1, "name": "Integration fixed",
		"model": "fixed", "amount_rub": "1000", "invoice_day": 1, "due_days": 7,
		"period_months": 1, "payment_channel": "bank", "effective_from": start,
		"status": "active", "terms": map[string]any{},
	}, http.StatusCreated)
	if agreement["id"] == nil {
		t.Fatal("agreement id missing")
	}
	businessRequest(t, http.MethodPost, businessID, "/receivables/generate", map[string]any{"from_month": month, "months": 1}, http.StatusOK)

	worker := businessRequest(t, http.MethodPost, businessID, "/workers", map[string]any{
		"user_id": testUserID, "name": "Integration Worker", "engagement_format": "self_employed",
	}, http.StatusCreated)
	workerID := worker["id"].(string)
	economics := businessRequest(t, http.MethodPost, businessID, "/task-economics", map[string]any{
		"workspace_id": testWorkspaceID, "project_id": projectID, "issue_id": issueID, "client_id": clientID,
		"service_type": "development", "service_value_rub": "1000", "source": "manual_override",
		"billing_disposition": "normal", "idempotency_key": "integration-economics",
		"participants": []map[string]any{{"worker_id": workerID, "role": "executor", "pool": "execution", "percent": "25"}},
	}, http.StatusCreated)
	economicsID := economics["id"].(string)
	businessRequest(t, http.MethodPost, businessID, "/task-economics/"+economicsID+"/accept", map[string]any{"reason": "integration acceptance"}, http.StatusOK)

	var receivableID string
	if err := testPool.QueryRow(ctx, `SELECT id FROM business_receivable WHERE business_id=$1 AND agreement_id=$2`, businessID, agreement["id"]).Scan(&receivableID); err != nil {
		t.Fatal(err)
	}
	businessRequest(t, http.MethodPost, businessID, "/receivable-tasks", map[string]any{
		"receivable_id": receivableID, "task_economics_id": economicsID, "allocated_value_rub": "1000",
	}, http.StatusCreated)

	transaction := businessRequest(t, http.MethodPost, businessID, "/bank/transactions", map[string]any{
		"booked_on": time.Now().UTC().Format("2006-01-02"), "direction": "inbound", "amount_rub": "1000",
		"counterparty_name": "Integration Payer", "counterparty_inn": "1234567890", "classification": "client_income",
		"idempotency_key": "integration-bank-income",
	}, http.StatusCreated)
	transactionID := transaction["id"].(string)
	var autoMatchCount int
	if err := testPool.QueryRow(ctx, `
		SELECT count(*) FROM business_transaction_match
		WHERE business_id=$1 AND transaction_id=$2 AND target_type='receivable' AND target_id=$3 AND status='confirmed'
	`, businessID, transactionID, receivableID).Scan(&autoMatchCount); err != nil {
		t.Fatal(err)
	}
	if autoMatchCount != 1 {
		t.Fatalf("expected auto bank match to receivable, got count=%d", autoMatchCount)
	}
	var receivableStatus string
	var paidAmount string
	if err := testPool.QueryRow(ctx, `SELECT status, paid_amount_rub::text FROM business_receivable WHERE id=$1`, receivableID).Scan(&receivableStatus, &paidAmount); err != nil {
		t.Fatal(err)
	}
	if receivableStatus != "paid" || paidAmount != "1000.00" {
		t.Fatalf("expected receivable paid 1000.00, got status=%s paid=%s", receivableStatus, paidAmount)
	}

	payout := businessRequest(t, http.MethodPost, businessID, "/payouts", map[string]any{"period_key": month, "idempotency_key": "integration-payout"}, http.StatusCreated)
	payoutID := payout["id"].(string)
	businessRequest(t, http.MethodPost, businessID, "/payouts/"+payoutID+"/approve", nil, http.StatusOK)
	businessRequest(t, http.MethodPost, businessID, "/payouts/"+payoutID+"/submit-draft", nil, http.StatusAccepted)
	payoutTransaction := businessRequest(t, http.MethodPost, businessID, "/bank/transactions", map[string]any{
		"booked_on": time.Now().UTC().Format("2006-01-02"), "direction": "outbound", "amount_rub": "250",
		"counterparty_name": "Integration Worker", "classification": "payroll", "idempotency_key": "integration-bank-payout",
	}, http.StatusCreated)
	businessRequest(t, http.MethodPost, businessID, "/bank/transactions/"+payoutTransaction["id"].(string)+"/matches", map[string]any{
		"target_type": "payout", "target_id": payoutID, "amount_rub": "250", "status": "confirmed", "idempotency_key": "integration-payout-match",
	}, http.StatusCreated)

	for _, path := range []string{"/snapshot", "/dashboard?month=" + month, "/me/compensation"} {
		resp := authRequest(t, http.MethodGet, "/api/businesses/"+businessID+path, nil)
		defer resp.Body.Close()
		if resp.StatusCode != http.StatusOK {
			t.Fatalf("GET %s: expected 200, got %d", path, resp.StatusCode)
		}
	}

	var accrualStatus string
	if err := testPool.QueryRow(ctx, `SELECT status FROM business_accrual WHERE business_id=$1 AND task_economics_id=$2`, businessID, economicsID).Scan(&accrualStatus); err != nil {
		t.Fatal(err)
	}
	if accrualStatus != "paid" {
		t.Fatalf("expected accrual paid, got %s", accrualStatus)
	}
}

// Money can arrive late for a period, never long before it. A receipt from an
// earlier year used to settle a current period, because auto-matching only
// looked at the client and the open balance.
func TestBusinessAutoMatchIgnoresStalePayments(t *testing.T) {
	ctx := context.Background()
	var businessID, projectID string
	if err := testPool.QueryRow(ctx, `INSERT INTO business_account(name,owner_user_id) VALUES ('Stale Match Business',$1) RETURNING id`, testUserID).Scan(&businessID); err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { cleanupBusinessIntegrationFixture(context.Background(), t, businessID) })
	if _, err := testPool.Exec(ctx, `INSERT INTO business_account_member(business_id,user_id,role) VALUES ($1,$2,'owner')`, businessID, testUserID); err != nil {
		t.Fatal(err)
	}
	if _, err := testPool.Exec(ctx, `INSERT INTO business_workspace(business_id,workspace_id,kind) VALUES ($1,$2,'operational')`, businessID, testWorkspaceID); err != nil {
		t.Fatal(err)
	}
	if err := testPool.QueryRow(ctx, `INSERT INTO project(workspace_id,title,status) VALUES ($1,'Stale match support','in_progress') RETURNING id`, testWorkspaceID).Scan(&projectID); err != nil {
		t.Fatal(err)
	}

	client := businessRequest(t, http.MethodPost, businessID, "/clients", map[string]any{
		"canonical_name": "Stale Match Client", "status": "active", "primary_payment_channel": "bank",
	}, http.StatusCreated)
	clientID := client["id"].(string)
	businessRequest(t, http.MethodPost, businessID, "/clients/"+clientID+"/payers", map[string]any{
		"name": "Stale Match Payer", "inn": "7712345678", "status": "active", "payment_channel": "bank",
	}, http.StatusCreated)

	now := time.Now().UTC()
	month := now.Format("2006-01")
	businessRequest(t, http.MethodPost, businessID, "/agreements", map[string]any{
		"client_id": clientID, "project_id": nil, "service_type": "support",
		"agreement_key": "stale-match-support", "version": 1, "name": "Stale match support",
		"model": "fixed", "amount_rub": "7000", "invoice_day": 1, "due_days": 7,
		"period_months": 1, "payment_channel": "bank", "effective_from": now.Format("2006-01-02"),
		"status": "active", "terms": map[string]any{},
	}, http.StatusCreated)
	businessRequest(t, http.MethodPost, businessID, "/receivables/generate", map[string]any{"from_month": month, "months": 1}, http.StatusOK)

	matchCount := func() int {
		var count int
		if err := testPool.QueryRow(ctx, `
			SELECT count(*) FROM business_transaction_match m
			JOIN business_receivable r ON r.id = m.target_id AND m.target_type = 'receivable'
			WHERE r.business_id = $1 AND m.status = 'confirmed'
		`, businessID).Scan(&count); err != nil {
			t.Fatal(err)
		}
		return count
	}
	payment := func(bookedOn, amount, key string) {
		businessRequest(t, http.MethodPost, businessID, "/bank/transactions", map[string]any{
			"booked_on": bookedOn, "direction": "inbound", "amount_rub": amount,
			"counterparty_name": "Stale Match Payer", "counterparty_inn": "7712345678",
			"classification": "client_income", "idempotency_key": key,
		}, http.StatusCreated)
	}

	payment(now.AddDate(-1, 0, 0).Format("2006-01-02"), "7000", "stale-match-old")
	if got := matchCount(); got != 0 {
		t.Fatalf("a year-old receipt settled a current period: %d match(es)", got)
	}

	// The same amount arriving inside the period does match, and a payment that
	// bundles money we pass on settles only our part.
	payment(now.Format("2006-01-02"), "13000", "stale-match-current")
	if got := matchCount(); got != 1 {
		t.Fatalf("current payment did not match: %d match(es)", got)
	}
	var planned, paid, status string
	if err := testPool.QueryRow(ctx, `
		SELECT planned_amount_rub::text, paid_amount_rub::text, status
		FROM business_receivable WHERE business_id = $1
	`, businessID).Scan(&planned, &paid, &status); err != nil {
		t.Fatal(err)
	}
	if planned != "7000.00" || paid != "7000.00" || status != "paid" {
		t.Fatalf("expected 7000.00 of 7000.00 paid, got planned=%s paid=%s status=%s", planned, paid, status)
	}
}

// A capped agreement plans from the work actually tracked in the project's
// billing period. The cap only bounds that number when the client agreed to a
// hard limit; otherwise the real amount is planned and flagged for review.
func TestBusinessCapAgreementPlansFromTrackedWork(t *testing.T) {
	ctx := context.Background()
	var businessID, projectID string
	if err := testPool.QueryRow(ctx, `INSERT INTO business_account(name,owner_user_id) VALUES ('Cap Mode Business',$1) RETURNING id`, testUserID).Scan(&businessID); err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { cleanupBusinessIntegrationFixture(context.Background(), t, businessID) })
	if _, err := testPool.Exec(ctx, `INSERT INTO business_account_member(business_id,user_id,role) VALUES ($1,$2,'owner')`, businessID, testUserID); err != nil {
		t.Fatal(err)
	}
	if _, err := testPool.Exec(ctx, `INSERT INTO business_workspace(business_id,workspace_id,kind) VALUES ($1,$2,'operational')`, businessID, testWorkspaceID); err != nil {
		t.Fatal(err)
	}
	if err := testPool.QueryRow(ctx, `INSERT INTO project(workspace_id,title,status) VALUES ($1,'Cap mode support','in_progress') RETURNING id`, testWorkspaceID).Scan(&projectID); err != nil {
		t.Fatal(err)
	}

	// Plan into next month so the fixture cannot collide with receivables the
	// lifecycle test generates for the current one.
	now := time.Now().UTC()
	period := time.Date(now.Year(), now.Month(), 1, 0, 0, 0, 0, time.UTC).AddDate(0, 1, 0)
	month := period.Format("2006-01")
	start := period.Format("2006-01-02")
	if _, err := testPool.Exec(ctx, `
		INSERT INTO client_billing_period(project_id,workspace_id,starts_on,ends_on,total_rub)
		VALUES ($1,$2,$3::date,($3::date + interval '1 month')::date,1500)
	`, projectID, testWorkspaceID, start); err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() {
		if _, err := testPool.Exec(context.Background(), `DELETE FROM client_billing_period WHERE project_id=$1`, projectID); err != nil {
			t.Errorf("cleanup client_billing_period: %v", err)
		}
	})

	client := businessRequest(t, http.MethodPost, businessID, "/clients", map[string]any{
		"canonical_name": "Cap Mode Client", "status": "active", "primary_payment_channel": "bank",
	}, http.StatusCreated)
	clientID := client["id"].(string)
	businessRequest(t, http.MethodPut, businessID, "/clients/"+clientID+"/projects", map[string]any{
		"workspace_id": testWorkspaceID, "project_id": projectID, "service_type": "support", "billable": true,
	}, http.StatusOK)

	agreementID := func(key, capMode string) string {
		created := businessRequest(t, http.MethodPost, businessID, "/agreements", map[string]any{
			"client_id": clientID, "project_id": projectID, "service_type": "support",
			"agreement_key": key, "version": 1, "name": "Cap " + capMode,
			"model": "cap", "cap_rub": "1000", "cap_mode": capMode, "invoice_day": 1, "due_days": 7,
			"period_months": 1, "payment_channel": "bank", "effective_from": start,
			"status": "active", "terms": map[string]any{},
		}, http.StatusCreated)
		id, ok := created["id"].(string)
		if !ok {
			t.Fatalf("agreement %s: id missing in %v", key, created)
		}
		return id
	}
	strictID := agreementID("cap-mode-strict", "strict")
	advisoryID := agreementID("cap-mode-advisory", "advisory")

	businessRequest(t, http.MethodPost, businessID, "/receivables/generate", map[string]any{"from_month": month, "months": 1}, http.StatusOK)

	planned := func(id string) (amount, source string, needsReview bool) {
		if err := testPool.QueryRow(ctx, `
			SELECT planned_amount_rub::text, source, needs_review FROM business_receivable
			WHERE business_id=$1 AND agreement_id=$2 AND period_key=$3
		`, businessID, id, month).Scan(&amount, &source, &needsReview); err != nil {
			t.Fatal(err)
		}
		return amount, source, needsReview
	}

	amount, source, needsReview := planned(strictID)
	if amount != "1000.00" || source != "billing_period" || needsReview {
		t.Fatalf("hard limit: expected 1000.00 from billing_period without review, got %s/%s/%v", amount, source, needsReview)
	}
	amount, source, needsReview = planned(advisoryID)
	if amount != "1500.00" || source != "billing_period" || !needsReview {
		t.Fatalf("advisory limit: expected 1500.00 from billing_period flagged for review, got %s/%s/%v", amount, source, needsReview)
	}

	businessRequest(t, http.MethodPatch, businessID, "/agreements/"+strictID, map[string]any{"status": "expired"}, http.StatusOK)
	businessRequest(t, http.MethodPatch, businessID, "/agreements/"+strictID, map[string]any{"status": "archived"}, http.StatusBadRequest)
}

// A payment that names its invoice settles that invoice, whatever the dates and
// amounts suggest. The line below is unreachable by every other route: its
// period starts more than two weeks out, so the window that keeps stale
// receipts off current periods hides it, and the money would land on the
// nearest open line instead. That is the shape of both wrong settlements this
// matcher has made on real money.
func TestBusinessAutoMatchPrefersInvoiceNumber(t *testing.T) {
	ctx := context.Background()
	var businessID, projectID string
	if err := testPool.QueryRow(ctx, `INSERT INTO business_account(name,owner_user_id) VALUES ('Invoice Number Business',$1) RETURNING id`, testUserID).Scan(&businessID); err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { cleanupBusinessIntegrationFixture(context.Background(), t, businessID) })
	if _, err := testPool.Exec(ctx, `INSERT INTO business_account_member(business_id,user_id,role) VALUES ($1,$2,'owner')`, businessID, testUserID); err != nil {
		t.Fatal(err)
	}
	if _, err := testPool.Exec(ctx, `INSERT INTO business_workspace(business_id,workspace_id,kind) VALUES ($1,$2,'operational')`, businessID, testWorkspaceID); err != nil {
		t.Fatal(err)
	}
	if err := testPool.QueryRow(ctx, `INSERT INTO project(workspace_id,title,status) VALUES ($1,'Invoice number support','in_progress') RETURNING id`, testWorkspaceID).Scan(&projectID); err != nil {
		t.Fatal(err)
	}

	client := businessRequest(t, http.MethodPost, businessID, "/clients", map[string]any{
		"canonical_name": "Invoice Number Client", "status": "active", "primary_payment_channel": "bank",
	}, http.StatusCreated)
	clientID := client["id"].(string)
	businessRequest(t, http.MethodPost, businessID, "/clients/"+clientID+"/payers", map[string]any{
		"name": "Invoice Number Payer", "inn": "7798765432", "status": "active", "payment_channel": "bank",
	}, http.StatusCreated)

	now := time.Now().UTC()
	monthStart := time.Date(now.Year(), now.Month(), 1, 0, 0, 0, 0, time.UTC)
	thisMonth := monthStart.Format("2006-01")
	nextMonth := monthStart.AddDate(0, 1, 0).Format("2006-01")
	businessRequest(t, http.MethodPost, businessID, "/agreements", map[string]any{
		"client_id": clientID, "project_id": nil, "service_type": "support",
		"agreement_key": "invoice-number-support", "version": 1, "name": "Invoice number support",
		"model": "fixed", "amount_rub": "7000", "invoice_day": 1, "due_days": 7,
		"period_months": 1, "payment_channel": "bank", "effective_from": monthStart.Format("2006-01-02"),
		"status": "active", "terms": map[string]any{},
	}, http.StatusCreated)
	businessRequest(t, http.MethodPost, businessID, "/receivables/generate", map[string]any{"from_month": thisMonth, "months": 2}, http.StatusOK)

	// Only the far line carries the invoice. Both lines plan the same amount, so
	// nothing but the number can tell them apart.
	invoiceDate := now.Format("2006-01-02")
	command, err := testPool.Exec(ctx, `
		UPDATE business_receivable
		SET status = 'invoiced', elba_invoice_number = '93', elba_invoice_date = $2::date
		WHERE business_id = $1 AND period_key = $3
	`, businessID, invoiceDate, nextMonth)
	if err != nil {
		t.Fatal(err)
	}
	if command.RowsAffected() != 1 {
		t.Fatalf("expected one line for %s to invoice, updated %d", nextMonth, command.RowsAffected())
	}

	businessRequest(t, http.MethodPost, businessID, "/bank/transactions", map[string]any{
		"booked_on": now.Format("2006-01-02"), "direction": "inbound", "amount_rub": "7000",
		"counterparty_name": "Invoice Number Payer", "counterparty_inn": "7798765432",
		"classification": "client_income", "idempotency_key": "invoice-number-payment",
		"purpose": "Оплата по счету № 93 от " + now.Format("02.01.2006") + " за работы по сайту Сумма 7000-00",
	}, http.StatusCreated)

	settled := func(periodKey string) (string, string) {
		var paid, status string
		if err := testPool.QueryRow(ctx, `
			SELECT paid_amount_rub::text, status FROM business_receivable
			WHERE business_id = $1 AND period_key = $2
		`, businessID, periodKey).Scan(&paid, &status); err != nil {
			t.Fatal(err)
		}
		return paid, status
	}
	if paid, status := settled(nextMonth); paid != "7000.00" || status != "paid" {
		t.Fatalf("the invoice named in the purpose was not settled: paid=%s status=%s", paid, status)
	}
	if paid, status := settled(thisMonth); paid != "0.00" || status == "paid" {
		t.Fatalf("the money landed on the wrong line: paid=%s status=%s", paid, status)
	}

	var matches int
	if err := testPool.QueryRow(ctx, `
		SELECT count(*) FROM business_transaction_match m
		JOIN business_receivable r ON r.id = m.target_id AND m.target_type = 'receivable'
		WHERE r.business_id = $1 AND m.status = 'confirmed'
	`, businessID).Scan(&matches); err != nil {
		t.Fatal(err)
	}
	if matches != 1 {
		t.Fatalf("expected exactly one settlement, got %d", matches)
	}
}
