"use client";

import { useMemo, useState } from "react";
import { useQuery, useMutation, useQueryClient } from "@tanstack/react-query";
import { ChevronRight, Check, Copy, ExternalLink, Trash2, Link2 } from "lucide-react";
import { toast } from "sonner";
import { Button } from "@multica/ui/components/ui/button";
import { useWorkspaceId } from "@multica/core/hooks";
import { memberListOptions } from "@multica/core/workspace/queries";
import { useAuthStore } from "@multica/core/auth";
import { useT } from "../../i18n";
import {
  fetchSitepingTokens,
  deleteSitepingToken,
  fetchSitepingMeta,
  saveSitepingMeta,
  buildSitepingShareUrl,
  ensureSitepingTokenForEmail,
  sitepingKeys,
} from "../siteping-api";

// SitePing integration section.
//
// 1. Token-gated <script> snippet — invisible by default, opens to clients
//    who arrive via a shareable link.
// 2. Site URL (used to assemble shareable links) + the list of issued links
//    for audit / revoke. Links themselves are created per member from the
//    "People" section above (each member/guest row has a copy-link action),
//    so there is no manual name/email entry here anymore.

const BRIDGE_URL = "https://siteping.koystrub.dev";

function buildSnippet(_projectId: string): string {
  // Customers paste this once. Everything mutable — token strip, fetch
  // overrides, widget config, future fixes — lives in /loader.js on the
  // bridge and propagates through its 5-minute cache to every site
  // automatically, no re-paste required.
  //
  // No projectId embedded in the HTML — the project is resolved from the
  // access token at runtime, so leaking the UUID into customer-site HTML
  // would have been a needless info-leak with zero functional value.
  return `<script async data-no-minify="1" data-no-optimize="1" src="${BRIDGE_URL}/loader.js"></script>`;
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

// Site URL + the list of issued share links (audit / revoke). Links are now
// created per member from the People section (each row has a copy-link action),
// so this component no longer offers manual link creation.
function ShareLinksManager({ projectId }: { projectId: string }) {
  const { t } = useT("projects");
  const qc = useQueryClient();
  const wsId = useWorkspaceId();
  const userId = useAuthStore((s) => s.user?.id);
  const { data: members = [] } = useQuery(memberListOptions(wsId));
  const currentMember = members.find((m) => m.user_id === userId) ?? null;
  const [siteUrlDraft, setSiteUrlDraft] = useState<string | null>(null);
  const [openingAdmin, setOpeningAdmin] = useState(false);

  const { data: meta } = useQuery({
    queryKey: sitepingKeys.meta(projectId),
    queryFn: () => fetchSitepingMeta(projectId),
  });
  const siteUrl = siteUrlDraft ?? meta?.siteUrl ?? "";

  const saveMetaMut = useMutation({
    mutationFn: (url: string) => saveSitepingMeta(projectId, url),
    onSuccess: () => {
      qc.invalidateQueries({ queryKey: sitepingKeys.meta(projectId) });
      setSiteUrlDraft(null);
      toast.success(t(($) => $.siteping.share.site_url_saved));
    },
    onError: () => toast.error(t(($) => $.siteping.share.site_url_save_failed)),
  });

  const { data: tokens = [], isLoading } = useQuery({
    queryKey: sitepingKeys.tokens(projectId),
    queryFn: () => fetchSitepingTokens(projectId),
  });

  const deleteMut = useMutation({
    mutationFn: (token: string) => deleteSitepingToken(token),
    onSuccess: () => {
      qc.invalidateQueries({ queryKey: sitepingKeys.tokens(projectId) });
      toast.success(t(($) => $.siteping.share.revoked_toast));
    },
    onError: (e: Error) => toast.error(`${t(($) => $.siteping.share.revoke_failed)}: ${e.message}`),
  });

  const copyLink = async (token: string) => {
    try {
      await navigator.clipboard.writeText(buildSitepingShareUrl(siteUrl, token));
      toast.success(t(($) => $.siteping.share.link_copied));
    } catch {
      toast.error(t(($) => $.siteping.copy_failed_toast));
    }
  };

  // Quick "open my site as admin" — mints/reuses a token for the current user
  // and opens the live site with the widget in a new tab under that identity.
  const handleOpenAsAdmin = async () => {
    if (!siteUrl) {
      toast.error(t(($) => $.siteping.share.admin_no_site_url));
      return;
    }
    if (!currentMember) return;
    setOpeningAdmin(true);
    try {
      const token = await ensureSitepingTokenForEmail(projectId, tokens, {
        name: currentMember.name,
        email: currentMember.email,
      });
      await qc.invalidateQueries({ queryKey: sitepingKeys.tokens(projectId) });
      window.open(buildSitepingShareUrl(siteUrl, token), "_blank", "noopener,noreferrer");
    } catch (e) {
      toast.error(e instanceof Error ? e.message : t(($) => $.siteping.share.admin_open_failed));
    } finally {
      setOpeningAdmin(false);
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
      {currentMember && (
        <Button
          type="button"
          variant="secondary"
          size="sm"
          disabled={openingAdmin}
          onClick={handleOpenAsAdmin}
          className="h-7 w-full gap-1.5 text-[11px]"
        >
          <ExternalLink className="size-3" />
          {openingAdmin
            ? t(($) => $.siteping.share.admin_opening)
            : t(($) => $.siteping.share.open_as_admin)}
        </Button>
      )}
      <div className="pt-1 border-t">
        <div className="text-xs font-medium truncate">
          {t(($) => $.siteping.share.title)}
        </div>
        <p className="text-[11px] text-muted-foreground mt-0.5">
          {t(($) => $.siteping.share.created_from_people_hint)}
        </p>
      </div>

      {isLoading && (
        <div className="text-[11px] text-muted-foreground">
          {t(($) => $.siteping.share.loading)}
        </div>
      )}
      {!isLoading && tokens.length === 0 && (
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
