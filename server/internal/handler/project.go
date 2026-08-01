package handler

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"log/slog"
	"net/http"
	"strconv"
	"strings"

	"github.com/go-chi/chi/v5"
	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgtype"
	"github.com/multica-ai/multica/server/internal/logger"
	"github.com/multica-ai/multica/server/internal/util"
	db "github.com/multica-ai/multica/server/pkg/db/generated"
	"github.com/multica-ai/multica/server/pkg/protocol"
)

type ProjectResponse struct {
	ID          string  `json:"id"`
	WorkspaceID string  `json:"workspace_id"`
	Title       string  `json:"title"`
	Description *string `json:"description"`
	Icon        *string `json:"icon"`
	Status      string  `json:"status"`
	Priority    string  `json:"priority"`
	ProjectType *string `json:"project_type"`
	ClientID    *string `json:"client_id"`
	ClientName  *string `json:"client_name"`
	LeadType    *string `json:"lead_type"`
	LeadID      *string `json:"lead_id"`
	// StartDate / DueDate are calendar days ("YYYY-MM-DD"), no time-of-day or
	// timezone — same contract as issue.start_date / issue.due_date.
	StartDate  *string `json:"start_date"`
	DueDate    *string `json:"due_date"`
	CreatedAt  string  `json:"created_at"`
	UpdatedAt  string  `json:"updated_at"`
	IssueCount int64   `json:"issue_count"`
	DoneCount  int64   `json:"done_count"`
	// ResourceCount is a breadcrumb pointing at the sub-collection at
	// /api/projects/{id}/resources. Resources themselves stay out of this
	// payload to keep parent metadata and child collections separate; clients
	// that need the list call ListProjectResources directly.
	ResourceCount int64 `json:"resource_count"`
}

func projectToResponse(p db.Project) ProjectResponse {
	return ProjectResponse{
		ID:          uuidToString(p.ID),
		WorkspaceID: uuidToString(p.WorkspaceID),
		Title:       p.Title,
		Description: textToPtr(p.Description),
		Icon:        textToPtr(p.Icon),
		Status:      p.Status,
		Priority:    p.Priority,
		ProjectType: textToPtr(p.ProjectType),
		LeadType:    textToPtr(p.LeadType),
		LeadID:      uuidToPtr(p.LeadID),
		StartDate:   dateToPtr(p.StartDate),
		DueDate:     dateToPtr(p.DueDate),
		CreatedAt:   timestampToString(p.CreatedAt),
		UpdatedAt:   timestampToString(p.UpdatedAt),
	}
}

func (h *Handler) loadProjectIssueStats(ctx context.Context, projectID pgtype.UUID) (int64, int64) {
	stats, err := h.Queries.GetProjectIssueStats(ctx, []pgtype.UUID{projectID})
	if err != nil || len(stats) == 0 {
		return 0, 0
	}
	return stats[0].TotalCount, stats[0].DoneCount
}

func (h *Handler) loadProjectResourceCount(ctx context.Context, projectID pgtype.UUID) int64 {
	rows, err := h.Queries.GetProjectResourceCounts(ctx, []pgtype.UUID{projectID})
	if err != nil || len(rows) == 0 {
		return 0
	}
	return rows[0].ResourceCount
}

type projectClientRef struct {
	ClientID   string
	ClientName string
}

// loadProjectClientRefs projects the existing Business client-to-project
// relation onto the common project API. Business remains the source of truth:
// this is intentionally read-only and does not duplicate client ownership on
// the project table.
func (h *Handler) loadProjectClientRefs(
	ctx context.Context,
	workspaceID pgtype.UUID,
	projectIDs []pgtype.UUID,
) map[string]projectClientRef {
	refs := make(map[string]projectClientRef)
	if h.DB == nil || len(projectIDs) == 0 {
		return refs
	}

	rows, err := h.DB.Query(ctx, `
		SELECT bcp.project_id, bc.id, bc.canonical_name
		FROM business_client_project AS bcp
		JOIN business_client AS bc
		  ON bc.id = bcp.client_id
		 AND bc.business_id = bcp.business_id
		WHERE bcp.workspace_id = $1
		  AND bcp.project_id = ANY($2::uuid[])
	`, workspaceID, projectIDs)
	if err != nil {
		slog.Warn("failed to load project clients", "workspace_id", uuidToString(workspaceID), "error", err)
		return refs
	}
	defer rows.Close()

	for rows.Next() {
		var projectID pgtype.UUID
		var clientID pgtype.UUID
		var clientName string
		if err := rows.Scan(&projectID, &clientID, &clientName); err != nil {
			slog.Warn("failed to scan project client", "workspace_id", uuidToString(workspaceID), "error", err)
			return refs
		}
		refs[uuidToString(projectID)] = projectClientRef{
			ClientID:   uuidToString(clientID),
			ClientName: clientName,
		}
	}
	if err := rows.Err(); err != nil {
		slog.Warn("failed while reading project clients", "workspace_id", uuidToString(workspaceID), "error", err)
	}
	return refs
}

func applyProjectClient(resp *ProjectResponse, refs map[string]projectClientRef) {
	ref, ok := refs[resp.ID]
	if !ok {
		return
	}
	resp.ClientID = &ref.ClientID
	resp.ClientName = &ref.ClientName
}

type CreateProjectRequest struct {
	Title       string                                `json:"title"`
	Description *string                               `json:"description"`
	Icon        *string                               `json:"icon"`
	Status      string                                `json:"status"`
	Priority    string                                `json:"priority"`
	ProjectType *string                               `json:"project_type"`
	LeadType    *string                               `json:"lead_type"`
	LeadID      *string                               `json:"lead_id"`
	StartDate   *string                               `json:"start_date"`
	DueDate     *string                               `json:"due_date"`
	Resources   []CreateProjectResourceRequestPayload `json:"resources,omitempty"`
}

