import React from "react";
import { beforeEach, describe, expect, it, vi } from "vitest";
import { fireEvent, screen, within } from "@testing-library/react";
import userEvent from "@testing-library/user-event";
import type { Project } from "@multica/core/types";
import { renderWithI18n } from "../../test/i18n";
import { NavigationProvider, type NavigationAdapter } from "../../navigation";
import { ProjectsPage } from "./projects-page";

const mocks = vi.hoisted(() => ({
  projects: [] as Project[],
  members: [] as Array<{ user_id: string; name: string; role: string }>,
  agents: [] as Array<{ id: string; name: string; archived_at: string | null }>,
  pins: [] as Array<{ item_type: string; item_id: string }>,
  updateProject: vi.fn(),
  deleteProject: vi.fn(),
  createPin: vi.fn(),
  deletePin: vi.fn(),
  openModal: vi.fn(),
  projectViewState: {
    viewMode: "compact",
    sortField: "name",
    sortDirection: "asc",
    hiddenColumns: [] as string[],
    filters: {
      statuses: [] as string[],
      priorities: [] as string[],
      types: [] as string[],
      clients: [] as string[],
      leads: [] as string[],
    },
    setViewMode: vi.fn(),
    toggleSort: vi.fn(),
    setSortField: vi.fn(),
    setSortDirection: vi.fn(),
    toggleColumn: vi.fn(),
    toggleFilter: vi.fn(),
    clearFilters: vi.fn(),
  },
}));

vi.mock("@tanstack/react-query", () => ({
  useQuery: (options: { queryKey?: readonly unknown[] }) => {
    const key = options.queryKey?.[0];
    if (key === "projects") {
      return { data: mocks.projects, isLoading: false };
    }
    if (key === "members") {
      return { data: mocks.members, isLoading: false };
    }
    if (key === "agents") {
      return { data: mocks.agents, isLoading: false };
    }
    if (key === "pins") {
      return { data: mocks.pins, isLoading: false };
    }
    return { data: [], isLoading: false };
  },
}));

vi.mock("@multica/core/projects", () => ({
  projectListOptions: () => ({ queryKey: ["projects"] }),
  useUpdateProject: () => ({ mutate: mocks.updateProject }),
  useDeleteProject: () => ({ mutate: mocks.deleteProject }),
  useProjectViewStore: (selector: (state: unknown) => unknown) =>
    selector(mocks.projectViewState),
}));

vi.mock("@multica/core/pins", () => ({
  pinListOptions: () => ({ queryKey: ["pins"] }),
  useCreatePin: () => ({ mutate: mocks.createPin }),
  useDeletePin: () => ({ mutate: mocks.deletePin }),
}));

vi.mock("@multica/core/hooks", () => ({
  useWorkspaceId: () => "workspace-1",
}));

vi.mock("@multica/core/paths", () => ({
  useWorkspacePaths: () => ({
    projectDetail: (id: string) => `/test-workspace/projects/${id}`,
    memberDetail: (id: string) => `/test-workspace/members/${id}`,
    agentDetail: (id: string) => `/test-workspace/agents/${id}`,
  }),
}));

vi.mock("@multica/core/auth", () => ({
  useAuthStore: (selector: (state: unknown) => unknown) =>
    selector({ user: { id: "user-1" } }),
}));

vi.mock("@multica/core/workspace/queries", () => ({
  memberListOptions: () => ({ queryKey: ["members"] }),
  agentListOptions: () => ({ queryKey: ["agents"] }),
}));

vi.mock("@multica/core/workspace/hooks", () => ({
  useActorName: () => ({
    getActorName: () => "Test Lead",
    getActorInitials: () => "TL",
    getActorAvatarUrl: () => null,
  }),
}));

vi.mock("@multica/core/modals", () => ({
  useModalStore: {
    getState: () => ({ open: mocks.openModal }),
  },
}));

