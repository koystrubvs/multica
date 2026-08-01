package handler

// project_scope.go — applying a request's project scope to the queries that
// read issues.
//
// The scope itself is resolved in the workspace middleware
// (internal/middleware/project_scope.go). This file is the handler-side pair:
// one accessor that never fails open, and one SQL fragment reused by every
// query that selects issues, the same way issueMarkedInternalSQL is reused by
// the billing queries.

import (
	"net/http"

	"github.com/multica-ai/multica/server/internal/middleware"
	"github.com/multica-ai/multica/server/internal/util"
	db "github.com/multica-ai/multica/server/pkg/db/generated"
)

// projectScope returns the caller's scope, resolving it directly for routes
// registered outside the workspace middleware (POST /api/upload-file and
// GET /api/attachments/{id}/download are auth-only, exactly as they resolve
// their workspace by hand).
//
// ok=false means the scope could not be established. It carries a deny-all
// scope so that a caller which ignores the flag hides everything rather than
// showing everything, but callers are expected to refuse the request.
func (h *Handler) projectScope(r *http.Request) (middleware.ProjectScope, bool) {
	if scope, ok := middleware.ProjectScopeFromContext(r.Context()); ok {
		return scope, true
	}
	workspaceID := h.resolveWorkspaceID(r)
	userID := requestUserID(r)
	if workspaceID == "" || userID == "" {
		return middleware.DenyAllProjectScope(), false
	}
	member, err := h.getWorkspaceMember(r.Context(), userID, workspaceID)
	if err != nil {
		return middleware.DenyAllProjectScope(), false
	}
	workspaceUUID, err := util.ParseUUID(workspaceID)
	if err != nil {
		return middleware.DenyAllProjectScope(), false
	}
	scope, err := middleware.ResolveProjectScope(r.Context(), h.Queries, workspaceUUID, member)
	if err != nil {
		return middleware.DenyAllProjectScope(), false
	}
	return scope, true
}

// projectScopeForWorkspace resolves the caller's scope in a workspace the
// request never named. Attachment routes work this way: they are auth-only and
// derive the workspace from the row they just loaded, so there is no context
// scope and no workspace header to fall back on.
func (h *Handler) projectScopeForWorkspace(r *http.Request, workspaceID string) (middleware.ProjectScope, bool) {
	userID := requestUserID(r)
	if workspaceID == "" || userID == "" {
		return middleware.DenyAllProjectScope(), false
	}
	member, err := h.getWorkspaceMember(r.Context(), userID, workspaceID)
	if err != nil {
		return middleware.DenyAllProjectScope(), false
	}
	workspaceUUID, err := util.ParseUUID(workspaceID)
	if err != nil {
		return middleware.DenyAllProjectScope(), false
	}
	scope, err := middleware.ResolveProjectScope(r.Context(), h.Queries, workspaceUUID, member)
	if err != nil {
		return middleware.DenyAllProjectScope(), false
	}
	return scope, true
}

// refuseIfScoped denies a whole surface to a caller who only sees some
// projects, and reports whether the request may continue.
//
// It is the deliberate answer for surfaces that either cannot be decomposed by
// project (runtime usage, host skill listings), or where decomposing them is a
// large change for a screen a scoped member has no business opening (the usage
// dashboards), or where the surface hands out the means to escape the scope
// entirely (creating an agent: an agent acts as the runtime owner and reads
// everything). Refusing is honest and cheap; a half-filtered dashboard would
// look filtered and not be.
//
// Nothing changes for the owner or for members who see the whole workspace,
// which today is everyone.
// An unresolvable scope is passed through here rather than refused, which is
// the opposite of the rule everywhere else in this file. The reason is the
// deny surface, not laziness: every route carrying this gate sits behind the
// workspace middleware, which both refuses non-members and injects the scope,
// so in production the scope always resolves. Refusing the unresolvable case
// would only change what a non-member or a malformed request is told — turning
// the handler's own 404 into a 403 and handing back an existence oracle.
func (h *Handler) refuseIfScoped(w http.ResponseWriter, r *http.Request) bool {
	scope, ok := h.projectScope(r)
	if ok && scope.Restricted() {
		writeError(w, http.StatusForbidden, "insufficient permissions")
		return false
	}
	return true
}

// attachmentReadable reports whether this caller may be handed a file. An
// attachment inherits the reach of the issue it hangs on: a file on an issue in
// a project the caller was not given is as private as the issue itself, and the
// id is guessable from any comment body they might later be shown.
//
// Attachments with no issue (chat, task and workspace uploads) are left to the
// membership check that already guards them.
func (h *Handler) attachmentReadable(r *http.Request, att db.Attachment) bool {
	if !att.IssueID.Valid {
		return true
	}
	scope, ok := h.projectScopeForWorkspace(r, uuidToString(att.WorkspaceID))
	if !ok {
		return false
	}
	if !scope.Restricted() {
		return true
	}
	issue, err := h.Queries.GetIssue(r.Context(), att.IssueID)
	if err != nil {
		return false
	}
	if !issue.ProjectID.Valid {
		return false
	}
	for _, granted := range scope.AllowedProjectIDs {
		if granted.Valid && granted.Bytes == issue.ProjectID.Bytes {
			return true
		}
	}
	return false
}

// scopeIssueSQL returns a WHERE fragment restricting issues to the caller's
// projects, or "" when the caller is unrestricted — which keeps the owner's
// queries byte-identical to what they were. Requires `i` (issue) in scope,
// mirroring issueMarkedInternalSQL.
//
// Issues with no project are NOT visible to a restricted caller, and that is
// deliberate rather than an oversight: autopilots without a project — Биллинг,
// Health Guardian, Updater — put the monthly client report, the amounts and the
// invoice drafts on exactly those issues and in their comments, and search
// returns comment snippets across the workspace. On production every one of
// them is assigned to an agent, so no employee loses work they were given.
func scopeIssueSQL(scope middleware.ProjectScope, addArg func(any) string) string {
	if !scope.Restricted() {
		return ""
	}
	if len(scope.AllowedProjectIDs) == 0 {
		// No grants means no issues. Rendering nothing here would silently
		// widen the query to the whole workspace.
		return "false"
	}
	return "i.project_id = ANY(" + addArg(scope.AllowedProjectIDs) + "::uuid[])"
}
