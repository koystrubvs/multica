package handler

// business_billing_runs.go — Business UI queue for agency billing periods:
// prepare elapsed cycles as `ready`, confirm (close + PDF + Elba + receivable).

import (
	"bytes"
	"context"
	"errors"
	"fmt"
	"log/slog"
	"net/http"
	"strings"
	"time"

	"github.com/go-chi/chi/v5"
	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgtype"

	db "github.com/multica-ai/multica/server/pkg/db/generated"
)

type businessBillingRunChargeJSON struct {
	ID         string  `json:"id"`
	IssueID    string  `json:"issue_id"`
	IssueTitle string  `json:"issue_title"`
	PriceRub   float64 `json:"price_rub"`
	Status     string  `json:"status"`
	CreatedAt  string  `json:"created_at"`
}

type businessBillingRunJSON struct {
	PeriodID           string                         `json:"period_id"`
	ProjectID          string                         `json:"project_id"`
	ProjectTitle       string                         `json:"project_title"`
	WorkspaceID        string                         `json:"workspace_id"`
	ClientID           *string                        `json:"client_id"`
	ClientName         string                         `json:"client_name"`
	ElbaContractorID   *string                        `json:"elba_contractor_id"`
	BillingMode        string                         `json:"billing_mode"`
	AnchorDay          int32                          `json:"anchor_day"`
	StartsOn           string                         `json:"starts_on"`
	EndsOn             string                         `json:"ends_on"`
	Status             string                         `json:"status"`
	TotalRub           float64                        `json:"total_rub"`
	ConfirmedTotalRub  float64                        `json:"confirmed_total_rub"`
	ChargeCount        int32                          `json:"charge_count"`
	DraftCount         int32                          `json:"draft_count"`
	ElbaInvoiceID      *string                        `json:"elba_invoice_id"`
	ElbaActID          *string                        `json:"elba_act_id"`
	ReportFile         *string                        `json:"report_file"`
	ElbaInvoiceURL     *string                        `json:"elba_invoice_url"`
	ElbaActURL         *string                        `json:"elba_act_url"`
	Charges            []businessBillingRunChargeJSON `json:"charges,omitempty"`
	ReadyOn            string                         `json:"ready_on"`
}

func formatPgDate(d pgtype.Date) string {
	if !d.Valid {
		return ""
	}
	return d.Time.Format("2006-01-02")
}

func elbaDocURL(kind, id string) *string {
	id = strings.TrimSpace(id)
	if id == "" {
		return nil
	}
	// Kontur Elba web app deep-link pattern used by operators.
	u := fmt.Sprintf("https://elba.kontur.ru/%s/%s", kind, id)
	return &u
}