vi.mock("@multica/ui/components/ui/dropdown-menu", () => ({
  DropdownMenu: ({ children }: { children: React.ReactNode }) => (
    <>{children}</>
  ),
  DropdownMenuTrigger: ({ render }: { render: React.ReactNode }) => (
    <>{render}</>
  ),
  DropdownMenuContent: ({ children }: { children: React.ReactNode }) => (
    <div>{children}</div>
  ),
  DropdownMenuGroup: ({ children }: { children: React.ReactNode }) => (
    <div>{children}</div>
  ),
  DropdownMenuLabel: ({ children }: { children: React.ReactNode }) => (
    <div>{children}</div>
  ),
  DropdownMenuItem: ({
    children,
    onClick,
  }: {
    children: React.ReactNode;
    onClick?: () => void;
  }) => (
    <button type="button" onClick={onClick}>
      {children}
    </button>
  ),
  DropdownMenuCheckboxItem: ({
    children,
    onCheckedChange,
  }: {
    children: React.ReactNode;
    onCheckedChange?: () => void;
  }) => (
    <button type="button" onClick={onCheckedChange}>
      {children}
    </button>
  ),
  DropdownMenuRadioGroup: ({ children }: { children: React.ReactNode }) => (
    <div>{children}</div>
  ),
  DropdownMenuRadioItem: ({
    children,
    onClick,
  }: {
    children: React.ReactNode;
    onClick?: () => void;
  }) => (
    <button type="button" onClick={onClick}>
      {children}
    </button>
  ),
  DropdownMenuSeparator: () => <hr />,
  DropdownMenuSub: ({ children }: { children: React.ReactNode }) => (
    <>{children}</>
  ),
  DropdownMenuSubContent: ({ children }: { children: React.ReactNode }) => (
    <div>{children}</div>
  ),
  DropdownMenuSubTrigger: ({ children }: { children: React.ReactNode }) => (
    <button type="button">{children}</button>
  ),
}));

vi.mock("@multica/ui/components/ui/popover", () => ({
  Popover: ({ children }: { children: React.ReactNode }) => <>{children}</>,
  PopoverTrigger: ({ render }: { render: React.ReactNode }) => <>{render}</>,
  PopoverContent: ({ children }: { children: React.ReactNode }) => (
    <div>{children}</div>
  ),
}));

vi.mock("@multica/ui/components/ui/tooltip", () => ({
  Tooltip: ({ children }: { children: React.ReactNode }) => <>{children}</>,
  TooltipTrigger: ({ render }: { render: React.ReactNode }) => <>{render}</>,
  TooltipContent: ({ children }: { children: React.ReactNode }) => (
    <div role="tooltip">{children}</div>
  ),
}));

const PROJECT: Project = {
  id: "project-1",
  workspace_id: "workspace-1",
  title: "Launch Plan",
  description: null,
  icon: null,
  status: "in_progress",
  priority: "high",
  project_type: "seo",
  client_id: "client-1",
  client_name: "Acme Clinic",
  lead_type: null,
  lead_id: null,
  start_date: null,
  due_date: null,
  created_at: "2026-06-01T00:00:00Z",
  updated_at: "2026-06-01T00:00:00Z",
  issue_count: 3,
  done_count: 1,
  resource_count: 0,
};

function makeAdapter(
  overrides: Partial<NavigationAdapter> = {},
): NavigationAdapter {
  return {
    push: vi.fn(),
    replace: vi.fn(),
    back: vi.fn(),
    pathname: "/test-workspace/projects",
    searchParams: new URLSearchParams(),
    getShareableUrl: (p) => p,
    ...overrides,
  };
}

function renderProjects(adapter = makeAdapter()) {
  renderWithI18n(
    <NavigationProvider value={adapter}>
      <ProjectsPage />
    </NavigationProvider>,
  );
  return adapter;
}

function projectRow() {
  const row = screen.getByText(PROJECT.title).closest('[role="row"]');
  if (!row) throw new Error("project row not found");
  return row as HTMLElement;
}

beforeEach(() => {
  mocks.projects = [PROJECT];
  mocks.members = [
    { user_id: "user-1", name: "User One", role: "admin" },
  ];
  mocks.agents = [];
  mocks.pins = [];
  mocks.updateProject.mockClear();
  mocks.deleteProject.mockClear();
  mocks.createPin.mockClear();
  mocks.deletePin.mockClear();
  mocks.openModal.mockClear();
  mocks.projectViewState.toggleSort.mockClear();
  mocks.projectViewState.setSortField.mockClear();
  mocks.projectViewState.toggleFilter.mockClear();
  mocks.projectViewState.clearFilters.mockClear();
  mocks.projectViewState.viewMode = "compact";
  mocks.projectViewState.sortField = "name";
  mocks.projectViewState.sortDirection = "asc";
  mocks.projectViewState.hiddenColumns = [];
  mocks.projectViewState.filters = {
    statuses: [],
    priorities: [],
    types: [],
    clients: [],
    leads: [],
  };
});

