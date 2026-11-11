package api

import (
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"github.com/hyperme-sh/hypermesh-cli/internal/localtransport"
	"io"
	"net"
	"net/http"
	"strings"
	"time"
)

const HeaderAPIKey = "X-Api-Key"
const HeaderTenantID = "X-Tenant-ID"
const HeaderLeaseID = "X-Hypermesh-Lease-Id"
const HeaderLeaseIDAlt = "X-Lease-Id"

type Client struct {
	HTTP           *http.Client
	APIBase        string
	ConductorBase  string
	APIKey         string
	TenantID       string
	ConductorClass string
	// ConductorSuggest is optional. Conductor rejects a suggestion above the policy tier.
	ConductorSuggest string
	// ConductorToken is X-Conductor-Token. It is read from the environment
	// and is not stored in config.toml.
	ConductorToken  string
	ConductorSocket string
}

func NewClient(apiBase, conductorBase, apiKey, tenantID string) *Client {
	return &Client{
		HTTP:          &http.Client{Timeout: 30 * time.Second},
		APIBase:       apiBase,
		ConductorBase: conductorBase,
		APIKey:        apiKey,
		TenantID:      tenantID,
	}
}

type HTTPError struct {
	Method string
	URL    string
	Status int
	Body   string
}

func (e *HTTPError) Error() string {
	msg := strings.TrimSpace(e.Body)
	if len(msg) > 300 {
		msg = msg[:300] + "…"
	}
	if msg == "" {
		return fmt.Sprintf("%s %s: HTTP %d", e.Method, e.URL, e.Status)
	}
	return fmt.Sprintf("%s %s: HTTP %d: %s", e.Method, e.URL, e.Status, msg)
}

func (c *Client) requireRenterAuth() error {
	if err := ValidateRenterKey(c.APIKey); err != nil {
		return err
	}
	if strings.TrimSpace(c.TenantID) == "" {
		return fmt.Errorf("tenant id is required (X-Tenant-ID)")
	}
	return nil
}

func (c *Client) applyRenterHeaders(req *http.Request) {
	if c.APIKey != "" {
		req.Header.Set(HeaderAPIKey, c.APIKey)
	}
	if c.TenantID != "" {
		req.Header.Set(HeaderTenantID, c.TenantID)
	}
}

func (c *Client) doJSON(method, rawURL string, body any, auth bool) (json.RawMessage, error) {
	var rdr io.Reader
	if body != nil {
		b, err := json.Marshal(body)
		if err != nil {
			return nil, err
		}
		rdr = bytes.NewReader(b)
	}
	req, err := http.NewRequest(method, rawURL, rdr)
	if err != nil {
		return nil, err
	}
	if body != nil {
		req.Header.Set("Content-Type", "application/json")
	}
	req.Header.Set("Accept", "application/json")
	if auth {
		if err := c.requireRenterAuth(); err != nil {
			return nil, err
		}
	}
	c.applyRenterHeaders(req)
	resp, err := c.HTTP.Do(req)
	if err != nil {
		return nil, err
	}
	defer resp.Body.Close()
	raw, err := io.ReadAll(io.LimitReader(resp.Body, 1<<20))
	if err != nil {
		return nil, err
	}
	if resp.StatusCode < 200 || resp.StatusCode >= 300 {
		return nil, &HTTPError{Method: method, URL: rawURL, Status: resp.StatusCode, Body: string(raw)}
	}
	if len(bytes.TrimSpace(raw)) == 0 {
		return json.RawMessage("null"), nil
	}
	if !json.Valid(raw) {
		return nil, fmt.Errorf("%s %s: response is not JSON", method, rawURL)
	}
	return json.RawMessage(raw), nil
}

func (c *Client) GetCatalog() (json.RawMessage, error) {
	u, err := CatalogURL(c.APIBase)
	if err != nil {
		return nil, err
	}
	return c.doJSON(http.MethodGet, u, nil, false)
}

func (c *Client) GetClasses() (json.RawMessage, error) {
	u, err := ClassesURL(c.APIBase)
	if err != nil {
		return nil, err
	}
	return c.doJSON(http.MethodGet, u, nil, false)
}

func (c *Client) GetRenterHosts(catalogID string) (json.RawMessage, error) {
	u, err := RenterHostsURL(c.APIBase, catalogID)
	if err != nil {
		return nil, err
	}
	return c.doJSON(http.MethodGet, u, nil, true)
}

