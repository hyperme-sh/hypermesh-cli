package api

import (
	"encoding/json"
	"io"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
)

func TestValidateRenterKey(t *testing.T) {
	t.Parallel()
	if err := ValidateRenterKey("org_live_ok"); err != nil {
		t.Fatal(err)
	}
	for _, p := range ForbiddenRenterPrefixes {
		if err := ValidateRenterKey(p + "secret"); err == nil {
			t.Fatalf("accepted forbidden prefix %s", p)
		}
	}
}

func TestCreateLeaseHTTP(t *testing.T) {
	t.Parallel()
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.Method != http.MethodPost || r.URL.Path != PathLeases {
			t.Fatalf("unexpected %s %s", r.Method, r.URL.Path)
		}
		if r.Header.Get(HeaderAPIKey) != "org_key" || r.Header.Get(HeaderTenantID) != "ten" {
			t.Fatalf("missing renter headers")
		}
		raw, err := io.ReadAll(r.Body)
		if err != nil {
			t.Fatal(err)
		}
		if strings.Contains(string(raw), "success_url") || strings.Contains(string(raw), "cancel_url") || strings.Contains(string(raw), "checkout_url") {
			t.Fatalf("checkout URL fields in body: %s", raw)
		}
		var body LeaseCreate
		if err := json.Unmarshal(raw, &body); err != nil {
			t.Fatal(err)
		}
		if body.Kind != KindP2LoadedModel || body.CatalogID != DefaultCatalogID || body.Purpose != PurposeRenter {
			t.Fatalf("body %+v", body)
		}
		if body.DeviceID != "22222222-2222-2222-2222-222222222222" {
			t.Fatalf("device_id %+v", body)
		}
		_ = json.NewEncoder(w).Encode(Lease{ID: "lease_1", Status: "offered", BillingRecordID: "bill_1"})
	}))
	defer srv.Close()

	c := NewClient(srv.URL, DefaultConductorBase, "org_key", "ten")
	lease, _, err := c.CreateLease(NewLeaseCreate("u", DefaultCatalogID, 1, "22222222-2222-2222-2222-222222222222"))
	if err != nil {
		t.Fatal(err)
	}
	if lease.ID != "lease_1" || lease.Status != "offered" || lease.BillingRecordID != "bill_1" {
		t.Fatalf("%+v", lease)
	}
}

func TestChatCompletionsHTTP(t *testing.T) {
	t.Parallel()
	var sawStub bool
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Path == PathRenterChatStub {
			sawStub = true
			w.WriteHeader(http.StatusConflict)
			return
		}
		if r.Method != http.MethodPost || r.URL.Path != PathChatCompletions {
			t.Fatalf("unexpected %s %s", r.Method, r.URL.Path)
		}
		if r.Header.Get("Authorization") != "Bearer org_key" {
			t.Fatalf("authorization %q", r.Header.Get("Authorization"))
		}
		if r.Header.Get(HeaderAPIKey) != "" || r.Header.Get(HeaderLeaseID) != "" || r.Header.Get(HeaderLeaseIDAlt) != "" || r.Header.Get(HeaderTenantID) != "" {
			t.Fatal("conductor chat must not send plane or lease headers")
		}
		if r.Header.Get(HeaderConductorClass) != "code" {
			t.Fatalf("class %q", r.Header.Get(HeaderConductorClass))
		}
		if r.Header.Get(HeaderConductorTask) != "lease_1" {
			t.Fatalf("task %q", r.Header.Get(HeaderConductorTask))
		}
		if r.Header.Get(HeaderConductorToken) != "proxy-tok" {
			t.Fatalf("token %q", r.Header.Get(HeaderConductorToken))
		}
		raw, _ := io.ReadAll(r.Body)
		var body ChatRequest
		if err := json.Unmarshal(raw, &body); err != nil {
			t.Fatal(err)
		}
		if body.LeaseID != "lease_1" || body.Model != DefaultCatalogID || len(body.Messages) != 1 {
			t.Fatalf("body %+v", body)
		}
		_, _ = w.Write([]byte(`{"choices":[{"message":{"role":"assistant","content":"ok"}}]}`))
	}))
	defer srv.Close()

	c := NewClient(DefaultAPIBase, srv.URL, "org_key", "ten")
	c.ConductorClass = "code"
	c.ConductorToken = "proxy-tok"
	raw, err := c.ChatCompletions("lease_1", ChatRequest{
		Model:    DefaultCatalogID,
		Messages: []ChatMessage{{Role: "user", Content: "hi"}},
	})
	if err != nil {
		t.Fatal(err)
	}
	if AssistantText(raw) != "ok" {
		t.Fatalf("assistant %q", AssistantText(raw))
	}
	if sawStub {
		t.Fatal("hit renter chat stub")
	}
}

