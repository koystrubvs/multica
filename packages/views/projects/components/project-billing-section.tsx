"use client";

// Agency billing config section on the project detail page (fork feature).
//
// Pricing knobs inherit from the workspace defaults (Settings -> Billing):
// an empty input means "inherit" and shows the effective value as its
// placeholder. Elba wiring: the workspace picks the organization; here the
// project links a contractor (invoice target) and may override the bank
// account. Closing a period auto-creates the счёт + акт when the contractor
// is linked; a failed push can be retried per period from the history list.

import { useEffect, useState } from "react";
import { useQuery, useMutation, useQueryClient } from "@tanstack/react-query";
import { ChevronRight } from "lucide-react";
import { toast } from "sonner";
import { Button } from "@multica/ui/components/ui/button";
import { Input } from "@multica/ui/components/ui/input";
import { Switch } from "@multica/ui/components/ui/switch";
import { api } from "@multica/core/api";
import type {
  ClientBillingConfig,
  ClientBillingConfigUpdate,
  ClientBillingMode,
  ClientBillingPeriod,
} from "@multica/core/types";
import { useT } from "../../i18n";

export const clientBillingKeys = {
  config: (projectId: string) => ["client-billing", "config", projectId] as const,
  charges: (projectId: string) => ["client-billing", "charges", projectId] as const,
  currentPeriod: (projectId: string) => ["client-billing", "current-period", projectId] as const,
  periods: (projectId: string) => ["client-billing", "periods", projectId] as const,
  wsConfig: () => ["client-billing", "workspace-config"] as const,
  disputes: (projectId: string) => ["client-billing", "disputes", projectId] as const,
  elbaContractors: (orgId: string) => ["client-billing", "elba-contractors", orgId] as const,
  elbaBankAccounts: (orgId: string) => ["client-billing", "elba-bank-accounts", orgId] as const,
};

function formatRub(value: number): string {
  return `${value.toLocaleString("ru-RU", { maximumFractionDigits: 2 })} ₽`;
}

function fmtDate(iso: string): string {
  return iso.slice(0, 10);
}

// Elba contractors carry `name` (+ `inn` for disambiguation — several share a
// name with different KPP); bank accounts label by `accountNumber` + bank.
// Never fall back to a bare UUID unless nothing else is present.
function str(v: unknown): string {
  return typeof v === "string" ? v : "";
}
function elbaContractorLabel(c: { id: string; [k: string]: unknown }): string {
  const name = str(c.name);
  const inn = str(c.inn);
  if (name) return inn ? `${name} (ИНН ${inn})` : name;
  return inn ? `ИНН ${inn}` : c.id;
}
function elbaBankAccountLabel(a: { id: string; [k: string]: unknown }): string {
  const acc = str(a.accountNumber);
  if (acc) {
    const bank = a.bank as { name?: string } | undefined;
    return bank?.name ? `${acc} · ${bank.name}` : acc;
  }
  return str(a.name) || a.id;
}

