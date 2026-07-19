"use client";

import { useState } from "react";
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

type Tab = "overview" | "clients" | "calendar" | "bank" | "economics" | "accruals" | "payouts";

const selectClass = "h-8 min-w-0 rounded-lg border border-input bg-transparent px-2.5 py-1 text-sm outline-none transition-colors focus-visible:border-ring focus-visible:ring-3 focus-visible:ring-ring/50 dark:bg-input/30";

function text(row: BusinessRow, key: string): string {
  const value = row[key];
  if (value === null || value === undefined) return "—";
  if (typeof value === "object") return JSON.stringify(value);
  return String(value);
}

function rub(value: string | undefined): string {
  const amount = Number(value ?? 0);
  return new Intl.NumberFormat("ru-RU", { style: "currency", currency: "RUB", maximumFractionDigits: 0 }).format(Number.isFinite(amount) ? amount : 0);
}

function RowTable({ rows, columns, empty }: { rows: BusinessRow[]; columns: string[]; empty: string }) {
  if (rows.length === 0) return <div className="rounded-lg border border-dashed p-8 text-center text-sm text-muted-foreground">{empty}</div>;
  return (
    <div className="overflow-x-auto rounded-lg border">
      <table className="w-full min-w-[720px] text-left text-sm">
        <thead className="bg-muted/60 text-xs text-muted-foreground">
          <tr>{columns.map((column) => <th key={column} className="px-3 py-2 font-medium">{column.replaceAll("_", " ")}</th>)}</tr>
        </thead>
        <tbody>
          {rows.slice(0, 100).map((row, index) => (
            <tr key={String(row.id ?? index)} className="border-t align-top">
              {columns.map((column) => <td key={column} className="max-w-[320px] truncate px-3 py-2" title={text(row, column)}>{text(row, column)}</td>)}
            </tr>
          ))}
        </tbody>
      </table>
    </div>
  );
}

function Section({ title, actions, children }: { title: string; actions?: React.ReactNode; children: React.ReactNode }) {
  return <section className="space-y-3"><div className="flex flex-wrap items-center justify-between gap-2"><h2 className="text-sm font-medium">{title}</h2>{actions}</div>{children}</section>;
}

function Metric({ label, value, warning }: { label: string; value: string; warning?: boolean }) {
  return <div className={cn("flex min-w-0 flex-col gap-2 rounded-lg border bg-card p-5", warning && "border-warning/50 bg-warning/5")}><div className="text-[11px] font-medium uppercase tracking-wider text-muted-foreground">{label}</div><div className="break-words text-2xl font-semibold leading-tight tabular-nums sm:text-3xl">{value}</div></div>;
}

function formData(event: FormEvent<HTMLFormElement>): FormData {
  event.preventDefault();
  return new FormData(event.currentTarget);
}

