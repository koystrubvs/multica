import { beforeEach, describe, expect, it, vi } from "vitest";
import { fireEvent, render, screen } from "@testing-library/react";
import { BoardColumn, formatCompactTokens } from "./board-column";

const toggleBoardColumnCollapsed = vi.hoisted(() => vi.fn());
const collapsedColumns = vi.hoisted(() => ({ ids: [] as string[] }));

vi.mock("@multica/core/hooks", () => ({
  useWorkspaceId: () => "ws-1",
}));

vi.mock("@multica/core/modals", () => ({
  useModalStore: {
    getState: () => ({ open: vi.fn() }),
  },
}));

vi.mock("@multica/core/issues/stores/view-store-context", () => ({
  useViewStore: (selector?: any) => {
    const state = {
      grouping: "status",
      sortBy: "position",
      boardCollapsedColumns: collapsedColumns.ids,
    };
    return selector ? selector(state) : state;
  },
  useViewStoreApi: () => ({
    getState: () => ({ toggleBoardColumnCollapsed, hideStatus: vi.fn() }),
  }),
}));

vi.mock("@multica/core/workspace/hooks", () => ({
  useActorName: () => ({
    getActorName: (_type: string, id: string) => id,
  }),
}));

vi.mock("../surface/selection-context", () => ({
  useIssueSurfaceSelection: () => ({
    selectedIds: new Set<string>(),
    select: vi.fn(),
    deselect: vi.fn(),
    toggle: vi.fn(),
    clear: vi.fn(),
  }),
}));

// Returns the key path instead of a translation, so the two collapse controls
// stay distinguishable by accessible name.
vi.mock("../../i18n", () => ({
  useT: () => ({
    t: (select: (dict: any) => string) => {
      const path: string[] = [];
      const probe: any = new Proxy(
        {},
        {
          get: (_target, key) => {
            path.push(String(key));
            return probe;
          },
        },
      );
      select(probe);
      return path.join(".");
    },
  }),
}));

vi.mock("@dnd-kit/core", () => ({
  DndContext: ({ children }: { children: React.ReactNode }) => children,
  DragOverlay: () => null,
  PointerSensor: class {},
  useSensor: () => ({}),
  useSensors: () => [],
  useDroppable: () => ({ setNodeRef: vi.fn(), isOver: false }),
}));

vi.mock("@dnd-kit/sortable", () => ({
  SortableContext: ({ children }: { children: React.ReactNode }) => children,
  verticalListSortingStrategy: {},
  arrayMove: <T,>(items: T[]) => items,
}));

const group = {
  id: "status:done",
  title: "done",
  status: "done" as const,
  createData: { status: "done" as const },
};

function renderColumn(props: { cost?: { tokens: number; price_rub: number } } = {}) {
  return render(
    <BoardColumn
      group={group}
      issueIds={[]}
      issueMap={new Map()}
      totalCount={7}
      cost={props.cost}
    />,
  );
}

beforeEach(() => {
  toggleBoardColumnCollapsed.mockClear();
  collapsedColumns.ids = [];
});

describe("board column collapse", () => {
  it("collapses from the column header", () => {
    renderColumn();

    fireEvent.click(screen.getByLabelText("board.collapse_column"));

    expect(toggleBoardColumnCollapsed).toHaveBeenCalledWith("status:done");
  });

  it("renders a rail instead of the column body when collapsed", () => {
    collapsedColumns.ids = ["status:done"];
    renderColumn();

    // The rail keeps the column's identity (localized name + count) …
    expect(screen.getByText("status.done")).toBeTruthy();
    expect(screen.getByText("7")).toBeTruthy();
    // … but neither the column body nor its header controls are mounted.
    expect(screen.queryByText("board.empty_column")).toBeNull();
    expect(screen.queryByLabelText("board.collapse_column")).toBeNull();
  });

  it("renders the client price when the viewer gets cost totals", () => {
    renderColumn({ cost: { tokens: 19_899_582, price_rub: 8500 } });

    // Grouped digits, no kopecks: the price is rounded to a 50 ₽ step per
    // issue, so decimals would always be ",00".
    expect(screen.getByText("8 500 ₽")).toBeTruthy();
  });

  it("renders no price line for a viewer without cost totals", () => {
    // A member or guest never receives totals — the endpoint answers 403 — so
    // the column must not grow an empty slot for them.
    renderColumn();

    expect(screen.queryByText(/₽/)).toBeNull();
  });

  it("renders no price line when nothing in the column is billable", () => {
    renderColumn({ cost: { tokens: 4200, price_rub: 0 } });

    expect(screen.queryByText(/₽/)).toBeNull();
  });

  it("abbreviates the token count by scale", () => {
    const units = { thousand: "К", million: "М", billion: "Б" };

    // Under a thousand stays exact — there is nothing to shorten.
    expect(formatCompactTokens(842, units)).toBe("842");
    expect(formatCompactTokens(1234, units)).toBe("1,2К");
    // A trailing zero is dropped: "5К", not "5,0К".
    expect(formatCompactTokens(5000, units)).toBe("5К");
    expect(formatCompactTokens(19_899_582, units)).toBe("19,9М");
    // A busy done column really does reach this scale; without the billion
    // step it would read "3205,7М".
    expect(formatCompactTokens(3_205_724_882, units)).toBe("3,2Б");
    // Boundaries land on the larger unit, not on 1000 of the smaller one.
    expect(formatCompactTokens(1_000_000, units)).toBe("1М");
    expect(formatCompactTokens(999_999, units)).toBe("1М");
  });

  it("expands again when the rail is clicked", () => {
    collapsedColumns.ids = ["status:done"];
    renderColumn();

    fireEvent.click(screen.getByLabelText("board.expand_column"));

    expect(toggleBoardColumnCollapsed).toHaveBeenCalledWith("status:done");
  });
});
