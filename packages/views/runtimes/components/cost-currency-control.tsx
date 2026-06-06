"use client";

import { useCostCurrencyStore } from "@multica/core/runtimes/cost-currency-store";

// Compact USD->RUB rate editor shown above the cost KPIs on the dashboard
// and every runtime usage page. The cost tables/charts price tokens in USD
// (provider list prices) and multiply by this rate for display; editing it
// here updates every cost figure live because all cost-rendering components
// subscribe to the same store. Persisted in localStorage via the store.
export function CostCurrencyControl() {
  const rubPerUsd = useCostCurrencyStore((s) => s.rubPerUsd);
  const setRubPerUsd = useCostCurrencyStore((s) => s.setRubPerUsd);
  return (
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
  );
}
