import { render, screen } from "@testing-library/react";
import { describe, expect, it, vi } from "vitest";
import { BusinessPage } from "./business-page";

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
    clients: [],
    projects: [],
    agreements: [],
    receivables: [],
    transactions: [],
    workers: [],
    task_economics: [],
    accruals: [],
    reserve_ledger: [],
    payout_batches: [],
  },
}));

vi.mock("@multica/core/config", () => ({
  useFeatureEnabled: () => true,
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

  it("uses the standard compact Multica page header", () => {
    render(<BusinessPage />);

    const scrollContainer = screen.getByTestId("business-scroll-container");
    const header = scrollContainer.previousElementSibling;
    expect(header?.tagName).toBe("HEADER");
    expect(header).toHaveClass("min-h-12", "border-b", "px-5");
    expect(header?.querySelector("h1")).toHaveClass("text-sm", "font-medium");
  });
});
