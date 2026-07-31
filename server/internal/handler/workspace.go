package handler

import (
	"encoding/json"
	"fmt"
	"log/slog"
	"net/http"
	"regexp"
	"strings"

	"github.com/go-chi/chi/v5"
	"github.com/jackc/pgx/v5/pgtype"
	"github.com/multica-ai/multica/server/internal/analytics"
	"github.com/multica-ai/multica/server/internal/logger"
	obsmetrics "github.com/multica-ai/multica/server/internal/metrics"
	"github.com/multica-ai/multica/server/internal/util"
	db "github.com/multica-ai/multica/server/pkg/db/generated"
	"github.com/multica-ai/multica/server/pkg/protocol"
)

var nonAlpha = regexp.MustCompile(`[^a-zA-Z]`)
var workspaceSlugPattern = regexp.MustCompile(`^[a-z0-9]+(?:-[a-z0-9]+)*$`)

// generateIssuePrefix produces a 2-5 char uppercase prefix from a workspace name.
// Examples: "Jiayuan's Workspace" → "JIA", "My Team" → "MYT", "AB" → "AB".
func generateIssuePrefix(name string) string {
	letters := nonAlpha.ReplaceAllString(name, "")
	if len(letters) == 0 {
		return "WS"
	}
	letters = strings.ToUpper(letters)
	if len(letters) > 3 {
		letters = letters[:3]
	}
	return letters
}

type WorkspaceResponse struct {
	ID          string  `json:"id"`
	Name        string  `json:"name"`
	Slug        string  `json:"slug"`
	Description *string `json:"description"`
	Context     *string `json:"context"`
	Settings    any     `json:"settings"`
	Repos       any     `json:"repos"`
	IssuePrefix string  `json:"issue_prefix"`
	AvatarURL   *string `json:"avatar_url"`
	CreatedAt   string  `json:"created_at"`
	UpdatedAt   string  `json:"updated_at"`
}

func (h *Handler) workspaceToResponse(w db.Workspace) WorkspaceResponse {
	var settings any
	if w.Settings != nil {
		json.Unmarshal(w.Settings, &settings)
	}
	if settings == nil {
		settings = map[string]any{}
	}
	var repos any
	if w.Repos != nil {
		json.Unmarshal(w.Repos, &repos)
	}
	if repos == nil {
		repos = []any{}
	}
	return WorkspaceResponse{
		ID:          uuidToString(w.ID),
		Name:        w.Name,
		Slug:        w.Slug,
		Description: textToPtr(w.Description),
		Context:     textToPtr(w.Context),
		Settings:    settings,
		Repos:       repos,
		IssuePrefix: w.IssuePrefix,
		AvatarURL:   h.resolveAvatarURLPtr(textToPtr(w.AvatarUrl)),
		CreatedAt:   timestampToString(w.CreatedAt),
		UpdatedAt:   timestampToString(w.UpdatedAt),
	}
}

// workspaceBroadcastPayload is the workspace shaped for the realtime room.
//
// The room holds every member, so the payload cannot be shaped per viewer —
// and it carried the full context, which is the billing playbook that
// redactWorkspaceForNonStaff withholds over HTTP. The key is DROPPED rather
// than nulled: clients merge this payload over their cached workspace, so an
// absent key leaves a staff client's cached context intact while a null would
// wipe it.
func workspaceBroadcastPayload(resp WorkspaceResponse) map[string]any {
	raw, err := json.Marshal(resp)
	if err != nil {
		// Fail closed: better a bare id than an accidental full payload.
		return map[string]any{"id": resp.ID}
	}
	var out map[string]any
	if err := json.Unmarshal(raw, &out); err != nil {
		return map[string]any{"id": resp.ID}
	}
	delete(out, "context")
	return out
}

// redactWorkspaceForNonStaff blanks the workspace context for anyone who is
// not owner/admin.
//
// The context is a free-form playbook every agent is fed, and in this
// deployment it is the BILLING playbook: which property marks a task
// internal, what counts as client work, how to report money. Hiding the
// billing property from employees is pointless while the instruction naming
// it is readable by every member.
//
// Agents are unaffected — they receive the context server-side when a task is
// built, not through this response.
func (h *Handler) redactWorkspaceForNonStaff(r *http.Request, resp WorkspaceResponse) WorkspaceResponse {
	if h.callerIsBillingStaff(r, resp.ID) {
		return resp
	}
	resp.Context = nil
	return resp
}

type MemberResponse struct {
	ID          string `json:"id"`
	WorkspaceID string `json:"workspace_id"`
	UserID      string `json:"user_id"`
	Role        string `json:"role"`
	CreatedAt   string `json:"created_at"`
}

func memberToResponse(m db.Member) MemberResponse {
	return MemberResponse{
		ID:          uuidToString(m.ID),
		WorkspaceID: uuidToString(m.WorkspaceID),
		UserID:      uuidToString(m.UserID),
		Role:        m.Role,
		CreatedAt:   timestampToString(m.CreatedAt),
	}
}

func (h *Handler) ListWorkspaces(w http.ResponseWriter, r *http.Request) {
	userID, ok := requireUserID(w, r)
	if !ok {
		return
	}

	workspaces, err := h.Queries.ListWorkspaces(r.Context(), parseUUID(userID))
	if err != nil {
		writeError(w, http.StatusInternalServerError, "failed to list workspaces")
		return
	}

	resp := make([]WorkspaceResponse, len(workspaces))
	for i, ws := range workspaces {
		resp[i] = h.redactWorkspaceForNonStaff(r, h.workspaceToResponse(ws))
	}

	writeJSON(w, http.StatusOK, resp)
}

