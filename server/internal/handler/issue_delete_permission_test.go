package handler

import (
	"context"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"testing"
)

// Issue deletion is irreversible — there is no soft delete — and used to be
// ungated: any member could delete any issue, and one BatchDeleteIssues call
// could take out every client's work at once.
//
// Owner and admin delete anything; a plain member deletes only what they
// created.

// deletableIssue seeds an issue with the given creator.
func deletableIssue(t *testing.T, title, creatorID string) string {
	t.Helper()
	// `number` defaults to 0 and is unique per workspace — the handlers assign
	// it, direct inserts must not collide when a test seeds more than one.
	var issueID string
	if err := testPool.QueryRow(context.Background(), `
		INSERT INTO issue (workspace_id, title, creator_type, creator_id, priority, number)
		VALUES ($1, $2, 'member', $3, 'medium',
		        (SELECT COALESCE(MAX(number), 0) + 1 FROM issue WHERE workspace_id = $1))
		RETURNING id
	`, testWorkspaceID, title, creatorID).Scan(&issueID); err != nil {
		t.Fatalf("seed issue %q: %v", title, err)
	}
	t.Cleanup(func() {
		testPool.Exec(context.Background(), `DELETE FROM issue WHERE id = $1`, issueID)
	})
	return issueID
}

func issueExists(t *testing.T, issueID string) bool {
	t.Helper()
	var n int
	if err := testPool.QueryRow(context.Background(),
		`SELECT count(*) FROM issue WHERE id = $1`, issueID).Scan(&n); err != nil {
		t.Fatalf("count issue: %v", err)
	}
	return n > 0
}

func TestDeleteIssue_MemberCannotDeleteSomeoneElsesIssue(t *testing.T) {
	if testHandler == nil || testPool == nil {
		t.Skip("database not available")
	}
	memberID := billingRoleUser(t, "delete-gate-member@multica.test", "member")
	ownersIssue := deletableIssue(t, "owned by the workspace owner", testUserID)
	membersIssue := deletableIssue(t, "filed by the member", memberID)

	w := httptest.NewRecorder()
	testHandler.DeleteIssue(w, withURLParam(newRequestAs(memberID, http.MethodDelete,
		"/api/issues/"+ownersIssue+"?workspace_id="+testWorkspaceID, nil), "id", ownersIssue))
	if w.Code != http.StatusForbidden {
		t.Fatalf("member deleting another's issue: status %d, want 403; body %s", w.Code, w.Body.String())
	}
	if !issueExists(t, ownersIssue) {
		t.Fatal("the issue was deleted despite the 403")
	}

	// Their own issue still goes.
	own := httptest.NewRecorder()
	testHandler.DeleteIssue(own, withURLParam(newRequestAs(memberID, http.MethodDelete,
		"/api/issues/"+membersIssue+"?workspace_id="+testWorkspaceID, nil), "id", membersIssue))
	if own.Code != http.StatusNoContent {
		t.Fatalf("member deleting own issue: status %d, want 204; body %s", own.Code, own.Body.String())
	}
	if issueExists(t, membersIssue) {
		t.Fatal("the member's own issue survived deletion")
	}
}

func TestDeleteIssue_AdminAndOwnerCanDeleteAnything(t *testing.T) {
	if testHandler == nil || testPool == nil {
		t.Skip("database not available")
	}
	adminID := billingRoleUser(t, "delete-gate-admin@multica.test", "admin")
	memberID := billingRoleUser(t, "delete-gate-victim@multica.test", "member")
	byMember := deletableIssue(t, "filed by a member, removed by admin", memberID)

	w := httptest.NewRecorder()
	testHandler.DeleteIssue(w, withURLParam(newRequestAs(adminID, http.MethodDelete,
		"/api/issues/"+byMember+"?workspace_id="+testWorkspaceID, nil), "id", byMember))
	if w.Code != http.StatusNoContent {
		t.Fatalf("admin delete: status %d, want 204; body %s", w.Code, w.Body.String())
	}
	if issueExists(t, byMember) {
		t.Fatal("admin delete did not remove the issue")
	}
}

// The batch route is the one that could wipe a workspace in a single request.
// It skips what the caller may not delete instead of failing wholesale, and
// reports the refused count so a partial result is visible.
func TestBatchDeleteIssues_SkipsIssuesTheMemberDoesNotOwn(t *testing.T) {
	if testHandler == nil || testPool == nil {
		t.Skip("database not available")
	}
	memberID := billingRoleUser(t, "batch-delete-member@multica.test", "member")
	theirs := deletableIssue(t, "batch: member's own", memberID)
	notTheirs := deletableIssue(t, "batch: someone else's", testUserID)

	w := httptest.NewRecorder()
	testHandler.BatchDeleteIssues(w, newRequestAs(memberID, http.MethodPost,
		"/api/issues/batch-delete?workspace_id="+testWorkspaceID,
		map[string]any{"issue_ids": []string{theirs, notTheirs}}))
	if w.Code != http.StatusOK {
		t.Fatalf("batch delete: status %d; body %s", w.Code, w.Body.String())
	}

	var out struct {
		Deleted int `json:"deleted"`
		Refused int `json:"refused"`
	}
	if err := json.Unmarshal(w.Body.Bytes(), &out); err != nil {
		t.Fatalf("decode response: %v", err)
	}
	if out.Deleted != 1 || out.Refused != 1 {
		t.Fatalf("deleted=%d refused=%d, want 1 and 1; body %s", out.Deleted, out.Refused, w.Body.String())
	}
	if issueExists(t, theirs) {
		t.Fatal("the member's own issue was not deleted")
	}
	if !issueExists(t, notTheirs) {
		t.Fatal("someone else's issue was deleted by a plain member")
	}
}
