"use client";

import { useState } from "react";
import type { FormEvent } from "react";
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
import { useT } from "../i18n";

const CLIENT_STATUSES = ["active", "prospect", "paused", "leaving", "lost"] as const;
const SERVICE_TYPES = ["development", "support", "seo", "content"] as const;
const AGREEMENT_MODELS = ["fixed", "cap", "time_material", "project"] as const;

type TT = (key: string, options?: { defaultValue?: string }) => string;

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
  const { contractors } = useElbaDirectory();

  const clientID = client ? String(client.id) : "";
  const agreements = (data.agreements ?? []).filter((row) => String(row.client_id) === clientID);
  const projects = (data.projects ?? []).filter((row) => String(row.client_id) === clientID);
  const payers = (data.payers ?? []).filter((row) => String(row.client_id) === clientID);
  const receivables = (data.receivables ?? [])
    .filter((row) => String(row.client_id) === clientID)
    .sort((a, b) => String(a.due_on ?? a.period_start ?? "").localeCompare(String(b.due_on ?? b.period_start ?? "")));
  const payerInns = new Set(payers.map((row) => text(row, "inn")).filter(Boolean));
  const bankRows = (data.transactions ?? [])
    .filter((row) => String(row.direction) === "inbound" && payerInns.has(text(row, "counterparty_inn")))
    .slice(0, 8);

  const run = async (key: string, action: () => Promise<unknown>) => {
    setBusy(key); setError("");
    try {
      await action();
      await onChanged();
    } catch (cause) {
      setError(cause instanceof Error ? cause.message : String(cause));
    } finally {
      setBusy("");
    }
  };

  return (
    <Sheet open={!!client} onOpenChange={(open) => { if (!open) onClose(); }}>
      <SheetContent side="right" className="w-full overflow-y-auto p-0 data-[side=right]:sm:max-w-2xl">
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

          <CardSection title={t(($) => $.card.details)}>
            <form className="flex flex-wrap items-center gap-1.5" onSubmit={(event) => { const fd = formData(event); void run("client", () => api.businessAction(businessID, `clients/${clientID}`, { status: fd.get("status"), primary_payment_channel: fd.get("channel"), notes: fd.get("notes") }, "PATCH")); }}>
              <NativeSelect size="sm" name="status" defaultValue={text(client, "status")}>
                {CLIENT_STATUSES.map((value) => <NativeSelectOption key={value} value={value}>{tt(`values.${value}`, { defaultValue: value })}</NativeSelectOption>)}
              </NativeSelect>
              <NativeSelect size="sm" name="channel" defaultValue={text(client, "primary_payment_channel")}>
                <NativeSelectOption value="bank">{tt("values.bank", { defaultValue: "bank" })}</NativeSelectOption>
                <NativeSelectOption value="personal_card">{tt("values.personal_card", { defaultValue: "personal_card" })}</NativeSelectOption>
              </NativeSelect>
              <Input name="notes" className="h-7 min-w-56 flex-1 text-xs" defaultValue={text(client, "notes")} placeholder={t(($) => $.fields.notes)} />
              <Button size="sm" variant="outline" className="h-7 text-xs" disabled={busy !== ""}>{t(($) => $.actions.save)}</Button>
            </form>
          </CardSection>

          <CardSection title={t(($) => $.sections.agreements)}>
            <div className="space-y-2">
              {agreements.length === 0 && <div className="text-xs text-muted-foreground">{t(($) => $.empty)}</div>}
              {agreements.map((agreement) => {
                const id = String(agreement.id);
                return (
                  <form key={id} className="flex flex-wrap items-center gap-1.5 rounded-lg border p-2" onSubmit={(event) => { const fd = formData(event); void run(`agr-${id}`, () => api.businessAction(businessID, `agreements/${id}`, { amount_rub: fd.get("amount"), cap_rub: fd.get("cap"), hourly_rate_rub: fd.get("hourly"), invoice_day: fd.get("day"), status: fd.get("status"), needs_review: fd.get("review") === "on" }, "PATCH")); }}>
                    <span className="min-w-0 flex-1 truncate text-xs font-medium" title={text(agreement, "name")}>{text(agreement, "name")}</span>
                    <span className={cn("inline-flex shrink-0 items-center rounded px-1.5 py-0.5 text-[11px] font-medium", "bg-muted text-muted-foreground")}>{tt(`values.${text(agreement, "model")}`, { defaultValue: text(agreement, "model") })}</span>
                    <label className="flex items-center gap-1 text-[11px] text-muted-foreground">{tt("columns.amount_rub", { defaultValue: "amount" })}<Input name="amount" inputMode="decimal" className="h-7 w-24 text-xs" defaultValue={agreement.amount_rub ? String(Number(agreement.amount_rub)) : ""} /></label>
                    <label className="flex items-center gap-1 text-[11px] text-muted-foreground">{tt("columns.cap_rub", { defaultValue: "cap" })}<Input name="cap" inputMode="decimal" className="h-7 w-20 text-xs" defaultValue={agreement.cap_rub ? String(Number(agreement.cap_rub)) : ""} /></label>
                    <label className="flex items-center gap-1 text-[11px] text-muted-foreground">{tt("columns.hourly_rate_rub", { defaultValue: "rate" })}<Input name="hourly" inputMode="decimal" className="h-7 w-16 text-xs" defaultValue={agreement.hourly_rate_rub ? String(Number(agreement.hourly_rate_rub)) : ""} /></label>
                    <label className="flex items-center gap-1 text-[11px] text-muted-foreground">{tt("columns.invoice_day", { defaultValue: "day" })}<Input name="day" inputMode="numeric" className="h-7 w-12 text-xs" defaultValue={agreement.invoice_day ? String(agreement.invoice_day) : ""} /></label>
                    <NativeSelect size="sm" name="status" defaultValue={text(agreement, "status")}>
                      <NativeSelectOption value="active">{tt("values.active", { defaultValue: "active" })}</NativeSelectOption>
                      <NativeSelectOption value="archived">{tt("values.archived", { defaultValue: "archived" })}</NativeSelectOption>
                    </NativeSelect>
                    <label className="flex items-center gap-1 text-[11px] text-muted-foreground"><input type="checkbox" name="review" defaultChecked={isTrue(agreement.needs_review)} />{tt("columns.needs_review", { defaultValue: "review" })}</label>
                    <Button size="sm" variant="outline" className="h-7 text-xs" disabled={busy !== ""}>{t(($) => $.actions.save)}</Button>
                  </form>
                );
              })}
              <form className="flex flex-wrap items-center gap-1.5" onSubmit={(event) => { const fd = formData(event); void run("agr-add", () => api.businessAction(businessID, "agreements", { client_id: clientID, service_type: fd.get("service"), agreement_key: `manual-${crypto.randomUUID().slice(0, 8)}`, version: 1, name: fd.get("name"), model: fd.get("model"), amount_rub: fd.get("amount") || null, due_days: 7, period_months: 1, payment_channel: text(client, "primary_payment_channel") || "bank", effective_from: new Date().toISOString().slice(0, 10), status: "active", is_estimate: false, needs_review: false, terms: {} })); event.currentTarget.reset(); }}>
                <Input required name="name" className="h-7 w-44 text-xs" placeholder={t(($) => $.fields.name)} />
                <NativeSelect size="sm" name="service">{SERVICE_TYPES.map((value) => <NativeSelectOption key={value} value={value}>{tt(`values.${value}`, { defaultValue: value })}</NativeSelectOption>)}</NativeSelect>
                <NativeSelect size="sm" name="model">{AGREEMENT_MODELS.map((value) => <NativeSelectOption key={value} value={value}>{tt(`values.${value}`, { defaultValue: value })}</NativeSelectOption>)}</NativeSelect>
                <Input name="amount" inputMode="decimal" className="h-7 w-24 text-xs" placeholder={t(($) => $.fields.amount)} />
                <Button size="sm" variant="outline" className="h-7 text-xs" disabled={busy !== ""}>{t(($) => $.actions.add_agreement)}</Button>
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
                if (!editable) {
                  return (
                    <div key={id} className="flex flex-wrap items-center gap-2 rounded-lg border p-2 text-xs">
                      <span className="tabular-nums text-muted-foreground">{monthKey}</span>
                      <span className="min-w-0 flex-1 truncate">{text(row, "notes") || text(row, "period_key")}</span>
                      <span className="font-medium tabular-nums">{rub(row.paid_amount_rub)}</span>
                      <span className="inline-flex items-center rounded bg-emerald-500/10 px-1.5 py-0.5 text-[11px] font-medium text-emerald-600 dark:text-emerald-400">{tt(`values.${status}`, { defaultValue: status })}</span>
                    </div>
                  );
                }
                return (
                  <form key={id} className="flex flex-wrap items-center gap-1.5 rounded-lg border p-2" onSubmit={(event) => { const fd = formData(event); void run(`rcv-${id}`, () => api.businessAction(businessID, `receivables/${id}`, { planned_amount_rub: fd.get("planned"), due_on: fd.get("due"), needs_review: fd.get("review") === "on" }, "PATCH")); }}>
                    <span className="tabular-nums text-xs text-muted-foreground">{monthKey}</span>
                    <span className="min-w-0 flex-1 truncate text-xs" title={text(row, "notes")}>{text(row, "notes") || text(row, "period_key")}</span>
                    <Input name="planned" inputMode="decimal" className="h-7 w-24 text-xs" defaultValue={String(Number(row.planned_amount_rub ?? 0))} />
                    <Input name="due" type="date" className="h-7 w-auto text-xs" defaultValue={text(row, "due_on")} />
                    <label className="flex items-center gap-1 text-[11px] text-muted-foreground"><input type="checkbox" name="review" defaultChecked={isTrue(row.needs_review)} />{tt("columns.needs_review", { defaultValue: "review" })}</label>
                    <Button size="sm" variant="outline" className="h-7 text-xs" disabled={busy !== ""}>{t(($) => $.actions.save)}</Button>
                    <Button type="button" size="sm" variant="ghost" className="h-7 text-xs text-muted-foreground" disabled={busy !== ""} onClick={() => void run(`skip-${id}`, () => api.businessAction(businessID, `receivables/${id}`, { status: "skipped" }, "PATCH"))}>{t(($) => $.actions.skip)}</Button>
                  </form>
                );
              })}
            </div>
          </CardSection>

          <CardSection title={t(($) => $.sections.projects)}>
            <div className="space-y-1.5">
              {projects.map((row) => {
                const clientContractor = text(payers.find((payer) => text(payer, "elba_contractor_id")) ?? {}, "elba_contractor_id");
                const state = projectBillingState(row, clientContractor);
                return (
                  <div key={String(row.id)} className="flex items-center gap-2 text-xs">
                    <span className="min-w-0 flex-1 truncate">{text(row, "project_title")}</span>
                    <span className="text-muted-foreground">{text(row, "workspace_name")}</span>
                    <span className="inline-flex items-center rounded bg-muted px-1.5 py-0.5 text-[11px] font-medium text-muted-foreground">{tt(`values.${text(row, "service_type")}`, { defaultValue: text(row, "service_type") })}</span>
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
              <form className="flex flex-wrap items-center gap-1.5" onSubmit={(event) => { const fd = formData(event); void run("map", () => api.businessAction(businessID, `clients/${clientID}/projects`, { workspace_id: fd.get("workspace"), project_id: fd.get("project"), service_type: fd.get("service"), billable: true }, "PUT")); event.currentTarget.reset(); }}>
                <Input required name="workspace" className="h-7 w-36 text-xs" placeholder={t(($) => $.fields.workspace_id)} />
                <Input required name="project" className="h-7 w-36 text-xs" placeholder={t(($) => $.fields.project_id)} />
                <NativeSelect size="sm" name="service">{SERVICE_TYPES.map((value) => <NativeSelectOption key={value} value={value}>{tt(`values.${value}`, { defaultValue: value })}</NativeSelectOption>)}</NativeSelect>
                <Button size="sm" variant="outline" className="h-7 text-xs" disabled={busy !== ""}>{t(($) => $.actions.map_project)}</Button>
              </form>
            </div>
          </CardSection>

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
                        <Button size="sm" variant="outline" className="h-7 text-xs" disabled={busy !== ""}>{t(($) => $.billing.apply)}</Button>
                      </>
                    )}
                  </form>
                );
              })}
              <form className="flex flex-wrap items-center gap-1.5" onSubmit={(event) => { const fd = formData(event); void run("payer", () => api.businessAction(businessID, `clients/${clientID}/payers`, { name: fd.get("name"), inn: fd.get("inn") || null, status: "active", payment_channel: text(client, "primary_payment_channel") || "bank" })); event.currentTarget.reset(); }}>
                <Input required name="name" className="h-7 w-52 text-xs" placeholder={t(($) => $.fields.name)} />
                <Input name="inn" inputMode="numeric" className="h-7 w-32 text-xs" placeholder={tt("columns.inn", { defaultValue: "INN" })} />
                <Button size="sm" variant="outline" className="h-7 text-xs" disabled={busy !== ""}>{t(($) => $.actions.add_payer)}</Button>
              </form>
            </div>
          </CardSection>

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
        </>}
      </SheetContent>
    </Sheet>
  );
}