func (h *Handler) ListBusinessBillingRuns(w http.ResponseWriter, r *http.Request) {
	businessID, _, ok := businessRequestIDs(w, r)
	if !ok {
		return
	}
	bizUUID, ok := parseUUIDOrBadRequest(w, businessID, "business_id")
	if !ok {
		return
	}
	rows, err := h.Queries.ListBusinessBillingRunPeriods(r.Context(), bizUUID)
	if err != nil {
		slog.Error("billing runs: list failed", "error", err)
		writeError(w, http.StatusInternalServerError, "failed to list billing runs")
		return
	}
	out := make([]businessBillingRunJSON, 0, len(rows))
	includeCharges := r.URL.Query().Get("include") == "charges"
	for _, row := range rows {
		item := businessBillingRunJSON{
			PeriodID:          uuidToString(row.ID),
			ProjectID:         uuidToString(row.ProjectID),
			ProjectTitle:      row.ProjectTitle,
			WorkspaceID:       uuidToString(row.WorkspaceID),
			ClientName:        textOrEmpty(row.ClientName),
			BillingMode:       row.BillingMode,
			AnchorDay:         row.AnchorDay,
			StartsOn:          formatPgDate(row.StartsOn),
			EndsOn:            formatPgDate(row.EndsOn),
			Status:            row.Status,
			TotalRub:          row.TotalRub,
			ConfirmedTotalRub: row.ConfirmedTotalRub,
			ChargeCount:       row.ChargeCount,
			DraftCount:        row.DraftCount,
			ElbaInvoiceID:     textToPtr(row.ElbaInvoiceID),
			ElbaActID:         textToPtr(row.ElbaActID),
			ReportFile:        textToPtr(row.ReportFile),
			ReadyOn:           formatPgDate(row.EndsOn),
		}
		if row.ClientID.Valid {
			s := uuidToString(row.ClientID)
			item.ClientID = &s
		}
		if row.ElbaContractorID.Valid && row.ElbaContractorID.String != "" {
			s := row.ElbaContractorID.String
			item.ElbaContractorID = &s
		}
		if row.ElbaInvoiceID.Valid {
			item.ElbaInvoiceURL = elbaDocURL("Bills", row.ElbaInvoiceID.String)
		}
		if row.ElbaActID.Valid {
			item.ElbaActURL = elbaDocURL("Acts", row.ElbaActID.String)
		}
		if includeCharges && (row.Status == "ready" || row.Status == "closed" || row.Status == "invoiced") {
			charges, cerr := h.Queries.ListClientBillingChargesByPeriod(r.Context(), row.ID)
			if cerr == nil {
				item.Charges = make([]businessBillingRunChargeJSON, 0, len(charges))
				for _, c := range charges {
					if c.Status == "void" {
						continue
					}
					createdAt := ""
					if c.CreatedAt.Valid {
						createdAt = c.CreatedAt.Time.UTC().Format(time.RFC3339)
					}
					item.Charges = append(item.Charges, businessBillingRunChargeJSON{
						ID:         uuidToString(c.ID),
						IssueID:    uuidToString(c.IssueID),
						IssueTitle: c.IssueTitle,
						PriceRub:   c.PriceRub,
						Status:     c.Status,
						CreatedAt:  createdAt,
					})
				}
			}
		}
		out = append(out, item)
	}
	writeJSON(w, http.StatusOK, map[string]any{"runs": out})
}

func textOrEmpty(t pgtype.Text) string {
	if !t.Valid {
		return ""
	}
	return t.String
}

