"use client";

// Business billing tab: everything that used to live in Settings -> Billing,
// re-anchored on business entities. A client's payer links to an Elba
// contractor (auto-suggested by INN) and the link is applied to every project
// mapped to that client, so the счёт/акт always targets the right legal
// entity. Workspace pricing defaults and the consolidated invoice action live
// here too.

import { useEffect, useMemo, useState } from "react";
import { useMutation, useQuery, useQueryClient } from "@tanstack/react-query";
import { toast } from "sonner";
import { api, ApiError } from "@multica/core/api";
import type { BusinessBillingRun, BusinessRow, BusinessSnapshot, ContractorPeriodGroup } from "@multica/core/types";
import { Button } from "@multica/ui/components/ui/button";
import { Input } from "@multica/ui/components/ui/input";
import {
  NativeSelect,
  NativeSelectOption,
} from "@multica/ui/components/ui/native-select";
import {
  Table,
  TableBody,
  TableCell,
  TableHead,
  TableHeader,
  TableRow,
} from "@multica/ui/components/ui/table";
import { useT } from "../i18n";

export const businessBillingKeys = {
  wsConfig: () => ["client-billing", "workspace-config"] as const,
  contractors: (orgId: string) => ["client-billing", "elba-contractors", orgId] as const,
  accounts: (orgId: string) => ["client-billing", "elba-bank-accounts", orgId] as const,
  orgs: () => ["client-billing", "elba-orgs"] as const,
  contractorConfigs: () => ["client-billing", "contractor-configs"] as const,
  invoiceable: () => ["client-billing", "invoiceable-groups"] as const,
  runs: (businessId: string) => ["business-billing-runs", businessId] as const,
};

type ElbaRow = { id: string; [key: string]: unknown };

function str(value: unknown): string {
  return typeof value === "string" ? value : "";
}

function contractorLabel(row: ElbaRow): string {
  const name = str(row.name) || str(row.fullName) || str(row.shortName);
  const inn = str(row.inn);
  if (name) return inn ? `${name} · ${inn}` : name;
  return inn || row.id;
}

function rub(value: unknown): string {
  const amount = Number(value ?? 0);
  return new Intl.NumberFormat("ru-RU", { style: "currency", currency: "RUB", maximumFractionDigits: 0 }).format(Number.isFinite(amount) ? amount : 0);
}

function text(row: BusinessRow, key: string): string {
  const value = row[key];
  if (value === null || value === undefined) return "";
  return String(value);
}

// Shared Elba directory (workspace org + contractors). The business page runs
// in the workspace that owns the Elba wiring, so workspace-scoped billing
// endpoints are correct here; project links are still written server-side via
// the business API to stay cross-workspace safe.
export function useElbaDirectory() {
  const wsConfig = useQuery({ queryKey: businessBillingKeys.wsConfig(), queryFn: () => api.getWorkspaceBillingConfig() });
  const orgId = wsConfig.data?.elba_org_id ?? "";
  const contractors = useQuery({
    queryKey: businessBillingKeys.contractors(orgId),
    queryFn: () => api.getElbaContractors(orgId),
    enabled: !!orgId,
    staleTime: 5 * 60_000,
    retry: false,
  });
  return {
    wsConfig: wsConfig.data,
    orgId,
    contractors: (contractors.data ?? []) as ElbaRow[],
    unavailable: contractors.isError && contractors.error instanceof ApiError && contractors.error.status === 503,
  };
}

type ProjectBillingState = "linked" | "mismatch" | "off";

export function projectBillingState(row: BusinessRow, contractorId: string): ProjectBillingState {
  const enabled = row.billing_enabled === true || row.billing_enabled === "t" || row.billing_enabled === "true";
  const linked = String(row.billing_contractor_id ?? "");
  if (enabled && contractorId && linked === contractorId) return "linked";
  if (enabled) return "mismatch";
  return "off";
}

