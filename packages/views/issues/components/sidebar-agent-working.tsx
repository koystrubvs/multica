"use client";

import { memo, useEffect, useMemo, useState } from "react";
import { useQuery } from "@tanstack/react-query";
import { Clock, Loader2 } from "lucide-react";
import { useWorkspaceId } from "@multica/core/hooks";
import { agentTaskSnapshotOptions } from "@multica/core/agents";
import type { AgentTask } from "@multica/core/types";
import { useActorName } from "@multica/core/workspace/hooks";
import { ActorAvatar } from "../../common/actor-avatar";
import { AgentAvatarStack } from "../../agents/components/agent-avatar-stack";
import { useT } from "../../i18n";

// SidebarAgentWorking — a prominent, always-expanded live banner pinned at
// the very top of the issue's right-hand properties panel. It answers, at
// a glance and without scrolling, "is an agent working on THIS issue right
// now, and which one?".
//
// The ExecutionLogSection below it carries the full run list (active +
// past, transcripts, cancel/retry). That section is information-dense and
// reads quietly — the running state is a tiny inline spinner next to the
// trigger text, with the agent shown only as an avatar. This card is the
// loud counterpart: big avatar, the agent's NAME, an animated "working"
// line, and a ticking elapsed clock. It renders nothing when no task is
// active, so it never occupies space on idle issues.
//
// Data source is the workspace-wide agent task snapshot (the same cache
// that powers the board card indicator and the header chip), filtered to
// this issue. It's driven live by WS task:* events via useRealtimeSync —
// no local subscriptions, no extra fetch (the query is already warm from
// the board), 30s staleTime is the offline fallback only.

function formatElapsed(startedAt: string): string {
  const elapsed = Date.now() - new Date(startedAt).getTime();
  const seconds = Math.floor(elapsed / 1000);
  if (seconds < 60) return `${seconds}s`;
  const minutes = Math.floor(seconds / 60);
  const secs = seconds % 60;
  return `${minutes}m ${secs}s`;
}

// Ticking elapsed label — anchored on started_at (running) and falling
// back through dispatched_at → created_at so a queued/parked task still
// shows how long it has been waiting.
function useElapsed(task: AgentTask): string {
  const [elapsed, setElapsed] = useState("");
  const startRef = task.started_at ?? task.dispatched_at ?? task.created_at;
  useEffect(() => {
    if (!startRef) return;
    setElapsed(formatElapsed(startRef));
    const interval = setInterval(() => setElapsed(formatElapsed(startRef)), 1000);
    return () => clearInterval(interval);
  }, [startRef]);
  return elapsed;
}

interface SidebarAgentWorkingProps {
  issueId: string;
}

export const SidebarAgentWorking = memo(function SidebarAgentWorking({
  issueId,
}: SidebarAgentWorkingProps) {
  const { getActorName } = useActorName();
  const wsId = useWorkspaceId();
  const { data: snapshot = [] } = useQuery(agentTaskSnapshotOptions(wsId));

  // Split this issue's non-terminal tasks into running vs parked (queued /
  // dispatched / waiting on a local_directory lock). Running takes the
  // card; if nothing is running we still surface the parked agent so the
  // banner doesn't blink out while a task is waiting its turn.
  const { running, parked } = useMemo(() => {
    const run: AgentTask[] = [];
    const park: AgentTask[] = [];
    for (const task of snapshot) {
      if (task.issue_id !== issueId) continue;
      if (task.status === "running") run.push(task);
      else if (
        task.status === "queued" ||
        task.status === "dispatched" ||
        task.status === "waiting_local_directory"
      )
        park.push(task);
    }
    return { running: run, parked: park };
  }, [snapshot, issueId]);

  const active = running.length > 0 ? running : parked;
  const agentIds = useMemo(
    () => [...new Set(active.map((tk) => tk.agent_id).filter((id): id is string => !!id))],
    [active],
  );

  if (active.length === 0) return null;

  const isRunning = running.length > 0;
  const multi = agentIds.length > 1;

  return (
    <div data-sp-feature="sidebar-agent-working" className="rounded-lg border border-info/20 bg-info/5 px-3 py-2.5">
      {multi ? (
        <MultiBanner agentIds={agentIds} count={agentIds.length} isRunning={isRunning} />
      ) : (
        <SingleBanner task={active[0]!} getActorName={getActorName} />
      )}
    </div>
  );
});

// One agent — big avatar with status dot, the agent's name on the
// "{name} is working" line, and a ticking clock under it.
function SingleBanner({
  task,
  getActorName,
}: {
  task: AgentTask;
  getActorName: (type: string, id: string) => string;
}) {
  const { t } = useT("issues");
  const elapsed = useElapsed(task);
  const name = task.agent_id
    ? getActorName("agent", task.agent_id)
    : t(($) => $.agent_live.fallback_name);

  const isWaiting = task.status === "waiting_local_directory";
  const isQueued = task.status === "queued" || task.status === "dispatched";
  const parked = isWaiting || isQueued;

  const label = isWaiting
    ? t(($) => $.agent_live.is_waiting_local_directory, { name })
    : isQueued
      ? t(($) => $.agent_live.is_queued, { name })
      : t(($) => $.agent_live.is_working, { name });

  return (
    <div className="flex items-center gap-2.5">
      {task.agent_id ? (
        <ActorAvatar
          actorType="agent"
          actorId={task.agent_id}
          size={28}
          enableHoverCard
          showStatusDot
        />
      ) : (
        <span className="h-7 w-7 shrink-0 rounded-full bg-info/10" />
      )}
      <div className="min-w-0 flex-1">
        <div className="flex items-center gap-1.5">
          {parked ? (
            <Clock className="h-3 w-3 shrink-0 text-muted-foreground" />
          ) : (
            <Loader2 className="h-3 w-3 shrink-0 animate-spin text-info" />
          )}
          <span className="truncate text-xs font-medium text-foreground">{label}</span>
        </div>
        <div className="mt-0.5 text-[11px] tabular-nums text-muted-foreground">
          {parked ? t(($) => $.agent_live.queued_elapsed_prefix, { elapsed }) : elapsed}
        </div>
      </div>
    </div>
  );
}

// Several agents on one issue (squad / parallel @-mentions) — avatar
// stack + the pluralised "N agents working" header.
function MultiBanner({
  agentIds,
  count,
  isRunning,
}: {
  agentIds: string[];
  count: number;
  isRunning: boolean;
}) {
  const { t } = useT("issues");
  return (
    <div className="flex items-center gap-2.5">
      <AgentAvatarStack agentIds={agentIds} size={28} max={4} />
      <div className="flex min-w-0 flex-1 items-center gap-1.5">
        {isRunning ? (
          <Loader2 className="h-3 w-3 shrink-0 animate-spin text-info" />
        ) : (
          <Clock className="h-3 w-3 shrink-0 text-muted-foreground" />
        )}
        <span className="truncate text-xs font-medium text-foreground">
          {t(($) => $.agent_activity.hover_header, { count })}
        </span>
      </div>
    </div>
  );
}