export function ProjectBillingSection({ projectId }: { projectId: string }) {
  const { t } = useT("projects");
  const [open, setOpen] = useState(false);
  const qc = useQueryClient();

  const { data: config, isLoading } = useQuery({
    queryKey: clientBillingKeys.config(projectId),
    queryFn: () => api.getProjectBillingConfig(projectId),
  });

  const saveMut = useMutation({
    mutationFn: (update: ClientBillingConfigUpdate) =>
      api.putProjectBillingConfig(projectId, update),
    onSuccess: () => {
      toast.success(t(($) => $.billing.saved_toast));
      qc.invalidateQueries({ queryKey: clientBillingKeys.config(projectId) });
      qc.invalidateQueries({ queryKey: clientBillingKeys.currentPeriod(projectId) });
    },
    onError: () => toast.error(t(($) => $.billing.save_failed_toast)),
  });

  return (
    <div>
      <button
        type="button"
        onClick={() => setOpen((v) => !v)}
        className={`flex w-full items-center gap-1 rounded-md px-2 py-1 text-xs font-medium transition-colors mb-2 hover:bg-accent/70 ${
          open ? "" : "text-muted-foreground hover:text-foreground"
        }`}
      >
        {t(($) => $.billing.section_title)}
        <ChevronRight
          className={`!size-3 shrink-0 stroke-[2.5] text-muted-foreground transition-transform ${open ? "rotate-90" : ""}`}
        />
      </button>
      {open && (
        <div className="pl-2 pr-2 pb-2">
          {isLoading ? null : !config ? (
            <div className="flex flex-col items-start gap-2">
              <p className="text-xs text-muted-foreground">
                {t(($) => $.billing.not_configured)}
              </p>
              <Button
                size="sm"
                className="h-7 px-3 text-xs"
                disabled={saveMut.isPending}
                onClick={() => saveMut.mutate({})}
              >
                {t(($) => $.billing.enable)}
              </Button>
            </div>
          ) : (
            <div className="flex flex-col gap-4">
              {config.enabled && <CurrentPeriodCard projectId={projectId} />}
              {config.enabled && <DisputesBlock projectId={projectId} />}
              <ConfigForm
                config={config}
                saving={saveMut.isPending}
                onSave={(u) => saveMut.mutate(u)}
              />
              {config.enabled && <PeriodHistory projectId={projectId} />}
            </div>
          )}
        </div>
      )}
    </div>
  );
}

function PeriodStatusBadge({ status }: { status: ClientBillingPeriod["status"] }) {
  const { t } = useT("projects");
  const styles: Record<ClientBillingPeriod["status"], string> = {
    open: "bg-sky-500/15 text-sky-600 dark:text-sky-400",
    ready: "bg-teal-500/15 text-teal-600 dark:text-teal-400",
    closed: "bg-amber-500/15 text-amber-600 dark:text-amber-400",
    invoiced: "bg-violet-500/15 text-violet-600 dark:text-violet-400",
    paid: "bg-emerald-500/15 text-emerald-600 dark:text-emerald-400",
  };
  const labels: Record<ClientBillingPeriod["status"], string> = {
    open: t(($) => $.billing.period_status_open),
    ready: t(($) => $.billing.period_status_ready),
    closed: t(($) => $.billing.period_status_closed),
    invoiced: t(($) => $.billing.period_status_invoiced),
    paid: t(($) => $.billing.period_status_paid),
  };
  return (
    <span className={`inline-flex shrink-0 items-center rounded px-1.5 py-0.5 text-[11px] font-medium whitespace-nowrap ${styles[status]}`}>
      {labels[status]}
    </span>
  );
}

