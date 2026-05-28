"use client";

import { useMemo, useState } from "react";
import { useQuery, useMutation, useQueryClient } from "@tanstack/react-query";
import { ChevronRight, Check, Copy, ExternalLink, Plus, Trash2, Link2 } from "lucide-react";
import { toast } from "sonner";
import { Button } from "@multica/ui/components/ui/button";
import { useT } from "../../i18n";

// SitePing integration section.
//
// 1. Token-gated <script> snippet — invisible by default, opens to clients
//    who arrive via a shareable link.
// 2. Share-link manager — create / list / revoke tokens that map to this
//    Multica project. Each token carries a client name+email that the
//    widget pre-fills server-side (can't be spoofed from the browser).

const BRIDGE_URL = "https://siteping.koystrub.dev";
const WIDGET_API_KEY = "JNelrIjpUUrrRapo4uB2xYsVKV02jtO";

function buildSnippet(projectId: string): string {
  // Customers paste this once. Everything mutable — token strip, fetch
  // overrides, widget config, future fixes — lives in /loader.js on the
  // bridge and propagates through its 5-minute cache to every site
  // automatically, no re-paste required.
  return `<script async src="${BRIDGE_URL}/loader.js"></script>

<!-- This project's Multica UUID (reference, not used at runtime):
     ${projectId} -->`;
}

interface TokenEntry {
  token: string;
  multicaProjectId: string;
  authorName: string | null;
  authorEmail: string | null;
  label: string | null;
  createdAt: string;
  lastUsedAt: string | null;
  uses: number;
}

async function fetchTokens(projectId: string): Promise<TokenEntry[]> {
  const r = await fetch(`/api/siteping-admin/tokens?projectId=${projectId}`);
  if (!r.ok) throw new Error(`HTTP ${r.status}`);
  const j = await r.json();
  return j.tokens || [];
}

async function createToken(projectId: string, body: {
  authorName: string;
  authorEmail: string;
  label: string;
}): Promise<TokenEntry> {
  const r = await fetch("/api/siteping-admin/tokens", {
    method: "POST",
    headers: { "Content-Type": "application/json" },
    body: JSON.stringify({ projectId, ...body }),
  });
  if (!r.ok) throw new Error(`HTTP ${r.status}`);
  return r.json();
}

async function deleteToken(token: string): Promise<void> {
  const r = await fetch(`/api/siteping-admin/tokens/${token}`, { method: "DELETE" });
  if (!r.ok) throw new Error(`HTTP ${r.status}`);
}

async function fetchMeta(projectId: string): Promise<{ siteUrl?: string }> {
  const r = await fetch(`/api/siteping-admin/site-url?projectId=${projectId}`);
  if (!r.ok) return {};
  return r.json();
}

async function saveMeta(projectId: string, siteUrl: string): Promise<void> {
  const r = await fetch(`/api/siteping-admin/site-url?projectId=${projectId}`, {
    method: "PUT",
    headers: { "Content-Type": "application/json" },
    body: JSON.stringify({ siteUrl }),
  });
  if (!r.ok) throw new Error(`HTTP ${r.status}`);
}

export function SitepingIntegrationSection({ projectId }: { projectId: string }) {
  const { t } = useT("projects");
  const [open, setOpen] = useState(true);
  const [snippetCopied, setSnippetCopied] = useState(false);

  const snippet = useMemo(() => buildSnippet(projectId), [projectId]);

  const handleCopySnippet = async () => {
    try {
      await navigator.clipboard.writeText(snippet);
      setSnippetCopied(true);
      toast.success(t(($) => $.siteping.copied_toast));
      setTimeout(() => setSnippetCopied(false), 2000);
    } catch {
      toast.error(t(($) => $.siteping.copy_failed_toast));
    }
  };

  return (
    <div>
      <button
        type="button"
        onClick={() => setOpen((v) => !v)}
        className={`flex w-full items-center gap-1 rounded-md px-2 py-1 text-xs font-medium transition-colors mb-2 hover:bg-accent/70 ${
          open ? "" : "text-muted-foreground hover:text-foreground"
        }`}
      >
        {t(($) => $.siteping.section_title)}
        <ChevronRight
          className={`!size-3 shrink-0 stroke-[2.5] text-muted-foreground transition-transform ${
            open ? "rotate-90" : ""
          }`}
        />
      </button>
      {open && (
        <div className="space-y-4 pl-2">
          <p className="text-xs text-muted-foreground">
            {t(($) => $.siteping.description)}
          </p>
          <div className="relative">
            <pre className="overflow-x-auto rounded-md border bg-muted/40 p-3 text-[11px] leading-relaxed font-mono max-h-64">
              <code>{snippet}</code>
            </pre>
            <Button
              type="button"
              variant="secondary"
              size="sm"
              onClick={handleCopySnippet}
              className="absolute top-2 right-2 h-7 gap-1.5 px-2 text-xs"
            >
              {snippetCopied ? <Check className="size-3" /> : <Copy className="size-3" />}
              {snippetCopied
                ? t(($) => $.siteping.copied_button)
                : t(($) => $.siteping.copy_button)}
            </Button>
          </div>
          <ShareLinksManager projectId={projectId} />
          <div className="flex items-center gap-3 text-[11px] text-muted-foreground">
            <a
              href="https://github.com/NeosiaNexus/SitePing"
              target="_blank"
              rel="noopener noreferrer"
              className="inline-flex items-center gap-1 hover:underline"
            >
              <ExternalLink className="size-3" />
              {t(($) => $.siteping.docs_link)}
            </a>
          </div>
        </div>
      )}
    </div>
  );
}

