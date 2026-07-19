package middleware

import (
	"context"
	"errors"
	"net/http"

	"github.com/go-chi/chi/v5"
	"github.com/jackc/pgx/v5"
	"github.com/multica-ai/multica/server/internal/util"
	db "github.com/multica-ai/multica/server/pkg/db/generated"
)

type businessContextKey int

const (
	ctxKeyBusinessID businessContextKey = iota
	ctxKeyBusinessMember
)

// BusinessIDFromContext returns the business account authorized by the
// business-role middleware. It is intentionally separate from workspace
// context: business membership never grants implicit workspace access.
func BusinessIDFromContext(ctx context.Context) string {
	id, _ := ctx.Value(ctxKeyBusinessID).(string)
	return id
}

// BusinessMemberFromContext returns the fresh, DB-backed business membership
// used for the current permission decision.
func BusinessMemberFromContext(ctx context.Context) (db.BusinessAccountMember, bool) {
	m, ok := ctx.Value(ctxKeyBusinessMember).(db.BusinessAccountMember)
	return m, ok
}

// RequireBusinessRoleFromURL authorizes a business-scoped route from a chi URL
// parameter. The membership is queried on every request; the workspace
// membership cache and owner_user_id are never authorization sources.
func RequireBusinessRoleFromURL(queries *db.Queries, param string, roles ...string) func(http.Handler) http.Handler {
	return func(next http.Handler) http.Handler {
		return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
			businessID := chi.URLParam(r, param)
			businessUUID, err := util.ParseUUID(businessID)
			if err != nil {
				writeError(w, http.StatusBadRequest, "invalid business_id")
				return
			}

			userID := r.Header.Get("X-User-ID")
			userUUID, err := util.ParseUUID(userID)
			if err != nil {
				writeError(w, http.StatusUnauthorized, "user not authenticated")
				return
			}

			member, err := queries.GetBusinessAccountMember(r.Context(), db.GetBusinessAccountMemberParams{
				BusinessID: businessUUID,
				UserID:     userUUID,
			})
			if errors.Is(err, pgx.ErrNoRows) {
				// Hide whether the account exists from non-members.
				writeError(w, http.StatusNotFound, "business not found")
				return
			}
			if err != nil {
				writeError(w, http.StatusInternalServerError, "failed to authorize business access")
				return
			}

			allowed := false
			for _, role := range roles {
				if member.Role == role {
					allowed = true
					break
				}
			}
			if len(roles) > 0 && !allowed {
				writeError(w, http.StatusForbidden, "insufficient permissions")
				return
			}

			ctx := context.WithValue(r.Context(), ctxKeyBusinessID, businessID)
			ctx = context.WithValue(ctx, ctxKeyBusinessMember, member)
			next.ServeHTTP(w, r.WithContext(ctx))
		})
	}
}