func (h *Handler) GetWorkspace(w http.ResponseWriter, r *http.Request) {
	id := workspaceIDFromURL(r, "id")
	idUUID, ok := parseUUIDOrBadRequest(w, id, "workspace id")
	if !ok {
		return
	}

	ws, err := h.Queries.GetWorkspace(r.Context(), idUUID)
	if err != nil {
		writeError(w, http.StatusNotFound, "workspace not found")
		return
	}
	writeJSON(w, http.StatusOK, h.redactWorkspaceForNonStaff(r, h.workspaceToResponse(ws)))
}

type CreateWorkspaceRequest struct {
	Name        string  `json:"name"`
	Slug        string  `json:"slug"`
	Description *string `json:"description"`
	Context     *string `json:"context"`
	IssuePrefix *string `json:"issue_prefix"`
}

func (h *Handler) CreateWorkspace(w http.ResponseWriter, r *http.Request) {
	userID, ok := requireUserID(w, r)
	if !ok {
		return
	}

	// Self-host gate (#3433): when the operator has set
	// DISABLE_WORKSPACE_CREATION=true, no caller — including existing
	// workspace owners — may create additional workspaces. The frontend
	// hides every "Create workspace" affordance via /api/config, but the
	// 403 here is the only authoritative check.
	if h.cfg.DisableWorkspaceCreation {
		writeError(w, http.StatusForbidden, "workspace creation is disabled for this instance")
		return
	}

	var req CreateWorkspaceRequest
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
		writeError(w, http.StatusBadRequest, "invalid request body")
		return
	}

	req.Name = strings.TrimSpace(req.Name)
	req.Slug = strings.ToLower(strings.TrimSpace(req.Slug))
	if req.Name == "" || req.Slug == "" {
		writeError(w, http.StatusBadRequest, "name and slug are required")
		return
	}
	if !workspaceSlugPattern.MatchString(req.Slug) {
		writeError(w, http.StatusBadRequest, "slug must contain only lowercase letters, numbers, and hyphens")
		return
	}
	if isReservedSlug(req.Slug) {
		writeError(w, http.StatusBadRequest, "slug is reserved")
		return
	}

	tx, err := h.TxStarter.Begin(r.Context())
	if err != nil {
		writeError(w, http.StatusInternalServerError, "failed to create workspace")
		return
	}
	defer tx.Rollback(r.Context())

	issuePrefix := generateIssuePrefix(req.Name)
	if req.IssuePrefix != nil && strings.TrimSpace(*req.IssuePrefix) != "" {
		issuePrefix = strings.ToUpper(strings.TrimSpace(*req.IssuePrefix))
	}

	qtx := h.Queries.WithTx(tx)
	ws, err := qtx.CreateWorkspace(r.Context(), db.CreateWorkspaceParams{
		Name:        req.Name,
		Slug:        req.Slug,
		Description: ptrToText(req.Description),
		Context:     ptrToText(req.Context),
		IssuePrefix: issuePrefix,
	})
	if err != nil {
		if isUniqueViolation(err) {
			writeError(w, http.StatusConflict, "workspace slug already exists")
			return
		}
		writeError(w, http.StatusInternalServerError, "failed to create workspace: "+err.Error())
		return
	}

	_, err = qtx.CreateMember(r.Context(), db.CreateMemberParams{
		WorkspaceID: ws.ID,
		UserID:      parseUUID(userID),
		Role:        "owner",
	})
	if err != nil {
		writeError(w, http.StatusInternalServerError, "failed to add owner: "+err.Error())
		return
	}

	// NOTE: CreateWorkspace deliberately does NOT mark the user as
	// onboarded. The `onboarded_at` flag is owned by CompleteOnboarding
	// (Step 3 of the flow) and by AcceptInvitation (invitee joining an
	// existing workspace). This decouples "the user has a workspace"
	// from "the user has finished setup"; the workspace-layer route
	// gate (web layout / desktop App.tsx overlay) redirects un-onboarded
	// users back to /onboarding instead.

	if err := tx.Commit(r.Context()); err != nil {
		writeError(w, http.StatusInternalServerError, "failed to create workspace")
		return
	}

	wsID := uuidToString(ws.ID)

	// "Is this the user's first workspace?" is derived in PostHog by looking
	// at whether they have a prior workspace_created event, not stamped at
	// emit time. Stamping here would race under concurrent creates without
	// a schema change, and the event stream answers the question exactly.
	obsmetrics.RecordEvent(h.Analytics, h.Metrics, analytics.WorkspaceCreated(userID, wsID))
	h.notifyDaemonWorkspacesChanged(userID)

	slog.Info("workspace created", append(logger.RequestAttrs(r), "workspace_id", wsID, "name", ws.Name, "slug", ws.Slug)...)
	writeJSON(w, http.StatusCreated, h.workspaceToResponse(ws))
}

type UpdateWorkspaceRequest struct {
	Name        *string `json:"name"`
	Description *string `json:"description"`
	Context     *string `json:"context"`
	Settings    any     `json:"settings"`
	Repos       any     `json:"repos"`
	IssuePrefix *string `json:"issue_prefix"`
	AvatarURL   *string `json:"avatar_url"`
}

type workspaceRepoRef struct {
	URL         string `json:"url"`
	Description string `json:"description,omitempty"`
}

