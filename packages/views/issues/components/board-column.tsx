"use client";

import { memo, useCallback, useMemo, useState, type ReactNode } from "react";
import { Virtuoso } from "react-virtuoso";
import {
  ChevronsLeftRight,
  ChevronsRightLeft,
  EyeOff,
  MoreHorizontal,
  Plus,
  UserMinus,
} from "lucide-react";
import { useDroppable } from "@dnd-kit/core";
import { SortableContext, verticalListSortingStrategy } from "@dnd-kit/sortable";
import type {
  Issue,
  IssueAssigneeType,
  IssueStatus,
  Project,
} from "@multica/core/types";
import { Button } from "@multica/ui/components/ui/button";
import {
  DropdownMenu,
  DropdownMenuTrigger,
  DropdownMenuContent,
  DropdownMenuItem,
} from "@multica/ui/components/ui/dropdown-menu";
import { STATUS_CONFIG } from "@multica/core/issues/config";
import {
  useViewStore,
  useViewStoreApi,
} from "@multica/core/issues/stores/view-store-context";
import { StatusHeading } from "./status-heading";
import { StatusIcon } from "./status-icon";
import { DraggableBoardCard } from "./board-card";
import type { ChildProgress } from "./list-row";
import { useT } from "../../i18n";
import { ActorAvatar } from "../../common/actor-avatar";
import { useRestoredScrollOffset, useRestoredScrollRef } from "../../platform";
import { DeferredPopup } from "../../common/deferred-popup";
import { DeferredTooltip } from "../../common/deferred-tooltip";
import { VirtuosoSeed } from "../../common/virtuoso-seed";
import type { IssueCreateDefaults } from "../surface/types";

// Insertion-position prediction intentionally omitted. The server's
// ORDER BY uses PostgreSQL's en_US.utf8 collation (glibc), which
// cannot be faithfully replicated in JavaScript (ICU/V8). Showing an
// inaccurate indicator is worse than showing none.

export const BOARD_COL_WIDTH = 360;
export const BOARD_CARD_WIDTH = BOARD_COL_WIDTH - 16 - 8; // col - col p-2(16) - droppable p-1(8)

// Collapsed rail: wide enough for the 28px round expand hit target plus the
// column's p-2, and narrow enough that a few collapsed columns cost about as
// much horizontal room as one open one.
export const BOARD_COL_COLLAPSED_WIDTH = 44;

// Board cards are ~90-140px tall, so ~10 fill a column viewport — unlike the
// generic VIRTUOSO_SEED_COUNT (30, sized for 36px list rows). The seed mounts
// synchronously per column on every surface remount, so oversizing it
// multiplies straight into tab-switch cost (columns × seed × per-card mount).
const BOARD_SEED_COUNT = 10;

// Median card height (incl. the 8px pt-2 gap) for pre-measurement scroll
// sizing only: the seed's trailing spacer and Virtuoso's defaultItemHeight
// share it so total scroll height — and the scrollbar thumb — stays steady
// across the seed → Virtuoso handoff instead of jumping when the unseeded
// rows suddenly get spaced out. Real measurements refine it afterwards.
const BOARD_CARD_ESTIMATED_HEIGHT = 110;

// Columns at or below this render every card plainly — no Virtuoso, no seed
// handoff, no estimated heights. Scroll height is then always browser-measured
// truth, so the column's scrollbar can never jump on mount, route return, or
// tab restore. Virtualization only pays for itself on large columns (Linear
// does the same per-column split: `data-virtual-cluster="false"` for small
// columns, padding-spacer virtualization for big ones). ~30 ≈ 3 viewports of
// cards; a full plain mount at that size is cheap now that per-card popups
// mount lazily. Known edge: a load-more append that crosses the threshold
// swaps the column to the virtualized path, which may adjust the scrollbar
// once — accepted, since it only happens mid-scroll at the column bottom.
const BOARD_VIRTUALIZE_THRESHOLD = 30;

// Passed to <Virtuoso components> when the column has no footer. Must be a
// STABLE object, never `undefined`: an explicit `undefined` prop overwrites
// react-virtuoso's internal `{}` default and its startup destructure of
// `EmptyPlaceholder`/`Footer` throws (MUL-4474).
const EMPTY_VIRTUOSO_COMPONENTS = {};

/** What one column's money line shows. Server-computed; see
 *  `ListIssueCostTotals` for why the price cannot be summed client-side. */
export interface BoardColumnCost {
  tokens: number;
  price_rub: number;
}

