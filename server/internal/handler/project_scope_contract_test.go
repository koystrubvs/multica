package handler

import (
	"context"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"

	"github.com/multica-ai/multica/server/internal/middleware"
	db "github.com/multica-ai/multica/server/pkg/db/generated"
)

// scopedMemberFixture creates a second person in the test workspace who may see
// exactly one of two projects, plus one issue in each project and one with no
// project at all. It returns the member's user id and the two project ids.
func scopedMemberFixture(t *testing.T) (userID, boundProject, unboundProject string) {
	t.Helper()
	ctx := context.Background()

	email := "scoped.member+" + strings.ToLower(t.Name()) + "@example.test"
	email = strings.ReplaceAll(email, "/", "-")
	if err := testPool.QueryRow(ctx, `
		INSERT INTO "user" (name, email) VALUES ('Scoped Member', $1)
		RETURNING id
	`, email).Scan(&userID); err != nil {
		t.Fatal(err)
	}
	if _, err := testPool.Exec(ctx, `
		INSERT INTO member (workspace_id, user_id, role, access_scope)
		VALUES ($1, $2, 'member', 'projects')
	`, testWorkspaceID, userID); err != nil {
		t.Fatal(err)
	}
	if err := testPool.QueryRow(ctx, `
		INSERT INTO project (workspace_id, title, status) VALUES ($1, 'Scope bound project', 'in_progress')
		RETURNING id
	`, testWorkspaceID).Scan(&boundProject); err != nil {
		t.Fatal(err)
	}
	if err := testPool.QueryRow(ctx, `
		INSERT INTO project (workspace_id, title, status) VALUES ($1, 'Scope unbound project', 'in_progress')
		RETURNING id
	`, testWorkspaceID).Scan(&unboundProject); err != nil {
		t.Fatal(err)
	}
	if _, err := testPool.Exec(ctx, `
		INSERT INTO member_project (workspace_id, user_id, project_id) VALUES ($1, $2, $3)
	`, testWorkspaceID, userID, boundProject); err != nil {
		t.Fatal(err)
	}
	var nextNumber int
	if err := testPool.QueryRow(ctx,
		`SELECT COALESCE(max(number), 0) + 1 FROM issue WHERE workspace_id = $1`,
		testWorkspaceID).Scan(&nextNumber); err != nil {
		t.Fatal(err)
	}
	for offset, seed := range []struct {
		title   string
		project any
	}{
		{"Scope visible issue zzqq", boundProject},
		{"Scope hidden issue zzqq", unboundProject},
		{"Scope orphan issue zzqq", nil},
	} {
		if _, err := testPool.Exec(ctx, `
			INSERT INTO issue (workspace_id, title, creator_type, creator_id, project_id, number)
			VALUES ($1, $2, 'member', $3, $4, $5)
		`, testWorkspaceID, seed.title, testUserID, seed.project, nextNumber+offset); err != nil {
			t.Fatal(err)
		}
	}

	t.Cleanup(func() {
		cleanup := context.Background()
		testPool.Exec(cleanup, `DELETE FROM issue WHERE workspace_id = $1 AND title LIKE 'Scope %'`, testWorkspaceID)
		testPool.Exec(cleanup, `DELETE FROM member_project WHERE user_id = $1`, userID)
		testPool.Exec(cleanup, `DELETE FROM project WHERE id = ANY(ARRAY[$1, $2]::uuid[])`, boundProject, unboundProject)
		testPool.Exec(cleanup, `DELETE FROM member WHERE user_id = $1`, userID)
		testPool.Exec(cleanup, `DELETE FROM "user" WHERE id = $1`, userID)
	})
	return userID, boundProject, unboundProject
}

// middlewareWorkspaceContext supplies what the workspace middleware normally
// injects. The inbox handlers read the workspace from the context, and calling
// them directly skips that step. The project scope is deliberately NOT injected
// — leaving it out exercises the fallback resolve that the auth-only routes
// rely on.
func middlewareWorkspaceContext(req *http.Request) context.Context {
	member, err := testHandler.Queries.GetMemberByUserAndWorkspace(req.Context(),
		db.GetMemberByUserAndWorkspaceParams{
			UserID:      parseUUID(req.Header.Get("X-User-ID")),
			WorkspaceID: parseUUID(testWorkspaceID),
		})
	if err != nil {
		panic("scoped member fixture is not a member: " + err.Error())
	}
	return middleware.SetMemberContext(req.Context(), testWorkspaceID, member)
}

func requestAsScopedMember(userID, method, path string) *http.Request {
	req := newRequest(method, path, nil)
	req.Header.Set("X-User-ID", userID)
	return req
}

