import { describe, expect, it } from "vitest";
import type { BusinessRow } from "@multica/core/types";
import { agreementBillingExpectation, billingModeMismatch } from "./business-billing-tab";

const CLIENT = "client-1";

function agreement(row: Partial<BusinessRow>): BusinessRow {
  return { client_id: CLIENT, status: "active", model: "cap", cap_mode: "strict", cap_rub: "25000", ...row };
}

describe("agreementBillingExpectation", () => {
  it("expects a capped bill only for a hard-limit agreement", () => {
    expect(agreementBillingExpectation([agreement({})], CLIENT)).toEqual({ mode: "subscription", fee: 25000, hardCaps: 1 });
    expect(agreementBillingExpectation([agreement({ cap_mode: "advisory" })], CLIENT)).toEqual({ mode: "postpaid", fee: 0, hardCaps: 0 });
    expect(agreementBillingExpectation([agreement({ model: "time_material", cap_rub: null })], CLIENT)).toEqual({ mode: "postpaid", fee: 0, hardCaps: 0 });
  });

  it("ignores other clients and inactive agreements", () => {
    const rows = [agreement({ client_id: "client-2" }), agreement({ status: "expired" })];
    expect(agreementBillingExpectation(rows, CLIENT).hardCaps).toBe(0);
  });

  it("reports every hard cap so the row can ask for a manual check", () => {
    const rows = [agreement({ cap_rub: "50000" }), agreement({ cap_rub: "25000" })];
    expect(agreementBillingExpectation(rows, CLIENT)).toEqual({ mode: "subscription", fee: 25000, hardCaps: 2 });
  });
});

describe("billingModeMismatch", () => {
  const capped = { mode: "subscription", fee: 25000, hardCaps: 1 } as const;
  const uncapped = { mode: "postpaid", fee: 0, hardCaps: 0 } as const;

  it("stays quiet when the mode matches the agreement", () => {
    expect(billingModeMismatch({ mode: "subscription", fee: "25000" }, capped)).toBeNull();
    expect(billingModeMismatch({ mode: "postpaid", fee: "" }, uncapped)).toBeNull();
  });

  it("flags a bill left uncapped under a hard-limit agreement", () => {
    expect(billingModeMismatch({ mode: "postpaid", fee: "" }, capped)).toEqual({ kind: "cap_missing", amount: 25000 });
  });

  it("flags a cap the agreement does not ask for", () => {
    expect(billingModeMismatch({ mode: "subscription", fee: "70000" }, uncapped)).toEqual({ kind: "cap_extra" });
  });

  it("flags a cap that differs from the agreed one", () => {
    expect(billingModeMismatch({ mode: "subscription", fee: "70000" }, capped)).toEqual({ kind: "fee_differs", amount: 25000, current: 70000 });
  });

  it("asks for a manual check when several hard caps apply", () => {
    expect(billingModeMismatch({ mode: "subscription", fee: "25000" }, { mode: "subscription", fee: 25000, hardCaps: 2 })).toEqual({ kind: "many_caps" });
  });
});
