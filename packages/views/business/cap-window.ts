// A capped agreement is watched over a window that runs from invoice day to
// invoice day (17 -> 17), not from the first of the month. That is the window
// the owner quotes to a client: "half the period is gone and 75% of the ceiling
// is already used". Receivables keep their calendar periods; this is a live
// read of the current window only.

const DAY_MS = 86_400_000;

export interface CapWindow {
  /** Inclusive start, YYYY-MM-DD. */
  start: string;
  /** Exclusive end, YYYY-MM-DD. */
  end: string;
  totalDays: number;
  elapsedDays: number;
  /** Share of the window already spent, 0..1. */
  elapsedRatio: number;
}

export interface CapCharge {
  chargedOn: string;
  amount: number;
}

export interface CapProgress {
  window: CapWindow;
  cap: number;
  used: number;
  count: number;
  /** Never negative: an overspent ceiling has nothing left, not a debt. */
  remaining: number;
  /** Share of the ceiling already used; goes above 1 when advisory. */
  usedRatio: number;
  overspent: boolean;
  /** Positive when the ceiling burns down faster than the window runs out. */
  paceGap: number;
}

export interface CapWindowInput {
  invoiceDay?: number | null;
  periodMonths?: number | null;
  effectiveFrom?: string | null;
  /** Today as YYYY-MM-DD. */
  today: string;
}

interface CalendarDate {
  year: number;
  month: number;
  day: number;
}

function parseDate(value: string): CalendarDate | null {
  const match = /^(\d{4})-(\d{2})-(\d{2})/.exec(value);
  if (!match) return null;
  return { year: Number(match[1]), month: Number(match[2]), day: Number(match[3]) };
}

function daysInMonth(year: number, month: number): number {
  return new Date(Date.UTC(year, month, 0)).getUTCDate();
}

function addMonths(year: number, month: number, count: number): { year: number; month: number } {
  const index = year * 12 + (month - 1) + count;
  return { year: Math.floor(index / 12), month: (index % 12) + 1 };
}

/** The invoice day clamped to a month that is too short for it. */
function anchorOf(year: number, month: number, day: number): string {
  const clamped = Math.min(Math.max(day, 1), daysInMonth(year, month));
  return `${String(year).padStart(4, "0")}-${String(month).padStart(2, "0")}-${String(clamped).padStart(2, "0")}`;
}

function utcMs(iso: string): number {
  const parsed = parseDate(iso);
  if (!parsed) return Number.NaN;
  return Date.UTC(parsed.year, parsed.month - 1, parsed.day);
}

export function resolveCapWindow(input: CapWindowInput): CapWindow | null {
  const today = parseDate(input.today);
  if (!today) return null;
  const periodMonths = Math.max(1, Math.trunc(input.periodMonths ?? 1));
  // No invoice day means nothing was agreed about billing dates, so the window
  // falls back to the calendar month the rest of the module already uses.
  const invoiceDay = Math.min(Math.max(Math.trunc(input.invoiceDay ?? 1), 1), 31);
  const origin = (input.effectiveFrom ? parseDate(input.effectiveFrom) : null) ?? today;

  let months = (today.year - origin.year) * 12 + (today.month - origin.month);
  if (today.day < Math.min(invoiceDay, daysInMonth(today.year, today.month))) months -= 1;
  const startMonth = addMonths(origin.year, origin.month, Math.floor(months / periodMonths) * periodMonths);
  const endMonth = addMonths(startMonth.year, startMonth.month, periodMonths);

  const start = anchorOf(startMonth.year, startMonth.month, invoiceDay);
  const end = anchorOf(endMonth.year, endMonth.month, invoiceDay);
  const totalDays = Math.round((utcMs(end) - utcMs(start)) / DAY_MS);
  const elapsedDays = Math.min(Math.max(Math.round((utcMs(input.today) - utcMs(start)) / DAY_MS), 0), totalDays);
  return {
    start,
    end,
    totalDays,
    elapsedDays,
    elapsedRatio: totalDays > 0 ? elapsedDays / totalDays : 0,
  };
}

export function sumCapCharges(charges: readonly CapCharge[], window: CapWindow): { amount: number; count: number } {
  let amount = 0;
  let count = 0;
  for (const charge of charges) {
    // ISO dates compare lexicographically, and the window end is exclusive.
    if (charge.chargedOn < window.start || charge.chargedOn >= window.end) continue;
    amount += charge.amount;
    count += 1;
  }
  return { amount, count };
}

export function capProgress(window: CapWindow, cap: number, used: number, count: number): CapProgress {
  const usedRatio = cap > 0 ? used / cap : 0;
  return {
    window,
    cap,
    used,
    count,
    remaining: Math.max(cap - used, 0),
    usedRatio,
    overspent: cap > 0 && used > cap,
    paceGap: usedRatio - window.elapsedRatio,
  };
}
