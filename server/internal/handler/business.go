package handler

import (
	"errors"
	"net/http"

	"github.com/go-chi/chi/v5"
	"github.com/jackc/pgx/v5"
	"github.com/multica-ai/multica/server/internal/featureflags"
	db "github.com/multica-ai/multica/server/pkg/db/generated"
	"github.com/multica-ai/multica/server/pkg/featureflag"
)

type BusinessAccountResponse struct {
	ID                          string `json:"id"`
	Name                        string `json:"name"`
	OwnerUserID                 string `json:"owner_user_id"`
	Currency                    string `json:"currency"`
	Timezone                    string `json:"timezone"`
	MonthlyOwnerIncomeTargetRUB string `json:"monthly_owner_income_target_rub"`
	Role                        string `json:"role"`
	CreatedAt                   string `json:"created_at"`
	UpdatedAt                   string `json:"updated_at"`
}

type BusinessWorkspaceResponse struct {
	BusinessID         string  `json:"business_id"`
	WorkspaceID        string  `json:"workspace_id"`
	WorkspaceName      string  `json:"workspace_name"`
	WorkspaceSlug      string  `json:"workspace_slug"`
	Kind               string  `json:"kind"`
	IncludeInPortfolio bool    `json:"include_in_portfolio"`
	IncludeRevenue     bool    `json:"include_revenue"`
	IncludeCosts       bool    `json:"include_costs"`
	ClientID           *string `json:"client_id,omitempty"`
	CreatedAt          string  `json:"created_at"`
	UpdatedAt          string  `json:"updated_at"`
}

// RequireBusinessControlPlane makes a disabled W1 surface indistinguishable
// from a route that has not been deployed. Rollback is therefore a flag flip;
// additive business tables remain in place.
func RequireBusinessControlPlane(flags *featureflag.Service) func(http.Handler) http.Handler {
	return func(next http.Handler) http.Handler {
		return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
			if !featureflags.BusinessControlPlaneEnabled(r.Context(), flags) {
				writeError(w, http.StatusNotFound, "not found")
				return
			}
			next.ServeHTTP(w, r)
		})
	}
}

// ListBusinessAccounts returns only accounts for which the authenticated user
// has a fresh owner membership. W1 deliberately exposes no non-owner surface.
func (h *Handler) ListBusinessAccounts(w http.ResponseWriter, r *http.Request) {
	userID, ok := requireUserID(w, r)
	if !ok {
		return
	}

	rows, err := h.Queries.ListBusinessAccountsForOwner(r.Context(), parseUUID(userID))
	if err != nil {
		writeError(w, http.StatusInternalServerError, "failed to list businesses")
		return
	}

	out := make([]BusinessAccountResponse, 0, len(rows))
	for _, row := range rows {
		out = append(out, businessAccountFromListRow(row))
	}
	writeJSON(w, http.StatusOK, out)
}

func (h *Handler) GetBusinessAccount(w http.ResponseWriter, r *http.Request) {
	userID, ok := requireUserID(w, r)
	if !ok {
		return
	}
	businessID, ok := parseUUIDOrBadRequest(w, chi.URLParam(r, "businessId"), "business_id")
	if !ok {
		return
	}

	row, err := h.Queries.GetBusinessAccountForOwner(r.Context(), db.GetBusinessAccountForOwnerParams{
		ID:     businessID,
		UserID: parseUUID(userID),
	})
	if errors.Is(err, pgx.ErrNoRows) {
		writeError(w, http.StatusNotFound, "business not found")
		return
	}
	if err != nil {
		writeError(w, http.StatusInternalServerError, "failed to get business")
		return
	}

	writeJSON(w, http.StatusOK, BusinessAccountResponse{
		ID:                          uuidToString(row.ID),
		Name:                        row.Name,
		OwnerUserID:                 uuidToString(row.OwnerUserID),
		Currency:                    row.Currency,
		Timezone:                    row.Timezone,
		MonthlyOwnerIncomeTargetRUB: row.MonthlyOwnerIncomeTargetRub,
		Role:                        row.Role,
		CreatedAt:                   timestampToString(row.CreatedAt),
		UpdatedAt:                   timestampToString(row.UpdatedAt),
	})
}

func (h *Handler) ListBusinessWorkspaces(w http.ResponseWriter, r *http.Request) {
	userID, ok := requireUserID(w, r)
	if !ok {
		return
	}
	businessID, ok := parseUUIDOrBadRequest(w, chi.URLParam(r, "businessId"), "business_id")
	if !ok {
		return
	}

	rows, err := h.Queries.ListBusinessWorkspacesForOwner(r.Context(), db.ListBusinessWorkspacesForOwnerParams{
		BusinessID: businessID,
		UserID:     parseUUID(userID),
	})
	if err != nil {
		writeError(w, http.StatusInternalServerError, "failed to list business workspaces")
		return
	}

	out := make([]BusinessWorkspaceResponse, 0, len(rows))
	for _, row := range rows {
		out = append(out, BusinessWorkspaceResponse{
			BusinessID:         uuidToString(row.BusinessID),
			WorkspaceID:        uuidToString(row.WorkspaceID),
			WorkspaceName:      row.WorkspaceName,
			WorkspaceSlug:      row.WorkspaceSlug,
			Kind:               row.Kind,
			IncludeInPortfolio: row.IncludeInPortfolio,
			IncludeRevenue:     row.IncludeRevenue,
			IncludeCosts:       row.IncludeCosts,
			ClientID:           uuidToPtr(row.ClientID),
			CreatedAt:          timestampToString(row.CreatedAt),
			UpdatedAt:          timestampToString(row.UpdatedAt),
		})
	}
	writeJSON(w, http.StatusOK, out)
}

func businessAccountFromListRow(row db.ListBusinessAccountsForOwnerRow) BusinessAccountResponse {
	return BusinessAccountResponse{
		ID:                          uuidToString(row.ID),
		Name:                        row.Name,
		OwnerUserID:                 uuidToString(row.OwnerUserID),
		Currency:                    row.Currency,
		Timezone:                    row.Timezone,
		MonthlyOwnerIncomeTargetRUB: row.MonthlyOwnerIncomeTargetRub,
		Role:                        row.Role,
		CreatedAt:                   timestampToString(row.CreatedAt),
		UpdatedAt:                   timestampToString(row.UpdatedAt),
	}
}
