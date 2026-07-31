package handler

import (
	"context"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"testing"
)

// member_project is the single binding table for every role (migration 311).
// The part that did not exist before in any form: revoking access has to say
// what happens to the work assigned to the person losing it. Removing a member
// used to leave issues pointing at a user row that no longer had a membership.

func memberProjectFixture(t *testing.T) (memberID, memberUserID, projectID string) {
	t.Helper()
	ctx := context.Background()

	memberUserID = billingRoleUser(t, "scope-member@multica.test", "member")
	if err := testPool.QueryRow(ctx,
		`SELECT id FROM member WHERE user_id = $1 AND workspace_id = $2`,
		memberUserID, testWorkspaceID).Scan(&memberID); err != nil {
		t.Fatalf("load member row: %v", err)
	}

	if err := testPool.QueryRow(ctx, `
		INSERT INTO project (workspace_id, title) VALUES ($1, 'scope test project') RETURNING id
	`, testWorkspaceID).Scan(&projectID); err != nil {
		t.Fatalf("create project: %v", err)
	}
	t.Cleanup(func() {
		testPool.Exec(context.Background(), `DELETE FROM member_project WHERE project_id = $1`, projectID)
		testPool.Exec(context.Background(), `DELETE FROM project WHERE id = $1`, projectID)
	})
	return memberID, memberUserID, projectID
}

func bindProject(t *testing.T, memberID, projectID string) *httptest.ResponseRecorder {
	t.Helper()
	w := httptest.NewRecorder()
	testHandler.SetGuestProject(w, withURLParams(
		newRequest(http.MethodPut, "/api/workspaces/"+testWorkspaceID+"/members/"+memberID+"/projects",
			map[string]any{"project_id": projectID}),
		"id", testWorkspaceID, "memberId", memberID))
	return w
}

func TestMemberProject_BindIsIdempotentAndShowsUpOnTheMember(t *testing.T) {
	if testHandler == nil || testPool == nil {
		t.Skip("database not available")
	}
	memberID, memberUserID, projectID := memberProjectFixture(t)

	for i := 0; i < 2; i++ {
		if w := bindProject(t, memberID, projectID); w.Code != http.StatusOK {
			t.Fatalf("bind #%d: status %d; body %s", i+1, w.Code, w.Body.String())
		}
	}

	var bindings int
	if err := testPool.QueryRow(context.Background(),
		`SELECT count(*) FROM member_project WHERE user_id = $1 AND project_id = $2`,
		memberUserID, projectID).Scan(&bindings); err != nil {
		t.Fatalf("count bindings: %v", err)
	}
	if bindings != 1 {
		t.Fatalf("bindings = %d after two binds, want 1", bindings)
	}

	// The members list carries it under the general name, for any role.
	list := httptest.NewRecorder()
	testHandler.ListMembersWithUser(list, withURLParam(
		newRequest(http.MethodGet, "/api/workspaces/"+testWorkspaceID+"/members", nil),
		"id", testWorkspaceID))
	if list.Code != http.StatusOK {
		t.Fatalf("list members: status %d", list.Code)
	}
	var members []struct {
		UserID      string   `json:"user_id"`
		AccessScope string   `json:"access_scope"`
		ProjectIDs  []string `json:"project_ids"`
	}
	if err := json.Unmarshal(list.Body.Bytes(), &members); err != nil {
		t.Fatalf("decode members: %v", err)
	}
	found := false
	for _, m := range members {
		if m.UserID != memberUserID {
			continue
		}
		found = true
		if m.AccessScope != "workspace" {
			t.Fatalf("access_scope = %q, want the default %q", m.AccessScope, "workspace")
		}
		if len(m.ProjectIDs) != 1 || m.ProjectIDs[0] != projectID {
			t.Fatalf("project_ids = %v, want [%s]", m.ProjectIDs, projectID)
		}
	}
	if !found {
		t.Fatal("the bound member is missing from the members list")
	}
}

