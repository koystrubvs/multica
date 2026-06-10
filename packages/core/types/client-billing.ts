// Agency-side client billing (fork feature). Mirrors the backend domain in
// server/internal/handler/client_billing.go: a per-project billing config and
// frozen per-issue price snapshots ("charges") taken when an issue reaches
// `done`. Entirely separate from the upstream cloud billing types in
// ./billing.ts.

export type ClientBillingMode = "postpaid" | "budget" | "subscription";

export type ClientBillingChargeStatus = "draft" | "confirmed" | "void";

export interface ClientBillingConfig {
  project_id: string;
  enabled: boolean;
  mode: ClientBillingMode;
  /** Agency markup K applied on top of the no-cache token cost. */
  markup: number;
  /** Per-task price floor, rubles. */
  min_price_rub: number;
  /** Ceil-rounding step, rubles. 0 disables rounding. */
  rounding_rub: number;
  /** Percent added to the CBR USD->RUB rate (internal fixed rate policy). */
  fx_markup_percent: number;
  /** mode=budget: soft cap for threshold alerts. 0 = unset. */
  budget_rub: number;
  /** mode=subscription: fixed fee per period. 0 = unset. */
  subscription_fee_rub: number;
  /** mode=subscription: fair-use cap on delivered work value. 0 = unset. */
  fair_use_rub: number;
  period_months: number;
  anchor_day: number;
  created_at: string;
  updated_at: string;
}

/** Partial update; omitted fields keep their current/default values. */
export interface ClientBillingConfigUpdate {
  enabled?: boolean;
  mode?: ClientBillingMode;
  markup?: number;
  min_price_rub?: number;
  rounding_rub?: number;
  fx_markup_percent?: number;
  budget_rub?: number;
  subscription_fee_rub?: number;
  fair_use_rub?: number;
  period_months?: number;
  anchor_day?: number;
}

/** One (provider, model) slice of an issue's usage, priced no-cache. */
export interface ClientBillingUsageLine {
  provider: string;
  model: string;
  input_tokens: number;
  output_tokens: number;
  cache_read_tokens: number;
  cache_write_tokens: number;
  nocache_usd: number;
  /** false = model missing from the price table; contributed $0. */
  priced: boolean;
}

export interface ClientBillingCharge {
  id: string;
  issue_id: string;
  project_id: string;
  workspace_id: string;
  usage: ClientBillingUsageLine[];
  /** Token cost at public list prices with NO cache discounts, USD. */
  nocache_usd: number;
  /** Effective USD->RUB rate frozen into the snapshot. */
  fx_rate: number;
  markup: number;
  price_rub: number;
  status: ClientBillingChargeStatus;
  adjusted_reason: string | null;
  confirmed_by: string | null;
  confirmed_at: string | null;
  created_at: string;
  updated_at: string;
  /** Present in project-level charge lists only. */
  issue_title?: string;
}
