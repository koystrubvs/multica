"use client";

import { useMemo, useState, useCallback, useRef, useEffect } from "react";
import { useDefaultLayout, usePanelRef } from "react-resizable-panels";
import { Check, ChevronRight, Link2, MoreHorizontal, PanelRight, Pin, PinOff, Trash2, UserMinus, UserPlus, X } from "lucide-react";
import { useQuery, useQueryClient } from "@tanstack/react-query";
import { cn } from "@multica/ui/lib/utils";
import { copyText } from "@multica/ui/lib/clipboard";
import { toast } from "sonner";
import type { ProjectStatus, ProjectPriority } from "@multica/core/types";
import { useAuthStore } from "@multica/core/auth";
import { useCurrentMember } from "@multica/core/permissions";
import { projectDetailOptions } from "@multica/core/projects/queries";
import { useUpdateProject, useDeleteProject } from "@multica/core/projects/mutations";
import { pinListOptions } from "@multica/core/pins";
import { useCreatePin, useDeletePin } from "@multica/core/pins";
import { memberListOptions, agentListOptions, workspaceKeys } from "@multica/core/workspace/queries";
import { api } from "@multica/core/api";
import {
  fetchSitepingTokens,
  fetchSitepingMeta,
  ensureSitepingTokenForEmail,
  buildSitepingShareUrl,
  sitepingKeys,
} from "../siteping-api";
import { useWorkspaceId } from "@multica/core/hooks";
import { useRecentContextStore } from "@multica/core/chat";
import { useWorkspacePaths } from "@multica/core/paths";
import { useActorName } from "@multica/core/workspace/hooks";
import { PROJECT_STATUS_ORDER, PROJECT_STATUS_CONFIG, PROJECT_PRIORITY_ORDER } from "@multica/core/projects/config";
import { getProjectIssueMetrics } from "./project-issue-metrics";
import { ActorAvatar } from "../../common/actor-avatar";
import { useNavigation } from "../../navigation";
import { TitleEditor, ContentEditor, type ContentEditorRef } from "../../editor";
import { PriorityIcon } from "../../issues/components/priority-icon";
import { ProjectResourcesSection } from "./project-resources-section";
import { SitepingIntegrationSection } from "./siteping-integration-section";
import { ProjectBillingSection } from "./project-billing-section";
import { ProjectStartDatePicker } from "./project-start-date-picker";
import { ProjectDueDatePicker } from "./project-due-date-picker";
import { IssueSurface } from "../../issues/surface/issue-surface";
import { Skeleton } from "@multica/ui/components/ui/skeleton";
import { Button } from "@multica/ui/components/ui/button";
import { ResizablePanelGroup, ResizablePanel, ResizableHandle } from "@multica/ui/components/ui/resizable";
import { Sheet, SheetContent } from "@multica/ui/components/ui/sheet";
import { useIsMobile } from "@multica/ui/hooks/use-mobile";
import {
  DropdownMenu,
  DropdownMenuContent,
  DropdownMenuItem,
  DropdownMenuSeparator,
  DropdownMenuTrigger,
} from "@multica/ui/components/ui/dropdown-menu";
import {
  Popover,
  PopoverTrigger,
  PopoverContent,
} from "@multica/ui/components/ui/popover";
import {
  Tooltip,
  TooltipTrigger,
  TooltipContent,
} from "@multica/ui/components/ui/tooltip";
import { EmojiPicker } from "@multica/ui/components/common/emoji-picker";
import { BreadcrumbHeader } from "../../layout/breadcrumb-header";
import {
  AnimatedRightSidebar,
  getAnimatedRightSidebarInitialOpen,
  rightSidebarPanelMotionProps,
  useAnimatedRightSidebarState,
} from "../../layout/animated-right-sidebar";
import {
  AlertDialog,
  AlertDialogAction,
  AlertDialogCancel,
  AlertDialogContent,
  AlertDialogDescription,
  AlertDialogFooter,
  AlertDialogHeader,
  AlertDialogTitle,
} from "@multica/ui/components/ui/alert-dialog";
import { useT } from "../../i18n";
import { useProjectStatusLabels, useProjectPriorityLabels } from "./labels";
import { PROJECT_TYPE_ORDER, useProjectTypeLabels } from "./project-type";
import { matchesPinyin } from "../../editor/extensions/pinyin-match";

// ---------------------------------------------------------------------------
// Property row — sidebar property display
// ---------------------------------------------------------------------------

function PropRow({
  label,
  children,
}: {
  label: string;
  children: React.ReactNode;
}) {
  // Subgrid row: the parent declares `grid-cols-[auto_1fr]`, so the label
  // column auto-sizes to the widest label across all rows (e.g. the longer
  // Russian "Ответственный") instead of a fixed w-16 the label overflowed.
  return (
    <div className="-mx-2 col-span-2 grid min-h-8 grid-cols-subgrid items-center rounded-md px-2 transition-colors hover:bg-accent/50">
      <span className="whitespace-nowrap text-caption text-muted-foreground">{label}</span>
      <div className="flex min-w-0 flex-1 items-center gap-1.5 text-caption truncate">
        {children}
      </div>
    </div>
  );
}

