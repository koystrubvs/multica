package handler

// elba.go — Kontur Elba REST client + picker endpoints (phase 3 of the
// agency billing plan). Ported from the Plane fork's working client
// (plane/utils/elba.py): same host, auth header and payload shapes.
//
// Host note: the real Elba API is `elba-api.kontur.ru/v1` — NOT the generic
// `api.kontur.ru` gateway (that host's edge resets every non-allowlisted
// client; an earlier wrong URL there sent us down a multi-hour "WAF/IP-ban"
// rabbit hole — see AGENCY_BILLING_SPEC.md). Plane talks to elba-api directly
// with a plain client; so do we (no proxy/sidecar needed).
//
// Configuration is environment-only: ELBA_API_KEY (required to enable the
// integration) and ELBA_BASE_URL (defaults to the correct Elba host). The key
// never reaches the frontend — the UI talks to these proxy endpoints.

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"log/slog"
	"net/http"
	"os"
	"strconv"
	"strings"
	"time"
)

const elbaDefaultBaseURL = "https://elba-api.kontur.ru/v1"

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
	return &elbaClient{
		baseURL: base,
		apiKey:  key,
		http:    &http.Client{Timeout: 15 * time.Second},
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

// elbaDocItem is one warehouse line of a bill / act, in the elba-api.kontur.ru
// v1 document shape: productName / unitName / quantity / price (>= 0) plus an
// optional per-line percent Discount (max 2 decimals). The old contract's
// negative-price discount line is NOT accepted by v1 — capping a subscription
// bill is expressed purely through per-line Discount (see
// distributeSubscriptionDiscounts). Reference: /tmp/elba_invoice.py (bill #101).
type elbaDocItem struct {
	ProductName string  `json:"productName"`
	Quantity    float64 `json:"quantity"`
	Price       float64 `json:"price"`
	UnitName    string  `json:"unitName"`
	// Percent, 0..100, 2 decimals. Serialized always (0 = no discount on the
	// line); the document-level withDiscount flag gates whether Elba applies it.
	Discount float64 `json:"discount"`
}

type elbaDocOptions struct {
	Date          string // YYYY-MM-DD
	Comment       string
	BankAccountID string // bills only
	WithDiscount  bool   // any line carries a discount
}

func elbaDocPayload(contractorID string, items []elbaDocItem, opts elbaDocOptions, isBill bool) map[string]any {
	payload := map[string]any{
		"date":           opts.Date,
		"contractorId":   contractorID,
		"warehouseItems": items,
		"withNDS":        false,
		// v1 requires ndsRate to be null when withNDS is false.
		"ndsRate": nil,
	}
	if opts.Comment != "" {
		payload["comment"] = opts.Comment
	}
	if isBill && opts.BankAccountID != "" {
		payload["bankAccountId"] = opts.BankAccountID
	}
	// withDiscount applies to both bills and acts under v1 (the reference sends
	// it on both so the act shows the same capped total).
	if opts.WithDiscount {
		payload["withDiscount"] = true
	}
	return payload
}

// elbaDocument is what we keep from a created document. The number matters as
// much as the id: payers name it in the payment purpose ("Оплата по счету № 93
// от 30 июня 2026"), so it is the only field of ours that ever comes back in a
// bank statement. The id is a UUID and never does.
type elbaDocument struct {
	ID     string
	Number string
	Date   string // YYYY-MM-DD, as Elba issued it
}

// CreateBill creates a счёт and returns its id, number and issue date.
func (c *elbaClient) CreateBill(ctx context.Context, orgID, contractorID string, items []elbaDocItem, opts elbaDocOptions) (elbaDocument, error) {
	data, err := c.do(ctx, http.MethodPost, "/organizations/"+orgID+"/bills", elbaDocPayload(contractorID, items, opts, true))
	if err != nil {
		return elbaDocument{}, err
	}
	doc := parseElbaDocument(data)
	// v1 answers the create call with the stored document, but the contract
	// does not promise the number, and a bill recorded without one can never be
	// recognised in a bank statement. One extra read is cheaper than that.
	if doc.ID != "" && doc.Number == "" {
		if fetched, ferr := c.GetBill(ctx, orgID, doc.ID); ferr == nil {
			doc.Number = fetched.Number
			if fetched.Date != "" {
				doc.Date = fetched.Date
			}
		} else {
			slog.Warn("elba: bill created without a number and re-read failed",
				"bill_id", doc.ID, "error", ferr)
		}
	}
	if doc.Date == "" {
		doc.Date = opts.Date
	}
	return doc, nil
}

// CreateAct creates an акт and returns its Elba document id. Acts carry a
// number too, but nobody pays against an act, so it is not recorded.
func (c *elbaClient) CreateAct(ctx context.Context, orgID, contractorID string, items []elbaDocItem, opts elbaDocOptions) (string, error) {
	data, err := c.do(ctx, http.MethodPost, "/organizations/"+orgID+"/acts", elbaDocPayload(contractorID, items, opts, false))
	if err != nil {
		return "", err
	}
	return parseElbaDocument(data).ID, nil
}

// GetBill reads one счёт back, which is how a bill created without a number in
// the create response gets one.
func (c *elbaClient) GetBill(ctx context.Context, orgID, billID string) (elbaDocument, error) {
	data, err := c.do(ctx, http.MethodGet, "/organizations/"+orgID+"/bills/"+billID, nil)
	if err != nil {
		return elbaDocument{}, err
	}
	return parseElbaDocument(data), nil
}

func parseElbaDocument(data any) elbaDocument {
	m, ok := data.(map[string]any)
	if !ok {
		return elbaDocument{}
	}
	doc := elbaDocument{}
	if id, ok := m["id"].(string); ok {
		doc.ID = id
	}
	doc.Number = elbaScalarString(m["number"])
	doc.Date = elbaScalarString(m["date"])
	if len(doc.Date) > 10 {
		// Tolerate a timestamp where a date is documented.
		doc.Date = doc.Date[:10]
	}
	return doc
}

// elbaScalarString reads a field that the API documents as a string but may
// send as a number (invoice numbers are digits).
func elbaScalarString(value any) string {
	switch v := value.(type) {
	case string:
		return strings.TrimSpace(v)
	case float64:
		return strconv.FormatFloat(v, 'f', -1, 64)
	case json.Number:
		return v.String()
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
