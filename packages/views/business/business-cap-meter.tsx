"use client";

import { useMemo } from "react";
import { capProgress, resolveCapWindow, sumCapCharges, type CapCharge } from "./cap-window";
import type { BusinessRow } from "@multica/core/types";
import { cn } from "@multica/ui/lib/utils";
import { useT } from "../i18n";

// A billing period in any of these states has already been billed to the
// client, so its work must not be charged against the current ceiling again.
const SETTLED_PERIODS = new Set(["closed", "invoiced", "paid"]);

function rub(value: number): string {
  return new Intl.NumberFormat("ru-RU", { style: "currency", currency: "RUB", maximumFractionDigits: 0 }).format(
    Number.isFinite(value) ? value : 0,
  );
}

function percent(ratio: number): string {
  return `${Math.round(ratio * 100)}%`;
}

function dayLabel(iso: string, locale: string): string {
  const parsed = new Date(`${iso}T00:00:00Z`);
  if (Number.isNaN(parsed.getTime())) return iso;
  return new Intl.DateTimeFormat(locale, { day: "numeric", month: "short", timeZone: "UTC" }).format(parsed);
}

// The owner's local day, not the UTC one: the window is quoted to clients in
// their own calendar terms.
export function localToday(): string {
  const now = new Date();
  return new Date(now.getTime() - now.getTimezoneOffset() * 60_000).toISOString().slice(0, 10);
}

/** True for agreements this meter can say anything about. */
export function hasCapMeter(agreement: BusinessRow): boolean {
  return String(agreement.model ?? "") === "cap" && Number(agreement.cap_rub ?? 0) > 0;
}

/** The charges from `billing_tasks` that count against one agreement's ceiling. */
export function capCharges(agreement: BusinessRow, charges: readonly BusinessRow[]): CapCharge[] {
  // A client with both an SEO and a support deal has one capped agreement per
  // project, so charges are attributed by project whenever the agreement has
  // one. Without a project (Dolgoletie runs two sites under one deal) the
  // client-wide sum is the intended reading.
  const projectID = String(agreement.project_id ?? "");
  const clientID = String(agreement.client_id ?? "");
  const rows: CapCharge[] = [];
  for (const row of charges) {
    if (String(row.client_id ?? "") !== clientID) continue;
    if (projectID && String(row.project_id ?? "") !== projectID) continue;
    // Internal work is not the client's to pay for, and work an issued invoice
    // already covers belongs to that invoice — the ceiling watches what is
    // accumulating towards the *next* one.
    if (row.is_internal === true) continue;
    if (SETTLED_PERIODS.has(String(row.period_status ?? ""))) continue;
    rows.push({ chargedOn: String(row.charged_on ?? "").slice(0, 10), amount: Number(row.price_rub ?? 0) });
  }
  return rows;
}

// How much of the ceiling the current invoice-day window has already eaten,
// next to how much of the window itself is gone. The gap between the two is
// the sentence the owner writes to a client: "half the period is gone and the
// cap is 75% used".
export function BusinessCapMeter({ agreement, charges, locale, today }: {
  agreement: BusinessRow;
  /** Snapshot `billing_tasks` rows; the meter picks its own out of them. */
  charges: readonly BusinessRow[];
  locale: string;
  today?: string;
}) {
  const { t } = useT("business");
  const day = today ?? localToday();

  const window = useMemo(() => resolveCapWindow({
    invoiceDay: agreement.invoice_day == null ? null : Number(agreement.invoice_day),
    periodMonths: Number(agreement.period_months ?? 1),
    effectiveFrom: agreement.effective_from ? String(agreement.effective_from).slice(0, 10) : null,
    today: day,
  }), [agreement.invoice_day, agreement.period_months, agreement.effective_from, day]);

  const scoped = useMemo(() => capCharges(agreement, charges), [charges, agreement]);

  const cap = Number(agreement.cap_rub ?? 0);
  if (!window || !(cap > 0)) return null;

  const totals = sumCapCharges(scoped, window);
  const progress = capProgress(window, cap, totals.amount, totals.count);
  const daysLeft = Math.max(window.totalDays - window.elapsedDays, 0);
  const ahead = !progress.overspent && progress.paceGap > 0.05;
  const fill = progress.overspent ? "bg-destructive" : ahead ? "bg-warning" : "bg-primary";

  return (
    <div className="space-y-1.5 rounded-md border bg-surface-raised/50 p-2" data-testid="cap-meter">
      <div className="flex flex-wrap items-baseline justify-between gap-x-2 text-[11px]">
        <span className="font-medium">{t(($) => $.cap.title)}</span>
        <span className="tabular-nums text-muted-foreground">
          {dayLabel(window.start, locale)} — {dayLabel(window.end, locale)}
          {" · "}
          {t(($) => $.cap.days_left)}: {daysLeft}
        </span>
      </div>
      <div
        className="relative h-2 w-full overflow-hidden rounded-full bg-muted"
        role="progressbar"
        aria-valuemin={0}
        aria-valuemax={100}
        aria-valuenow={Math.round(progress.usedRatio * 100)}
        aria-label={t(($) => $.cap.cap_share)}
      >
        <div className={cn("h-full", fill)} style={{ width: percent(Math.min(progress.usedRatio, 1)) }} />
        {/* Where the period itself stands — the line the fill is racing. */}
        <span
          aria-hidden
          className="absolute inset-y-0 w-px bg-foreground/70"
          style={{ left: percent(window.elapsedRatio) }}
        />
      </div>
      <div className="flex flex-wrap justify-between gap-x-3 gap-y-0.5 text-[11px] tabular-nums text-muted-foreground">
        <span>
          {t(($) => $.cap.used)}: <span className="font-medium text-foreground">{rub(progress.used)}</span>
          {" / "}{rub(cap)}
          {" · "}{t(($) => $.cap.remaining)}: <span className="font-medium text-foreground">{rub(progress.remaining)}</span>
        </span>
        <span>
          {t(($) => $.cap.period_share)} {percent(window.elapsedRatio)}
          {" · "}
          {t(($) => $.cap.cap_share)} {percent(progress.usedRatio)}
        </span>
      </div>
      {progress.overspent && <p className="text-[11px] text-destructive">{t(($) => $.cap.over)}</p>}
      {ahead && <p className="text-[11px] text-warning">{t(($) => $.cap.faster)}</p>}
      {totals.count === 0 && <p className="text-[11px] text-muted-foreground">{t(($) => $.cap.no_charges)}</p>}
    </div>
  );
}
