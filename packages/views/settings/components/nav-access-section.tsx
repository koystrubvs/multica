"use client";

import { useCallback, useMemo, useState } from "react";
import { useQueryClient } from "@tanstack/react-query";
import { toast } from "sonner";
import { api } from "@multica/core/api";
import type { Workspace } from "@multica/core/types";
import {
  NAV_SECTIONS,
  NAV_SECTION_ROLES,
  readHiddenNavSections,
  withHiddenNavSections,
  workspaceKeys,
  type HiddenNavSections,
  type NavSection,
  type NavSectionRole,
} from "@multica/core/workspace";
import { Switch } from "@multica/ui/components/ui/switch";
import { useT } from "../../i18n";
import { SettingsCard, SettingsRow, SettingsSection } from "./settings-layout";

/**
 * Which sections each role may open.
 *
 * The switches read "visible", while what is stored is the hidden list — an
 * empty setting has to mean "everything as before", so that a section added by
 * a later release does not silently vanish for everyone.
 *
 * Owner and admin are absent by design: the setting is how they state the
 * policy, and letting them close the screen that edits it would be a trap.
 */
export function NavAccessSection({ workspace }: { workspace: Workspace }) {
  const { t } = useT("settings");
  const queryClient = useQueryClient();
  const [saving, setSaving] = useState(false);
  const [override, setOverride] = useState<HiddenNavSections | null>(null);

  const stored = useMemo(() => readHiddenNavSections(workspace), [workspace]);
  // The local override holds the switch the user just flipped until the
  // refetched workspace catches up, so the control does not snap back.
  const hidden = override ?? stored;

  const save = useCallback(
    async (next: HiddenNavSections) => {
      setOverride(next);
      setSaving(true);
      try {
        // settings is a shared blob and PUT replaces it wholesale, so the
        // patch is written over the current value rather than in place of it.
        await api.updateWorkspace(workspace.id, {
          settings: withHiddenNavSections(workspace.settings, next),
        });
        // Both the workspace record the sidebar reads and the list the
        // switcher renders carry `settings`, so refresh the whole subtree.
        await queryClient.invalidateQueries({
          queryKey: workspaceKeys.all(workspace.id),
        });
        await queryClient.invalidateQueries({ queryKey: workspaceKeys.list() });
        toast.success(t(($) => $.workspace.nav_access.toast_saved));
      } catch (err) {
        setOverride(null);
        toast.error(
          err instanceof Error
            ? err.message
            : t(($) => $.workspace.nav_access.toast_failed),
        );
      } finally {
        setSaving(false);
      }
    },
    [workspace.id, workspace.settings, queryClient, t],
  );

  const toggle = useCallback(
    (role: NavSectionRole, section: NavSection, visible: boolean) => {
      const current = hidden[role] ?? [];
      const nextForRole = visible
        ? current.filter((s) => s !== section)
        : current.includes(section)
          ? current
          : [...current, section];
      void save({ ...hidden, [role]: nextForRole });
    },
    [hidden, save],
  );

  const roleLabel = (role: NavSectionRole) =>
    role === "member"
      ? t(($) => $.workspace.nav_access.role_member)
      : t(($) => $.workspace.nav_access.role_guest);

  const sectionLabel = (section: NavSection) => {
    switch (section) {
      case "autopilots":
        return t(($) => $.workspace.nav_access.section_autopilots);
      case "agents":
        return t(($) => $.workspace.nav_access.section_agents);
      case "squads":
        return t(($) => $.workspace.nav_access.section_squads);
      case "runtimes":
        return t(($) => $.workspace.nav_access.section_runtimes);
      case "skills":
        return t(($) => $.workspace.nav_access.section_skills);
    }
  };

  return (
    <SettingsSection title={t(($) => $.workspace.nav_access.title)}>
      <SettingsCard>
        <div className="px-4 pt-3 text-caption text-muted-foreground">
          {t(($) => $.workspace.nav_access.description)}
        </div>
        {NAV_SECTION_ROLES.map((role) => (
          <div key={role}>
            <div className="px-4 pt-4 pb-1 text-label font-medium text-foreground">
              {roleLabel(role)}
            </div>
            {NAV_SECTIONS.map((section) => {
              const visible = !(hidden[role] ?? []).includes(section);
              return (
                <SettingsRow key={`${role}:${section}`} label={sectionLabel(section)}>
                  <Switch
                    checked={visible}
                    disabled={saving}
                    aria-label={`${roleLabel(role)} — ${sectionLabel(section)}`}
                    onCheckedChange={(next: boolean) => toggle(role, section, next)}
                  />
                </SettingsRow>
              );
            })}
          </div>
        ))}
        <div className="px-4 pt-2 pb-3 text-caption text-muted-foreground">
          {t(($) => $.workspace.nav_access.hint)}
        </div>
      </SettingsCard>
    </SettingsSection>
  );
}

NavAccessSection.displayName = "NavAccessSection";