// Current cycle: dates, confirmed total, progress against the budget /
// fair-use cap (when the mode has one) and the close action.
function CurrentPeriodCard({ projectId }: { projectId: string }) {
  const { t } = useT("projects");
  const qc = useQueryClient();
  const { data: current } = useQuery({
    queryKey: clientBillingKeys.currentPeriod(projectId),
    queryFn: () => api.getProjectBillingCurrentPeriod(projectId),
  });

  const invalidatePeriods = () => {
    qc.invalidateQueries({ queryKey: clientBillingKeys.currentPeriod(projectId) });
    qc.invalidateQueries({ queryKey: clientBillingKeys.periods(projectId) });
  };

  const closeMut = useMutation({
    mutationFn: (periodId: string) => api.closeBillingPeriod(projectId, periodId),
    onSuccess: (res) => {
      if (res.elba_error) {
        toast.error(t(($) => $.billing.elba_push_failed_toast, { error: res.elba_error }));
      } else if (res.period.status === "invoiced") {
        toast.success(t(($) => $.billing.period_invoiced_toast));
      } else {
        toast.success(t(($) => $.billing.period_closed_toast));
      }
      invalidatePeriods();
    },
    onError: () => toast.error(t(($) => $.billing.period_action_failed_toast)),
  });

  const sweepMut = useMutation({
    mutationFn: () => api.sweepProjectBilling(projectId),
    onSuccess: (res) => {
      toast.success(t(($) => $.billing.sweep_done_toast, { count: res.created }));
      qc.invalidateQueries({ queryKey: clientBillingKeys.charges(projectId) });
      invalidatePeriods();
    },
    onError: () => toast.error(t(($) => $.billing.period_action_failed_toast)),
  });

  if (!current) return null;
  const { period, confirmed_total, draft_count, limit_rub, percent } = current;
  const overBudget = limit_rub > 0 && percent >= 100;

  return (
    <div className="rounded-md border border-border/60 p-2.5 flex flex-col gap-1.5">
      <div className="flex items-center justify-between gap-2">
        <span className="text-xs text-muted-foreground">
          {t(($) => $.billing.current_period, {
            from: fmtDate(period.starts_on),
            to: fmtDate(period.ends_on),
          })}
        </span>
        <PeriodStatusBadge status={period.status} />
      </div>
      <div className="flex items-baseline gap-2">
        <span className="text-sm font-semibold">{formatRub(confirmed_total)}</span>
        {draft_count > 0 && (
          <span className="text-[11px] text-muted-foreground">
            {t(($) => $.billing.draft_count, { count: draft_count })}
          </span>
        )}
      </div>
      {limit_rub > 0 && (
        <div className="flex flex-col gap-1">
          <div className="h-1.5 w-full rounded-full bg-muted overflow-hidden">
            <div
              className={`h-full rounded-full transition-all ${overBudget ? "bg-destructive" : "bg-primary"}`}
              style={{ width: `${Math.min(percent, 100)}%` }}
            />
          </div>
          <span className={`text-[11px] ${overBudget ? "text-destructive" : "text-muted-foreground"}`}>
            {t(($) => $.billing.budget_progress, {
              percent,
              limit: formatRub(limit_rub),
            })}
          </span>
        </div>
      )}
      <div className="flex flex-wrap gap-1.5">
        <Button
          size="sm"
          variant="outline"
          className="h-6 px-2 text-xs"
          disabled={sweepMut.isPending}
          onClick={() => sweepMut.mutate()}
        >
          {t(($) => $.billing.sweep)}
        </Button>
        <Button
          size="sm"
          variant="outline"
          className="h-6 px-2 text-xs"
          disabled={closeMut.isPending}
          onClick={() => closeMut.mutate(period.id)}
        >
          {t(($) => $.billing.close_period)}
        </Button>
      </div>
    </div>
  );
}