/**
 * Server group keys a board column maps onto, in the key space of
 * `POST /api/issues/table/cost-totals`.
 *
 * A property column can map onto several: the board keeps one "No value"
 * column while the server splits it into `unset:` (property never set) plus an
 * `unavailable:<raw>` per value whose option was deleted.
 */
function boardGroupCostKeys(group: BoardColumnGroup): {
  exact: string[];
  prefixes: string[];
} {
  if (group.status) return { exact: [group.status], prefixes: [] };
  if (group.propertyId !== undefined) {
    if (group.propertyOptionId) {
      return { exact: [`value:${group.propertyOptionId}`], prefixes: [] };
    }
    return { exact: [], prefixes: ["unset:", "unavailable:"] };
  }
  if (group.assigneeType && group.assigneeId) {
    return { exact: [`${group.assigneeType}:${group.assigneeId}`], prefixes: [] };
  }
  return { exact: ["__unassigned__"], prefixes: [] };
}

/** This column's slice of the cost response, or undefined when the viewer is
 *  not the owner (no totals at all) or nothing in the column is billable. */
export function boardGroupCost(
  totals: Map<string, BoardColumnCost> | undefined,
  group: BoardColumnGroup,
): BoardColumnCost | undefined {
  if (!totals || totals.size === 0) return undefined;
  const { exact, prefixes } = boardGroupCostKeys(group);
  let tokens = 0;
  let priceRub = 0;
  let matched = false;
  const take = (entry: BoardColumnCost | undefined) => {
    if (!entry) return;
    tokens += entry.tokens;
    priceRub += entry.price_rub;
    matched = true;
  };
  for (const key of exact) take(totals.get(key));
  if (prefixes.length > 0) {
    for (const [key, entry] of totals) {
      if (prefixes.some((prefix) => key.startsWith(prefix))) take(entry);
    }
  }
  return matched ? { tokens, price_rub: priceRub } : undefined;
}

export interface BoardColumnGroup {
  id: string;
  title: string;
  status?: IssueStatus;
  assigneeType?: IssueAssigneeType | null;
  assigneeId?: string | null;
  /** Set when the board is grouped by a select-type custom property. */
  propertyId?: string;
  /** Option id for this column; null = the "No value" column. */
  propertyOptionId?: string | null;
  propertyOptionColor?: string;
  totalCount?: number;
  createData?: IssueCreateDefaults;
}