// CreateProjectResourceRequestPayload mirrors CreateProjectResourceRequest but
// is embedded inside the project create payload. Kept as a separate type so a
// future change to the standalone request can't silently break this surface.
type CreateProjectResourceRequestPayload struct {
	ResourceType string          `json:"resource_type"`
	ResourceRef  json.RawMessage `json:"resource_ref"`
	Label        *string         `json:"label"`
	Position     *int32          `json:"position"`
}

type UpdateProjectRequest struct {
	Title       *string `json:"title"`
	Description *string `json:"description"`
	Icon        *string `json:"icon"`
	Status      *string `json:"status"`
	Priority    *string `json:"priority"`
	ProjectType *string `json:"project_type"`
	LeadType    *string `json:"lead_type"`
	LeadID      *string `json:"lead_id"`
	StartDate   *string `json:"start_date"`
	DueDate     *string `json:"due_date"`
}

func (h *Handler) ListProjects(w http.ResponseWriter, r *http.Request) {
	workspaceID := h.resolveWorkspaceID(r)
	wsUUID, ok := parseUUIDOrBadRequest(w, workspaceID, "workspace_id")
	if !ok {
		return
	}
	var statusFilter pgtype.Text
	if s := r.URL.Query().Get("status"); s != "" {
		statusFilter = pgtype.Text{String: s, Valid: true}
	}
	var priorityFilter pgtype.Text
	if p := r.URL.Query().Get("priority"); p != "" {
		priorityFilter = pgtype.Text{String: p, Valid: true}
	}
	scope, scopeOK := h.projectScope(r)
	if !scopeOK {
		writeError(w, http.StatusForbidden, "insufficient permissions")
		return
	}
	projects, err := h.Queries.ListProjects(r.Context(), db.ListProjectsParams{
		WorkspaceID:     wsUUID,
		ScopeAll:        !scope.Restricted(),
		ScopeProjectIds: scope.AllowedProjectIDs,
		Status:          statusFilter,
		Priority:        priorityFilter,
	})
	if err != nil {
		writeError(w, http.StatusInternalServerError, "failed to list projects")
		return
	}

	// Batch-fetch issue stats and resource counts for all projects
	statsMap := make(map[string]db.GetProjectIssueStatsRow)
	resourceCountMap := make(map[string]int64)
	clientRefs := make(map[string]projectClientRef)
	if len(projects) > 0 {
		projectIDs := make([]pgtype.UUID, len(projects))
		for i, p := range projects {
			projectIDs[i] = p.ID
		}
		clientRefs = h.loadProjectClientRefs(r.Context(), wsUUID, projectIDs)
		stats, err := h.Queries.GetProjectIssueStats(r.Context(), projectIDs)
		if err == nil {
			for _, s := range stats {
				statsMap[uuidToString(s.ProjectID)] = s
			}
		}
		counts, err := h.Queries.GetProjectResourceCounts(r.Context(), projectIDs)
		if err == nil {
			for _, c := range counts {
				resourceCountMap[uuidToString(c.ProjectID)] = c.ResourceCount
			}
		}
	}

	resp := make([]ProjectResponse, len(projects))
	for i, p := range projects {
		resp[i] = projectToResponse(p)
		applyProjectClient(&resp[i], clientRefs)
		if s, ok := statsMap[resp[i].ID]; ok {
			resp[i].IssueCount = s.TotalCount
			resp[i].DoneCount = s.DoneCount
		}
		resp[i].ResourceCount = resourceCountMap[resp[i].ID]
	}
	writeJSON(w, http.StatusOK, map[string]any{"projects": resp, "total": len(resp)})
}

func (h *Handler) GetProject(w http.ResponseWriter, r *http.Request) {
	id := chi.URLParam(r, "id")
	workspaceID := h.resolveWorkspaceID(r)
	idUUID, ok := parseUUIDOrBadRequest(w, id, "project id")
	if !ok {
		return
	}
	wsUUID, ok := parseUUIDOrBadRequest(w, workspaceID, "workspace id")
	if !ok {
		return
	}
	project, err := h.Queries.GetProjectInWorkspace(r.Context(), db.GetProjectInWorkspaceParams{
		ID: idUUID, WorkspaceID: wsUUID,
	})
	if err != nil {
		writeError(w, http.StatusNotFound, "project not found")
		return
	}
	resp := projectToResponse(project)
	applyProjectClient(&resp, h.loadProjectClientRefs(r.Context(), wsUUID, []pgtype.UUID{project.ID}))
	resp.IssueCount, resp.DoneCount = h.loadProjectIssueStats(r.Context(), project.ID)
	resp.ResourceCount = h.loadProjectResourceCount(r.Context(), project.ID)
	writeJSON(w, http.StatusOK, resp)
}

// validProjectStatuses / validProjectPriorities mirror the CHECK constraints on
// the project table (migrations 034, 035). CreateProject / UpdateProject
// pre-validate against these so an unknown enum value returns a clean 400 with
// the allowed list instead of surfacing the DB CHECK violation as a 500 — the
// exact mismatch reported in #3925 (`--status active`).
var validProjectStatuses = []string{"planned", "in_progress", "paused", "completed", "cancelled"}
var validProjectPriorities = []string{"urgent", "high", "medium", "low", "none"}
var validProjectTypes = []string{"support", "seo", "development", "transit"}

