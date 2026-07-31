"use client";

// Project access for a business worker, shown on the Сотрудники table.
//
// The binding itself lives on the workspace MEMBER (member_project +
// member.access_scope), not in the business module: access control must not
// sit behind the finance gate, not every member is a worker (guests, service
// accounts), and not every worker is a member (a contractor paid through Elba
// who never signs in). This cell is a second window onto the member data, so
// the operator can manage a person from one screen — it writes to the member
// endpoints, exactly like the members tab does.
//
// business_worker.user_id is what makes the join possible; workers without it
// are simply not connected to anyone here.

import { useState } from "react";
import { useQuery, useQueryClient } from "@tanstack/react-query";
import { FolderGit2, Check } from "lucide-react";
import { toast } from "sonner";
import { Button } from "@multica/ui/components/ui/button";
import { Badge } from "@multica/ui/components/ui/badge";
import { Checkbox } from "@multica/ui/components/ui/checkbox";
import {
  Popover,
  PopoverTrigger,
  PopoverContent,
} from "@multica/ui/components/ui/popover";
import {
  AlertDialog,
  AlertDialogContent,
  AlertDialogHeader,
  AlertDialogTitle,
  AlertDialogDescription,
  AlertDialogFooter,
  AlertDialogCancel,
  AlertDialogAction,
} from "@multica/ui/components/ui/alert-dialog";
import { api, ApiError } from "@multica/core/api";
import { useWorkspaceId } from "@multica/core/hooks";
import { memberListOptions, workspaceKeys } from "@multica/core/workspace/queries";
import { projectListOptions } from "@multica/core/projects/queries";
import { issueKeys } from "@multica/core/issues/queries";
import { useT } from "../i18n";

export function WorkerAccessCell({ userID }: { userID: string | null }) {
  const { t } = useT("settings");
  const wsId = useWorkspaceId();
  const qc = useQueryClient();
  const [busy, setBusy] = useState(false);
  const [handoff, setHandoff] = useState<{ projectId: string; assignedIssues: number } | null>(null);

  const { data: members = [] } = useQuery(memberListOptions(wsId));
  const { data: projects = [] } = useQuery(projectListOptions(wsId));

  const member = userID ? members.find((m) => m.user_id === userID) : undefined;

  if (!userID) {
    return <span className="text-[11px] text-muted-foreground">{t(($) => $.members.access_worker_unlinked)}</span>;
  }
  if (!member) {
    return <span className="text-[11px] text-muted-foreground">{t(($) => $.members.access_worker_not_member)}</span>;
  }

  const isOwner = member.role === "owner";
  const scoped = member.access_scope === "projects";
  const boundIds = new Set(member.project_ids ?? []);

  const refresh = () => {
    qc.invalidateQueries({ queryKey: workspaceKeys.members(wsId) });
    qc.invalidateQueries({ queryKey: issueKeys.all(wsId) });
  };

  const run = async (fn: () => Promise<unknown>, onConflict?: (assigned: number) => void) => {
    setBusy(true);
    try {
      await fn();
      refresh();
    } catch (e) {
      const assigned =
        e instanceof ApiError && e.status === 409
          ? Number((e.body as { assigned_issues?: number } | undefined)?.assigned_issues ?? 0)
          : 0;
      if (assigned > 0 && onConflict) {
        onConflict(assigned);
        return;
      }
      toast.error(e instanceof Error ? e.message : t(($) => $.members.toast_access_failed));
    } finally {
      setBusy(false);
    }
  };

  const toggleProject = (projectId: string, bind: boolean) => {
    if (bind) {
      void run(() => api.setMemberProject(wsId, member.id, projectId));
      return;
    }
    void run(
      () => api.unsetMemberProject(wsId, member.id, projectId),
      (assigned) => setHandoff({ projectId, assignedIssues: assigned }),
    );
  };

  return (
    <div className="flex items-center gap-1.5">
      <Badge variant={scoped ? "outline" : "secondary"} className="gap-1 text-[11px] font-normal">
        {scoped ? <FolderGit2 className="h-3 w-3" /> : null}
        {scoped
          ? t(($) => $.members.access_n_projects, { count: boundIds.size })
          : t(($) => $.members.access_whole_workspace)}
      </Badge>

      {!isOwner && (
        <Popover>
          <PopoverTrigger
            render={
              <Button variant="ghost" size="sm" className="h-6 px-2 text-[11px]" disabled={busy}>
                {t(($) => $.members.access_configure)}
              </Button>
            }
          />
          <PopoverContent align="start" className="w-64 p-2">
            <Button
              variant="ghost"
              size="sm"
              className="mb-1 h-7 w-full justify-start text-xs"
              disabled={busy}
              onClick={() =>
                void run(() =>
                  api.setMemberAccessScope(wsId, member.id, scoped ? "workspace" : "projects"),
                )
              }
            >
              {scoped
                ? t(($) => $.members.access_open_workspace)
                : t(($) => $.members.access_limit_projects)}
            </Button>

            {scoped && (
              <div className="max-h-56 overflow-y-auto">
                {projects.map((p) => {
                  const bound = boundIds.has(p.id);
                  return (
                    <label
                      key={p.id}
                      className="flex cursor-pointer items-center gap-2 rounded px-1.5 py-1 text-xs hover:bg-accent/70"
                    >
                      <Checkbox
                        checked={bound}
                        disabled={busy}
                        onCheckedChange={() => toggleProject(p.id, !bound)}
                      />
                      <span className="truncate">{p.title}</span>
                      {bound && <Check className="ml-auto h-3 w-3 text-muted-foreground" />}
                    </label>
                  );
                })}
                {projects.length === 0 && (
                  <div className="px-1.5 py-2 text-xs text-muted-foreground">
                    {t(($) => $.members.guest_no_project)}
                  </div>
                )}
              </div>
            )}
          </PopoverContent>
        </Popover>
      )}

      {/* Same rule as the members tab: work assigned in the project being
          revoked has to go somewhere, and "nowhere" is a choice that has to be
          made rather than defaulted into. Successor picking lives in the
          members tab; here the answer is the simple one. */}
      <AlertDialog open={!!handoff} onOpenChange={(v) => { if (!v) setHandoff(null); }}>
        <AlertDialogContent>
          <AlertDialogHeader>
            <AlertDialogTitle>{t(($) => $.members.handoff_title)}</AlertDialogTitle>
            <AlertDialogDescription>
              {t(($) => $.members.handoff_description, { count: handoff?.assignedIssues ?? 0 })}
            </AlertDialogDescription>
          </AlertDialogHeader>
          <AlertDialogFooter>
            <AlertDialogCancel>{t(($) => $.members.confirm_cancel)}</AlertDialogCancel>
            <AlertDialogAction
              onClick={async () => {
                if (!handoff) return;
                await run(() =>
                  api.unsetMemberProject(wsId, member.id, handoff.projectId, {
                    onAssignedIssues: "unassign",
                  }),
                );
                setHandoff(null);
              }}
            >
              {t(($) => $.members.handoff_unassigned)}
            </AlertDialogAction>
          </AlertDialogFooter>
        </AlertDialogContent>
      </AlertDialog>
    </div>
  );
}