// PrepareBusinessBillingRuns sweeps enabled projects, materializes periods,
// and marks elapsed open periods as ready for review.
func (h *Handler) PrepareBusinessBillingRuns(w http.ResponseWriter, r *http.Request) {
	businessID, userID, ok := businessRequestIDs(w, r)
	if !ok {
		return
	}
	userUUID, ok := parseUUIDOrBadRequest(w, userID, "user_id")
	if !ok {
		return
	}

	type projectRow struct {
		ProjectID   pgtype.UUID
		WorkspaceID pgtype.UUID
	}
	rows, err := h.DB.Query(r.Context(), `
		SELECT DISTINCT bcp.project_id, bcp.workspace_id
		FROM business_client_project bcp
		JOIN client_billing_config cfg ON cfg.project_id = bcp.project_id AND cfg.enabled
		WHERE bcp.business_id = $1
	`, businessID)
	if err != nil {
		writeError(w, http.StatusInternalServerError, "failed to list billing projects")
		return
	}
	defer rows.Close()
	projects := make([]projectRow, 0)
	for rows.Next() {
		var p projectRow
		if err := rows.Scan(&p.ProjectID, &p.WorkspaceID); err != nil {
			writeError(w, http.StatusInternalServerError, "failed to list billing projects")
			return
		}
		projects = append(projects, p)
	}

	prepared := 0
	readyMarked := 0
	for _, p := range projects {
		cfg, err := h.Queries.GetClientBillingConfig(r.Context(), p.ProjectID)
		if err != nil || !cfg.Enabled {
			continue
		}
		project, err := h.Queries.GetProject(r.Context(), p.ProjectID)
		if err != nil {
			continue
		}
		if _, err := h.sweepProjectBilling(r.Context(), project, cfg); err != nil {
			slog.Warn("billing prepare: sweep failed", "project_id", uuidToString(p.ProjectID), "error", err)
		}
		before, _ := h.Queries.ListClientBillingPeriods(r.Context(), p.ProjectID)
		readyBefore := map[string]bool{}
		for _, per := range before {
			if per.Status == "ready" {
				readyBefore[uuidToString(per.ID)] = true
			}
		}
		if _, err := h.ensureOpenBillingPeriod(r.Context(), cfg, p.ProjectID, p.WorkspaceID); err != nil {
			slog.Warn("billing prepare: ensure period failed", "project_id", uuidToString(p.ProjectID), "error", err)
			continue
		}
		prepared++
		after, _ := h.Queries.ListClientBillingPeriods(r.Context(), p.ProjectID)
		for _, per := range after {
			if per.Status == "ready" && !readyBefore[uuidToString(per.ID)] {
				readyMarked++
				if _, _, err := h.confirmPeriodDrafts(r.Context(), per.ID, userUUID); err != nil {
					slog.Warn("billing prepare: confirm drafts failed", "period_id", uuidToString(per.ID), "error", err)
				}
				total, _ := h.Queries.SumConfirmedChargesInPeriod(r.Context(), per.ID)
				_ = h.Queries.UpdateClientBillingPeriodTotal(r.Context(), db.UpdateClientBillingPeriodTotalParams{
					ID: per.ID, TotalRub: total,
				})
			}
		}
	}

	writeJSON(w, http.StatusOK, map[string]any{
		"projects_prepared": prepared,
		"periods_marked_ready": readyMarked,
	})
}