// Open client disputes: resolving them is a precondition for closing the
// period (the backend refuses otherwise). keep = price stands; exclude =
// drafts void + the unbilled delta is burned; adjust = custom price with a
// mandatory comment.
function DisputesBlock({ projectId }: { projectId: string }) {
  const { t } = useT("projects");
  const qc = useQueryClient();
  const { data: disputes = [] } = useQuery({
    queryKey: clientBillingKeys.disputes(projectId),
    queryFn: () => api.listProjectBillingDisputes(projectId, "open"),
  });

  const resolveMut = useMutation({
    mutationFn: ({
      disputeId,
      resolution,
      comment,
      priceRub,
    }: {
      disputeId: string;
      resolution: "keep" | "exclude" | "adjust";
      comment?: string;
      priceRub?: number;
    }) => api.resolveBillingDispute(projectId, disputeId, resolution, comment, priceRub),
    onSuccess: () => {
      toast.success(t(($) => $.billing.dispute_resolved_toast));
      qc.invalidateQueries({ queryKey: clientBillingKeys.disputes(projectId) });
      qc.invalidateQueries({ queryKey: clientBillingKeys.charges(projectId) });
      qc.invalidateQueries({ queryKey: clientBillingKeys.currentPeriod(projectId) });
    },
    onError: () => toast.error(t(($) => $.billing.period_action_failed_toast)),
  });

  const handleAdjust = (disputeId: string) => {
    const priceRaw = window.prompt(t(($) => $.billing.dispute_adjust_price_prompt));
    if (priceRaw === null) return;
    const price = Number(priceRaw);
    if (!Number.isFinite(price) || price < 0) return;
    const comment = window.prompt(t(($) => $.billing.dispute_adjust_comment_prompt));
    if (!comment) return;
    resolveMut.mutate({ disputeId, resolution: "adjust", comment, priceRub: price });
  };

  if (disputes.length === 0) return null;

  return (
    <div className="flex flex-col gap-1.5">
      <span className="text-[11px] font-medium uppercase tracking-wide text-red-600 dark:text-red-400">
        {t(($) => $.billing.disputes_title, { count: disputes.length })}
      </span>
      {disputes.map((d) => (
        <div key={d.id} className="flex flex-col gap-1 rounded-md border border-red-500/30 bg-red-500/5 px-2 py-1.5">
          <span className="text-xs font-medium">{d.issue_title}</span>
          <span className="text-[11px] text-muted-foreground">{d.reason}</span>
          <div className="flex flex-wrap gap-1">
            <Button
              size="sm"
              variant="outline"
              className="h-5 px-1.5 text-[11px]"
              disabled={resolveMut.isPending}
              onClick={() => resolveMut.mutate({ disputeId: d.id, resolution: "keep" })}
            >
              {t(($) => $.billing.dispute_keep)}
            </Button>
            <Button
              size="sm"
              variant="outline"
              className="h-5 px-1.5 text-[11px]"
              disabled={resolveMut.isPending}
              onClick={() => resolveMut.mutate({ disputeId: d.id, resolution: "exclude" })}
            >
              {t(($) => $.billing.dispute_exclude)}
            </Button>
            <Button
              size="sm"
              variant="ghost"
              className="h-5 px-1.5 text-[11px] text-muted-foreground"
              disabled={resolveMut.isPending}
              onClick={() => handleAdjust(d.id)}
            >
              {t(($) => $.billing.dispute_adjust)}
            </Button>
          </div>
        </div>
      ))}
    </div>
  );
}

