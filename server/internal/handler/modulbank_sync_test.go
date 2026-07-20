package handler

import (
	"context"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"testing"
	"time"
)

func TestMapModulbankOperation(t *testing.T) {
	mapped, active, err := mapModulbankOperation(modulbankOperation{
		ID:                "operation-1",
		Status:            "Received",
		Category:          "Debet",
		ContragentName:    "  ООО Клиент  ",
		ContragentINN:     "66 260 707 8800",
		Currency:          "RUR",
		Amount:            14500.25,
		BankAccountNumber: "40702810000000000001",
		PaymentPurpose:    " Оплата по счёту ",
		Executed:          "2026-07-16T00:00:00",
	})
	if err != nil {
		t.Fatal(err)
	}
	if !active || mapped.direction != "inbound" || mapped.amountCents != 1450025 {
		t.Fatalf("unexpected mapped operation: active=%v direction=%s amount=%d", active, mapped.direction, mapped.amountCents)
	}
	if mapped.row.Counterparty != "ООО Клиент" || mapped.row.INN != "662607078800" || mapped.row.Inflow != 1450025 {
		t.Fatalf("unexpected bank row: %#v", mapped.row)
	}
}

func TestMapModulbankOperationVoidsRejected(t *testing.T) {
	_, active, err := mapModulbankOperation(modulbankOperation{
		ID:       "operation-2",
		Status:   "RejectByBank",
		Category: "Credit",
		Currency: "RUR",
		Amount:   100,
		Created:  "2026-07-16T00:00:00",
	})
	if err != nil {
		t.Fatal(err)
	}
	if active {
		t.Fatal("rejected operation must not be active")
	}
}

func TestModulbankClientPaginates(t *testing.T) {
	pageCalls := 0
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if got := r.Header.Get("Authorization"); got != "Bearer test-token" {
			t.Fatalf("unexpected authorization header %q", got)
		}
		switch r.URL.Path {
		case "/account-info":
			_ = json.NewEncoder(w).Encode([]modulbankCompany{{BankAccounts: []modulbankAccount{{ID: "account-1"}}}})
		case "/operation-history/account-1":
			pageCalls++
			var request struct {
				Skip    int    `json:"skip"`
				Records int    `json:"records"`
				From    string `json:"from"`
			}
			if err := json.NewDecoder(r.Body).Decode(&request); err != nil {
				t.Fatal(err)
			}
			if request.Records != modulbankPageSize || request.From != "2026-07-01" {
				t.Fatalf("unexpected request: %#v", request)
			}
			count := modulbankPageSize
			if request.Skip > 0 {
				count = 1
			}
			operations := make([]modulbankOperation, count)
			for i := range operations {
				operations[i] = modulbankOperation{ID: "operation"}
			}
			_ = json.NewEncoder(w).Encode(operations)
		default:
			http.NotFound(w, r)
		}
	}))
	defer server.Close()

	syncer := &ModulbankSyncer{token: "test-token", baseURL: server.URL, httpClient: server.Client()}
	accounts, err := syncer.accounts(context.Background())
	if err != nil || len(accounts) != 1 {
		t.Fatalf("accounts: len=%d err=%v", len(accounts), err)
	}
	from := time.Date(2026, 7, 1, 0, 0, 0, 0, time.UTC)
	operations, err := syncer.operations(context.Background(), accounts[0].ID, &from)
	if err != nil {
		t.Fatal(err)
	}
	if len(operations) != modulbankPageSize+1 || pageCalls != 2 {
		t.Fatalf("unexpected pagination: rows=%d calls=%d", len(operations), pageCalls)
	}
}