export function BusinessBillingTab({ businessID, data, onChanged }: {
  businessID: string;
  data: BusinessSnapshot;
  onChanged: () => Promise<unknown>;
}) {
  const { t } = useT("business");
  const tt = t as unknown as (key: string, options?: { defaultValue?: string }) => string;
  const qc = useQueryClient();
  const { wsConfig, orgId, contractors, unavailable } = useElbaDirectory();

  const configs = useQuery({ queryKey: businessBillingKeys.contractorConfigs(), queryFn: () => api.listContractorBillingConfigs() });
  const groups = useQuery({ queryKey: businessBillingKeys.invoiceable(), queryFn: () => api.listInvoiceableContractorGroups() });

  const clientName = useMemo(() => {
    const map = new Map<string, string>();
    for (const row of data.clients ?? []) map.set(String(row.id), String(row.canonical_name ?? ""));
    return map;
  }, [data.clients]);

  const projectsByClient = useMemo(() => {
    const map = new Map<string, BusinessRow[]>();
    for (const row of data.projects ?? []) {
      const key = String(row.client_id);
      const list = map.get(key) ?? [];
      list.push(row);
      map.set(key, list);
    }
    return map;
  }, [data.projects]);

  const contractorByINN = useMemo(() => {
    const map = new Map<string, ElbaRow>();
    for (const row of contractors) {
      const inn = str(row.inn);
      if (inn) map.set(inn, row);
    }
    return map;
  }, [contractors]);

  const payers = useMemo(() =>
    [...(data.payers ?? [])].sort((a, b) =>
      (clientName.get(String(a.client_id)) ?? "").localeCompare(clientName.get(String(b.client_id)) ?? "", "ru")
      || text(a, "name").localeCompare(text(b, "name"), "ru"),
    ), [data.payers, clientName]);

  // Per-payer drafts: contractor + billing mode/fee. Seeded lazily from the
  // saved link, the INN auto-match, and the saved contractor config.
  const [drafts, setDrafts] = useState<Record<string, { contractor: string; mode: string; fee: string }>>({});
  const draftFor = (payer: BusinessRow): { contractor: string; mode: string; fee: string; suggested: boolean } => {
    const id = String(payer.id);
    const saved = text(payer, "elba_contractor_id");
    const suggestion = !saved ? contractorByINN.get(text(payer, "inn"))?.id ?? "" : "";
    const existing = drafts[id];
    const contractor = existing?.contractor ?? (saved || suggestion);
    const cfg = (configs.data ?? []).find((row) => row.elba_contractor_id === contractor);
    const clientProjects = projectsByClient.get(String(payer.client_id)) ?? [];
    const subscriptionProject = clientProjects.find((row) => text(row, "billing_mode") === "subscription");
    const projectMode = subscriptionProject ? "subscription" : (clientProjects[0] ? text(clientProjects[0], "billing_mode") || "postpaid" : "postpaid");
    const projectFee = subscriptionProject ? text(subscriptionProject, "billing_subscription_fee_rub") : "";
    return {
      contractor,
      mode: existing?.mode ?? (cfg?.mode ?? projectMode),
      fee: existing?.fee ?? (cfg?.subscription_fee_rub ? String(cfg.subscription_fee_rub) : projectFee),
      suggested: !saved && !existing?.contractor && !!suggestion,
    };
  };
  const setDraft = (payer: BusinessRow, patch: Partial<{ contractor: string; mode: string; fee: string }>) => {
    const id = String(payer.id);
    const current = draftFor(payer);
    setDrafts((prev) => ({ ...prev, [id]: { contractor: current.contractor, mode: current.mode, fee: current.fee, ...patch } }));
  };

  const saveMut = useMutation({
    mutationFn: async (input: { payerID: string; contractor: string; contractorName: string; mode: string; fee: string }) => {
      const result = await api.businessAction<{ billing_updated?: number; billing_created?: number }>(
        businessID,
        `payers/${input.payerID}`,
        { elba_contractor_id: input.contractor || null, apply_contractor_to_projects: true },
        "PATCH",
      );
      if (input.contractor) {
        await api.upsertContractorBillingConfig({
          elba_contractor_id: input.contractor,
          name: input.contractorName,
          mode: input.mode === "subscription" ? "subscription" : "postpaid",
          subscription_fee_rub: input.fee.trim() === "" ? 0 : Number(input.fee),
        });
      }
      return result;
    },
    onSuccess: async (result) => {
      const linked = Number(result?.billing_updated ?? 0) + Number(result?.billing_created ?? 0);
      toast.success(t(($) => $.billing.saved_toast, { linked }));
      qc.invalidateQueries({ queryKey: businessBillingKeys.contractorConfigs() });
      qc.invalidateQueries({ queryKey: businessBillingKeys.invoiceable() });
      await onChanged();
    },
    onError: (cause) => toast.error(cause instanceof Error ? cause.message : String(cause)),
  });

  const invoiceMut = useMutation({
    mutationFn: (group: ContractorPeriodGroup) => api.invoiceContractorPeriod(group.elba_contractor_id, group.starts_on, group.ends_on),
    onSuccess: (result) => {
      toast.success(t(($) => $.billing.invoice_done_toast, { amount: result.bill_rub.toLocaleString("ru-RU") }));
      qc.invalidateQueries({ queryKey: businessBillingKeys.invoiceable() });
    },
    onError: (cause) => toast.error(cause instanceof Error ? cause.message : String(cause)),
  });

  const contractorDisplay = (id: string): string => {
    const fromElba = contractors.find((row) => row.id === id);
    if (fromElba) return contractorLabel(fromElba);
    const cfg = (configs.data ?? []).find((row) => row.elba_contractor_id === id);
    return cfg?.name || id;
  };
  const payerByContractor = useMemo(() => {
    const map = new Map<string, BusinessRow>();
    for (const row of payers) {
      const id = text(row, "elba_contractor_id");
      if (id && !map.has(id)) map.set(id, row);
    }
    return map;
  }, [payers]);

  const fmtDate = (value: string) => value.slice(0, 10).split("-").reverse().join(".");

  return (
    <div className="space-y-6">
      <BillingRunsQueue businessID={businessID} onChanged={onChanged} />

      <section className="space-y-1.5">
        <h2 className="text-sm font-medium">{t(($) => $.billing.contractors_title)}</h2>
        <p className="text-[11px] text-muted-foreground">{t(($) => $.billing.contractors_hint)}</p>
        {unavailable && <p className="text-xs text-muted-foreground">{t(($) => $.billing.elba_unavailable)}</p>}
        <div className="rounded-lg border">
          <Table containerClassName="max-h-[60vh] overflow-auto">
            <TableHeader>
              <TableRow>
                <TableHead className="sticky top-0 z-10 h-8 w-40 bg-background text-xs">{t(($) => $.filters.client)}</TableHead>
                <TableHead className="sticky top-0 z-10 h-8 min-w-72 bg-background text-xs">{t(($) => $.billing.payer_and_contractor)}</TableHead>
                <TableHead className="sticky top-0 z-10 h-8 min-w-64 bg-background text-xs">{t(($) => $.billing.linked_projects)}</TableHead>
                <TableHead className="sticky top-0 z-10 h-8 min-w-64 bg-background text-xs">{t(($) => $.billing.calculation)}</TableHead>
                <TableHead className="sticky top-0 z-10 h-8 bg-background text-xs" />
              </TableRow>
            </TableHeader>
            <TableBody>
              {payers.length === 0 && (
                <TableRow><TableCell colSpan={5} className="py-6 text-center text-xs text-muted-foreground">{t(($) => $.empty)}</TableCell></TableRow>
              )}
              {payers.map((payer) => {
                const id = String(payer.id);
                const channel = text(payer, "payment_channel");
                const bank = channel === "bank";
                const draft = draftFor(payer);
                const clientProjects = projectsByClient.get(String(payer.client_id)) ?? [];
                const cap = Number(draft.fee || 0);
                return (
                  <TableRow key={id}>
                    <TableCell className="max-w-40 py-2 align-top text-xs font-medium">
                      <div className="truncate" title={clientName.get(String(payer.client_id)) ?? ""}>{clientName.get(String(payer.client_id)) ?? "—"}</div>
                    </TableCell>
                    <TableCell className="max-w-[28rem] py-2 align-top text-xs">
                      <div className="truncate font-medium" title={text(payer, "name")}>{text(payer, "name")}</div>
                      {text(payer, "inn") && <div className="tabular-nums text-[11px] text-muted-foreground">{text(payer, "inn")}</div>}
                      {bank ? (
                        <div className="mt-1 flex min-w-0 items-center gap-1.5">
                          <NativeSelect className="min-w-0 flex-1" size="sm" aria-label={t(($) => $.billing.contractor)} title={draft.contractor ? contractorDisplay(draft.contractor) : ""} value={draft.contractor} onChange={(event) => setDraft(payer, { contractor: event.target.value })}>
                            <NativeSelectOption value="">{t(($) => $.billing.contractor_empty)}</NativeSelectOption>
                            {contractors.map((row) => <NativeSelectOption key={row.id} value={row.id}>{contractorLabel(row)}</NativeSelectOption>)}
                            {draft.contractor && !contractors.some((row) => row.id === draft.contractor) && (
                              <NativeSelectOption value={draft.contractor}>{contractorDisplay(draft.contractor)}</NativeSelectOption>
                            )}
                          </NativeSelect>
                          {draft.suggested && <span className="whitespace-nowrap rounded bg-amber-500/10 px-1.5 py-0.5 text-[11px] font-medium text-amber-600 dark:text-amber-400">{t(($) => $.billing.match_by_inn)}</span>}
                        </div>
                      ) : (
                        <div className="mt-1 text-[11px] text-muted-foreground">{tt(`values.${channel}`, { defaultValue: channel })}</div>
                      )}
                    </TableCell>
                    <TableCell className="max-w-80 py-2 align-top text-xs">
                      {clientProjects.length === 0 ? (
                        <span className="text-[11px] text-muted-foreground">{t(($) => $.billing.no_projects)}</span>
                      ) : clientProjects.map((project) => (
                        <div key={String(project.id)} className="mb-1 flex min-w-0 items-center gap-1.5 last:mb-0">
                          <span className="min-w-0 flex-1 truncate" title={text(project, "project_title")}>{text(project, "project_title")}</span>
                          <span className="shrink-0 rounded bg-muted px-1.5 py-0.5 text-[11px] text-muted-foreground">{tt(`values.${text(project, "service_type")}`, { defaultValue: text(project, "service_type") })}</span>
                        </div>
                      ))}
                    </TableCell>
                    <TableCell className="py-2 align-top">
                      {bank && <div className="space-y-1.5">
                        <NativeSelect className="w-full" size="sm" aria-label={t(($) => $.billing.mode)} value={draft.mode} onChange={(event) => setDraft(payer, { mode: event.target.value })}>
                          <NativeSelectOption value="postpaid">{t(($) => $.billing.mode_postpaid)}</NativeSelectOption>
                          <NativeSelectOption value="subscription">{t(($) => $.billing.mode_subscription)}</NativeSelectOption>
                        </NativeSelect>
                        {draft.mode === "subscription" && (
                          <div className="flex items-center gap-1.5">
                            <Input aria-label={t(($) => $.billing.fee)} inputMode="decimal" className="h-7 w-28 text-xs" value={draft.fee} onChange={(event) => setDraft(payer, { fee: event.target.value })} />
                            <span className="text-[11px] text-muted-foreground">{t(($) => $.billing.rub_per_period)}</span>
                          </div>
                        )}
                        <p className="max-w-64 text-[11px] leading-4 text-muted-foreground">
                          {draft.mode === "subscription" && cap > 0
                            ? t(($) => $.billing.cap_formula, { amount: rub(cap) })
                            : t(($) => $.billing.postpaid_formula)}
                        </p>
                      </div>}
                    </TableCell>
                    <TableCell className="py-2 text-right align-top">
                      {bank && (
                        <Button
                          type="button"
                          size="sm"
                          variant="outline"
                          className="h-7 text-xs"
                          disabled={saveMut.isPending}
                          onClick={() => saveMut.mutate({
                            payerID: id,
                            contractor: draft.contractor,
                            contractorName: draft.contractor ? contractorDisplay(draft.contractor) : "",
                            mode: draft.mode,
                            fee: draft.fee,
                          })}
                        >
                          {t(($) => $.billing.apply)}
                        </Button>
                      )}
                    </TableCell>
                  </TableRow>
                );
              })}
            </TableBody>
          </Table>
        </div>
      </section>

      <section className="space-y-1.5">
        <h2 className="text-sm font-medium">{t(($) => $.billing.invoiceable_title)}</h2>
        {(groups.data ?? []).length === 0 ? (
          <div className="rounded-lg border border-dashed p-4 text-center text-xs text-muted-foreground">{t(($) => $.billing.invoiceable_empty)}</div>
        ) : (
          <div className="space-y-2">
            {(groups.data ?? []).map((group) => {
              const payer = payerByContractor.get(group.elba_contractor_id);
              const client = payer ? clientName.get(String(payer.client_id)) : undefined;
              const config = (configs.data ?? []).find((row) => row.elba_contractor_id === group.elba_contractor_id);
              const cap = config?.mode === "subscription" ? Number(config.subscription_fee_rub ?? 0) : 0;
              const payable = cap > 0 ? Math.min(group.total_rub, cap) : group.total_rub;
              const exceedsCap = cap > 0 && group.total_rub > cap;
              return (
                <div key={`${group.elba_contractor_id}-${group.starts_on}`} className="rounded-lg border p-3">
                  <div className="flex flex-wrap items-center justify-between gap-2">
                    <div className="text-xs font-medium">
                      {client || contractorDisplay(group.elba_contractor_id)}
                      <span className="ml-2 font-normal tabular-nums text-muted-foreground">{fmtDate(group.starts_on)} — {fmtDate(group.ends_on)}</span>
                    </div>
                    <Button type="button" size="sm" className="h-7 text-xs" disabled={invoiceMut.isPending} onClick={() => invoiceMut.mutate(group)}>
                      {t(($) => $.billing.invoice_for, { amount: payable.toLocaleString("ru-RU") })}
                    </Button>
                  </div>
                  {exceedsCap && (
                    <div className="mt-1.5 rounded bg-amber-500/10 px-2 py-1 text-[11px] text-amber-700 dark:text-amber-400">
                      {t(($) => $.billing.cap_exceeded, { gross: rub(group.total_rub), cap: rub(cap) })}
                    </div>
                  )}
                  <ul className="mt-1.5 space-y-0.5 text-[11px] text-muted-foreground">
                    {group.projects.map((project) => (
                      <li key={project.period_id} className="flex justify-between gap-3">
                        <span className="truncate">{project.project_title}</span>
                        <span className="tabular-nums">{rub(project.total_rub)}</span>
                      </li>
                    ))}
                  </ul>
                </div>
              );
            })}
          </div>
        )}
      </section>

      <BillingDefaults wsConfig={wsConfig} orgId={orgId} />
    </div>
  );
}

