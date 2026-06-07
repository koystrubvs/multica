"use client";

import { useMemo } from "react";
import { useQuery } from "@tanstack/react-query";
import { fxDailyOptions } from "@multica/core/runtimes/queries";
import { FALLBACK_RUB_PER_USD } from "@multica/core/runtimes/fx";
import type { FxResolver } from "../runtimes/utils";

// Fetches the daily USD->RUB series for [fromIso, toIso] and returns a resolver
// that maps any YYYY-MM-DD to the exchange rate in effect that day. Cost is
// computed in USD throughout (per-model provider list prices); the ruble figure
// shown for a task is its USD cost times the rate of the day it ran — so
// historical spend is valued at the historical rate, not today's.
//
// The backend already returns a dense, ascending, carry-forward-filled series,
// so the hot path is a direct map hit; the `<= iso` scan only matters at the
// edges (a date before the first point, or a gap the backend somehow left).
export function useFxResolver(
  fromIso: string,
  toIso: string,
  workspaceId: string,
): { resolve: FxResolver; loaded: boolean } {
  const { data, isLoading } = useQuery(fxDailyOptions(fromIso, toIso, workspaceId));
  return useMemo(() => {
    const rates = data ?? [];
    const map = new Map(rates.map((r) => [r.date, r.usd_rub] as const));
    const dates = rates.map((r) => r.date); // ascending (backend ORDER BY date)
    const first = rates[0]?.usd_rub ?? 0;
    const resolve: FxResolver = (iso) => {
      const hit = map.get(iso);
      if (hit && hit > 0) return hit;
      let last = 0;
      for (const d of dates) {
        if (d <= iso) last = map.get(d) ?? 0;
        else break;
      }
      if (last > 0) return last;
      return first > 0 ? first : FALLBACK_RUB_PER_USD;
    };
    return { resolve, loaded: !isLoading };
  }, [data, isLoading]);
}