// Revoking access to a project must not silently orphan the work assigned in
// it. With no decision in the body the endpoint refuses and reports the count.
func TestMemberProject_UnbindRefusesUntilAssignedWorkIsDecided(t *testing.T) {
	if testHandler == nil || testPool == nil {
		t.Skip("database not available")
	}
	memberID, memberUserID, projectID := memberProjectFixture(t)
	bindProject(t, memberID, projectID)

	var issueID string
	if err := testPool.QueryRow(context.Background(), `
		INSERT INTO issue (workspace_id, project_id, title, creator_type, creator_id,
		                   assignee_type, assignee_id, priority, number)
		VALUES ($1, $2, 'assigned to the scoped member', 'member', $3, 'member', $3, 'medium',
		        (SELECT COALESCE(MAX(number), 0) + 1 FROM issue WHERE workspace_id = $1))
		RETURNING id
	`, testWorkspaceID, projectID, memberUserID).Scan(&issueID); err != nil {
		t.Fatalf("seed assigned issue: %v", err)
	}
	t.Cleanup(func() { testPool.Exec(context.Background(), `DELETE FROM issue WHERE id = $1`, issueID) })

	undecided := httptest.NewRecorder()
	testHandler.UnsetGuestProject(undecided, withURLParams(
		newRequest(http.MethodDelete, "/api/workspaces/"+testWorkspaceID+"/members/"+memberID+"/projects",
			map[string]any{"project_id": projectID}),
		"id", testWorkspaceID, "memberId", memberID))
	if undecided.Code != http.StatusConflict {
		t.Fatalf("undecided unbind: status %d, want 409; body %s", undecided.Code, undecided.Body.String())
	}

	var conflict struct {
		AssignedIssues int `json:"assigned_issues"`
	}
	if err := json.Unmarshal(undecided.Body.Bytes(), &conflict); err != nil {
		t.Fatalf("decode conflict: %v", err)
	}
	if conflict.AssignedIssues != 1 {
		t.Fatalf("assigned_issues = %d, want 1", conflict.AssignedIssues)
	}

	// The binding survived the refusal.
	var still int
	testPool.QueryRow(context.Background(),
		`SELECT count(*) FROM member_project WHERE user_id = $1 AND project_id = $2`,
		memberUserID, projectID).Scan(&still)
	if still != 1 {
		t.Fatalf("binding count = %d after the refusal, want 1", still)
	}

	// Deciding "unassign" goes through and clears the assignee.
	decided := httptest.NewRecorder()
	testHandler.UnsetGuestProject(decided, withURLParams(
		newRequest(http.MethodDelete, "/api/workspaces/"+testWorkspaceID+"/members/"+memberID+"/projects",
			map[string]any{"project_id": projectID, "on_assigned_issues": "unassign"}),
		"id", testWorkspaceID, "memberId", memberID))
	if decided.Code != http.StatusOK {
		t.Fatalf("decided unbind: status %d; body %s", decided.Code, decided.Body.String())
	}

	var assigneeType, assigneeID *string
	if err := testPool.QueryRow(context.Background(),
		`SELECT assignee_type, assignee_id::text FROM issue WHERE id = $1`, issueID).
		Scan(&assigneeType, &assigneeID); err != nil {
		t.Fatalf("read back issue: %v", err)
	}
	if assigneeType != nil || assigneeID != nil {
		t.Fatalf("assignee not cleared: type=%v id=%v", assigneeType, assigneeID)
	}
}

