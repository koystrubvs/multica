"use client";

// Agency billing config section on the project detail page (fork feature).
//
// Lets staff enable client billing for a project and tune the pricing knobs
// (markup, floor, rounding, fx markup, mode-specific budgets). When enabled,
// every issue of this project that reaches `done` with agent usage gets a
// draft price snapshot — see issue-billing-section.tsx for the issue surface.

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
};

function formatRub(value: number): string {
  return `${value.toLocaleString("ru-RU", { maximumFractionDigits: 2 })} ₽`;
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

function fmtDate(iso: string): string {
  return iso.slice(0, 10);
}

function PeriodStatusBadge({ status }: { status: ClientBillingPeriod["status"] }) {
  const { t } = useT("projects");
  const styles: Record<ClientBillingPeriod["status"], string> = {
    open: "bg-sky-500/15 text-sky-600 dark:text-sky-400",
    closed: "bg-amber-500/15 text-amber-600 dark:text-amber-400",
    invoiced: "bg-violet-500/15 text-violet-600 dark:text-violet-400",
    paid: "bg-emerald-500/15 text-emerald-600 dark:text-emerald-400",
  };
  const labels: Record<ClientBillingPeriod["status"], string> = {
    open: t(($) => $.billing.period_status_open),
    closed: t(($) => $.billing.period_status_closed),
    invoiced: t(($) => $.billing.period_status_invoiced),
    paid: t(($) => $.billing.period_status_paid),
  };
  return (
    <span className={`inline-flex items-center rounded px-1.5 py-0.5 text-[11px] font-medium ${styles[status]}`}>
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
    onSuccess: () => {
      toast.success(t(($) => $.billing.period_closed_toast));
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
      <div>
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

// Past cycles with their frozen totals and the payment lifecycle actions.
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

  const past = periods.filter((p) => p.status !== "open");
  if (past.length === 0) return null;

  return (
    <div className="flex flex-col gap-1">
      <span className="text-[11px] font-medium uppercase tracking-wide text-muted-foreground">
        {t(($) => $.billing.period_history)}
      </span>
      {past.map((p) => (
        <div key={p.id} className="flex items-center justify-between gap-2 rounded-md border border-border/40 px-2 py-1.5">
          <div className="flex items-center gap-2 min-w-0">
            <span className="text-xs text-muted-foreground whitespace-nowrap">
              {fmtDate(p.starts_on)} — {fmtDate(p.ends_on)}
            </span>
            <PeriodStatusBadge status={p.status} />
          </div>
          <div className="flex items-center gap-2">
            <span className="text-xs font-medium whitespace-nowrap">{formatRub(p.total_rub)}</span>
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
      ))}
    </div>
  );
}

function NumberField({
  label,
  value,
  onChange,
  step = "any",
}: {
  label: string;
  value: string;
  onChange: (v: string) => void;
  step?: string;
}) {
  return (
    <label className="flex flex-col gap-1 text-xs">
      <span className="text-muted-foreground">{label}</span>
      <Input
        type="number"
        step={step}
        value={value}
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
  const [markup, setMarkup] = useState(String(config.markup));
  const [minPrice, setMinPrice] = useState(String(config.min_price_rub));
  const [rounding, setRounding] = useState(String(config.rounding_rub));
  const [fxMarkup, setFxMarkup] = useState(String(config.fx_markup_percent));
  const [budget, setBudget] = useState(String(config.budget_rub || ""));
  const [subscriptionFee, setSubscriptionFee] = useState(String(config.subscription_fee_rub || ""));
  const [fairUse, setFairUse] = useState(String(config.fair_use_rub || ""));

  // Re-sync local state when a save round-trips new server values.
  useEffect(() => {
    setEnabled(config.enabled);
    setMode(config.mode);
    setMarkup(String(config.markup));
    setMinPrice(String(config.min_price_rub));
    setRounding(String(config.rounding_rub));
    setFxMarkup(String(config.fx_markup_percent));
    setBudget(String(config.budget_rub || ""));
    setSubscriptionFee(String(config.subscription_fee_rub || ""));
    setFairUse(String(config.fair_use_rub || ""));
  }, [config]);

  const num = (s: string): number | undefined => {
    if (s.trim() === "") return undefined;
    const n = Number(s);
    return Number.isFinite(n) ? n : undefined;
  };

  const handleSave = () => {
    const update: ClientBillingConfigUpdate = { enabled, mode };
    const m = num(markup);
    if (m !== undefined) update.markup = m;
    const mp = num(minPrice);
    if (mp !== undefined) update.min_price_rub = mp;
    const r = num(rounding);
    if (r !== undefined) update.rounding_rub = r;
    const fx = num(fxMarkup);
    if (fx !== undefined) update.fx_markup_percent = fx;
    update.budget_rub = num(budget) ?? 0;
    update.subscription_fee_rub = num(subscriptionFee) ?? 0;
    update.fair_use_rub = num(fairUse) ?? 0;
    onSave(update);
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

      <div className="grid grid-cols-2 gap-2">
        <NumberField label={t(($) => $.billing.markup)} value={markup} onChange={setMarkup} step="0.1" />
        <NumberField label={t(($) => $.billing.fx_markup)} value={fxMarkup} onChange={setFxMarkup} step="0.5" />
        <NumberField label={t(($) => $.billing.min_price)} value={minPrice} onChange={setMinPrice} step="50" />
        <NumberField label={t(($) => $.billing.rounding)} value={rounding} onChange={setRounding} step="10" />
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

      <div>
        <Button size="sm" className="h-7 px-3 text-xs" disabled={saving} onClick={handleSave}>
          {t(($) => $.billing.save)}
        </Button>
      </div>
    </div>
  );
}
