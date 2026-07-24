package handler

import (
	"context"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"testing"
)

func TestProjectTypeLifecycleAndDeadlineRules(t *testing.T) {
	w := httptest.NewRecorder()
	testHandler.CreateProject(w, newRequest("POST", "/api/projects?workspace_id="+testWorkspaceID, map[string]any{
		"title":        "typed development project",
		"project_type": "development",
		"due_date":     "2026-12-15",
	}))
	created := decodeProject(t, w, http.StatusCreated)
	t.Cleanup(func() {
		_, _ = testPool.Exec(context.Background(), `DELETE FROM project WHERE id = $1`, created.ID)
	})
	if created.ProjectType == nil || *created.ProjectType != "development" {
		t.Fatalf("create project_type = %v, want development", created.ProjectType)
	}
	if created.DueDate == nil || *created.DueDate != "2026-12-15" {
		t.Fatalf("create due_date = %v, want 2026-12-15", created.DueDate)
	}

	// Switching to an indefinite service clears the contractual deadline in
	// the same update, even when the caller does not send due_date.
	w = httptest.NewRecorder()
	putReq := withURLParam(newRequest("PUT", "/api/projects/"+created.ID, map[string]any{
		"project_type": "support",
	}), "id", created.ID)
	testHandler.UpdateProject(w, putReq)
	updated := decodeProject(t, w, http.StatusOK)
	if updated.ProjectType == nil || *updated.ProjectType != "support" {
		t.Fatalf("update project_type = %v, want support", updated.ProjectType)
	}
	if updated.DueDate != nil {
		t.Fatalf("support due_date must be cleared, got %v", updated.DueDate)
	}

	// GET and search both carry the new field.
	w = httptest.NewRecorder()
	getReq := withURLParam(newRequest("GET", "/api/projects/"+created.ID, nil), "id", created.ID)
	testHandler.GetProject(w, getReq)
	got := decodeProject(t, w, http.StatusOK)
	if got.ProjectType == nil || *got.ProjectType != "support" {
		t.Fatalf("get project_type = %v, want support", got.ProjectType)
	}

	w = httptest.NewRecorder()
	testHandler.SearchProjects(w, newRequest("GET", "/api/projects/search?q=typed+development", nil))
	if w.Code != http.StatusOK {
		t.Fatalf("search status = %d: %s", w.Code, w.Body.String())
	}
	var searchResp struct {
		Projects []SearchProjectResponse `json:"projects"`
	}
	if err := json.NewDecoder(w.Body).Decode(&searchResp); err != nil {
		t.Fatalf("decode search: %v", err)
	}
	found := false
	for _, project := range searchResp.Projects {
		if project.ID == created.ID {
			found = true
			if project.ProjectType == nil || *project.ProjectType != "support" {
				t.Fatalf("search project_type = %v, want support", project.ProjectType)
			}
		}
	}
	if !found {
		t.Fatalf("typed project not found in search response: %s", w.Body.String())
	}
}

func TestProjectTypeValidation(t *testing.T) {
	for name, payload := range map[string]map[string]any{
		"unknown type": {
			"title":        "unknown type",
			"project_type": "maintenance",
		},
		"indefinite type with deadline": {
			"title":        "dated support",
			"project_type": "support",
			"due_date":     "2026-12-15",
		},
	} {
		t.Run(name, func(t *testing.T) {
			w := httptest.NewRecorder()
			testHandler.CreateProject(w, newRequest("POST", "/api/projects?workspace_id="+testWorkspaceID, payload))
			if w.Code != http.StatusBadRequest {
				t.Fatalf("expected 400, got %d: %s", w.Code, w.Body.String())
			}
		})
	}
}

func TestProjectClientProjection(t *testing.T) {
	ctx := context.Background()

	w := httptest.NewRecorder()
	testHandler.CreateProject(w, newRequest("POST", "/api/projects?workspace_id="+testWorkspaceID, map[string]any{
		"title":        "project with business client",
		"project_type": "seo",
	}))
	created := decodeProject(t, w, http.StatusCreated)

	var businessID string
	if err := testPool.QueryRow(ctx, `
		INSERT INTO business_account (name, owner_user_id)
		VALUES ('Project client projection', $1)
		RETURNING id
	`, testUserID).Scan(&businessID); err != nil {
		t.Fatalf("create business: %v", err)
	}

	var clientID string
	if err := testPool.QueryRow(ctx, `
		INSERT INTO business_client (business_id, canonical_name, status)
		VALUES ($1, 'Acme Clinic', 'active')
		RETURNING id
	`, businessID).Scan(&clientID); err != nil {
		t.Fatalf("create business client: %v", err)
	}

	if _, err := testPool.Exec(ctx, `
		INSERT INTO business_client_project (
			business_id, client_id, workspace_id, project_id, service_type
		)
		VALUES ($1, $2, $3, $4, 'seo')
	`, businessID, clientID, testWorkspaceID, created.ID); err != nil {
		t.Fatalf("link project client: %v", err)
	}

	t.Cleanup(func() {
		_, _ = testPool.Exec(context.Background(), `DELETE FROM business_client_project WHERE project_id = $1`, created.ID)
		_, _ = testPool.Exec(context.Background(), `DELETE FROM business_client WHERE id = $1`, clientID)
		_, _ = testPool.Exec(context.Background(), `DELETE FROM business_account WHERE id = $1`, businessID)
		_, _ = testPool.Exec(context.Background(), `DELETE FROM project WHERE id = $1`, created.ID)
	})

	w = httptest.NewRecorder()
	testHandler.ListProjects(w, newRequest("GET", "/api/projects?workspace_id="+testWorkspaceID, nil))
	if w.Code != http.StatusOK {
		t.Fatalf("list status = %d: %s", w.Code, w.Body.String())
	}
	var listResp struct {
		Projects []ProjectResponse `json:"projects"`
	}
	if err := json.NewDecoder(w.Body).Decode(&listResp); err != nil {
		t.Fatalf("decode project list: %v", err)
	}
	var listed *ProjectResponse
	for i := range listResp.Projects {
		if listResp.Projects[i].ID == created.ID {
			listed = &listResp.Projects[i]
			break
		}
	}
	if listed == nil {
		t.Fatalf("project missing from list response")
	}
	if listed.ClientID == nil || *listed.ClientID != clientID {
		t.Fatalf("list client_id = %v, want %s", listed.ClientID, clientID)
	}
	if listed.ClientName == nil || *listed.ClientName != "Acme Clinic" {
		t.Fatalf("list client_name = %v, want Acme Clinic", listed.ClientName)
	}

	w = httptest.NewRecorder()
	getReq := withURLParam(newRequest("GET", "/api/projects/"+created.ID, nil), "id", created.ID)
	testHandler.GetProject(w, getReq)
	got := decodeProject(t, w, http.StatusOK)
	if got.ClientID == nil || *got.ClientID != clientID {
		t.Fatalf("get client_id = %v, want %s", got.ClientID, clientID)
	}
	if got.ClientName == nil || *got.ClientName != "Acme Clinic" {
		t.Fatalf("get client_name = %v, want Acme Clinic", got.ClientName)
	}
}