func isIndefiniteProjectType(value string) bool {
	return value == "support" || value == "seo" || value == "transit"
}

// validateProjectEnum writes a 400 and returns false when value is not in
// allowed; the caller returns immediately on false.
func validateProjectEnum(w http.ResponseWriter, field, value string, allowed []string) bool {
	for _, a := range allowed {
		if value == a {
			return true
		}
	}
	writeError(w, http.StatusBadRequest, fmt.Sprintf("invalid %s %q; valid values: %s", field, value, strings.Join(allowed, ", ")))
	return false
}

// writeProjectWriteError maps a failed project INSERT/UPDATE to an HTTP
// response. A CHECK constraint violation is a client error (400) — pre-validation
// already covers status/priority, so this backstops any other constrained column
// (e.g. lead_type). Anything else is a genuine server fault: log the underlying
// error so transient DB failures are diagnosable (#3925 had no server-side
// signal) and return 500.
func (h *Handler) writeProjectWriteError(w http.ResponseWriter, r *http.Request, err error, action string) {
	if isCheckViolation(err) {
		writeError(w, http.StatusBadRequest, "project "+action+" rejected: a field value failed a database constraint")
		return
	}
	slog.Error("project "+action+" failed", append(logger.RequestAttrs(r), "error", err)...)
	writeError(w, http.StatusInternalServerError, "failed to "+action+" project")
}

func (h *Handler) CreateProject(w http.ResponseWriter, r *http.Request) {
	var req CreateProjectRequest
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
		writeError(w, http.StatusBadRequest, "invalid request body")
		return
	}
	if req.Title == "" {
		writeError(w, http.StatusBadRequest, "title is required")
		return
	}
	workspaceID := h.resolveWorkspaceID(r)
	userID, ok := requireUserID(w, r)
	if !ok {
		return
	}
	status := req.Status
	if status == "" {
		status = "planned"
	}
	if !validateProjectEnum(w, "status", status, validProjectStatuses) {
		return
	}
	priority := req.Priority
	if priority == "" {
		priority = "none"
	}
	if !validateProjectEnum(w, "priority", priority, validProjectPriorities) {
		return
	}
	var projectType pgtype.Text
	if req.ProjectType != nil {
		if !validateProjectEnum(w, "project_type", *req.ProjectType, validProjectTypes) {
			return
		}
		projectType = pgtype.Text{String: *req.ProjectType, Valid: true}
	}
	var leadType pgtype.Text
	var leadID pgtype.UUID
	if req.LeadType != nil {
		leadType = pgtype.Text{String: *req.LeadType, Valid: true}
	}
	if req.LeadID != nil {
		id, ok := parseUUIDOrBadRequest(w, *req.LeadID, "lead_id")
		if !ok {
			return
		}
		leadID = id
	}
	wsUUID, ok := parseUUIDOrBadRequest(w, workspaceID, "workspace_id")
	if !ok {
		return
	}

	// start_date / due_date are optional calendar days; an absent or empty
	// value leaves the column NULL. Mirrors CreateIssue's date handling.
	var startDate pgtype.Date
	if req.StartDate != nil && *req.StartDate != "" {
		d, err := util.ParseCalendarDate(*req.StartDate)
		if err != nil {
			writeError(w, http.StatusBadRequest, "invalid start_date format, expected YYYY-MM-DD")
			return
		}
		startDate = d
	}
	var dueDate pgtype.Date
	if req.DueDate != nil && *req.DueDate != "" {
		d, err := util.ParseCalendarDate(*req.DueDate)
		if err != nil {
			writeError(w, http.StatusBadRequest, "invalid due_date format, expected YYYY-MM-DD")
			return
		}
		dueDate = d
	}
	if projectType.Valid && isIndefiniteProjectType(projectType.String) && dueDate.Valid {
		writeError(w, http.StatusBadRequest, "due_date must be empty for an indefinite project_type")
		return
	}

	// Pre-validate every resource payload before opening a transaction so an
	// invalid ref produces a clean 400 with no DB work. For local_directory we
	// also enforce one row per daemon_id within the batch — the daemon-side
	// resolver picks the first match by daemon_id, so two rows on the same
	// daemon would silently route the agent into whichever sorts first.
	// The standalone POST/PUT paths run the same check via
	// findLocalDirectoryConflict; this loop just covers the bundled-create
	// surface, where there is no existing row to compare against yet.
	normalizedRefs := make([]json.RawMessage, len(req.Resources))
	localDirSeen := map[string]int{}
	for i, res := range req.Resources {
		res.ResourceType = strings.TrimSpace(res.ResourceType)
		if res.ResourceType == "" {
			writeError(w, http.StatusBadRequest, "resources[].resource_type is required")
			return
		}
		ref, err := validateAndNormalizeResourceRef(res.ResourceType, res.ResourceRef)
		if err != nil {
			writeError(w, http.StatusBadRequest, "resources["+strconv.Itoa(i)+"]: "+err.Error())
			return
		}
		normalizedRefs[i] = ref
		if res.ResourceType == "local_directory" {
			var ld localDirectoryRef
			if err := json.Unmarshal(ref, &ld); err != nil {
				writeError(w, http.StatusBadRequest, "resources["+strconv.Itoa(i)+"]: "+err.Error())
				return
			}
			if prev, ok := localDirSeen[ld.DaemonID]; ok {
				writeError(w, http.StatusBadRequest, "resources["+strconv.Itoa(i)+"]: duplicate local_directory for daemon (already at index "+strconv.Itoa(prev)+"); each daemon may attach at most one local_directory per project")
				return
			}
			localDirSeen[ld.DaemonID] = i
		}
	}

	createParams := db.CreateProjectParams{
		WorkspaceID: wsUUID,
		Title:       req.Title,
		Description: ptrToText(req.Description),
		Icon:        ptrToText(req.Icon),
		Status:      status,
		LeadType:    leadType,
		LeadID:      leadID,
		Priority:    priority,
		ProjectType: projectType,
		StartDate:   startDate,
		DueDate:     dueDate,
	}

	// Without resources, keep the simple non-tx path.
	if len(req.Resources) == 0 {
		project, err := h.Queries.CreateProject(r.Context(), createParams)
		if err != nil {
			h.writeProjectWriteError(w, r, err, "create")
			return
		}
		resp := projectToResponse(project)
		h.publish(protocol.EventProjectCreated, workspaceID, "member", userID, map[string]any{"project": resp})
		writeJSON(w, http.StatusCreated, resp)
		return
	}

	// Transactional path: project + all resources are atomic.
	tx, err := h.TxStarter.Begin(r.Context())
	if err != nil {
		writeError(w, http.StatusInternalServerError, "failed to start transaction")
		return
	}
	defer tx.Rollback(r.Context())
	qtx := h.Queries.WithTx(tx)

	project, err := qtx.CreateProject(r.Context(), createParams)
	if err != nil {
		h.writeProjectWriteError(w, r, err, "create")
		return
	}

	creator, _ := h.parseUserUUIDOrZero(userID)
	resourceRows := make([]db.ProjectResource, 0, len(req.Resources))
	for i, res := range req.Resources {
		var label pgtype.Text
		if res.Label != nil && strings.TrimSpace(*res.Label) != "" {
			label = pgtype.Text{String: strings.TrimSpace(*res.Label), Valid: true}
		}
		var position int32 = int32(i)
		if res.Position != nil {
			position = *res.Position
		}
		row, err := qtx.CreateProjectResource(r.Context(), db.CreateProjectResourceParams{
			ProjectID:    project.ID,
			WorkspaceID:  project.WorkspaceID,
			ResourceType: res.ResourceType,
			ResourceRef:  normalizedRefs[i],
			Label:        label,
			Position:     position,
			CreatedBy:    creator,
		})
		if err != nil {
			if isUniqueViolation(err) {
				writeError(w, http.StatusConflict, "resources["+strconv.Itoa(i)+"]: this resource is already attached")
				return
			}
			writeError(w, http.StatusInternalServerError, "failed to attach resource at index "+strconv.Itoa(i))
			return
		}
		resourceRows = append(resourceRows, row)
	}
	if err := tx.Commit(r.Context()); err != nil {
		writeError(w, http.StatusInternalServerError, "failed to commit project create")
		return
	}

	resourceResp := make([]ProjectResourceResponse, len(resourceRows))
	for i, row := range resourceRows {
		resourceResp[i] = projectResourceToResponse(row)
	}
	resp := projectToResponse(project)
	resp.ResourceCount = int64(len(resourceResp))
	h.publish(protocol.EventProjectCreated, workspaceID, "member", userID, map[string]any{"project": resp})
	for _, rr := range resourceResp {
		h.publish(protocol.EventProjectResourceCreated, workspaceID, "member", userID, map[string]any{
			"resource":   rr,
			"project_id": resp.ID,
		})
	}
	// One-shot create echo: the parent ProjectResponse fields plus the just-
	// created resources. This is a transient creation echo, not a contract for
	// reads — GET /projects/{id} stays metadata-only with resource_count.
	writeJSON(w, http.StatusCreated, struct {
		ProjectResponse
		Resources []ProjectResourceResponse `json:"resources"`
	}{
		ProjectResponse: resp,
		Resources:       resourceResp,
	})
}

