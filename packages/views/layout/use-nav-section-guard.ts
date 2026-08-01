"use client";

import { useEffect } from "react";
import { useCurrentWorkspace, useWorkspacePaths } from "@multica/core/paths";
import { useCurrentMember } from "@multica/core/permissions";
import { navSectionVisible, type NavSection } from "@multica/core/workspace";
import { useNavigation } from "../navigation";

/**
 * Sends someone away from a section their role cannot open.
 *
 * Hiding the sidebar item leaves the address typeable, and the server refuses
 * the data rather than the page — which on its own produces an empty screen
 * with a failed request instead of an answer. This turns that into a redirect
 * back to the issue list.
 *
 * Returns whether the section is closed, so the caller can render nothing for
 * the frame between the decision and the navigation.
 *
 * While the member query is still loading `role` is undefined, which reads as
 * unrestricted — so an owner never sees a flash of redirect on a cold load.
 * The cost is that a restricted person may see the page for one frame before
 * being sent back; the data is refused server-side regardless, so nothing
 * leaks in that frame.
 */
export function useNavSectionGuard(section: NavSection): boolean {
  const workspace = useCurrentWorkspace();
  const { role } = useCurrentMember(workspace?.id ?? "");
  const navigation = useNavigation();
  const paths = useWorkspacePaths();
  const closed = !navSectionVisible(workspace, role, section);

  useEffect(() => {
    if (closed) navigation.replace(paths.issues());
    // `navigation` and `paths` are recreated per render by their providers;
    // depending on them would re-fire the redirect on every render.
    // eslint-disable-next-line react-hooks/exhaustive-deps
  }, [closed]);

  return closed;
}