func (c *Client) CreateLease(body LeaseCreate) (Lease, json.RawMessage, error) {
	if err := body.Validate(); err != nil {
		return Lease{}, nil, err
	}
	u, err := LeasesURL(c.APIBase)
	if err != nil {
		return Lease{}, nil, err
	}
	raw, err := c.doJSON(http.MethodPost, u, body, true)
	if err != nil {
		return Lease{}, nil, err
	}
	var lease Lease
	if err := json.Unmarshal(raw, &lease); err != nil {
		return Lease{}, raw, fmt.Errorf("decode lease: %w", err)
	}
	return lease, raw, nil
}

func (c *Client) ListLeases() (json.RawMessage, error) {
	u, err := LeasesURL(c.APIBase)
	if err != nil {
		return nil, err
	}
	return c.doJSON(http.MethodGet, u, nil, true)
}

func (c *Client) GetLease(id string) (Lease, json.RawMessage, error) {
	u, err := LeaseURL(c.APIBase, id)
	if err != nil {
		return Lease{}, nil, err
	}
	raw, err := c.doJSON(http.MethodGet, u, nil, true)
	if err != nil {
		return Lease{}, nil, err
	}
	var lease Lease
	if err := json.Unmarshal(raw, &lease); err != nil {
		return Lease{}, raw, fmt.Errorf("decode lease: %w", err)
	}
	return lease, raw, nil
}

func (c *Client) CompleteLease(id string) (json.RawMessage, error) {
	u, err := LeaseCompleteURL(c.APIBase, id)
	if err != nil {
		return nil, err
	}
	return c.doJSON(http.MethodPost, u, map[string]string{}, true)
}

type ChatMessage struct {
	Role    string `json:"role"`
	Content string `json:"content"`
}

type ChatRequest struct {
	Model    string        `json:"model"`
	Messages []ChatMessage `json:"messages"`
	LeaseID  string        `json:"lease_id,omitempty"`
	// Class and Suggest are conductor headers, not JSON fields. Conductor
	// strips a conductor_ prefix before forwarding, but the headers are the
	// documented client surface (internal/proxy/proxy.go identity).
	Class   string `json:"-"`
	Suggest string `json:"-"`
}

func (c *Client) ChatCompletions(leaseID string, reqBody ChatRequest) (json.RawMessage, error) {
	if err := c.requireRenterAuth(); err != nil {
		return nil, err
	}
	if strings.TrimSpace(leaseID) == "" && strings.TrimSpace(reqBody.LeaseID) == "" {
		return nil, fmt.Errorf("lease id is required for conductor chat")
	}
	if reqBody.LeaseID == "" {
		reqBody.LeaseID = leaseID
	}
	if strings.TrimSpace(reqBody.Model) == "" {
		reqBody.Model = DefaultCatalogID
	}
	if len(reqBody.Messages) == 0 {
		return nil, fmt.Errorf("messages are required")
	}
	u, err := ChatCompletionsURL(c.ConductorBase)
	if err != nil {
		return nil, err
	}
	b, err := json.Marshal(reqBody)
	if err != nil {
		return nil, err
	}
	req, err := http.NewRequest(http.MethodPost, u, bytes.NewReader(b))
	if err != nil {
		return nil, err
	}
	chatClient := *c.HTTP
	chatClient.CheckRedirect = func(_ *http.Request, _ []*http.Request) error { return http.ErrUseLastResponse }
	if req.URL.Scheme != "https" {
		if c.ConductorBase != DefaultConductorBase && c.ConductorSocket == "" {
			return nil, fmt.Errorf("Conductor chat requires a protected local socket or verified HTTPS")
		}
		path := c.ConductorSocket
		if path == "" {
			path, err = localtransport.DefaultPath()
			if err != nil {
				return nil, err
			}
		}
		transport := &http.Transport{DialContext: func(ctx context.Context, _, _ string) (net.Conn, error) { return localtransport.DialContext(ctx, path) }}
		defer transport.CloseIdleConnections()
		chatClient.Transport = transport
	}
	req.Header.Set("Content-Type", "application/json")
	req.Header.Set("Accept", "application/json")
	// Conductor openai passthrough reads Authorization: Bearer and forwards
	// that header. It does not read X-Api-Key. The gateway accepts an API key
	// in either header, and rejects the request when both are present
	// (hypermesh-gateway renter_auth.py _reject_ambiguous_credentials).
	req.Header.Set("Authorization", "Bearer "+strings.TrimSpace(c.APIKey))
	class := strings.TrimSpace(reqBody.Class)
	if class == "" {
		class = strings.TrimSpace(c.ConductorClass)
	}
	if class != "" {
		req.Header.Set(HeaderConductorClass, class)
	}
	suggest := strings.TrimSpace(reqBody.Suggest)
	if suggest == "" {
		suggest = strings.TrimSpace(c.ConductorSuggest)
	}
	if suggest != "" {
		req.Header.Set(HeaderConductorSuggest, suggest)
	}
	if task := strings.TrimSpace(reqBody.LeaseID); conductorTaskID(task) {
		req.Header.Set(HeaderConductorTask, task)
	}
	if token := strings.TrimSpace(c.ConductorToken); token != "" {
		req.Header.Set(HeaderConductorToken, token)
	}
	resp, err := chatClient.Do(req)
	if err != nil {
		return nil, err
	}
	defer resp.Body.Close()
	raw, err := io.ReadAll(io.LimitReader(resp.Body, 1<<20))
	if err != nil {
		return nil, err
	}
	if resp.StatusCode < 200 || resp.StatusCode >= 300 {
		// Do not echo the prompt. Surface status and a short API error only.
		return nil, &HTTPError{Method: http.MethodPost, URL: u, Status: resp.StatusCode, Body: chatErrorHint(raw)}
	}
	if !json.Valid(raw) {
		return nil, fmt.Errorf("chat completions: response is not JSON")
	}
	return json.RawMessage(raw), nil
}