function fmtRunDate(value: string): string {
  if (!value) return "—";
  const day = value.slice(0, 10);
  const [y, m, d] = day.split("-");
  if (!y || !m || !d) return day;
  return `${d}.${m}.${y}`;
}

function runStatusLabel(status: string, t: ReturnType<typeof useT<"business">>["t"]): string {
  switch (status) {
    case "ready":
      return t(($) => $.billing.runs_status_ready);
    case "open":
      return t(($) => $.billing.runs_status_upcoming);
    case "closed":
      return t(($) => $.billing.runs_status_closed);
    case "invoiced":
      return t(($) => $.billing.runs_status_invoiced);
    default:
      return status;
  }
}

function BillingRunsQueue({ businessID, onChanged }: {
  businessID: string;
  onChanged: () => Promise<unknown>;
}) {
  const { t } = useT("business");
  const qc = useQueryClient();
  const [expanded, setExpanded] = useState<string | null>(null);

  const runs = useQuery({
    queryKey: businessBillingKeys.runs(businessID),
    queryFn: () => api.listBusinessBillingRuns(businessID, { includeCharges: true }),
  });

  const prepareMut = useMutation({
    mutationFn: () => api.prepareBusinessBillingRuns(businessID),
    onSuccess: (result) => {
      toast.success(t(($) => $.billing.runs_prepare_toast, {
        ready: result.periods_marked_ready,
        projects: result.projects_prepared,
      }));
      qc.invalidateQueries({ queryKey: businessBillingKeys.runs(businessID) });
      void onChanged();
    },
    onError: (cause) => toast.error(cause instanceof Error ? cause.message : String(cause)),
  });

  const confirmMut = useMutation({
    mutationFn: (periodId: string) => api.confirmBusinessBillingPeriod(businessID, periodId),
    onSuccess: (result) => {
      if (result.elba_error) {
        toast.error(result.elba_error);
      } else if (result.elba_skipped === true) {
        toast.success(t(($) => $.billing.runs_confirm_no_elba));
      } else {
        toast.success(t(($) => $.billing.runs_confirm_toast));
      }
      qc.invalidateQueries({ queryKey: businessBillingKeys.runs(businessID) });
      qc.invalidateQueries({ queryKey: businessBillingKeys.invoiceable() });
      void onChanged();
    },
    onError: (cause) => toast.error(cause instanceof Error ? cause.message : String(cause)),
  });

  const items = runs.data?.runs ?? [];
  const ready = items.filter((row) => row.status === "ready");
  const upcoming = items.filter((row) => row.status === "open");
  const recent = items.filter((row) => row.status === "invoiced" || row.status === "closed");

  const renderRow = (row: BusinessBillingRun) => {
    const open = expanded === row.period_id;
    const amount = row.confirmed_total_rub || row.total_rub;
    return (
      <div key={row.period_id} className="rounded-lg border p-3">
        <div className="flex flex-wrap items-start justify-between gap-2">
          <button
            type="button"
            className="min-w-0 flex-1 text-left"
            onClick={() => setExpanded(open ? null : row.period_id)}
          >
            <div className="text-xs font-medium">
              {row.client_name || row.project_title}
              <span className="ml-2 rounded bg-muted px-1.5 py-0.5 text-[10px] font-normal text-muted-foreground">
                {runStatusLabel(row.status, t)}
              </span>
            </div>
            <div className="mt-0.5 text-[11px] text-muted-foreground">
              {row.project_title}
              <span className="mx-1">·</span>
              {fmtRunDate(row.starts_on)} — {fmtRunDate(row.ends_on)}
              <span className="mx-1">·</span>
              <span className="tabular-nums">{rub(amount)}</span>
              <span className="mx-1">·</span>
              {t(($) => $.billing.runs_tasks, { count: row.charge_count })}
            </div>
          </button>
          <div className="flex flex-wrap items-center gap-1.5">
            {(row.status === "ready") && (
              <Button
                type="button"
                size="sm"
                className="h-7 text-xs"
                disabled={confirmMut.isPending}
                onClick={() => confirmMut.mutate(row.period_id)}
              >
                {t(($) => $.billing.runs_confirm)}
              </Button>
            )}
            {row.report_file && (
              <a
                href={row.report_file}
                target="_blank"
                rel="noopener noreferrer"
                className="inline-flex h-7 items-center rounded-lg border border-border bg-background px-2.5 text-xs hover:bg-muted"
              >
                {t(($) => $.billing.runs_pdf)}
              </a>
            )}
            {row.elba_invoice_url && (
              <a
                href={row.elba_invoice_url}
                target="_blank"
                rel="noopener noreferrer"
                className="inline-flex h-7 items-center rounded-lg border border-border bg-background px-2.5 text-xs hover:bg-muted"
              >
                {t(($) => $.billing.runs_bill)}
              </a>
            )}
            {row.elba_act_url && (
              <a
                href={row.elba_act_url}
                target="_blank"
                rel="noopener noreferrer"
                className="inline-flex h-7 items-center rounded-lg border border-border bg-background px-2.5 text-xs hover:bg-muted"
              >
                {t(($) => $.billing.runs_act)}
              </a>
            )}
          </div>
        </div>
        {open && (row.charges?.length ?? 0) > 0 && (
          <ul className="mt-2 space-y-0.5 border-t pt-2 text-[11px] text-muted-foreground">
            {row.charges!.map((charge) => (
              <li key={charge.id} className="flex justify-between gap-3">
                <span className="truncate">{charge.issue_title}</span>
                <span className="tabular-nums">{rub(charge.price_rub)}</span>
              </li>
            ))}
          </ul>
        )}
      </div>
    );
  };

  return (
    <section className="space-y-1.5">
      <div className="flex flex-wrap items-center justify-between gap-2">
        <div>
          <h2 className="text-sm font-medium">{t(($) => $.billing.runs_title)}</h2>
          <p className="text-[11px] text-muted-foreground">{t(($) => $.billing.runs_hint)}</p>
        </div>
        <Button
          type="button"
          size="sm"
          variant="outline"
          className="h-7 text-xs"
          disabled={prepareMut.isPending}
          onClick={() => prepareMut.mutate()}
        >
          {t(($) => $.billing.runs_prepare)}
        </Button>
      </div>
      {runs.isLoading ? (
        <div className="rounded-lg border border-dashed p-4 text-center text-xs text-muted-foreground">{t(($) => $.loading)}</div>
      ) : items.length === 0 ? (
        <div className="rounded-lg border border-dashed p-4 text-center text-xs text-muted-foreground">{t(($) => $.billing.runs_empty)}</div>
      ) : (
        <div className="space-y-3">
          {ready.length > 0 && (
            <div className="space-y-2">
              <div className="text-[11px] font-medium text-muted-foreground">{t(($) => $.billing.runs_ready_group)}</div>
              {ready.map(renderRow)}
            </div>
          )}
          {upcoming.length > 0 && (
            <div className="space-y-2">
              <div className="text-[11px] font-medium text-muted-foreground">{t(($) => $.billing.runs_upcoming_group)}</div>
              {upcoming.map(renderRow)}
            </div>
          )}
          {recent.length > 0 && (
            <div className="space-y-2">
              <div className="text-[11px] font-medium text-muted-foreground">{t(($) => $.billing.runs_recent_group)}</div>
              {recent.map(renderRow)}
            </div>
          )}
        </div>
      )}
    </section>
  );
}