func validateAndNormalizeWorkspaceRepos(value any) ([]byte, error) {
	raw, err := json.Marshal(value)
	if err != nil {
		return nil, err
	}

	var repos []workspaceRepoRef
	if err := json.Unmarshal(raw, &repos); err != nil {
		return nil, fmt.Errorf("repos must be an array of repository objects: %w", err)
	}

	normalized := make([]workspaceRepoRef, 0, len(repos))
	seen := make(map[string]struct{}, len(repos))
	for i, repo := range repos {
		repo.URL = strings.TrimSpace(repo.URL)
		repo.Description = strings.TrimSpace(repo.Description)
		if repo.URL == "" {
			return nil, fmt.Errorf("repos[%d]: url is required", i)
		}
		if !isValidGitRepoURL(repo.URL) {
			return nil, fmt.Errorf("repos[%d]: url must be a valid http(s) or ssh git URL", i)
		}
		if _, ok := seen[repo.URL]; ok {
			continue
		}
		seen[repo.URL] = struct{}{}
		normalized = append(normalized, repo)
	}

	out, err := json.Marshal(normalized)
	if err != nil {
		return nil, err
	}
	return out, nil
}

func (h *Handler) UpdateWorkspace(w http.ResponseWriter, r *http.Request) {
	id := workspaceIDFromURL(r, "id")
	idUUID, ok := parseUUIDOrBadRequest(w, id, "workspace id")
	if !ok {
		return
	}

	var req UpdateWorkspaceRequest
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
		writeError(w, http.StatusBadRequest, "invalid request body")
		return
	}

	params := db.UpdateWorkspaceParams{
		ID: idUUID,
	}
	if req.Name != nil {
		name := strings.TrimSpace(*req.Name)
		if name == "" {
			writeError(w, http.StatusBadRequest, "name is required")
			return
		}
		params.Name = pgtype.Text{String: name, Valid: true}
	}
	if req.Description != nil {
		params.Description = pgtype.Text{String: *req.Description, Valid: true}
	}
	if req.Context != nil {
		params.Context = pgtype.Text{String: *req.Context, Valid: true}
	}
	if req.Settings != nil {
		s, _ := json.Marshal(req.Settings)
		params.Settings = s
	}
	if req.Repos != nil {
		reposJSON, err := validateAndNormalizeWorkspaceRepos(req.Repos)
		if err != nil {
			writeError(w, http.StatusBadRequest, err.Error())
			return
		}
		params.Repos = reposJSON
	}
	if req.IssuePrefix != nil {
		prefix := strings.ToUpper(strings.TrimSpace(*req.IssuePrefix))
		if prefix != "" {
			params.IssuePrefix = pgtype.Text{String: prefix, Valid: true}
		}
	}
	if req.AvatarURL != nil {
		// Read the stored value so an unchanged re-send skips revalidation —
		// this handler is the one avatar writer that doesn't already have the
		// row in hand.
		var current string
		if existing, err := h.Queries.GetWorkspace(r.Context(), idUUID); err == nil {
			current = existing.AvatarUrl.String
		}
		accepted, ok := h.acceptAvatarURL(w, r, *req.AvatarURL, current)
		if !ok {
			return
		}
		params.AvatarUrl = pgtype.Text{String: accepted, Valid: true}
	}

	ws, err := h.Queries.UpdateWorkspace(r.Context(), params)
	if err != nil {
		slog.Warn("update workspace failed", append(logger.RequestAttrs(r), "error", err, "workspace_id", id)...)
		writeError(w, http.StatusInternalServerError, "failed to update workspace: "+err.Error())
		return
	}

	slog.Info("workspace updated", append(logger.RequestAttrs(r), "workspace_id", id)...)
	userID := requestUserID(r)
	h.publish(protocol.EventWorkspaceUpdated, uuidToString(ws.ID), "member", userID, map[string]any{
		"workspace": workspaceBroadcastPayload(h.workspaceToResponse(ws)),
	})
	if req.Name != nil {
		if members, err := h.Queries.ListMembers(r.Context(), ws.ID); err == nil {
			userIDs := make([]string, 0, len(members))
			for _, member := range members {
				userIDs = append(userIDs, uuidToString(member.UserID))
			}
			h.notifyDaemonWorkspacesChanged(userIDs...)
		}
	}

	writeJSON(w, http.StatusOK, h.workspaceToResponse(ws))
}

func (h *Handler) ListMembers(w http.ResponseWriter, r *http.Request) {
	workspaceID := chi.URLParam(r, "id")
	member, ok := h.requireWorkspaceMember(w, r, workspaceID, "workspace not found")
	if !ok {
		return
	}

	members, err := h.Queries.ListMembers(r.Context(), member.WorkspaceID)
	if err != nil {
		writeError(w, http.StatusInternalServerError, "failed to list members")
		return
	}

	resp := make([]MemberResponse, len(members))
	for i, m := range members {
		resp[i] = memberToResponse(m)
	}

	writeJSON(w, http.StatusOK, resp)
}

type MemberWithUserResponse struct {
	ID          string  `json:"id"`
	WorkspaceID string  `json:"workspace_id"`
	UserID      string  `json:"user_id"`
	Role        string  `json:"role"`
	CreatedAt   string  `json:"created_at"`
	Name        string  `json:"name"`
	Email       string  `json:"email"`
	AvatarURL   *string `json:"avatar_url"`
	// AccessScope is "workspace" (sees every project) or "projects" (sees only
	// what ProjectIDs grants). Guests are scoped by their bindings regardless.
	AccessScope string `json:"access_scope"`
	// ProjectIDs lists the projects this person is bound to, for any role.
	ProjectIDs []string `json:"project_ids,omitempty"`
	// GuestProjectIDs is the same list under the name the SitePing bridge and
	// older clients read. Kept because this is an API boundary — installed
	// desktop builds and the out-of-repo bridge both parse it.
	GuestProjectIDs []string `json:"guest_project_ids,omitempty"`
}