// The whole point of the mechanism: every list surface a scoped member can
// reach returns their project and nothing else. Each surface is asserted
// separately because each one builds its SQL its own way — the table compiler,
// two dynamic builders and two static queries — and closing four of five is
// worth nothing.
func TestScopedMemberSeesOnlyBoundProjects(t *testing.T) {
	if testHandler == nil || testPool == nil {
		t.Skip("database not available")
	}
	userID, boundProject, _ := scopedMemberFixture(t)

	surfaces := []struct {
		name    string
		path    string
		handler func(http.ResponseWriter, *http.Request)
	}{
		{"paginated issue list", "/api/issues?limit=100", testHandler.ListIssues},
		{"open-only issue list", "/api/issues?open_only=true", testHandler.ListIssues},
		{"grouped issue list", "/api/issues/grouped?group_by=assignee", testHandler.ListGroupedIssues},
		{"search", "/api/issues/search?q=zzqq", testHandler.SearchIssues},
	}
	for _, surface := range surfaces {
		t.Run(surface.name, func(t *testing.T) {
			w := httptest.NewRecorder()
			surface.handler(w, requestAsScopedMember(userID, "GET", surface.path))
			if w.Code != http.StatusOK {
				t.Fatalf("status = %d, body = %s", w.Code, w.Body.String())
			}
			body := w.Body.String()
			if !strings.Contains(body, "Scope visible issue") {
				t.Errorf("the member's own project is missing from %s: %s", surface.name, body)
			}
			if strings.Contains(body, "Scope hidden issue") {
				t.Errorf("%s leaked an issue from an unbound project", surface.name)
			}
			if strings.Contains(body, "Scope orphan issue") {
				t.Errorf("%s leaked a project-less issue (billing autopilots live there)", surface.name)
			}
		})
	}

	t.Run("project list", func(t *testing.T) {
		w := httptest.NewRecorder()
		testHandler.ListProjects(w, requestAsScopedMember(userID, "GET", "/api/projects"))
		if w.Code != http.StatusOK {
			t.Fatalf("status = %d, body = %s", w.Code, w.Body.String())
		}
		body := w.Body.String()
		if !strings.Contains(body, "Scope bound project") {
			t.Errorf("the member's own project is missing: %s", body)
		}
		if strings.Contains(body, "Scope unbound project") {
			t.Error("the project list leaked a project the member is not bound to")
		}
	})

	// The owner is unaffected — the predicate must not be a tax on the person
	// who is allowed to see everything.
	t.Run("owner still sees everything", func(t *testing.T) {
		w := httptest.NewRecorder()
		testHandler.ListIssues(w, newRequest("GET", "/api/issues?limit=100", nil))
		if w.Code != http.StatusOK {
			t.Fatalf("status = %d", w.Code)
		}
		for _, title := range []string{"Scope visible issue", "Scope hidden issue", "Scope orphan issue"} {
			if !strings.Contains(w.Body.String(), title) {
				t.Errorf("owner lost sight of %q", title)
			}
		}
	})

	_ = boundProject
}

// A member switched to 'projects' mode before anyone gave them a project must
// see an empty workspace, not the whole one. This is the failure that turns the
// feature into decoration, and it is one missing WHERE away at all times.
func TestScopedMemberWithoutGrantsSeesNothing(t *testing.T) {
	if testHandler == nil || testPool == nil {
		t.Skip("database not available")
	}
	userID, boundProject, _ := scopedMemberFixture(t)
	if _, err := testPool.Exec(context.Background(),
		`DELETE FROM member_project WHERE user_id = $1`, userID); err != nil {
		t.Fatal(err)
	}
	_ = boundProject

	w := httptest.NewRecorder()
	testHandler.ListIssues(w, requestAsScopedMember(userID, "GET", "/api/issues?limit=100"))
	if w.Code != http.StatusOK {
		t.Fatalf("status = %d, body = %s", w.Code, w.Body.String())
	}
	var payload struct {
		Issues []json.RawMessage `json:"issues"`
		Total  int               `json:"total"`
	}
	if err := json.Unmarshal(w.Body.Bytes(), &payload); err != nil {
		t.Fatalf("decode: %v (body %s)", err, w.Body.String())
	}
	if len(payload.Issues) != 0 || payload.Total != 0 {
		t.Fatalf("a member with no granted projects saw %d issue(s), total=%d", len(payload.Issues), payload.Total)
	}
}

