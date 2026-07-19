"use client";

import { useMemo, useState } from "react";
import type { FormEvent } from "react";
import { useQuery } from "@tanstack/react-query";
import { api } from "@multica/core/api";
import { useFeatureEnabled } from "@multica/core/config";
import {
  BUSINESS_ACCRUALS_FLAG,
  BUSINESS_BANK_IMPORT_FLAG,
  BUSINESS_CALENDAR_FLAG,
  BUSINESS_CLIENTS_UI_FLAG,
  BUSINESS_CONTROL_PLANE_FLAG,
  BUSINESS_DASHBOARD_FLAG,
  BUSINESS_PAYOUT_BATCHES_FLAG,
  BUSINESS_TASK_ECONOMICS_ACCEPT_FLAG,
  BUSINESS_TASK_ECONOMICS_SHADOW_FLAG,
  MODULBANK_PAYOUT_DRAFTS_FLAG,
} from "@multica/core/feature-flags";
import type { BusinessRow } from "@multica/core/types";
import { Button } from "@multica/ui/components/ui/button";
import { Input } from "@multica/ui/components/ui/input";
import { Tabs, TabsList, TabsTrigger } from "@multica/ui/components/ui/tabs";
import { cn } from "@multica/ui/lib/utils";
import { AlertTriangle, Building2, RefreshCw, Upload } from "lucide-react";
import { useT } from "../i18n";
import { PageHeader } from "../layout/page-header";

type Tab = "overview" | "clients" | "calendar" | "bank" | "team" | "economics" | "accruals" | "payouts";

const selectClass = "h-8 min-w-0 rounded-lg border border-input bg-transparent px-2.5 py-1 text-sm outline-none transition-colors focus-visible:border-ring focus-visible:ring-3 focus-visible:ring-ring/50 dark:bg-input/30";

const SERVICE_TYPES = ["development", "support", "seo", "content"] as const;
const CLASSIFICATIONS = ["client_income", "payroll", "tax", "service", "transfer", "owner_draw", "vitmax_transit", "unknown"] as const;
const WORKER_ROLES = ["executor", "pm", "reviewer", "seo", "content", "copywriter", "designer", "domain_reviewer"] as const;

type TT = (key: string, options?: { defaultValue?: string }) => string;

type ColumnKind = "text" | "money" | "date" | "datetime" | "bool" | "enum" | "percent";
interface ColumnSpec {
  key: string;
  kind?: ColumnKind;
}

function normalizeColumn(column: string | ColumnSpec): ColumnSpec {
  return typeof column === "string" ? { key: column } : column;
}

function isTruthyFlag(value: unknown): boolean {
  return value === true || value === "t" || value === "true";
}

function text(row: BusinessRow, key: string): string {
  const value = row[key];
  if (value === null || value === undefined) return "—";
  if (typeof value === "object") return JSON.stringify(value);
  return String(value);
}

function rub(value: string | number | undefined): string {
  const amount = Number(value ?? 0);
  return new Intl.NumberFormat("ru-RU", { style: "currency", currency: "RUB", maximumFractionDigits: 0 }).format(Number.isFinite(amount) ? amount : 0);
}

function cellText(row: BusinessRow, spec: ColumnSpec, tt: TT, locale: string): string {
  const value = row[spec.key];
  if (value === null || value === undefined || value === "") return "—";
  switch (spec.kind) {
    case "money":
      return rub(String(value));
    case "date": {
      const parsed = new Date(String(value));
      return Number.isNaN(parsed.getTime()) ? String(value) : parsed.toLocaleDateString(locale);
    }
    case "datetime": {
      const parsed = new Date(String(value));
      return Number.isNaN(parsed.getTime()) ? String(value) : parsed.toLocaleString(locale, { dateStyle: "short", timeStyle: "short" });
    }
    case "bool":
      return isTruthyFlag(value) ? "✓" : "—";
    case "enum":
      return tt(`values.${String(value)}`, { defaultValue: String(value) });
    case "percent":
      return `${Number(value)}%`;
    default:
      return text(row, spec.key);
  }
}

function RowTable({ rows, columns, empty, tt, locale, extra }: {
  rows: BusinessRow[];
  columns: (string | ColumnSpec)[];
  empty: string;
  tt: TT;
  locale: string;
  extra?: { header: string; render: (row: BusinessRow) => React.ReactNode };
}) {
  const specs = columns.map(normalizeColumn);
  if (rows.length === 0) return <div className="rounded-lg border border-dashed p-8 text-center text-sm text-muted-foreground">{empty}</div>;
  return (
    <div className="overflow-x-auto rounded-lg border">
      <table className="w-full min-w-[720px] text-left text-sm">
        <thead className="bg-muted/60 text-xs text-muted-foreground">
          <tr>
            {specs.map((spec) => <th key={spec.key} className="px-3 py-2 font-medium">{tt(`columns.${spec.key}`, { defaultValue: spec.key.replaceAll("_", " ") })}</th>)}
            {extra && <th className="px-3 py-2 font-medium">{extra.header}</th>}
          </tr>
        </thead>
        <tbody>
          {rows.slice(0, 200).map((row, index) => (
            <tr key={String(row.id ?? index)} className="border-t align-top">
              {specs.map((spec) => {
                const value = cellText(row, spec, tt, locale);
                const warn = (spec.key === "is_overdue" && isTruthyFlag(row[spec.key])) || (spec.key === "needs_review" && isTruthyFlag(row[spec.key]));
                return <td key={spec.key} className={cn("max-w-[320px] truncate px-3 py-2 tabular-nums", warn && "font-medium text-warning")} title={value}>{value}</td>;
              })}
              {extra && <td className="px-3 py-2">{extra.render(row)}</td>}
            </tr>
          ))}
        </tbody>
      </table>
    </div>
  );
}

function FilterSelect({ label, value, onChange, options, allLabel }: {
  label: string;
  value: string;
  onChange: (value: string) => void;
  options: { value: string; label: string }[];
  allLabel: string;
}) {
  return (
    <label className="flex items-center gap-1.5 text-xs text-muted-foreground">
      <span>{label}</span>
      <select className={selectClass} value={value} onChange={(event) => onChange(event.target.value)}>
        <option value="">{allLabel}</option>
        {options.map((option) => <option key={option.value} value={option.value}>{option.label}</option>)}
      </select>
    </label>
  );
}

