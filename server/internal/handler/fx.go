package handler

import (
	"context"
	"fmt"
	"io"
	"log/slog"
	"net/http"
	"net/url"
	"regexp"
	"sort"
	"strconv"
	"strings"
	"time"
)

// fx.go — daily USD->RUB rates from the Bank of Russia (ЦБ РФ), used to value
// historical agent token cost at the rate that was in effect on the day each
// task ran (see migration 118). Token cost is computed in USD on the client;
// this endpoint only supplies the per-date multiplier.
//
// Design notes:
//   - Storage (fx_rate_daily) holds ONLY real CBR business-day rates. Weekend /
//     holiday / today-before-publish gaps are filled by carry-forward at READ
//     time, never persisted, so a provisional carry value can never get frozen
//     into history.
//   - Backfill is lazy: a request for [from,to] fetches from CBR only the
//     sub-range the table is missing, then upserts. Historical rows never
//     change, so after the first full-window load only the new tail (1-3 days)
//     is ever re-fetched.

// cbrDynamicBase is the CBR daily-dynamics endpoint. Plain HTTP on purpose:
// from this host cbr.ru's HTTPS handshake hangs (12s timeouts), while HTTP
// returns the feed instantly. The payload is public FX data — no secrecy lost
// — and we only read ASCII numeric fields out of it. A package var so tests can
// point it at an httptest server instead of the live CBR.
var cbrDynamicBase = "http://www.cbr.ru/scripts/XML_dynamic.asp"

// cbrUSDCode is CBR's internal currency code for the US dollar.
const cbrUSDCode = "R01235"

// fxFallbackRubPerUsd is the last-resort USD->RUB applied by the CLIENT when
// this endpoint yields nothing (CBR unreachable AND the table empty). Defined
// here only for documentation parity; the server never injects it.
const fxFallbackRubPerUsd = 90.0

// fxMaxRangeDays caps how far back a single request may reach, bounding both
// the CBR fetch and the response size.
const fxMaxRangeDays = 400

const isoLayout = "2006-01-02"

// fxRate is one (date, rate) point in the JSON response. Date is YYYY-MM-DD.
type fxRate struct {
	Date   string  `json:"date"`
	UsdRub float64 `json:"usd_rub"`
}

var isoDateRe = regexp.MustCompile(`^\d{4}-\d{2}-\d{2}$`)

// cbrRecordRe pulls (Date, Nominal, Value) out of each <Record> in the CBR
// XML. We scrape with a regex rather than encoding/xml because the feed is
// served as windows-1251 (Cyrillic in the root @name attribute) and the
// numeric fields we need are pure ASCII — sidestepping all charset handling.
var cbrRecordRe = regexp.MustCompile(
	`(?s)<Record\s+Date="(\d{2}\.\d{2}\.\d{4})"[^>]*>.*?<Nominal>(\d+)</Nominal>.*?<Value>([0-9,]+)</Value>`,
)

// GetFxDaily returns the per-date USD->RUB rate for [from,to] (inclusive),
// carry-forward filled so every calendar date in the range has a value.
//
//	GET /api/fx/daily?from=YYYY-MM-DD&to=YYYY-MM-DD
func (h *Handler) GetFxDaily(w http.ResponseWriter, r *http.Request) {
	from := r.URL.Query().Get("from")
	to := r.URL.Query().Get("to")
	if !isoDateRe.MatchString(from) || !isoDateRe.MatchString(to) || from > to {
		writeError(w, http.StatusBadRequest, "from/to must be YYYY-MM-DD with from <= to")
		return
	}
	// Clamp the lower bound so a stray ?from=1900-01-01 can't trigger a
	// century-wide CBR fetch.
	if toT, err := time.Parse(isoLayout, to); err == nil {
		minFrom := toT.AddDate(0, 0, -fxMaxRangeDays).Format(isoLayout)
		if from < minFrom {
			from = minFrom
		}
	}

	ctx := r.Context()
	if err := h.ensureFxRange(ctx, from, to); err != nil {
		// Non-fatal: serve whatever is already stored. A transient CBR outage
		// shouldn't blank out cost figures; carry-forward over stale rows is a
		// fine degradation for an internal cost-visibility view.
		slog.Warn("fx: lazy backfill failed, serving stored rates", "from", from, "to", to, "error", err)
	}

	stored, err := h.loadFxRange(ctx, from, to)
	if err != nil {
		writeError(w, http.StatusInternalServerError, "failed to load fx rates")
		return
	}
	writeJSON(w, http.StatusOK, fillCarryForward(from, to, stored))
}