export const BoardColumn = memo(function BoardColumn({
  group,
  issueIds,
  issueMap,
  childProgressMap,
  projectMap,
  totalCount,
  cost,
  footer,
  projectId,
  onCreateIssue,
  sortLabel,
}: {
  group: BoardColumnGroup;
  issueIds: string[];
  issueMap: Map<string, Issue>;
  childProgressMap?: Map<string, ChildProgress>;
  projectMap?: Map<string, Project>;
  totalCount?: number;
  /** Owner-only client price for this column; absent for everyone else. */
  cost?: BoardColumnCost;
  footer?: ReactNode;
  /** When set, the per-column "+" pre-fills the project on the create form. */
  projectId?: string;
  onCreateIssue?: (defaults: IssueCreateDefaults) => void;
  sortLabel?: string | null;
}) {
  const status = group.status;
  const cfg = status ? STATUS_CONFIG[status] : null;
  const { setNodeRef, isOver } = useDroppable({ id: group.id });
  const viewStoreApi = useViewStoreApi();
  const { t } = useT("issues");
  const collapsed = useViewStore((s) =>
    s.boardCollapsedColumns.includes(group.id),
  );
  const toggleCollapsed = useCallback(
    () => viewStoreApi.getState().toggleBoardColumnCollapsed(group.id),
    [viewStoreApi, group.id],
  );

  // Resolve IDs to Issue objects, preserving parent-provided order
  const resolvedIssues = useMemo(
    () =>
      issueIds.flatMap((id) => {
        const issue = issueMap.get(id);
        return issue ? [issue] : [];
      }),
    [issueIds, issueMap],
  );

  // The column's scroll container is both dnd-kit's droppable and Virtuoso's
  // customScrollParent, so a merged callback ref feeds the element to both.
  // useDroppable's setNodeRef is stable across renders. Keeping the droppable
  // on the always-mounted scroll container (not on individual cards) is what
  // lets cross-column drops survive virtualization — only the cards inside
  // window in/out of the DOM.
  const [scrollEl, setScrollEl] = useState<HTMLDivElement | null>(null);
  // Pull-based scroll restoration (MUL-4741): assign the saved offset at
  // ref-attach (the seed + estimate spacer give the column a truthful height
  // on its first commit, so the assignment sticks pre-paint) and feed the
  // same offset into the Virtuoso as its initial position.
  const scrollMementoKey = `board:${group.id}`;
  const restoredScrollTop = useRestoredScrollOffset(scrollMementoKey);
  const restoreScrollRef = useRestoredScrollRef(scrollMementoKey);
  const mergedRef = useCallback(
    (el: HTMLDivElement | null) => {
      setNodeRef(el);
      setScrollEl(el);
      restoreScrollRef(el);
    },
    [setNodeRef, restoreScrollRef],
  );
  // Infinite-scroll sentinel rides Virtuoso's Footer slot so it sits at the
  // real end of the virtualized list and its IntersectionObserver still fires
  // loadMore when scrolled to the bottom.
  const footerComponents = useMemo(
    () => (footer ? { Footer: () => <>{footer}</> } : EMPTY_VIRTUOSO_COMPONENTS),
    [footer],
  );

  const computeItemKey = (_index: number, issue: Issue) => issue.id;
  const itemContent = (index: number, issue: Issue) => (
    // pt-2 on every card but the first reproduces the previous `space-y-2`
    // gap; padding (not margin) is inside Virtuoso's measured item box so its
    // height math stays correct.
    <div className={index === 0 ? undefined : "pt-2"}>
      <DraggableBoardCard
        issue={issue}
        childProgress={childProgressMap?.get(issue.id)}
        project={
          issue.project_id ? projectMap?.get(issue.project_id) : undefined
        }
        disableSorting={!!sortLabel}
      />
    </div>
  );

  // Collapsed: the whole rail is the expand control. The droppable ref stays on
  // it (cards can still be dropped into a collapsed column), but nothing from
  // the list mounts — no Virtuoso, no seed, no per-card popups.
  if (collapsed) {
    return (
      <button
        type="button"
        ref={setNodeRef}
        onClick={toggleCollapsed}
        title={t(($) => $.board.expand_column)}
        aria-label={t(($) => $.board.expand_column)}
        style={{ width: BOARD_COL_COLLAPSED_WIDTH }}
        className={`flex shrink-0 flex-col items-center gap-2 rounded-xl p-2 transition-colors ${
          isOver ? "bg-accent/60" : cfg?.columnBg ?? "bg-muted/40"
        } hover:bg-accent/40`}
      >
        <span className="flex size-7 shrink-0 items-center justify-center rounded-full text-muted-foreground">
          <ChevronsLeftRight className="size-3.5" />
        </span>
        <BoardRailHeading group={group} count={totalCount ?? issueIds.length} />
      </button>
    );
  }

  return (
    <div style={{ width: BOARD_COL_WIDTH }} className={`flex shrink-0 flex-col rounded-xl ${cfg?.columnBg ?? "bg-muted/40"} p-2`}>
      <div className="mb-2 flex items-center justify-between px-1.5">
        <BoardGroupHeading group={group} count={totalCount ?? issueIds.length} />

        {/* Right: cost + collapse + add + menu */}
        <div className="flex items-center gap-1">
          <BoardColumnCostLine cost={cost} />
          {/* Column-header popups mount lazily: a board/swimlane renders one
              header per column and almost none of these menus/tooltips are
              ever opened — eagerly mounting them dominated surface mount
              cost (DeferredPopup / DeferredTooltip). */}
          <DeferredTooltip
            content={t(($) => $.board.collapse_column)}
            trigger={
              <Button
                variant="ghost"
                size="icon-sm"
                className="rounded-full text-muted-foreground"
                // No `aria-expanded`: the ghost variant pins
                // `aria-expanded:bg-muted` (so a button stays lit while its
                // popover is open), which would leave a permanent filled
                // circle behind this icon.
                aria-label={t(($) => $.board.collapse_column)}
                onClick={toggleCollapsed}
              >
                <ChevronsRightLeft className="size-3.5" />
              </Button>
            }
          />
          {status && (
            <DeferredPopup
              ariaHasPopup="menu"
              triggerRender={
                <Button variant="ghost" size="icon-sm" className="rounded-full text-muted-foreground">
                  <MoreHorizontal className="size-3.5" />
                </Button>
              }
            >
              {(open, onOpenChange) => (
                <DropdownMenu open={open} onOpenChange={onOpenChange}>
                  <DropdownMenuTrigger
                    render={
                      <Button variant="ghost" size="icon-sm" className="rounded-full text-muted-foreground">
                        <MoreHorizontal className="size-3.5" />
                      </Button>
                    }
                  />
                  <DropdownMenuContent align="end">
                    <DropdownMenuItem onClick={() => viewStoreApi.getState().hideStatus(status)}>
                      <EyeOff className="size-3.5" />
                      {t(($) => $.board.hide_column)}
                    </DropdownMenuItem>
                  </DropdownMenuContent>
                </DropdownMenu>
              )}
            </DeferredPopup>
          )}
          {onCreateIssue && (
            <DeferredTooltip
              content={t(($) => $.board.add_issue_tooltip)}
              trigger={
                <Button
                  variant="ghost"
                  size="icon-sm"
                  className="rounded-full text-muted-foreground"
                  onClick={() => {
                    const data = {
                      ...(group.createData ?? {}),
                      ...(projectId ? { project_id: projectId } : {}),
                    };
                    onCreateIssue(data);
                  }}
                >
                  <Plus className="size-3.5" />
                </Button>
              }
            />
          )}
        </div>
      </div>
      <div className="relative min-h-[200px] flex-1 rounded-lg">
        {isOver && sortLabel && (
          <div className="pointer-events-none absolute inset-0 z-10 flex items-center justify-center rounded-lg bg-background/40">
            <span className="rounded-md bg-popover px-2.5 py-1 text-xs font-medium text-popover-foreground shadow-sm border border-border">
              {sortLabel}
            </span>
          </div>
        )}
        <div
          ref={mergedRef}
          // Per-column scroll registration for the tab session memento
          // (MUL-4741): the group id is the stable memento key, so every
          // column's offset survives tab switches/reloads independently.
          data-tab-scroll-root={scrollMementoKey}
          className={`absolute inset-0 overflow-y-auto rounded-lg p-1 transition-colors ${
            isOver && sortLabel
              ? "ring-2 ring-brand/25 bg-accent/15"
              : isOver
                ? "bg-accent/60"
                : ""
          }`}
        >
          {resolvedIssues.length > 0 ? (
            <SortableContext items={issueIds} strategy={verticalListSortingStrategy}>
              {resolvedIssues.length <= BOARD_VIRTUALIZE_THRESHOLD ? (
                /* Small column: plain full render (reusing the same
                   itemContent, so it is byte-identical to the virtualized
                   rows). No handoff, no estimates — see
                   BOARD_VIRTUALIZE_THRESHOLD. The footer (infinite-scroll
                   sentinel) renders at the real end of the flow, where its
                   IntersectionObserver works the same as in Virtuoso's
                   Footer slot. */
                <>
                  <VirtuosoSeed
                    data={resolvedIssues}
                    itemContent={itemContent}
                    computeItemKey={computeItemKey}
                    count={resolvedIssues.length}
                  />
                  {footer}
                </>
              ) : scrollEl ? (
                <Virtuoso
                  customScrollParent={scrollEl}
                  data={resolvedIssues}
                  computeItemKey={computeItemKey}
                  initialScrollTop={restoredScrollTop}
                  initialItemCount={Math.min(resolvedIssues.length, BOARD_SEED_COUNT)}
                  defaultItemHeight={BOARD_CARD_ESTIMATED_HEIGHT}
                  increaseViewportBy={{ top: 300, bottom: 300 }}
                  components={footerComponents}
                  itemContent={itemContent}
                />
              ) : (
                /* Large column, merged scroll ref not settled yet after a
                   remount: seed a bounded slice of real cards so the column
                   never paints blank; once the ref lands, mount the Virtuoso
                   with a matching `initialItemCount` to survive the
                   measurement frame (MUL-4750). */
                <VirtuosoSeed
                  data={resolvedIssues}
                  itemContent={itemContent}
                  computeItemKey={computeItemKey}
                  count={BOARD_SEED_COUNT}
                  estimatedItemHeight={BOARD_CARD_ESTIMATED_HEIGHT}
                />
              )}
            </SortableContext>
          ) : (
            <>
              {issueIds.length === 0 && (
                <p className="py-8 text-center text-xs text-muted-foreground">
                  {t(($) => $.board.empty_column)}
                </p>
              )}
              {footer}
            </>
          )}
        </div>
      </div>
    </div>
  );
});

