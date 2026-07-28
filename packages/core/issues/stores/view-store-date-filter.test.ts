// @vitest-environment jsdom

import { describe, expect, it } from "vitest";
import {
  mergeViewStatePersisted,
  refreshIssueDateFilter,
  viewStorePersistOptions,
  viewStoreSlice,
  type IssueViewState,
} from "./view-store";
import { addDaysDateOnly, todayDateOnly } from "../date";

function state(overrides: Partial<IssueViewState> = {}): IssueViewState {
  return { ...viewStoreSlice(() => undefined), ...overrides };
}

describe("refreshIssueDateFilter", () => {
  it("re-resolves a preset against today", () => {
    const stale = {
      field: "created_at" as const,
      from: "2020-01-01",
      to: "2020-01-07",
      preset: 7 as const,
    };
    expect(refreshIssueDateFilter(stale)).toEqual({
      field: "created_at",
      from: addDaysDateOnly(-6),
      to: todayDateOnly(),
      preset: 7,
    });
  });

  it("leaves a hand-picked range alone", () => {
    const custom = { field: "updated_at" as const, from: "2026-03-01", to: "2026-03-15" };
    expect(refreshIssueDateFilter(custom)).toEqual(custom);
  });

  it("passes an absent filter through", () => {
    expect(refreshIssueDateFilter(null)).toBeNull();
    expect(refreshIssueDateFilter(undefined)).toBeNull();
  });
});

describe("issue view persistence", () => {
  it("keeps the date filter across a reload", () => {
    const filter = { field: "created_at" as const, from: "2026-07-22", to: "2026-07-28", preset: 7 as const };
    const persisted = viewStorePersistOptions("test").partialize(state({ dateFilter: filter }));
    expect(persisted.dateFilter).toEqual(filter);

    const merged = mergeViewStatePersisted(persisted, state());
    expect(merged.dateFilter).toEqual({ ...filter, from: addDaysDateOnly(-6), to: todayDateOnly() });
  });

  it("restores a hand-picked range unchanged", () => {
    const filter = { field: "updated_at" as const, from: "2026-03-01", to: "2026-03-15" };
    const merged = mergeViewStatePersisted({ dateFilter: filter }, state());
    expect(merged.dateFilter).toEqual(filter);
  });

  it("has no date filter when nothing was stored", () => {
    expect(mergeViewStatePersisted({}, state()).dateFilter).toBeNull();
  });
});