function Section({ title, actions, children }: { title: string; actions?: React.ReactNode; children: React.ReactNode }) {
  return <section className="space-y-3"><div className="flex flex-wrap items-center justify-between gap-2"><h2 className="text-sm font-medium">{title}</h2>{actions}</div>{children}</section>;
}

function Metric({ label, value, hint, warning }: { label: string; value: string; hint?: string; warning?: boolean }) {
  return <div className={cn("flex min-w-0 flex-col gap-1.5 rounded-lg border bg-card p-4", warning && "border-warning/50 bg-warning/5")}><div className="text-[11px] font-medium uppercase tracking-wider text-muted-foreground">{label}</div><div className="break-words text-xl font-semibold leading-tight tabular-nums sm:text-2xl">{value}</div>{hint && <div className="text-xs text-muted-foreground">{hint}</div>}</div>;
}

function formData(event: FormEvent<HTMLFormElement>): FormData {
  event.preventDefault();
  return new FormData(event.currentTarget);
}

function monthOf(value: unknown): string {
  return String(value ?? "").slice(0, 7);
}

export function BusinessPage() {
  const { t, i18n } = useT("business");
  const tt = t as unknown as TT;
  const locale = i18n?.language || "ru";
  const controlPlaneEnabled = useFeatureEnabled(BUSINESS_CONTROL_PLANE_FLAG);
  const dashboardEnabled = useFeatureEnabled(BUSINESS_DASHBOARD_FLAG);
  const enabled = controlPlaneEnabled && dashboardEnabled;
  const clientsEnabled = useFeatureEnabled(BUSINESS_CLIENTS_UI_FLAG);
  const calendarEnabled = useFeatureEnabled(BUSINESS_CALENDAR_FLAG);
  const bankEnabled = useFeatureEnabled(BUSINESS_BANK_IMPORT_FLAG);
  const economicsEnabled = useFeatureEnabled(BUSINESS_TASK_ECONOMICS_SHADOW_FLAG);
  const acceptEnabled = useFeatureEnabled(BUSINESS_TASK_ECONOMICS_ACCEPT_FLAG);
  const accrualsEnabled = useFeatureEnabled(BUSINESS_ACCRUALS_FLAG);
  const payoutsEnabled = useFeatureEnabled(BUSINESS_PAYOUT_BATCHES_FLAG);
  const bankDraftsEnabled = useFeatureEnabled(MODULBANK_PAYOUT_DRAFTS_FLAG);
  const [selectedBusiness, setSelectedBusiness] = useState("");
  const [month, setMonth] = useState(() => new Date().toISOString().slice(0, 7));
  const [tab, setTab] = useState<Tab>("overview");
  const [busy, setBusy] = useState("");
  const [message, setMessage] = useState("");
  const [error, setError] = useState("");

  const [scopeClient, setScopeClient] = useState("");
  const [scopeProject, setScopeProject] = useState("");
  const [scopeService, setScopeService] = useState("");

  const [clientStatusFilter, setClientStatusFilter] = useState("");
  const [counterpartyFilter, setCounterpartyFilter] = useState("");
  const [receivableStatusFilter, setReceivableStatusFilter] = useState("");
  const [receivableClientFilter, setReceivableClientFilter] = useState("");
  const [receivableReviewOnly, setReceivableReviewOnly] = useState(false);
  const [receivableOverdueOnly, setReceivableOverdueOnly] = useState(false);
  const [txClassFilter, setTxClassFilter] = useState("");
  const [txDirectionFilter, setTxDirectionFilter] = useState("");
  const [txSearch, setTxSearch] = useState("");
  const [econWorker, setEconWorker] = useState("");

  const accounts = useQuery({ queryKey: ["business", "accounts"], queryFn: () => api.listBusinessAccounts(), enabled });
  const businessID = selectedBusiness || accounts.data?.[0]?.id || "";
  const dashboard = useQuery({
    queryKey: ["business", businessID, "dashboard", month, scopeClient, scopeProject, scopeService],
    queryFn: () => api.getBusinessDashboard(businessID, month, {
      client_id: scopeClient || undefined,
      project_id: scopeProject || undefined,
      service_type: scopeService || undefined,
    }),
    enabled: enabled && !!businessID,
  });
  const snapshot = useQuery({ queryKey: ["business", businessID, "snapshot"], queryFn: () => api.getBusinessSnapshot(businessID), enabled: enabled && !!businessID });

  const execute = async (key: string, action: () => Promise<unknown>) => {
    setBusy(key); setError(""); setMessage("");
    try {
      await action();
      setMessage(t(($) => $.success));
      await Promise.all([snapshot.refetch(), dashboard.refetch()]);
    } catch (cause) {
      setError(cause instanceof Error ? cause.message : String(cause));
    } finally {
      setBusy("");
    }
  };

  const data = snapshot.data;

  const clientNameByID = useMemo(() => {
    const map = new Map<string, string>();
    for (const row of data?.clients ?? []) map.set(String(row.id), String(row.canonical_name ?? row.id));
    return map;
  }, [data?.clients]);

  const enrichedPayers = useMemo(() => (data?.payers ?? []).map((row) => ({ ...row, client_name: clientNameByID.get(String(row.client_id)) ?? String(row.client_id) })), [data?.payers, clientNameByID]);

  const filteredClients = useMemo(() => (data?.clients ?? []).filter((row) => !clientStatusFilter || String(row.status) === clientStatusFilter), [data?.clients, clientStatusFilter]);

  const filteredCounterparties = useMemo(() => (data?.counterparties ?? []).filter((row) => !counterpartyFilter || String(row.classification) === counterpartyFilter), [data?.counterparties, counterpartyFilter]);

  const filteredReceivables = useMemo(() => (data?.receivables ?? []).filter((row) =>
    (!receivableStatusFilter || String(row.status) === receivableStatusFilter)
    && (!receivableClientFilter || String(row.client_id) === receivableClientFilter)
    && (!receivableReviewOnly || isTruthyFlag(row.needs_review))
    && (!receivableOverdueOnly || isTruthyFlag(row.is_overdue))
  ), [data?.receivables, receivableStatusFilter, receivableClientFilter, receivableReviewOnly, receivableOverdueOnly]);

  const receivableTotals = useMemo(() => filteredReceivables.reduce<{ planned: number; paid: number }>((acc, row) => ({
    planned: acc.planned + Number(row.planned_amount_rub ?? 0),
    paid: acc.paid + Number(row.paid_amount_rub ?? 0),
  }), { planned: 0, paid: 0 }), [filteredReceivables]);

  const filteredTransactions = useMemo(() => {
    const needle = txSearch.trim().toLowerCase();
    return (data?.transactions ?? []).filter((row) =>
      (!txClassFilter || String(row.classification) === txClassFilter)
      && (!txDirectionFilter || String(row.direction) === txDirectionFilter)
      && (!needle || `${String(row.counterparty_name ?? "")} ${String(row.purpose ?? "")} ${String(row.counterparty_inn ?? "")}`.toLowerCase().includes(needle))
    );
  }, [data?.transactions, txClassFilter, txDirectionFilter, txSearch]);

  const transactionTotals = useMemo(() => filteredTransactions.reduce<{ inbound: number; outbound: number }>((acc, row) => {
    const amount = Number(row.amount_rub ?? 0);
    return String(row.direction) === "inbound"
      ? { ...acc, inbound: acc.inbound + amount }
      : { ...acc, outbound: acc.outbound + amount };
  }, { inbound: 0, outbound: 0 }), [filteredTransactions]);

  const clientBreakdown = useMemo(() => {
    const map = new Map<string, { client_name: string; planned_amount_rub: number; paid_amount_rub: number; overdue: number }>();
    for (const row of data?.receivables ?? []) {
      if (String(row.period_key) !== month) continue;
      const name = String(row.client_name ?? row.client_id);
      const entry = map.get(name) ?? { client_name: name, planned_amount_rub: 0, paid_amount_rub: 0, overdue: 0 };
      entry.planned_amount_rub += Number(row.planned_amount_rub ?? 0);
      entry.paid_amount_rub += Number(row.paid_amount_rub ?? 0);
      if (isTruthyFlag(row.is_overdue)) entry.overdue += 1;
      map.set(name, entry);
    }
    return [...map.values()].sort((a, b) => b.planned_amount_rub - a.planned_amount_rub);
  }, [data?.receivables, month]);

  const workerMonths = useMemo(() => {
    const map = new Map<string, { worker_name: string; month: string; accrued_rub: number; funded_rub: number; paid_rub: number }>();
    for (const row of data?.accruals ?? []) {
      const period = monthOf(row.created_at);
      const name = String(row.worker_name ?? row.worker_id);
      const key = `${name}|${period}`;
      const entry = map.get(key) ?? { worker_name: name, month: period, accrued_rub: 0, funded_rub: 0, paid_rub: 0 };
      entry.accrued_rub += Number(row.original_amount_rub ?? 0) + Number(row.adjustment_rub ?? 0);
      entry.funded_rub += Number(row.funded_rub ?? 0) + Number(row.reserve_funded_rub ?? 0);
      entry.paid_rub += Number(row.paid_rub ?? 0);
      map.set(key, entry);
    }
    return [...map.values()].sort((a, b) => b.month.localeCompare(a.month) || a.worker_name.localeCompare(b.worker_name));
  }, [data?.accruals]);

  const enrichedPayoutItems = useMemo(() => {
    const batches = new Map<string, BusinessRow>();
    for (const row of data?.payout_batches ?? []) batches.set(String(row.id), row);
    return (data?.payout_items ?? []).map((row) => {
      const batch = batches.get(String(row.payout_batch_id));
      return { ...row, period_key: batch?.period_key ?? "—", batch_status: batch?.status ?? "—" };
    });
  }, [data?.payout_items, data?.payout_batches]);

  const enrichedParticipants = useMemo(() => {
    const economics = new Map<string, BusinessRow>();
    for (const row of data?.task_economics ?? []) economics.set(String(row.id), row);
    return (data?.task_participants ?? []).map((row) => {
      const econ = economics.get(String(row.task_economics_id));
      return { ...row, issue_title: econ?.issue_title ?? "—", status: econ?.status ?? "—" };
    });
  }, [data?.task_participants, data?.task_economics]);

  if (!enabled) return <div className="flex min-h-[50vh] items-center justify-center p-8 text-muted-foreground">{t(($) => $.unavailable)}</div>;
  if (accounts.isLoading || (businessID && (dashboard.isLoading || snapshot.isLoading))) return <div className="flex min-h-[50vh] items-center justify-center p-8 text-muted-foreground">{t(($) => $.loading)}</div>;
  if (accounts.error || dashboard.error || snapshot.error) return <div className="p-8 text-destructive">{String(accounts.error ?? dashboard.error ?? snapshot.error)}</div>;
  if (!businessID || !data || !dashboard.data) return <div className="p-8 text-muted-foreground">{t(($) => $.empty)}</div>;

  const metrics = dashboard.data;
  const clientOptions = (data.clients ?? []).map((row) => ({ value: String(row.id), label: String(row.canonical_name) }));
  const projectOptions = (data.projects ?? []).map((row) => ({ value: String(row.project_id), label: String(row.project_title ?? row.project_id) }));
  const serviceOptions = SERVICE_TYPES.map((value) => ({ value, label: tt(`values.${value}`, { defaultValue: value }) }));
  const econWorkerRow = (data.workers ?? []).find((row) => String(row.id) === econWorker) ?? (data.workers ?? [])[0];
  const tabs: { key: Tab; enabled: boolean }[] = [
    { key: "overview", enabled: true }, { key: "clients", enabled: clientsEnabled }, { key: "calendar", enabled: calendarEnabled },
    { key: "bank", enabled: bankEnabled }, { key: "team", enabled: economicsEnabled }, { key: "economics", enabled: economicsEnabled },
    { key: "accruals", enabled: accrualsEnabled }, { key: "payouts", enabled: payoutsEnabled },
  ];
  const filterRows = `${t(($) => $.filters.rows)}: ${filteredTransactions.length}`;

  return (
    <div className="flex min-h-0 flex-1 flex-col">
      <PageHeader className="h-auto min-h-12 flex-wrap justify-between gap-2 px-5 py-2">
        <div className="flex min-w-0 items-center gap-2">
          <Building2 className="size-4 shrink-0 text-muted-foreground" />
          <h1 className="truncate text-sm font-medium">{t(($) => $.title)}</h1>
        </div>
        <div className="flex flex-wrap items-center gap-2">
          {accounts.data && accounts.data.length > 1 && <select aria-label={t(($) => $.title)} className={selectClass} value={businessID} onChange={(event) => setSelectedBusiness(event.target.value)}>{accounts.data.map((account) => <option key={account.id} value={account.id}>{account.name}</option>)}</select>}
          <label className="flex items-center gap-2 text-xs text-muted-foreground"><span>{t(($) => $.month)}</span><Input className="w-auto" type="month" value={month} onChange={(event) => setMonth(event.target.value)} /></label>
          <Button type="button" size="sm" variant="outline" onClick={() => void Promise.all([snapshot.refetch(), dashboard.refetch()])}><RefreshCw aria-hidden="true" />{t(($) => $.refresh)}</Button>
        </div>
      </PageHeader>

      <div data-testid="business-scroll-container" className="min-h-0 flex-1 overflow-y-auto">
        <div className="mx-auto w-full max-w-6xl space-y-5 p-4 sm:p-6">
      <p className="text-xs text-muted-foreground">{t(($) => $.subtitle)}</p>

      {(error || message) && <div className={cn("rounded-lg border p-3 text-sm", error ? "border-destructive/40 bg-destructive/5 text-destructive" : "border-emerald-500/40 bg-emerald-500/5 text-emerald-700")}>{error || message}</div>}

      <Tabs value={tab} onValueChange={(value) => setTab(value as Tab)}>
        <TabsList variant="line" className="max-w-full justify-start overflow-x-auto">
          {tabs.filter((item) => item.enabled).map(({ key }) => <TabsTrigger key={key} value={key}>{t(($) => $.tabs[key])}</TabsTrigger>)}
        </TabsList>
      </Tabs>

      {tab === "overview" && <div className="space-y-6">
        <div className="flex flex-wrap items-center gap-3 rounded-lg border bg-muted/30 p-3">
          <FilterSelect label={t(($) => $.filters.client)} value={scopeClient} onChange={setScopeClient} options={clientOptions} allLabel={t(($) => $.filters.all)} />
          <FilterSelect label={t(($) => $.filters.project)} value={scopeProject} onChange={setScopeProject} options={projectOptions} allLabel={t(($) => $.filters.all)} />
          <FilterSelect label={t(($) => $.filters.service)} value={scopeService} onChange={setScopeService} options={serviceOptions} allLabel={t(($) => $.filters.all)} />
          {(scopeClient || scopeProject || scopeService) && <Button type="button" size="sm" variant="ghost" onClick={() => { setScopeClient(""); setScopeProject(""); setScopeService(""); }}>{t(($) => $.actions.clear_filters)}</Button>}
        </div>

        <Section title={t(($) => $.metric_groups.calendar)}>
          <div className="grid gap-3 sm:grid-cols-2 xl:grid-cols-5">
            <Metric label={t(($) => $.metrics.expected)} value={rub(metrics.expected_rub)} />
            <Metric label={t(($) => $.metrics.invoiced)} value={rub(metrics.invoiced_rub)} />
            <Metric label={t(($) => $.metrics.receivable_paid)} value={rub(metrics.receivable_paid_rub)} />
            <Metric label={t(($) => $.metrics.overdue)} value={rub(metrics.overdue_rub)} hint={`${t(($) => $.filters.rows)}: ${metrics.overdue_count ?? 0}`} warning={Number(metrics.overdue_rub) > 0} />
            <Metric label={t(($) => $.metrics.not_invoiced)} value={rub(metrics.not_invoiced_rub)} warning={Number(metrics.not_invoiced_rub) > 0} />
          </div>
        </Section>

        <Section title={t(($) => $.metric_groups.bank)}>
          <div className="grid gap-3 sm:grid-cols-2 xl:grid-cols-4">
            <Metric label={t(($) => $.metrics.client_income)} value={rub(metrics.bank_client_income_rub)} />
            <Metric label={t(($) => $.metrics.unknown)} value={rub(metrics.unknown_inbound_rub)} hint={`${t(($) => $.filters.rows)}: ${metrics.unmatched_count ?? 0}`} warning={(metrics.unmatched_count ?? 0) > 0} />
            <Metric label={t(($) => $.values.vitmax)} value={rub(metrics.vitmax_transit_rub)} />
            <Metric label={t(($) => $.values.transfers)} value={rub(metrics.transfer_rub)} />
          </div>
        </Section>

        <Section title={t(($) => $.metric_groups.economics)}>
          <div className="grid gap-3 sm:grid-cols-2 xl:grid-cols-4">
            <Metric label={t(($) => $.metrics.task_value)} value={rub(metrics.task_value_rub)} />
            <Metric label={t(($) => $.metrics.participant_accrued)} value={rub(metrics.participant_accrued_rub)} />
            <Metric label={t(($) => $.metrics.company_pool)} value={rub(metrics.company_target_pool_rub)} hint={t(($) => $.metrics.company_costs) + ": " + rub(metrics.company_costs_rub)} />
            <Metric label={t(($) => $.metrics.owner_margin)} value={rub(metrics.owner_target_margin_rub)} />
          </div>
        </Section>

        <Section title={t(($) => $.metric_groups.team)}>
          <div className="grid gap-3 sm:grid-cols-2 xl:grid-cols-4">
            <Metric label={t(($) => $.metrics.payable)} value={rub(metrics.payable_rub)} />
            <Metric label={t(($) => $.metrics.paid_workers)} value={rub(metrics.paid_to_workers_rub)} />
            <Metric label={t(($) => $.metrics.reserve)} value={rub(metrics.reserve_balance_rub)} warning={Number(metrics.reserve_deficit_rub) > 0} />
            <Metric label={t(($) => $.metrics.reserve_obligation)} value={rub(metrics.reserve_obligation_rub)} />
          </div>
        </Section>

        <Section title={t(($) => $.metric_groups.summary)}>
          <div className="grid gap-3 sm:grid-cols-2 xl:grid-cols-3">
            <Metric label={t(($) => $.metrics.owner_net)} value={rub(metrics.owner_net_income_rub)} />
            <Metric label={t(($) => $.metrics.target)} value={`${metrics.owner_target_progress_pct}%`} hint={rub(metrics.owner_income_target_rub)} />
            <Metric label={t(($) => $.metrics.company_costs)} value={rub(metrics.company_costs_rub)} />
          </div>
        </Section>

        {clientBreakdown.length > 0 && <Section title={t(($) => $.sections.by_client)}>
          <RowTable tt={tt} locale={locale} rows={clientBreakdown as unknown as BusinessRow[]} columns={[{ key: "client_name" }, { key: "planned_amount_rub", kind: "money" }, { key: "paid_amount_rub", kind: "money" }, { key: "overdue" }]} empty={t(($) => $.empty)} />
        </Section>}

        <div className="grid gap-3 md:grid-cols-2">
          <div className="rounded-lg border p-4 text-sm text-muted-foreground"><AlertTriangle className="mr-2 inline size-4 text-warning"/>{t(($) => $.vitmax_note)}</div>
          <div className="rounded-lg border p-4 text-sm text-muted-foreground">{t(($) => $.no_penalties_note)}</div>
        </div>
      </div>}

      {tab === "clients" && clientsEnabled && <div className="space-y-8">
        <Section title={t(($) => $.sections.clients)} actions={<div className="flex flex-wrap items-center gap-3">
          <FilterSelect label={t(($) => $.filters.status)} value={clientStatusFilter} onChange={setClientStatusFilter} options={["active", "prospect", "paused", "leaving", "lost"].map((value) => ({ value, label: tt(`values.${value}`, { defaultValue: value }) }))} allLabel={t(($) => $.filters.all)} />
          <form className="flex flex-wrap gap-2" onSubmit={(event) => { const fd=formData(event); void execute("client", () => api.businessAction(businessID,"clients",{canonical_name:fd.get("name"),status:"active",primary_payment_channel:"bank"})); event.currentTarget.reset(); }}><Input required name="name" className="w-52" placeholder={t(($) => $.fields.name)}/><Button disabled={busy!==""}>{t(($) => $.actions.add_client)}</Button></form>
        </div>}>
          <RowTable tt={tt} locale={locale} rows={filteredClients} columns={[{ key: "canonical_name" }, { key: "status", kind: "enum" }, { key: "primary_payment_channel", kind: "enum" }, { key: "notes" }]} empty={t(($) => $.empty)} />
        </Section>
        <Section title={t(($) => $.sections.payers)}>
          <RowTable tt={tt} locale={locale} rows={enrichedPayers} columns={[{ key: "client_name" }, { key: "name" }, { key: "inn" }, { key: "payment_channel", kind: "enum" }, { key: "status", kind: "enum" }]} empty={t(($) => $.empty)} />
        </Section>
        <Section title={t(($) => $.sections.projects)} actions={<form className="grid gap-2 sm:grid-cols-5" onSubmit={(event)=>{const fd=formData(event);const client=String(fd.get("client"));void execute("map",()=>api.businessAction(businessID,`clients/${client}/projects`,{workspace_id:fd.get("workspace"),project_id:fd.get("project"),service_type:fd.get("service"),billable:true},"PUT"));}}>
          <select required name="client" className={selectClass}>{(data.clients ?? []).map((row)=><option key={text(row,"id")} value={text(row,"id")}>{text(row,"canonical_name")}</option>)}</select><Input required name="workspace" placeholder={t(($)=>$.fields.workspace_id)}/><Input required name="project" placeholder={t(($)=>$.fields.project_id)}/><select name="service" className={selectClass}>{SERVICE_TYPES.map((value)=><option key={value} value={value}>{tt(`values.${value}`, { defaultValue: value })}</option>)}</select><Button disabled={busy!==""}>{t(($)=>$.actions.map_project)}</Button>
        </form>}><RowTable tt={tt} locale={locale} rows={data.projects ?? []} columns={[{ key: "client_name" }, { key: "project_title" }, { key: "workspace_name" }, { key: "service_type", kind: "enum" }, { key: "billable", kind: "bool" }]} empty={t(($)=>$.empty)}/></Section>
        <Section title={t(($) => $.sections.counterparties)} actions={<FilterSelect label={t(($) => $.filters.classification)} value={counterpartyFilter} onChange={setCounterpartyFilter} options={["client_payer", "worker_payee", "vendor", "transit", "ignored", "unresolved"].map((value) => ({ value, label: tt(`values.${value}`, { defaultValue: value }) }))} allLabel={t(($) => $.filters.all)} />}>
          <RowTable tt={tt} locale={locale} rows={filteredCounterparties} columns={[{ key: "name" }, { key: "inn" }, { key: "classification", kind: "enum" }, { key: "confidence", kind: "enum" }, { key: "reason" }]} empty={t(($) => $.empty)} />
        </Section>
      </div>}

      {tab === "calendar" && calendarEnabled && <div className="space-y-8">
        <Section title={t(($) => $.sections.agreements)} actions={<form className="grid gap-2 md:grid-cols-4" onSubmit={(event)=>{const fd=formData(event);void execute("agreement",()=>api.businessAction(businessID,"agreements",{client_id:fd.get("client"),service_type:fd.get("service"),agreement_key:fd.get("key"),version:1,name:fd.get("name"),model:"fixed",amount_rub:fd.get("amount"),due_days:7,period_months:1,payment_channel:"bank",effective_from:fd.get("date"),status:"active",is_estimate:false,needs_review:false,terms:{}}));}}>
          <select required name="client" className={selectClass}>{(data.clients ?? []).map((row)=><option key={text(row,"id")} value={text(row,"id")}>{text(row,"canonical_name")}</option>)}</select><Input required name="name" placeholder={t(($)=>$.fields.name)}/><Input required name="key" placeholder={t(($)=>$.fields.agreement_key)}/><select name="service" className={selectClass}>{SERVICE_TYPES.map((value)=><option key={value} value={value}>{tt(`values.${value}`, { defaultValue: value })}</option>)}</select><Input required name="amount" inputMode="decimal" placeholder={t(($)=>$.fields.amount)}/><Input required name="date" type="date"/><Button disabled={busy!==""}>{t(($)=>$.actions.add_agreement)}</Button>
        </form>}>
          <RowTable tt={tt} locale={locale} rows={data.agreements ?? []} columns={[{ key: "client_name" }, { key: "name" }, { key: "service_type", kind: "enum" }, { key: "model", kind: "enum" }, { key: "amount_rub", kind: "money" }, { key: "hourly_rate_rub", kind: "money" }, { key: "cap_rub", kind: "money" }, { key: "invoice_day" }, { key: "status", kind: "enum" }, { key: "needs_review", kind: "bool" }]} empty={t(($) => $.empty)} />
        </Section>
        <Section title={t(($)=>$.sections.receivables)} actions={<div className="flex flex-wrap items-center gap-3">
          <FilterSelect label={t(($) => $.filters.status)} value={receivableStatusFilter} onChange={setReceivableStatusFilter} options={["expected", "invoiced", "partially_paid", "paid", "skipped", "written_off"].map((value) => ({ value, label: tt(`values.${value}`, { defaultValue: value }) }))} allLabel={t(($) => $.filters.all)} />
          <FilterSelect label={t(($) => $.filters.client)} value={receivableClientFilter} onChange={setReceivableClientFilter} options={clientOptions} allLabel={t(($) => $.filters.all)} />
          <label className="flex items-center gap-1.5 text-xs text-muted-foreground"><input type="checkbox" checked={receivableReviewOnly} onChange={(event) => setReceivableReviewOnly(event.target.checked)} />{t(($) => $.filters.only_review)}</label>
          <label className="flex items-center gap-1.5 text-xs text-muted-foreground"><input type="checkbox" checked={receivableOverdueOnly} onChange={(event) => setReceivableOverdueOnly(event.target.checked)} />{t(($) => $.filters.only_overdue)}</label>
          <form className="flex flex-wrap gap-2" onSubmit={(event)=>{const fd=formData(event);void execute("receivables",()=>api.businessAction(businessID,"receivables/generate",{from_month:fd.get("month"),months:Number(fd.get("months"))}));}}><Input name="month" type="month" defaultValue={month} className="w-auto"/><Input name="months" type="number" min="1" max="24" defaultValue="3" className="w-20"/><Button disabled={busy!==""}>{t(($)=>$.actions.generate_receivables)}</Button></form>
        </div>}>
          <div className="space-y-2">
            <div className="text-xs text-muted-foreground">{t(($) => $.filters.rows)}: {filteredReceivables.length} · {t(($) => $.metrics.expected)}: {rub(receivableTotals.planned)} · {t(($) => $.metrics.receivable_paid)}: {rub(receivableTotals.paid)}</div>
            <RowTable tt={tt} locale={locale} rows={filteredReceivables} columns={[{ key: "period_key" }, { key: "client_name" }, { key: "project_title" }, { key: "planned_amount_rub", kind: "money" }, { key: "paid_amount_rub", kind: "money" }, { key: "invoice_on", kind: "date" }, { key: "due_on", kind: "date" }, { key: "status", kind: "enum" }, { key: "is_overdue", kind: "bool" }, { key: "needs_review", kind: "bool" }]} empty={t(($)=>$.empty)}/>
          </div>
        </Section>
      </div>}

      {tab === "bank" && bankEnabled && <div className="space-y-8">
        <Section title={t(($)=>$.actions.import_statement)}><form className="flex flex-wrap items-end gap-2" onSubmit={(event)=>{const fd=formData(event);const file=fd.get("file");if(file instanceof File)void execute("bank",()=>api.importBusinessBankFile(businessID,file));}}><label className="grid gap-1 text-xs text-muted-foreground">{t(($)=>$.fields.file)}<Input required name="file" type="file" accept=".csv,.xlsx"/></label><Button disabled={busy!==""}><Upload aria-hidden="true"/>{t(($)=>$.actions.import_statement)}</Button></form></Section>
        <Section title={t(($)=>$.actions.add_transaction)}><form className="grid gap-2 md:grid-cols-4" onSubmit={(event)=>{const fd=formData(event);void execute("transaction",()=>api.businessAction(businessID,"bank/transactions",{booked_on:fd.get("date"),direction:fd.get("direction"),amount_rub:fd.get("amount"),counterparty_name:fd.get("counterparty"),purpose:fd.get("purpose"),classification:"unknown",idempotency_key:crypto.randomUUID()}));event.currentTarget.reset();}}><Input required name="date" type="date"/><select name="direction" className={selectClass}><option value="inbound">{t(($)=>$.values.inbound)}</option><option value="outbound">{t(($)=>$.values.outbound)}</option></select><Input required name="amount" placeholder={t(($)=>$.fields.amount)}/><Input required name="counterparty" placeholder={t(($)=>$.fields.counterparty)}/><Input name="purpose" placeholder={t(($)=>$.fields.purpose)}/><Button disabled={busy!==""}>{t(($)=>$.actions.add_transaction)}</Button></form></Section>
        <Section title={t(($)=>$.sections.transactions)} actions={<div className="flex flex-wrap items-center gap-3">
          <FilterSelect label={t(($) => $.filters.classification)} value={txClassFilter} onChange={setTxClassFilter} options={CLASSIFICATIONS.map((value) => ({ value, label: tt(`values.${value}`, { defaultValue: value }) }))} allLabel={t(($) => $.filters.all)} />
          <FilterSelect label={t(($) => $.filters.direction)} value={txDirectionFilter} onChange={setTxDirectionFilter} options={[{ value: "inbound", label: t(($) => $.values.inbound) }, { value: "outbound", label: t(($) => $.values.outbound) }]} allLabel={t(($) => $.filters.all)} />
          <Input value={txSearch} onChange={(event) => setTxSearch(event.target.value)} className="w-56" placeholder={t(($) => $.filters.search)} />
        </div>}>
          <div className="space-y-2">
            <div className="text-xs text-muted-foreground">{filterRows} · {t(($) => $.values.inbound)}: {rub(transactionTotals.inbound)} · {t(($) => $.values.outbound)}: {rub(transactionTotals.outbound)}</div>
            <RowTable tt={tt} locale={locale} rows={filteredTransactions} columns={[{ key: "booked_on", kind: "date" }, { key: "direction", kind: "enum" }, { key: "amount_rub", kind: "money" }, { key: "counterparty_name" }, { key: "counterparty_inn" }, { key: "classification", kind: "enum" }, { key: "purpose" }]} empty={t(($)=>$.empty)}
              extra={{ header: t(($) => $.actions.classify), render: (row) => (
                <form className="flex items-center gap-1" onSubmit={(event) => { const fd = formData(event); void execute(`classify-${String(row.id)}`, () => api.businessAction(businessID, `bank/transactions/${String(row.id)}/classify`, { classification: fd.get("cls"), confidence: "confirmed", reason: "manual reclassification" })); }}>
                  <select name="cls" className={selectClass} defaultValue={String(row.classification ?? "unknown")}>{CLASSIFICATIONS.map((value) => <option key={value} value={value}>{tt(`values.${value}`, { defaultValue: value })}</option>)}</select>
                  <Button size="sm" variant="outline" disabled={busy !== ""}>{t(($) => $.actions.save)}</Button>
                </form>
              ) }} />
          </div>
        </Section>
      </div>}

      {tab === "team" && economicsEnabled && <div className="space-y-8">
        <Section title={t(($)=>$.sections.workers)} actions={<form className="flex flex-wrap gap-2" onSubmit={(event)=>{const fd=formData(event);void execute("worker",()=>api.businessAction(businessID,"workers",{name:fd.get("name"),engagement_format:"self_employed"}));event.currentTarget.reset();}}><Input required name="name" className="w-52" placeholder={t(($)=>$.fields.name)}/><Button disabled={busy!==""}>{t(($)=>$.actions.add_worker)}</Button></form>}>
          <div className="space-y-2">
            <div className="text-xs text-muted-foreground">{t(($) => $.team.rate_hint)}</div>
            <div className="overflow-x-auto rounded-lg border">
              <table className="w-full min-w-[720px] text-left text-sm">
                <thead className="bg-muted/60 text-xs text-muted-foreground"><tr>
                  <th className="px-3 py-2 font-medium">{tt("columns.name", { defaultValue: "name" })}</th>
                  <th className="px-3 py-2 font-medium">{tt("columns.status", { defaultValue: "status" })}</th>
                  <th className="px-3 py-2 font-medium">{tt("columns.engagement_format", { defaultValue: "format" })}</th>
                  <th className="px-3 py-2 font-medium">{t(($) => $.team.default_rate)}</th>
                </tr></thead>
                <tbody>
                  {(data.workers ?? []).map((row) => {
                    const id = String(row.id);
                    return (
                      <tr key={id} className="border-t align-middle">
                        <td className="px-3 py-2">{text(row, "name")}</td>
                        <td className="px-3 py-2">{tt(`values.${String(row.status)}`, { defaultValue: String(row.status) })}</td>
                        <td className="px-3 py-2">{tt(`values.${String(row.engagement_format)}`, { defaultValue: String(row.engagement_format) })}</td>
                        <td className="px-3 py-2">
                          <form className="flex flex-wrap items-center gap-1.5" onSubmit={(event) => { const fd = formData(event); void execute(`rate-${id}`, () => api.businessAction(businessID, `workers/${id}`, { default_role: fd.get("role"), default_percent: fd.get("percent") }, "PATCH")); }}>
                            <select name="role" className={selectClass} defaultValue={String(row.default_role ?? "")}>
                              <option value="">{t(($) => $.team.no_rate)}</option>
                              {WORKER_ROLES.map((value) => <option key={value} value={value}>{tt(`values.${value}`, { defaultValue: value })}</option>)}
                            </select>
                            <Input name="percent" inputMode="decimal" className="w-20" defaultValue={row.default_percent ? String(Number(row.default_percent)) : ""} placeholder="%" />
                            <Button size="sm" variant="outline" disabled={busy !== ""}>{t(($) => $.actions.save)}</Button>
                          </form>
                        </td>
                      </tr>
                    );
                  })}
                  {(data.workers ?? []).length === 0 && <tr><td colSpan={4} className="p-8 text-center text-sm text-muted-foreground">{t(($) => $.empty)}</td></tr>}
                </tbody>
              </table>
            </div>
          </div>
        </Section>
        <Section title={t(($) => $.sections.worker_months)}>
          <RowTable tt={tt} locale={locale} rows={workerMonths as unknown as BusinessRow[]} columns={[{ key: "worker_name" }, { key: "month" }, { key: "accrued_rub", kind: "money" }, { key: "funded_rub", kind: "money" }, { key: "paid_rub", kind: "money" }]} empty={t(($) => $.empty)} />
        </Section>
        <Section title={t(($) => $.sections.payout_items)}>
          <RowTable tt={tt} locale={locale} rows={enrichedPayoutItems} columns={[{ key: "worker_name" }, { key: "period_key" }, { key: "amount_rub", kind: "money" }, { key: "status", kind: "enum" }, { key: "batch_status", kind: "enum" }, { key: "created_at", kind: "datetime" }]} empty={t(($) => $.empty)} />
        </Section>
        <Section title={t(($) => $.sections.participations)}>
          <RowTable tt={tt} locale={locale} rows={enrichedParticipants} columns={[{ key: "worker_name" }, { key: "issue_title" }, { key: "role", kind: "enum" }, { key: "pool", kind: "enum" }, { key: "percent", kind: "percent" }, { key: "amount_rub", kind: "money" }, { key: "status", kind: "enum" }]} empty={t(($) => $.empty)} />
        </Section>
      </div>}

      {tab === "economics" && economicsEnabled && <div className="space-y-8">
        <Section title={t(($)=>$.actions.draft_economics)}><form className="grid gap-2 md:grid-cols-4" onSubmit={(event)=>{const fd=formData(event);void execute("economics",()=>api.businessAction(businessID,"task-economics",{workspace_id:fd.get("workspace"),project_id:fd.get("project"),issue_id:fd.get("issue"),client_id:fd.get("client")||null,service_type:fd.get("service"),service_value_rub:fd.get("amount"),source:"manual_override",billing_disposition:"normal",idempotency_key:crypto.randomUUID(),participants:[{worker_id:fd.get("worker"),role:fd.get("role"),pool:fd.get("role")==="pm"?"pm":"execution",percent:fd.get("percent")}] }));}}>
          <Input required name="workspace" placeholder={t(($)=>$.fields.workspace_id)}/><Input required name="project" placeholder={t(($)=>$.fields.project_id)}/><Input required name="issue" placeholder={t(($)=>$.fields.issue_id)}/><select name="client" className={selectClass}><option value="">—</option>{(data.clients ?? []).map((row)=><option key={text(row,"id")} value={text(row,"id")}>{text(row,"canonical_name")}</option>)}</select><select name="service" className={selectClass}>{SERVICE_TYPES.map((value)=><option key={value} value={value}>{tt(`values.${value}`, { defaultValue: value })}</option>)}</select><Input required name="amount" placeholder={t(($)=>$.fields.amount)}/><select required name="worker" className={selectClass} value={econWorker || String(econWorkerRow?.id ?? "")} onChange={(event) => setEconWorker(event.target.value)}>{(data.workers ?? []).map((row)=><option key={text(row,"id")} value={text(row,"id")}>{text(row,"name")}</option>)}</select><select key={`role-${String(econWorkerRow?.id ?? "")}`} name="role" className={selectClass} defaultValue={String(econWorkerRow?.default_role ?? "executor")}>{WORKER_ROLES.map((value)=><option key={value} value={value}>{tt(`values.${value}`, { defaultValue: value })}</option>)}</select><Input key={`pct-${String(econWorkerRow?.id ?? "")}`} required name="percent" defaultValue={econWorkerRow?.default_percent ? String(Number(econWorkerRow.default_percent)) : ""} placeholder={t(($)=>$.fields.percent)}/><Button disabled={busy!==""}>{t(($)=>$.actions.draft_economics)}</Button>
        </form></Section>
        <Section title={t(($)=>$.sections.tasks)}><div className="space-y-2"><RowTable tt={tt} locale={locale} rows={data.task_economics ?? []} columns={[{ key: "issue_title" }, { key: "project_title" }, { key: "client_name" }, { key: "service_type", kind: "enum" }, { key: "service_value_rub", kind: "money" }, { key: "status", kind: "enum" }, { key: "pm_eligible", kind: "bool" }, { key: "accepted_at", kind: "datetime" }]} empty={t(($)=>$.empty)}/>{acceptEnabled&&accrualsEnabled&&(data.task_economics ?? []).filter((row)=>text(row,"status")==="draft").map((row)=><Button type="button" variant="outline" key={text(row,"id")} disabled={busy!==""} onClick={()=>void execute("accept",()=>api.businessAction(businessID,`task-economics/${text(row,"id")}/accept`,{reason:"owner acceptance"}))}>{t(($)=>$.actions.accept)} · {text(row,"issue_title") !== "—" ? text(row,"issue_title") : text(row,"issue_id")}</Button>)}</div></Section>
      </div>}

      {tab === "accruals" && accrualsEnabled && <div className="space-y-8">
        <Section title={t(($)=>$.sections.accruals)}><RowTable tt={tt} locale={locale} rows={data.accruals ?? []} columns={[{ key: "worker_name" }, { key: "role", kind: "enum" }, { key: "original_amount_rub", kind: "money" }, { key: "adjustment_rub", kind: "money" }, { key: "funded_rub", kind: "money" }, { key: "reserve_funded_rub", kind: "money" }, { key: "paid_rub", kind: "money" }, { key: "status", kind: "enum" }, { key: "reserve_due_on", kind: "date" }]} empty={t(($)=>$.empty)}/></Section>
        <Section title={t(($)=>$.sections.reserve)} actions={<form className="flex flex-wrap gap-2" onSubmit={(event)=>{const fd=formData(event);void execute("reserve",()=>api.businessAction(businessID,"reserve/entries",{entry_type:"contribution",amount_rub:fd.get("amount"),reason:fd.get("reason"),idempotency_key:crypto.randomUUID()}));event.currentTarget.reset();}}><Input required name="amount" className="w-32" placeholder={t(($)=>$.fields.amount)}/><Input required name="reason" className="w-64" placeholder={t(($)=>$.fields.reason)}/><Button disabled={busy!==""}>{t(($)=>$.actions.add_reserve)}</Button></form>}><RowTable tt={tt} locale={locale} rows={data.reserve_ledger ?? []} columns={[{ key: "occurred_at", kind: "datetime" }, { key: "entry_type", kind: "enum" }, { key: "amount_rub", kind: "money" }, { key: "reason" }]} empty={t(($)=>$.empty)}/></Section>
      </div>}

      {tab === "payouts" && payoutsEnabled && <div className="space-y-8">
        <div className="rounded-lg border p-3 text-sm text-muted-foreground">{t(($)=>$.bank_draft_note)}</div>
        <Section title={t(($)=>$.sections.payouts)} actions={<form className="flex flex-wrap gap-2" onSubmit={(event)=>{const fd=formData(event);void execute("payout",()=>api.businessAction(businessID,"payouts",{period_key:fd.get("period"),idempotency_key:`payout:${String(fd.get("period"))}`}));}}><Input required name="period" type="month" defaultValue={month} className="w-auto"/><Button disabled={busy!==""}>{t(($)=>$.actions.build_payout)}</Button></form>}>
          <div className="space-y-3"><RowTable tt={tt} locale={locale} rows={data.payout_batches ?? []} columns={[{ key: "period_key" }, { key: "status", kind: "enum" }, { key: "total_rub", kind: "money" }, { key: "worker_count" }, { key: "approved_at", kind: "datetime" }, { key: "submitted_at", kind: "datetime" }, { key: "paid_at", kind: "datetime" }]} empty={t(($)=>$.empty)}/><div className="flex flex-wrap gap-2">{(data.payout_batches ?? []).map((row)=>{const status=text(row,"status"),id=text(row,"id");return <div key={id} className="flex gap-1">{status==="draft"&&<Button type="button" variant="outline" disabled={busy!==""} onClick={()=>void execute("approve",()=>api.businessAction(businessID,`payouts/${id}/approve`))}>{t(($)=>$.actions.approve)} · {text(row,"period_key")}</Button>}{status==="approved"&&bankDraftsEnabled&&<Button type="button" variant="outline" disabled={busy!==""} onClick={()=>void execute("submit",()=>api.businessAction(businessID,`payouts/${id}/submit-draft`))}>{t(($)=>$.actions.submit_draft)} · {text(row,"period_key")}</Button>}</div>})}</div></div>
        </Section>
        <Section title={t(($) => $.sections.payout_items)}>
          <RowTable tt={tt} locale={locale} rows={enrichedPayoutItems} columns={[{ key: "worker_name" }, { key: "period_key" }, { key: "amount_rub", kind: "money" }, { key: "status", kind: "enum" }, { key: "external_operation_id" }]} empty={t(($) => $.empty)} />
        </Section>
      </div>}
        </div>
      </div>
    </div>
  );
}
