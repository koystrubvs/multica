package handler

import (
	"bytes"
	"testing"

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
