// Agency-side client billing (fork feature). Mirrors the backend domain in
// server/internal/handler/client_billing.go: a per-project billing config and
// frozen per-issue price snapshots ("charges") taken when an issue reaches
// `done`. Entirely separate from the upstream cloud billing types in
// ./billing.ts.

export type ClientBillingMode = "postpaid" | "budget" | "subscription";

export type ClientBillingChargeStatus = "draft" | "confirmed" | "void";

/** The four pricing knobs as actually applied to a snapshot, after the
 *  project-override -> workspace-default -> fallback inheritance chain. */
export interface ClientBillingEffectivePricing {
  markup: number;
  min_price_rub: number;
  rounding_rub: number;
  fx_markup_percent: number;
}

export interface ClientBillingConfig {
  project_id: string;
  enabled: boolean;
  mode: ClientBillingMode;
  /** Project override; null = inherited from the workspace defaults. */
  markup: number | null;
  min_price_rub: number | null;
  rounding_rub: number | null;
  fx_markup_percent: number | null;
  /** mode=budget: soft cap for threshold alerts. 0 = unset. */
  budget_rub: number;
  /** mode=subscription: fixed fee per period. 0 = unset. */
  subscription_fee_rub: number;
  /** mode=subscription: fair-use cap on delivered work value. 0 = unset. */
  fair_use_rub: number;
  period_months: number;
  anchor_day: number;
  /** Kontur Elba contractor linked to this project (invoicing target). */
  elba_contractor_id: string | null;
  /** Project-level bank account override (else workspace default). */
  elba_bank_account_id: string | null;
  /** Resolved pricing the next snapshot will use. */
  effective: ClientBillingEffectivePricing;
  created_at: string;
  updated_at: string;
}

/** Partial update. Pricing knobs are tri-state: omitted = keep, explicit
 *  null = reset to "inherit from workspace", number = project override. */
export interface ClientBillingConfigUpdate {
  enabled?: boolean;
  mode?: ClientBillingMode;
  markup?: number | null;
  min_price_rub?: number | null;
  rounding_rub?: number | null;
  fx_markup_percent?: number | null;
  budget_rub?: number;
  subscription_fee_rub?: number;
  fair_use_rub?: number;
  period_months?: number;
  anchor_day?: number;
  elba_contractor_id?: string | null;
  elba_bank_account_id?: string | null;
}

/** Workspace-level pricing defaults + Elba organization wiring. */
export interface ClientBillingWorkspaceConfig {
  workspace_id: string;
  markup: number;
  min_price_rub: number;
  rounding_rub: number;
  fx_markup_percent: number;
  elba_org_id: string | null;
  elba_bank_account_id: string | null;
  /** false = never saved; values are the hardcoded fallbacks. */
  exists: boolean;
}

export interface ClientBillingWorkspaceConfigUpdate {
  markup?: number;
  min_price_rub?: number;
  rounding_rub?: number;
  fx_markup_percent?: number;
  elba_org_id?: string;
  elba_bank_account_id?: string;
}

/** Contractor-level billing settings (migration 202): the mode + subscription
 *  cap applied to a whole Elba contractor's consolidated invoice, independent
 *  of the per-project metering configs. */
export interface ClientBillingContractorConfig {
  workspace_id: string;
  elba_contractor_id: string;
  name: string | null;
  mode: "postpaid" | "subscription";
  subscription_fee_rub: number;
  elba_bank_account_id: string | null;
  created_at: string;
  updated_at: string;
}

export interface ClientBillingContractorConfigUpdate {
  elba_contractor_id: string;
  name?: string;
  mode?: "postpaid" | "subscription";
  subscription_fee_rub?: number;
  elba_bank_account_id?: string;
}

/** One project's closed period inside an invoiceable contractor group. */
export interface ContractorPeriodGroupProject {
  period_id: string;
  project_id: string;
  project_title: string;
  total_rub: number;
}

/** Closed, uninvoiced periods for one (contractor, cycle) — one «Выставить
 *  счёт» button in the UI. */
export interface ContractorPeriodGroup {
  elba_contractor_id: string;
  starts_on: string;
  ends_on: string;
  total_rub: number;
  projects: ContractorPeriodGroupProject[];
}

/** Result of POST .../contractors/{id}/invoice — the consolidated счёт+акт. */
export interface ContractorInvoiceResult {
  contractor_id: string;
  starts_on: string;
  ends_on: string;
  mode: string;
  bill_id: string;
  act_id: string;
  gross_rub: number;
  bill_rub: number;
  period_ids: string[];
  period_count: number;
  act_error?: string;
}