// ---------------------------------------------------------------------------
// ProjectDetail
// ---------------------------------------------------------------------------

export function ProjectDetail({ projectId }: { projectId: string }) {
  const { t } = useT("projects");
  const statusLabels = useProjectStatusLabels();
  const priorityLabels = useProjectPriorityLabels();
  const projectTypeLabels = useProjectTypeLabels();
  const wsId = useWorkspaceId();
  const wsPaths = useWorkspacePaths();
  const router = useNavigation();
  const userId = useAuthStore((s) => s.user?.id);
  const { role: currentRole } = useCurrentMember(wsId);
  const isBillingStaff = currentRole === "owner" || currentRole === "admin";
  const { data: project, isLoading } = useQuery(projectDetailOptions(wsId, projectId));
  const recordRecentContext = useRecentContextStore((s) => s.recordVisit);
  useEffect(() => {
    if (project) {
      recordRecentContext(wsId, {
        type: "project",
        id: project.id,
        label: project.title,
        subtitle: project.description ?? undefined,
        icon: project.icon,
        projectStatus: project.status,
      });
    }
  }, [project?.id, project?.title, project?.description, project?.icon, project?.status, recordRecentContext, wsId]);
  const issueScope = useMemo(
    () => ({ type: "project" as const, projectId }),
    [projectId],
  );
  const { data: members = [] } = useQuery(memberListOptions(wsId));
  const { data: agents = [] } = useQuery(agentListOptions(wsId));
  const { getActorName } = useActorName();
  const updateProject = useUpdateProject();
  const deleteProject = useDeleteProject();
  const { data: pinnedItems = [] } = useQuery({
    ...pinListOptions(wsId, userId ?? ""),
    enabled: !!userId,
  });
  const isPinned = pinnedItems.some((p) => p.item_type === "project" && p.item_id === projectId);
  const isWorkspaceAdmin = useMemo(() => {
    if (!userId) return false;
    const me = members.find((m) => m.user_id === userId);
    return me?.role === "owner" || me?.role === "admin";
  }, [members, userId]);
  const createPin = useCreatePin();
  const deletePinMut = useDeletePin();
  const descEditorRef = useRef<ContentEditorRef>(null);
  const isMobile = useIsMobile();
  const [deleteDialogOpen, setDeleteDialogOpen] = useState(false);
  const [iconPickerOpen, setIconPickerOpen] = useState(false);
  const [propertiesOpen, setPropertiesOpen] = useState(true);
  const [progressOpen, setProgressOpen] = useState(true);
  const [descriptionOpen, setDescriptionOpen] = useState(true);
  const [peopleOpen, setPeopleOpen] = useState(true);
  // Guest create/unbind (right-panel People section). Guests exist to be picked
  // when sharing a SitePing link, so they are managed here at the project.
  const qc = useQueryClient();
  const [guestFormOpen, setGuestFormOpen] = useState(false);
  const [guestName, setGuestName] = useState("");
  const [guestEmail, setGuestEmail] = useState("");
  const [guestCreating, setGuestCreating] = useState(false);
  const [guestActionId, setGuestActionId] = useState<string | null>(null);

  // SitePing share links — copied per member from the People section. Site URL
  // (meta) + existing tokens are loaded here so a member/guest row can mint or
  // reuse a token and copy its link in one click. Same query keys as the
  // SitePing integration section → shared cache, mutual invalidation.
  const { data: spMeta } = useQuery({ queryKey: sitepingKeys.meta(projectId), queryFn: () => fetchSitepingMeta(projectId) });
  const { data: spTokens = [] } = useQuery({ queryKey: sitepingKeys.tokens(projectId), queryFn: () => fetchSitepingTokens(projectId) });
  const [copyingMemberId, setCopyingMemberId] = useState<string | null>(null);

  // Sidebar panel
  const { defaultLayout, onLayoutChanged } = useDefaultLayout({
    id: "multica_project_detail_layout",
  });
  const sidebarRef = usePanelRef();
  const desktopSidebarInitialOpen = getAnimatedRightSidebarInitialOpen(
    true,
    defaultLayout,
  );
  // Desktop and mobile sidebar state must be separate. A single state defaulting
  // to `true` made the mobile <Sheet> mount in the open position on first render
  // (after `useIsMobile()` flipped from false→true), briefly covering the page
  // with its modal backdrop and locking scroll — leaving the page unresponsive.
  const {
    open: desktopSidebarOpen,
    visualOpen: desktopSidebarVisualOpen,
    motionEnabled: desktopSidebarMotionEnabled,
    beginToggle: beginDesktopSidebarToggle,
    handleResize: handleDesktopSidebarResize,
  } = useAnimatedRightSidebarState(desktopSidebarInitialOpen);
  const [mobileSidebarOpen, setMobileSidebarOpen] = useState(false);
  const sidebarOpen = isMobile ? mobileSidebarOpen : desktopSidebarOpen;

  useEffect(() => {
    if (isMobile) {
      setMobileSidebarOpen(false);
    }
  }, [isMobile]);

  const handleToggleSidebar = useCallback(() => {
    if (isMobile) {
      setMobileSidebarOpen((open) => !open);
      return;
    }

    const panel = sidebarRef.current;
    if (!panel) return;
    const nextOpen = panel.isCollapsed();
    beginDesktopSidebarToggle(nextOpen);
    window.requestAnimationFrame(() => {
      if (nextOpen) panel.expand();
      else panel.collapse();
    });
  }, [beginDesktopSidebarToggle, isMobile, sidebarRef]);

  // Lead popover
  const [leadOpen, setLeadOpen] = useState(false);
  const [leadFilter, setLeadFilter] = useState("");
  const leadQuery = leadFilter.toLowerCase();
  const filteredMembers = members.filter((m) => m.name.toLowerCase().includes(leadQuery) || matchesPinyin(m.name, leadQuery));
  const filteredAgents = agents.filter((a) => !a.archived_at && (a.name.toLowerCase().includes(leadQuery) || matchesPinyin(a.name, leadQuery)));

  const handleUpdateField = useCallback(
    (data: Parameters<typeof updateProject.mutate>[0] extends { id: string } & infer R ? R : never) => {
      if (!project) return;
      updateProject.mutate({ id: project.id, ...data });
    },
    [project, updateProject],
  );

  const handleDelete = useCallback(() => {
    if (!project) return;
    deleteProject.mutate(project.id, {
      onSuccess: () => {
        toast.success(t(($) => $.detail.toast_project_deleted));
        router.push(wsPaths.projects());
      },
    });
  }, [project, deleteProject, router, wsPaths, t]);

  if (isLoading) {
    return (
      <div className="mx-auto w-full max-w-4xl px-8 py-10 space-y-4">
        <Skeleton className="h-5 w-32" />
        <Skeleton className="h-8 w-64" />
        <Skeleton className="h-4 w-96" />
        <Skeleton className="h-40 w-full mt-8" />
      </div>
    );
  }

  if (!project) {
    return <div className="flex items-center justify-center h-full text-muted-foreground">{t(($) => $.detail.not_found)}</div>;
  }

  const issueMetrics = getProjectIssueMetrics(project);
  const statusCfg = PROJECT_STATUS_CONFIG[project.status];

  // People with access to this project. Regular workspace roles (owner/admin/
  // member) see every project — they form the "team". Guests (P10, widget-only)
  // are scoped per-project: only those whose guest_project_ids includes this
  // project are shown. Read-only here; bind/unbind lives in Settings → Members.
  const teamMembers = members.filter((m) => (m.role as string) !== "guest");
  const boundGuests = members.filter(
    (m) => (m.role as string) === "guest" && (m.guest_project_ids ?? []).includes(projectId),
  );
  const currentMember = members.find((m) => m.user_id === userId) ?? null;
  const canManageGuests = currentMember?.role === "owner" || currentMember?.role === "admin";

  const handleCreateGuest = async () => {
    const email = guestEmail.trim();
    if (!email) return;
    setGuestCreating(true);
    try {
      // Provisions user + guest member and binds it to this project in one call.
      await api.createSitepingGuest(wsId, { email, name: guestName.trim() || undefined, project_id: projectId });
      await qc.invalidateQueries({ queryKey: workspaceKeys.members(wsId) });
      setGuestName("");
      setGuestEmail("");
      setGuestFormOpen(false);
      toast.success(t(($) => $.people.guest_created_toast));
    } catch (e) {
      toast.error(e instanceof Error ? e.message : t(($) => $.people.guest_create_failed));
    } finally {
      setGuestCreating(false);
    }
  };

  const handleUnbindGuest = async (memberId: string) => {
    setGuestActionId(memberId);
    try {
      await api.unsetGuestProject(wsId, memberId, projectId);
      await qc.invalidateQueries({ queryKey: workspaceKeys.members(wsId) });
      toast.success(t(($) => $.people.guest_unbound_toast));
    } catch (e) {
      toast.error(e instanceof Error ? e.message : t(($) => $.people.guest_unbind_failed));
    } finally {
      setGuestActionId(null);
    }
  };

  // Mint-or-reuse a SitePing token for this member and copy the shareable link.
  // Works for team members and guests alike; guests must already be bound to the
  // project (the gate is on the comment side, not here).
  const handleCopyShareLink = async (member: { id: string; name: string; email: string }) => {
    const siteUrl = spMeta?.siteUrl;
    if (!siteUrl) {
      toast.error(t(($) => $.people.copy_link_no_site_url));
      return;
    }
    setCopyingMemberId(member.id);
    try {
      const token = await ensureSitepingTokenForEmail(projectId, spTokens, member);
      await qc.invalidateQueries({ queryKey: sitepingKeys.tokens(projectId) });
      await navigator.clipboard.writeText(buildSitepingShareUrl(siteUrl, token));
      toast.success(t(($) => $.people.copy_link_copied));
    } catch (e) {
      toast.error(e instanceof Error ? e.message : t(($) => $.people.copy_link_failed));
    } finally {
      setCopyingMemberId(null);
    }
  };

  const sidebarContent = (
    <div className="space-y-5">
      {/* Icon + Title */}
      <div>
        <Popover open={iconPickerOpen} onOpenChange={setIconPickerOpen}>
          <PopoverTrigger
            render={
              <button
                type="button"
                className="text-display-sm cursor-pointer rounded-lg p-1 -ml-1 hover:bg-accent/60 transition-colors"
                title={t(($) => $.detail.icon_tooltip)}
              >
                {project.icon || "📁"}
              </button>
            }
          />
          <PopoverContent align="start" className="w-auto p-0">
            <EmojiPicker
              onSelect={(emoji) => {
                handleUpdateField({ icon: emoji });
                setIconPickerOpen(false);
              }}
            />
          </PopoverContent>
        </Popover>
        <TitleEditor
          key={`title-${projectId}`}
          defaultValue={project.title}
          placeholder={t(($) => $.detail.title_placeholder)}
          className="mt-2 w-full text-title-sm font-semibold leading-snug tracking-tight"
          onBlur={(value) => {
            const trimmed = value.trim();
            if (trimmed && trimmed !== project.title) handleUpdateField({ title: trimmed });
          }}
        />
      </div>

      {/* Properties */}
      <div>
        <button
          type="button"
          className={`flex w-full items-center gap-1 rounded-md px-2 py-1 text-caption font-medium transition-colors mb-2 hover:bg-accent/70 ${propertiesOpen ? "" : "text-muted-foreground hover:text-foreground"}`}
          onClick={() => setPropertiesOpen(!propertiesOpen)}
        >
          {t(($) => $.detail.section_properties)}
          <ChevronRight className={`!size-3 shrink-0 stroke-[2.5] text-muted-foreground transition-transform ${propertiesOpen ? "rotate-90" : ""}`} />
        </button>
        {propertiesOpen && <div className="grid grid-cols-[auto_1fr] gap-x-2 gap-y-0.5 pl-2">
          <PropRow label={t(($) => $.table.status)}>
            <DropdownMenu>
              <DropdownMenuTrigger
                render={
                  <button type="button" className="inline-flex items-center gap-1.5 text-caption hover:text-foreground transition-colors">
                    <span className={cn("size-2 rounded-full", statusCfg.dotColor)} />
                    <span>{statusLabels[project.status]}</span>
                  </button>
                }
              />
              <DropdownMenuContent align="start" className="w-44">
                {PROJECT_STATUS_ORDER.map((s) => (
                  <DropdownMenuItem key={s} onClick={() => handleUpdateField({ status: s as ProjectStatus })}>
                    <span className={cn("size-2 rounded-full", PROJECT_STATUS_CONFIG[s].dotColor)} />
                    <span>{statusLabels[s]}</span>
                    {s === project.status && <Check className="ml-auto h-3.5 w-3.5" />}
                  </DropdownMenuItem>
                ))}
              </DropdownMenuContent>
            </DropdownMenu>
          </PropRow>
          <PropRow label={t(($) => $.table.priority)}>
            <DropdownMenu>
              <DropdownMenuTrigger
                render={
                  <button type="button" className="inline-flex items-center gap-1.5 text-caption hover:text-foreground transition-colors">
                    <PriorityIcon priority={project.priority} />
                    <span>{priorityLabels[project.priority]}</span>
                  </button>
                }
              />
              <DropdownMenuContent align="start" className="w-44">
                {PROJECT_PRIORITY_ORDER.map((p) => (
                  <DropdownMenuItem key={p} onClick={() => handleUpdateField({ priority: p as ProjectPriority })}>
                    <PriorityIcon priority={p} />
                    <span>{priorityLabels[p]}</span>
                    {p === project.priority && <Check className="ml-auto h-3.5 w-3.5" />}
                  </DropdownMenuItem>
                ))}
              </DropdownMenuContent>
            </DropdownMenu>
          </PropRow>
          <PropRow label={t(($) => $.type.label)}>
            <DropdownMenu>
              <DropdownMenuTrigger
                render={
                  <button type="button" className="inline-flex items-center gap-1.5 text-xs hover:text-foreground transition-colors">
                    <span className={project.project_type ? "" : "text-muted-foreground"}>
                      {project.project_type
                        ? projectTypeLabels[project.project_type]
                        : t(($) => $.type.unspecified)}
                    </span>
                  </button>
                }
              />
              <DropdownMenuContent align="start" className="w-56">
                <DropdownMenuItem onClick={() => handleUpdateField({ project_type: null })}>
                  <span className="text-muted-foreground">{t(($) => $.type.unspecified)}</span>
                  {project.project_type === null && <Check className="ml-auto h-3.5 w-3.5" />}
                </DropdownMenuItem>
                {PROJECT_TYPE_ORDER.map((value) => (
                  <DropdownMenuItem
                    key={value}
                    onClick={() =>
                      handleUpdateField(
                        value === "development"
                          ? { project_type: value }
                          : { project_type: value, due_date: null },
                      )
                    }
                  >
                    <span>{projectTypeLabels[value]}</span>
                    {value === project.project_type && <Check className="ml-auto h-3.5 w-3.5" />}
                  </DropdownMenuItem>
                ))}
              </DropdownMenuContent>
            </DropdownMenu>
          </PropRow>
          <PropRow label={t(($) => $.table.lead)}>
            <Popover open={leadOpen} onOpenChange={(v) => { setLeadOpen(v); if (!v) setLeadFilter(""); }}>
              <PopoverTrigger
                render={
                  <button type="button" className="inline-flex items-center gap-1.5 text-caption hover:text-foreground transition-colors">
                    {project.lead_type && project.lead_id ? (
                      <>
                        <ActorAvatar actorType={project.lead_type} actorId={project.lead_id} size="sm" enableHoverCard showStatusDot />
                        <span className="cursor-pointer">{getActorName(project.lead_type, project.lead_id)}</span>
                      </>
                    ) : (
                      <span className="text-muted-foreground">{t(($) => $.lead.no_lead)}</span>
                    )}
                  </button>
                }
              />
              <PopoverContent align="start" className="w-52 p-0">
                <div className="px-2 py-1.5 border-b">
                  <input
                    type="text"
                    value={leadFilter}
                    onChange={(e) => setLeadFilter(e.target.value)}
                    placeholder={t(($) => $.lead.assign_placeholder)}
                    className="w-full bg-transparent text-body placeholder:text-muted-foreground outline-none"
                  />
                </div>
                <div className="p-1 max-h-60 overflow-y-auto">
                  <button
                    type="button"
                    onClick={() => { handleUpdateField({ lead_type: null, lead_id: null }); setLeadOpen(false); }}
                    className="flex w-full items-center gap-2 rounded-md px-2 py-1.5 text-body hover:bg-accent transition-colors"
                  >
                    <UserMinus className="h-3.5 w-3.5 text-muted-foreground" />
                    <span className="text-muted-foreground">{t(($) => $.lead.no_lead)}</span>
                  </button>
                  {filteredMembers.length > 0 && (
                    <>
                      <div className="px-2 pt-2 pb-1 text-caption font-medium text-muted-foreground uppercase tracking-wider">{t(($) => $.lead.members_group)}</div>
                      {filteredMembers.map((m) => (
                        <button
                          type="button"
                          key={m.user_id}
                          onClick={() => { handleUpdateField({ lead_type: "member", lead_id: m.user_id }); setLeadOpen(false); }}
                          className="flex w-full items-center gap-2 rounded-md px-2 py-1.5 text-body hover:bg-accent transition-colors"
                        >
                          <ActorAvatar actorType="member" actorId={m.user_id} size="sm" />
                          <span>{m.name}</span>
                        </button>
                      ))}
                    </>
                  )}
                  {filteredAgents.length > 0 && (
                    <>
                      <div className="px-2 pt-2 pb-1 text-caption font-medium text-muted-foreground uppercase tracking-wider">{t(($) => $.lead.agents_group)}</div>
                      {filteredAgents.map((a) => (
                        <button
                          type="button"
                          key={a.id}
                          onClick={() => { handleUpdateField({ lead_type: "agent", lead_id: a.id }); setLeadOpen(false); }}
                          className="flex w-full items-center gap-2 rounded-md px-2 py-1.5 text-body hover:bg-accent transition-colors"
                        >
                          <ActorAvatar actorType="agent" actorId={a.id} size="sm" showStatusDot />
                          <span>{a.name}</span>
                        </button>
                      ))}
                    </>
                  )}
                  {filteredMembers.length === 0 && filteredAgents.length === 0 && leadFilter && (
                    <div className="px-2 py-3 text-center text-body text-muted-foreground">{t(($) => $.lead.no_results)}</div>
                  )}
                </div>
              </PopoverContent>
            </Popover>
          </PropRow>
          <PropRow label={t(($) => $.detail.prop_start_date)}>
            <ProjectStartDatePicker startDate={project.start_date} onUpdate={handleUpdateField} />
          </PropRow>
          {(!project.project_type || project.project_type === "development") && (
            <PropRow
              label={
                project.project_type === "development"
                  ? t(($) => $.detail.prop_contract_due_date)
                  : t(($) => $.detail.prop_due_date)
              }
            >
              <ProjectDueDatePicker dueDate={project.due_date} onUpdate={handleUpdateField} />
            </PropRow>
          )}
        </div>}
      </div>

      {/* Progress */}
      {issueMetrics.totalCount > 0 && (() => {
        const pct = Math.round((issueMetrics.completedCount / issueMetrics.totalCount) * 100);
        return (
          <div>
            <button
              type="button"
              className={`flex w-full items-center gap-1 rounded-md px-2 py-1 text-caption font-medium transition-colors mb-2 hover:bg-accent/70 ${progressOpen ? "" : "text-muted-foreground hover:text-foreground"}`}
              onClick={() => setProgressOpen(!progressOpen)}
            >
              {t(($) => $.detail.section_progress)}
              <ChevronRight className={`!size-3 shrink-0 stroke-[2.5] text-muted-foreground transition-transform ${progressOpen ? "rotate-90" : ""}`} />
            </button>
            {progressOpen && <div className="pl-2 flex items-center gap-3">
              <div className="relative h-2 flex-1 rounded-full bg-muted overflow-hidden">
                <div
                  className="absolute inset-y-0 left-0 rounded-full bg-emerald-500 transition-all"
                  style={{ width: `${pct}%` }}
                />
              </div>
              <span className="text-caption text-muted-foreground tabular-nums shrink-0">
                {issueMetrics.completedCount}/{issueMetrics.totalCount}
              </span>
            </div>}
          </div>
        );
      })()}

      {/* Description */}
      <div>
        <button
          type="button"
          className={`flex w-full items-center gap-1 rounded-md px-2 py-1 text-caption font-medium transition-colors mb-2 hover:bg-accent/70 ${descriptionOpen ? "" : "text-muted-foreground hover:text-foreground"}`}
          onClick={() => setDescriptionOpen(!descriptionOpen)}
        >
          {t(($) => $.detail.section_description)}
          <ChevronRight className={`!size-3 shrink-0 stroke-[2.5] text-muted-foreground transition-transform ${descriptionOpen ? "rotate-90" : ""}`} />
        </button>
        {descriptionOpen && <div className="pl-2">
          <ContentEditor
            ref={descEditorRef}
            key={projectId}
            value={project.description || ""}
            placeholder={t(($) => $.detail.description_placeholder)}
            onUpdate={(md) => handleUpdateField({ description: md || null })}
            debounceMs={1500}
          />
          <p className="mt-1 px-2 text-caption text-muted-foreground">
            {t(($) => $.detail.description_hint)}
          </p>
        </div>}
      </div>

      {/* People */}
      <div>
        <button
          type="button"
          className={`flex w-full items-center gap-1 rounded-md px-2 py-1 text-xs font-medium transition-colors mb-2 hover:bg-accent/70 ${peopleOpen ? "" : "text-muted-foreground hover:text-foreground"}`}
          onClick={() => setPeopleOpen(!peopleOpen)}
        >
          {t(($) => $.detail.section_people)}
          <ChevronRight className={`!size-3 shrink-0 stroke-[2.5] text-muted-foreground transition-transform ${peopleOpen ? "rotate-90" : ""}`} />
        </button>
        {peopleOpen && <div className="pl-2 space-y-3">
          {teamMembers.length > 0 && (
            <div className="space-y-0.5">
              <div className="px-1 pb-1 text-xs font-medium text-muted-foreground uppercase tracking-wider">{t(($) => $.people.team_group)}</div>
              {teamMembers.map((m) => {
                const isLead = project.lead_type === "member" && project.lead_id === m.user_id;
                return (
                  <div key={m.user_id} className="group/member flex items-center gap-2 rounded-md px-1 py-1 text-xs hover:bg-accent/50">
                    <ActorAvatar actorType="member" actorId={m.user_id} size="xs" enableHoverCard showStatusDot />
                    <span className="truncate">{m.name}</span>
                    {isLead && (
                      <span className="shrink-0 rounded bg-muted px-1.5 py-0.5 text-[10px] text-muted-foreground">{t(($) => $.table.lead)}</span>
                    )}
                    <button
                      type="button"
                      disabled={copyingMemberId === m.user_id}
                      onClick={() => handleCopyShareLink({ id: m.user_id, name: m.name, email: m.email })}
                      title={t(($) => $.people.copy_link_title)}
                      className="ml-auto shrink-0 rounded p-0.5 text-muted-foreground opacity-0 transition-opacity hover:bg-accent hover:text-foreground group-hover/member:opacity-100 disabled:opacity-50"
                    >
                      <Link2 className="size-3" />
                    </button>
                  </div>
                );
              })}
            </div>
          )}
          <div className="space-y-0.5">
            <div className="flex items-center justify-between gap-2 px-1 pb-1">
              <span className="text-xs font-medium text-muted-foreground uppercase tracking-wider">{t(($) => $.people.guests_group)}</span>
              {canManageGuests && (
                <button
                  type="button"
                  onClick={() => setGuestFormOpen((v) => !v)}
                  className="inline-flex shrink-0 items-center gap-1 rounded px-1.5 py-0.5 text-[11px] text-muted-foreground transition-colors hover:bg-accent hover:text-foreground"
                >
                  <UserPlus className="size-3" />
                  {t(($) => $.people.guest_create_button)}
                </button>
              )}
            </div>

            {guestFormOpen && canManageGuests && (
              <div className="mb-1 space-y-1.5 rounded-md border bg-muted/30 p-2">
                <input
                  type="text"
                  value={guestName}
                  onChange={(e) => setGuestName(e.target.value)}
                  placeholder={t(($) => $.people.guest_name_placeholder)}
                  className="h-7 w-full rounded-md border bg-transparent px-2 text-xs outline-none focus-visible:ring-1 focus-visible:ring-ring"
                />
                <input
                  type="email"
                  value={guestEmail}
                  onChange={(e) => setGuestEmail(e.target.value)}
                  onKeyDown={(e) => { if (e.key === "Enter" && guestEmail.trim() && !guestCreating) handleCreateGuest(); }}
                  placeholder={t(($) => $.people.guest_email_placeholder)}
                  className="h-7 w-full rounded-md border bg-transparent px-2 text-xs outline-none focus-visible:ring-1 focus-visible:ring-ring"
                />
                <div className="flex justify-end gap-1.5">
                  <Button type="button" variant="ghost" size="sm" onClick={() => { setGuestFormOpen(false); setGuestName(""); setGuestEmail(""); }} className="h-6 px-2 text-[11px]">
                    {t(($) => $.people.guest_cancel)}
                  </Button>
                  <Button type="button" size="sm" disabled={!guestEmail.trim() || guestCreating} onClick={handleCreateGuest} className="h-6 px-2 text-[11px]">
                    {guestCreating ? t(($) => $.people.guest_creating) : t(($) => $.people.guest_create)}
                  </Button>
                </div>
              </div>
            )}

            {boundGuests.map((m) => (
              <div key={m.user_id} className="group/guest flex items-center gap-2 rounded-md px-1 py-1 text-xs hover:bg-accent/50">
                <ActorAvatar actorType="member" actorId={m.user_id} size="xs" enableHoverCard />
                <span className="truncate">{m.name}</span>
                <span className="ml-auto shrink-0 rounded bg-amber-500/15 px-1.5 py-0.5 text-[10px] text-amber-600 dark:text-amber-400">{t(($) => $.people.guest_badge)}</span>
                <button
                  type="button"
                  disabled={copyingMemberId === m.user_id}
                  onClick={() => handleCopyShareLink({ id: m.user_id, name: m.name, email: m.email })}
                  title={t(($) => $.people.copy_link_title)}
                  className="shrink-0 rounded p-0.5 text-muted-foreground opacity-0 transition-opacity hover:bg-accent hover:text-foreground group-hover/guest:opacity-100 disabled:opacity-50"
                >
                  <Link2 className="size-3" />
                </button>
                {canManageGuests && (
                  <button
                    type="button"
                    disabled={guestActionId === m.id}
                    onClick={() => handleUnbindGuest(m.id)}
                    title={t(($) => $.people.guest_unbind_title)}
                    className="shrink-0 rounded p-0.5 text-muted-foreground opacity-0 transition-opacity hover:bg-accent hover:text-destructive group-hover/guest:opacity-100 disabled:opacity-50"
                  >
                    <X className="size-3" />
                  </button>
                )}
              </div>
            ))}
            {boundGuests.length === 0 && !guestFormOpen && (
              <div className="px-1 py-1 text-[11px] italic text-muted-foreground">{t(($) => $.people.no_guests)}</div>
            )}
          </div>
        </div>}
      </div>

      {/* Resources */}
      <ProjectResourcesSection projectId={projectId} />
      <SitepingIntegrationSection projectId={projectId} />

      {/* Agency billing config (fork) — owner/admin only. Every endpoint it
          talks to is gated on the billing role server-side; mounting it for an
          employee would render a section of failed requests. */}
      {isBillingStaff && <ProjectBillingSection projectId={projectId} />}
    </div>
  );

  return (
    <>
    <ResizablePanelGroup orientation="horizontal" className="flex-1 min-h-0" defaultLayout={defaultLayout} onLayoutChanged={onLayoutChanged}>
      <ResizablePanel id="content" minSize="50%">
        <div className="flex h-full flex-col">
          <BreadcrumbHeader
            segments={[{ href: wsPaths.projects(), label: t(($) => $.detail.breadcrumb_fallback) }]}
            leaf={<span className="truncate font-medium text-foreground">{project.title}</span>}
            actions={
              <>
              <Button
                variant="ghost"
                size="icon-sm"
                className={cn("text-muted-foreground", isPinned && "text-foreground")}
                title={isPinned ? t(($) => $.detail.unpin_tooltip) : t(($) => $.detail.pin_tooltip)}
                onClick={() => {
                  if (isPinned) {
                    deletePinMut.mutate({ itemType: "project", itemId: projectId });
                  } else {
                    createPin.mutate({ item_type: "project", item_id: projectId });
                  }
                }}
              >
                {isPinned ? <PinOff /> : <Pin />}
              </Button>
              <DropdownMenu>
                <DropdownMenuTrigger
                  render={
                    <Button variant="ghost" size="icon-sm" className="text-muted-foreground">
                      <MoreHorizontal />
                    </Button>
                  }
                />
                <DropdownMenuContent align="end" className="w-auto">
                  <DropdownMenuItem onClick={() => {
                    void copyText(window.location.href).then((ok) => {
                      if (ok) toast.success(t(($) => $.detail.toast_link_copied));
                    });
                  }}>
                    <Link2 className="h-3.5 w-3.5" />
                    {t(($) => $.detail.copy_link)}
                  </DropdownMenuItem>
                  {isWorkspaceAdmin && (
                    <>
                      <DropdownMenuSeparator />
                      <DropdownMenuItem
                        variant="destructive"
                        onClick={() => setDeleteDialogOpen(true)}
                      >
                        <Trash2 className="h-3.5 w-3.5" />
                        {t(($) => $.detail.delete_action)}
                      </DropdownMenuItem>
                    </>
                  )}
                </DropdownMenuContent>
              </DropdownMenu>
              <Tooltip>
                <TooltipTrigger
                  render={
                    <Button
                      variant={sidebarOpen ? "secondary" : "ghost"}
                      size="icon-sm"
                      className={sidebarOpen ? "" : "text-muted-foreground"}
                      onClick={handleToggleSidebar}
                    >
                      <PanelRight />
                    </Button>
                  }
                />
                <TooltipContent side="bottom">{t(($) => $.detail.sidebar_tooltip)}</TooltipContent>
              </Tooltip>
              </>
            }
          />

          <IssueSurface
            scope={issueScope}
            modes={["board", "list", "table", "swimlane", "gantt"]}
          />
          </div>
        </ResizablePanel>
        {!isMobile && <ResizableHandle />}
        {!isMobile && (
        <ResizablePanel
          id="sidebar"
          {...rightSidebarPanelMotionProps}
          data-right-sidebar-motion={desktopSidebarMotionEnabled ? "enabled" : undefined}
          defaultSize={desktopSidebarOpen ? 320 : 0}
          minSize={260}
          maxSize={420}
          collapsible
          groupResizeBehavior="preserve-pixel-size"
          panelRef={sidebarRef}
          onResize={handleDesktopSidebarResize}
        >
          <AnimatedRightSidebar open={desktopSidebarVisualOpen} motionEnabled={desktopSidebarMotionEnabled}>
            {sidebarContent}
          </AnimatedRightSidebar>
        </ResizablePanel>
        )}
        {isMobile && (
          <Sheet open={mobileSidebarOpen} onOpenChange={setMobileSidebarOpen}>
            <SheetContent side="right" showCloseButton={false} className="w-[320px] overflow-y-auto p-4">
              {sidebarContent}
            </SheetContent>
          </Sheet>
        )}
      </ResizablePanelGroup>

      {/* Delete confirmation */}
      {isWorkspaceAdmin && (
        <AlertDialog open={deleteDialogOpen} onOpenChange={setDeleteDialogOpen}>
          <AlertDialogContent>
            <AlertDialogHeader>
              <AlertDialogTitle>{t(($) => $.delete_dialog.title)}</AlertDialogTitle>
              <AlertDialogDescription>
                {t(($) => $.delete_dialog.description)}
              </AlertDialogDescription>
            </AlertDialogHeader>
            <AlertDialogFooter>
              <AlertDialogCancel>{t(($) => $.delete_dialog.cancel)}</AlertDialogCancel>
              <AlertDialogAction onClick={handleDelete} className="bg-destructive text-white hover:bg-destructive/90">
                {t(($) => $.delete_dialog.confirm)}
              </AlertDialogAction>
            </AlertDialogFooter>
          </AlertDialogContent>
        </AlertDialog>
      )}
    </>
  );
}
