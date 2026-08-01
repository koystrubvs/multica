package middleware

// nav_sections.go — which parts of the app a role is allowed to open.
//
// The owner picks this per role in workspace settings; it is stored on
// `workspace.settings` rather than in a column of its own because it is
// presentation policy, not a relation, and it needed no migration.
//
// The list stored is of HIDDEN sections, not allowed ones. That way an empty
// setting means "everything as before", and a section added by a later upstream
// release starts visible instead of silently disappearing for everyone.
//
// Hiding a section from the sidebar is cosmetic on its own — the route is still
// typeable — so the same rule is enforced here, on the routes that serve those
// pages. What it deliberately does NOT cover is the agent and squad *lists*:
// they populate the assignee picker, quick create, the issue header and the
// board cards, so closing them would break assigning work to an agent for
// exactly the people who are supposed to keep doing it.

import (
	"encoding/json"
	"net/http"

	db "github.com/multica-ai/multica/server/pkg/db/generated"
)

// Sections that can be hidden. Business and Usage are absent on purpose: they
// are already owner/admin-only in both the sidebar and the API.
const (
	NavSectionAutopilots = "autopilots"
	NavSectionAgents     = "agents"
	NavSectionSquads     = "squads"
	NavSectionRuntimes   = "runtimes"
	NavSectionSkills     = "skills"
)

// NavSectionsSettingKey is the key under `workspace.settings` holding a
// role -> hidden section list map, e.g.
//
//	{"hidden_nav_sections": {"member": ["autopilots", "runtimes"]}}
const NavSectionsSettingKey = "hidden_nav_sections"

// HiddenNavSections returns the sections hidden from `role`.
//
// Owner and admin are never restricted: the setting is how they express the
// policy, and locking themselves out of the screen that edits it would be a
// trap. Unparseable settings hide nothing — this is a visibility preference,
// and failing it closed would black out the app over a malformed JSON blob
// while protecting nothing that the role checks do not already protect.
func HiddenNavSections(settings []byte, role string) map[string]bool {
	if role == "owner" || role == "admin" || len(settings) == 0 {
		return nil
	}
	var parsed struct {
		Hidden map[string][]string `json:"hidden_nav_sections"`
	}
	if err := json.Unmarshal(settings, &parsed); err != nil {
		return nil
	}
	list, ok := parsed.Hidden[role]
	if !ok || len(list) == 0 {
		return nil
	}
	hidden := make(map[string]bool, len(list))
	for _, s := range list {
		hidden[s] = true
	}
	return hidden
}

// NavSectionHidden reports whether this role may not open `section`.
func NavSectionHidden(settings []byte, role, section string) bool {
	return HiddenNavSections(settings, role)[section]
}

// defaultHiddenForMember is what a workspace starts out hiding from members:
// the operational tooling that runs the agent fleet. Skills is deliberately
// absent — it is a library people read, not a control surface.
var defaultHiddenForMember = []string{
	NavSectionAutopilots,
	NavSectionAgents,
	NavSectionSquads,
	NavSectionRuntimes,
}

// DefaultWorkspaceSettings is stamped onto a workspace at creation.
//
// Written as a real stored value rather than expressed as "absence means
// hidden", because absence has to keep meaning "everything as before": that is
// what stops a section introduced by a later upstream release from disappearing
// for everyone the moment it lands. A stored default shows up in the settings
// screen as switches the owner can see and flip; an implicit one would not.
//
// It is applied only at creation, so existing workspaces keep whatever they
// have.
func DefaultWorkspaceSettings() []byte {
	out, err := json.Marshal(map[string]any{
		NavSectionsSettingKey: map[string][]string{
			"member": defaultHiddenForMember,
		},
	})
	if err != nil {
		// Marshalling a literal map of strings cannot fail; treat any future
		// change that breaks it as "no default" rather than a broken workspace.
		return nil
	}
	return out
}

// RequireNavSection refuses a route whose section the caller's role cannot
// open. It runs after the workspace middleware and reads the member it
// resolved, so it costs one workspace read and only on the routes it guards.
//
// A caller with no member in context never reached the workspace middleware,
// which means the route is misconfigured; refuse rather than wave it through.
func RequireNavSection(queries *db.Queries, section string) func(http.Handler) http.Handler {
	return func(next http.Handler) http.Handler {
		return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
			member, ok := MemberFromContext(r.Context())
			if !ok {
				writeError(w, http.StatusForbidden, "insufficient permissions")
				return
			}
			if member.Role == "owner" || member.Role == "admin" {
				next.ServeHTTP(w, r)
				return
			}
			ws, err := queries.GetWorkspace(r.Context(), member.WorkspaceID)
			if err != nil {
				writeError(w, http.StatusForbidden, "insufficient permissions")
				return
			}
			if NavSectionHidden(ws.Settings, member.Role, section) {
				writeError(w, http.StatusForbidden, "insufficient permissions")
				return
			}
			next.ServeHTTP(w, r)
		})
	}
}