func (h *Handler) UpdateProject(w http.ResponseWriter, r *http.Request) {
	id := chi.URLParam(r, "id")
	workspaceID := h.resolveWorkspaceID(r)
	idUUID, ok := parseUUIDOrBadRequest(w, id, "project id")
	if !ok {
		return
	}
	wsUUID, ok := parseUUIDOrBadRequest(w, workspaceID, "workspace id")
	if !ok {
		return
	}
	prevProject, err := h.Queries.GetProjectInWorkspace(r.Context(), db.GetProjectInWorkspaceParams{
		ID: idUUID, WorkspaceID: wsUUID,
	})
	if err != nil {
		writeError(w, http.StatusNotFound, "project not found")
		return
	}
	userID, ok := requireUserID(w, r)
	if !ok {
		return
	}
	bodyBytes, err := io.ReadAll(r.Body)
	if err != nil {
		writeError(w, http.StatusBadRequest, "failed to read request body")
		return
	}
	var req UpdateProjectRequest
	if err := json.Unmarshal(bodyBytes, &req); err != nil {
		writeError(w, http.StatusBadRequest, "invalid request body")
		return
	}
	var rawFields map[string]json.RawMessage
	json.Unmarshal(bodyBytes, &rawFields)

	params := db.UpdateProjectParams{
		ID:          prevProject.ID,
		Description: prevProject.Description,
		Icon:        prevProject.Icon,
		LeadType:    prevProject.LeadType,
		LeadID:      prevProject.LeadID,
		ProjectType: prevProject.ProjectType,
		StartDate:   prevProject.StartDate,
		DueDate:     prevProject.DueDate,
	}
	if req.Title != nil {
		params.Title = pgtype.Text{String: *req.Title, Valid: true}
	}
	if req.Status != nil {
		if !validateProjectEnum(w, "status", *req.Status, validProjectStatuses) {
			return
		}
		params.Status = pgtype.Text{String: *req.Status, Valid: true}
	}
	if req.Priority != nil {
		if !validateProjectEnum(w, "priority", *req.Priority, validProjectPriorities) {
			return
		}
		params.Priority = pgtype.Text{String: *req.Priority, Valid: true}
	}
	if _, ok := rawFields["project_type"]; ok {
		if req.ProjectType != nil {
			if !validateProjectEnum(w, "project_type", *req.ProjectType, validProjectTypes) {
				return
			}
			params.ProjectType = pgtype.Text{String: *req.ProjectType, Valid: true}
		} else {
			params.ProjectType = pgtype.Text{Valid: false}
		}
	}
	if _, ok := rawFields["description"]; ok {
		if req.Description != nil {
			params.Description = pgtype.Text{String: *req.Description, Valid: true}
		} else {
			params.Description = pgtype.Text{Valid: false}
		}
	}
	if _, ok := rawFields["icon"]; ok {
		if req.Icon != nil {
			params.Icon = pgtype.Text{String: *req.Icon, Valid: true}
		} else {
			params.Icon = pgtype.Text{Valid: false}
		}
	}
	if _, ok := rawFields["lead_type"]; ok {
		if req.LeadType != nil {
			params.LeadType = pgtype.Text{String: *req.LeadType, Valid: true}
		} else {
			params.LeadType = pgtype.Text{Valid: false}
		}
	}
	if _, ok := rawFields["lead_id"]; ok {
		if req.LeadID != nil {
			leadUUID, ok := parseUUIDOrBadRequest(w, *req.LeadID, "lead_id")
			if !ok {
				return
			}
			params.LeadID = leadUUID
		} else {
			params.LeadID = pgtype.UUID{Valid: false}
		}
	}
	// Dates follow the issue contract: a present key with an empty/null value
	// clears the date; an absent key leaves the prior value untouched.
	if _, ok := rawFields["start_date"]; ok {
		if req.StartDate != nil && *req.StartDate != "" {
			d, err := util.ParseCalendarDate(*req.StartDate)
			if err != nil {
				writeError(w, http.StatusBadRequest, "invalid start_date format, expected YYYY-MM-DD")
				return
			}
			params.StartDate = d
		} else {
			params.StartDate = pgtype.Date{Valid: false} // explicit null = clear date
		}
	}
	if _, ok := rawFields["due_date"]; ok {
		if req.DueDate != nil && *req.DueDate != "" {
			d, err := util.ParseCalendarDate(*req.DueDate)
			if err != nil {
				writeError(w, http.StatusBadRequest, "invalid due_date format, expected YYYY-MM-DD")
				return
			}
			params.DueDate = d
		} else {
			params.DueDate = pgtype.Date{Valid: false} // explicit null = clear date
		}
	}
	if params.ProjectType.Valid && isIndefiniteProjectType(params.ProjectType.String) {
		if _, typeChanged := rawFields["project_type"]; typeChanged {
			// Changing to an indefinite service removes a stale contractual
			// deadline atomically, even when the caller omits due_date.
			params.DueDate = pgtype.Date{Valid: false}
		} else if _, dueChanged := rawFields["due_date"]; dueChanged && params.DueDate.Valid {
			writeError(w, http.StatusBadRequest, "due_date must be empty for an indefinite project_type")
			return
		}
	}
	project, err := h.Queries.UpdateProject(r.Context(), params)
	if err != nil {
		h.writeProjectWriteError(w, r, err, "update")
		return
	}
	resp := projectToResponse(project)
	applyProjectClient(&resp, h.loadProjectClientRefs(r.Context(), wsUUID, []pgtype.UUID{project.ID}))
	resp.IssueCount, resp.DoneCount = h.loadProjectIssueStats(r.Context(), project.ID)
	resp.ResourceCount = h.loadProjectResourceCount(r.Context(), project.ID)
	h.publish(protocol.EventProjectUpdated, workspaceID, "member", userID, map[string]any{"project": resp})
	writeJSON(w, http.StatusOK, resp)
}

