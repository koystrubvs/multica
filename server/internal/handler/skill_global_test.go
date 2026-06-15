package handler

import (
	"context"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"testing"
)

// insertGlobalTestSkill writes a global skill row (workspace_id IS NULL) owned
// by ownerID directly via SQL and registers a cleanup hook.
func insertGlobalTestSkill(t *testing.T, name, ownerID string) string {
	t.Helper()
	var id string
	if err := testPool.QueryRow(context.Background(), `
		INSERT INTO skill (workspace_id, name, description, content, config, created_by)
		VALUES (NULL, $1, $2, $3, '{}'::jsonb, $4)
		RETURNING id
	`, name, "global fixture", "# global", ownerID).Scan(&id); err != nil {
		t.Fatalf("insert global skill: %v", err)
	}
	t.Cleanup(func() {
		testPool.Exec(context.Background(), `DELETE FROM skill WHERE id = $1`, id)
	})
	return id
}

// TestCreateSkill_GlobalStoresNullWorkspace verifies the `global` flag creates
// a workspace-unscoped skill (workspace_id IS NULL) owned by the caller, and
// that the response advertises is_global.
func TestCreateSkill_GlobalStoresNullWorkspace(t *testing.T) {
	w := httptest.NewRecorder()
	req := newRequest("POST", "/api/skills", map[string]any{
		"name":   "global-create-" + t.Name(),
		"global": true,
	})
	testHandler.CreateSkill(w, req)
	if w.Code != http.StatusCreated {
		t.Fatalf("CreateSkill global: expected 201, got %d: %s", w.Code, w.Body.String())
	}

	var resp map[string]any
	if err := json.Unmarshal(w.Body.Bytes(), &resp); err != nil {
		t.Fatalf("decode body: %v", err)
	}
	id, _ := resp["id"].(string)
	if id == "" {
		t.Fatalf("CreateSkill global: missing id in response: %v", resp)
	}
	t.Cleanup(func() {
		testPool.Exec(context.Background(), `DELETE FROM skill WHERE id = $1`, id)
	})

	if resp["is_global"] != true {
		t.Fatalf("CreateSkill global: expected is_global=true, got %v", resp["is_global"])
	}
	if ws, _ := resp["workspace_id"].(string); ws != "" {
		t.Fatalf("CreateSkill global: expected empty workspace_id, got %q", ws)
	}

	// The row must carry a NULL workspace_id and the caller as owner.
	var wsNull bool
	var createdBy string
	if err := testPool.QueryRow(context.Background(),
		`SELECT workspace_id IS NULL, created_by FROM skill WHERE id = $1`, id,
	).Scan(&wsNull, &createdBy); err != nil {
		t.Fatalf("read back skill: %v", err)
	}
	if !wsNull {
		t.Fatalf("CreateSkill global: expected NULL workspace_id in DB")
	}
	if createdBy != testUserID {
		t.Fatalf("CreateSkill global: expected created_by=%s, got %s", testUserID, createdBy)
	}
}

// TestListSkills_IncludesGlobalSkillForMember: a global skill owned by a member
// of the workspace surfaces in that workspace's list — and in every other
// workspace the owner belongs to.
func TestListSkills_IncludesGlobalSkillForMember(t *testing.T) {
	globalID := insertGlobalTestSkill(t, "global-visible-"+t.Name(), testUserID)
	otherWsID := createOtherTestWorkspace(t) // testUserID is owner here too

	for _, wsID := range []string{testWorkspaceID, otherWsID} {
		w := httptest.NewRecorder()
		req := newRequest("GET", "/api/skills?workspace_id="+wsID, nil)
		req.Header.Set("X-Workspace-ID", wsID)
		testHandler.ListSkills(w, req)
		if w.Code != http.StatusOK {
			t.Fatalf("ListSkills(%s): expected 200, got %d: %s", wsID, w.Code, w.Body.String())
		}
		if !listContainsSkill(t, w.Body.Bytes(), globalID) {
			t.Fatalf("ListSkills(%s): expected global skill %s to be visible", wsID, globalID)
		}
	}
}

// TestListSkills_ExcludesGlobalSkillOfNonMember: a global skill owned by a user
// who is NOT a member of the workspace must not leak into it (tenant boundary).
func TestListSkills_ExcludesGlobalSkillOfNonMember(t *testing.T) {
	strangerID := createEphemeralUser(t, "global-stranger")
	globalID := insertGlobalTestSkill(t, "global-hidden-"+t.Name(), strangerID)

	w := httptest.NewRecorder()
	req := newRequest("GET", "/api/skills?workspace_id="+testWorkspaceID, nil)
	testHandler.ListSkills(w, req)
	if w.Code != http.StatusOK {
		t.Fatalf("ListSkills: expected 200, got %d: %s", w.Code, w.Body.String())
	}
	if listContainsSkill(t, w.Body.Bytes(), globalID) {
		t.Fatalf("ListSkills: global skill %s of a non-member must not be visible", globalID)
	}
}