// Past cycles with their frozen totals and the invoice / payment lifecycle.
// Each period renders as a two-line card: dates + status on top, total +
// actions below — the sidebar column is too narrow for a single row.
function PeriodHistory({ projectId }: { projectId: string }) {
  const { t } = useT("projects");
  const qc = useQueryClient();
  const { data: periods = [] } = useQuery({
    queryKey: clientBillingKeys.periods(projectId),
    queryFn: () => api.listProjectBillingPeriods(projectId),
  });

  const invalidatePeriods = () => {
    qc.invalidateQueries({ queryKey: clientBillingKeys.currentPeriod(projectId) });
    qc.invalidateQueries({ queryKey: clientBillingKeys.periods(projectId) });
  };

  const paidMut = useMutation({
    mutationFn: (periodId: string) => api.markBillingPeriodPaid(projectId, periodId),
    onSuccess: () => {
      toast.success(t(($) => $.billing.period_paid_toast));
      invalidatePeriods();
    },
    onError: () => toast.error(t(($) => $.billing.period_action_failed_toast)),
  });

  const reopenMut = useMutation({
    mutationFn: (periodId: string) => api.reopenBillingPeriod(projectId, periodId),
    onSuccess: invalidatePeriods,
    onError: () => toast.error(t(($) => $.billing.period_action_failed_toast)),
  });

  const invoiceMut = useMutation({
    mutationFn: (periodId: string) => api.invoiceBillingPeriod(projectId, periodId),
    onSuccess: () => {
      toast.success(t(($) => $.billing.period_invoiced_toast));
      invalidatePeriods();
    },
    onError: (e) =>
      toast.error(
        t(($) => $.billing.elba_push_failed_toast, {
          error: e instanceof Error ? e.message : "",
        }),
      ),
  });

  const past = periods.filter((p) => p.status !== "open");
  if (past.length === 0) return null;

  return (
    <div className="flex flex-col gap-1.5">
      <span className="text-[11px] font-medium uppercase tracking-wide text-muted-foreground">
        {t(($) => $.billing.period_history)}
      </span>
      {past.map((p) => (
        <div key={p.id} className="flex flex-col gap-1 rounded-md border border-border/40 px-2 py-1.5">
          <div className="flex items-center justify-between gap-2">
            <span className="text-xs text-muted-foreground whitespace-nowrap">
              {fmtDate(p.starts_on)} — {fmtDate(p.ends_on)}
            </span>
            <PeriodStatusBadge status={p.status} />
          </div>
          <div className="flex flex-wrap items-center justify-between gap-x-2 gap-y-1">
            <span className="text-xs font-semibold whitespace-nowrap">{formatRub(p.total_rub)}</span>
            <div className="flex flex-wrap items-center gap-1">
              {p.status === "closed" && (
                <Button
                  size="sm"
                  variant="outline"
                  className="h-5 px-1.5 text-[11px]"
                  disabled={invoiceMut.isPending}
                  onClick={() => invoiceMut.mutate(p.id)}
                >
                  {t(($) => $.billing.create_invoice)}
                </Button>
              )}
              {(p.status === "closed" || p.status === "invoiced") && (
                <>
                  <Button
                    size="sm"
                    variant="outline"
                    className="h-5 px-1.5 text-[11px]"
                    disabled={paidMut.isPending}
                    onClick={() => paidMut.mutate(p.id)}
                  >
                    {t(($) => $.billing.mark_paid)}
                  </Button>
                  <Button
                    size="sm"
                    variant="ghost"
                    className="h-5 px-1.5 text-[11px] text-muted-foreground"
                    disabled={reopenMut.isPending}
                    onClick={() => reopenMut.mutate(p.id)}
                  >
                    {t(($) => $.billing.reopen_period)}
                  </Button>
                </>
              )}
            </div>
          </div>
          {p.elba_invoice_id && (
            <span className="text-[11px] text-muted-foreground">
              {t(($) => $.billing.elba_invoice_ref, { id: p.elba_invoice_id })}
            </span>
          )}
        </div>
      ))}
    </div>
  );
}

function NumberField({
  label,
  value,
  onChange,
  placeholder,
  step = "any",
}: {
  label: string;
  value: string;
  onChange: (v: string) => void;
  placeholder?: string;
  step?: string;
}) {
  return (
    <label className="flex flex-col gap-1 text-xs">
      <span className="text-muted-foreground">{label}</span>
      <Input
        type="number"
        step={step}
        value={value}
        placeholder={placeholder}
        onChange={(e) => onChange(e.target.value)}
        className="h-7 text-xs"
      />
    </label>
  );
}