/** Loosely-typed Kontur Elba directory entries (proxied via the backend). */
export interface ElbaEntity {
  id: string;
  name?: string;
  [key: string]: unknown;
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

export type ClientBillingChargeSource = "done_hook" | "sweep" | "manual";

export interface ClientBillingCharge {
  id: string;
  issue_id: string;
  project_id: string;
  workspace_id: string;
  /** Billing period the charge is attached to (sweep sets it immediately;
   *  legacy done_hook drafts get it on confirm). */
  period_id: string | null;
  /** Where the line came from (metering v2, migration 134). */
  source: ClientBillingChargeSource;
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

export type ClientBillingPeriodStatus = "open" | "ready" | "closed" | "invoiced" | "paid";

/** One invoicing cycle of a project (phase 2, migration 121). */
export interface ClientBillingPeriod {
  id: string;
  project_id: string;
  workspace_id: string;
  /** Date-only "YYYY-MM-DD" (may arrive with a time suffix — slice to 10). */
  starts_on: string;
  /** Exclusive upper bound, same format. */
  ends_on: string;
  status: ClientBillingPeriodStatus;
  total_rub: number;
  last_alert_percent: number;
  elba_invoice_id: string | null;
  elba_act_id: string | null;
  report_file: string | null;
  closed_at: string | null;
  paid_at: string | null;
  created_at: string;
  updated_at: string;
}

/** Business billing queue row (period + client/contractor context). */
export interface BusinessBillingRunCharge {
  id: string;
  issue_id: string;
  issue_title: string;
  price_rub: number;
  status: string;
  created_at: string;
}

export interface BusinessBillingRun {
  period_id: string;
  project_id: string;
  project_title: string;
  workspace_id: string;
  client_id: string | null;
  client_name: string;
  elba_contractor_id: string | null;
  billing_mode: string;
  anchor_day: number;
  starts_on: string;
  ends_on: string;
  status: ClientBillingPeriodStatus;
  total_rub: number;
  confirmed_total_rub: number;
  charge_count: number;
  draft_count: number;
  elba_invoice_id: string | null;
  elba_act_id: string | null;
  report_file: string | null;
  elba_invoice_url: string | null;
  elba_act_url: string | null;
  charges?: BusinessBillingRunCharge[];
  ready_on: string;
}

export interface BusinessBillingRunsResponse {
  runs: BusinessBillingRun[];
}

export interface ConfirmBusinessBillingPeriodResult {
  period: ClientBillingPeriod;
  economics_accepted?: number;
  report_file?: string;
  elba_invoice_id?: string | null;
  elba_act_id?: string | null;
  elba_invoice_url?: string | null;
  elba_act_url?: string | null;
  elba_error?: string;
  elba_skipped?: boolean;
}

/** Live progress for the cycle covering today (GET .../periods/current). */
export interface ClientBillingCurrentPeriod {
  period: ClientBillingPeriod;
  confirmed_total: number;
  draft_count: number;
  /** budget_rub (mode=budget) or fair_use_rub (mode=subscription); 0 = no cap. */
  limit_rub: number;
  percent: number;
}

// --- Metering v2 (migration 134) ---

/** One row of an issue's charge ledger as embedded in IssueBillingCost. */
export interface ClientBillingChargeSlim {
  id: string;
  period_id: string | null;
  price_rub: number;
  nocache_usd: number;
  status: ClientBillingChargeStatus;
  source: ClientBillingChargeSource;
  adjusted_reason: string | null;
  created_at: string;
}

export type ClientBillingDisputeStatus = "open" | "resolved";
export type ClientBillingDisputeResolution = "keep" | "exclude" | "adjust";

/** A client's "this price is wrong" claim on one issue. */
export interface ClientBillingDispute {
  id: string;
  issue_id: string;
  project_id: string;
  opened_by_type: "member" | "guest";
  opened_by: string | null;
  reason: string;
  status: ClientBillingDisputeStatus;
  resolution: ClientBillingDisputeResolution | null;
  resolution_comment: string | null;
  resolved_by: string | null;
  resolved_at: string | null;
  created_at: string;
  /** Present in project-level lists only. */
  issue_title?: string;
}

/** Live cost of an issue in ANY status (GET /issues/{id}/billing/cost).
 *  `internal` (the agency's cache-discounted cost) is present for the
 *  workspace OWNER only — it is the margin and must never reach clients. */
export interface IssueBillingCost {
  billing_enabled: boolean;
  usage: ClientBillingUsageLine[];
  total_tokens: number;
  nocache_usd: number;
  fx_rate: number;
  markup: number;
  estimate_rub: number;
  billed_rub: number;
  unbilled_nocache_usd: number;
  unbilled_estimate_rub: number;
  charges: ClientBillingChargeSlim[];
  dispute?: ClientBillingDispute;
  internal?: { usd: number; rub: number };
}
