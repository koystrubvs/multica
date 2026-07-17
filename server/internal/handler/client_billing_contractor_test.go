package handler

import (
	"math"
	"testing"
)

// TestBuildContractorInvoiceItemsAcceptance reproduces the issue's acceptance
// criterion: «Долголетие» = spina.spb.ru (80 100 ₽) + plastika.me (154 060 ₽),
// subscription capped at 100 000 ₽. One consolidated invoice must bill
// 100 000 ₽ with an акт full value of 234 160 ₽ and a «Скидка по абонементу»
// line of −134 160 ₽.
func TestBuildContractorInvoiceItemsAcceptance(t *testing.T) {
	groups := []contractorInvoiceGroup{
		{ProjectTitle: "spina.spb.ru", Items: []elbaDocItem{
			{Name: "spina.spb.ru: SEO-аудит", Quantity: 1, Price: 80_100, Unit: "усл"},
		}},
		{ProjectTitle: "plastika.me", Items: []elbaDocItem{
			{Name: "plastika.me: контент", Quantity: 1, Price: 154_060, Unit: "усл"},
		}},
	}
	items, billTotal, grossTotal := buildContractorInvoiceItems(groups, "subscription", 100_000)

	if math.Abs(grossTotal-234_160) > 1e-9 {
		t.Errorf("grossTotal (акт full value) = %v, want 234160", grossTotal)
	}
	if math.Abs(billTotal-100_000) > 1e-9 {
		t.Errorf("billTotal = %v, want 100000", billTotal)
	}
	// two charge lines + one discount line
	if len(items) != 3 {
		t.Fatalf("expected 3 lines (2 charges + discount), got %d: %+v", len(items), items)
	}
	last := items[len(items)-1]
	if last.Name != "Скидка по абонементу" {
		t.Errorf("last line name = %q, want discount line", last.Name)
	}
	if math.Abs(last.Price-(-134_160)) > 1e-9 {
		t.Errorf("discount price = %v, want -134160", last.Price)
	}
	// The line list nets to the fixed fee (what the client pays).
	var net float64
	for _, it := range items {
		net += it.Price
	}
	if math.Abs(net-100_000) > 1e-9 {
		t.Errorf("net of all lines = %v, want 100000", net)
	}
}

// Postpaid: no cap, no discount line, bill == gross.
func TestBuildContractorInvoiceItemsPostpaid(t *testing.T) {
	groups := []contractorInvoiceGroup{
		{ProjectTitle: "a", Items: []elbaDocItem{{Name: "a: x", Quantity: 1, Price: 5000, Unit: "усл"}}},
		{ProjectTitle: "b", Items: []elbaDocItem{{Name: "b: y", Quantity: 1, Price: 3000, Unit: "усл"}}},
	}
	items, billTotal, grossTotal := buildContractorInvoiceItems(groups, "postpaid", 0)
	if len(items) != 2 {
		t.Errorf("postpaid must not add a discount line, got %d lines", len(items))
	}
	if billTotal != 8000 || grossTotal != 8000 {
		t.Errorf("billTotal=%v grossTotal=%v, want 8000/8000", billTotal, grossTotal)
	}
}

// Subscription but delivered value under the fee: no discount, bill == gross
// (a customer who under-uses the abonement is not billed the full fee here —
// same behaviour as pushPeriodToElba, which only discounts when total > fee).
func TestBuildContractorInvoiceItemsUnderFee(t *testing.T) {
	groups := []contractorInvoiceGroup{
		{ProjectTitle: "a", Items: []elbaDocItem{{Name: "a: x", Quantity: 1, Price: 40_000, Unit: "усл"}}},
	}
	items, billTotal, grossTotal := buildContractorInvoiceItems(groups, "subscription", 100_000)
	if len(items) != 1 {
		t.Errorf("under-fee must not add a discount line, got %d lines", len(items))
	}
	if billTotal != 40_000 || grossTotal != 40_000 {
		t.Errorf("billTotal=%v grossTotal=%v, want 40000/40000", billTotal, grossTotal)
	}
}