// ConfirmBusinessBillingPeriod closes a ready/open period, generates the
// client PDF report, pushes счёт+акт to Elba, and marks matching receivables.
func (h *Handler) ConfirmBusinessBillingPeriod(w http.ResponseWriter, r *http.Request) {
	businessID, userID, ok := businessRequestIDs(w, r)
	if !ok {
		return
	}
	periodID := chi.URLParam(r, "periodId")
	periodUUID, ok := parseUUIDOrBadRequest(w, periodID, "period_id")
	if !ok {
		return
	}
	userUUID, ok := parseUUIDOrBadRequest(w, userID, "user_id")
	if !ok {
		return
	}

	var projectID, workspaceID pgtype.UUID
	var status string
	err := h.DB.QueryRow(r.Context(), `
		SELECT per.project_id, per.workspace_id, per.status
		FROM client_billing_period per
		JOIN business_client_project bcp ON bcp.project_id = per.project_id AND bcp.business_id = $1
		WHERE per.id = $2
	`, businessID, periodUUID).Scan(&projectID, &workspaceID, &status)
	if errors.Is(err, pgx.ErrNoRows) {
		writeError(w, http.StatusNotFound, "billing period not found in this business")
		return
	}
	if err != nil {
		writeError(w, http.StatusInternalServerError, "failed to load billing period")
		return
	}
	if status != "ready" && status != "open" {
		writeError(w, http.StatusConflict, "only ready or open periods can be confirmed")
		return
	}

	project, err := h.Queries.GetProject(r.Context(), projectID)
	if err != nil {
		writeError(w, http.StatusInternalServerError, "failed to load project")
		return
	}
	cfg, err := h.Queries.GetClientBillingConfig(r.Context(), projectID)
	if err != nil || !cfg.Enabled {
		writeError(w, http.StatusBadRequest, "billing is not enabled for this project")
		return
	}

	if open, err := h.Queries.CountOpenClientBillingDisputesInProject(r.Context(), projectID); err == nil && open > 0 {
		writeError(w, http.StatusConflict, fmt.Sprintf("resolve %d open billing dispute(s) before confirming", open))
		return
	}

	if _, err := h.sweepProjectBilling(r.Context(), project, cfg); err != nil {
		writeError(w, http.StatusInternalServerError, "sweep failed: "+err.Error())
		return
	}
	_, internalVoided, err := h.confirmPeriodDrafts(r.Context(), periodUUID, userUUID)
	if err != nil {
		writeError(w, http.StatusInternalServerError, "failed to confirm draft charges")
		return
	}

	// Best-effort: accept existing draft task economics for period issues.
	econAccepted := h.acceptPeriodEconomics(r.Context(), businessID, userID, periodUUID)

	total, err := h.Queries.SumConfirmedChargesInPeriod(r.Context(), periodUUID)
	if err != nil {
		writeError(w, http.StatusInternalServerError, "failed to sum period charges")
		return
	}
	closed, err := h.Queries.CloseClientBillingPeriod(r.Context(), db.CloseClientBillingPeriodParams{
		ID: periodUUID, TotalRub: total,
	})
	if errors.Is(err, pgx.ErrNoRows) {
		writeError(w, http.StatusConflict, "period could not be closed")
		return
	}
	if err != nil {
		writeError(w, http.StatusInternalServerError, "failed to close billing period")
		return
	}

	reportURL, reportErr := h.generateBillingPeriodReport(r.Context(), project, closed)
	if reportErr != nil {
		slog.Warn("billing confirm: report generation failed", "period_id", periodID, "error", reportErr)
	} else if reportURL != "" {
		if _, err := h.DB.Exec(r.Context(), `
			UPDATE client_billing_period SET report_file = $2, updated_at = now() WHERE id = $1
		`, periodUUID, reportURL); err != nil {
			slog.Warn("billing confirm: report_file update failed", "error", err)
		}
	}

	periodRow := db.GetClientBillingPeriodInProjectRow{
		ID: closed.ID, ProjectID: closed.ProjectID, WorkspaceID: closed.WorkspaceID,
		StartsOn: closed.StartsOn, EndsOn: closed.EndsOn, Status: closed.Status,
		TotalRub: closed.TotalRub, LastAlertPercent: closed.LastAlertPercent,
		ElbaInvoiceID: closed.ElbaInvoiceID, ElbaActID: closed.ElbaActID,
		ReportFile: pgtype.Text{String: reportURL, Valid: reportURL != ""},
		ClosedAt: closed.ClosedAt, PaidAt: closed.PaidAt, CreatedAt: closed.CreatedAt, UpdatedAt: closed.UpdatedAt,
	}
	invoiced, pushErr := h.pushPeriodToElba(r.Context(), project, periodRow)
	resp := map[string]any{
		"period":             closed,
		"economics_accepted": econAccepted,
		"internal_voided":    internalVoided,
	}
	if reportURL != "" {
		resp["report_file"] = reportURL
	}
	if pushErr == nil {
		resp["period"] = invoiced
		if reportURL != "" {
			_, _ = h.DB.Exec(r.Context(), `
				UPDATE client_billing_period SET report_file = $2, updated_at = now() WHERE id = $1
			`, periodUUID, reportURL)
		}
		resp["elba_invoice_id"] = textToPtr(invoiced.ElbaInvoiceID)
		resp["elba_act_id"] = textToPtr(invoiced.ElbaActID)
		if invoiced.ElbaInvoiceID.Valid {
			resp["elba_invoice_url"] = elbaDocURL("Bills", invoiced.ElbaInvoiceID.String)
		}
		if invoiced.ElbaActID.Valid {
			resp["elba_act_url"] = elbaDocURL("Acts", invoiced.ElbaActID.String)
		}
		h.markReceivablesInvoiced(r.Context(), businessID, projectID, closed, invoiced.ElbaInvoiceID, invoiced.ElbaActID)
	} else if !errors.Is(pushErr, errElbaSkipped) {
		resp["elba_error"] = pushErr.Error()
	} else {
		resp["elba_skipped"] = true
	}

	writeJSON(w, http.StatusOK, resp)
}