export function BusinessPage() {
  const { t } = useT("business");
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

  const accounts = useQuery({ queryKey: ["business", "accounts"], queryFn: () => api.listBusinessAccounts(), enabled });
  const businessID = selectedBusiness || accounts.data?.[0]?.id || "";
  const dashboard = useQuery({ queryKey: ["business", businessID, "dashboard", month], queryFn: () => api.getBusinessDashboard(businessID, month), enabled: enabled && !!businessID });
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

  if (!enabled) return <div className="flex min-h-[50vh] items-center justify-center p-8 text-muted-foreground">{t(($) => $.unavailable)}</div>;
  if (accounts.isLoading || (businessID && (dashboard.isLoading || snapshot.isLoading))) return <div className="flex min-h-[50vh] items-center justify-center p-8 text-muted-foreground">{t(($) => $.loading)}</div>;
  if (accounts.error || dashboard.error || snapshot.error) return <div className="p-8 text-destructive">{String(accounts.error ?? dashboard.error ?? snapshot.error)}</div>;
  if (!businessID || !snapshot.data || !dashboard.data) return <div className="p-8 text-muted-foreground">{t(($) => $.empty)}</div>;

  const data = snapshot.data;
  const metrics = dashboard.data;
  const tabs: { key: Tab; enabled: boolean }[] = [
    { key: "overview", enabled: true }, { key: "clients", enabled: clientsEnabled }, { key: "calendar", enabled: calendarEnabled },
    { key: "bank", enabled: bankEnabled }, { key: "economics", enabled: economicsEnabled }, { key: "accruals", enabled: accrualsEnabled }, { key: "payouts", enabled: payoutsEnabled },
  ];

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
        <div className="grid gap-3 sm:grid-cols-2 xl:grid-cols-4">
          <Metric label={t(($) => $.metrics.client_income)} value={rub(metrics.bank_client_income_rub)} />
          <Metric label={t(($) => $.metrics.overdue)} value={rub(metrics.overdue_rub)} warning={Number(metrics.overdue_rub) > 0} />
          <Metric label={t(($) => $.metrics.payable)} value={rub(metrics.payable_rub)} />
          <Metric label={t(($) => $.metrics.reserve)} value={rub(metrics.reserve_balance_rub)} warning={Number(metrics.reserve_deficit_rub) > 0} />
          <Metric label={t(($) => $.metrics.owner_net)} value={rub(metrics.owner_net_income_rub)} />
          <Metric label={t(($) => $.metrics.target)} value={`${metrics.owner_target_progress_pct}%`} />
          <Metric label={t(($) => $.metrics.task_value)} value={rub(metrics.task_value_rub)} />
          <Metric label={t(($) => $.metrics.unknown)} value={`${rub(metrics.unknown_inbound_rub)} · ${metrics.unmatched_count}`} warning={metrics.unmatched_count > 0} />
        </div>
        <div className="grid gap-3 md:grid-cols-2">
          <div className="rounded-lg border p-4 text-sm text-muted-foreground"><AlertTriangle className="mr-2 inline size-4 text-warning"/>{t(($) => $.vitmax_note)}<div className="mt-2 font-medium text-foreground">{t(($) => $.values.vitmax)}: {rub(metrics.vitmax_transit_rub)} · {t(($) => $.values.transfers)}: {rub(metrics.transfer_rub)}</div></div>
          <div className="rounded-lg border p-4 text-sm text-muted-foreground">{t(($) => $.no_penalties_note)}</div>
        </div>
      </div>}

      {tab === "clients" && clientsEnabled && <div className="space-y-8">
        <Section title={t(($) => $.sections.clients)} actions={<form className="flex flex-wrap gap-2" onSubmit={(event) => { const fd=formData(event); void execute("client", () => api.businessAction(businessID,"clients",{canonical_name:fd.get("name"),status:"active",primary_payment_channel:"bank"})); event.currentTarget.reset(); }}><Input required name="name" className="w-52" placeholder={t(($) => $.fields.name)}/><Button disabled={busy!==""}>{t(($) => $.actions.add_client)}</Button></form>}>
          <RowTable rows={data.clients} columns={["canonical_name","status","primary_payment_channel","manager_user_id","notes"]} empty={t(($) => $.empty)} />
        </Section>
        <Section title={t(($) => $.sections.projects)} actions={<form className="grid gap-2 sm:grid-cols-5" onSubmit={(event)=>{const fd=formData(event);const client=String(fd.get("client"));void execute("map",()=>api.businessAction(businessID,`clients/${client}/projects`,{workspace_id:fd.get("workspace"),project_id:fd.get("project"),service_type:fd.get("service"),billable:true},"PUT"));}}>
          <select required name="client" className={selectClass}>{data.clients.map((row)=><option key={text(row,"id")} value={text(row,"id")}>{text(row,"canonical_name")}</option>)}</select><Input required name="workspace" placeholder={t(($)=>$.fields.workspace_id)}/><Input required name="project" placeholder={t(($)=>$.fields.project_id)}/><select name="service" className={selectClass}><option value="development">{t(($)=>$.values.development)}</option><option value="support">{t(($)=>$.values.support)}</option><option value="seo">{t(($)=>$.values.seo)}</option><option value="content">{t(($)=>$.values.content)}</option></select><Button disabled={busy!==""}>{t(($)=>$.actions.map_project)}</Button>
        </form>}><RowTable rows={data.projects} columns={["client_name","project_title","workspace_name","service_type","billable"]} empty={t(($)=>$.empty)}/></Section>
      </div>}

      {tab === "calendar" && calendarEnabled && <div className="space-y-8">
        <Section title={t(($)=>$.actions.add_agreement)}><form className="grid gap-2 md:grid-cols-4" onSubmit={(event)=>{const fd=formData(event);void execute("agreement",()=>api.businessAction(businessID,"agreements",{client_id:fd.get("client"),service_type:fd.get("service"),agreement_key:fd.get("key"),version:1,name:fd.get("name"),model:"fixed",amount_rub:fd.get("amount"),due_days:7,period_months:1,payment_channel:"bank",effective_from:fd.get("date"),status:"active",is_estimate:false,needs_review:false,terms:{}}));}}>
          <select required name="client" className={selectClass}>{data.clients.map((row)=><option key={text(row,"id")} value={text(row,"id")}>{text(row,"canonical_name")}</option>)}</select><Input required name="name" placeholder={t(($)=>$.fields.name)}/><Input required name="key" placeholder={t(($)=>$.fields.agreement_key)}/><select name="service" className={selectClass}><option value="development">{t(($)=>$.values.development)}</option><option value="support">{t(($)=>$.values.support)}</option><option value="seo">{t(($)=>$.values.seo)}</option><option value="content">{t(($)=>$.values.content)}</option></select><Input required name="amount" inputMode="decimal" placeholder={t(($)=>$.fields.amount)}/><Input required name="date" type="date"/><Button disabled={busy!==""}>{t(($)=>$.actions.add_agreement)}</Button>
        </form></Section>
        <Section title={t(($)=>$.sections.receivables)} actions={<form className="flex flex-wrap gap-2" onSubmit={(event)=>{const fd=formData(event);void execute("receivables",()=>api.businessAction(businessID,"receivables/generate",{from_month:fd.get("month"),months:Number(fd.get("months"))}));}}><Input name="month" type="month" defaultValue={month} className="w-auto"/><Input name="months" type="number" min="1" max="24" defaultValue="3" className="w-20"/><Button disabled={busy!==""}>{t(($)=>$.actions.generate_receivables)}</Button></form>}><RowTable rows={data.receivables} columns={["period_key","client_id","planned_amount_rub","paid_amount_rub","invoice_on","due_on","status","needs_review"]} empty={t(($)=>$.empty)}/></Section>
      </div>}

      {tab === "bank" && bankEnabled && <div className="space-y-8">
        <Section title={t(($)=>$.actions.import_statement)}><form className="flex flex-wrap items-end gap-2" onSubmit={(event)=>{const fd=formData(event);const file=fd.get("file");if(file instanceof File)void execute("bank",()=>api.importBusinessBankFile(businessID,file));}}><label className="grid gap-1 text-xs text-muted-foreground">{t(($)=>$.fields.file)}<Input required name="file" type="file" accept=".csv,.xlsx"/></label><Button disabled={busy!==""}><Upload aria-hidden="true"/>{t(($)=>$.actions.import_statement)}</Button></form></Section>
        <Section title={t(($)=>$.actions.add_transaction)}><form className="grid gap-2 md:grid-cols-4" onSubmit={(event)=>{const fd=formData(event);void execute("transaction",()=>api.businessAction(businessID,"bank/transactions",{booked_on:fd.get("date"),direction:fd.get("direction"),amount_rub:fd.get("amount"),counterparty_name:fd.get("counterparty"),purpose:fd.get("purpose"),classification:"unknown",idempotency_key:crypto.randomUUID()}));event.currentTarget.reset();}}><Input required name="date" type="date"/><select name="direction" className={selectClass}><option value="inbound">{t(($)=>$.values.inbound)}</option><option value="outbound">{t(($)=>$.values.outbound)}</option></select><Input required name="amount" placeholder={t(($)=>$.fields.amount)}/><Input required name="counterparty" placeholder={t(($)=>$.fields.counterparty)}/><Input name="purpose" placeholder={t(($)=>$.fields.purpose)}/><Button disabled={busy!==""}>{t(($)=>$.actions.add_transaction)}</Button></form></Section>
        <Section title={t(($)=>$.sections.transactions)}><RowTable rows={data.transactions} columns={["booked_on","direction","amount_rub","counterparty_name","counterparty_inn","classification","classification_confidence","purpose"]} empty={t(($)=>$.empty)}/></Section>
      </div>}

      {tab === "economics" && economicsEnabled && <div className="space-y-8">
        <Section title={t(($)=>$.sections.workers)} actions={<form className="flex flex-wrap gap-2" onSubmit={(event)=>{const fd=formData(event);void execute("worker",()=>api.businessAction(businessID,"workers",{name:fd.get("name"),engagement_format:"self_employed"}));event.currentTarget.reset();}}><Input required name="name" className="w-52" placeholder={t(($)=>$.fields.name)}/><Button disabled={busy!==""}>{t(($)=>$.actions.add_worker)}</Button></form>}><RowTable rows={data.workers} columns={["name","status","engagement_format","recipient_mask"]} empty={t(($)=>$.empty)}/></Section>
        <Section title={t(($)=>$.actions.draft_economics)}><form className="grid gap-2 md:grid-cols-4" onSubmit={(event)=>{const fd=formData(event);void execute("economics",()=>api.businessAction(businessID,"task-economics",{workspace_id:fd.get("workspace"),project_id:fd.get("project"),issue_id:fd.get("issue"),client_id:fd.get("client")||null,service_type:fd.get("service"),service_value_rub:fd.get("amount"),source:"manual_override",billing_disposition:"normal",idempotency_key:crypto.randomUUID(),participants:[{worker_id:fd.get("worker"),role:fd.get("role"),pool:fd.get("role")==="pm"?"pm":"execution",percent:fd.get("percent")}] }));}}>
          <Input required name="workspace" placeholder={t(($)=>$.fields.workspace_id)}/><Input required name="project" placeholder={t(($)=>$.fields.project_id)}/><Input required name="issue" placeholder={t(($)=>$.fields.issue_id)}/><select name="client" className={selectClass}><option value="">—</option>{data.clients.map((row)=><option key={text(row,"id")} value={text(row,"id")}>{text(row,"canonical_name")}</option>)}</select><select name="service" className={selectClass}><option value="development">{t(($)=>$.values.development)}</option><option value="support">{t(($)=>$.values.support)}</option><option value="seo">{t(($)=>$.values.seo)}</option><option value="content">{t(($)=>$.values.content)}</option></select><Input required name="amount" placeholder={t(($)=>$.fields.amount)}/><select required name="worker" className={selectClass}>{data.workers.map((row)=><option key={text(row,"id")} value={text(row,"id")}>{text(row,"name")}</option>)}</select><select name="role" className={selectClass}><option value="executor">{t(($)=>$.values.executor)}</option><option value="pm">{t(($)=>$.values.pm)}</option><option value="reviewer">{t(($)=>$.values.reviewer)}</option><option value="seo">{t(($)=>$.values.seo)}</option><option value="content">{t(($)=>$.values.content)}</option></select><Input required name="percent" placeholder={t(($)=>$.fields.percent)}/><Button disabled={busy!==""}>{t(($)=>$.actions.draft_economics)}</Button>
        </form></Section>
        <Section title={t(($)=>$.sections.tasks)}><div className="space-y-2"><RowTable rows={data.task_economics} columns={["issue_id","service_type","service_value_rub","status","pm_eligible","accepted_at"]} empty={t(($)=>$.empty)}/>{acceptEnabled&&accrualsEnabled&&data.task_economics.filter((row)=>text(row,"status")==="draft").map((row)=><Button type="button" variant="outline" key={text(row,"id")} disabled={busy!==""} onClick={()=>void execute("accept",()=>api.businessAction(businessID,`task-economics/${text(row,"id")}/accept`,{reason:"owner acceptance"}))}>{t(($)=>$.actions.accept)} · {text(row,"issue_id")}</Button>)}</div></Section>
      </div>}

      {tab === "accruals" && accrualsEnabled && <div className="space-y-8">
        <Section title={t(($)=>$.sections.accruals)}><RowTable rows={data.accruals} columns={["worker_id","role","original_amount_rub","funded_rub","reserve_funded_rub","paid_rub","status","reserve_due_on"]} empty={t(($)=>$.empty)}/></Section>
        <Section title={t(($)=>$.sections.reserve)} actions={<form className="flex flex-wrap gap-2" onSubmit={(event)=>{const fd=formData(event);void execute("reserve",()=>api.businessAction(businessID,"reserve/entries",{entry_type:"contribution",amount_rub:fd.get("amount"),reason:fd.get("reason"),idempotency_key:crypto.randomUUID()}));event.currentTarget.reset();}}><Input required name="amount" className="w-32" placeholder={t(($)=>$.fields.amount)}/><Input required name="reason" className="w-64" placeholder={t(($)=>$.fields.reason)}/><Button disabled={busy!==""}>{t(($)=>$.actions.add_reserve)}</Button></form>}><RowTable rows={data.reserve_ledger} columns={["occurred_at","entry_type","amount_rub","reason","accrual_id"]} empty={t(($)=>$.empty)}/></Section>
      </div>}

      {tab === "payouts" && payoutsEnabled && <div className="space-y-8">
        <div className="rounded-lg border p-3 text-sm text-muted-foreground">{t(($)=>$.bank_draft_note)}</div>
        <Section title={t(($)=>$.sections.payouts)} actions={<form className="flex flex-wrap gap-2" onSubmit={(event)=>{const fd=formData(event);void execute("payout",()=>api.businessAction(businessID,"payouts",{period_key:fd.get("period"),idempotency_key:`payout:${String(fd.get("period"))}`}));}}><Input required name="period" type="month" defaultValue={month} className="w-auto"/><Button disabled={busy!==""}>{t(($)=>$.actions.build_payout)}</Button></form>}>
          <div className="space-y-3"><RowTable rows={data.payout_batches} columns={["period_key","status","total_rub","worker_count","approved_at","submitted_at","paid_at"]} empty={t(($)=>$.empty)}/><div className="flex flex-wrap gap-2">{data.payout_batches.map((row)=>{const status=text(row,"status"),id=text(row,"id");return <div key={id} className="flex gap-1">{status==="draft"&&<Button type="button" variant="outline" disabled={busy!==""} onClick={()=>void execute("approve",()=>api.businessAction(businessID,`payouts/${id}/approve`))}>{t(($)=>$.actions.approve)} · {text(row,"period_key")}</Button>}{status==="approved"&&bankDraftsEnabled&&<Button type="button" variant="outline" disabled={busy!==""} onClick={()=>void execute("submit",()=>api.businessAction(businessID,`payouts/${id}/submit-draft`))}>{t(($)=>$.actions.submit_draft)} · {text(row,"period_key")}</Button>}</div>})}</div></div>
        </Section>
      </div>}
        </div>
      </div>
    </div>
  );
}