func (h *Handler) ListMembersWithUser(w http.ResponseWriter, r *http.Request) {
	workspaceID := workspaceIDFromURL(r, "id")
	wsUUID, ok := parseUUIDOrBadRequest(w, workspaceID, "workspace id")
	if !ok {
		return
	}

	members, err := h.Queries.ListMembersWithUser(r.Context(), wsUUID)
	if err != nil {
		writeError(w, http.StatusInternalServerError, "failed to list members")
		return
	}

	// One read for the whole workspace instead of a query per member: the
	// members screen renders every row's bindings at once.
	bindings := map[string][]string{}
	if rows, berr := h.Queries.ListMemberProjectsByWorkspace(r.Context(), wsUUID); berr == nil {
		for _, row := range rows {
			uid := uuidToString(row.UserID)
			bindings[uid] = append(bindings[uid], uuidToString(row.ProjectID))
		}
	} else {
		slog.Warn("list member projects failed", append(logger.RequestAttrs(r), "error", berr)...)
	}

	resp := make([]MemberWithUserResponse, len(members))
	for i, m := range members {
		resp[i] = MemberWithUserResponse{
			ID:          uuidToString(m.ID),
			WorkspaceID: uuidToString(m.WorkspaceID),
			UserID:      uuidToString(m.UserID),
			Role:        m.Role,
			CreatedAt:   timestampToString(m.CreatedAt),
			Name:        m.UserName,
			Email:       m.UserEmail,
			AvatarURL:   h.resolveAvatarURLPtr(textToPtr(m.UserAvatarUrl)),
			AccessScope: m.AccessScope,
		}
		if ids := bindings[uuidToString(m.UserID)]; len(ids) > 0 {
			resp[i].ProjectIDs = ids
			if m.Role == "guest" {
				resp[i].GuestProjectIDs = ids
			}
		}
	}

	writeJSON(w, http.StatusOK, resp)
}

// GuestProjectRequest is the body for binding/unbinding a member to a project.
// The name is kept because the SitePing bridge posts to /guest-project with
// this shape; the mechanism itself is no longer guest-specific.
type GuestProjectRequest struct {
	ProjectID string `json:"project_id"`
	// Unbind only. What to do with issues in this project that are assigned to
	// the person losing access:
	//
	//	""         — undecided: refuse with 409 and report the count, so the
	//	             operator has to choose rather than silently orphan work
	//	"unassign" — clear the assignee
	//	"reassign" — hand them to ReassignTo (a member user id)
	//
	// Nothing did this before, for any scenario: removing a member left issues
	// pointing at a user row that no longer existed.
	OnAssignedIssues string `json:"on_assigned_issues"`
	ReassignTo       string `json:"reassign_to"`
}

// SetGuestProject binds a guest member to a project (P10 Variant A). Owner/admin
// only (route group). Idempotent. The project must belong to this workspace.
// Binding is allowed for any member row but only takes effect for role=guest
// (the issue gate ignores it otherwise) so you can bind before/after setting
// the role without an ordering trap.
func (h *Handler) SetGuestProject(w http.ResponseWriter, r *http.Request) {
	workspaceID := workspaceIDFromURL(r, "id")
	wsUUID, ok := parseUUIDOrBadRequest(w, workspaceID, "workspace id")
	if !ok {
		return
	}
	memberUUID, ok := parseUUIDOrBadRequest(w, chi.URLParam(r, "memberId"), "memberId")
	if !ok {
		return
	}
	var req GuestProjectRequest
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
		writeError(w, http.StatusBadRequest, "invalid request body")
		return
	}
	projUUID, ok := parseUUIDOrBadRequest(w, req.ProjectID, "project_id")
	if !ok {
		return
	}
	member, err := h.Queries.GetMember(r.Context(), memberUUID)
	if err != nil || uuidToString(member.WorkspaceID) != workspaceID {
		writeError(w, http.StatusNotFound, "member not found")
		return
	}
	project, err := h.Queries.GetProject(r.Context(), projUUID)
	if err != nil || uuidToString(project.WorkspaceID) != workspaceID {
		writeError(w, http.StatusBadRequest, "project not found in this workspace")
		return
	}
	actor, _ := util.ParseUUID(requestUserID(r))
	if _, err := h.Queries.CreateMemberProject(r.Context(), db.CreateMemberProjectParams{
		WorkspaceID: wsUUID,
		UserID:      member.UserID,
		ProjectID:   projUUID,
		CreatedBy:   actor,
	}); err != nil {
		writeError(w, http.StatusInternalServerError, "failed to bind project")
		return
	}
	writeJSON(w, http.StatusOK, map[string]string{"status": "bound"})
}

