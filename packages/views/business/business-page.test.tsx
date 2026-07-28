import { fireEvent, render, screen } from "@testing-library/react";
import { describe, expect, it, vi } from "vitest";
import {
  BusinessPage,
  counterpartyReasonTranslationKey,
  counterpartyResolutionTransactionID,
  groupUnresolvedBankCounterparties,
  parseCounterpartyResolutionTarget,
} from "./business-page";

const { dashboard, snapshot } = vi.hoisted(() => ({
  dashboard: {
    bank_client_income_rub: "439150.00",
    overdue_rub: "50000.00",
    payable_rub: "0",
    reserve_balance_rub: "0",
    reserve_deficit_rub: "0",
    owner_net_income_rub: "439150.00",
    owner_target_progress_pct: "43.92",
    task_value_rub: "0",
    unknown_inbound_rub: "110000.00",
    unmatched_count: 37,
    vitmax_transit_rub: "398600.00",
    transfer_rub: "1365600.00",
  },
  snapshot: {
    clients: [{ id: "client-1", canonical_name: "Client" }],
    projects: [],
    agreements: [],
    receivables: [],
    transactions: [{
      id: "transaction-1",
      booked_on: new Date().toISOString().slice(0, 10),
      direction: "outbound",
      amount_rub: "1250.00",
      counterparty_name: "Vendor",
      counterparty_inn: "1234567890",
      classification: "unknown",
      classification_confidence: "unresolved",
      purpose: "Services",
      is_matched: false,
    }],
    counterparties: [],
    bank_imports: [],
    recurring_costs: [],
    workers: [{ id: "worker-1", name: "Worker" }],
    task_economics: [],
    accruals: [],
    reserve_ledger: [],
    payout_batches: [],
  },
}));

vi.mock("@multica/core/config", () => ({
  useFeatureEnabled: () => true,
}));

const businessAction = vi.hoisted(() => vi.fn().mockResolvedValue({}));

vi.mock("@multica/core/api", () => ({
  api: { businessAction },
}));

vi.mock("@tanstack/react-query", () => ({
  useQuery: ({ queryKey }: { queryKey: readonly unknown[] }) => {
    const shared = {
      isLoading: false,
      error: null,
      refetch: vi.fn().mockResolvedValue(undefined),
    };
    if (queryKey[2] === "dashboard") return { ...shared, data: dashboard };
    if (queryKey[2] === "snapshot") return { ...shared, data: snapshot };
    return {
      ...shared,
      data: [{ id: "business-1", name: "Business" }],
    };
  },
}));

