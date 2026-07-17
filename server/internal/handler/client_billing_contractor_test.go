package handler

import (
	"math"
	"testing"
)

// twoDecimals reports whether a percent is expressible with at most 2 decimal
// places (Elba's per-line discount precision).
func twoDecimals(p float64) bool {
	return math.Abs(p*100-math.Round(p*100)) < 1e-9
}

// TestDistributeSubscriptionDiscountsAcceptance reproduces the issue's
// acceptance criterion under the Elba v1 contract (no negative lines): the
// subscription cap is reached by per-line discount percents. «Долголетие» =
// spina.spb.ru (80 100 ₽) + plastika.me (154 060 ₽), capped at 100 000 ₽.
func TestDistributeSubscriptionDiscountsAcceptance(t *testing.T) {
	prices := []float64{80_100, 154_060}
	pcts := distributeSubscriptionDiscounts(prices, 100_000)

	if len(pcts) != 2 {
		t.Fatalf("expected 2 discounts, got %d", len(pcts))
	}
	for i, p := range pcts {
		if p < 0 || p > 100 {
			t.Errorf("discount[%d] = %v out of range", i, p)
		}
		if !twoDecimals(p) {
			t.Errorf("discount[%d] = %v is not 2-decimal", i, p)
		}
	}
	payable := discountedTotalRub(prices, pcts)
	if math.Abs(payable-100_000) > 1e-9 {
		t.Errorf("payable after per-line discounts = %v, want exactly 100000", payable)
	}
}

func TestBuildContractorInvoiceItemsAcceptance(t *testing.T) {
	groups := []contractorInvoiceGroup{
		{ProjectTitle: "spina.spb.ru", Items: []elbaDocItem{
			{ProductName: "spina.spb.ru: SEO-аудит", Quantity: 1, Price: 80_100, UnitName: "усл"},
		}},
		{ProjectTitle: "plastika.me", Items: []elbaDocItem{
			{ProductName: "plastika.me: контент", Quantity: 1, Price: 154_060, UnitName: "усл"},
		}},
	}
	items, billTotal, grossTotal, withDiscount := buildContractorInvoiceItems(groups, "subscription", 100_000)

	if !withDiscount {
		t.Error("expected withDiscount=true when gross exceeds the fee")
	}
	if math.Abs(grossTotal-234_160) > 1e-9 {
		t.Errorf("grossTotal (акт full value) = %v, want 234160", grossTotal)
	}
	if math.Abs(billTotal-100_000) > 1e-9 {
		t.Errorf("billTotal = %v, want 100000", billTotal)
	}
	if len(items) != 2 {
		t.Fatalf("expected 2 lines (no negative discount line), got %d: %+v", len(items), items)
	}
	for _, it := range items {
		if it.Price < 0 {
			t.Errorf("line %q has negative price %v — forbidden by Elba v1", it.ProductName, it.Price)
		}
		if it.Discount < 0 || it.Discount > 100 || !twoDecimals(it.Discount) {
			t.Errorf("line %q discount %v invalid", it.ProductName, it.Discount)
		}
	}
	// The discounted line totals net to the fixed fee.
	prices := []float64{items[0].Price, items[1].Price}
	pcts := []float64{items[0].Discount, items[1].Discount}
	if net := discountedTotalRub(prices, pcts); math.Abs(net-100_000) > 1e-9 {
		t.Errorf("discounted line total = %v, want 100000", net)
	}
}

// Postpaid: no cap, no discounts, bill == gross.
func TestBuildContractorInvoiceItemsPostpaid(t *testing.T) {
	groups := []contractorInvoiceGroup{
		{ProjectTitle: "a", Items: []elbaDocItem{{ProductName: "a: x", Quantity: 1, Price: 5000, UnitName: "усл"}}},
		{ProjectTitle: "b", Items: []elbaDocItem{{ProductName: "b: y", Quantity: 1, Price: 3000, UnitName: "усл"}}},
	}
	items, billTotal, grossTotal, withDiscount := buildContractorInvoiceItems(groups, "postpaid", 0)
	if withDiscount {
		t.Error("postpaid must not apply discounts")
	}
	for _, it := range items {
		if it.Discount != 0 {
			t.Errorf("postpaid line %q got discount %v", it.ProductName, it.Discount)
		}
	}
	if billTotal != 8000 || grossTotal != 8000 {
		t.Errorf("billTotal=%v grossTotal=%v, want 8000/8000", billTotal, grossTotal)
	}
}

// Subscription but delivered value under the fee: no discount, bill == gross
// (same threshold as pushPeriodToElba — cap only when total > fee).
func TestBuildContractorInvoiceItemsUnderFee(t *testing.T) {
	groups := []contractorInvoiceGroup{
		{ProjectTitle: "a", Items: []elbaDocItem{{ProductName: "a: x", Quantity: 1, Price: 40_000, UnitName: "усл"}}},
	}
	items, billTotal, grossTotal, withDiscount := buildContractorInvoiceItems(groups, "subscription", 100_000)
	if withDiscount {
		t.Error("under-fee must not apply discounts")
	}
	if len(items) != 1 || items[0].Discount != 0 {
		t.Errorf("under-fee must leave the line undiscounted, got %+v", items)
	}
	if billTotal != 40_000 || grossTotal != 40_000 {
		t.Errorf("billTotal=%v grossTotal=%v, want 40000/40000", billTotal, grossTotal)
	}
}

// A larger, uneven spread: many lines, the last two balance to the fee exactly.
func TestDistributeSubscriptionDiscountsManyLines(t *testing.T) {
	prices := []float64{12_000, 3_450, 88_000, 5_600, 41_250}
	fee := 90_000.0
	pcts := distributeSubscriptionDiscounts(prices, fee)
	for i, p := range pcts {
		if p < 0 || p > 100 || !twoDecimals(p) {
			t.Errorf("discount[%d] = %v invalid", i, p)
		}
	}
	payable := discountedTotalRub(prices, pcts)
	// Exact-to-the-kopeck is not guaranteed for every input, but must land
	// within one kopeck-grid step of the coarsest line (well under 1 ₽ here).
	if math.Abs(payable-fee) > 1 {
		t.Errorf("payable = %v, want ~%v (±1 ₽)", payable, fee)
	}
}
