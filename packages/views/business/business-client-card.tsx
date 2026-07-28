"use client";

import { useMemo, useState } from "react";
import type { FormEvent } from "react";
import { toast } from "sonner";
import { api } from "@multica/core/api";
import type { BusinessRow, BusinessSnapshot } from "@multica/core/types";
import { Button } from "@multica/ui/components/ui/button";
import { Input } from "@multica/ui/components/ui/input";
import {
  NativeSelect,
  NativeSelectOption,
} from "@multica/ui/components/ui/native-select";
import {
  Sheet,
  SheetContent,
  SheetDescription,
  SheetHeader,
  SheetTitle,
} from "@multica/ui/components/ui/sheet";
import { cn } from "@multica/ui/lib/utils";
import { projectBillingState, useElbaDirectory } from "./business-billing-tab";
import { BusinessCapMeter, hasCapMeter } from "./business-cap-meter";
import { BusinessClientContracts } from "./business-client-contracts";
import { useT } from "../i18n";

type ClientTab = "overview" | "contracts" | "finance" | "projects" | "payers" | "bank";

const CLIENT_STATUSES = ["active", "prospect", "paused", "leaving", "lost"] as const;
const SERVICE_TYPES = ["development", "support", "seo", "content"] as const;
const AGREEMENT_MODELS = ["fixed", "cap", "time_material", "project"] as const;
// Mirrors the CHECK constraint on business_agreement.status. The select used to
// offer "archived", which the database has never accepted — every archive
// attempt came back as a 500.
const AGREEMENT_STATUSES = ["draft", "active", "paused", "expired", "superseded"] as const;
// A ceiling means different things per client: for some it is a hard limit, for
// others just the line above which we warn them.
const AGREEMENT_CAP_MODES = ["strict", "advisory"] as const;

// Every agreement used to render all three money inputs, so a fixed-fee deal
// showed an empty "rate per hour" and a T&M deal showed an empty "amount" —
// the main reason the finance tab read as noise. Each model now shows only the
// fields it actually uses; the rest keep their stored values untouched because
// the PATCH endpoint treats an absent field as "leave as is".
const AGREEMENT_MONEY_FIELDS: Record<string, readonly ("amount" | "cap" | "hourly")[]> = {
  fixed: ["amount"],
  project: ["amount"],
  cap: ["cap", "hourly"],
  time_material: ["hourly"],
};

type TT = (
  key: string,
  options?: { defaultValue?: string; count?: number },
) => string;

function text(row: BusinessRow, key: string): string {
  const value = row[key];
  if (value === null || value === undefined) return "";
  return String(value);
}

function rub(value: unknown): string {
  const amount = Number(value ?? 0);
  return new Intl.NumberFormat("ru-RU", { style: "currency", currency: "RUB", maximumFractionDigits: 0 }).format(Number.isFinite(amount) ? amount : 0);
}

function isTrue(value: unknown): boolean {
  return value === true || value === "t" || value === "true";
}

function formData(event: FormEvent<HTMLFormElement>): FormData {
  event.preventDefault();
  return new FormData(event.currentTarget);
}

function Field({ label, children }: { label: string; children: React.ReactNode }) {
  return (
    <label className="space-y-0.5">
      <span className="block text-[10px] uppercase tracking-wide text-muted-foreground">{label}</span>
      {children}
    </label>
  );
}

function Tag({ children }: { children: React.ReactNode }) {
  return (
    <span className="inline-flex shrink-0 items-center rounded bg-muted px-1.5 py-0.5 text-[11px] font-medium text-muted-foreground">
      {children}
    </span>
  );
}

function ReceivableBadge({ status, overdue, label }: { status: string; overdue: boolean; label: string }) {
  const tone = status === "paid" || status === "partially_paid"
    ? "bg-emerald-500/10 text-emerald-600 dark:text-emerald-400"
    : overdue || status === "overdue"
      ? "bg-destructive/10 text-destructive"
      : "bg-muted text-muted-foreground";
  return <span className={cn("inline-flex shrink-0 items-center rounded px-1.5 py-0.5 text-[11px] font-medium", tone)}>{label}</span>;
}

