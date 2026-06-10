package handler

import (
	"math"
	"testing"

	db "github.com/multica-ai/multica/server/pkg/db/generated"
)

func TestComputePriceRub(t *testing.T) {
	cases := []struct {
		name                              string
		usd, fx, markup, minimum, rounding, want float64
	}{
		// 11.5 * 86.1 * 3 = 2970.45 -> ceil to next 50 step = 3000
		{"markup then round up", 11.5, 86.1, 3, 500, 50, 3000},
		// 0.5 * 80 * 3 = 120 -> below the floor -> 500 (already a 50-multiple)
		{"floor applies", 0.5, 80, 3, 500, 50, 500},
		// rounding disabled keeps the raw value (to cents)
		{"no rounding", 1, 90, 2, 0, 0, 180},
		// exact multiple must not be bumped a whole extra step by float noise
		{"exact multiple stays", 5, 100, 3, 500, 50, 1500},
		// tiny usage with zero floor still rounds up to one step
		{"tiny rounds to one step", 0.01, 90, 3, 0, 50, 50},
	}
	for _, tc := range cases {
		got := computePriceRub(tc.usd, tc.fx, tc.markup, tc.minimum, tc.rounding)
		if math.Abs(got-tc.want) > 1e-9 {
			t.Errorf("%s: computePriceRub(%v,%v,%v,%v,%v) = %v, want %v",
				tc.name, tc.usd, tc.fx, tc.markup, tc.minimum, tc.rounding, got, tc.want)
		}
	}
}

func TestComputeBillingUsageNoCachePricing(t *testing.T) {
	rows := []db.ListIssueUsageByModelRow{
		{
			Provider:         "anthropic",
			Model:            "claude-opus-4-8",
			InputTokens:      100_000,
			OutputTokens:     10_000,
			CacheReadTokens:  900_000,
			CacheWriteTokens: 50_000,
		},
	}
	lines, totalUSD, totalTokens := computeBillingUsage(rows)
	if len(lines) != 1 || !lines[0].Priced {
		t.Fatalf("expected one priced line, got %+v", lines)
	}
	// Input-class tokens (input + cache_read + cache_write = 1.05M) are all
	// billed at the full $5/M input price — the cache discount is NOT passed
	// on. Output: 10K at $25/M.
	want := 1.05*5.0 + 0.01*25.0 // 5.25 + 0.25 = 5.50
	if math.Abs(totalUSD-want) > 1e-9 {
		t.Errorf("totalUSD = %v, want %v", totalUSD, want)
	}
	if totalTokens != 1_060_000 {
		t.Errorf("totalTokens = %d, want 1060000", totalTokens)
	}
}

func TestComputeBillingUsageUnknownModel(t *testing.T) {
	rows := []db.ListIssueUsageByModelRow{
		{Provider: "acme", Model: "frontier-x", InputTokens: 1000, OutputTokens: 100},
	}
	lines, totalUSD, totalTokens := computeBillingUsage(rows)
	if totalUSD != 0 {
		t.Errorf("unknown model must contribute $0, got %v", totalUSD)
	}
	if totalTokens != 1100 {
		t.Errorf("totalTokens = %d, want 1100 (unpriced tokens still counted)", totalTokens)
	}
	if len(lines) != 1 || lines[0].Priced {
		t.Errorf("expected one unpriced line, got %+v", lines)
	}
}