/**
 * The column's client price, tokens behind a tooltip.
 *
 * Rounded to whole roubles on purpose: prices are already rounded to a 50 ₽
 * step per issue, so the kopecks would always be ",00" and only cost header
 * width. Nothing renders at zero — an empty column would otherwise grow a
 * meaningless "0 ₽", and a column whose issues are all internal work reads as
 * "nothing to bill here", which is true.
 */
function BoardColumnCostLine({ cost }: { cost?: BoardColumnCost }) {
  const { t } = useT("issues");
  if (!cost || cost.price_rub <= 0) return null;
  return (
    <DeferredTooltip
      // `count` drives plural selection, `formatted` carries the grouped
      // digits: i18next interpolates numbers verbatim, and a raw 19899582
      // is unreadable.
      content={t(($) => $.board.cost_tokens, {
        count: cost.tokens,
        formatted: cost.tokens.toLocaleString("ru-RU"),
      })}
      trigger={
        <span className="cursor-default rounded px-1 text-[11px] font-medium tabular-nums text-muted-foreground">
          {cost.price_rub.toLocaleString("ru-RU", { maximumFractionDigits: 0 })} ₽
        </span>
      }
    />
  );
}

/**
 * Heading for a collapsed column: same identity as {@link BoardGroupHeading}
 * (icon + count + name) stacked down the rail, with the name rotated so a full
 * status/assignee/option label stays readable at 44px wide.
 */