describe("BusinessPage layout", () => {
  it("owns vertical scrolling inside the fixed dashboard shell", () => {
    render(<BusinessPage />);

    const scrollContainer = screen.getByTestId("business-scroll-container");
    expect(scrollContainer).toHaveClass("min-h-0", "flex-1", "overflow-y-auto");
    expect(scrollContainer.parentElement).toHaveClass(
      "flex",
      "min-h-0",
      "flex-1",
      "flex-col",
    );
  });

  it("uses the standard compact Multica page header above the nav/content split", () => {
    const { container } = render(<BusinessPage />);

    const header = container.querySelector("header");
    expect(header).not.toBeNull();
    expect(header).toHaveClass("min-h-12", "border-b", "px-5");
    expect(header?.querySelector("h1")).toHaveClass("text-sm", "font-medium");
  });

  it("bounds business tables with their own scroll area and hides legacy bank entry controls", () => {
    render(<BusinessPage />);

    // The unit test intentionally has no i18next instance, so tab labels are
    // empty; Bank is the fifth stable navigation item.
    const tabs = screen.getAllByRole("tab");
    expect(tabs.length).toBeGreaterThan(4);
    fireEvent.click(tabs[4]!);

    const table = screen.getAllByTestId("business-row-table")[0]!;
    expect(table.parentElement).toHaveClass("max-h-[60vh]", "overflow-auto");
    expect(table.querySelector("th")).toHaveClass("sticky", "top-0");
    expect(screen.queryByText(/import statement|загрузить выписку/i)).not.toBeInTheDocument();
    expect(screen.queryByText(/add transaction|добавить операцию/i)).not.toBeInTheDocument();
  });

  it("submits counterparty rules and only offers roles compatible with operation direction", () => {
    const { container } = render(<BusinessPage />);

    const tabs = screen.getAllByRole("tab");
    fireEvent.click(tabs[4]!);

    const resolutionForm = container.querySelector("form.min-w-80");
    expect(resolutionForm).not.toBeNull();
    expect(resolutionForm?.querySelector("button")).toHaveAttribute("type", "submit");

    const optionValues = [...(resolutionForm?.querySelectorAll("option") ?? [])]
      .map((option) => option.value);
    expect(optionValues).not.toContain("client:client-1");
    expect(optionValues).toContain("worker:worker-1");
    expect(optionValues).toEqual(expect.arrayContaining([
      "class:vendor",
      "class:transit",
      "class:ignored",
    ]));
  });

  it("moves bank search to the page header and keeps metadata in one summary row", () => {
    const { container } = render(<BusinessPage />);

    const tabs = screen.getAllByRole("tab");
    expect(screen.queryByTestId("bank-counterparty-search")).not.toBeInTheDocument();
    fireEvent.click(tabs[4]!);

    const search = screen.getByTestId("bank-counterparty-search");
    expect(container.querySelector("header")?.contains(search)).toBe(true);

    const summary = screen.getByTestId("bank-summary");
    expect(summary).toHaveClass("whitespace-nowrap");
    expect(summary).toHaveTextContent(/1.?250/);
  });

  it("hides the unresolved counterparties section when every operation is resolved", () => {
    const originalTransactions = snapshot.transactions;
    snapshot.transactions = [];

    try {
      render(<BusinessPage />);
      const tabs = screen.getAllByRole("tab");
      fireEvent.click(tabs[4]!);

      expect(screen.queryByTestId("unresolved-counterparties-section")).not.toBeInTheDocument();
    } finally {
      snapshot.transactions = originalTransactions;
    }
  });
});

describe("receivable payments", () => {
  const month = new Date().toISOString().slice(0, 7);

  async function withReceivables<T>(rows: Record<string, unknown>[], body: () => Promise<T>): Promise<T> {
    const originalReceivables = snapshot.receivables;
    const originalAgreements = snapshot.agreements;
    snapshot.receivables = rows as typeof snapshot.receivables;
    snapshot.agreements = [
      { id: "agreement-card", name: "Client — SEO", client_id: "client-1", model: "fixed", payment_channel: "personal_card" },
      { id: "agreement-bank", name: "Client — support", client_id: "client-1", model: "fixed", payment_channel: "bank" },
    ] as typeof snapshot.agreements;
    try {
      return await body();
    } finally {
      snapshot.receivables = originalReceivables;
      snapshot.agreements = originalAgreements;
    }
  }

  function receivable(overrides: Record<string, unknown>): Record<string, unknown> {
    return {
      client_id: "client-1",
      client_name: "Client",
      agreement_id: "agreement-card",
      period_key: month,
      planned_amount_rub: "50000.00",
      paid_amount_rub: "0",
      status: "overdue",
      invoice_on: `${month}-01`,
      due_on: `${month}-08`,
      is_overdue: true,
      ...overrides,
    };
  }

  it("records a payment from the side panel opened for the picked receivable", async () => {
    await withReceivables([receivable({ id: "receivable-1" })], async () => {
      const { container } = render(<BusinessPage />);
      fireEvent.click(screen.getAllByRole("tab")[1]!);

      // The row itself only carries a trigger; the fields live in the drawer.
      expect(container.querySelector('input[name="received_on"]')).toBeNull();
      fireEvent.click(screen.getByTestId("record-payment-trigger"));

      const dateInput = await screen.findByDisplayValue(/^\d{4}-\d{2}-\d{2}$/);
      const form = dateInput.closest("form")!;
      // The outstanding balance is the default so the common case is one click.
      expect(form.querySelector('input[name="amount"]')).toHaveValue(50000);
      // Card agreements preselect their own channel, never bank.
      expect(form.querySelector('select[name="channel"]')).toHaveValue("personal_card");

      businessAction.mockClear();
      fireEvent.submit(form);

      expect(businessAction).toHaveBeenCalledTimes(1);
      const [businessId, path, body] = businessAction.mock.calls[0]!;
      expect(businessId).toBe("business-1");
      expect(path).toBe("receivables/receivable-1/payments");
      expect(body).toMatchObject({
        amount_rub: "50000",
        received_on: expect.stringMatching(/^\d{4}-\d{2}-\d{2}$/),
        payment_channel: "personal_card",
      });
      expect(String((body as { idempotency_key: string }).idempotency_key)).not.toBe("");
    });
  });

  it("leaves bank agreements to statement reconciliation", async () => {
    await withReceivables([receivable({ id: "receivable-bank", agreement_id: "agreement-bank" })], async () => {
      render(<BusinessPage />);
      fireEvent.click(screen.getAllByRole("tab")[1]!);

      expect(screen.queryByTestId("record-payment-trigger")).not.toBeInTheDocument();
    });
  });

  it("offers no payment action for receivables that cannot take one", async () => {
    await withReceivables([
      receivable({ id: "receivable-paid", status: "paid", paid_amount_rub: "50000.00" }),
      receivable({ id: "receivable-written-off", status: "written_off", due_on: `${month}-21` }),
    ], async () => {
      render(<BusinessPage />);
      fireEvent.click(screen.getAllByRole("tab")[1]!);

      expect(screen.queryByTestId("record-payment-trigger")).not.toBeInTheDocument();
    });
  });
});