// ensureFxRange fetches from CBR (and upserts) only the part of [from,to] the
// table is missing. Cheap and idempotent: the common steady-state path fetches
// just the few days since the last sync.
func (h *Handler) ensureFxRange(ctx context.Context, from, to string) error {
	minS, maxS, err := h.fxRangeBounds(ctx, from, to)
	if err != nil {
		return err
	}

	var fetchFrom, fetchTo string
	switch {
	case maxS == "":
		// Nothing stored in range — fetch the whole window once.
		fetchFrom, fetchTo = from, to
	case minS > from:
		// An older slice of the window has never been fetched; just refresh the
		// whole range (still one CBR call).
		fetchFrom, fetchTo = from, to
	case maxS < to:
		// Only the recent tail is missing. Re-fetch from the last stored day so
		// business days that landed since are captured.
		fetchFrom, fetchTo = maxS, to
	default:
		return nil // fully covered
	}

	recs, err := fetchCBRRates(ctx, fetchFrom, fetchTo)
	if err != nil {
		return err
	}
	return h.upsertFxRates(ctx, recs)
}

// fxRangeBounds returns min/max stored date within [from,to] as YYYY-MM-DD, or
// ("","") when the range holds no rows.
func (h *Handler) fxRangeBounds(ctx context.Context, from, to string) (string, string, error) {
	row := h.DB.QueryRow(ctx,
		`SELECT to_char(min(date),'YYYY-MM-DD'), to_char(max(date),'YYYY-MM-DD')
		   FROM fx_rate_daily
		  WHERE date BETWEEN $1::date AND $2::date`,
		from, to,
	)
	var minS, maxS *string
	if err := row.Scan(&minS, &maxS); err != nil {
		return "", "", err
	}
	if minS == nil || maxS == nil {
		return "", "", nil
	}
	return *minS, *maxS, nil
}

// loadFxRange reads stored (date, rate) points in [from,to], ascending.
func (h *Handler) loadFxRange(ctx context.Context, from, to string) ([]fxRate, error) {
	rows, err := h.DB.Query(ctx,
		`SELECT to_char(date,'YYYY-MM-DD'), usd_rub::float8
		   FROM fx_rate_daily
		  WHERE date BETWEEN $1::date AND $2::date
		  ORDER BY date`,
		from, to,
	)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	var out []fxRate
	for rows.Next() {
		var fr fxRate
		if err := rows.Scan(&fr.Date, &fr.UsdRub); err != nil {
			return nil, err
		}
		out = append(out, fr)
	}
	return out, rows.Err()
}

// upsertFxRates writes CBR points, overwriting an existing day's value (lets a
// provisional pre-publish day get corrected once CBR posts the real rate).
func (h *Handler) upsertFxRates(ctx context.Context, recs []fxRate) error {
	for _, rec := range recs {
		if rec.UsdRub <= 0 {
			continue
		}
		if _, err := h.DB.Exec(ctx,
			`INSERT INTO fx_rate_daily (date, usd_rub, source, updated_at)
			 VALUES ($1::date, $2, 'cbr', now())
			 ON CONFLICT (date) DO UPDATE
			   SET usd_rub = EXCLUDED.usd_rub, source = 'cbr', updated_at = now()`,
			rec.Date, rec.UsdRub,
		); err != nil {
			return err
		}
	}
	return nil
}

