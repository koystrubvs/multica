package handler

import "testing"

func TestBusinessRecurringCostNormalization(t *testing.T) {
	fields := businessRecurringCostFields{
		Name:      " CloudCode ",
		Category:  "ai",
		Amount:    "400",
		Currency:  "usd",
		ChargeDay: 15,
		StartsOn:  "2026-07-01",
		Status:    "active",
	}
	if err := fields.normalizeAndValidate(); err != nil {
		t.Fatal(err)
	}
	if fields.Name != "CloudCode" || fields.Amount != "400.00" || fields.Currency != "USD" {
		t.Fatalf("unexpected normalized fields: %#v", fields)
	}
}

func TestBusinessRecurringCostRejectsInvalidSchedule(t *testing.T) {
	fields := businessRecurringCostFields{
		Name:      "Cursor",
		Category:  "ai",
		Amount:    "60",
		Currency:  "USD",
		ChargeDay: 32,
		StartsOn:  "2026-07-15",
		Status:    "active",
	}
	if err := fields.normalizeAndValidate(); err == nil {
		t.Fatal("expected invalid schedule")
	}
}