func (h *Handler) DeleteProject(w http.ResponseWriter, r *http.Request) {
	id := chi.URLParam(r, "id")
	workspaceID := h.resolveWorkspaceID(r)
	idUUID, ok := parseUUIDOrBadRequest(w, id, "project id")
	if !ok {
		return
	}
	wsUUID, ok := parseUUIDOrBadRequest(w, workspaceID, "workspace id")
	if !ok {
		return
	}
	project, err := h.Queries.GetProjectInWorkspace(r.Context(), db.GetProjectInWorkspaceParams{
		ID: idUUID, WorkspaceID: wsUUID,
	})
	if err != nil {
		writeError(w, http.StatusNotFound, "project not found")
		return
	}
	requester, ok := h.requireWorkspaceRole(w, r, uuidToString(project.WorkspaceID), "project not found", "owner", "admin")
	if !ok {
		return
	}
	userID := uuidToString(requester.UserID)
	tx, err := h.TxStarter.Begin(r.Context())
	if err != nil {
		writeError(w, http.StatusInternalServerError, "failed to start transaction")
		return
	}
	defer tx.Rollback(r.Context())
	qtx := h.Queries.WithTx(tx)

	if _, err := qtx.LockProjectForDelete(r.Context(), db.LockProjectForDeleteParams{
		ID:          project.ID,
		WorkspaceID: project.WorkspaceID,
	}); err != nil {
		if errors.Is(err, pgx.ErrNoRows) {
			writeError(w, http.StatusNotFound, "project not found")
			return
		}
		writeError(w, http.StatusInternalServerError, "failed to lock project")
		return
	}
	if err := qtx.ClearChatSessionProjectByProject(r.Context(), db.ClearChatSessionProjectByProjectParams{
		ProjectID:   project.ID,
		WorkspaceID: project.WorkspaceID,
	}); err != nil {
		writeError(w, http.StatusInternalServerError, "failed to clear project chat context")
		return
	}
	if err := qtx.DeleteProject(r.Context(), db.DeleteProjectParams{
		ID:          project.ID,
		WorkspaceID: project.WorkspaceID,
	}); err != nil {
		writeError(w, http.StatusInternalServerError, "failed to delete project")
		return
	}
	if err := tx.Commit(r.Context()); err != nil {
		writeError(w, http.StatusInternalServerError, "failed to commit project delete")
		return
	}
	h.publish(protocol.EventProjectDeleted, workspaceID, "member", userID, map[string]any{"project_id": uuidToString(project.ID)})
	w.WriteHeader(http.StatusNoContent)
}

