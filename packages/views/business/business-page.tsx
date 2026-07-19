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
import { cn } from "@multica/ui/lib/utils";
import { AlertTriangle, Building2, RefreshCw, Upload } from "lucide-react";
import { useT } from "../i18n";

type Tab = "overview" | "clients" | "calendar" | "bank" | "economics" | "accruals" | "payouts";

const inputClass = "h-9 min-w-0 rounded-md border border-border bg-background px-3 text-sm outline-none focus:border-ring";
const buttonClass = "inline-flex h-9 items-center justify-center rounded-md bg-foreground px-3 text-sm font-medium text-background disabled:cursor-not-allowed disabled:opacity-50";
const secondaryButtonClass = "inline-flex h-9 items-center justify-center rounded-md border border-border bg-background px-3 text-sm font-medium hover:bg-muted disabled:opacity-50";

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
  return <section className="space-y-3"><div className="flex flex-wrap items-center justify-between gap-2"><h2 className="text-lg font-semibold">{title}</h2>{actions}</div>{children}</section>;
}

function Metric({ label, value, warning }: { label: string; value: string; warning?: boolean }) {
  return <div className={cn("rounded-xl border bg-card p-4", warning && "border-amber-500/50 bg-amber-500/5")}><div className="text-xs text-muted-foreground">{label}</div><div className="mt-1 text-xl font-semibold tabular-nums">{value}</div></div>;
}

function formData(event: FormEvent<HTMLFormElement>): FormData {
  event.preventDefault();
  return new FormData(event.currentTarget);
}

