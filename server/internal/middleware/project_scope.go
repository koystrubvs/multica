package middleware

// project_scope.go — which projects the caller may be told about.
//
// Access is granted per person: a member in 'projects' mode sees the projects
// bound to them in member_project and nothing else, and the same bindings are
// edited from either end — the project picking its people, or the person
// picking their projects. Checking that per loaded entity works for /{id}
// routes and does nothing for lists, so the allowed set is resolved once per
// request here and injected into the WHERE clause of the queries that read
// issues.

import (
	"context"

	"github.com/jackc/pgx/v5/pgtype"
	db "github.com/multica-ai/multica/server/pkg/db/generated"
)

// ProjectScope is the set of projects the caller may see.
type ProjectScope struct {
	// Unrestricted holds for the workspace owner, for anyone still in
	// 'workspace' mode, and for agents, whose requests carry the runtime
	// owner's X-User-ID. Nothing is filtered.
	Unrestricted bool
	// AllowedProjectIDs are the bindings, and they are exhaustive: an empty
	// list for a restricted caller means no issues at all, never "everything".
	AllowedProjectIDs []pgtype.UUID
}

// Restricted reports whether this caller's reads must be filtered.
func (s ProjectScope) Restricted() bool {
	return !s.Unrestricted
}

// DenyAllProjectScope is what a failed resolve must degrade to: restricted,
// with nothing allowed. It exists so that a caller which ignores the error
// still leaks nothing.
func DenyAllProjectScope() ProjectScope {
	return ProjectScope{}
}

// ProjectScopeQuerier is the single query the rule needs. Declared narrowly so
// the realtime scope authorizer — which is unit tested against an in-memory
// fake — can share this rule instead of restating it.
type ProjectScopeQuerier interface {
	ListMemberProjectsByUser(ctx context.Context, arg db.ListMemberProjectsByUserParams) ([]pgtype.UUID, error)
}

// ResolveProjectScope computes what this member may see.
//
// The owner is always unrestricted. Everyone else follows their access mode,
// except guests, who are bound by their project list whatever the mode says —
// that is how the SitePing client gate has always worked, and widening it here
// would hand a client the whole workspace.
func ResolveProjectScope(ctx context.Context, queries ProjectScopeQuerier, workspaceID pgtype.UUID, member db.Member) (ProjectScope, error) {
	if member.Role == "owner" {
		return ProjectScope{Unrestricted: true}, nil
	}
	if member.Role != "guest" && member.AccessScope != "projects" {
		return ProjectScope{Unrestricted: true}, nil
	}
	allowed, err := queries.ListMemberProjectsByUser(ctx, db.ListMemberProjectsByUserParams{
		UserID:      member.UserID,
		WorkspaceID: workspaceID,
	})
	if err != nil {
		return DenyAllProjectScope(), err
	}
	return ProjectScope{AllowedProjectIDs: allowed}, nil
}

// ProjectScopeFromContext returns the scope injected by the workspace
// middleware.
//
// ok=false means the route did not pass through that middleware (the auth-only
// upload and attachment-download routes do not) and MUST be read as "resolve it
// yourself or refuse" — never as "there is nothing to restrict".
func ProjectScopeFromContext(ctx context.Context) (ProjectScope, bool) {
	scope, ok := ctx.Value(ctxKeyProjectScope).(ProjectScope)
	return scope, ok
}

// SetProjectScopeContext injects a resolved scope, for the middleware and for
// handlers on routes that resolve it themselves.
func SetProjectScopeContext(ctx context.Context, scope ProjectScope) context.Context {
	return context.WithValue(ctx, ctxKeyProjectScope, scope)
}