// SearchProjectResponse extends ProjectResponse with search metadata.
type SearchProjectResponse struct {
	ProjectResponse
	MatchSource    string  `json:"match_source"`
	MatchedSnippet *string `json:"matched_snippet,omitempty"`
}

// buildProjectSearchQuery builds a dynamic SQL query for project search.
func buildProjectSearchQuery(phrase string, terms []string, includeClosed bool) (string, []any) {
	phrase = strings.ToLower(phrase)
	for i, t := range terms {
		terms[i] = strings.ToLower(t)
	}

	argIdx := 1
	args := []any{}
	nextArg := func(val any) string {
		args = append(args, val)
		s := fmt.Sprintf("$%d", argIdx)
		argIdx++
		return s
	}

	escapedPhrase := escapeLike(phrase)
	phraseParam := nextArg(escapedPhrase)
	phraseContains := "'%' || " + phraseParam + " || '%'"
	phraseStartsWith := phraseParam + " || '%'"

	wsParam := nextArg(nil) // workspace_id placeholder

	var termParams []string
	if len(terms) > 1 {
		for _, t := range terms {
			et := escapeLike(t)
			termParams = append(termParams, nextArg(et))
		}
	}

	// --- WHERE clause ---
	var whereParts []string

	// Full phrase match: title or description
	phraseMatch := fmt.Sprintf(
		"(LOWER(p.title) LIKE %s OR LOWER(COALESCE(p.description, '')) LIKE %s)",
		phraseContains, phraseContains,
	)
	whereParts = append(whereParts, phraseMatch)

	// Multi-word AND match
	if len(termParams) > 1 {
		var termConditions []string
		for _, tp := range termParams {
			tc := "'%' || " + tp + " || '%'"
			termConditions = append(termConditions, fmt.Sprintf(
				"(LOWER(p.title) LIKE %s OR LOWER(COALESCE(p.description, '')) LIKE %s)",
				tc, tc,
			))
		}
		whereParts = append(whereParts, "("+strings.Join(termConditions, " AND ")+")")
	}

	whereClause := "(" + strings.Join(whereParts, " OR ") + ")"

	if !includeClosed {
		whereClause += " AND p.status NOT IN ('completed', 'cancelled')"
	}

	// --- ORDER BY ranking ---
	var rankCases []string

	// Tier 0: Exact title match
	rankCases = append(rankCases, fmt.Sprintf("WHEN LOWER(p.title) = %s THEN 0", phraseParam))

	// Tier 1: Title starts with phrase
	rankCases = append(rankCases, fmt.Sprintf("WHEN LOWER(p.title) LIKE %s THEN 1", phraseStartsWith))

	// Tier 2: Title contains phrase
	rankCases = append(rankCases, fmt.Sprintf("WHEN LOWER(p.title) LIKE %s THEN 2", phraseContains))

	// Tier 3: Title matches all words (multi-word only)
	if len(termParams) > 1 {
		var titleTerms []string
		for _, tp := range termParams {
			titleTerms = append(titleTerms, fmt.Sprintf("LOWER(p.title) LIKE '%s' || %s || '%s'", "%", tp, "%"))
		}
		rankCases = append(rankCases, fmt.Sprintf("WHEN (%s) THEN 3", strings.Join(titleTerms, " AND ")))
	}

	// Tier 4: Description contains phrase
	rankCases = append(rankCases, fmt.Sprintf("WHEN LOWER(COALESCE(p.description, '')) LIKE %s THEN 4", phraseContains))

	rankExpr := "CASE " + strings.Join(rankCases, " ") + " ELSE 5 END"

	// --- match_source expression ---
	matchSourceExpr := fmt.Sprintf(`CASE
		WHEN LOWER(p.title) LIKE %s THEN 'title'
		ELSE 'description'
	END`, phraseContains)

	if len(termParams) > 1 {
		var titleTerms []string
		for _, tp := range termParams {
			titleTerms = append(titleTerms, fmt.Sprintf("LOWER(p.title) LIKE '%s' || %s || '%s'", "%", tp, "%"))
		}
		matchSourceExpr = fmt.Sprintf(`CASE
			WHEN LOWER(p.title) LIKE %s THEN 'title'
			WHEN (%s) THEN 'title'
			ELSE 'description'
		END`,
			phraseContains, strings.Join(titleTerms, " AND "),
		)
	}

	limitParam := nextArg(nil)
	offsetParam := nextArg(nil)

	query := fmt.Sprintf(`SELECT p.id, p.workspace_id, p.title, p.description, p.icon,
		p.status, p.priority, p.project_type, p.lead_type, p.lead_id,
		p.start_date, p.due_date,
		p.created_at, p.updated_at,
		COUNT(*) OVER() AS total_count,
		%s AS match_source
	FROM project p
	WHERE p.workspace_id = %s AND %s
	ORDER BY %s, p.updated_at DESC
	LIMIT %s OFFSET %s`,
		matchSourceExpr,
		wsParam,
		whereClause,
		rankExpr,
		limitParam,
		offsetParam,
	)

	return query, args
}

