import { describe, expect, it } from "vitest";
import type { BusinessRow } from "@multica/core/types";
import { calculateRecurringCostTotal } from "./business-costs-tab";

describe("calculateRecurringCostTotal", () => {
  it("counts active USD schedules for each selected month", () => {
    const rows: BusinessRow[] = [
      { amount: "400", currency: "USD", charge_day: 15, starts_on: "2026-07-01", ends_on: null, status: "active" },
      { amount: "100", currency: "USD", charge_day: 15, starts_on: "2026-07-01", ends_on: null, status: "active" },
      { amount: "60", currency: "USD", charge_day: 15, starts_on: "2026-07-01", ends_on: null, status: "active" },
      { amount: "20", currency: "USD", charge_day: 15, starts_on: "2026-07-01", ends_on: null, status: "active" },
    ];

    expect(calculateRecurringCostTotal(rows, ["2026-07"], () => 90)).toBe(52_200);
    expect(calculateRecurringCostTotal(rows, ["2026-07", "2026-08"], () => 90)).toBe(104_400);
  });

  it("does not count paused or not-yet-started schedules", () => {
    const rows: BusinessRow[] = [
      { amount: "100", currency: "RUB", charge_day: 31, starts_on: "2026-08-01", status: "active" },
      { amount: "200", currency: "RUB", charge_day: 15, starts_on: "2026-07-01", status: "paused" },
    ];

    expect(calculateRecurringCostTotal(rows, ["2026-07"], () => 90)).toBe(0);
  });
});
