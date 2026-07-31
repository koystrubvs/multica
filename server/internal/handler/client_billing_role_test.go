package handler

import (
	"context"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
)

// Money is gated on the workspace ROLE: owner and admin see rouble prices, the
// agency markup, the charge ledger and the Elba wiring; ordinary members and
// guests do not.
//
// Before this gate existed, requireBillingEditor admitted "any member except
// guests" and every billing READ was ungated entirely — an employee could open
// an issue and read the client price and the markup, or edit the markup and
// issue a real invoice. This file is the regression fence for that.

// billingRoleUser creates a user with the given workspace role and returns its
// user id.
func billingRoleUser(t *testing.T, email, role string) string {
	t.Helper()
	ctx := context.Background()
	var userID string
	if err := testPool.QueryRow(ctx, `
		INSERT INTO "user" (name, email) VALUES ($1, $2) RETURNING id
	`, email, email).Scan(&userID); err != nil {
		t.Fatalf("create %s user: %v", role, err)
	}
	t.Cleanup(func() { testPool.Exec(context.Background(), `DELETE FROM "user" WHERE id = $1`, userID) })
	if _, err := testPool.Exec(ctx, `
		INSERT INTO member (workspace_id, user_id, role) VALUES ($1, $2, $3)
	`, testWorkspaceID, userID, role); err != nil {
		t.Fatalf("add %s member: %v", role, err)
	}
	return userID
}

// billingRoleFixture seeds one project and one issue to address the
// per-resource endpoints with, plus the three non-owner actors. testUserID is
// already the workspace owner.
func billingRoleFixture(t *testing.T) (projectID, issueID, adminID, memberID, guestID string) {
	t.Helper()
	ctx := context.Background()

	if err := testPool.QueryRow(ctx, `
		INSERT INTO project (workspace_id, title) VALUES ($1, $2) RETURNING id
	`, testWorkspaceID, "billing-role-test-project").Scan(&projectID); err != nil {
		t.Fatalf("create project: %v", err)
	}
	t.Cleanup(func() { testPool.Exec(context.Background(), `DELETE FROM project WHERE id = $1`, projectID) })

	if err := testPool.QueryRow(ctx, `
		INSERT INTO issue (workspace_id, project_id, title, creator_type, creator_id, priority)
		VALUES ($1, $2, 'billing role test issue', 'member', $3, 'medium')
		RETURNING id
	`, testWorkspaceID, projectID, testUserID).Scan(&issueID); err != nil {
		t.Fatalf("create issue: %v", err)
	}
	t.Cleanup(func() { testPool.Exec(context.Background(), `DELETE FROM issue WHERE id = $1`, issueID) })

	adminID = billingRoleUser(t, "billing-role-admin@multica.test", "admin")
	memberID = billingRoleUser(t, "billing-role-member@multica.test", "member")
	guestID = billingRoleUser(t, "billing-role-guest@multica.test", "guest")
	return projectID, issueID, adminID, memberID, guestID
}

// TestBillingEndpoints_RoleGate walks every money read endpoint with the four
// roles.
//
// Staff are asserted as "not 403" rather than "200": several of these return
// 404 when nothing is configured for the fixture project, which is a correct
// answer and not what this test is about. Members and guests must be refused
// before the handler ever looks at the data.
func TestBillingEndpoints_RoleGate(t *testing.T) {
	if testHandler == nil || testPool == nil {
		t.Skip("database not available")
	}

	projectID, issueID, adminID, memberID, guestID := billingRoleFixture(t)

	endpoints := []struct {
		name    string
		method  string
		path    string
		handler http.HandlerFunc
		param   [2]string // chi URL param, empty when the route has none
	}{
		{
			name:    "GetIssueBillingCost",
			method:  http.MethodGet,
			path:    "/api/issues/" + issueID + "/billing/cost?workspace_id=" + testWorkspaceID,
			handler: testHandler.GetIssueBillingCost,
			param:   [2]string{"id", issueID},
		},
		{
			name:    "GetIssueBillingCharge",
			method:  http.MethodGet,
			path:    "/api/issues/" + issueID + "/billing/charge?workspace_id=" + testWorkspaceID,
			handler: testHandler.GetIssueBillingCharge,
			param:   [2]string{"id", issueID},
		},
		{
			name:    "GetProjectBillingConfig",
			method:  http.MethodGet,
			path:    "/api/projects/" + projectID + "/billing/config?workspace_id=" + testWorkspaceID,
			handler: testHandler.GetProjectBillingConfig,
			param:   [2]string{"id", projectID},
		},
		{
			name:    "GetWorkspaceBillingConfig",
			method:  http.MethodGet,
			path:    "/api/billing/workspace-config?workspace_id=" + testWorkspaceID,
			handler: testHandler.GetWorkspaceBillingConfig,
		},
		{
			name:    "ListContractorBillingConfigs",
			method:  http.MethodGet,
			path:    "/api/billing/contractor-configs?workspace_id=" + testWorkspaceID,
			handler: testHandler.ListContractorBillingConfigs,
		},
		{
			name:    "ListInvoiceableContractorGroups",
			method:  http.MethodGet,
			path:    "/api/billing/invoiceable?workspace_id=" + testWorkspaceID,
			handler: testHandler.ListInvoiceableContractorGroups,
		},
	}

	// Expected outcomes, not exact codes:
	//   allow  — the role gate must not fire. 404 is fine (nothing configured
	//            for the fixture project) — we only assert it is not a 403.
	//   forbid — exactly 403 from the money gate. A member can legitimately
	//            see the project and the issue, so the refusal has to come
	//            from the role check and be visible as one.
	//   refuse — 403 or 404. On issue- and project-scoped routes a guest is
	//            stopped even earlier, by the guest project scope, which
	//            answers 404 on purpose: a guest must not learn that an issue
	//            outside their project exists.
	const (
		allow  = "allow"
		forbid = "forbid"
		refuse = "refuse"
	)

	actors := []struct {
		role   string
		userID string
		want   string
	}{
		{"owner", testUserID, allow},
		{"admin", adminID, allow},
		{"member", memberID, forbid},
		{"guest", guestID, refuse},
	}

	for _, ep := range endpoints {
		for _, actor := range actors {
			t.Run(ep.name+"/"+actor.role, func(t *testing.T) {
				req := newRequestAs(actor.userID, ep.method, ep.path, nil)
				if ep.param[0] != "" {
					req = withURLParam(req, ep.param[0], ep.param[1])
				}
				w := httptest.NewRecorder()
				ep.handler(w, req)

				switch actor.want {
				case allow:
					if w.Code == http.StatusForbidden {
						t.Fatalf("%s as %s: role-refused with 403, want access; body: %s",
							ep.name, actor.role, w.Body.String())
					}
				case forbid:
					if w.Code != http.StatusForbidden {
						t.Fatalf("%s as %s: got status %d, want 403; body: %s",
							ep.name, actor.role, w.Code, w.Body.String())
					}
				case refuse:
					if w.Code != http.StatusForbidden && w.Code != http.StatusNotFound {
						t.Fatalf("%s as %s: got status %d, want 403 or 404; body: %s",
							ep.name, actor.role, w.Code, w.Body.String())
					}
				}
			})
		}
	}
}