function ConfigForm({
  config,
  saving,
  onSave,
}: {
  config: ClientBillingConfig;
  saving: boolean;
  onSave: (u: ClientBillingConfigUpdate) => void;
}) {
  const { t } = useT("projects");
  const [enabled, setEnabled] = useState(config.enabled);
  const [mode, setMode] = useState<ClientBillingMode>(config.mode);
  // Pricing overrides: "" = inherit (placeholder shows the effective value).
  const [markup, setMarkup] = useState(config.markup === null ? "" : String(config.markup));
  const [minPrice, setMinPrice] = useState(config.min_price_rub === null ? "" : String(config.min_price_rub));
  const [rounding, setRounding] = useState(config.rounding_rub === null ? "" : String(config.rounding_rub));
  const [fxMarkup, setFxMarkup] = useState(config.fx_markup_percent === null ? "" : String(config.fx_markup_percent));
  const [budget, setBudget] = useState(String(config.budget_rub || ""));
  const [subscriptionFee, setSubscriptionFee] = useState(String(config.subscription_fee_rub || ""));
  const [fairUse, setFairUse] = useState(String(config.fair_use_rub || ""));
  const [contractor, setContractor] = useState(config.elba_contractor_id ?? "");
  const [bankAccount, setBankAccount] = useState(config.elba_bank_account_id ?? "");

  // Re-sync local state when a save round-trips new server values.
  useEffect(() => {
    setEnabled(config.enabled);
    setMode(config.mode);
    setMarkup(config.markup === null ? "" : String(config.markup));
    setMinPrice(config.min_price_rub === null ? "" : String(config.min_price_rub));
    setRounding(config.rounding_rub === null ? "" : String(config.rounding_rub));
    setFxMarkup(config.fx_markup_percent === null ? "" : String(config.fx_markup_percent));
    setBudget(String(config.budget_rub || ""));
    setSubscriptionFee(String(config.subscription_fee_rub || ""));
    setFairUse(String(config.fair_use_rub || ""));
    setContractor(config.elba_contractor_id ?? "");
    setBankAccount(config.elba_bank_account_id ?? "");
  }, [config]);

  // "" -> null (inherit); invalid -> null too (backend keeps inherit).
  const overrideOrNull = (s: string): number | null => {
    if (s.trim() === "") return null;
    const n = Number(s);
    return Number.isFinite(n) ? n : null;
  };
  const num = (s: string): number => {
    const n = Number(s);
    return Number.isFinite(n) ? n : 0;
  };

  const handleSave = () => {
    onSave({
      enabled,
      mode,
      markup: overrideOrNull(markup),
      min_price_rub: overrideOrNull(minPrice),
      rounding_rub: overrideOrNull(rounding),
      fx_markup_percent: overrideOrNull(fxMarkup),
      budget_rub: num(budget),
      subscription_fee_rub: num(subscriptionFee),
      fair_use_rub: num(fairUse),
      elba_contractor_id: contractor || null,
      elba_bank_account_id: bankAccount || null,
    });
  };

  return (
    <div className="flex flex-col gap-3">
      <div className="flex items-center gap-2 text-xs">
        <Switch checked={enabled} onCheckedChange={setEnabled} />
        <span className={enabled ? "" : "text-muted-foreground"}>
          {t(($) => $.billing.enabled_label)}
        </span>
      </div>

      <label className="flex flex-col gap-1 text-xs">
        <span className="text-muted-foreground">{t(($) => $.billing.mode)}</span>
        <select
          value={mode}
          onChange={(e) => setMode(e.target.value as ClientBillingMode)}
          className="h-7 rounded-md border border-input bg-transparent px-2 text-xs shadow-xs outline-none focus-visible:ring-2 focus-visible:ring-ring/50"
        >
          <option value="postpaid">{t(($) => $.billing.mode_postpaid)}</option>
          <option value="budget">{t(($) => $.billing.mode_budget)}</option>
          <option value="subscription">{t(($) => $.billing.mode_subscription)}</option>
        </select>
      </label>

      <p className="text-[11px] text-muted-foreground">
        {t(($) => $.billing.inherit_hint)}
      </p>
      <div className="grid grid-cols-2 gap-2">
        <NumberField
          label={t(($) => $.billing.markup)}
          value={markup}
          onChange={setMarkup}
          placeholder={String(config.effective.markup)}
          step="0.1"
        />
        <NumberField
          label={t(($) => $.billing.fx_markup)}
          value={fxMarkup}
          onChange={setFxMarkup}
          placeholder={String(config.effective.fx_markup_percent)}
          step="0.5"
        />
        <NumberField
          label={t(($) => $.billing.min_price)}
          value={minPrice}
          onChange={setMinPrice}
          placeholder={String(config.effective.min_price_rub)}
          step="50"
        />
        <NumberField
          label={t(($) => $.billing.rounding)}
          value={rounding}
          onChange={setRounding}
          placeholder={String(config.effective.rounding_rub)}
          step="10"
        />
      </div>

      {mode === "budget" && (
        <NumberField label={t(($) => $.billing.budget)} value={budget} onChange={setBudget} step="1000" />
      )}
      {mode === "subscription" && (
        <div className="grid grid-cols-2 gap-2">
          <NumberField
            label={t(($) => $.billing.subscription_fee)}
            value={subscriptionFee}
            onChange={setSubscriptionFee}
            step="1000"
          />
          <NumberField label={t(($) => $.billing.fair_use)} value={fairUse} onChange={setFairUse} step="1000" />
        </div>
      )}

      <ElbaProjectFields
        contractor={contractor}
        bankAccount={bankAccount}
        onContractor={setContractor}
        onBankAccount={setBankAccount}
      />

      <div>
        <Button size="sm" className="h-7 px-3 text-xs" disabled={saving} onClick={handleSave}>
          {t(($) => $.billing.save)}
        </Button>
      </div>
    </div>
  );
}

