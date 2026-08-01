package handler

import (
	"strconv"
	"testing"

	"github.com/jackc/pgx/v5/pgtype"
	"github.com/multica-ai/multica/server/internal/middleware"
)

func testArgAdder() (func(any) string, *[]any) {
	args := make([]any, 0)
	add := func(value any) string {
		args = append(args, value)
		return "$" + strconv.Itoa(len(args))
	}
	return add, &args
}

func TestScopeIssueSQL(t *testing.T) {
	allowed := []pgtype.UUID{{Bytes: [16]byte{1}, Valid: true}}

	// The owner's queries must come out exactly as they were, down to the
	// argument list — this predicate is on every issue read in the product.
	t.Run("unrestricted gets no predicate and no argument", func(t *testing.T) {
		add, args := testArgAdder()
		if got := scopeIssueSQL(middleware.ProjectScope{Unrestricted: true}, add); got != "" {
			t.Fatalf("fragment = %q, want empty", got)
		}
		if len(*args) != 0 {
			t.Fatalf("unrestricted query gained %d argument(s)", len(*args))
		}
	})

	t.Run("bound projects are the whole of what is visible", func(t *testing.T) {
		add, args := testArgAdder()
		got := scopeIssueSQL(middleware.ProjectScope{AllowedProjectIDs: allowed}, add)
		want := "i.project_id = ANY($1::uuid[])"
		if got != want {
			t.Fatalf("fragment = %q, want %q", got, want)
		}
		if len(*args) != 1 {
			t.Fatalf("expected exactly one bound argument, got %d", len(*args))
		}
	})

	// The failure that would matter: no grants must mean no issues. An empty
	// predicate here would hand the restricted member the entire workspace.
	t.Run("no grants means nothing, not everything", func(t *testing.T) {
		add, _ := testArgAdder()
		if got := scopeIssueSQL(middleware.ProjectScope{}, add); got != "false" {
			t.Fatalf("fragment = %q, want %q", got, "false")
		}
	})
}

// A scope that failed to resolve must deny. Restricted() reporting false for a
// deny-all scope is the one bug that would turn this mechanism into decoration.
func TestDenyAllProjectScopeDenies(t *testing.T) {
	scope := middleware.DenyAllProjectScope()
	if scope.Unrestricted || !scope.Restricted() {
		t.Fatal("a failed resolve must be restricted")
	}
	if len(scope.AllowedProjectIDs) != 0 {
		t.Fatal("a failed resolve must allow nothing")
	}
	add, _ := testArgAdder()
	if got := scopeIssueSQL(scope, add); got != "false" {
		t.Fatalf("fragment = %q, want %q", got, "false")
	}
}

// unrestrictedTestScope is what every pre-existing search test assumes: the
// owner's view, where the predicate is absent entirely.
func unrestrictedTestScope() middleware.ProjectScope {
	return middleware.ProjectScope{Unrestricted: true}
}