func (h *Handler) acceptPeriodEconomics(ctx context.Context, businessID, userID string, periodID pgtype.UUID) int {
	rows, err := h.DB.Query(ctx, `
		SELECT te.id::text
		FROM business_task_economics te
		JOIN client_billing_charge c ON c.issue_id = te.issue_id AND c.period_id = $2 AND c.status = 'confirmed'
		WHERE te.business_id = $1 AND te.status = 'draft'
	`, businessID, periodID)
	if err != nil {
		return 0
	}
	defer rows.Close()
	ids := make([]string, 0)
	for rows.Next() {
		var id string
		if rows.Scan(&id) == nil {
			ids = append(ids, id)
		}
	}
	accepted := 0
	for _, id := range ids {
		tx, err := h.TxStarter.Begin(ctx)
		if err != nil {
			continue
		}
		var status string
		if err := tx.QueryRow(ctx, `SELECT status FROM business_task_economics WHERE business_id=$1 AND id=$2 FOR UPDATE`, businessID, id).Scan(&status); err != nil {
			_ = tx.Rollback(ctx)
			continue
		}
		if status == "accepted" {
			_ = tx.Rollback(ctx)
			accepted++
			continue
		}
		var n int
		if err := tx.QueryRow(ctx, `SELECT count(*) FROM business_task_participant WHERE business_id=$1 AND task_economics_id=$2 AND participation_confirmed`, businessID, id).Scan(&n); err != nil || n == 0 {
			_ = tx.Rollback(ctx)
			continue
		}
		if _, err := tx.Exec(ctx, `UPDATE business_task_economics SET status='accepted',accepted_at=now(),accepted_by=$3,updated_at=now() WHERE business_id=$1 AND id=$2`, businessID, id, userID); err != nil {
			_ = tx.Rollback(ctx)
			continue
		}
		if _, err := tx.Exec(ctx, `
			INSERT INTO business_accrual (business_id,task_economics_id,participant_id,worker_id,role,policy_snapshot,original_amount_rub,holdback_percent,idempotency_key)
			SELECT p.business_id,p.task_economics_id,p.id,p.worker_id,p.role,e.policy_snapshot,p.amount_rub,0,'accept:'||e.id::text||':'||p.id::text
			FROM business_task_participant p JOIN business_task_economics e ON e.id=p.task_economics_id AND e.business_id=p.business_id
			WHERE p.business_id=$1 AND p.task_economics_id=$2 AND p.participation_confirmed
			ON CONFLICT (business_id,idempotency_key) DO NOTHING
		`, businessID, id); err != nil {
			slog.Warn("billing confirm: accrual insert failed", "economics_id", id, "error", err)
			_ = tx.Rollback(ctx)
			continue
		}
		if err := tx.Commit(ctx); err != nil {
			_ = tx.Rollback(ctx)
			continue
		}
		accepted++
	}
	return accepted
}

func (h *Handler) markReceivablesInvoiced(ctx context.Context, businessID string, projectID pgtype.UUID, period db.CloseClientBillingPeriodRow, invoiceID, actID pgtype.Text) {
	if !period.StartsOn.Valid {
		return
	}
	periodKey := period.StartsOn.Time.Format("2006-01")
	_, err := h.DB.Exec(ctx, `
		UPDATE business_receivable
		SET status = CASE WHEN status IN ('paid', 'partially_paid', 'skipped', 'written_off') THEN status ELSE 'invoiced' END,
		    elba_invoice_id = COALESCE(NULLIF($4, ''), elba_invoice_id),
		    elba_act_id = COALESCE(NULLIF($5, ''), elba_act_id),
		    client_billing_period_id = $3,
		    updated_at = now()
		WHERE business_id = $1
		  AND project_id = $2
		  AND period_key = $6
	`, businessID, projectID, period.ID, textOrEmpty(invoiceID), textOrEmpty(actID), periodKey)
	if err != nil {
		slog.Warn("billing confirm: receivable update failed", "period_id", uuidToString(period.ID), "error", err)
	}
}