// UnsetGuestProject removes a guest member's binding to a project. Owner/admin
// only. Idempotent (no-op if the binding does not exist).
func (h *Handler) UnsetGuestProject(w http.ResponseWriter, r *http.Request) {
	workspaceID := workspaceIDFromURL(r, "id")
	memberUUID, ok := parseUUIDOrBadRequest(w, chi.URLParam(r, "memberId"), "memberId")
	if !ok {
		return
	}
	var req GuestProjectRequest
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
		writeError(w, http.StatusBadRequest, "invalid request body")
		return
	}
	projUUID, ok := parseUUIDOrBadRequest(w, req.ProjectID, "project_id")
	if !ok {
		return
	}
	wsUUID, ok := parseUUIDOrBadRequest(w, workspaceID, "workspace id")
	if !ok {
		return
	}
	member, err := h.Queries.GetMember(r.Context(), memberUUID)
	if err != nil || uuidToString(member.WorkspaceID) != workspaceID {
		writeError(w, http.StatusNotFound, "member not found")
		return
	}

	// Work assigned to this person in this project would otherwise be left
	// with an assignee who can no longer see it. Make the caller decide.
	assigned, err := h.Queries.CountIssuesAssignedToMemberInProject(r.Context(), db.CountIssuesAssignedToMemberInProjectParams{
		WorkspaceID: wsUUID,
		ProjectID:   projUUID,
		AssigneeID:  member.UserID,
	})
	if err != nil {
		writeError(w, http.StatusInternalServerError, "failed to count assigned issues")
		return
	}

	var newAssignee pgtype.UUID
	switch {
	case assigned == 0:
		// Nothing to hand over.
	case req.OnAssignedIssues == "unassign":
		// newAssignee stays invalid -> assignee cleared.
	case req.OnAssignedIssues == "reassign":
		target, parsed := parseUUIDOrBadRequest(w, req.ReassignTo, "reassign_to")
		if !parsed {
			return
		}
		targetMember, terr := h.Queries.GetMemberByUserAndWorkspace(r.Context(), db.GetMemberByUserAndWorkspaceParams{
			UserID:      target,
			WorkspaceID: wsUUID,
		})
		if terr != nil {
			writeError(w, http.StatusBadRequest, "reassign_to is not a member of this workspace")
			return
		}
		newAssignee = targetMember.UserID
	default:
		writeJSON(w, http.StatusConflict, map[string]any{
			"error":           "issues are assigned to this member in this project",
			"assigned_issues": assigned,
			"choices":         []string{"unassign", "reassign"},
		})
		return
	}

	tx, err := h.TxStarter.Begin(r.Context())
	if err != nil {
		writeError(w, http.StatusInternalServerError, "failed to unbind project")
		return
	}
	defer tx.Rollback(r.Context())
	qtx := h.Queries.WithTx(tx)

	var reassigned int64
	if assigned > 0 {
		reassigned, err = qtx.ReassignIssuesInProject(r.Context(), db.ReassignIssuesInProjectParams{
			WorkspaceID:   wsUUID,
			ProjectID:     projUUID,
			FromUserID:    member.UserID,
			NewAssigneeID: newAssignee,
		})
		if err != nil {
			writeError(w, http.StatusInternalServerError, "failed to reassign issues")
			return
		}
	}
	if err := qtx.DeleteMemberProject(r.Context(), db.DeleteMemberProjectParams{
		UserID:    member.UserID,
		ProjectID: projUUID,
	}); err != nil {
		writeError(w, http.StatusInternalServerError, "failed to unbind project")
		return
	}
	// One transaction on purpose: between the reassignment and the unbind the
	// issue would sit with an assignee who has already lost the project.
	if err := tx.Commit(r.Context()); err != nil {
		writeError(w, http.StatusInternalServerError, "failed to unbind project")
		return
	}

	writeJSON(w, http.StatusOK, map[string]any{"status": "unbound", "reassigned_issues": reassigned})
}

type CreateMemberRequest struct {
	Email string `json:"email"`
	Role  string `json:"role"`
}

func (h *Handler) memberWithUserResponse(member db.Member, user db.User) MemberWithUserResponse {
	return MemberWithUserResponse{
		ID:          uuidToString(member.ID),
		WorkspaceID: uuidToString(member.WorkspaceID),
		UserID:      uuidToString(member.UserID),
		Role:        member.Role,
		AccessScope: member.AccessScope,
		CreatedAt:   timestampToString(member.CreatedAt),
		Name:        user.Name,
		Email:       user.Email,
		AvatarURL:   h.resolveAvatarURLPtr(textToPtr(user.AvatarUrl)),
	}
}

func normalizeMemberRole(role string) (string, bool) {
	if role == "" {
		return "member", true
	}

	role = strings.TrimSpace(role)
	switch role {
	// "guest" (P10) is a deliberately narrow role for SitePing clients: it can
	// view the board and comment, but is excluded from every owner/admin/member
	// -gated endpoint and is blocked from assigning agents/squads in
	// validateAssigneePair.
	case "owner", "admin", "member", "guest":
		return role, true
	default:
		return "", false
	}
}

func (h *Handler) CreateMember(w http.ResponseWriter, r *http.Request) {
	workspaceID := workspaceIDFromURL(r, "id")
	requester, ok := h.workspaceMember(w, r, workspaceID)
	if !ok {
		return
	}

	var req CreateMemberRequest
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
		writeError(w, http.StatusBadRequest, "invalid request body")
		return
	}

	email := strings.ToLower(strings.TrimSpace(req.Email))
	if email == "" {
		writeError(w, http.StatusBadRequest, "email is required")
		return
	}

	role, valid := normalizeMemberRole(req.Role)
	if !valid {
		writeError(w, http.StatusBadRequest, "invalid member role")
		return
	}
	if role == "owner" && requester.Role != "owner" {
		writeError(w, http.StatusForbidden, "insufficient permissions")
		return
	}

	user, err := h.Queries.GetUserByEmail(r.Context(), email)
	if err != nil {
		if isNotFound(err) {
			// Auto-create user with email so they can be invited before signing up
			user, err = h.Queries.CreateUser(r.Context(), db.CreateUserParams{
				Name:  email,
				Email: email,
			})
			if err != nil {
				writeError(w, http.StatusInternalServerError, "failed to create user")
				return
			}
		} else {
			writeError(w, http.StatusInternalServerError, "failed to load user")
			return
		}
	}

	member, err := h.Queries.CreateMember(r.Context(), db.CreateMemberParams{
		WorkspaceID: requester.WorkspaceID,
		UserID:      user.ID,
		Role:        role,
	})
	if err != nil {
		if isUniqueViolation(err) {
			writeError(w, http.StatusConflict, "user is already a member")
			return
		}
		slog.Warn("create member failed", append(logger.RequestAttrs(r), "error", err, "workspace_id", workspaceID, "email", email)...)
		writeError(w, http.StatusInternalServerError, "failed to create member")
		return
	}

	slog.Info("member added", append(logger.RequestAttrs(r), "member_id", uuidToString(member.ID), "workspace_id", workspaceID, "email", email, "role", role)...)
	userID := requestUserID(r)
	eventPayload := map[string]any{"member": h.memberWithUserResponse(member, user)}
	if ws, err := h.Queries.GetWorkspace(r.Context(), requester.WorkspaceID); err == nil {
		eventPayload["workspace_name"] = ws.Name
	}
	h.publish(protocol.EventMemberAdded, uuidToString(requester.WorkspaceID), "member", userID, eventPayload)
	h.notifyDaemonWorkspacesChanged(uuidToString(user.ID))

	writeJSON(w, http.StatusCreated, h.memberWithUserResponse(member, user))
}