describe("ProjectsPage compact row navigation", () => {
  it("renders project type in its own table column", () => {
    renderProjects();

    expect(screen.getByRole("columnheader", { name: "Project type" })).toBeInTheDocument();

    const row = projectRow();
    const titleCell = within(row).getByText(PROJECT.title).closest('[role="cell"]');
    const typeCell = within(row).getByText("Website SEO").closest('[role="cell"]');

    expect(typeCell).toBeInTheDocument();
    expect(typeCell).not.toBe(titleCell);
  });

  it("sorts from the project type column header", async () => {
    const user = userEvent.setup();
    renderProjects();

    const typeHeader = screen.getByRole("columnheader", { name: "Project type" });
    await user.click(within(typeHeader).getByRole("button", { name: "Project type" }));

    expect(mocks.projectViewState.toggleSort).toHaveBeenCalledWith("type");
  });

  it("orders project types consistently and keeps unspecified projects last", () => {
    mocks.projects = [
      { ...PROJECT, id: "none", title: "No Type", project_type: null },
      { ...PROJECT, id: "transit", title: "Transit", project_type: "transit" },
      { ...PROJECT, id: "development", title: "Development", project_type: "development" },
      { ...PROJECT, id: "seo", title: "SEO", project_type: "seo" },
      { ...PROJECT, id: "support", title: "Support", project_type: "support" },
    ];
    mocks.projectViewState.sortField = "type";

    renderProjects();

    const projectNames = screen
      .getAllByRole("row")
      .slice(1)
      .map((row) => {
        const nameCell = within(row).getAllByRole("cell")[1]!;
        return within(nameCell).getByText(
          /^(Support|SEO|Development|Transit|No Type)$/,
        ).textContent;
      });

    expect(projectNames).toEqual([
      "Support",
      "SEO",
      "Development",
      "Transit",
      "No Type",
    ]);
  });

  it("includes project type in the column picker", async () => {
    const user = userEvent.setup();
    renderProjects();

    const typeLabel = screen
      .getAllByText("Project type", { selector: "span" })
      .find((node) => node.closest("label"));
    const pickerRow = typeLabel?.closest("label");
    expect(pickerRow).not.toBeNull();

    const typeSwitch = within(pickerRow as HTMLLabelElement).getByRole("switch");
    expect(typeSwitch).toBeChecked();

    await user.click(typeSwitch);

    expect(mocks.projectViewState.toggleColumn).toHaveBeenCalledWith("type");
  });

  it("renders and sorts the client column", async () => {
    const user = userEvent.setup();
    renderProjects();

    const clientHeader = screen.getByRole("columnheader", { name: "Client" });
    const clientCell = within(projectRow())
      .getByText("Acme Clinic")
      .closest('[role="cell"]');

    expect(clientCell).toBeInTheDocument();
    await user.click(within(clientHeader).getByRole("button", { name: "Client" }));
    expect(mocks.projectViewState.toggleSort).toHaveBeenCalledWith("client");
  });

  it("orders clients alphabetically and keeps unassigned projects last", () => {
    mocks.projects = [
      { ...PROJECT, id: "none", title: "No Client", client_id: null, client_name: null },
      { ...PROJECT, id: "beta", title: "Beta Project", client_id: "beta", client_name: "Beta" },
      { ...PROJECT, id: "alpha", title: "Alpha Project", client_id: "alpha", client_name: "Alpha" },
    ];
    mocks.projectViewState.sortField = "client";

    renderProjects();

    const projectNames = screen
      .getAllByRole("row")
      .slice(1)
      .map((row) =>
        within(within(row).getAllByRole("cell")[1]!)
          .getByText(/^(Alpha Project|Beta Project|No Client)$/)
          .textContent,
      );

    expect(projectNames).toEqual(["Alpha Project", "Beta Project", "No Client"]);
  });

  it("includes client in the column picker", async () => {
    const user = userEvent.setup();
    renderProjects();

    const clientLabel = screen
      .getAllByText("Client", { selector: "span" })
      .find((node) => node.closest("label"));
    const pickerRow = clientLabel?.closest("label");
    expect(pickerRow).not.toBeNull();

    await user.click(within(pickerRow as HTMLLabelElement).getByRole("switch"));

    expect(mocks.projectViewState.toggleColumn).toHaveBeenCalledWith("client");
  });

  it("includes clients in the filter menu", async () => {
    const user = userEvent.setup();
    renderProjects();

    const clientFilterLabel = screen
      .getAllByText("Acme Clinic", { exact: true })
      .find((node) => node.closest("button"));
    expect(clientFilterLabel).toBeDefined();

    await user.click(clientFilterLabel!.closest("button")!);

    expect(mocks.projectViewState.toggleFilter).toHaveBeenCalledWith(
      "clients",
      "client-1",
    );
  });

  it("filters projects by selected clients", () => {
    mocks.projects = [
      PROJECT,
      {
        ...PROJECT,
        id: "other-project",
        title: "Other Project",
        client_id: "client-2",
        client_name: "Other Client",
      },
    ];
    mocks.projectViewState.filters.clients = ["client-1"];

    renderProjects();

    expect(screen.getByText(PROJECT.title)).toBeInTheDocument();
    expect(screen.queryByText("Other Project")).not.toBeInTheDocument();
  });

  it("includes project types in the filter menu", async () => {
    const user = userEvent.setup();
    renderProjects();

    const seoFilterLabel = screen
      .getAllByText("Website SEO", { exact: true })
      .find((node) => node.closest("button"));
    expect(seoFilterLabel).toBeDefined();

    await user.click(seoFilterLabel!.closest("button")!);

    expect(mocks.projectViewState.toggleFilter).toHaveBeenCalledWith(
      "types",
      "seo",
    );
  });

  it("filters projects by selected project types", () => {
    mocks.projects = [
      PROJECT,
      {
        ...PROJECT,
        id: "support-project",
        title: "Support Project",
        project_type: "support",
      },
    ];
    mocks.projectViewState.filters.types = ["seo"];

    renderProjects();

    expect(screen.getByText(PROJECT.title)).toBeInTheDocument();
    expect(screen.queryByText("Support Project")).not.toBeInTheDocument();
  });

  it("renders the project name as text, not a title link", () => {
    renderProjects();

    const row = projectRow();
    expect(within(row).getByText(PROJECT.title).tagName).toBe("SPAN");
    expect(
      within(row).queryByRole("link", { name: PROJECT.title }),
    ).not.toBeInTheDocument();
  });

  it("navigates from the row surface", async () => {
    const user = userEvent.setup();
    const push = vi.fn();
    renderProjects(makeAdapter({ push }));

    await user.click(projectRow());

    expect(push).toHaveBeenCalledWith("/test-workspace/projects/project-1");
    expect(push).toHaveBeenCalledTimes(1);
  });

  it("does not navigate when inline controls are clicked", async () => {
    const user = userEvent.setup();
    const push = vi.fn();
    renderProjects(makeAdapter({ push }));
    const row = projectRow();

    await user.click(within(row).getByRole("button", { pressed: false }));
    await user.click(within(row).getByRole("button", { name: "Project actions" }));
    await user.click(within(row).getAllByRole("button", { name: "In Progress" })[0]!);
    await user.click(within(row).getAllByRole("button", { name: "High" })[0]!);
    await user.click(within(row).getByRole("button", { name: "—" }));

    expect(push).not.toHaveBeenCalled();
  });

  it("uses the rowLink modifier and middle-click paths when openInNewTab is available", () => {
    const push = vi.fn();
    const openInNewTab = vi.fn();
    renderProjects(makeAdapter({ push, openInNewTab }));
    const row = projectRow();

    fireEvent.click(row, { metaKey: true });
    fireEvent.click(row, { ctrlKey: true });
    const middleClick = new MouseEvent("auxclick", {
      bubbles: true,
      button: 1,
      cancelable: true,
    });
    row.dispatchEvent(middleClick);

    expect(middleClick.defaultPrevented).toBe(true);
    expect(openInNewTab).toHaveBeenCalledTimes(3);
    expect(openInNewTab).toHaveBeenNthCalledWith(1, "/test-workspace/projects/project-1");
    expect(openInNewTab).toHaveBeenNthCalledWith(2, "/test-workspace/projects/project-1");
    expect(openInNewTab).toHaveBeenNthCalledWith(3, "/test-workspace/projects/project-1");
    expect(push).not.toHaveBeenCalled();
  });

  // Web (no adapter): the row is a <div>, so nothing native catches a
  // modifier or middle click — rowLink opens the browser tab itself instead
  // of navigating in place (MUL-5456).
  it("has a single rowLink path for modifier and middle clicks without openInNewTab", () => {
    const push = vi.fn();
    const open = vi.spyOn(window, "open").mockReturnValue(null);
    renderProjects(makeAdapter({ push }));
    const row = projectRow();

    fireEvent.click(row, { metaKey: true });
    fireEvent.click(row, { ctrlKey: true });
    const middleClick = new MouseEvent("auxclick", {
      bubbles: true,
      button: 1,
      cancelable: true,
    });
    row.dispatchEvent(middleClick);

    expect(middleClick.defaultPrevented).toBe(true);
    expect(open).toHaveBeenCalledTimes(3);
    for (const nth of [1, 2, 3]) {
      expect(open).toHaveBeenNthCalledWith(
        nth,
        "/test-workspace/projects/project-1",
        "_blank",
        "noopener,noreferrer",
      );
    }
    expect(push).not.toHaveBeenCalled();
    open.mockRestore();
  });
});