func (h *Handler) generateBillingPeriodReport(ctx context.Context, project db.Project, period db.CloseClientBillingPeriodRow) (string, error) {
	if h.Storage == nil {
		return "", errors.New("storage not configured")
	}
	charges, err := h.Queries.ListClientBillingChargesByPeriod(ctx, period.ID)
	if err != nil {
		return "", err
	}
	var b bytes.Buffer
	b.WriteString("<!DOCTYPE html><html><head><meta charset=\"utf-8\"><title>Отчёт по работам</title>")
	b.WriteString("<style>body{font-family:system-ui,sans-serif;padding:24px;color:#111}table{border-collapse:collapse;width:100%}th,td{border:1px solid #ddd;padding:8px;text-align:left}th{background:#f5f5f5}.right{text-align:right}h1{font-size:20px}</style></head><body>")
	fmt.Fprintf(&b, "<h1>Отчёт по работам — %s</h1>", htmlEscape(project.Title))
	fmt.Fprintf(&b, "<p>Период: %s — %s<br>Сумма: %.0f ₽</p>", formatPgDate(period.StartsOn), formatPgDate(period.EndsOn), period.TotalRub)
	b.WriteString("<table><thead><tr><th>Дата</th><th>Задача</th><th class=\"right\">Сумма, ₽</th></tr></thead><tbody>")
	for _, c := range charges {
		if c.Status != "confirmed" {
			continue
		}
		created := ""
		if c.ConfirmedAt.Valid {
			created = c.ConfirmedAt.Time.UTC().Format("2006-01-02")
		} else if c.CreatedAt.Valid {
			created = c.CreatedAt.Time.UTC().Format("2006-01-02")
		}
		fmt.Fprintf(&b, "<tr><td>%s</td><td>%s</td><td class=\"right\">%.0f</td></tr>", created, htmlEscape(c.IssueTitle), c.PriceRub)
	}
	b.WriteString("</tbody></table></body></html>")

	key := fmt.Sprintf("workspaces/%s/billing-reports/%s.html", uuidToString(period.WorkspaceID), uuidToString(period.ID))
	url, err := h.Storage.Upload(ctx, key, b.Bytes(), "text/html; charset=utf-8", "billing-report.html")
	if err != nil {
		return "", err
	}
	return url, nil
}

func htmlEscape(s string) string {
	r := strings.NewReplacer("&", "&amp;", "<", "&lt;", ">", "&gt;", "\"", "&quot;")
	return r.Replace(s)
}

// GetBusinessBillingPeriodReport redirects/serves the stored report_file.
func (h *Handler) GetBusinessBillingPeriodReport(w http.ResponseWriter, r *http.Request) {
	businessID, _, ok := businessRequestIDs(w, r)
	if !ok {
		return
	}
	periodID := chi.URLParam(r, "periodId")
	periodUUID, ok := parseUUIDOrBadRequest(w, periodID, "period_id")
	if !ok {
		return
	}
	var report pgtype.Text
	err := h.DB.QueryRow(r.Context(), `
		SELECT per.report_file
		FROM client_billing_period per
		JOIN business_client_project bcp ON bcp.project_id = per.project_id AND bcp.business_id = $1
		WHERE per.id = $2
	`, businessID, periodUUID).Scan(&report)
	if errors.Is(err, pgx.ErrNoRows) {
		writeError(w, http.StatusNotFound, "period not found")
		return
	}
	if err != nil || !report.Valid || report.String == "" {
		writeError(w, http.StatusNotFound, "report not generated yet")
		return
	}
	writeJSON(w, http.StatusOK, map[string]string{"report_file": report.String})
}