type UpdateMemberRequest struct {
	Role string `json:"role"`
	// AccessScope may be sent on its own (without role) to switch a person
	// between "sees the whole workspace" and "sees only granted projects".
	AccessScope *string `json:"access_scope"`
}

func (h *Handler) UpdateMember(w http.ResponseWriter, r *http.Request) {
	workspaceID := workspaceIDFromURL(r, "id")
	requester, ok := h.workspaceMember(w, r, workspaceID)
	if !ok {
		return
	}

	memberID := chi.URLParam(r, "memberId")
	memberUUID, ok := parseUUIDOrBadRequest(w, memberID, "member id")
	if !ok {
		return
	}
	target, err := h.Queries.GetMember(r.Context(), memberUUID)
	if err != nil || uuidToString(target.WorkspaceID) != uuidToString(requester.WorkspaceID) {
		writeError(w, http.StatusNotFound, "member not found")
		return
	}

	var req UpdateMemberRequest
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
		writeError(w, http.StatusBadRequest, "invalid request body")
		return
	}
	// access_scope can be changed on its own; role stays required otherwise.
	if req.AccessScope != nil {
		scope := strings.TrimSpace(*req.AccessScope)
		if scope != "workspace" && scope != "projects" {
			writeError(w, http.StatusBadRequest, `access_scope must be "workspace" or "projects"`)
			return
		}
		if requester.Role != "owner" && requester.Role != "admin" {
			writeError(w, http.StatusForbidden, "insufficient permissions")
			return
		}
		updated, err := h.Queries.UpdateMemberAccessScope(r.Context(), db.UpdateMemberAccessScopeParams{
			ID:          target.ID,
			AccessScope: scope,
		})
		if err != nil {
			writeError(w, http.StatusInternalServerError, "failed to update member access scope")
			return
		}
		h.MembershipCache.Invalidate(r.Context(), uuidToString(target.UserID), workspaceID)
		target = updated
		if strings.TrimSpace(req.Role) == "" {
			user, uerr := h.Queries.GetUser(r.Context(), updated.UserID)
			if uerr != nil {
				writeError(w, http.StatusInternalServerError, "failed to load member")
				return
			}
			resp := h.memberWithUserResponse(updated, user)
			h.publish(protocol.EventMemberUpdated, uuidToString(requester.WorkspaceID), "member", requestUserID(r), map[string]any{"member": resp})
			writeJSON(w, http.StatusOK, resp)
			return
		}
	}

	if strings.TrimSpace(req.Role) == "" {
		writeError(w, http.StatusBadRequest, "role is required")
		return
	}

	role, valid := normalizeMemberRole(req.Role)
	if !valid {
		writeError(w, http.StatusBadRequest, "invalid member role")
		return
	}

	if (target.Role == "owner" || role == "owner") && requester.Role != "owner" {
		writeError(w, http.StatusForbidden, "insufficient permissions")
		return
	}

	if target.Role == "owner" && role != "owner" {
		members, err := h.Queries.ListMembers(r.Context(), target.WorkspaceID)
		if err != nil {
			writeError(w, http.StatusInternalServerError, "failed to update member")
			return
		}
		if countOwners(members) <= 1 {
			writeError(w, http.StatusBadRequest, "workspace must have at least one owner")
			return
		}
	}

	updatedMember, err := h.Queries.UpdateMemberRole(r.Context(), db.UpdateMemberRoleParams{
		ID:   target.ID,
		Role: role,
	})
	if err != nil {
		writeError(w, http.StatusInternalServerError, "failed to update member")
		return
	}

	h.MembershipCache.Invalidate(r.Context(), uuidToString(target.UserID), workspaceID)

	user, err := h.Queries.GetUser(r.Context(), updatedMember.UserID)
	if err != nil {
		writeError(w, http.StatusInternalServerError, "failed to load member")
		return
	}

	userID := requestUserID(r)
	h.publish(protocol.EventMemberUpdated, uuidToString(requester.WorkspaceID), "member", userID, map[string]any{
		"member": h.memberWithUserResponse(updatedMember, user),
	})

	writeJSON(w, http.StatusOK, h.memberWithUserResponse(updatedMember, user))
}

