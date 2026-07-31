package handler

import (
	"context"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
)

// A property with visibility='owner' is invisible to ordinary members: it is
// absent from the catalog, its detail is 404, its value is stripped from the
// issue payload, and neither setting nor clearing it is allowed.
//
// Agents are the exception that makes the feature safe rather than harmful:
// the workspace context tells every agent to fill «Биллинг» when it closes a
// task, and a gate that blocked them would silently start invoicing internal
// work to clients.

// hiddenPropertyFixture creates an owner-only text property plus an issue
// carrying a value for it, and returns their ids alongside a plain member.
func hiddenPropertyFixture(t *testing.T) (propertyID, issueID, memberID string) {
	t.Helper()
	ctx := context.Background()

	if err := testPool.QueryRow(ctx, `
		INSERT INTO issue_property (workspace_id, name, type, description, icon, config, position, visibility)
		VALUES ($1, 'Биллинг', 'text', '', '', '{}'::jsonb, 99, 'owner')
		RETURNING id
	`, testWorkspaceID).Scan(&propertyID); err != nil {
		t.Fatalf("create hidden property: %v", err)
	}
	t.Cleanup(func() {
		testPool.Exec(context.Background(), `DELETE FROM issue_property WHERE id = $1`, propertyID)
	})

	if err := testPool.QueryRow(ctx, `
		INSERT INTO issue (workspace_id, title, creator_type, creator_id, priority, properties)
		VALUES ($1, 'hidden property carrier', 'member', $2, 'medium', jsonb_build_object($3::text, 'внутренняя'))
		RETURNING id
	`, testWorkspaceID, testUserID, propertyID).Scan(&issueID); err != nil {
		t.Fatalf("create issue: %v", err)
	}
	t.Cleanup(func() {
		testPool.Exec(context.Background(), `DELETE FROM issue WHERE id = $1`, issueID)
	})

	memberID = billingRoleUser(t, "hidden-prop-member@multica.test", "member")
	return propertyID, issueID, memberID
}

func TestHiddenProperty_CatalogAndDetail(t *testing.T) {
	if testHandler == nil || testPool == nil {
		t.Skip("database not available")
	}
	propertyID, _, memberID := hiddenPropertyFixture(t)

	listFor := func(userID string) string {
		w := httptest.NewRecorder()
		testHandler.ListProperties(w, newRequestAs(userID, http.MethodGet,
			"/api/properties?workspace_id="+testWorkspaceID, nil))
		if w.Code != http.StatusOK {
			t.Fatalf("list properties: status %d; body %s", w.Code, w.Body.String())
		}
		return w.Body.String()
	}

	if body := listFor(testUserID); !strings.Contains(body, propertyID) {
		t.Fatalf("owner does not see the hidden property in the catalog: %s", body)
	}
	if body := listFor(memberID); strings.Contains(body, propertyID) {
		t.Fatalf("member sees the hidden property in the catalog: %s", body)
	}

	// The detail endpoint answers 404 rather than 403 — a member must not
	// learn that the definition exists.
	w := httptest.NewRecorder()
	testHandler.GetProperty(w, withURLParam(newRequestAs(memberID, http.MethodGet,
		"/api/properties/"+propertyID+"?workspace_id="+testWorkspaceID, nil), "id", propertyID))
	if w.Code != http.StatusNotFound {
		t.Fatalf("member GetProperty: status %d, want 404; body %s", w.Code, w.Body.String())
	}
}

func TestHiddenProperty_ValueStrippedFromIssuePayload(t *testing.T) {
	if testHandler == nil || testPool == nil {
		t.Skip("database not available")
	}
	propertyID, issueID, memberID := hiddenPropertyFixture(t)

	get := func(userID string) map[string]any {
		w := httptest.NewRecorder()
		testHandler.GetIssue(w, withURLParam(newRequestAs(userID, http.MethodGet,
			"/api/issues/"+issueID+"?workspace_id="+testWorkspaceID, nil), "id", issueID))
		if w.Code != http.StatusOK {
			t.Fatalf("get issue as %s: status %d; body %s", userID, w.Code, w.Body.String())
		}
		var out map[string]any
		if err := json.Unmarshal(w.Body.Bytes(), &out); err != nil {
			t.Fatalf("decode issue: %v", err)
		}
		props, _ := out["properties"].(map[string]any)
		return props
	}

	if _, ok := get(testUserID)[propertyID]; !ok {
		t.Fatal("owner lost the hidden property value from the issue payload")
	}
	if _, ok := get(memberID)[propertyID]; ok {
		t.Fatal("member received the hidden property value in the issue payload")
	}
}