func TestChatRejectsForbiddenKey(t *testing.T) {
	t.Parallel()
	c := NewClient(DefaultAPIBase, DefaultConductorBase, "hm_rtr_nope", "ten")
	_, err := c.ChatCompletions("lease_1", ChatRequest{
		Model:    DefaultCatalogID,
		Messages: []ChatMessage{{Role: "user", Content: "hi"}},
	})
	if err == nil || !strings.Contains(err.Error(), "hm_rtr_") {
		t.Fatalf("got %v", err)
	}
}

func TestCreateLeaseMissingDeviceIDDoesNotPOST(t *testing.T) {
	t.Parallel()
	called := false
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		called = true
		w.WriteHeader(http.StatusOK)
	}))
	defer srv.Close()
	c := NewClient(srv.URL, DefaultConductorBase, "org_key", "ten")
	_, _, err := c.CreateLease(NewLeaseCreate("u", DefaultCatalogID, 1, ""))
	if err == nil {
		t.Fatal("expected device_id error")
	}
	if !strings.Contains(err.Error(), "device_id") {
		t.Fatalf("got %v", err)
	}
	if called {
		t.Fatal("posted without device_id")
	}
}

func TestGetRenterHosts(t *testing.T) {
	t.Parallel()
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.Method != http.MethodGet || r.URL.Path != PathRenterHosts {
			t.Fatalf("unexpected %s %s", r.Method, r.URL.Path)
		}
		if r.Header.Get(HeaderAPIKey) != "org_key" || r.Header.Get(HeaderTenantID) != "ten" {
			t.Fatal("missing renter headers")
		}
		if r.URL.RawQuery != "catalog_id="+DefaultCatalogID {
			t.Fatalf("query %q", r.URL.RawQuery)
		}
		if _, ok := r.URL.Query()["view"]; ok {
			t.Fatal("invented query key view")
		}
		_, _ = w.Write([]byte(`[{"device_id":"22222222-2222-2222-2222-222222222222","public_label":"agx-large 22222222","class_id":"agx-large","certified":true,"online":true,"sell_state":"selling"}]`))
	}))
	defer srv.Close()
	c := NewClient(srv.URL, DefaultConductorBase, "org_key", "ten")
	raw, err := c.GetRenterHosts(DefaultCatalogID)
	if err != nil {
		t.Fatal(err)
	}
	if !strings.Contains(string(raw), "22222222-2222-2222-2222-222222222222") {
		t.Fatalf("%s", raw)
	}
}

func TestGetCatalogPublic(t *testing.T) {
	t.Parallel()
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Path != PathCatalog {
			t.Fatalf("path %s", r.URL.Path)
		}
		_, _ = w.Write([]byte(`[{"id":"llama-3.1-8b-q4"}]`))
	}))
	defer srv.Close()
	c := NewClient(srv.URL, DefaultConductorBase, "", "")
	raw, err := c.GetCatalog()
	if err != nil {
		t.Fatal(err)
	}
	if !strings.Contains(string(raw), DefaultCatalogID) {
		t.Fatalf("%s", raw)
	}
}

func TestChatErrorDoesNotEchoPrompt(t *testing.T) {
	t.Parallel()
	secret := "UNIQUE_PROMPT_BODY_SHOULD_NOT_LEAK"
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(http.StatusPaymentRequired)
		_, _ = w.Write([]byte(`{"error":{"message":"lease not active"}}`))
	}))
	defer srv.Close()
	c := NewClient(DefaultAPIBase, srv.URL, "org_key", "ten")
	_, err := c.ChatCompletions("lease_1", ChatRequest{
		Model:    DefaultCatalogID,
		Messages: []ChatMessage{{Role: "user", Content: secret}},
	})
	if err == nil {
		t.Fatal("expected error")
	}
	if strings.Contains(err.Error(), secret) {
		t.Fatalf("prompt leaked in error: %v", err)
	}
	if !strings.Contains(err.Error(), "lease not active") {
		t.Fatalf("wanted API hint, got %v", err)
	}
}
