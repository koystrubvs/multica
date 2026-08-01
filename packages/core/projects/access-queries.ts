import { queryOptions, useMutation, useQueryClient } from "@tanstack/react-query";
import { api } from "../api";
import { projectKeys } from "./queries";

export const projectAccessKeys = {
  members: (wsId: string, projectId: string) =>
    [...projectKeys.detail(wsId, projectId), "members"] as const,
};

export function projectMembersOptions(wsId: string, projectId: string) {
  return queryOptions({
    queryKey: projectAccessKeys.members(wsId, projectId),
    queryFn: () => api.listProjectMembers(projectId),
  });
}

export function useBindProjectMember(wsId: string, projectId: string) {
  const qc = useQueryClient();
  return useMutation({
    mutationFn: (userId: string) => api.bindProjectMember(projectId, userId),
    onSettled: () => {
      qc.invalidateQueries({
        queryKey: projectAccessKeys.members(wsId, projectId),
      });
    },
  });
}

/** Revoking is not optimistic and never local-only: the server may refuse with
 *  409 when work is assigned to this person in this project, and the caller
 *  has to answer where that work goes before anything changes. */
export function useUnbindProjectMember(wsId: string, projectId: string) {
  const qc = useQueryClient();
  return useMutation({
    mutationFn: ({
      userId,
      onAssignedIssues,
      reassignTo,
    }: {
      userId: string;
      onAssignedIssues?: "unassign" | "reassign";
      reassignTo?: string;
    }) => api.unbindProjectMember(projectId, userId, { onAssignedIssues, reassignTo }),
    onSettled: () => {
      qc.invalidateQueries({
        queryKey: projectAccessKeys.members(wsId, projectId),
      });
      // The reassignment moves issues to someone else, so any list showing
      // assignees in this project is now stale.
      qc.invalidateQueries({ queryKey: ["issues"] });
    },
  });
}
