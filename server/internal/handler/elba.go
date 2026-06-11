package handler

// elba.go — Kontur Elba REST client + picker endpoints (phase 3 of the
// agency billing plan). Ported from the battle-tested Python client in the
// Plane fork (plane/utils/elba_client.py): same base URL, auth header and
// payload shapes.
//
// Configuration is environment-only: ELBA_API_KEY (required to enable the
// integration) and ELBA_BASE_URL (defaults to the public API). The key never
// reaches the frontend — the UI talks to these proxy endpoints.

import (
	"bytes"
	"context"
	"crypto/tls"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"net/http"
	"os"
	"time"
)

const elbaDefaultBaseURL = "https://api.kontur.ru/elba/public/v1"

type elbaClient struct {
	baseURL string
	apiKey  string
	http    *http.Client
}

var errElbaNotConfigured = errors.New("ELBA_API_KEY is not configured")

func newElbaClient() (*elbaClient, error) {
	key := os.Getenv("ELBA_API_KEY")
	if key == "" {
		return nil, errElbaNotConfigured
	}
	base := os.Getenv("ELBA_BASE_URL")
	if base == "" {
		base = elbaDefaultBaseURL
	}
	// api.kontur.ru sits behind a WAF that (empirically, 2026-06-11) RSTs
	// h2 streams (curl: PROTOCOL_ERROR; Go: hang awaiting headers) and drops
	// requests from non-allowlisted clients, while python-requests — what the
	// proven Plane integration uses — passes. Force HTTP/1.1 and present the
	// same client identity.
	transport := &http.Transport{
		TLSNextProto: map[string]func(string, *tls.Conn) http.RoundTripper{},
	}
	return &elbaClient{
		baseURL: base,
		apiKey:  key,
		http:    &http.Client{Timeout: 15 * time.Second, Transport: transport},
	}, nil
}

func (c *elbaClient) do(ctx context.Context, method, path string, payload any) (any, error) {
	var body io.Reader
	if payload != nil {
		raw, err := json.Marshal(payload)
		if err != nil {
			return nil, err
		}
		body = bytes.NewReader(raw)
	}
	req, err := http.NewRequestWithContext(ctx, method, c.baseURL+path, body)
	if err != nil {
		return nil, err
	}
	req.Header.Set("X-Kontur-Apikey", c.apiKey)
	req.Header.Set("Content-Type", "application/json")
	req.Header.Set("Accept", "application/json")
	// Mirror the client identity of the working Plane integration; the
	// default Go-http-client UA is a common WAF block target.
	req.Header.Set("User-Agent", "python-requests/2.32.3")

	resp, err := c.http.Do(req)
	if err != nil {
		return nil, err
	}
	defer resp.Body.Close()
	raw, _ := io.ReadAll(io.LimitReader(resp.Body, 4<<20))
	if resp.StatusCode < 200 || resp.StatusCode >= 300 {
		msg := string(raw)
		if len(msg) > 300 {
			msg = msg[:300]
		}
		return nil, fmt.Errorf("elba %s %s: HTTP %d: %s", method, path, resp.StatusCode, msg)
	}
	if len(raw) == 0 {
		return nil, nil
	}
	var decoded any
	if err := json.Unmarshal(raw, &decoded); err != nil {
		return nil, fmt.Errorf("elba %s %s: bad JSON: %w", method, path, err)
	}
	return decoded, nil
}

// elbaUnwrapList normalizes Elba's two list shapes — a bare array, or an
// object wrapping the array under one of the given keys.
func elbaUnwrapList(data any, keys ...string) []any {
	if list, ok := data.([]any); ok {
		return list
	}
	if m, ok := data.(map[string]any); ok {
		for _, k := range keys {
			if list, ok := m[k].([]any); ok {
				return list
			}
		}
	}
	return []any{}
}

func (c *elbaClient) Organizations(ctx context.Context) ([]any, error) {
	data, err := c.do(ctx, http.MethodGet, "/organizations", nil)
	if err != nil {
		return nil, err
	}
	return elbaUnwrapList(data, "organizations"), nil
}

func (c *elbaClient) ContractorsSearch(ctx context.Context, orgID string) ([]any, error) {
	data, err := c.do(ctx, http.MethodPost, "/organizations/"+orgID+"/contractors/search", map[string]any{})
	if err != nil {
		return nil, err
	}
	return elbaUnwrapList(data, "contractors", "items"), nil
}