// Access can be edited from either end, so the rule that revoking must say
// where the assigned work goes has to hold at both. A second door that just
// deletes the binding would leave issues with an assignee who cannot open them
// — which is the exact failure the member-side 409 was added to prevent.
func TestProjectSideRevokeStillDemandsAnAnswer(t *testing.T) {
	if testHandler == nil || testPool == nil {
		t.Skip("database not available")
	}
	ctx := context.Background()
	userID, boundProject, _ := scopedMemberFixture(t)

	// Put work on the member inside the project they are about to lose.
	if _, err := testPool.Exec(ctx, `
		UPDATE issue SET assignee_type = 'member', assignee_id = $1
		WHERE workspace_id = $2 AND project_id = $3
	`, userID, testWorkspaceID, boundProject); err != nil {
		t.Fatal(err)
	}

	revoke := func(body any) *httptest.ResponseRecorder {
		req := newRequest("DELETE", "/api/projects/"+boundProject+"/members/"+userID, body)
		req = withURLParams(req, "id", boundProject, "userId", userID)
		w := httptest.NewRecorder()
		testHandler.UnbindProjectMember(w, req)
		return w
	}

	w := revoke(map[string]any{})
	if w.Code != http.StatusConflict {
		t.Fatalf("revoking with work assigned returned %d, want 409: %s", w.Code, w.Body.String())
	}
	var conflict struct {
		AssignedIssues int      `json:"assigned_issues"`
		Choices        []string `json:"choices"`
	}
	if err := json.Unmarshal(w.Body.Bytes(), &conflict); err != nil {
		t.Fatalf("decode conflict: %v", err)
	}
	if conflict.AssignedIssues != 1 || len(conflict.Choices) != 2 {
		t.Fatalf("conflict does not say what is at stake: %s", w.Body.String())
	}

	var stillBound bool
	if err := testPool.QueryRow(ctx,
		`SELECT EXISTS(SELECT 1 FROM member_project WHERE user_id = $1 AND project_id = $2)`,
		userID, boundProject).Scan(&stillBound); err != nil {
		t.Fatal(err)
	}
	if !stillBound {
		t.Fatal("the binding was removed despite the refusal")
	}

	if w := revoke(map[string]any{"on_assigned_issues": "unassign"}); w.Code != http.StatusOK {
		t.Fatalf("answered revoke returned %d: %s", w.Code, w.Body.String())
	}
	var orphanedAssignee *string
	if err := testPool.QueryRow(ctx, `
		SELECT assignee_id::text FROM issue
		WHERE workspace_id = $1 AND project_id = $2
	`, testWorkspaceID, boundProject).Scan(&orphanedAssignee); err != nil {
		t.Fatal(err)
	}
	if orphanedAssignee != nil {
		t.Fatalf("issue kept an assignee who lost the project: %s", *orphanedAssignee)
	}
}

// The project-side list has to agree with the resolver, or the screen shows a
// checkbox that does not match what the person can actually open.
func TestProjectMemberListMatchesTheResolver(t *testing.T) {
	if testHandler == nil || testPool == nil {
		t.Skip("database not available")
	}
	userID, boundProject, unboundProject := scopedMemberFixture(t)

	read := func(projectID string) map[string]ProjectMemberAccess {
		req := withURLParams(newRequest("GET", "/api/projects/"+projectID+"/members", nil), "id", projectID)
		w := httptest.NewRecorder()
		testHandler.ListProjectMembers(w, req)
		if w.Code != http.StatusOK {
			t.Fatalf("status = %d, body = %s", w.Code, w.Body.String())
		}
		var payload struct {
			Members []ProjectMemberAccess `json:"members"`
		}
		if err := json.Unmarshal(w.Body.Bytes(), &payload); err != nil {
			t.Fatalf("decode: %v", err)
		}
		byUser := make(map[string]ProjectMemberAccess, len(payload.Members))
		for _, m := range payload.Members {
			byUser[m.UserID] = m
		}
		return byUser
	}

	onBound := read(boundProject)
	if row, ok := onBound[userID]; !ok || !row.Bound || !row.Sees {
		t.Fatalf("the bound member is not shown as having access: %+v", row)
	}
	if row, ok := onBound[testUserID]; !ok || row.Bound || !row.Sees {
		t.Fatalf("the owner must show as seeing the project without a binding: %+v", row)
	}

	onUnbound := read(unboundProject)
	if row, ok := onUnbound[userID]; !ok || row.Bound || row.Sees {
		t.Fatalf("a scoped member must not show as seeing an unbound project: %+v", row)
	}
}