// Contractor (invoice target) + optional bank-account override. Requires the
// workspace to have its Elba organization picked in Settings -> Billing.
function ElbaProjectFields({
  contractor,
  bankAccount,
  onContractor,
  onBankAccount,
}: {
  contractor: string;
  bankAccount: string;
  onContractor: (v: string) => void;
  onBankAccount: (v: string) => void;
}) {
  const { t } = useT("projects");
  const { data: wsConfig } = useQuery({
    queryKey: clientBillingKeys.wsConfig(),
    queryFn: () => api.getWorkspaceBillingConfig(),
  });
  const orgId = wsConfig?.elba_org_id ?? "";

  const { data: contractors = [] } = useQuery({
    queryKey: clientBillingKeys.elbaContractors(orgId),
    queryFn: () => api.getElbaContractors(orgId),
    enabled: !!orgId,
    staleTime: 5 * 60_000,
    retry: false,
  });
  const { data: accounts = [] } = useQuery({
    queryKey: clientBillingKeys.elbaBankAccounts(orgId),
    queryFn: () => api.getElbaBankAccounts(orgId),
    enabled: !!orgId,
    staleTime: 5 * 60_000,
    retry: false,
  });

  if (!orgId) {
    return (
      <p className="text-[11px] text-muted-foreground">
        {t(($) => $.billing.elba_org_missing_hint)}
      </p>
    );
  }

  const selectClass =
    "h-7 rounded-md border border-input bg-transparent px-2 text-xs shadow-xs outline-none focus-visible:ring-2 focus-visible:ring-ring/50";

  return (
    <div className="flex flex-col gap-2">
      <label className="flex flex-col gap-1 text-xs">
        <span className="text-muted-foreground">{t(($) => $.billing.elba_contractor)}</span>
        <select value={contractor} onChange={(e) => onContractor(e.target.value)} className={selectClass}>
          <option value="">{t(($) => $.billing.elba_contractor_none)}</option>
          {contractors.map((c) => (
            <option key={c.id} value={c.id}>
              {elbaContractorLabel(c)}
            </option>
          ))}
        </select>
        <span className="text-[10px] text-muted-foreground">
          {t(($) => $.billing.elba_contractor_hint)}
        </span>
      </label>
      <label className="flex flex-col gap-1 text-xs">
        <span className="text-muted-foreground">{t(($) => $.billing.elba_bank_account)}</span>
        <select value={bankAccount} onChange={(e) => onBankAccount(e.target.value)} className={selectClass}>
          <option value="">{t(($) => $.billing.elba_bank_account_default)}</option>
          {accounts.map((a) => (
            <option key={a.id} value={a.id}>
              {elbaBankAccountLabel(a)}
            </option>
          ))}
        </select>
      </label>
    </div>
  );
}