func chatErrorHint(raw []byte) string {
	var env struct {
		Error struct {
			Message string `json:"message"`
			Type    string `json:"type"`
		} `json:"error"`
		Detail string `json:"detail"`
	}
	if json.Unmarshal(raw, &env) == nil {
		if env.Error.Message != "" {
			return env.Error.Message
		}
		if env.Detail != "" {
			return env.Detail
		}
	}
	return "conductor rejected the request"
}

// conductorTaskID matches hypermesh-conductor internal/proxy/proxy.go idPattern.
func conductorTaskID(id string) bool {
	if id == "" || len(id) > 200 {
		return false
	}
	for i, r := range id {
		switch {
		case r >= 'A' && r <= 'Z', r >= 'a' && r <= 'z', r >= '0' && r <= '9':
		case i > 0 && (r == '.' || r == '_' || r == ':' || r == '-'):
		default:
			return false
		}
	}
	return true
}

func AssistantText(raw json.RawMessage) string {
	var env struct {
		Choices []struct {
			Message struct {
				Content string `json:"content"`
			} `json:"message"`
		} `json:"choices"`
	}
	if json.Unmarshal(raw, &env) != nil {
		return ""
	}
	if len(env.Choices) == 0 {
		return ""
	}
	return env.Choices[0].Message.Content
}

type visorPromptBody struct {
	Kind   string `json:"kind"`
	Prompt string `json:"prompt"`
	Model  string `json:"model,omitempty"`
}

// PostVisorPrompt posts one prompt to the open visor stream.
// The API key is X-Api-Key. Model is omitted when empty. Lease and tenant headers are not sent.
func (c *Client) PostVisorPrompt(visorBase, sessionID, prompt, model string) ([]byte, error) {
	if err := ValidateRenterKey(c.APIKey); err != nil {
		return nil, err
	}
	prompt = strings.TrimSpace(prompt)
	if prompt == "" {
		return nil, fmt.Errorf("prompt is required")
	}
	u, err := VisorStreamURL(visorBase, sessionID)
	if err != nil {
		return nil, err
	}
	payload := visorPromptBody{Kind: "prompt", Prompt: prompt, Model: strings.TrimSpace(model)}
	b, err := json.Marshal(payload)
	if err != nil {
		return nil, err
	}
	b = append(b, '\n')
	req, err := http.NewRequest(http.MethodPost, u, bytes.NewReader(b))
	if err != nil {
		return nil, err
	}
	req.Header.Set("Content-Type", "application/x-ndjson")
	req.Header.Set("Accept", "application/x-ndjson")
	req.Header.Set(HeaderAPIKey, strings.TrimSpace(c.APIKey))
	httpClient := c.HTTP
	if httpClient == nil {
		httpClient = &http.Client{Timeout: 30 * time.Second}
	}
	local := *httpClient
	local.CheckRedirect = func(*http.Request, []*http.Request) error {
		return http.ErrUseLastResponse
	}
	resp, err := local.Do(req)
	if err != nil {
		return nil, fmt.Errorf("visor stream request failed")
	}
	defer resp.Body.Close()
	raw, err := io.ReadAll(io.LimitReader(resp.Body, 1<<20))
	if err != nil {
		return nil, fmt.Errorf("visor stream request failed")
	}
	text := RedactKey(string(raw), c.APIKey)
	if resp.StatusCode < 200 || resp.StatusCode >= 300 {
		return nil, fmt.Errorf("visor stream HTTP %d: %s", resp.StatusCode, strings.TrimSpace(text))
	}
	return []byte(text), nil
}
