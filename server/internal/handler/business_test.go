package handler

import (
	"net/http"
	"net/http/httptest"
	"testing"

	"github.com/multica-ai/multica/server/internal/featureflags"
	"github.com/multica-ai/multica/server/pkg/featureflag"
)

func TestRequireBusinessControlPlaneDefaultsToNotFound(t *testing.T) {
	called := false
	next := http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		called = true
		w.WriteHeader(http.StatusNoContent)
	})

	rec := httptest.NewRecorder()
	RequireBusinessControlPlane(nil)(next).ServeHTTP(rec, httptest.NewRequest(http.MethodGet, "/api/businesses", nil))
	if rec.Code != http.StatusNotFound {
		t.Fatalf("status = %d, want %d", rec.Code, http.StatusNotFound)
	}
	if called {
		t.Fatal("disabled business control plane called downstream handler")
	}
}

func TestRequireBusinessControlPlaneAllowsEnabledFlag(t *testing.T) {
	provider := featureflag.NewStaticProvider()
	provider.Set(featureflags.BusinessControlPlane, featureflag.Rule{Default: true})
	flags := featureflag.NewService(provider)

	rec := httptest.NewRecorder()
	RequireBusinessControlPlane(flags)(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(http.StatusNoContent)
	})).ServeHTTP(rec, httptest.NewRequest(http.MethodGet, "/api/businesses", nil))
	if rec.Code != http.StatusNoContent {
		t.Fatalf("status = %d, want %d", rec.Code, http.StatusNoContent)
	}
}