// A notification is addressed to the person, but it carries the issue's title
// and links to something they would get a 404 for. Withholding the issue while
// still announcing it is the same leak with extra steps.
func TestScopedMemberInboxHidesUnreachableIssues(t *testing.T) {
	if testHandler == nil || testPool == nil {
		t.Skip("database not available")
	}
	ctx := context.Background()
	userID, _, _ := scopedMemberFixture(t)

	rows, err := testPool.Query(ctx, `
		SELECT id, title FROM issue
		WHERE workspace_id = $1 AND title LIKE 'Scope %'
	`, testWorkspaceID)
	if err != nil {
		t.Fatal(err)
	}
	type seed struct{ id, title string }
	var seeds []seed
	for rows.Next() {
		var s seed
		if err := rows.Scan(&s.id, &s.title); err != nil {
			rows.Close()
			t.Fatal(err)
		}
		seeds = append(seeds, s)
	}
	rows.Close()
	if len(seeds) != 3 {
		t.Fatalf("expected three seeded issues, got %d", len(seeds))
	}
	for _, s := range seeds {
		if _, err := testPool.Exec(ctx, `
			INSERT INTO inbox_item (workspace_id, recipient_type, recipient_id, type, severity, issue_id, title)
			VALUES ($1, 'member', $2, 'mention', 'info', $3, $4)
		`, testWorkspaceID, userID, s.id, "Notice: "+s.title); err != nil {
			t.Fatal(err)
		}
	}
	t.Cleanup(func() {
		testPool.Exec(context.Background(),
			`DELETE FROM inbox_item WHERE recipient_id = $1`, userID)
	})

	req := requestAsScopedMember(userID, "GET", "/api/inbox")
	req = req.WithContext(middlewareWorkspaceContext(req))
	w := httptest.NewRecorder()
	testHandler.ListInbox(w, req)
	if w.Code != http.StatusOK {
		t.Fatalf("status = %d, body = %s", w.Code, w.Body.String())
	}
	body := w.Body.String()
	if !strings.Contains(body, "Notice: Scope visible issue") {
		t.Errorf("the member lost a notification about their own project: %s", body)
	}
	for _, hidden := range []string{"Notice: Scope hidden issue", "Notice: Scope orphan issue"} {
		if strings.Contains(body, hidden) {
			t.Errorf("inbox leaked %q", hidden)
		}
	}

	// The badge has to agree with the list, or it lights for an empty inbox.
	countRecorder := httptest.NewRecorder()
	countReq := requestAsScopedMember(userID, "GET", "/api/inbox/unread-count")
	countReq = countReq.WithContext(middlewareWorkspaceContext(countReq))
	testHandler.CountUnreadInbox(countRecorder, countReq)
	if countRecorder.Code != http.StatusOK {
		t.Fatalf("count status = %d, body = %s", countRecorder.Code, countRecorder.Body.String())
	}
	var counted struct {
		Count int `json:"count"`
	}
	if err := json.Unmarshal(countRecorder.Body.Bytes(), &counted); err != nil {
		t.Fatalf("decode count: %v (body %s)", err, countRecorder.Body.String())
	}
	if counted.Count != 1 {
		t.Fatalf("unread badge = %d, want 1 (the one reachable notification)", counted.Count)
	}
}

// Surfaces that cannot be split by project are refused outright. A scoped
// member who could open the usage dashboard would read the cost of work in
// projects they cannot see; one who could create an agent would then read the
// whole workspace through it, since an agent acts as the runtime owner.
func TestScopedMemberIsRefusedUndecomposableSurfaces(t *testing.T) {
	if testHandler == nil || testPool == nil {
		t.Skip("database not available")
	}
	userID, _, _ := scopedMemberFixture(t)

	surfaces := []struct {
		name    string
		method  string
		path    string
		body    any
		handler func(http.ResponseWriter, *http.Request)
	}{
		{"usage dashboard", "GET", "/api/dashboard/usage/daily", nil, testHandler.GetDashboardUsageDaily},
		{"usage by agent", "GET", "/api/dashboard/usage/by-agent", nil, testHandler.GetDashboardUsageByAgent},
		{"failures dashboard", "GET", "/api/dashboard/failures/daily", nil, testHandler.GetDashboardFailuresDaily},
		{"agent task snapshot", "GET", "/api/agent-task-snapshot", nil, testHandler.ListWorkspaceAgentTaskSnapshot},
		{"working agents", "GET", "/api/working-agents", nil, testHandler.ListWorkspaceWorkingAgents},
		{"runtime usage", "GET", "/api/runtimes/x/usage", nil, testHandler.GetRuntimeUsage},
		{"create agent", "POST", "/api/agents", map[string]any{"name": "Scope escape"}, testHandler.CreateAgent},
	}
	for _, surface := range surfaces {
		t.Run(surface.name, func(t *testing.T) {
			req := newRequest(surface.method, surface.path, surface.body)
			req.Header.Set("X-User-ID", userID)
			w := httptest.NewRecorder()
			surface.handler(w, req)
			if w.Code != http.StatusForbidden {
				t.Fatalf("status = %d, want 403: %s", w.Code, w.Body.String())
			}
		})
	}

	// The same surfaces stay open to the owner: this is a containment for
	// scoped members, not a product-wide downgrade.
	t.Run("owner keeps the dashboard", func(t *testing.T) {
		w := httptest.NewRecorder()
		testHandler.GetDashboardUsageDaily(w, newRequest("GET", "/api/dashboard/usage/daily", nil))
		if w.Code == http.StatusForbidden {
			t.Fatalf("the owner was refused their own dashboard: %s", w.Body.String())
		}
	})
}

