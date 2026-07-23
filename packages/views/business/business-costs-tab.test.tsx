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

  it("rounds the RUB total to the nearest hundred", () => {
    const rows: BusinessRow[] = [
      { amount: "580", currency: "USD", charge_day: 15, starts_on: "2026-07-01", status: "active" },
    ];

    expect(calculateRecurringCostTotal(rows, ["2026-07"], () => 77.4912)).toBe(44_900);
  });

  it("counts a yearly schedule only in its anniversary month", () => {
    const rows: BusinessRow[] = [
      {
        amount: "18200",
        currency: "RUB",
        frequency: "yearly",
        charge_day: 15,
        starts_on: "2026-07-01",
        status: "active",
      },
    ];

    expect(calculateRecurringCostTotal(rows, ["2026-06"], () => 90)).toBe(0);
    expect(calculateRecurringCostTotal(rows, ["2026-07", "2026-08"], () => 90)).toBe(18_200);
    expect(calculateRecurringCostTotal(rows, ["2027-07"], () => 90)).toBe(18_200);
  });
});
