import type { Workspace } from "../types";

/**
 * Which parts of the app a role may open.
 *
 * The owner picks this per role in workspace settings. What is stored is the
 * list of HIDDEN sections, so an empty setting means "everything as before"
 * and a section introduced by a later release starts visible instead of
 * quietly disappearing for everyone.
 *
 * The server enforces the same list (server/internal/middleware/nav_sections.go).
 * Hiding here is what the person sees; hiding there is what actually holds,
 * because a sidebar item is only a link and the route stays typeable.
 */
export const NAV_SECTIONS = [
  "autopilots",
  "agents",
  "squads",
  "runtimes",
  "skills",
] as const;

export type NavSection = (typeof NAV_SECTIONS)[number];

/** Roles the setting can be expressed for. Owner and admin are never hidden
 * from anything: the setting is how they state the policy, and locking
 * themselves out of the screen that edits it would be a trap. */
export const NAV_SECTION_ROLES = ["member", "guest"] as const;

export type NavSectionRole = (typeof NAV_SECTION_ROLES)[number];

export const HIDDEN_NAV_SECTIONS_KEY = "hidden_nav_sections";

export type HiddenNavSections = Partial<Record<NavSectionRole, NavSection[]>>;

function isNavSection(value: unknown): value is NavSection {
  return (
    typeof value === "string" && (NAV_SECTIONS as readonly string[]).includes(value)
  );
}

/**
 * Reads the stored map defensively: this comes off the API, and settings is a
 * free-form JSON blob that other features may also write into.
 */
export function readHiddenNavSections(
  workspace: Pick<Workspace, "settings"> | null | undefined,
): HiddenNavSections {
  const raw = workspace?.settings?.[HIDDEN_NAV_SECTIONS_KEY];
  if (!raw || typeof raw !== "object" || Array.isArray(raw)) return {};
  const out: HiddenNavSections = {};
  for (const role of NAV_SECTION_ROLES) {
    const list = (raw as Record<string, unknown>)[role];
    if (!Array.isArray(list)) continue;
    const sections = list.filter(isNavSection);
    if (sections.length > 0) out[role] = sections;
  }
  return out;
}

/**
 * Whether `role` may open `section`. An unknown role (owner, admin, or
 * anything a later release adds) is never restricted.
 */
export function navSectionVisible(
  workspace: Pick<Workspace, "settings"> | null | undefined,
  role: string | null | undefined,
  section: NavSection,
): boolean {
  if (role !== "member" && role !== "guest") return true;
  const hidden = readHiddenNavSections(workspace)[role];
  return !hidden?.includes(section);
}

/**
 * Turns the checkbox state back into what gets stored. Written as a patch over
 * the existing settings object because `settings` is shared with other
 * features and PUT replaces it wholesale — dropping the rest of the blob here
 * would silently discard them.
 */
export function withHiddenNavSections(
  settings: Record<string, unknown> | null | undefined,
  hidden: HiddenNavSections,
): Record<string, unknown> {
  const next: Record<string, unknown> = { ...(settings ?? {}) };
  const cleaned: HiddenNavSections = {};
  for (const role of NAV_SECTION_ROLES) {
    const list = hidden[role];
    if (list && list.length > 0) cleaned[role] = list;
  }
  if (Object.keys(cleaned).length === 0) {
    delete next[HIDDEN_NAV_SECTIONS_KEY];
  } else {
    next[HIDDEN_NAV_SECTIONS_KEY] = cleaned;
  }
  return next;
}