// The gate has to judge the project the issue will END UP in. A sub-issue
// inherits its parent's project when none is given, and that back-fill happens
// after the handler's check — so passing only a parent in an unreachable
// project used to place work there and return that project's UUID.
func TestScopedMemberCannotFileIntoAnUnboundProjectViaParent(t *testing.T) {
	if testHandler == nil || testPool == nil {
		t.Skip("database not available")
	}
	ctx := context.Background()
	userID, _, unboundProject := scopedMemberFixture(t)

	var parentID string
	if err := testPool.QueryRow(ctx, `
		SELECT id FROM issue
		WHERE workspace_id = $1 AND project_id = $2 LIMIT 1
	`, testWorkspaceID, unboundProject).Scan(&parentID); err != nil {
		t.Fatal(err)
	}

	req := newRequest("POST", "/api/issues", map[string]any{
		"title":           "Smuggled into a project they cannot see",
		"parent_issue_id": parentID,
	})
	req.Header.Set("X-User-ID", userID)
	w := httptest.NewRecorder()
	testHandler.CreateIssue(w, req)
	if w.Code != http.StatusForbidden {
		t.Fatalf("status = %d, want 403: %s", w.Code, w.Body.String())
	}

	var smuggled int
	if err := testPool.QueryRow(ctx, `
		SELECT count(*) FROM issue WHERE workspace_id = $1 AND title = 'Smuggled into a project they cannot see'
	`, testWorkspaceID).Scan(&smuggled); err != nil {
		t.Fatal(err)
	}
	if smuggled != 0 {
		t.Fatalf("the issue was created anyway (%d rows)", smuggled)
	}
}

// A file is exactly as reachable as the issue it hangs on. The attachment id
// travels in comment markdown, so "they never see the id" is not a defence.
func TestScopedMemberCannotDownloadAnUnreachableAttachment(t *testing.T) {
	if testHandler == nil || testPool == nil {
		t.Skip("database not available")
	}
	ctx := context.Background()
	userID, boundProject, unboundProject := scopedMemberFixture(t)

	attachmentFor := func(projectID string, name string) string {
		var issueID, attachmentID string
		if err := testPool.QueryRow(ctx, `
			SELECT id FROM issue WHERE workspace_id = $1 AND project_id = $2 LIMIT 1
		`, testWorkspaceID, projectID).Scan(&issueID); err != nil {
			t.Fatal(err)
		}
		if err := testPool.QueryRow(ctx, `
			INSERT INTO attachment (workspace_id, issue_id, uploader_type, uploader_id, filename, url, content_type, size_bytes)
			VALUES ($1, $2, 'member', $3, $4, 'https://example.test/f', 'text/plain', 10)
			RETURNING id
		`, testWorkspaceID, issueID, testUserID, name).Scan(&attachmentID); err != nil {
			t.Fatal(err)
		}
		return attachmentID
	}
	reachable := attachmentFor(boundProject, "reachable.txt")
	unreachable := attachmentFor(unboundProject, "unreachable.txt")
	t.Cleanup(func() {
		testPool.Exec(context.Background(),
			`DELETE FROM attachment WHERE id = ANY(ARRAY[$1, $2]::uuid[])`, reachable, unreachable)
	})

	get := func(attachmentID string) int {
		req := withURLParams(newRequest("GET", "/api/attachments/"+attachmentID, nil), "id", attachmentID)
		req.Header.Set("X-User-ID", userID)
		w := httptest.NewRecorder()
		testHandler.GetAttachmentByID(w, req)
		return w.Code
	}
	if code := get(unreachable); code != http.StatusNotFound {
		t.Fatalf("attachment on an unbound project answered %d, want 404", code)
	}
	if code := get(reachable); code == http.StatusNotFound {
		t.Fatal("the member lost an attachment on their own project")
	}
}