// TestListIssueCostTotals_RoleGate covers the board's money line separately:
// it is the one endpoint that was already gated, at "owner" only, and the gate
// widened to admin when admin became the money role.
func TestListIssueCostTotals_RoleGate(t *testing.T) {
	if testHandler == nil || testPool == nil {
		t.Skip("database not available")
	}
	_, _, adminID, memberID, guestID := billingRoleFixture(t)

	// No issue or project in the path here, so the guest reaches the same
	// role gate as a member and both must see exactly 403.
	for _, actor := range []struct {
		role      string
		userID    string
		forbidden bool
	}{
		{"owner", testUserID, false},
		{"admin", adminID, false},
		{"member", memberID, true},
		{"guest", guestID, true},
	} {
		t.Run(actor.role, func(t *testing.T) {
			req := newRequestAs(actor.userID, http.MethodPost,
				"/api/issues/table/cost-totals?workspace_id="+testWorkspaceID,
				map[string]any{"group": map[string]any{"field": "status"}})
			w := httptest.NewRecorder()
			testHandler.ListIssueCostTotals(w, req)

			forbidden := w.Code == http.StatusForbidden
			if forbidden != actor.forbidden {
				t.Fatalf("cost-totals as %s: got status %d, want forbidden=%v; body: %s",
					actor.role, w.Code, actor.forbidden, w.Body.String())
			}
		})
	}
}

// TestGetIssueUsage_TokensStayVisibleMoneyDoesNot pins the split the owner
// asked for: an employee sees how many tokens their task burned, but no USD.
// The token counts are what the issue sidebar renders, so gating the whole
// endpoint would have been wrong.
func TestGetIssueUsage_TokensStayVisibleMoneyDoesNot(t *testing.T) {
	if testHandler == nil || testPool == nil {
		t.Skip("database not available")
	}
	_, issueID, adminID, memberID, _ := billingRoleFixture(t)

	for _, tc := range []struct {
		role    string
		userID  string
		wantUSD bool
	}{
		{"owner", testUserID, true},
		{"admin", adminID, true},
		{"member", memberID, false},
	} {
		t.Run(tc.role, func(t *testing.T) {
			req := withURLParam(
				newRequestAs(tc.userID, http.MethodGet,
					"/api/issues/"+issueID+"/usage?workspace_id="+testWorkspaceID, nil),
				"id", issueID)
			w := httptest.NewRecorder()
			testHandler.GetIssueUsage(w, req)

			if w.Code != http.StatusOK {
				t.Fatalf("usage as %s: status %d; body: %s", tc.role, w.Code, w.Body.String())
			}
			body := w.Body.String()
			if !strings.Contains(body, "total_input_tokens") {
				t.Fatalf("usage as %s: token counts missing from payload: %s", tc.role, body)
			}
			if got := strings.Contains(body, "cost_usd_ticks"); got != tc.wantUSD {
				t.Fatalf("usage as %s: cost_usd_ticks present=%v, want %v; body: %s",
					tc.role, got, tc.wantUSD, body)
			}
		})
	}
}
