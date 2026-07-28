import { describe, expect, it } from "vitest";
import type { BusinessRow } from "@multica/core/types";
import { capCharges, hasCapMeter } from "./business-cap-meter";

const CLIENT = "client-1";
const SUPPORT_PROJECT = "project-support";
const SEO_PROJECT = "project-seo";

function charge(row: Partial<BusinessRow>): BusinessRow {
  return {
    client_id: CLIENT,
    project_id: SUPPORT_PROJECT,
    charged_on: "2026-07-20",
    price_rub: "1000",
    period_status: "",
    is_internal: false,
    ...row,
  };
}

describe("hasCapMeter", () => {
  it("only speaks for capped agreements with a ceiling set", () => {
    expect(hasCapMeter({ model: "cap", cap_rub: "25000", invoice_day: 30 })).toBe(true);
    expect(hasCapMeter({ model: "cap", cap_rub: null, invoice_day: 30 })).toBe(false);
    expect(hasCapMeter({ model: "fixed", cap_rub: "25000", invoice_day: 30 })).toBe(false);
  });

  it("stays silent for irregular support, which has no invoice day", () => {
    expect(hasCapMeter({ model: "cap", cap_rub: "50000", invoice_day: null })).toBe(false);
  });
});

describe("capCharges", () => {
  const rows = [
    charge({}),
    charge({ project_id: SEO_PROJECT, price_rub: "9000" }),
    charge({ client_id: "client-2", price_rub: "7000" }),
    charge({ is_internal: true, price_rub: "500" }),
    charge({ period_status: "invoiced", price_rub: "4000" }),
    charge({ period_status: "open", price_rub: "2000" }),
  ];

  it("counts only the pinned project when the agreement has one", () => {
    const kept = capCharges({ client_id: CLIENT, project_id: SUPPORT_PROJECT }, rows);
    expect(kept.map((row) => row.amount)).toEqual([1000, 2000]);
  });

  it("counts the whole client when the agreement has no project", () => {
    const kept = capCharges({ client_id: CLIENT, project_id: null }, rows);
    expect(kept.map((row) => row.amount)).toEqual([1000, 9000, 2000]);
  });

  it("drops internal work and anything an issued invoice already covers", () => {
    const kept = capCharges({ client_id: CLIENT, project_id: SUPPORT_PROJECT }, rows);
    expect(kept.some((row) => row.amount === 500)).toBe(false);
    expect(kept.some((row) => row.amount === 4000)).toBe(false);
  });

  it("normalizes the charge date to a plain day", () => {
    const kept = capCharges({ client_id: CLIENT, project_id: SUPPORT_PROJECT }, [
      charge({ charged_on: "2026-07-20T09:15:00Z" }),
    ]);
    expect(kept[0]?.chargedOn).toBe("2026-07-20");
  });
});