func TestHiddenProperty_MemberCannotWriteOrClear(t *testing.T) {
	if testHandler == nil || testPool == nil {
		t.Skip("database not available")
	}
	propertyID, issueID, memberID := hiddenPropertyFixture(t)

	// Hiding the definition from the catalog is not protection on its own:
	// the UUID is stable and sits in every historical payload, so the write
	// path needs its own gate. Without it a member could clear the
	// «внутренняя» mark and push their own work into a client invoice.
	set := httptest.NewRecorder()
	testHandler.SetIssueProperty(set, withURLParams(
		newRequestAs(memberID, http.MethodPut,
			"/api/issues/"+issueID+"/properties/"+propertyID+"?workspace_id="+testWorkspaceID,
			map[string]any{"value": "клиентская"}),
		"id", issueID, "propertyId", propertyID))
	if set.Code != http.StatusNotFound {
		t.Fatalf("member SetIssueProperty: status %d, want 404; body %s", set.Code, set.Body.String())
	}

	del := httptest.NewRecorder()
	testHandler.DeleteIssueProperty(del, withURLParams(
		newRequestAs(memberID, http.MethodDelete,
			"/api/issues/"+issueID+"/properties/"+propertyID+"?workspace_id="+testWorkspaceID, nil),
		"id", issueID, "propertyId", propertyID))
	if del.Code != http.StatusNotFound {
		t.Fatalf("member DeleteIssueProperty: status %d, want 404; body %s", del.Code, del.Body.String())
	}

	// The value survived both attempts.
	var stored string
	if err := testPool.QueryRow(context.Background(),
		`SELECT properties ->> $2 FROM issue WHERE id = $1`, issueID, propertyID).Scan(&stored); err != nil {
		t.Fatalf("read back property value: %v", err)
	}
	if stored != "внутренняя" {
		t.Fatalf("property value changed to %q", stored)
	}
}

func TestHiddenProperty_OwnerCanStillWrite(t *testing.T) {
	if testHandler == nil || testPool == nil {
		t.Skip("database not available")
	}
	propertyID, issueID, _ := hiddenPropertyFixture(t)

	w := httptest.NewRecorder()
	testHandler.SetIssueProperty(w, withURLParams(
		newRequestAs(testUserID, http.MethodPut,
			"/api/issues/"+issueID+"/properties/"+propertyID+"?workspace_id="+testWorkspaceID,
			map[string]any{"value": "клиентская"}),
		"id", issueID, "propertyId", propertyID))
	if w.Code != http.StatusOK {
		t.Fatalf("owner SetIssueProperty: status %d; body %s", w.Code, w.Body.String())
	}
}

// The invoice pipeline matches this definition BY NAME (issueMarkedInternalSQL
// compares lower(ip.name) = 'биллинг'), so a rename or an archive would
// silently change what clients get invoiced.
func TestBillingClassifierProperty_CannotBeRenamedOrArchived(t *testing.T) {
	if testHandler == nil || testPool == nil {
		t.Skip("database not available")
	}
	propertyID, _, _ := hiddenPropertyFixture(t)

	rename := httptest.NewRecorder()
	testHandler.UpdateProperty(rename, withURLParam(newRequestAs(testUserID, http.MethodPatch,
		"/api/properties/"+propertyID+"?workspace_id="+testWorkspaceID,
		map[string]any{"name": "Биллинг2"}), "id", propertyID))
	if rename.Code != http.StatusBadRequest {
		t.Fatalf("rename: status %d, want 400; body %s", rename.Code, rename.Body.String())
	}

	archive := httptest.NewRecorder()
	testHandler.UpdateProperty(archive, withURLParam(newRequestAs(testUserID, http.MethodPatch,
		"/api/properties/"+propertyID+"?workspace_id="+testWorkspaceID,
		map[string]any{"archived": true}), "id", propertyID))
	if archive.Code != http.StatusBadRequest {
		t.Fatalf("archive: status %d, want 400; body %s", archive.Code, archive.Body.String())
	}

	// A non-name edit on the same definition still works.
	ok := httptest.NewRecorder()
	testHandler.UpdateProperty(ok, withURLParam(newRequestAs(testUserID, http.MethodPatch,
		"/api/properties/"+propertyID+"?workspace_id="+testWorkspaceID,
		map[string]any{"description": "клиентская или внутренняя"}), "id", propertyID))
	if ok.Code != http.StatusOK {
		t.Fatalf("description edit: status %d; body %s", ok.Code, ok.Body.String())
	}
}