// fetchCBRRates pulls daily USD rates for [from,to] from the CBR dynamics feed.
// CBR only returns business days; weekend/holiday gaps are the caller's
// carry-forward concern.
func fetchCBRRates(ctx context.Context, from, to string) ([]fxRate, error) {
	d1, err := isoToCBR(from)
	if err != nil {
		return nil, err
	}
	d2, err := isoToCBR(to)
	if err != nil {
		return nil, err
	}
	q := url.Values{}
	q.Set("date_req1", d1)
	q.Set("date_req2", d2)
	q.Set("VAL_NM_RQ", cbrUSDCode)
	reqURL := cbrDynamicBase + "?" + q.Encode()

	reqCtx, cancel := context.WithTimeout(ctx, 12*time.Second)
	defer cancel()
	req, err := http.NewRequestWithContext(reqCtx, http.MethodGet, reqURL, nil)
	if err != nil {
		return nil, err
	}
	resp, err := http.DefaultClient.Do(req)
	if err != nil {
		return nil, err
	}
	defer resp.Body.Close()
	if resp.StatusCode != http.StatusOK {
		return nil, fmt.Errorf("cbr: unexpected status %d", resp.StatusCode)
	}
	body, err := io.ReadAll(io.LimitReader(resp.Body, 4<<20))
	if err != nil {
		return nil, err
	}

	matches := cbrRecordRe.FindAllStringSubmatch(string(body), -1)
	out := make([]fxRate, 0, len(matches))
	for _, m := range matches {
		iso, err := cbrToISO(m[1])
		if err != nil {
			continue
		}
		nominal, err := strconv.ParseFloat(m[2], 64)
		if err != nil || nominal <= 0 {
			continue
		}
		value, err := strconv.ParseFloat(strings.ReplaceAll(m[3], ",", "."), 64)
		if err != nil || value <= 0 {
			continue
		}
		// Value is RUB per `Nominal` units of the currency; Nominal is 1 for USD
		// but normalize defensively.
		out = append(out, fxRate{Date: iso, UsdRub: value / nominal})
	}
	return out, nil
}

// fillCarryForward returns a dense [from,to] series: every calendar date gets
// the most recent rate on or before it. Dates before the first stored point
// borrow that first point (better than a hole); an empty input yields an empty
// result and the client falls back to its own constant.
func fillCarryForward(from, to string, stored []fxRate) []fxRate {
	if len(stored) == 0 {
		return []fxRate{}
	}
	sort.Slice(stored, func(i, j int) bool { return stored[i].Date < stored[j].Date })

	fromT, err1 := time.Parse(isoLayout, from)
	toT, err2 := time.Parse(isoLayout, to)
	if err1 != nil || err2 != nil {
		return stored
	}

	idx := 0
	last := stored[0].UsdRub // seed so pre-first dates aren't holes
	out := make([]fxRate, 0, int(toT.Sub(fromT).Hours()/24)+1)
	for d := fromT; !d.After(toT); d = d.AddDate(0, 0, 1) {
		iso := d.Format(isoLayout)
		for idx < len(stored) && stored[idx].Date <= iso {
			last = stored[idx].UsdRub
			idx++
		}
		out = append(out, fxRate{Date: iso, UsdRub: last})
	}
	return out
}

// isoToCBR converts YYYY-MM-DD to CBR's DD/MM/YYYY query format.
func isoToCBR(iso string) (string, error) {
	t, err := time.Parse(isoLayout, iso)
	if err != nil {
		return "", err
	}
	return t.Format("02/01/2006"), nil
}

// cbrToISO converts CBR's DD.MM.YYYY record date to YYYY-MM-DD.
func cbrToISO(d string) (string, error) {
	t, err := time.Parse("02.01.2006", d)
	if err != nil {
		return "", err
	}
	return t.Format(isoLayout), nil
}