// Workspace-level pricing defaults + Elba organization wiring, formerly the
// top half of Settings -> Billing.
function BillingDefaults({ wsConfig, orgId }: {
  wsConfig: ReturnType<typeof useElbaDirectory>["wsConfig"];
  orgId: string;
}) {
  const { t } = useT("business");
  const qc = useQueryClient();

  const [markup, setMarkup] = useState("");
  const [minPrice, setMinPrice] = useState("");
  const [rounding, setRounding] = useState("");
  const [fxMarkup, setFxMarkup] = useState("");
  const [org, setOrg] = useState("");
  const [bankAccount, setBankAccount] = useState("");

  useEffect(() => {
    if (!wsConfig) return;
    setMarkup(String(wsConfig.markup));
    setMinPrice(String(wsConfig.min_price_rub));
    setRounding(String(wsConfig.rounding_rub));
    setFxMarkup(String(wsConfig.fx_markup_percent));
    setOrg(wsConfig.elba_org_id ?? "");
    setBankAccount(wsConfig.elba_bank_account_id ?? "");
  }, [wsConfig]);

  const orgs = useQuery({ queryKey: businessBillingKeys.orgs(), queryFn: () => api.getElbaOrganizations(), staleTime: 5 * 60_000, retry: false });
  const accounts = useQuery({
    queryKey: businessBillingKeys.accounts(org || orgId),
    queryFn: () => api.getElbaBankAccounts(org || orgId),
    enabled: !!(org || orgId),
    staleTime: 5 * 60_000,
    retry: false,
  });

  const saveMut = useMutation({
    mutationFn: () => {
      const num = (value: string, fallback: number): number => {
        const parsed = Number(value);
        return value.trim() !== "" && Number.isFinite(parsed) ? parsed : fallback;
      };
      return api.putWorkspaceBillingConfig({
        markup: num(markup, 3),
        min_price_rub: num(minPrice, 500),
        rounding_rub: num(rounding, 50),
        fx_markup_percent: num(fxMarkup, 5),
        elba_org_id: org,
        elba_bank_account_id: bankAccount,
      });
    },
    onSuccess: () => {
      toast.success(t(($) => $.success));
      qc.invalidateQueries({ queryKey: businessBillingKeys.wsConfig() });
    },
    onError: (cause) => toast.error(cause instanceof Error ? cause.message : String(cause)),
  });

  const field = (label: string, value: string, onChange: (next: string) => void) => (
    <label className="flex flex-col gap-1 text-xs">
      <span className="text-muted-foreground">{label}</span>
      <Input inputMode="decimal" value={value} onChange={(event) => onChange(event.target.value)} className="h-7 text-xs" />
    </label>
  );

  return (
    <section className="space-y-1.5">
      <h2 className="text-sm font-medium">{t(($) => $.billing.defaults_title)}</h2>
      <p className="text-[11px] text-muted-foreground">{t(($) => $.billing.defaults_hint)}</p>
      <div className="rounded-lg border p-3">
        <div className="grid grid-cols-2 gap-2 @3xl:grid-cols-4">
          {field(t(($) => $.billing.markup), markup, setMarkup)}
          {field(t(($) => $.billing.fx_markup), fxMarkup, setFxMarkup)}
          {field(t(($) => $.billing.min_price), minPrice, setMinPrice)}
          {field(t(($) => $.billing.rounding), rounding, setRounding)}
          <label className="flex flex-col gap-1 text-xs">
            <span className="text-muted-foreground">{t(($) => $.billing.org)}</span>
            <NativeSelect size="sm" aria-label={t(($) => $.billing.org)} value={org} onChange={(event) => { setOrg(event.target.value); setBankAccount(""); }}>
              <NativeSelectOption value="">{t(($) => $.billing.org_empty)}</NativeSelectOption>
              {((orgs.data ?? []) as ElbaRow[]).map((row) => <NativeSelectOption key={row.id} value={row.id}>{str(row.name) || (str(row.inn) ? `ИНН ${str(row.inn)}` : row.id)}</NativeSelectOption>)}
            </NativeSelect>
          </label>
          <label className="flex flex-col gap-1 text-xs">
            <span className="text-muted-foreground">{t(($) => $.billing.bank_account)}</span>
            <NativeSelect size="sm" aria-label={t(($) => $.billing.bank_account)} value={bankAccount} onChange={(event) => setBankAccount(event.target.value)} disabled={!(org || orgId)}>
              <NativeSelectOption value="">{t(($) => $.billing.account_empty)}</NativeSelectOption>
              {((accounts.data ?? []) as ElbaRow[]).map((row) => {
                const acc = str(row.accountNumber);
                const bankName = (row.bank as { name?: string } | undefined)?.name;
                return <NativeSelectOption key={row.id} value={row.id}>{acc ? (bankName ? `${acc} · ${bankName}` : acc) : str(row.name) || row.id}</NativeSelectOption>;
              })}
            </NativeSelect>
          </label>
        </div>
        <Button type="button" size="sm" className="mt-3 h-7 text-xs" disabled={saveMut.isPending} onClick={() => saveMut.mutate()}>
          {t(($) => $.actions.save)}
        </Button>
      </div>
    </section>
  );
}