describe("bank counterparty resolution", () => {
  it("localizes every system reason currently stored in production", () => {
    expect(counterpartyReasonTranslationKey("Owner-approved personal business payer registry"))
      .toBe("bank.reasons.owner_approved_personal_payer_registry");
    expect(counterpartyReasonTranslationKey("manual bank counterparty resolution"))
      .toBe("bank.reasons.manual_counterparty_resolution");
    expect(counterpartyReasonTranslationKey("VitMax transit; excluded from personal revenue"))
      .toBe("bank.reasons.vitmax_transit_excluded");
    expect(counterpartyReasonTranslationKey("Own-account transfer; excluded from revenue and expenses"))
      .toBe("bank.reasons.own_account_transfer_excluded");
    expect(counterpartyReasonTranslationKey("Дантистоф — подтверждено владельцем 19.07.2026"))
      .toBeNull();
  });

  it("groups unknown operations by INN and keeps direction totals", () => {
    const rows = groupUnresolvedBankCounterparties([
      { id: "1", classification: "unknown", counterparty_name: "Vendor A", counterparty_inn: "123", direction: "outbound", amount_rub: "100" },
      { id: "2", classification: "unknown", counterparty_name: "Vendor A renamed", counterparty_inn: "123", direction: "outbound", amount_rub: "50" },
      { id: "3", classification: "service", counterparty_name: "Vendor A", counterparty_inn: "123", direction: "outbound", amount_rub: "900" },
    ]);

    expect(rows).toHaveLength(1);
    expect(rows[0]).toMatchObject({ transaction_id: "1", outbound_transaction_id: "1", inbound_transaction_id: "", operation_count: 2, outbound_rub: 150, inbound_rub: 0 });
  });

  it("uses an operation whose direction matches the selected entity", () => {
    const [row] = groupUnresolvedBankCounterparties([
      { id: "out", classification: "unknown", counterparty_name: "Mixed", counterparty_inn: "123", direction: "outbound", amount_rub: "100" },
      { id: "in", classification: "unknown", counterparty_name: "Mixed", counterparty_inn: "123", direction: "inbound", amount_rub: "50" },
    ]);

    expect(counterpartyResolutionTransactionID(row!, { classification: "client_payer", client_id: "client-1" })).toBe("in");
    expect(counterpartyResolutionTransactionID(row!, { classification: "worker_payee", worker_id: "worker-1" })).toBe("out");
    expect(counterpartyResolutionTransactionID(row!, { classification: "transit" })).toBe("out");
  });

  it("maps UI targets to business entities rather than projects", () => {
    expect(parseCounterpartyResolutionTarget("client:client-1")).toEqual({ classification: "client_payer", client_id: "client-1" });
    expect(parseCounterpartyResolutionTarget("worker:worker-1")).toEqual({ classification: "worker_payee", worker_id: "worker-1" });
    expect(parseCounterpartyResolutionTarget("class:vendor")).toEqual({ classification: "vendor" });
    expect(parseCounterpartyResolutionTarget("project:project-1")).toBeNull();
  });
});
