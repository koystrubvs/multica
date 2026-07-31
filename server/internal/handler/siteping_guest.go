package handler

import (
	"encoding/json"
	"net/http"
	"strings"
	"time"

	"github.com/jackc/pgx/v5/pgtype"
	"github.com/multica-ai/multica/server/internal/auth"
	db "github.com/multica-ai/multica/server/pkg/db/generated"
)

// siteping-guest provisioning (P10). The bridge cannot mint a personal access
// token for anyone but itself through the public API (CreatePersonalAccessToken
// is self-scoped), and the upstream member-add flow now goes through an
// interactive invitation accept — neither fits the "token link materialises a
// client" SitePing onboarding. This admin-only endpoint does the whole thing
// in one call: find-or-create the user (bypassing the open-signup gate, since
// an authenticated owner/admin is explicitly vouching for this client), ensure
// a workspace member with the narrow "guest" role, and mint a long-lived PAT
// the bridge stores so it can act on the guest's behalf (comment attribution).
//
// The guest can also log into the Multica UI directly afterwards: once the
// user row exists, the email-code login flow always admits existing users
// regardless of ALLOW_SIGNUP (see checkSignupAllowed).

// guestPATLifetime matches the bridge's expectation of a stable credential.
// A SitePing client relationship outlives the 90-day default PAT window, so we
// issue a year and let the bridge re-provision if it ever lapses.
const guestPATLifetime = 365 * 24 * time.Hour

type CreateSitepingGuestRequest struct {
	Email string `json:"email"`
	Name  string `json:"name"`
	// ProjectID optionally scopes the guest to a single project (P10 Variant A).
	ProjectID string `json:"project_id"`
}

type CreateSitepingGuestResponse struct {
	UserID   string `json:"user_id"`
	MemberID string `json:"member_id"`
	Email    string `json:"email"`
	Name     string `json:"name"`
	Role     string `json:"role"`
	// ProjectID echoes the project the guest was scoped to, if any.
	ProjectID string `json:"project_id,omitempty"`
	// Token is the raw PAT — returned once, never persisted in plaintext here.
	Token string `json:"token"`
}

func (h *Handler) CreateSitepingGuest(w http.ResponseWriter, r *http.Request) {
	workspaceID := workspaceIDFromURL(r, "id")
	requester, ok := h.workspaceMember(w, r, workspaceID)
	if !ok {
		return
	}

	var req CreateSitepingGuestRequest
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
		writeError(w, http.StatusBadRequest, "invalid request body")
		return
	}
	email := strings.ToLower(strings.TrimSpace(req.Email))
	if email == "" {
		writeError(w, http.StatusBadRequest, "email is required")
		return
	}
	name := strings.TrimSpace(req.Name)
	if name == "" {
		name = email
		if at := strings.Index(email, "@"); at > 0 {
			name = email[:at]
		}
	}

	// Find-or-create the user directly (admin-vouched provisioning bypasses the
	// open-signup gate, exactly like the legacy CreateMember auto-create path).
	user, err := h.Queries.GetUserByEmail(r.Context(), email)
	if err != nil {
		if !isNotFound(err) {
			writeError(w, http.StatusInternalServerError, "failed to load user")
			return
		}
		user, err = h.Queries.CreateUser(r.Context(), db.CreateUserParams{Name: name, Email: email})
		if err != nil {
			writeError(w, http.StatusInternalServerError, "failed to create user")
			return
		}
	}

	// Ensure a guest membership; tolerate an already-existing member (re-provision).
	member, err := h.Queries.CreateMember(r.Context(), db.CreateMemberParams{
		WorkspaceID: requester.WorkspaceID,
		UserID:      user.ID,
		Role:        "guest",
	})
	if err != nil {
		if !isUniqueViolation(err) {
			writeError(w, http.StatusInternalServerError, "failed to create member")
			return
		}
		member, err = h.Queries.GetMemberByUserAndWorkspace(r.Context(), db.GetMemberByUserAndWorkspaceParams{
			UserID:      user.ID,
			WorkspaceID: requester.WorkspaceID,
		})
		if err != nil {
			writeError(w, http.StatusInternalServerError, "failed to load existing member")
			return
		}
	}

	// Optionally bind the guest to a project (P10 Variant A scoping). The bridge
	// passes the project that owns the SitePing site; the guest can then only
	// file/read/comment within it.
	boundProjectID := ""
	if pid := strings.TrimSpace(req.ProjectID); pid != "" {
		projUUID, okp := parseUUIDOrBadRequest(w, pid, "project_id")
		if !okp {
			return
		}
		project, perr := h.Queries.GetProject(r.Context(), projUUID)
		if perr != nil || uuidToString(project.WorkspaceID) != uuidToString(requester.WorkspaceID) {
			writeError(w, http.StatusBadRequest, "project not found in this workspace")
			return
		}
		if _, perr := h.Queries.CreateMemberProject(r.Context(), db.CreateMemberProjectParams{
			WorkspaceID: requester.WorkspaceID,
			UserID:      user.ID,
			ProjectID:   projUUID,
		}); perr != nil {
			writeError(w, http.StatusInternalServerError, "failed to bind project")
			return
		}
		boundProjectID = pid
	}

	// Mint a PAT for the guest so the bridge can act on their behalf.
	rawToken, err := auth.GeneratePATToken()
	if err != nil {
		writeError(w, http.StatusInternalServerError, "failed to generate token")
		return
	}
	prefix := rawToken
	if len(prefix) > 12 {
		prefix = prefix[:12]
	}
	if _, err := h.Queries.CreatePersonalAccessToken(r.Context(), db.CreatePersonalAccessTokenParams{
		UserID:      user.ID,
		Name:        "siteping-guest",
		TokenHash:   auth.HashToken(rawToken),
		TokenPrefix: prefix,
		ExpiresAt:   pgtype.Timestamptz{Time: time.Now().Add(guestPATLifetime), Valid: true},
	}); err != nil {
		writeError(w, http.StatusInternalServerError, "failed to create token")
		return
	}

	writeJSON(w, http.StatusCreated, CreateSitepingGuestResponse{
		UserID:   uuidToString(user.ID),
		MemberID: uuidToString(member.ID),
		Email:    email,
		Name:     name,
		Role:      member.Role,
		ProjectID: boundProjectID,
		Token:     rawToken,
	})
}