function CardSection({ title, children }: { title: string; children: React.ReactNode }) {
  return (
    <section className="space-y-2 border-t px-4 py-3 first:border-t-0">
      <h3 className="text-[11px] font-medium uppercase tracking-wide text-muted-foreground">{title}</h3>
      {children}
    </section>
  );
}

export function BusinessClientCard({ businessID, client, data, onClose, onChanged }: {
  businessID: string;
  client: BusinessRow | null;
  data: BusinessSnapshot;
  onClose: () => void;
  onChanged: () => Promise<void>;
}) {
  const { t, i18n } = useT("business");
  const tt = t as unknown as TT;
  const locale = i18n?.language || "ru";
  const [busy, setBusy] = useState("");
  const [error, setError] = useState("");
  const [tab, setTab] = useState<ClientTab>("overview");
  const [workspaceFilter, setWorkspaceFilter] = useState("");
  const [selectedProjects, setSelectedProjects] = useState<string[]>([]);
  const { contractors } = useElbaDirectory();

  const clientID = client ? String(client.id) : "";
  const contracts = (data.contracts ?? []).filter((row) => String(row.client_id) === clientID);
  const clientTabs: { key: ClientTab; label: string }[] = [
    { key: "overview", label: tt("card.tab_overview", { defaultValue: "Overview" }) },
    { key: "contracts", label: tt("contracts.tab", { defaultValue: "Contracts" }) },
    { key: "finance", label: tt("card.tab_finance", { defaultValue: "Finance" }) },
    { key: "projects", label: t(($) => $.sections.projects) },
    { key: "payers", label: t(($) => $.sections.payers) },
    { key: "bank", label: t(($) => $.card.bank) },
  ];
  const agreements = (data.agreements ?? []).filter((row) => String(row.client_id) === clientID);
  const projects = (data.projects ?? []).filter((row) => String(row.client_id) === clientID);
  const linkedProjectIDs = useMemo(
    () => new Set(projects.map((row) => String(row.project_id))),
    [projects],
  );
  const unlinkedProjects = useMemo(
    () => (data.available_projects ?? []).filter((row) => !linkedProjectIDs.has(String(row.project_id))),
    [data.available_projects, linkedProjectIDs],
  );
  const selectableProjects = useMemo(
    () => unlinkedProjects.filter((row) => !workspaceFilter || String(row.workspace_id) === workspaceFilter),
    [unlinkedProjects, workspaceFilter],
  );
  const payers = (data.payers ?? []).filter((row) => String(row.client_id) === clientID);
  const receivables = (data.receivables ?? [])
    .filter((row) => String(row.client_id) === clientID)
    .sort((a, b) => String(a.due_on ?? a.period_start ?? "").localeCompare(String(b.due_on ?? b.period_start ?? "")));
  // A receivable row carries only its agreement id, but "Остаток по контра…"
  // tells the owner nothing about which deal it belongs to.
  const agreementNames = new Map(agreements.map((row) => [String(row.id), text(row, "name")]));
  const payerInns = new Set(payers.map((row) => text(row, "inn")).filter(Boolean));
  const bankRows = (data.transactions ?? [])
    .filter((row) => String(row.direction) === "inbound" && payerInns.has(text(row, "counterparty_inn")))
    .slice(0, 8);

  const run = async (key: string, action: () => Promise<unknown>, successMessage?: string) => {
    setBusy(key); setError("");
    try {
      await action();
      await onChanged();
      toast.success(successMessage ?? t(($) => $.success));
    } catch (cause) {
      const message = cause instanceof Error ? cause.message : String(cause);
      setError(message);
      toast.error(message);
    } finally {
      setBusy("");
    }
  };

  const toggleProject = (projectID: string) => {
    setSelectedProjects((prev) => (
      prev.includes(projectID) ? prev.filter((id) => id !== projectID) : [...prev, projectID]
    ));
  };

  return (
    <Sheet open={!!client} onOpenChange={(open) => {
      if (!open) {
        setSelectedProjects([]);
        setWorkspaceFilter("");
        setError("");
        setTab("overview");
        onClose();
      }
    }}>
      <SheetContent side="right" className="w-full overflow-y-auto p-0 data-[side=right]:sm:max-w-3xl">
        {client && <>
          <SheetHeader className="border-b px-4 py-3">
            <SheetTitle className="text-base">{text(client, "canonical_name")}</SheetTitle>
            <SheetDescription className="text-xs">
              {tt(`values.${text(client, "status")}`, { defaultValue: text(client, "status") })}
              {" · "}
              {tt(`values.${text(client, "primary_payment_channel")}`, { defaultValue: text(client, "primary_payment_channel") })}
            </SheetDescription>
          </SheetHeader>

          {error && <div className="mx-4 mt-3 rounded-lg border border-destructive/40 bg-destructive/5 p-2 text-xs text-destructive">{error}</div>}

          <div className="sticky top-0 z-10 flex gap-1 overflow-x-auto border-b bg-surface-raised px-2">
            {clientTabs.map((entry) => (
              <button
                key={entry.key}
                type="button"
                onClick={() => setTab(entry.key)}
                className={cn(
                  "shrink-0 whitespace-nowrap border-b-2 px-3 py-2 text-xs transition-colors",
                  tab === entry.key ? "border-primary font-medium text-foreground" : "border-transparent text-muted-foreground hover:text-foreground",
                )}
              >
                {entry.label}
              </button>
            ))}
          </div>

          {/* Tab panels share one wrapper so CardSection's `first:border-t-0`
              can match again. Without it the sections were siblings of the
              sticky tab strip, so the first one always drew its own top border
              right under the strip's bottom border — a double rule on every
              tab. */}
          <div>
          {tab === "overview" && (
          <CardSection title={t(($) => $.card.details)}>
            <form className="flex flex-wrap items-end gap-1.5" onSubmit={(event) => { const fd = formData(event); void run("client", () => api.businessAction(businessID, `clients/${clientID}`, { status: fd.get("status"), primary_payment_channel: fd.get("channel"), notes: fd.get("notes"), transit_commission_percent: fd.get("transit_commission"), transit_tax_percent: fd.get("transit_tax") }, "PATCH")); }}>
              <NativeSelect size="sm" name="status" defaultValue={text(client, "status")}>
                {CLIENT_STATUSES.map((value) => <NativeSelectOption key={value} value={value}>{tt(`values.${value}`, { defaultValue: value })}</NativeSelectOption>)}
              </NativeSelect>
              <NativeSelect size="sm" name="channel" defaultValue={text(client, "primary_payment_channel")}>
                <NativeSelectOption value="bank">{tt("values.bank", { defaultValue: "bank" })}</NativeSelectOption>
                <NativeSelectOption value="personal_card">{tt("values.personal_card", { defaultValue: "personal_card" })}</NativeSelectOption>
              </NativeSelect>
              <Input name="notes" className="h-7 min-w-56 flex-1 text-xs" defaultValue={text(client, "notes")} placeholder={t(($) => $.fields.notes)} />
              {/* Empty means the client is not a pass-through one at all; 0 means
                  they are, and the whole difference goes back to them. */}
              <Field label={tt("columns.transit_commission_percent", { defaultValue: "transit %" })}>
                <Input name="transit_commission" inputMode="decimal" className="h-7 w-20 text-xs" defaultValue={client.transit_commission_percent == null ? "" : String(Number(client.transit_commission_percent))} />
              </Field>
              <Field label={tt("columns.transit_tax_percent", { defaultValue: "tax %" })}>
                <Input name="transit_tax" inputMode="decimal" className="h-7 w-20 text-xs" defaultValue={client.transit_tax_percent == null ? "" : String(Number(client.transit_tax_percent))} />
              </Field>
              <Button type="submit" size="sm" variant="outline" className="h-7 text-xs" disabled={busy !== ""}>{t(($) => $.actions.save)}</Button>
            </form>
          </CardSection>
          )}

          {tab === "contracts" && (
          <CardSection title={tt("contracts.tab", { defaultValue: "Contracts" })}>
            <BusinessClientContracts businessID={businessID} clientID={clientID} contracts={contracts} locale={locale} onChanged={onChanged} />
          </CardSection>
          )}

          {tab === "finance" && (<>
          <CardSection title={t(($) => $.sections.agreements)}>
            <div className="space-y-2">
              {agreements.length === 0 && <div className="text-xs text-muted-foreground">{t(($) => $.empty)}</div>}
              {agreements.map((agreement) => {
                const id = String(agreement.id);
                const money = AGREEMENT_MONEY_FIELDS[text(agreement, "model")] ?? ["amount"];
                return (
                  <form key={id} className="space-y-2 rounded-lg border p-2.5" onSubmit={(event) => { const fd = formData(event); void run(`agr-${id}`, () => api.businessAction(businessID, `agreements/${id}`, { name: fd.get("name"), amount_rub: fd.get("amount"), cap_rub: fd.get("cap"), cap_mode: fd.get("cap_mode"), hourly_rate_rub: fd.get("hourly"), invoice_day: fd.get("day"), status: fd.get("status"), needs_review: fd.get("review") === "on" }, "PATCH")); }}>
                    <div className="flex items-center gap-1.5">
                      <Input required name="name" defaultValue={text(agreement, "name")} aria-label={t(($) => $.fields.name)} className="h-7 min-w-0 flex-1 text-xs font-medium" />
                      <Tag>{tt(`values.${text(agreement, "service_type")}`, { defaultValue: text(agreement, "service_type") })}</Tag>
                      <Tag>{tt(`values.${text(agreement, "model")}`, { defaultValue: text(agreement, "model") })}</Tag>
                    </div>
                    {hasCapMeter(agreement) && (
                      <BusinessCapMeter agreement={agreement} charges={data.billing_tasks ?? []} locale={locale} />
                    )}
                    <div className="flex flex-wrap items-end gap-2">
                      {money.includes("amount") && (
                        <Field label={tt("columns.amount_rub", { defaultValue: "amount" })}>
                          <Input name="amount" inputMode="decimal" className="h-7 w-28 text-xs" defaultValue={agreement.amount_rub ? String(Number(agreement.amount_rub)) : ""} />
                        </Field>
                      )}
                      {money.includes("cap") && (
                        <Field label={tt("columns.cap_rub", { defaultValue: "cap" })}>
                          <Input name="cap" inputMode="decimal" className="h-7 w-28 text-xs" defaultValue={agreement.cap_rub ? String(Number(agreement.cap_rub)) : ""} />
                        </Field>
                      )}
                      {money.includes("cap") && (
                        <Field label={tt("columns.cap_mode", { defaultValue: "cap mode" })}>
                          <NativeSelect size="sm" name="cap_mode" defaultValue={text(agreement, "cap_mode") || "strict"}>
                            {AGREEMENT_CAP_MODES.map((value) => <NativeSelectOption key={value} value={value}>{tt(`values.${value}`, { defaultValue: value })}</NativeSelectOption>)}
                          </NativeSelect>
                        </Field>
                      )}
                      {money.includes("hourly") && (
                        <Field label={tt("columns.hourly_rate_rub", { defaultValue: "rate" })}>
                          <Input name="hourly" inputMode="decimal" className="h-7 w-24 text-xs" defaultValue={agreement.hourly_rate_rub ? String(Number(agreement.hourly_rate_rub)) : ""} />
                        </Field>
                      )}
                      <Field label={tt("columns.invoice_day", { defaultValue: "day" })}>
                        <Input name="day" inputMode="numeric" className="h-7 w-14 text-xs" defaultValue={agreement.invoice_day ? String(agreement.invoice_day) : ""} />
                      </Field>
                      <Field label={tt("columns.status", { defaultValue: "status" })}>
                        <NativeSelect size="sm" name="status" defaultValue={text(agreement, "status")}>
                          {AGREEMENT_STATUSES.map((value) => <NativeSelectOption key={value} value={value}>{tt(`values.${value}`, { defaultValue: value })}</NativeSelectOption>)}
                        </NativeSelect>
                      </Field>
                      <label className="flex h-7 items-center gap-1 text-[11px] text-muted-foreground"><input type="checkbox" name="review" defaultChecked={isTrue(agreement.needs_review)} />{tt("columns.needs_review", { defaultValue: "review" })}</label>
                      <Button type="submit" size="sm" variant="outline" className="ml-auto h-7 text-xs" disabled={busy !== ""}>{t(($) => $.actions.save)}</Button>
                    </div>
                  </form>
                );
              })}
              <form className="flex flex-wrap items-center gap-1.5" onSubmit={(event) => { const fd = formData(event); void run("agr-add", () => api.businessAction(businessID, "agreements", { client_id: clientID, service_type: fd.get("service"), agreement_key: `manual-${crypto.randomUUID().slice(0, 8)}`, version: 1, name: fd.get("name"), model: fd.get("model"), amount_rub: fd.get("amount") || null, due_days: 7, period_months: 1, payment_channel: text(client, "primary_payment_channel") || "bank", effective_from: new Date().toISOString().slice(0, 10), status: "active", is_estimate: false, needs_review: false, terms: {} })); event.currentTarget.reset(); }}>
                <Input required name="name" className="h-7 w-44 text-xs" placeholder={t(($) => $.fields.name)} />
                <NativeSelect size="sm" name="service">{SERVICE_TYPES.map((value) => <NativeSelectOption key={value} value={value}>{tt(`values.${value}`, { defaultValue: value })}</NativeSelectOption>)}</NativeSelect>
                <NativeSelect size="sm" name="model">{AGREEMENT_MODELS.map((value) => <NativeSelectOption key={value} value={value}>{tt(`values.${value}`, { defaultValue: value })}</NativeSelectOption>)}</NativeSelect>
                <Input name="amount" inputMode="decimal" className="h-7 w-24 text-xs" placeholder={t(($) => $.fields.amount)} />
                <Button type="submit" size="sm" variant="outline" className="h-7 text-xs" disabled={busy !== ""}>{t(($) => $.actions.add_agreement)}</Button>
              </form>
            </div>
          </CardSection>

          <CardSection title={t(($) => $.sections.receivables)}>
            <div className="space-y-1.5">
              {receivables.length === 0 && <div className="text-xs text-muted-foreground">{t(($) => $.empty)}</div>}
              {receivables.map((row) => {
                const id = String(row.id);
                const status = text(row, "status");
                const editable = status !== "paid" && status !== "partially_paid";
                const monthKey = String(row.due_on ?? row.period_start ?? "").slice(0, 7);
                const overdue = isTrue(row.is_overdue);
                const title = text(row, "notes") || agreementNames.get(String(row.agreement_id)) || text(row, "period_key");
                const header = (
                  <div className="flex items-center gap-2 text-xs">
                    <span className="shrink-0 tabular-nums text-muted-foreground">{monthKey}</span>
                    <span className="min-w-0 flex-1 truncate" title={title}>{title}</span>
                    <ReceivableBadge
                      status={status}
                      overdue={overdue}
                      label={overdue && editable ? tt("values.overdue", { defaultValue: "overdue" }) : tt(`values.${status}`, { defaultValue: status })}
                    />
                  </div>
                );
                if (!editable) {
                  return (
                    <div key={id} className="space-y-1.5 rounded-lg border p-2.5">
                      {header}
                      <div className="flex flex-wrap items-baseline gap-x-4 gap-y-1 text-xs">
                        <span className="text-muted-foreground">{tt("columns.planned_amount_rub", { defaultValue: "planned" })}: <span className="font-medium tabular-nums text-foreground">{rub(row.planned_amount_rub)}</span></span>
                        <span className="text-muted-foreground">{tt("columns.paid_amount_rub", { defaultValue: "paid" })}: <span className="font-medium tabular-nums text-foreground">{rub(row.paid_amount_rub)}</span></span>
                      </div>
                    </div>
                  );
                }
                return (
                  <form key={id} className="space-y-2 rounded-lg border p-2.5" onSubmit={(event) => { const fd = formData(event); void run(`rcv-${id}`, () => api.businessAction(businessID, `receivables/${id}`, { planned_amount_rub: fd.get("planned"), due_on: fd.get("due"), needs_review: fd.get("review") === "on" }, "PATCH")); }}>
                    {header}
                    <div className="flex flex-wrap items-end gap-2">
                      <Field label={tt("columns.planned_amount_rub", { defaultValue: "planned" })}>
                        <Input name="planned" inputMode="decimal" className="h-7 w-28 text-xs" defaultValue={String(Number(row.planned_amount_rub ?? 0))} />
                      </Field>
                      <Field label={tt("columns.due_on", { defaultValue: "due" })}>
                        <Input name="due" type="date" className="h-7 w-auto text-xs" defaultValue={text(row, "due_on")} />
                      </Field>
                      <label className="flex h-7 items-center gap-1 text-[11px] text-muted-foreground"><input type="checkbox" name="review" defaultChecked={isTrue(row.needs_review)} />{tt("columns.needs_review", { defaultValue: "review" })}</label>
                      <Button type="submit" size="sm" variant="outline" className="ml-auto h-7 text-xs" disabled={busy !== ""}>{t(($) => $.actions.save)}</Button>
                      <Button type="button" size="sm" variant="ghost" className="h-7 text-xs text-muted-foreground" disabled={busy !== ""} onClick={() => void run(`skip-${id}`, () => api.businessAction(businessID, `receivables/${id}`, { status: "skipped" }, "PATCH"))}>{t(($) => $.actions.skip)}</Button>
                    </div>
                  </form>
                );
              })}
            </div>
          </CardSection>
          </>)}

          {tab === "projects" && (
          <CardSection title={t(($) => $.sections.projects)}>
            <div className="space-y-3">
              {projects.length === 0 && (
                <div className="text-xs text-muted-foreground">{tt("card.no_linked_projects", { defaultValue: "No projects linked yet." })}</div>
              )}
              {projects.map((row) => {
                const clientContractor = text(payers.find((payer) => text(payer, "elba_contractor_id")) ?? {}, "elba_contractor_id");
                const state = projectBillingState(row, clientContractor);
                const projectType = text(row, "project_type") || text(row, "service_type");
                return (
                  <div key={String(row.id)} className="flex flex-wrap items-center gap-2 text-xs">
                    <span className="min-w-0 flex-1 truncate font-medium" title={text(row, "project_title")}>{text(row, "project_title")}</span>
                    <span className="text-muted-foreground">{text(row, "workspace_name")}</span>
                    {projectType && (
                      <span className="inline-flex items-center rounded bg-muted px-1.5 py-0.5 text-[11px] font-medium text-muted-foreground">
                        {tt(`values.${projectType}`, { defaultValue: projectType })}
                      </span>
                    )}
                    <span className={cn(
                      "inline-flex items-center whitespace-nowrap rounded px-1.5 py-0.5 text-[11px] font-medium",
                      state === "linked" && "bg-emerald-500/10 text-emerald-600 dark:text-emerald-400",
                      state === "mismatch" && "bg-amber-500/10 text-amber-600 dark:text-amber-400",
                      state === "off" && "bg-muted text-muted-foreground",
                    )}>
                      {state === "linked" ? t(($) => $.billing.state_linked) : state === "mismatch" ? t(($) => $.billing.state_mismatch) : t(($) => $.billing.state_off)}
                    </span>
                  </div>
                );
              })}

              <form
                className="space-y-2 rounded-lg border p-3"
                onSubmit={(event) => {
                  event.preventDefault();
                  const chosenIDs = selectedProjects.length > 0
                    ? selectedProjects
                    : (workspaceFilter
                      ? selectableProjects.map((row) => String(row.project_id))
                      : []);
                  const candidates = unlinkedProjects.filter((row) => chosenIDs.includes(String(row.project_id)));
                  if (candidates.length === 0) {
                    const message = tt("card.select_projects_required", {
                      defaultValue: "Select at least one project, or choose a whole workspace.",
                    });
                    setError(message);
                    toast.error(message);
                    return;
                  }
                  void run(
                    "map",
                    async () => {
                      await Promise.all(candidates.map((row) => api.businessAction(businessID, `clients/${clientID}/projects`, {
                        workspace_id: row.workspace_id,
                        project_id: row.project_id,
                        billable: true,
                      }, "PUT")));
                      setSelectedProjects([]);
                      setWorkspaceFilter("");
                    },
                    tt("card.mapped_toast", { count: candidates.length, defaultValue: `Linked projects: ${candidates.length}` }),
                  );
                }}
              >
                <div className="text-[11px] font-medium text-muted-foreground">
                  {tt("card.link_projects", { defaultValue: "Link more projects" })}
                </div>
                <NativeSelect
                  size="sm"
                  name="workspace"
                  aria-label={t(($) => $.fields.workspace)}
                  value={workspaceFilter}
                  onChange={(event) => {
                    setWorkspaceFilter(event.target.value);
                    setSelectedProjects([]);
                  }}
                >
                  <NativeSelectOption value="">{tt("card.filter_all_workspaces", { defaultValue: "All workspaces" })}</NativeSelectOption>
                  {(data.available_workspaces ?? []).map((row) => (
                    <NativeSelectOption key={text(row, "workspace_id")} value={text(row, "workspace_id")}>
                      {t(($) => $.fields.entire_workspace)}: {text(row, "workspace_name")}
                    </NativeSelectOption>
                  ))}
                </NativeSelect>
                <div className="max-h-48 space-y-1 overflow-y-auto rounded-md border bg-background p-2">
                  {selectableProjects.length === 0 ? (
                    <div className="px-1 py-2 text-xs text-muted-foreground">
                      {tt("card.no_available_projects", { defaultValue: "No unlinked projects left." })}
                    </div>
                  ) : (
                    selectableProjects.map((row) => {
                      const id = text(row, "project_id");
                      const checked = selectedProjects.includes(id);
                      const projectType = text(row, "project_type");
                      return (
                        <label
                          key={id}
                          className={cn(
                            "flex cursor-pointer items-center gap-2 rounded-md px-1.5 py-1 text-xs hover:bg-muted/60",
                            checked && "bg-muted/40",
                          )}
                        >
                          <input
                            type="checkbox"
                            className="size-3.5 accent-primary"
                            checked={checked}
                            onChange={() => toggleProject(id)}
                          />
                          <span className="min-w-0 flex-1 truncate" title={`${text(row, "workspace_name")} · ${text(row, "project_title")}`}>
                            {text(row, "workspace_name")} · {text(row, "project_title")}
                          </span>
                          {projectType && (
                            <span className="shrink-0 rounded bg-muted px-1.5 py-0.5 text-[10px] text-muted-foreground">
                              {tt(`values.${projectType}`, { defaultValue: projectType })}
                            </span>
                          )}
                        </label>
                      );
                    })
                  )}
                </div>
                <Button type="submit" size="sm" variant="outline" className="h-7 text-xs" disabled={busy !== ""}>
                  {t(($) => $.actions.map_projects)}
                  {selectedProjects.length > 0 ? ` (${selectedProjects.length})` : workspaceFilter ? ` · ${selectableProjects.length}` : ""}
                </Button>
                {workspaceFilter && selectedProjects.length === 0 && selectableProjects.length > 0 && (
                  <p className="text-[11px] text-muted-foreground">
                    {tt("card.workspace_link_hint", {
                      count: selectableProjects.length,
                      defaultValue: `Without checkboxes, all ${selectableProjects.length} projects in this workspace will be linked.`,
                    })}
                  </p>
                )}
              </form>
            </div>
          </CardSection>
          )}

          {tab === "payers" && (
          <CardSection title={t(($) => $.sections.payers)}>
            <div className="space-y-1.5">
              {payers.map((row) => {
                const id = String(row.id);
                const bank = text(row, "payment_channel") === "bank";
                return (
                  <form key={id} className="flex flex-wrap items-center gap-1.5 text-xs" onSubmit={(event) => { const fd = formData(event); void run(`payer-${id}`, () => api.businessAction(businessID, `payers/${id}`, { elba_contractor_id: fd.get("contractor") || null, apply_contractor_to_projects: true }, "PATCH")); }}>
                    <span className="min-w-0 flex-1 truncate">{text(row, "name")}</span>
                    {text(row, "inn") && <span className="tabular-nums text-muted-foreground">{tt("columns.inn", { defaultValue: "INN" })} {text(row, "inn")}</span>}
                    {bank && (
                      <>
                        <NativeSelect size="sm" name="contractor" aria-label={t(($) => $.billing.contractor)} defaultValue={text(row, "elba_contractor_id")}>
                          <NativeSelectOption value="">{t(($) => $.billing.contractor_empty)}</NativeSelectOption>
                          {contractors.map((option) => {
                            const name = typeof option.name === "string" ? option.name : option.id;
                            const inn = typeof option.inn === "string" ? option.inn : "";
                            return <NativeSelectOption key={option.id} value={option.id}>{inn ? `${name} · ${inn}` : name}</NativeSelectOption>;
                          })}
                        </NativeSelect>
                        <Button type="submit" size="sm" variant="outline" className="h-7 text-xs" disabled={busy !== ""}>{t(($) => $.billing.apply)}</Button>
                      </>
                    )}
                  </form>
                );
              })}
              <form className="flex flex-wrap items-center gap-1.5" onSubmit={(event) => { const fd = formData(event); void run("payer", () => api.businessAction(businessID, `clients/${clientID}/payers`, { name: fd.get("name"), inn: fd.get("inn") || null, status: "active", payment_channel: text(client, "primary_payment_channel") || "bank" })); event.currentTarget.reset(); }}>
                <Input required name="name" className="h-7 w-52 text-xs" placeholder={t(($) => $.fields.name)} />
                <Input name="inn" inputMode="numeric" className="h-7 w-32 text-xs" placeholder={tt("columns.inn", { defaultValue: "INN" })} />
                <Button type="submit" size="sm" variant="outline" className="h-7 text-xs" disabled={busy !== ""}>{t(($) => $.actions.add_payer)}</Button>
              </form>
            </div>
          </CardSection>
          )}

          {tab === "bank" && (
          <CardSection title={t(($) => $.card.bank)}>
            <div className="space-y-1">
              {bankRows.length === 0 && <div className="text-xs text-muted-foreground">{t(($) => $.empty)}</div>}
              {bankRows.map((row) => (
                <div key={String(row.id)} className="flex items-center gap-2 text-xs">
                  <span className="tabular-nums text-muted-foreground">{new Date(text(row, "booked_on")).toLocaleDateString(locale)}</span>
                  <span className="font-medium tabular-nums">{rub(row.amount_rub)}</span>
                  <span className="min-w-0 flex-1 truncate text-muted-foreground" title={text(row, "purpose")}>{text(row, "purpose")}</span>
                </div>
              ))}
            </div>
          </CardSection>
          )}
          </div>
        </>}
      </SheetContent>
    </Sheet>
  );
}
