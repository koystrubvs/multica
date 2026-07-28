package handler

import (
	"bytes"
	"testing"
	"time"

	"github.com/xuri/excelize/v2"
)

func TestParseBusinessBankCSV(t *testing.T) {
	rows, err := parseBusinessBankCSV([]byte("account;date;counterparty;inn;inflow;outflow;purpose\n40817;19.07.2026;Client;1234567890;1 234,56;;Invoice 10\n"))
	if err != nil {
		t.Fatal(err)
	}
	if len(rows) != 1 || rows[0].Inflow != 123456 || rows[0].Counterparty != "Client" {
		t.Fatalf("unexpected rows: %#v", rows)
	}
	if got := businessBankDedupKey(rows[0], "inbound", rows[0].Inflow); len(got) != 64 {
		t.Fatalf("unexpected dedup key: %q", got)
	}
}

func TestParseBusinessBankXLSX(t *testing.T) {
	book := excelize.NewFile()
	sheet := book.GetSheetName(0)
	values := [][]any{
		{"account", "date", "counterparty", "inn", "inflow", "outflow", "purpose"},
		{"40817", "2026-07-19", "Client", "1234567890", "", "500.25", "Service"},
	}
	for row, cells := range values {
		for column, value := range cells {
			cell, _ := excelize.CoordinatesToCellName(column+1, row+1)
			if err := book.SetCellValue(sheet, cell, value); err != nil {
				t.Fatal(err)
			}
		}
	}
	var data bytes.Buffer
	if err := book.Write(&data); err != nil {
		t.Fatal(err)
	}
	rows, err := parseBusinessBankXLSX(data.Bytes())
	if err != nil {
		t.Fatal(err)
	}
	if len(rows) != 1 || rows[0].Outflow != 50025 {
		t.Fatalf("unexpected rows: %#v", rows)
	}
}

func TestBusinessFixedDecimal(t *testing.T) {
	percent, err := parseBusinessPercent("25.0000")
	if err != nil || percent != 250000 {
		t.Fatalf("percent=%d err=%v", percent, err)
	}
	signed, err := parseSignedBusinessMoney("-1234,56")
	if err != nil || signed != -123456 {
		t.Fatalf("signed=%d err=%v", signed, err)
	}
}

func TestPickAutoMatchReceivable(t *testing.T) {
	dueEarly := time.Date(2026, 7, 10, 0, 0, 0, 0, time.UTC)
	dueLate := time.Date(2026, 7, 24, 0, 0, 0, 0, time.UTC)

	t.Run("picks closest remaining under cap", func(t *testing.T) {
		id, ok := pickAutoMatchReceivable(5_000_000, []autoMatchReceivableCandidate{
			{ID: "seo", Remaining: 5_000_000, DueOn: &dueLate, PeriodKey: "2026-07"},
			{ID: "support", Remaining: 10_000_000, DueOn: &dueEarly, PeriodKey: "2026-07"},
		})
		if !ok || id != "seo" {
			t.Fatalf("got id=%q ok=%v, want seo", id, ok)
		}
	})

	t.Run("picks support for near-cap payment", func(t *testing.T) {
		id, ok := pickAutoMatchReceivable(9_820_000, []autoMatchReceivableCandidate{
			{ID: "seo", Remaining: 5_000_000, DueOn: &dueLate, PeriodKey: "2026-07"},
			{ID: "support", Remaining: 10_000_000, DueOn: &dueEarly, PeriodKey: "2026-07"},
		})
		if !ok || id != "support" {
			t.Fatalf("got id=%q ok=%v, want support", id, ok)
		}
	})

	t.Run("settles the oldest debt when amounts tie", func(t *testing.T) {
		// A recurring agreement generates identical future periods, so equal
		// remaining amounts are the norm rather than a sign of ambiguity.
		id, ok := pickAutoMatchReceivable(5_000_000, []autoMatchReceivableCandidate{
			{ID: "august", Remaining: 5_000_000, DueOn: &dueLate, PeriodKey: "2026-08"},
			{ID: "july", Remaining: 5_000_000, DueOn: &dueEarly, PeriodKey: "2026-07"},
		})
		if !ok || id != "july" {
			t.Fatalf("got id=%q ok=%v, want july", id, ok)
		}
	})

	t.Run("refuses when amount, due date and period all tie", func(t *testing.T) {
		id, ok := pickAutoMatchReceivable(5_000_000, []autoMatchReceivableCandidate{
			{ID: "a", Remaining: 5_000_000, DueOn: &dueEarly, PeriodKey: "2026-07"},
			{ID: "b", Remaining: 5_000_000, DueOn: &dueEarly, PeriodKey: "2026-07"},
		})
		if ok || id != "" {
			t.Fatalf("got id=%q ok=%v, want no match", id, ok)
		}
	})

	t.Run("prefers a dated receivable over an undated one", func(t *testing.T) {
		id, ok := pickAutoMatchReceivable(5_000_000, []autoMatchReceivableCandidate{
			{ID: "dated", Remaining: 5_000_000, DueOn: &dueEarly, PeriodKey: "2026-07"},
			{ID: "undated", Remaining: 5_000_000, PeriodKey: "2026-07"},
		})
		if !ok || id != "dated" {
			t.Fatalf("got id=%q ok=%v, want dated", id, ok)
		}
	})

	t.Run("rejects amount above remaining", func(t *testing.T) {
		id, ok := pickAutoMatchReceivable(6_000_000, []autoMatchReceivableCandidate{
			{ID: "seo", Remaining: 5_000_000, PeriodKey: "2026-07"},
		})
		if ok || id != "" {
			t.Fatalf("got id=%q ok=%v, want no match", id, ok)
		}
	})
}

