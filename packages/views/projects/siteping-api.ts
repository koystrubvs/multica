// Shared SitePing bridge API helpers + types.
//
// The bridge (siteping.koystrub.dev) owns the share-token store and per-project
// site-URL metadata. Both the project's "People" section (where a link is copied
// per member) and the SitePing integration section (site URL + issued-link list)
// talk to it through these helpers, so the request shapes stay in one place.

export const SITEPING_BRIDGE_URL = "https://siteping.koystrub.dev";

export interface SitepingTokenEntry {
  token: string;
  multicaProjectId: string;
  authorName: string | null;
  authorEmail: string | null;
  label: string | null;
  createdAt: string;
  lastUsedAt: string | null;
  uses: number;
}

export async function fetchSitepingTokens(projectId: string): Promise<SitepingTokenEntry[]> {
  const r = await fetch(`/api/siteping-admin/tokens?projectId=${projectId}`);
  if (!r.ok) throw new Error(`HTTP ${r.status}`);
  const j = await r.json();
  return j.tokens || [];
}

export async function createSitepingToken(
  projectId: string,
  body: { authorName: string; authorEmail: string; label: string },
): Promise<SitepingTokenEntry> {
  const r = await fetch("/api/siteping-admin/tokens", {
    method: "POST",
    headers: { "Content-Type": "application/json" },
    body: JSON.stringify({ projectId, ...body }),
  });
  if (!r.ok) throw new Error(`HTTP ${r.status}`);
  return r.json();
}

export async function deleteSitepingToken(token: string): Promise<void> {
  // DELETE on the collection route with the id as a query param — the
  // dynamic `tokens/[tokenId]` route 404s in this Next build.
  const r = await fetch(
    `/api/siteping-admin/tokens?token=${encodeURIComponent(token)}`,
    { method: "DELETE" },
  );
  if (!r.ok) throw new Error(`HTTP ${r.status}`);
}

export async function fetchSitepingMeta(projectId: string): Promise<{ siteUrl?: string }> {
  const r = await fetch(`/api/siteping-admin/site-url?projectId=${projectId}`);
  if (!r.ok) return {};
  return r.json();
}

export async function saveSitepingMeta(projectId: string, siteUrl: string): Promise<void> {
  const r = await fetch(`/api/siteping-admin/site-url?projectId=${projectId}`, {
    method: "PUT",
    headers: { "Content-Type": "application/json" },
    body: JSON.stringify({ siteUrl }),
  });
  if (!r.ok) throw new Error(`HTTP ${r.status}`);
}

// React-query keys shared across components so a token/meta mutation in one
// place refreshes the other.
export interface SitepingStatusEntry {
  connected: boolean;
  installed: boolean | null;
  pingedAt: string | null;
  siteUrl: string | null;
  tokenCount: number;
  feedbackCount: number;
}

// Bulk per-project SitePing connection status (one request for the whole
// projects list). Map is keyed by Multica project id; absent = never set up.
export async function fetchSitepingStatus(): Promise<Record<string, SitepingStatusEntry>> {
  const r = await fetch("/api/siteping-admin/status");
  if (!r.ok) throw new Error(`HTTP ${r.status}`);
  const j = await r.json();
  return (j.projects as Record<string, SitepingStatusEntry>) || {};
}

export const sitepingKeys = {
  tokens: (projectId: string) => ["siteping-tokens", projectId] as const,
  meta: (projectId: string) => ["siteping-meta", projectId] as const,
  status: () => ["siteping-status"] as const,
};

// Build the shareable site URL for a token. Falls back to a placeholder host
// when the project has no site URL set yet (the caller should gate on siteUrl
// before offering "copy", but this keeps the function total).
export function buildSitepingShareUrl(siteUrl: string | undefined, token: string): string {
  const base = (siteUrl || "https://your-site.com").replace(/\/+$/, "");
  return `${base}/?siteping_access_token=${token}`;
}

// Find an existing token for a member's email (case-insensitive), else create
// one bound to this project. Returns the token string ready to share.
export async function ensureSitepingTokenForEmail(
  projectId: string,
  tokens: SitepingTokenEntry[],
  member: { name: string; email: string },
): Promise<string> {
  const email = member.email.trim().toLowerCase();
  const existing = tokens.find((t) => (t.authorEmail || "").trim().toLowerCase() === email);
  if (existing) return existing.token;
  const created = await createSitepingToken(projectId, {
    authorName: member.name,
    authorEmail: member.email,
    label: member.name,
  });
  return created.token;
}
