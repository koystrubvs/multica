"use client";

import { useState } from "react";
import { useQuery } from "@tanstack/react-query";
import { toast } from "sonner";
import { ApiError } from "@multica/core/api";
import { useWorkspaceId } from "@multica/core/hooks";
import {
  projectMembersOptions,
  useBindProjectMember,
  useUnbindProjectMember,
} from "@multica/core/projects";
import type { ProjectMemberAccess } from "@multica/core/types";
import { useT } from "../../i18n";
import { Button } from "@multica/ui/components/ui/button";
import { Switch } from "@multica/ui/components/ui/switch";
import {
  Dialog,
  DialogContent,
  DialogDescription,
  DialogFooter,
  DialogHeader,
  DialogTitle,
} from "@multica/ui/components/ui/dialog";
import {
  Select,
  SelectContent,
  SelectItem,
  SelectTrigger,
  SelectValue,
} from "@multica/ui/components/ui/select";

/** The 409 the server answers when work is assigned to the person being
 *  revoked. It is not an error to swallow — it is the question the screen has
 *  to ask before anything changes. */
interface AssignedWorkConflict {
  assigned_issues?: number;
}

function assignedIssuesFromError(error: unknown): number | null {
  if (!(error instanceof ApiError) || error.status !== 409) return null;
  const body = error.body as AssignedWorkConflict | undefined;
  return typeof body?.assigned_issues === "number" ? body.assigned_issues : 0;
}

interface Props {
  projectId: string;
}

export function ProjectAccessSection({ projectId }: Props) {
  const { t } = useT("projects");
  const wsId = useWorkspaceId();
  const { data: members = [], isLoading } = useQuery(
    projectMembersOptions(wsId, projectId),
  );
  const bind = useBindProjectMember(wsId, projectId);
  const unbind = useUnbindProjectMember(wsId, projectId);

  // Set while the server is asking what to do with work assigned in this
  // project. Null means no question is pending.
  const [pending, setPending] = useState<{
    member: ProjectMemberAccess;
    assignedIssues: number;
  } | null>(null);
  const [reassignTo, setReassignTo] = useState<string>("");

  const revoke = (member: ProjectMemberAccess) => {
    unbind.mutate(
      { userId: member.user_id },
      {
        onError: (error) => {
          const assigned = assignedIssuesFromError(error);
          if (assigned === null) {
            toast.error(t(($) => $.access.revoke_failed));
            return;
          }
          setPending({ member, assignedIssues: assigned });
          setReassignTo("");
        },
      },
    );
  };

  const answerAndRevoke = (onAssignedIssues: "unassign" | "reassign") => {
    if (!pending) return;
    if (onAssignedIssues === "reassign" && !reassignTo) return;
    unbind.mutate(
      {
        userId: pending.member.user_id,
        onAssignedIssues,
        reassignTo: onAssignedIssues === "reassign" ? reassignTo : undefined,
      },
      {
        onSuccess: () => setPending(null),
        onError: () => toast.error(t(($) => $.access.revoke_failed)),
      },
    );
  };

  // Everyone who could take over the work: people who will still see the
  // project after the revoke.
  const handoverItems = members
    .filter((m) => m.user_id !== pending?.member.user_id && m.sees)
    .map((m) => ({ value: m.user_id, label: m.name || m.email }));

  return (
    <section className="space-y-3">
      <div>
        <h2 className="text-body font-medium">{t(($) => $.access.title)}</h2>
        <p className="text-caption text-muted-foreground">
          {t(($) => $.access.subtitle)}
        </p>
      </div>

      {isLoading ? (
        <p className="text-body text-muted-foreground">{t(($) => $.access.loading)}</p>
      ) : (
        <ul className="divide-y rounded-md border">
          {members.map((member) => (
            <li
              key={member.user_id}
              className="flex items-center justify-between gap-3 px-3 py-2"
            >
              <div className="min-w-0">
                <p className="truncate text-body">{member.name || member.email}</p>
                <p className="truncate text-caption text-muted-foreground">
                  {member.email}
                </p>
              </div>
              {member.access_scope === "projects" || member.role === "guest" ? (
                <Switch
                  checked={member.bound}
                  disabled={bind.isPending || unbind.isPending}
                  onCheckedChange={(next) =>
                    next
                      ? bind.mutate(member.user_id, {
                          onError: () => toast.error(t(($) => $.access.grant_failed)),
                        })
                      : revoke(member)
                  }
                  aria-label={t(($) => $.access.toggle_label, {
                    name: member.name || member.email,
                  })}
                />
              ) : (
                // No switch: this person sees the whole workspace, and an
                // unchecked box here would read as "denied".
                <span className="shrink-0 text-caption text-muted-foreground">
                  {t(($) => $.access.sees_whole_workspace)}
                </span>
              )}
            </li>
          ))}
        </ul>
      )}

      <Dialog open={pending !== null} onOpenChange={(open) => !open && setPending(null)}>
        <DialogContent>
          <DialogHeader>
            <DialogTitle>{t(($) => $.access.handover_title)}</DialogTitle>
            <DialogDescription>
              {t(($) => $.access.handover_description, {
                name: pending?.member.name || pending?.member.email || "",
                count: pending?.assignedIssues ?? 0,
              })}
            </DialogDescription>
          </DialogHeader>

          <Select
            items={handoverItems}
            value={reassignTo}
            onValueChange={(value) => setReassignTo(value ?? "")}
          >
            <SelectTrigger aria-label={t(($) => $.access.handover_select_label)}>
              <SelectValue>
                {handoverItems.find((item) => item.value === reassignTo)?.label ??
                  t(($) => $.access.handover_placeholder)}
              </SelectValue>
            </SelectTrigger>
            <SelectContent>
              {handoverItems.map((item) => (
                <SelectItem key={item.value} value={item.value}>
                  {item.label}
                </SelectItem>
              ))}
            </SelectContent>
          </Select>

          <DialogFooter>
            <Button
              variant="outline"
              onClick={() => answerAndRevoke("unassign")}
              disabled={unbind.isPending}
            >
              {t(($) => $.access.unassign)}
            </Button>
            <Button
              onClick={() => answerAndRevoke("reassign")}
              disabled={unbind.isPending || !reassignTo}
            >
              {t(($) => $.access.reassign)}
            </Button>
          </DialogFooter>
        </DialogContent>
      </Dialog>
    </section>
  );
}
