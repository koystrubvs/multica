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