func (h *Handler) SearchProjects(w http.ResponseWriter, r *http.Request) {
	ctx := r.Context()
	workspaceID := h.resolveWorkspaceID(r)

	q := r.URL.Query().Get("q")
	if q == "" {
		writeError(w, http.StatusBadRequest, "q parameter is required")
		return
	}

	limit := 20
	offset := 0
	if l := r.URL.Query().Get("limit"); l != "" {
		if v, err := strconv.Atoi(l); err == nil && v > 0 {
			limit = v
		}
	}
	if limit > 50 {
		limit = 50
	}
	if o := r.URL.Query().Get("offset"); o != "" {
		if v, err := strconv.Atoi(o); err == nil && v >= 0 {
			offset = v
		}
	}

	includeClosed := r.URL.Query().Get("include_closed") == "true"

	wsUUID, ok := parseUUIDOrBadRequest(w, workspaceID, "workspace_id")
	if !ok {
		return
	}
	terms := splitSearchTerms(q)

	sqlQuery, args := buildProjectSearchQuery(q, terms, includeClosed)
	args[1] = wsUUID
	args[len(args)-2] = limit
	args[len(args)-1] = offset

	type projectSearchRow struct {
		project     db.Project
		totalCount  int64
		matchSource string
	}

	var results []projectSearchRow
	err := runSearchQuery(ctx, h.TxStarter, sqlQuery, args, func(rows pgx.Rows) error {
		for rows.Next() {
			var row projectSearchRow
			if err := rows.Scan(
				&row.project.ID,
				&row.project.WorkspaceID,
				&row.project.Title,
				&row.project.Description,
				&row.project.Icon,
				&row.project.Status,
				&row.project.Priority,
				&row.project.ProjectType,
				&row.project.LeadType,
				&row.project.LeadID,
				&row.project.StartDate,
				&row.project.DueDate,
				&row.project.CreatedAt,
				&row.project.UpdatedAt,
				&row.totalCount,
				&row.matchSource,
			); err != nil {
				return fmt.Errorf("scan: %w", err)
			}
			results = append(results, row)
		}
		return rows.Err()
	})
	if err != nil {
		// Statement-timeout surfaces as SQLSTATE 57014 — same
		// fail-fast contract as SearchIssues (see runSearchQuery).
		if isSearchStatementTimeout(err) {
			slog.Warn("search projects timed out",
				"workspace_id", workspaceID,
				"query", q,
				"timeout", searchStatementTimeout)
			writeError(w, http.StatusServiceUnavailable, "search timed out; please refine your query or try again")
			return
		}
		slog.Warn("search projects failed", "error", err, "workspace_id", workspaceID, "query", q)
		writeError(w, http.StatusInternalServerError, "failed to search projects")
		return
	}

	var total int64
	if len(results) > 0 {
		total = results[0].totalCount
	}

	// Batch-fetch issue stats and resource counts
	statsMap := make(map[string]db.GetProjectIssueStatsRow)
	resourceCountMap := make(map[string]int64)
	clientRefs := make(map[string]projectClientRef)
	if len(results) > 0 {
		projectIDs := make([]pgtype.UUID, len(results))
		for i, r := range results {
			projectIDs[i] = r.project.ID
		}
		clientRefs = h.loadProjectClientRefs(ctx, wsUUID, projectIDs)
		stats, err := h.Queries.GetProjectIssueStats(ctx, projectIDs)
		if err == nil {
			for _, s := range stats {
				statsMap[uuidToString(s.ProjectID)] = s
			}
		}
		counts, err := h.Queries.GetProjectResourceCounts(ctx, projectIDs)
		if err == nil {
			for _, c := range counts {
				resourceCountMap[uuidToString(c.ProjectID)] = c.ResourceCount
			}
		}
	}

	resp := make([]SearchProjectResponse, len(results))
	for i, row := range results {
		pr := projectToResponse(row.project)
		applyProjectClient(&pr, clientRefs)
		if s, ok := statsMap[pr.ID]; ok {
			pr.IssueCount = s.TotalCount
			pr.DoneCount = s.DoneCount
		}
		pr.ResourceCount = resourceCountMap[pr.ID]
		spr := SearchProjectResponse{
			ProjectResponse: pr,
			MatchSource:     row.matchSource,
		}
		if row.matchSource == "description" {
			desc := ""
			if row.project.Description.Valid {
				desc = row.project.Description.String
			}
			if desc != "" {
				snippet := extractSnippet(desc, q)
				spr.MatchedSnippet = &snippet
			}
		}
		resp[i] = spr
	}

	w.Header().Set("X-Total-Count", strconv.FormatInt(total, 10))
	writeJSON(w, http.StatusOK, map[string]any{
		"projects": resp,
		"total":    total,
	})
}

