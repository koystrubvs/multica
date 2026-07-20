package handler

import (
	"strings"
	"testing"
)

func TestBusinessDashboardKeepsInvoiceAndPaidBucketsDisjoint(t *testing.T) {
	if strings.Contains(businessDashboardSQL, "status IN ('invoiced','partially_paid','paid')") {
		t.Fatal("paid receivables must not remain in the invoiced bucket")
	}
	if !strings.Contains(businessDashboardSQL, "status IN ('invoiced','partially_paid','overdue')") {
		t.Fatal("invoiced bucket must include only outstanding issued receivables")
	}
}

func TestBusinessDashboardDrilldownCountsOnlyActionableRows(t *testing.T) {
	if !strings.Contains(businessDashboardSQL, "t.direction = 'inbound'") {
		t.Fatal("unmatched receipts must exclude outbound transactions")
	}
	if !strings.Contains(businessDashboardSQL, "GREATEST(planned_amount_rub - paid_amount_rub, 0) > 0") {
		t.Fatal("overdue count must exclude zero-balance receivables")
	}
}

func TestBusinessSeriesSupportsDailyAndMonthlyBuckets(t *testing.T) {
	for _, marker := range []string{"$4::text = 'day'", "YYYY-MM-DD", "YYYY-MM"} {
		if !strings.Contains(businessSeriesSQL, marker) {
			t.Fatalf("business series is missing granularity marker %q", marker)
		}
	}
}