function ShareLinksManager({ projectId }: { projectId: string }) {
  const { t } = useT("projects");
  const qc = useQueryClient();
  const [showForm, setShowForm] = useState(false);
  const [authorName, setAuthorName] = useState("");
  const [authorEmail, setAuthorEmail] = useState("");
  const [label, setLabel] = useState("");
  const [siteUrlDraft, setSiteUrlDraft] = useState<string | null>(null);

  const { data: meta } = useQuery({
    queryKey: ["siteping-meta", projectId],
    queryFn: () => fetchMeta(projectId),
  });
  const siteUrl = siteUrlDraft ?? meta?.siteUrl ?? "";

  const saveMetaMut = useMutation({
    mutationFn: (url: string) => saveMeta(projectId, url),
    onSuccess: () => {
      qc.invalidateQueries({ queryKey: ["siteping-meta", projectId] });
      setSiteUrlDraft(null);
      toast.success(t(($) => $.siteping.share.site_url_saved));
    },
    onError: () => toast.error(t(($) => $.siteping.share.site_url_save_failed)),
  });

  const { data: tokens = [], isLoading } = useQuery({
    queryKey: ["siteping-tokens", projectId],
    queryFn: () => fetchTokens(projectId),
  });

  const createMut = useMutation({
    mutationFn: () => createToken(projectId, { authorName, authorEmail, label }),
    onSuccess: () => {
      qc.invalidateQueries({ queryKey: ["siteping-tokens", projectId] });
      setShowForm(false);
      setAuthorName("");
      setAuthorEmail("");
      setLabel("");
      toast.success(t(($) => $.siteping.share.created_toast));
    },
    onError: (e: Error) => toast.error(`${t(($) => $.siteping.share.create_failed)}: ${e.message}`),
  });

  const deleteMut = useMutation({
    mutationFn: (token: string) => deleteToken(token),
    onSuccess: () => {
      qc.invalidateQueries({ queryKey: ["siteping-tokens", projectId] });
      toast.success(t(($) => $.siteping.share.revoked_toast));
    },
    onError: (e: Error) => toast.error(`${t(($) => $.siteping.share.revoke_failed)}: ${e.message}`),
  });

  const buildShareUrl = (token: string) => {
    const base = (siteUrl || "https://your-site.com").replace(/\/+$/, "");
    return `${base}/?siteping_access_token=${token}`;
  };

  const copyLink = async (token: string) => {
    try {
      await navigator.clipboard.writeText(buildShareUrl(token));
      toast.success(t(($) => $.siteping.share.link_copied));
    } catch {
      toast.error(t(($) => $.siteping.copy_failed_toast));
    }
  };

  return (
    <div className="space-y-2 rounded-md border bg-background/50 p-2.5">
      <div className="space-y-1">
        <label className="text-[11px] font-medium text-muted-foreground">
          {t(($) => $.siteping.share.site_url_label)}
        </label>
        <div className="flex gap-1">
          <input
            type="url"
            value={siteUrl}
            onChange={(e) => setSiteUrlDraft(e.target.value)}
            placeholder="https://bambinoclinic.ru"
            className="h-7 flex-1 min-w-0 rounded-md border bg-background px-2 text-xs outline-none focus-visible:ring-1 focus-visible:ring-ring"
          />
          {siteUrlDraft !== null && siteUrlDraft !== (meta?.siteUrl || "") && (
            <Button
              type="button"
              size="sm"
              disabled={saveMetaMut.isPending}
              onClick={() => saveMetaMut.mutate(siteUrlDraft.trim())}
              className="h-7 px-2 text-[11px] shrink-0"
            >
              {saveMetaMut.isPending
                ? t(($) => $.siteping.share.saving)
                : t(($) => $.siteping.share.save)}
            </Button>
          )}
        </div>
      </div>
      <div className="flex items-center justify-between gap-2 pt-1 border-t">
        <div className="text-xs font-medium truncate">
          {t(($) => $.siteping.share.title)}
        </div>
        <Button
          type="button"
          variant="secondary"
          size="sm"
          onClick={() => setShowForm((v) => !v)}
          className="h-6 gap-1 px-2 text-[11px] shrink-0"
        >
          <Plus className="size-3" />
          {t(($) => $.siteping.share.new_button)}
        </Button>
      </div>

      {showForm && (
        <div className="space-y-2 rounded-md border bg-muted/30 p-2">
          <input
            type="text"
            value={authorName}
            onChange={(e) => setAuthorName(e.target.value)}
            placeholder={t(($) => $.siteping.share.name_placeholder)}
            className="h-8 w-full rounded-md border bg-transparent px-2 text-xs outline-none focus-visible:ring-1 focus-visible:ring-ring"
          />
          <input
            type="email"
            value={authorEmail}
            onChange={(e) => setAuthorEmail(e.target.value)}
            placeholder={t(($) => $.siteping.share.email_placeholder)}
            className="h-8 w-full rounded-md border bg-transparent px-2 text-xs outline-none focus-visible:ring-1 focus-visible:ring-ring"
          />
          <input
            type="text"
            value={label}
            onChange={(e) => setLabel(e.target.value)}
            placeholder={t(($) => $.siteping.share.label_placeholder)}
            className="h-8 w-full rounded-md border bg-transparent px-2 text-xs outline-none focus-visible:ring-1 focus-visible:ring-ring"
          />
          <div className="flex justify-end gap-2">
            <Button
              type="button"
              variant="ghost"
              size="sm"
              onClick={() => setShowForm(false)}
              className="h-7 px-2 text-xs"
            >
              {t(($) => $.siteping.share.cancel)}
            </Button>
            <Button
              type="button"
              size="sm"
              disabled={!authorName.trim() || createMut.isPending}
              onClick={() => createMut.mutate()}
              className="h-7 px-2 text-xs"
            >
              {createMut.isPending
                ? t(($) => $.siteping.share.creating)
                : t(($) => $.siteping.share.create)}
            </Button>
          </div>
        </div>
      )}

      {isLoading && (
        <div className="text-[11px] text-muted-foreground">
          {t(($) => $.siteping.share.loading)}
        </div>
      )}
      {!isLoading && tokens.length === 0 && !showForm && (
        <div className="text-[11px] text-muted-foreground italic">
          {t(($) => $.siteping.share.empty)}
        </div>
      )}
      {tokens.map((tk) => (
        <div
          key={tk.token}
          className="rounded-md border bg-background p-2 text-xs space-y-1"
        >
          <div className="flex items-center gap-1.5 min-w-0">
            <Link2 className="size-3 shrink-0 text-muted-foreground" />
            <span className="font-medium truncate flex-1 min-w-0" title={tk.label || tk.authorName || ""}>
              {tk.label || tk.authorName || tk.token.slice(0, 8)}
            </span>
            <Button
              type="button"
              variant="ghost"
              size="sm"
              onClick={() => copyLink(tk.token)}
              className="h-5 w-5 p-0 shrink-0"
              title={t(($) => $.siteping.share.copy_link)}
            >
              <Copy className="size-3" />
            </Button>
            <Button
              type="button"
              variant="ghost"
              size="sm"
              onClick={() => {
                if (confirm(t(($) => $.siteping.share.revoke_confirm))) {
                  deleteMut.mutate(tk.token);
                }
              }}
              className="h-5 w-5 p-0 text-destructive hover:text-destructive shrink-0"
              title={t(($) => $.siteping.share.revoke)}
            >
              <Trash2 className="size-3" />
            </Button>
          </div>
          {tk.authorEmail && (
            <div className="text-[10px] text-muted-foreground truncate pl-4.5">
              {tk.authorEmail}
            </div>
          )}
          <div className="text-[10px] text-muted-foreground pl-4.5">
            {t(($) => $.siteping.share.uses_count, { count: tk.uses })}
            {tk.lastUsedAt && ` · ${new Date(tk.lastUsedAt).toLocaleDateString()}`}
          </div>
        </div>
      ))}
    </div>
  );
}