function BoardRailHeading({
  group,
  count,
}: {
  group: BoardColumnGroup;
  count: number;
}) {
  const { t } = useT("issues");
  const status = group.status;
  const title = status ? t(($) => $.status[status]) : group.title;

  return (
    <>
      {status ? (
        <StatusIcon status={status} className="h-3.5 w-3.5 shrink-0" />
      ) : group.propertyId !== undefined ? (
        <span
          className="size-2.5 shrink-0 rounded-full bg-muted-foreground/30"
          style={group.propertyOptionColor ? { backgroundColor: group.propertyOptionColor } : undefined}
        />
      ) : group.assigneeType && group.assigneeId ? (
        <ActorAvatar
          actorType={group.assigneeType}
          actorId={group.assigneeId}
          size="sm"
          showStatusDot={group.assigneeType === "agent"}
        />
      ) : (
        <span className="flex size-[18px] shrink-0 items-center justify-center rounded-full bg-background text-muted-foreground">
          <UserMinus className="size-3.5" />
        </span>
      )}
      <span className="shrink-0 rounded-full bg-background px-1.5 py-0.5 text-[11px] font-medium tabular-nums text-muted-foreground">
        {count}
      </span>
      {/* Rotated with writing-mode (not `rotate-90`, which would keep the
          element's horizontal box and overflow the rail). Clips with an
          ellipsis at the bottom when the label is longer than the column. */}
      {/* `text-start` because the rail is a <button>, whose UA `text-align:
          center` would otherwise center the label along the rail's vertical
          axis instead of anchoring it under the count. */}
      <span className="min-h-0 flex-1 overflow-hidden text-ellipsis whitespace-nowrap text-start text-xs font-semibold [writing-mode:vertical-rl]">
        {title}
      </span>
    </>
  );
}

function BoardGroupHeading({
  group,
  count,
}: {
  group: BoardColumnGroup;
  count: number;
}) {
  if (group.status) {
    return <StatusHeading status={group.status} count={count} />;
  }

  if (group.propertyId !== undefined) {
    return (
      <div className="flex min-w-0 items-center gap-2">
        <span
          className="size-2.5 shrink-0 rounded-full bg-muted-foreground/30"
          style={group.propertyOptionColor ? { backgroundColor: group.propertyOptionColor } : undefined}
        />
        <span className="truncate text-sm font-medium" title={group.title}>
          {group.title}
        </span>
        <span className="shrink-0 rounded-full bg-background px-1.5 py-0.5 text-[11px] font-medium tabular-nums text-muted-foreground">
          {count}
        </span>
      </div>
    );
  }

  const actorIcon =
    group.assigneeType && group.assigneeId ? (
      <ActorAvatar
        actorType={group.assigneeType}
        actorId={group.assigneeId}
        size="sm"
        showStatusDot={group.assigneeType === "agent"}
      />
    ) : (
      <span className="flex size-[18px] shrink-0 items-center justify-center rounded-full bg-background text-muted-foreground">
        <UserMinus className="size-3.5" />
      </span>
    );

  return (
    <div className="flex min-w-0 items-center gap-2">
      {actorIcon}
      <span className="truncate text-sm font-medium" title={group.title}>
        {group.title}
      </span>
      <span className="shrink-0 rounded-full bg-background px-1.5 py-0.5 text-[11px] font-medium tabular-nums text-muted-foreground">
        {count}
      </span>
    </div>
  );
}
