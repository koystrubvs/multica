package handler

import (
	"testing"
	"time"
)

func d(s string) time.Time {
	t, err := time.Parse("2006-01-02", s)
	if err != nil {
		panic(err)
	}
	return t
}

func TestPeriodBoundsFor(t *testing.T) {
	cases := []struct {
		name              string
		months, anchor    int
		today             string
		wantStart, wantEnd string
	}{
		{"anchor 1, mid-month", 1, 1, "2026-06-10", "2026-06-01", "2026-07-01"},
		{"anchor 1, on anchor day", 1, 1, "2026-06-01", "2026-06-01", "2026-07-01"},
		{"anchor 15, before anchor", 1, 15, "2026-06-10", "2026-05-15", "2026-06-15"},
		{"anchor 15, after anchor", 1, 15, "2026-06-20", "2026-06-15", "2026-07-15"},
		{"quarterly, before anchor", 3, 1, "2026-06-10", "2026-06-01", "2026-09-01"},
		{"anchor 28 across feb", 1, 28, "2026-03-05", "2026-02-28", "2026-03-28"},
	}
	for _, tc := range cases {
		start, end := periodBoundsFor(tc.months, tc.anchor, d(tc.today))
		if !start.Equal(d(tc.wantStart)) || !end.Equal(d(tc.wantEnd)) {
			t.Errorf("%s: got [%s, %s), want [%s, %s)",
				tc.name, start.Format("2006-01-02"), end.Format("2006-01-02"), tc.wantStart, tc.wantEnd)
		}
	}
}

func TestNextPeriodBounds(t *testing.T) {
	cases := []struct {
		name               string
		prevEnd            string
		months             int
		today              string
		wantStart, wantEnd string
	}{
		// normal succession: previous cycle just elapsed
		{"immediate successor", "2026-06-01", 1, "2026-06-10", "2026-06-01", "2026-07-01"},
		// several cycles elapsed with no activity — fast-forward
		{"fast forward", "2026-03-01", 1, "2026-06-10", "2026-06-01", "2026-07-01"},
		// early manual close: prev end still in the future -> keep future start
		{"early close keeps future start", "2026-07-01", 1, "2026-06-10", "2026-07-01", "2026-08-01"},
	}
	for _, tc := range cases {
		start, end := nextPeriodBounds(d(tc.prevEnd), tc.months, d(tc.today))
		if !start.Equal(d(tc.wantStart)) || !end.Equal(d(tc.wantEnd)) {
			t.Errorf("%s: got [%s, %s), want [%s, %s)",
				tc.name, start.Format("2006-01-02"), end.Format("2006-01-02"), tc.wantStart, tc.wantEnd)
		}
	}
}

func TestHighestCrossedThreshold(t *testing.T) {
	cases := []struct {
		percent, watermark, want int
	}{
		{10, 0, 0},    // below every threshold
		{30, 0, 25},   // crossed 25
		{30, 25, 0},   // 25 already alerted
		{80, 25, 75},  // jumped over 50 straight to 75 (single highest alert)
		{120, 75, 100},
		{120, 100, 0}, // everything already alerted
	}
	for _, tc := range cases {
		if got := highestCrossedThreshold(tc.percent, tc.watermark); got != tc.want {
			t.Errorf("highestCrossedThreshold(%d, %d) = %d, want %d", tc.percent, tc.watermark, got, tc.want)
		}
	}
}
