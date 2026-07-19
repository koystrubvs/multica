package main

import (
	"context"
	"encoding/json"
	"io"
	"net/http"
	"net/http/httptest"
	"testing"

	"github.com/multica-ai/multica/server/internal/analytics"
	"github.com/multica-ai/multica/server/internal/events"
	"github.com/multica-ai/multica/server/internal/featureflags"
	"github.com/multica-ai/multica/server/internal/realtime"
	"github.com/multica-ai/multica/server/pkg/featureflag"
)

func TestBusinessControlPlaneW1(t *testing.T) {
	ctx := context.Background()

	// The release toggle defaults off even when the schema and code are present.
	resp := authRequest(t, http.MethodGet, "/api/businesses", nil)
	defer resp.Body.Close()
	if resp.StatusCode != http.StatusNotFound {
		t.Fatalf("flag-off status = %d, want %d", resp.StatusCode, http.StatusNotFound)
	}

	var businessID string
	if err := testPool.QueryRow(ctx, `
		INSERT INTO business_account (name, owner_user_id)
		VALUES ('Integration Test Business', $1)
		RETURNING id
	`, testUserID).Scan(&businessID); err != nil {
		t.Fatalf("insert business account: %v", err)
	}

	if _, err := testPool.Exec(ctx, `
		INSERT INTO business_account_member (business_id, user_id, role)
		VALUES ($1, $2, 'owner')
	`, businessID, testUserID); err != nil {
		t.Fatalf("insert business owner: %v", err)
	}
	if _, err := testPool.Exec(ctx, `
		INSERT INTO business_workspace (
			business_id, workspace_id, kind,
			include_in_portfolio, include_revenue, include_costs
		)
		VALUES ($1, $2, 'operational', true, true, true)
	`, businessID, testWorkspaceID); err != nil {
		t.Fatalf("insert business workspace: %v", err)
	}

	var viewerBusinessID string
	if err := testPool.QueryRow(ctx, `
		INSERT INTO business_account (name, owner_user_id)
		VALUES ('Viewer-only Integration Business', $1)
		RETURNING id
	`, testUserID).Scan(&viewerBusinessID); err != nil {
		t.Fatalf("insert viewer business: %v", err)
	}
	if _, err := testPool.Exec(ctx, `
		INSERT INTO business_account_member (business_id, user_id, role)
		VALUES ($1, $2, 'viewer')
	`, viewerBusinessID, testUserID); err != nil {
		t.Fatalf("insert viewer membership: %v", err)
	}

	foreignEmail := "business-w1-foreign@multica.ai"
	var foreignUserID, foreignBusinessID string
	if err := testPool.QueryRow(ctx, `
		INSERT INTO "user" (name, email)
		VALUES ('Business W1 Foreign Owner', $1)
		RETURNING id
	`, foreignEmail).Scan(&foreignUserID); err != nil {
		t.Fatalf("insert foreign user: %v", err)
	}
	if err := testPool.QueryRow(ctx, `
		INSERT INTO business_account (name, owner_user_id)
		VALUES ('Foreign Integration Business', $1)
		RETURNING id
	`, foreignUserID).Scan(&foreignBusinessID); err != nil {
		t.Fatalf("insert foreign business: %v", err)
	}
	if _, err := testPool.Exec(ctx, `
		INSERT INTO business_account_member (business_id, user_id, role)
		VALUES ($1, $2, 'owner')
	`, foreignBusinessID, foreignUserID); err != nil {
		t.Fatalf("insert foreign owner: %v", err)
	}

	t.Cleanup(func() {
		_, _ = testPool.Exec(context.Background(), `DELETE FROM business_workspace WHERE business_id = $1`, businessID)
		_, _ = testPool.Exec(context.Background(), `DELETE FROM business_account_member WHERE business_id IN ($1, $2, $3)`, businessID, viewerBusinessID, foreignBusinessID)
		_, _ = testPool.Exec(context.Background(), `DELETE FROM business_account WHERE id IN ($1, $2, $3)`, businessID, viewerBusinessID, foreignBusinessID)
		_, _ = testPool.Exec(context.Background(), `DELETE FROM "user" WHERE id = $1`, foreignUserID)
	})

	provider := featureflag.NewStaticProvider()
	provider.Set(featureflags.BusinessControlPlane, featureflag.Rule{Default: true})
	flags := featureflag.NewService(provider)
	hub := realtime.NewHub()
	go hub.Run()
	bus := events.New()
	registerListeners(bus, hub)
	router, _ := NewRouterWithOptions(testPool, hub, bus, analytics.NoopClient{}, nil, RouterOptions{FeatureFlags: flags})
	enabledServer := httptest.NewServer(router)
	defer enabledServer.Close()

	resp = requestBusinessAPI(t, enabledServer.URL, "", "/api/businesses")
	resp.Body.Close()
	if resp.StatusCode != http.StatusUnauthorized {
		t.Fatalf("unauthenticated status = %d, want %d", resp.StatusCode, http.StatusUnauthorized)
	}

	resp = requestBusinessAPI(t, enabledServer.URL, testToken, "/api/businesses")
	var accounts []struct {
		ID                          string `json:"id"`
		Role                        string `json:"role"`
		MonthlyOwnerIncomeTargetRUB string `json:"monthly_owner_income_target_rub"`
	}
	decodeBusinessResponse(t, resp, http.StatusOK, &accounts)
	if len(accounts) != 1 || accounts[0].ID != businessID || accounts[0].Role != "owner" {
		t.Fatalf("owner account list = %#v, want only %s", accounts, businessID)
	}
	if accounts[0].MonthlyOwnerIncomeTargetRUB != "1000000.00" {
		t.Fatalf("monthly target = %q, want 1000000.00", accounts[0].MonthlyOwnerIncomeTargetRUB)
	}

	resp = requestBusinessAPI(t, enabledServer.URL, testToken, "/api/businesses/"+businessID)
	var account struct {
		ID       string `json:"id"`
		Currency string `json:"currency"`
		Timezone string `json:"timezone"`
	}
	decodeBusinessResponse(t, resp, http.StatusOK, &account)
	if account.ID != businessID || account.Currency != "RUB" || account.Timezone != "Asia/Yekaterinburg" {
		t.Fatalf("business account = %#v", account)
	}

	resp = requestBusinessAPI(t, enabledServer.URL, testToken, "/api/businesses/"+businessID+"/workspaces")
	var workspaces []struct {
		WorkspaceID        string `json:"workspace_id"`
		Kind               string `json:"kind"`
		IncludeInPortfolio bool   `json:"include_in_portfolio"`
		IncludeRevenue     bool   `json:"include_revenue"`
		IncludeCosts       bool   `json:"include_costs"`
	}
	decodeBusinessResponse(t, resp, http.StatusOK, &workspaces)
	if len(workspaces) != 1 || workspaces[0].WorkspaceID != testWorkspaceID || workspaces[0].Kind != "operational" {
		t.Fatalf("business workspace list = %#v", workspaces)
	}
	if !workspaces[0].IncludeInPortfolio || !workspaces[0].IncludeRevenue || !workspaces[0].IncludeCosts {
		t.Fatalf("operational workspace flags = %#v", workspaces[0])
	}

	resp = requestBusinessAPI(t, enabledServer.URL, testToken, "/api/businesses/not-a-uuid")
	resp.Body.Close()
	if resp.StatusCode != http.StatusBadRequest {
		t.Fatalf("malformed business id status = %d, want %d", resp.StatusCode, http.StatusBadRequest)
	}

	resp = requestBusinessAPI(t, enabledServer.URL, testToken, "/api/businesses/"+viewerBusinessID)
	resp.Body.Close()
	if resp.StatusCode != http.StatusForbidden {
		t.Fatalf("viewer status = %d, want %d", resp.StatusCode, http.StatusForbidden)
	}

	resp = requestBusinessAPI(t, enabledServer.URL, testToken, "/api/businesses/"+foreignBusinessID)
	resp.Body.Close()
	if resp.StatusCode != http.StatusNotFound {
		t.Fatalf("foreign business status = %d, want %d", resp.StatusCode, http.StatusNotFound)
	}

	// Enabling the business surface does not alter native workspace routes.
	resp = requestBusinessAPI(t, enabledServer.URL, testToken, "/api/workspaces")
	resp.Body.Close()
	if resp.StatusCode != http.StatusOK {
		t.Fatalf("existing workspace route status = %d, want %d", resp.StatusCode, http.StatusOK)
	}
}

func requestBusinessAPI(t *testing.T, baseURL, token, path string) *http.Response {
	t.Helper()
	req, err := http.NewRequest(http.MethodGet, baseURL+path, nil)
	if err != nil {
		t.Fatalf("create request: %v", err)
	}
	if token != "" {
		req.Header.Set("Authorization", "Bearer "+token)
	}
	resp, err := http.DefaultClient.Do(req)
	if err != nil {
		t.Fatalf("request %s: %v", path, err)
	}
	return resp
}

func decodeBusinessResponse(t *testing.T, resp *http.Response, wantStatus int, dst any) {
	t.Helper()
	defer resp.Body.Close()
	if resp.StatusCode != wantStatus {
		body, _ := io.ReadAll(resp.Body)
		t.Fatalf("status = %d, want %d: %s", resp.StatusCode, wantStatus, body)
	}
	if err := json.NewDecoder(resp.Body).Decode(dst); err != nil {
		t.Fatalf("decode response: %v", err)
	}
}