// Handing the work to a named colleague is the other half of the decision.
func TestMemberProject_UnbindCanReassignToAnotherMember(t *testing.T) {
	if testHandler == nil || testPool == nil {
		t.Skip("database not available")
	}
	memberID, memberUserID, projectID := memberProjectFixture(t)
	successorID := billingRoleUser(t, "scope-successor@multica.test", "member")
	bindProject(t, memberID, projectID)

	var issueID string
	if err := testPool.QueryRow(context.Background(), `
		INSERT INTO issue (workspace_id, project_id, title, creator_type, creator_id,
		                   assignee_type, assignee_id, priority, number)
		VALUES ($1, $2, 'handed over', 'member', $3, 'member', $3, 'medium',
		        (SELECT COALESCE(MAX(number), 0) + 1 FROM issue WHERE workspace_id = $1))
		RETURNING id
	`, testWorkspaceID, projectID, memberUserID).Scan(&issueID); err != nil {
		t.Fatalf("seed assigned issue: %v", err)
	}
	t.Cleanup(func() { testPool.Exec(context.Background(), `DELETE FROM issue WHERE id = $1`, issueID) })

	w := httptest.NewRecorder()
	testHandler.UnsetGuestProject(w, withURLParams(
		newRequest(http.MethodDelete, "/api/workspaces/"+testWorkspaceID+"/members/"+memberID+"/projects",
			map[string]any{
				"project_id":         projectID,
				"on_assigned_issues": "reassign",
				"reassign_to":        successorID,
			}),
		"id", testWorkspaceID, "memberId", memberID))
	if w.Code != http.StatusOK {
		t.Fatalf("reassigning unbind: status %d; body %s", w.Code, w.Body.String())
	}

	var newAssignee string
	if err := testPool.QueryRow(context.Background(),
		`SELECT assignee_id::text FROM issue WHERE id = $1`, issueID).Scan(&newAssignee); err != nil {
		t.Fatalf("read back issue: %v", err)
	}
	if newAssignee != successorID {
		t.Fatalf("assignee = %s, want the successor %s", newAssignee, successorID)
	}
}

// Removing a member drops their bindings and clears their assignments in the
// same transaction as the member row. Before this, both were left behind.
func TestRemoveMember_DropsBindingsAndClearsAssignments(t *testing.T) {
	if testHandler == nil || testPool == nil {
		t.Skip("database not available")
	}
	ctx := context.Background()
	memberID, memberUserID, projectID := memberProjectFixture(t)
	bindProject(t, memberID, projectID)

	var issueID string
	if err := testPool.QueryRow(ctx, `
		INSERT INTO issue (workspace_id, project_id, title, creator_type, creator_id,
		                   assignee_type, assignee_id, priority, number)
		VALUES ($1, $2, 'left behind by a departing member', 'member', $3, 'member', $3, 'medium',
		        (SELECT COALESCE(MAX(number), 0) + 1 FROM issue WHERE workspace_id = $1))
		RETURNING id
	`, testWorkspaceID, projectID, memberUserID).Scan(&issueID); err != nil {
		t.Fatalf("seed assigned issue: %v", err)
	}
	t.Cleanup(func() { testPool.Exec(context.Background(), `DELETE FROM issue WHERE id = $1`, issueID) })

	w := httptest.NewRecorder()
	testHandler.DeleteMember(w, withURLParams(
		newRequest(http.MethodDelete, "/api/workspaces/"+testWorkspaceID+"/members/"+memberID, nil),
		"id", testWorkspaceID, "memberId", memberID))
	if w.Code != http.StatusNoContent && w.Code != http.StatusOK {
		t.Fatalf("delete member: status %d; body %s", w.Code, w.Body.String())
	}

	var bindings int
	testPool.QueryRow(ctx, `SELECT count(*) FROM member_project WHERE user_id = $1`, memberUserID).Scan(&bindings)
	if bindings != 0 {
		t.Fatalf("bindings left after removal: %d", bindings)
	}

	var assigneeID *string
	if err := testPool.QueryRow(ctx,
		`SELECT assignee_id::text FROM issue WHERE id = $1`, issueID).Scan(&assigneeID); err != nil {
		t.Fatalf("read back issue: %v", err)
	}
	if assigneeID != nil {
		t.Fatalf("issue still assigned to the removed member: %v", *assigneeID)
	}
}