// TestUpdateGlobalSkill_NonOwnerForbidden: a workspace owner/admin who is NOT
// the global skill's owner cannot manage it, even though it is visible to them.
func TestUpdateGlobalSkill_NonOwnerForbidden(t *testing.T) {
	// Owner is a member of the workspace (so the skill is visible there), but is
	// not testUserID, who issues the request as workspace owner.
	ownerID, _ := createEphemeralMember(t, testWorkspaceID, "global-owner", "member")
	globalID := insertGlobalTestSkill(t, "global-locked-"+t.Name(), ownerID)

	w := httptest.NewRecorder()
	req := newRequest("PUT", "/api/skills/"+globalID, map[string]any{"description": "hijacked"})
	req = withURLParam(req, "id", globalID)
	testHandler.UpdateSkill(w, req)
	if w.Code != http.StatusForbidden {
		t.Fatalf("UpdateSkill non-owner global: expected 403, got %d: %s", w.Code, w.Body.String())
	}
}

// TestDeleteGlobalSkill_OwnerSucceeds: the owner can delete their global skill.
func TestDeleteGlobalSkill_OwnerSucceeds(t *testing.T) {
	globalID := insertGlobalTestSkill(t, "global-del-"+t.Name(), testUserID)

	w := httptest.NewRecorder()
	req := newRequest("DELETE", "/api/skills/"+globalID, nil)
	req = withURLParam(req, "id", globalID)
	testHandler.DeleteSkill(w, req)
	if w.Code != http.StatusNoContent {
		t.Fatalf("DeleteSkill owner global: expected 204, got %d: %s", w.Code, w.Body.String())
	}

	var count int
	if err := testPool.QueryRow(context.Background(),
		`SELECT count(*) FROM skill WHERE id = $1`, globalID,
	).Scan(&count); err != nil {
		t.Fatalf("count skill: %v", err)
	}
	if count != 0 {
		t.Fatalf("DeleteSkill owner global: row still present")
	}
}

// TestSetAgentSkills_AttachesVisibleGlobalSkill: a global skill visible in the
// agent's workspace can be attached to that agent.
func TestSetAgentSkills_AttachesVisibleGlobalSkill(t *testing.T) {
	agentID := createHandlerTestAgent(t, "Global Skill Attach "+t.Name(), nil)
	globalID := insertGlobalTestSkill(t, "global-attach-"+t.Name(), testUserID)

	w := httptest.NewRecorder()
	req := newRequest("PUT", "/api/agents/"+agentID+"/skills", map[string]any{
		"skill_ids": []string{globalID},
	})
	req = withURLParam(req, "id", agentID)
	testHandler.SetAgentSkills(w, req)
	if w.Code != http.StatusOK {
		t.Fatalf("SetAgentSkills global: expected 200, got %d: %s", w.Code, w.Body.String())
	}

	var count int
	if err := testPool.QueryRow(context.Background(),
		`SELECT count(*) FROM agent_skill WHERE agent_id = $1 AND skill_id = $2`, agentID, globalID,
	).Scan(&count); err != nil {
		t.Fatalf("count agent_skill: %v", err)
	}
	if count != 1 {
		t.Fatalf("SetAgentSkills global: expected attachment row, got %d", count)
	}
}

// TestSetAgentSkills_RejectsNameCollision: attaching a workspace skill and a
// same-named global skill to one agent is rejected, since the daemon writes
// both to the same on-disk path.
func TestSetAgentSkills_RejectsNameCollision(t *testing.T) {
	agentID := createHandlerTestAgent(t, "Collision Attach "+t.Name(), nil)
	name := "collide-" + t.Name()

	var wsSkillID string
	if err := testPool.QueryRow(context.Background(), `
		INSERT INTO skill (workspace_id, name, description, content, config, created_by)
		VALUES ($1, $2, '', '', '{}'::jsonb, $3) RETURNING id
	`, testWorkspaceID, name, testUserID).Scan(&wsSkillID); err != nil {
		t.Fatalf("insert workspace skill: %v", err)
	}
	t.Cleanup(func() {
		testPool.Exec(context.Background(), `DELETE FROM skill WHERE id = $1`, wsSkillID)
	})
	globalID := insertGlobalTestSkill(t, name, testUserID) // identical name, global scope

	w := httptest.NewRecorder()
	req := newRequest("PUT", "/api/agents/"+agentID+"/skills", map[string]any{
		"skill_ids": []string{wsSkillID, globalID},
	})
	req = withURLParam(req, "id", agentID)
	testHandler.SetAgentSkills(w, req)
	if w.Code != http.StatusConflict {
		t.Fatalf("SetAgentSkills name collision: expected 409, got %d: %s", w.Code, w.Body.String())
	}
}

// listContainsSkill reports whether a /api/skills list response contains the
// skill id.
func listContainsSkill(t *testing.T, body []byte, id string) bool {
	t.Helper()
	var rows []map[string]any
	if err := json.Unmarshal(body, &rows); err != nil {
		t.Fatalf("decode skill list: %v", err)
	}
	for _, row := range rows {
		if row["id"] == id {
			return true
		}
	}
	return false
}