func (h *Handler) DeleteMember(w http.ResponseWriter, r *http.Request) {
	workspaceID := workspaceIDFromURL(r, "id")
	requester, ok := h.workspaceMember(w, r, workspaceID)
	if !ok {
		return
	}

	memberID := chi.URLParam(r, "memberId")
	memberUUID, ok := parseUUIDOrBadRequest(w, memberID, "member id")
	if !ok {
		return
	}
	target, err := h.Queries.GetMember(r.Context(), memberUUID)
	if err != nil || uuidToString(target.WorkspaceID) != uuidToString(requester.WorkspaceID) {
		writeError(w, http.StatusNotFound, "member not found")
		return
	}

	if target.Role == "owner" && requester.Role != "owner" {
		writeError(w, http.StatusForbidden, "insufficient permissions")
		return
	}

	if target.Role == "owner" {
		members, err := h.Queries.ListMembers(r.Context(), target.WorkspaceID)
		if err != nil {
			writeError(w, http.StatusInternalServerError, "failed to delete member")
			return
		}
		if countOwners(members) <= 1 {
			writeError(w, http.StatusBadRequest, "workspace must have at least one owner")
			return
		}
	}

	requesterUserID := requestUserID(r)
	result, err := h.revokeAndRemoveMember(r.Context(), target.WorkspaceID, target.UserID, target.ID, parseUUID(requesterUserID))
	if err != nil {
		slog.Warn("delete member failed", append(logger.RequestAttrs(r), "error", err, "member_id", memberID, "workspace_id", workspaceID)...)
		writeError(w, http.StatusInternalServerError, "failed to delete member")
		return
	}

	h.MembershipCache.Invalidate(r.Context(), uuidToString(target.UserID), workspaceID)

	wsIDStr := uuidToString(requester.WorkspaceID)
	logRevocation(result, wsIDStr, uuidToString(target.UserID))
	h.publishRevocation(r.Context(), result, wsIDStr, "member", requesterUserID)

	slog.Info("member removed", append(logger.RequestAttrs(r), "member_id", uuidToString(target.ID), "workspace_id", workspaceID, "user_id", uuidToString(target.UserID))...)
	h.publish(protocol.EventMemberRemoved, wsIDStr, "member", requesterUserID, map[string]any{
		"member_id":    uuidToString(target.ID),
		"workspace_id": wsIDStr,
		"user_id":      uuidToString(target.UserID),
	})
	h.notifyDaemonWorkspacesChanged(uuidToString(target.UserID))

	w.WriteHeader(http.StatusNoContent)
}

func (h *Handler) LeaveWorkspace(w http.ResponseWriter, r *http.Request) {
	workspaceID := workspaceIDFromURL(r, "id")
	member, ok := h.workspaceMember(w, r, workspaceID)
	if !ok {
		return
	}

	if member.Role == "owner" {
		members, err := h.Queries.ListMembers(r.Context(), member.WorkspaceID)
		if err != nil {
			writeError(w, http.StatusInternalServerError, "failed to leave workspace")
			return
		}
		if countOwners(members) <= 1 {
			writeError(w, http.StatusBadRequest, "workspace must have at least one owner")
			return
		}
	}

	result, err := h.revokeAndRemoveMember(r.Context(), member.WorkspaceID, member.UserID, member.ID, member.UserID)
	if err != nil {
		slog.Warn("leave workspace failed", append(logger.RequestAttrs(r), "error", err, "workspace_id", workspaceID)...)
		writeError(w, http.StatusInternalServerError, "failed to leave workspace")
		return
	}

	h.MembershipCache.Invalidate(r.Context(), uuidToString(member.UserID), workspaceID)

	userID := requestUserID(r)
	logRevocation(result, workspaceID, uuidToString(member.UserID))
	h.publishRevocation(r.Context(), result, workspaceID, "member", userID)

	slog.Info("member removed", append(logger.RequestAttrs(r), "member_id", uuidToString(member.ID), "workspace_id", workspaceID, "user_id", uuidToString(member.UserID))...)
	h.publish(protocol.EventMemberRemoved, workspaceID, "member", userID, map[string]any{
		"member_id":    uuidToString(member.ID),
		"workspace_id": workspaceID,
		"user_id":      uuidToString(member.UserID),
	})
	h.notifyDaemonWorkspacesChanged(uuidToString(member.UserID))

	w.WriteHeader(http.StatusNoContent)
}

