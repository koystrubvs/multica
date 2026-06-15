"use client";

import { useQuery } from "@tanstack/react-query";
import type { MemberRole, SkillSummary } from "@multica/core/types";
import { useAuthStore } from "@multica/core/auth";
import { memberListOptions } from "@multica/core/workspace/queries";

/**
 * Whether the current user may edit/delete the given skill.
 *
 * Rule: for a workspace skill, workspace admins & owners can edit any skill and
 * everyone else can only edit skills they created. For a global skill
 * (`is_global`), only the owner (`created_by`) can edit — workspace roles do not
 * apply, because the skill spans every workspace its owner belongs to. Server
 * enforces this independently; the hook mirrors it so the UI can hide/disable
 * actions instead of waiting for a 403.
 *
 * `wsId` is explicit (not read from `WorkspaceIdProvider`) so this hook stays
 * usable in components that render before workspace context is wired, and so
 * the scope of the permission check is always obvious to the caller. Matches
 * the repo rule for workspace-aware hooks.
 */
export function useCanEditSkill(
  skill: SkillSummary | null | undefined,
  wsId: string,
): boolean {
  const userId = useAuthStore((s) => s.user?.id ?? null);
  const { data: members = [] } = useQuery(memberListOptions(wsId));

  if (!skill) return false;
  const myRole = members.find((m) => m.user_id === userId)?.role ?? null;
  return canEditSkill(skill, { userId, role: myRole });
}

/**
 * Non-hook variant for places that already have the role + userId at hand
 * (e.g. list rows that compute role once for the whole page).
 */
export function canEditSkill(
  skill: SkillSummary,
  opts: { userId: string | null; role: MemberRole | null },
): boolean {
  // Global skills are owner-only; workspace admin/owner privileges do not apply.
  if (skill.is_global === true) {
    return skill.created_by != null && skill.created_by === opts.userId;
  }
  if (opts.role === "admin" || opts.role === "owner") return true;
  return skill.created_by === opts.userId;
}
