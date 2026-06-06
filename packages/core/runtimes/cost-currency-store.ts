"use client";

import { create } from "zustand";
import { createJSONStorage, persist, type StateStorage } from "zustand/middleware";
import { defaultStorage } from "../platform/storage";

// USD->RUB display rate. Token cost is computed in USD throughout (the
// provider price tables in packages/views/runtimes/utils.ts are USD list
// prices); the ruble figure shown to the user is a pure presentation-layer
// multiply by this rate. Stored globally (not workspace-scoped) and
// persisted to localStorage so it survives reloads and is shared across the
// dashboard and every runtime page — same pattern as custom-pricing-store.
//
// 100 is a round, deliberately conservative default anchor; the operator
// edits it in the UI (CostCurrencyControl). It is intentionally NOT fetched
// from a live FX source: this is an internal cost-visibility figure, not an
// invoice, so a stable hand-set rate beats a moving target.
export const DEFAULT_RUB_PER_USD = 100;

const stateStorage = defaultStorage as unknown as StateStorage;

export interface CostCurrencyState {
  rubPerUsd: number;
  setRubPerUsd: (rate: number) => void;
}

export const useCostCurrencyStore = create<CostCurrencyState>()(
  persist(
    (set) => ({
      rubPerUsd: DEFAULT_RUB_PER_USD,
      setRubPerUsd: (rate) =>
        set(() => ({
          // Guard against NaN / <=0 from the editor so a fat-fingered input
          // can't zero out (or invert) every cost label in the app.
          rubPerUsd:
            Number.isFinite(rate) && rate > 0 ? rate : DEFAULT_RUB_PER_USD,
        })),
    }),
    {
      name: "multica_cost_currency_rub",
      storage: createJSONStorage(() => stateStorage),
    },
  ),
);

// Vanilla accessor for non-React callers, mirroring getCustomPricing.
export function getRubPerUsd(): number {
  return useCostCurrencyStore.getState().rubPerUsd;
}