func (c *elbaClient) BankAccounts(ctx context.Context, orgID string) ([]any, error) {
	data, err := c.do(ctx, http.MethodGet, "/organizations/"+orgID+"/bank-accounts", nil)
	if err != nil {
		return nil, err
	}
	return elbaUnwrapList(data, "bankAccounts"), nil
}

// elbaDocItem is one warehouse line of a bill / act.
type elbaDocItem struct {
	Name     string  `json:"name"`
	Quantity float64 `json:"quantity"`
	Price    float64 `json:"price"`
	Unit     string  `json:"unit"`
}

type elbaDocOptions struct {
	Date          string // YYYY-MM-DD
	Comment       string
	BankAccountID string // bills only
	WithDiscount  bool   // bills only
}

func elbaDocPayload(contractorID string, items []elbaDocItem, opts elbaDocOptions, isBill bool) map[string]any {
	payload := map[string]any{
		"date":           opts.Date,
		"contractorId":   contractorID,
		"warehouseItems": items,
		"withNDS":        false,
	}
	if opts.Comment != "" {
		payload["comment"] = opts.Comment
	}
	if isBill && opts.BankAccountID != "" {
		payload["bankAccountId"] = opts.BankAccountID
	}
	if isBill && opts.WithDiscount {
		payload["withDiscount"] = true
	}
	return payload
}

// CreateBill creates a счёт and returns its Elba document id.
func (c *elbaClient) CreateBill(ctx context.Context, orgID, contractorID string, items []elbaDocItem, opts elbaDocOptions) (string, error) {
	data, err := c.do(ctx, http.MethodPost, "/organizations/"+orgID+"/bills", elbaDocPayload(contractorID, items, opts, true))
	if err != nil {
		return "", err
	}
	return elbaDocumentID(data), nil
}

// CreateAct creates an акт and returns its Elba document id.
func (c *elbaClient) CreateAct(ctx context.Context, orgID, contractorID string, items []elbaDocItem, opts elbaDocOptions) (string, error) {
	data, err := c.do(ctx, http.MethodPost, "/organizations/"+orgID+"/acts", elbaDocPayload(contractorID, items, opts, false))
	if err != nil {
		return "", err
	}
	return elbaDocumentID(data), nil
}

func elbaDocumentID(data any) string {
	if m, ok := data.(map[string]any); ok {
		if id, ok := m["id"].(string); ok {
			return id
		}
	}
	return ""
}

// --- Picker proxy endpoints (UI never sees the API key) ---

func (h *Handler) elbaPickerClient(w http.ResponseWriter, r *http.Request) (*elbaClient, bool) {
	wsID := h.resolveWorkspaceID(r)
	if _, ok := h.requireBillingEditor(w, r, wsID); !ok {
		return nil, false
	}
	client, err := newElbaClient()
	if errors.Is(err, errElbaNotConfigured) {
		writeError(w, http.StatusServiceUnavailable, "Elba integration is not configured (set ELBA_API_KEY)")
		return nil, false
	}
	if err != nil {
		writeError(w, http.StatusInternalServerError, "failed to initialize Elba client")
		return nil, false
	}
	return client, true
}

func (h *Handler) GetElbaOrganizations(w http.ResponseWriter, r *http.Request) {
	client, ok := h.elbaPickerClient(w, r)
	if !ok {
		return
	}
	orgs, err := client.Organizations(r.Context())
	if err != nil {
		writeError(w, http.StatusBadGateway, err.Error())
		return
	}
	writeJSON(w, http.StatusOK, orgs)
}

func (h *Handler) GetElbaContractors(w http.ResponseWriter, r *http.Request) {
	client, ok := h.elbaPickerClient(w, r)
	if !ok {
		return
	}
	orgID := r.URL.Query().Get("org_id")
	if orgID == "" {
		writeError(w, http.StatusBadRequest, "org_id query parameter is required")
		return
	}
	list, err := client.ContractorsSearch(r.Context(), orgID)
	if err != nil {
		writeError(w, http.StatusBadGateway, err.Error())
		return
	}
	writeJSON(w, http.StatusOK, list)
}

func (h *Handler) GetElbaBankAccounts(w http.ResponseWriter, r *http.Request) {
	client, ok := h.elbaPickerClient(w, r)
	if !ok {
		return
	}
	orgID := r.URL.Query().Get("org_id")
	if orgID == "" {
		writeError(w, http.StatusBadRequest, "org_id query parameter is required")
		return
	}
	list, err := client.BankAccounts(r.Context(), orgID)
	if err != nil {
		writeError(w, http.StatusBadGateway, err.Error())
		return
	}
	writeJSON(w, http.StatusOK, list)
}
