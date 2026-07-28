import { describe, expect, it } from "vitest";

import { capProgress, resolveCapWindow, sumCapCharges } from "./cap-window";

describe("resolveCapWindow", () => {
  it("runs from the invoice day to the next invoice day", () => {
    const window = resolveCapWindow({ invoiceDay: 17, today: "2026-07-28" });
    expect(window).toMatchObject({ start: "2026-07-17", end: "2026-08-17", totalDays: 31, elapsedDays: 11 });
  });

  it("stays in the previous window until the invoice day arrives", () => {
    const window = resolveCapWindow({ invoiceDay: 17, today: "2026-07-05" });
    expect(window).toMatchObject({ start: "2026-06-17", end: "2026-07-17" });
  });

  it("opens the window on the invoice day itself", () => {
    const window = resolveCapWindow({ invoiceDay: 20, today: "2026-07-20" });
    expect(window).toMatchObject({ start: "2026-07-20", end: "2026-08-20", elapsedDays: 0, elapsedRatio: 0 });
  });

  it("clamps an invoice day the month is too short for", () => {
    const window = resolveCapWindow({ invoiceDay: 31, today: "2026-02-10" });
    expect(window).toMatchObject({ start: "2026-01-31", end: "2026-02-28" });
  });

  it("falls back to the calendar month when no invoice day is agreed", () => {
    const window = resolveCapWindow({ invoiceDay: null, today: "2026-07-28" });
    expect(window).toMatchObject({ start: "2026-07-01", end: "2026-08-01" });
  });

  it("tiles multi-month periods from the effective date", () => {
    const window = resolveCapWindow({
      invoiceDay: 10,
      periodMonths: 3,
      effectiveFrom: "2026-01-10",
      today: "2026-07-28",
    });
    expect(window).toMatchObject({ start: "2026-07-10", end: "2026-10-10" });
  });

  it("rejects a date it cannot read", () => {
    expect(resolveCapWindow({ invoiceDay: 17, today: "" })).toBeNull();
  });
});

describe("sumCapCharges", () => {
  const window = resolveCapWindow({ invoiceDay: 17, today: "2026-07-28" });

  it("counts only what falls inside the window, end excluded", () => {
    expect(window).not.toBeNull();
    const total = sumCapCharges(
      [
        { chargedOn: "2026-07-16", amount: 1000 },
        { chargedOn: "2026-07-17", amount: 2000 },
        { chargedOn: "2026-07-28", amount: 500 },
        { chargedOn: "2026-08-17", amount: 9000 },
      ],
      window!,
    );
    expect(total).toEqual({ amount: 2500, count: 2 });
  });
});

describe("capProgress", () => {
  const window = resolveCapWindow({ invoiceDay: 17, today: "2026-08-01" });

  it("compares the ceiling burn against the elapsed period", () => {
    expect(window).not.toBeNull();
    const progress = capProgress(window!, 100_000, 75_000, 4);
    expect(progress.remaining).toBe(25_000);
    expect(progress.usedRatio).toBeCloseTo(0.75, 5);
    expect(progress.overspent).toBe(false);
    expect(progress.paceGap).toBeGreaterThan(0);
  });

  it("keeps an overspent ceiling at zero remaining", () => {
    expect(window).not.toBeNull();
    const progress = capProgress(window!, 100_000, 120_000, 7);
    expect(progress.remaining).toBe(0);
    expect(progress.overspent).toBe(true);
  });
});
