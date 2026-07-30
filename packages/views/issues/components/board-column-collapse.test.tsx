import { beforeEach, describe, expect, it, vi } from "vitest";
import { fireEvent, render, screen } from "@testing-library/react";
import { BoardColumn } from "./board-column";

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

function renderColumn() {
  return render(
    <BoardColumn
      group={group}
      issueIds={[]}
      issueMap={new Map()}
      totalCount={7}
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

  it("expands again when the rail is clicked", () => {
    collapsedColumns.ids = ["status:done"];
    renderColumn();

    fireEvent.click(screen.getByLabelText("board.expand_column"));

    expect(toggleBoardColumnCollapsed).toHaveBeenCalledWith("status:done");
  });
});
