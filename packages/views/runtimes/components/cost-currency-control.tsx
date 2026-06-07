"use client";

import { useState } from "react";
import { RefreshCw } from "lucide-react";
import { useCostCurrencyStore } from "@multica/core/runtimes/cost-currency-store";

// Compact USD->RUB rate editor shown above the cost KPIs on the dashboard
// and every runtime usage page. The cost tables/charts price tokens in USD
// (provider list prices) and multiply by this rate for display; editing it
// here updates every cost figure live because all cost-rendering components
// subscribe to the same store. Persisted in localStorage via the store.
//
// The "CBR" button pulls the current official USD->RUB rate from the Central
// Bank of Russia (via the CORS-friendly cbr-xml-daily.ru mirror of the CBR
// daily feed) and drops it into the field. It stays an editable override:
// the operator can still hand-tune the number after syncing. The fetch is
// client-side and best-effort -- a network/parse failure leaves the current
// value untouched and surfaces the reason in the button tooltip, since this
// is an internal cost-visibility figure, not an invoice.
const CBR_DAILY_URL = "https://www.cbr-xml-daily.ru/daily_json.js";

export function CostCurrencyControl() {
  const rubPerUsd = useCostCurrencyStore((s) => s.rubPerUsd);
  const setRubPerUsd = useCostCurrencyStore((s) => s.setRubPerUsd);
  const [syncing, setSyncing] = useState(false);
  const [error, setError] = useState<string | null>(null);

  const syncFromCbr = async () => {
    setSyncing(true);
    setError(null);
    try {
      const res = await fetch(CBR_DAILY_URL, { cache: "no-store" });
      if (!res.ok) throw new Error(`HTTP ${res.status}`);
      const data = await res.json();
      const value = data?.Valute?.USD?.Value;
      if (typeof value !== "number" || !Number.isFinite(value) || value <= 0) {
        throw new Error("bad payload");
      }
      // CBR quotes RUB per 1 USD -- exactly our rubPerUsd. Round to 2dp so the
      // field shows a tidy 73.47-style number rather than 73.4689.
      setRubPerUsd(Math.round(value * 100) / 100);
    } catch (e) {
      setError(e instanceof Error ? e.message : "fetch failed");
    } finally {
      setSyncing(false);
    }
  };

  return (
    <span className="inline-flex items-center gap-1.5">
      <label
        className="inline-flex items-center gap-1.5 text-xs text-muted-foreground"
        title="Курс USD→RUB для отображения стоимости агентов"
      >
        {/* eslint-disable-next-line i18next/no-literal-string -- currency symbols, not translatable copy */}
        <span className="font-medium uppercase tracking-wider">₽/$</span>
        <input
          type="number"
          min={1}
          step={1}
          value={rubPerUsd}
          onChange={(e) => setRubPerUsd(Number(e.target.value))}
          className="w-16 rounded-md border bg-background px-2 py-1 text-right tabular-nums text-foreground"
          aria-label="Курс рубля к доллару для отображения стоимости"
        />
      </label>
      <button
        type="button"
        onClick={syncFromCbr}
        disabled={syncing}
        title={
          error
            ? `Не удалось получить курс ЦБ РФ (${error})`
            : "Подставить актуальный курс ЦБ РФ"
        }
        aria-label="Синхронизировать курс с ЦБ РФ (cbr-xml-daily)"
        className={`inline-flex items-center gap-1 rounded-md border px-1.5 py-1 text-xs font-medium transition-colors disabled:cursor-not-allowed disabled:opacity-50 ${
          error
            ? "border-warning/40 text-warning hover:text-warning"
            : "border-border text-muted-foreground hover:text-foreground"
        }`}
      >
        <RefreshCw className={`h-3 w-3 ${syncing ? "animate-spin" : ""}`} />
        {/* eslint-disable-next-line i18next/no-literal-string -- short proper-noun label */}
        <span>ЦБ</span>
      </button>
    </span>
  );
}