// ProjectMemberAccess is one row of "who can see this project", answered from
// the project's side. The same fact as a member's project list, read the other
// way round — both write the same member_project rows.
type ProjectMemberAccess struct {
	MemberID    string `json:"member_id"`
	UserID      string `json:"user_id"`
	Name        string `json:"name"`
	Email       string `json:"email"`
	Role        string `json:"role"`
	AccessScope string `json:"access_scope"`
	// Bound is an explicit binding to this project.
	Bound bool `json:"bound"`
	// Sees is what the screen actually asks. It is not the same as Bound: the
	// owner and anyone still in 'workspace' mode sees the project with no
	// binding at all, and showing them as unchecked would read as "denied".
	Sees bool `json:"sees"`
}

// loadProjectForAccess resolves the project in the caller's workspace.
func (h *Handler) loadProjectForAccess(w http.ResponseWriter, r *http.Request) (db.Project, pgtype.UUID, bool) {
	var zero db.Project
	projUUID, ok := parseUUIDOrBadRequest(w, chi.URLParam(r, "id"), "project id")
	if !ok {
		return zero, pgtype.UUID{}, false
	}
	wsUUID, ok := parseUUIDOrBadRequest(w, h.resolveWorkspaceID(r), "workspace id")
	if !ok {
		return zero, pgtype.UUID{}, false
	}
	project, err := h.Queries.GetProjectInWorkspace(r.Context(), db.GetProjectInWorkspaceParams{
		ID: projUUID, WorkspaceID: wsUUID,
	})
	if err != nil {
		writeError(w, http.StatusNotFound, "project not found")
		return zero, pgtype.UUID{}, false
	}
	return project, wsUUID, true
}

// ListProjectMembers answers "who sees this project". Owner/admin only — it
// enumerates the workspace's people.
func (h *Handler) ListProjectMembers(w http.ResponseWriter, r *http.Request) {
	project, wsUUID, ok := h.loadProjectForAccess(w, r)
	if !ok {
		return
	}
	members, err := h.Queries.ListMembersWithUser(r.Context(), wsUUID)
	if err != nil {
		writeError(w, http.StatusInternalServerError, "failed to list members")
		return
	}
	boundUserIDs, err := h.Queries.ListMemberUserIDsByProject(r.Context(), project.ID)
	if err != nil {
		writeError(w, http.StatusInternalServerError, "failed to list project bindings")
		return
	}
	bound := make(map[string]bool, len(boundUserIDs))
	for _, id := range boundUserIDs {
		bound[uuidToString(id)] = true
	}

	rows := make([]ProjectMemberAccess, 0, len(members))
	for _, m := range members {
		userID := uuidToString(m.UserID)
		isBound := bound[userID]
		// Mirrors ResolveProjectScope: owner is never scoped, a guest is always
		// scoped by bindings whatever the mode says, everyone else follows the
		// mode. Diverging from that resolver here would draw a checkbox that
		// does not match what the person can actually open.
		unrestricted := m.Role == "owner" || (m.Role != "guest" && m.AccessScope != "projects")
		rows = append(rows, ProjectMemberAccess{
			MemberID:    uuidToString(m.ID),
			UserID:      userID,
			Name:        m.UserName,
			Email:       m.UserEmail,
			Role:        m.Role,
			AccessScope: m.AccessScope,
			Bound:       isBound,
			Sees:        unrestricted || isBound,
		})
	}
	writeJSON(w, http.StatusOK, map[string]any{"members": rows})
}

// BindProjectMember grants one person access to this project.
func (h *Handler) BindProjectMember(w http.ResponseWriter, r *http.Request) {
	project, wsUUID, ok := h.loadProjectForAccess(w, r)
	if !ok {
		return
	}
	var req struct {
		UserID string `json:"user_id"`
	}
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
		writeError(w, http.StatusBadRequest, "invalid request body")
		return
	}
	userUUID, ok := parseUUIDOrBadRequest(w, req.UserID, "user_id")
	if !ok {
		return
	}
	if _, err := h.Queries.GetMemberByUserAndWorkspace(r.Context(), db.GetMemberByUserAndWorkspaceParams{
		UserID: userUUID, WorkspaceID: wsUUID,
	}); err != nil {
		writeError(w, http.StatusBadRequest, "user is not a member of this workspace")
		return
	}
	actor, _ := util.ParseUUID(requestUserID(r))
	if _, err := h.Queries.CreateMemberProject(r.Context(), db.CreateMemberProjectParams{
		WorkspaceID: wsUUID,
		UserID:      userUUID,
		ProjectID:   project.ID,
		CreatedBy:   actor,
	}); err != nil {
		writeError(w, http.StatusInternalServerError, "failed to bind project")
		return
	}
	writeJSON(w, http.StatusOK, map[string]string{"status": "bound"})
}

// UnbindProjectMember revokes one person's access to this project, through the
// same path as the member-side revoke: if work is assigned to them here, the
// caller has to say where it goes.
func (h *Handler) UnbindProjectMember(w http.ResponseWriter, r *http.Request) {
	project, wsUUID, ok := h.loadProjectForAccess(w, r)
	if !ok {
		return
	}
	var req GuestProjectRequest
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil && err != io.EOF {
		writeError(w, http.StatusBadRequest, "invalid request body")
		return
	}
	userUUID, ok := parseUUIDOrBadRequest(w, chi.URLParam(r, "userId"), "userId")
	if !ok {
		return
	}
	if _, err := h.Queries.GetMemberByUserAndWorkspace(r.Context(), db.GetMemberByUserAndWorkspaceParams{
		UserID: userUUID, WorkspaceID: wsUUID,
	}); err != nil {
		writeError(w, http.StatusNotFound, "user is not a member of this workspace")
		return
	}
	h.unbindMemberFromProject(w, r, wsUUID, project.ID, userUUID, req)
}
