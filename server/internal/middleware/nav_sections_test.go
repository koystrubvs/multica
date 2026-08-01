package middleware

import "testing"

func TestNavSectionHidden(t *testing.T) {
	const settings = `{"hidden_nav_sections":{"member":["autopilots","runtimes"],"guest":["agents"]}}`

	tests := []struct {
		name     string
		settings string
		role     string
		section  string
		want     bool
	}{
		{"hidden for the role that names it", settings, "member", NavSectionAutopilots, true},
		{"another hidden section for the same role", settings, "member", NavSectionRuntimes, true},
		{"section the role did not hide", settings, "member", NavSectionAgents, false},
		{"roles do not bleed into each other", settings, "guest", NavSectionAutopilots, false},
		{"guest keeps its own list", settings, "guest", NavSectionAgents, true},

		// The owner sets this policy; hiding the screen that edits it from the
		// people who edit it would lock the workspace out of its own setting.
		{"owner is never restricted", settings, "owner", NavSectionAutopilots, false},
		{"admin is never restricted", settings, "admin", NavSectionAutopilots, false},

		// Absence must read as "everything as before" so that a section added
		// by a later release starts visible instead of vanishing for everyone.
		{"empty settings hide nothing", `{}`, "member", NavSectionAutopilots, false},
		{"no settings at all hide nothing", ``, "member", NavSectionAutopilots, false},
		{"role absent from the map", `{"hidden_nav_sections":{"guest":["agents"]}}`, "member", NavSectionAgents, false},
		{"empty list for the role", `{"hidden_nav_sections":{"member":[]}}`, "member", NavSectionAgents, false},

		// Malformed input must not black out the app: this is a visibility
		// preference, and the role checks it sits beside are the real gate.
		{"broken json hides nothing", `{not json`, "member", NavSectionAutopilots, false},
		{"wrong shape hides nothing", `{"hidden_nav_sections":"autopilots"}`, "member", NavSectionAutopilots, false},

		// Other features share `settings`; an unrelated blob must be ignored
		// rather than mistaken for a policy.
		{"unrelated settings ignored", `{"something_else":{"member":["agents"]}}`, "member", NavSectionAgents, false},
	}

	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			got := NavSectionHidden([]byte(tc.settings), tc.role, tc.section)
			if got != tc.want {
				t.Fatalf("NavSectionHidden(role=%q, section=%q) = %v, want %v", tc.role, tc.section, got, tc.want)
			}
		})
	}
}

func TestDefaultWorkspaceSettingsReadBackByTheSameRule(t *testing.T) {
	settings := DefaultWorkspaceSettings()
	if len(settings) == 0 {
		t.Fatal("default settings are empty")
	}

	// The value a new workspace is stamped with has to be understood by the
	// reader that gates the routes — otherwise the switches would show one
	// thing and the server would do another.
	for _, section := range []string{
		NavSectionAutopilots, NavSectionAgents, NavSectionSquads, NavSectionRuntimes,
	} {
		if !NavSectionHidden(settings, "member", section) {
			t.Errorf("member should start with %q hidden", section)
		}
	}

	// Skills is a library people read, not a control surface.
	if NavSectionHidden(settings, "skills", NavSectionSkills) {
		t.Error("skills must not be hidden by default")
	}
	if NavSectionHidden(settings, "member", NavSectionSkills) {
		t.Error("skills must not be hidden from members by default")
	}
	// Only members are defaulted: guests are left as they were so the default
	// cannot silently change what a client already sees.
	if NavSectionHidden(settings, "guest", NavSectionAgents) {
		t.Error("guests must not be defaulted")
	}
	if NavSectionHidden(settings, "owner", NavSectionAgents) {
		t.Error("owner must never be restricted")
	}
}

func TestHiddenNavSectionsReturnsNilForUnrestrictedRoles(t *testing.T) {
	const settings = `{"hidden_nav_sections":{"owner":["agents"],"member":["agents"]}}`
	if got := HiddenNavSections([]byte(settings), "owner"); got != nil {
		t.Fatalf("owner: got %v, want nil — an owner entry in the map must not restrict them", got)
	}
	if got := HiddenNavSections([]byte(settings), "member"); !got[NavSectionAgents] {
		t.Fatalf("member: got %v, want agents hidden", got)
	}
}