func TestBusinessReceivablePaidStatus(t *testing.T) {
	if got := businessReceivablePaidStatus(100000, 98200, "cap"); got != "paid" {
		t.Fatalf("cap near-full: got %q", got)
	}
	if got := businessReceivablePaidStatus(100000, 90000, "cap"); got != "partially_paid" {
		t.Fatalf("cap below 95%%: got %q", got)
	}
	if got := businessReceivablePaidStatus(50000, 49000, "fixed"); got != "partially_paid" {
		t.Fatalf("fixed near-full: got %q", got)
	}
	if got := businessReceivablePaidStatus(50000, 50000, "fixed"); got != "paid" {
		t.Fatalf("fixed full: got %q", got)
	}
	if got := businessReceivablePaidStatus(100000, 0, "cap"); got != "" {
		t.Fatalf("unpaid: got %q", got)
	}
}

func TestBusinessCounterpartyTransactionClassification(t *testing.T) {
	tests := []struct {
		name           string
		classification string
		direction      string
		wantClass      string
		wantOK         bool
	}{
		{name: "client receipt", classification: "client_payer", direction: "inbound", wantClass: "client_income", wantOK: true},
		{name: "client payout rejected", classification: "client_payer", direction: "outbound", wantOK: false},
		{name: "worker payout", classification: "worker_payee", direction: "outbound", wantClass: "payroll", wantOK: true},
		{name: "worker receipt rejected", classification: "worker_payee", direction: "inbound", wantOK: false},
		{name: "vendor", classification: "vendor", direction: "outbound", wantClass: "service", wantOK: true},
		{name: "transit", classification: "transit", direction: "inbound", wantClass: "transfer", wantOK: true},
		{name: "ignored", classification: "ignored", direction: "outbound", wantClass: "transfer", wantOK: true},
	}
	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			gotClass, gotConfidence, gotOK := businessCounterpartyTransactionClassification(test.classification, test.direction)
			if gotClass != test.wantClass || gotOK != test.wantOK {
				t.Fatalf("class=%q ok=%v, want class=%q ok=%v", gotClass, gotOK, test.wantClass, test.wantOK)
			}
			if gotOK && gotConfidence != "confirmed" {
				t.Fatalf("confidence=%q, want confirmed", gotConfidence)
			}
		})
	}
}