export function BusinessPage() {
  const { t } = useT("business");
  const enabled = useFeatureEnabled(BUSINESS_CONTROL_PLANE_FLAG) && useFeatureEnabled(BUSINESS_DASHBOARD_FLAG);
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
    <div className="mx-auto w-full max-w-[1600px] space-y-6 p-4 md:p-8">
      <header className="flex flex-wrap items-start justify-between gap-4">
        <div><div className="flex items-center gap-2"><Building2 className="size-6"/><h1 className="text-2xl font-semibold">{t(($) => $.title)}</h1></div><p className="mt-1 text-sm text-muted-foreground">{t(($) => $.subtitle)}</p></div>
        <div className="flex flex-wrap items-center gap-2">
          {accounts.data && accounts.data.length > 1 && <select className={inputClass} value={businessID} onChange={(event) => setSelectedBusiness(event.target.value)}>{accounts.data.map((account) => <option key={account.id} value={account.id}>{account.name}</option>)}</select>}
          <label className="flex items-center gap-2 text-sm"><span>{t(($) => $.month)}</span><input className={inputClass} type="month" value={month} onChange={(event) => setMonth(event.target.value)} /></label>
          <button className={secondaryButtonClass} onClick={() => void Promise.all([snapshot.refetch(), dashboard.refetch()])}><RefreshCw className="mr-2 size-4"/>{t(($) => $.refresh)}</button>
        </div>
      </header>

      {(error || message) && <div className={cn("rounded-lg border p-3 text-sm", error ? "border-destructive/40 bg-destructive/5 text-destructive" : "border-emerald-500/40 bg-emerald-500/5 text-emerald-700")}>{error || message}</div>}

      <nav className="flex gap-1 overflow-x-auto border-b">
        {tabs.filter((item) => item.enabled).map(({ key }) => <button key={key} onClick={() => setTab(key)} className={cn("whitespace-nowrap border-b-2 px-3 py-2 text-sm", tab === key ? "border-foreground font-medium" : "border-transparent text-muted-foreground")}>{t(($) => $.tabs[key])}</button>)}
      </nav>

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
          <div className="rounded-lg border p-4 text-sm text-muted-foreground"><AlertTriangle className="mr-2 inline size-4 text-amber-500"/>{t(($) => $.vitmax_note)}<div className="mt-2 font-medium text-foreground">VitMax: {rub(metrics.vitmax_transit_rub)} · transfers: {rub(metrics.transfer_rub)}</div></div>
          <div className="rounded-lg border p-4 text-sm text-muted-foreground">{t(($) => $.no_penalties_note)}</div>
        </div>
      </div>}

      {tab === "clients" && clientsEnabled && <div className="space-y-8">
        <Section title={t(($) => $.sections.clients)} actions={<form className="flex gap-2" onSubmit={(event) => { const fd=formData(event); void execute("client", () => api.businessAction(businessID,"clients",{canonical_name:fd.get("name"),status:"active",primary_payment_channel:"bank"})); event.currentTarget.reset(); }}><input required name="name" className={inputClass} placeholder={t(($) => $.fields.name)}/><button disabled={busy!==""} className={buttonClass}>{t(($) => $.actions.add_client)}</button></form>}>
          <RowTable rows={data.clients} columns={["canonical_name","status","primary_payment_channel","manager_user_id","notes"]} empty={t(($) => $.empty)} />
        </Section>
        <Section title={t(($) => $.sections.projects)} actions={<form className="grid gap-2 sm:grid-cols-5" onSubmit={(event)=>{const fd=formData(event);const client=String(fd.get("client"));void execute("map",()=>api.businessAction(businessID,`clients/${client}/projects`,{workspace_id:fd.get("workspace"),project_id:fd.get("project"),service_type:fd.get("service"),billable:true},"PUT"));}}>
          <select required name="client" className={inputClass}>{data.clients.map((row)=><option key={text(row,"id")} value={text(row,"id")}>{text(row,"canonical_name")}</option>)}</select><input required name="workspace" className={inputClass} placeholder={t(($)=>$.fields.workspace_id)}/><input required name="project" className={inputClass} placeholder={t(($)=>$.fields.project_id)}/><select name="service" className={inputClass}><option value="development">development</option><option value="support">support</option><option value="seo">SEO</option><option value="content">content</option></select><button disabled={busy!==""} className={buttonClass}>{t(($)=>$.actions.map_project)}</button>
        </form>}><RowTable rows={data.projects} columns={["client_name","project_title","workspace_name","service_type","billable"]} empty={t(($)=>$.empty)}/></Section>
      </div>}

      {tab === "calendar" && calendarEnabled && <div className="space-y-8">
        <Section title={t(($)=>$.actions.add_agreement)}><form className="grid gap-2 md:grid-cols-4" onSubmit={(event)=>{const fd=formData(event);void execute("agreement",()=>api.businessAction(businessID,"agreements",{client_id:fd.get("client"),service_type:fd.get("service"),agreement_key:fd.get("key"),version:1,name:fd.get("name"),model:"fixed",amount_rub:fd.get("amount"),due_days:7,period_months:1,payment_channel:"bank",effective_from:fd.get("date"),status:"active",is_estimate:false,needs_review:false,terms:{}}));}}>
          <select required name="client" className={inputClass}>{data.clients.map((row)=><option key={text(row,"id")} value={text(row,"id")}>{text(row,"canonical_name")}</option>)}</select><input required name="name" className={inputClass} placeholder={t(($)=>$.fields.name)}/><input required name="key" className={inputClass} placeholder="agreement key"/><select name="service" className={inputClass}><option value="development">development</option><option value="support">support</option><option value="seo">SEO</option><option value="content">content</option></select><input required name="amount" inputMode="decimal" className={inputClass} placeholder={t(($)=>$.fields.amount)}/><input required name="date" type="date" className={inputClass}/><button disabled={busy!==""} className={buttonClass}>{t(($)=>$.actions.add_agreement)}</button>
        </form></Section>
        <Section title={t(($)=>$.sections.receivables)} actions={<form className="flex gap-2" onSubmit={(event)=>{const fd=formData(event);void execute("receivables",()=>api.businessAction(businessID,"receivables/generate",{from_month:fd.get("month"),months:Number(fd.get("months"))}));}}><input name="month" type="month" defaultValue={month} className={inputClass}/><input name="months" type="number" min="1" max="24" defaultValue="3" className={cn(inputClass,"w-20")}/><button disabled={busy!==""} className={buttonClass}>{t(($)=>$.actions.generate_receivables)}</button></form>}><RowTable rows={data.receivables} columns={["period_key","client_id","planned_amount_rub","paid_amount_rub","invoice_on","due_on","status","needs_review"]} empty={t(($)=>$.empty)}/></Section>
      </div>}

      {tab === "bank" && bankEnabled && <div className="space-y-8">
        <Section title={t(($)=>$.actions.import_statement)}><form className="flex flex-wrap items-end gap-2" onSubmit={(event)=>{const fd=formData(event);const file=fd.get("file");if(file instanceof File)void execute("bank",()=>api.importBusinessBankFile(businessID,file));}}><label className="grid gap-1 text-xs text-muted-foreground">{t(($)=>$.fields.file)}<input required name="file" type="file" accept=".csv,.xlsx" className={inputClass}/></label><button disabled={busy!==""} className={buttonClass}><Upload className="mr-2 size-4"/>{t(($)=>$.actions.import_statement)}</button></form></Section>
        <Section title={t(($)=>$.actions.add_transaction)}><form className="grid gap-2 md:grid-cols-4" onSubmit={(event)=>{const fd=formData(event);void execute("transaction",()=>api.businessAction(businessID,"bank/transactions",{booked_on:fd.get("date"),direction:fd.get("direction"),amount_rub:fd.get("amount"),counterparty_name:fd.get("counterparty"),purpose:fd.get("purpose"),classification:"unknown",idempotency_key:crypto.randomUUID()}));event.currentTarget.reset();}}><input required name="date" type="date" className={inputClass}/><select name="direction" className={inputClass}><option value="inbound">inbound</option><option value="outbound">outbound</option></select><input required name="amount" className={inputClass} placeholder={t(($)=>$.fields.amount)}/><input required name="counterparty" className={inputClass} placeholder={t(($)=>$.fields.counterparty)}/><input name="purpose" className={inputClass} placeholder={t(($)=>$.fields.purpose)}/><button disabled={busy!==""} className={buttonClass}>{t(($)=>$.actions.add_transaction)}</button></form></Section>
        <Section title={t(($)=>$.sections.transactions)}><RowTable rows={data.transactions} columns={["booked_on","direction","amount_rub","counterparty_name","counterparty_inn","classification","classification_confidence","purpose"]} empty={t(($)=>$.empty)}/></Section>
      </div>}

      {tab === "economics" && economicsEnabled && <div className="space-y-8">
        <Section title={t(($)=>$.sections.workers)} actions={<form className="flex gap-2" onSubmit={(event)=>{const fd=formData(event);void execute("worker",()=>api.businessAction(businessID,"workers",{name:fd.get("name"),engagement_format:"self_employed"}));event.currentTarget.reset();}}><input required name="name" className={inputClass} placeholder={t(($)=>$.fields.name)}/><button disabled={busy!==""} className={buttonClass}>{t(($)=>$.actions.add_worker)}</button></form>}><RowTable rows={data.workers} columns={["name","status","engagement_format","recipient_mask"]} empty={t(($)=>$.empty)}/></Section>
        <Section title={t(($)=>$.actions.draft_economics)}><form className="grid gap-2 md:grid-cols-4" onSubmit={(event)=>{const fd=formData(event);void execute("economics",()=>api.businessAction(businessID,"task-economics",{workspace_id:fd.get("workspace"),project_id:fd.get("project"),issue_id:fd.get("issue"),client_id:fd.get("client")||null,service_type:fd.get("service"),service_value_rub:fd.get("amount"),source:"manual_override",billing_disposition:"normal",idempotency_key:crypto.randomUUID(),participants:[{worker_id:fd.get("worker"),role:fd.get("role"),pool:fd.get("role")==="pm"?"pm":"execution",percent:fd.get("percent")}] }));}}>
          <input required name="workspace" className={inputClass} placeholder={t(($)=>$.fields.workspace_id)}/><input required name="project" className={inputClass} placeholder={t(($)=>$.fields.project_id)}/><input required name="issue" className={inputClass} placeholder={t(($)=>$.fields.issue_id)}/><select name="client" className={inputClass}><option value="">—</option>{data.clients.map((row)=><option key={text(row,"id")} value={text(row,"id")}>{text(row,"canonical_name")}</option>)}</select><select name="service" className={inputClass}><option value="development">development</option><option value="support">support</option><option value="seo">SEO</option><option value="content">content</option></select><input required name="amount" className={inputClass} placeholder={t(($)=>$.fields.amount)}/><select required name="worker" className={inputClass}>{data.workers.map((row)=><option key={text(row,"id")} value={text(row,"id")}>{text(row,"name")}</option>)}</select><select name="role" className={inputClass}><option value="executor">executor</option><option value="pm">PM</option><option value="reviewer">reviewer</option><option value="seo">SEO</option><option value="content">content</option></select><input required name="percent" className={inputClass} placeholder={t(($)=>$.fields.percent)}/><button disabled={busy!==""} className={buttonClass}>{t(($)=>$.actions.draft_economics)}</button>
        </form></Section>
        <Section title={t(($)=>$.sections.tasks)}><div className="space-y-2"><RowTable rows={data.task_economics} columns={["issue_id","service_type","service_value_rub","status","pm_eligible","accepted_at"]} empty={t(($)=>$.empty)}/>{acceptEnabled&&accrualsEnabled&&data.task_economics.filter((row)=>text(row,"status")==="draft").map((row)=><button key={text(row,"id")} className={secondaryButtonClass} disabled={busy!==""} onClick={()=>void execute("accept",()=>api.businessAction(businessID,`task-economics/${text(row,"id")}/accept`,{reason:"owner acceptance"}))}>{t(($)=>$.actions.accept)} · {text(row,"issue_id")}</button>)}</div></Section>
      </div>}

      {tab === "accruals" && accrualsEnabled && <div className="space-y-8">
        <Section title={t(($)=>$.sections.accruals)}><RowTable rows={data.accruals} columns={["worker_id","role","original_amount_rub","funded_rub","reserve_funded_rub","paid_rub","status","reserve_due_on"]} empty={t(($)=>$.empty)}/></Section>
        <Section title={t(($)=>$.sections.reserve)} actions={<form className="flex flex-wrap gap-2" onSubmit={(event)=>{const fd=formData(event);void execute("reserve",()=>api.businessAction(businessID,"reserve/entries",{entry_type:"contribution",amount_rub:fd.get("amount"),reason:fd.get("reason"),idempotency_key:crypto.randomUUID()}));event.currentTarget.reset();}}><input required name="amount" className={inputClass} placeholder={t(($)=>$.fields.amount)}/><input required name="reason" className={inputClass} placeholder={t(($)=>$.fields.reason)}/><button disabled={busy!==""} className={buttonClass}>{t(($)=>$.actions.add_reserve)}</button></form>}><RowTable rows={data.reserve_ledger} columns={["occurred_at","entry_type","amount_rub","reason","accrual_id"]} empty={t(($)=>$.empty)}/></Section>
      </div>}

      {tab === "payouts" && payoutsEnabled && <div className="space-y-8">
        <div className="rounded-lg border p-3 text-sm text-muted-foreground">{t(($)=>$.bank_draft_note)}</div>
        <Section title={t(($)=>$.sections.payouts)} actions={<form className="flex gap-2" onSubmit={(event)=>{const fd=formData(event);void execute("payout",()=>api.businessAction(businessID,"payouts",{period_key:fd.get("period"),idempotency_key:`payout:${String(fd.get("period"))}`}));}}><input required name="period" type="month" defaultValue={month} className={inputClass}/><button disabled={busy!==""} className={buttonClass}>{t(($)=>$.actions.build_payout)}</button></form>}>
          <div className="space-y-3"><RowTable rows={data.payout_batches} columns={["period_key","status","total_rub","worker_count","approved_at","submitted_at","paid_at"]} empty={t(($)=>$.empty)}/><div className="flex flex-wrap gap-2">{data.payout_batches.map((row)=>{const status=text(row,"status"),id=text(row,"id");return <div key={id} className="flex gap-1">{status==="draft"&&<button className={secondaryButtonClass} disabled={busy!==""} onClick={()=>void execute("approve",()=>api.businessAction(businessID,`payouts/${id}/approve`))}>{t(($)=>$.actions.approve)} · {text(row,"period_key")}</button>}{status==="approved"&&bankDraftsEnabled&&<button className={secondaryButtonClass} disabled={busy!==""} onClick={()=>void execute("submit",()=>api.businessAction(businessID,`payouts/${id}/submit-draft`))}>{t(($)=>$.actions.submit_draft)} · {text(row,"period_key")}</button>}</div>})}</div></div>
        </Section>
      </div>}
    </div>
  );
}
