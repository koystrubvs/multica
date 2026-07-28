export interface BusinessAccount {
  id: string;
  name: string;
  owner_user_id: string;
  currency: string;
  timezone: string;
  monthly_owner_income_target_rub: string;
  role: string;
  created_at: string;
  updated_at: string;
}

export interface BusinessDashboard {
  month: string;
  expected_rub: string;
  invoiced_rub: string;
  receivable_paid_rub: string;
  overdue_rub: string;
  not_invoiced_rub: string;
  bank_client_income_rub: string;
  vitmax_transit_rub: string;
  transfer_rub: string;
  unknown_inbound_rub: string;
  /** Money invoiced on the client's behalf and handed back; only the commission is ours. */
  transit_body_rub: string;
  transit_commission_rub: string;
  transit_net_rub: string;
  task_value_rub: string;
  participant_accrued_rub: string;
  company_target_pool_rub: string;
  owner_target_margin_rub: string;
  company_costs_rub: string;
  payable_rub: string;
  paid_to_workers_rub: string;
  reserve_balance_rub: string;
  reserve_obligation_rub: string;
  reserve_deficit_rub: string;
  owner_net_income_rub: string;
  owner_income_target_rub: string;
  owner_target_progress_pct: string;
  unmatched_count: number;
  overdue_count: number;
  generated_at: string;
}

export interface BusinessSeriesPoint {
  month: string;
  expected_rub: string;
  receivable_paid_rub: string;
  bank_income_rub: string;
  vitmax_transit_rub: string;
  unknown_inbound_rub: string;
}

export interface BusinessDashboardSeries {
  from: string;
  months: number;
  periods?: number;
  granularity?: "month" | "day";
  points: BusinessSeriesPoint[];
}

export type BusinessRow = Record<string, unknown>;

export interface BusinessSnapshot {
  clients: BusinessRow[];
  aliases: BusinessRow[];
  payers: BusinessRow[];
  projects: BusinessRow[];
  available_workspaces: BusinessRow[];
  available_projects: BusinessRow[];
  counterparties: BusinessRow[];
  agreements: BusinessRow[];
  receivables: BusinessRow[];
  bank_imports: BusinessRow[];
  transactions: BusinessRow[];
  matches: BusinessRow[];
  company_costs: BusinessRow[];
  recurring_costs: BusinessRow[];
  workers: BusinessRow[];
  policies: BusinessRow[];
  client_requests: BusinessRow[];
  task_economics: BusinessRow[];
  task_participants: BusinessRow[];
  receivable_tasks: BusinessRow[];
  accruals: BusinessRow[];
  quality_cases: BusinessRow[];
  accrual_adjustments: BusinessRow[];
  reserve_ledger: BusinessRow[];
  payout_batches: BusinessRow[];
  payout_items: BusinessRow[];
  bank_outbox: BusinessRow[];
  billing_candidates: BusinessRow[];
  billing_month_client_totals: BusinessRow[];
  billing_tasks: BusinessRow[];
  contracts: BusinessRow[];
  generated_at: string;
}

export interface BusinessBankImportResult {
  batch_id: string;
  status: string;
  rows_total: number;
  rows_inserted: number;
  rows_duplicate: number;
  rows_invalid: number;
  already_imported: boolean;
}