func (h *Handler) DeleteWorkspace(w http.ResponseWriter, r *http.Request) {
	workspaceID := workspaceIDFromURL(r, "id")

	// Defense in depth: the route is already gated by the
	// RequireWorkspaceRoleFromURL("owner") middleware, but we re-check here
	// so that the handler is safe regardless of how it gets wired up
	// (direct calls in tests, future router refactors, etc.).
	requester, ok := h.workspaceMember(w, r, workspaceID)
	if !ok {
		return
	}
	if requester.Role != "owner" {
		writeError(w, http.StatusForbidden, "insufficient permissions")
		return
	}

	// Invalidate membership cache for all workspace members before deletion.
	// After CASCADE deletes the member rows, cache entries become harmless
	// orphans (downstream lookups for the deleted workspace will fail), but
	// proactive invalidation prevents any stale-access window up to TTL.
	var affectedUserIDs []string
	if members, err := h.Queries.ListMembers(r.Context(), requester.WorkspaceID); err == nil {
		affectedUserIDs = make([]string, 0, len(members))
		for _, m := range members {
			userID := uuidToString(m.UserID)
			h.MembershipCache.Invalidate(r.Context(), userID, workspaceID)
			affectedUserIDs = append(affectedUserIDs, userID)
		}
	}

	// The teardown runs in one transaction so the chat_session row locks below
	// are still held when DeleteWorkspace sweeps chat_draft_restore. Without
	// them, FinalizeDeferredCancelledChat could commit a restore for one of
	// these sessions after the sweep's snapshot was taken: the session cascades
	// away, the restore has no FK to follow it (MUL-3515) and no reaper, and the
	// user's prompt is stranded forever (#5219). The finalizer takes the same
	// lock before inserting, so it either blocks until the session is gone and
	// skips the insert, or commits first and the sweep sees its row.
	//
	// The workspace row is locked first, because the session locks only cover
	// sessions that already exist: a CreateChatSession committing inside the
	// delete window would otherwise slip in a session nobody locked, and its
	// restore would outlive the cascade the same way. Holding the workspace row
	// FOR UPDATE blocks that insert on its workspace FK (FOR KEY SHARE).
	tx, err := h.TxStarter.Begin(r.Context())
	if err != nil {
		slog.Warn("begin workspace delete tx failed", append(logger.RequestAttrs(r), "error", err, "workspace_id", workspaceID)...)
		writeError(w, http.StatusInternalServerError, "failed to delete workspace")
		return
	}
	defer tx.Rollback(r.Context())
	qtx := h.Queries.WithTx(tx)

	if _, err := qtx.LockWorkspaceForDelete(r.Context(), requester.WorkspaceID); err != nil {
		slog.Warn("lock workspace for delete failed", append(logger.RequestAttrs(r), "error", err, "workspace_id", workspaceID)...)
		writeError(w, http.StatusInternalServerError, "failed to delete workspace")
		return
	}

	if _, err := qtx.LockChatSessionsByWorkspace(r.Context(), requester.WorkspaceID); err != nil {
		slog.Warn("lock workspace chat sessions failed", append(logger.RequestAttrs(r), "error", err, "workspace_id", workspaceID)...)
		writeError(w, http.StatusInternalServerError, "failed to delete workspace")
		return
	}

	// Keep the relationship graph in the application layer. Each step is a
	// set-based delete scoped by workspace_id; the legacy cascades remain only
	// as an expand-phase safety net until a later schema contract.
	ctx := r.Context()
	deleteSteps := []struct {
		name string
		run  func() error
	}{
		{
			name: "set teardown mode",
			run:  func() error { return qtx.SetWorkspaceTeardownMode(ctx) },
		},
		{
			name: "prepare relationship graph",
			run:  func() error { return qtx.PrepareWorkspaceDeletionLinks(ctx, requester.WorkspaceID) },
		},
		{
			name: "delete chat pins",
			run:  func() error { return qtx.DeleteChatPinnedAgentsByWorkspace(ctx, requester.WorkspaceID) },
		},
		{
			// This is the first stage that touches usage rollups. Keep the
			// global rollup lock out of relationship preparation so unrelated
			// workspaces skip the shortest possible rollup window.
			name: "lock task usage rollup",
			run:  func() error { return qtx.LockTaskUsageRollupForWorkspaceDelete(ctx) },
		},
		{
			name: "delete leaf data",
			run:  func() error { return qtx.DeleteWorkspaceLeafData(ctx, requester.WorkspaceID) },
		},
		{
			name: "delete autopilot runs",
			run:  func() error { return qtx.DeleteWorkspaceAutopilotRuns(ctx, requester.WorkspaceID) },
		},
		{
			name: "delete tasks",
			run:  func() error { return qtx.DeleteWorkspaceTasks(ctx, requester.WorkspaceID) },
		},
		{
			name: "delete chat messages",
			run:  func() error { return qtx.DeleteWorkspaceChatMessages(ctx, requester.WorkspaceID) },
		},
		{
			name: "delete communication roots",
			run:  func() error { return qtx.DeleteWorkspaceCommunicationRoots(ctx, requester.WorkspaceID) },
		},
		{
			name: "delete comments",
			run:  func() error { return qtx.DeleteWorkspaceComments(ctx, requester.WorkspaceID) },
		},
		{
			name: "delete issue roots",
			run:  func() error { return qtx.DeleteWorkspaceIssueRoots(ctx, requester.WorkspaceID) },
		},
		{
			name: "delete autopilot children",
			run:  func() error { return qtx.DeleteWorkspaceAutopilotChildren(ctx, requester.WorkspaceID) },
		},
		{
			name: "delete autopilots",
			run:  func() error { return qtx.DeleteWorkspaceAutopilots(ctx, requester.WorkspaceID) },
		},
		{
			name: "delete pull requests",
			run:  func() error { return qtx.DeleteWorkspacePullRequests(ctx, requester.WorkspaceID) },
		},
		{
			name: "delete integrations",
			run:  func() error { return qtx.DeleteWorkspaceConnections(ctx, requester.WorkspaceID) },
		},
		{
			name: "delete squads and skills",
			run:  func() error { return qtx.DeleteWorkspaceSquadsAndSkills(ctx, requester.WorkspaceID) },
		},
		{
			name: "delete agents",
			run:  func() error { return qtx.DeleteWorkspaceAgents(ctx, requester.WorkspaceID) },
		},
		{
			name: "delete runtimes and projects",
			run:  func() error { return qtx.DeleteWorkspaceRuntimesAndProjects(ctx, requester.WorkspaceID) },
		},
		{
			name: "delete administration data",
			run:  func() error { return qtx.DeleteWorkspaceAdministration(ctx, requester.WorkspaceID) },
		},
		{
			// At this point workspaceMember has resolved → workspaceID is a
			// valid UUID, so reuse the resolved value. The existing final
			// statement also sweeps any expand-phase compatibility leftovers.
			name: "delete workspace",
			run:  func() error { return qtx.DeleteWorkspace(ctx, requester.WorkspaceID) },
		},
	}
	for _, step := range deleteSteps {
		if err := step.run(); err != nil {
			slog.Warn("workspace delete step failed", append(
				logger.RequestAttrs(r),
				"error", err,
				"workspace_id", workspaceID,
				"step", step.name,
			)...)
			writeError(w, http.StatusInternalServerError, "failed to delete workspace")
			return
		}
	}

	if err := tx.Commit(r.Context()); err != nil {
		slog.Warn("commit workspace delete failed", append(logger.RequestAttrs(r), "error", err, "workspace_id", workspaceID)...)
		writeError(w, http.StatusInternalServerError, "failed to delete workspace")
		return
	}

	slog.Info("workspace deleted", append(logger.RequestAttrs(r), "workspace_id", workspaceID)...)
	h.publish(protocol.EventWorkspaceDeleted, workspaceID, "member", requestUserID(r), map[string]any{
		"workspace_id": workspaceID,
	})
	h.notifyDaemonWorkspacesChanged(affectedUserIDs...)

	w.WriteHeader(http.StatusNoContent)
}
