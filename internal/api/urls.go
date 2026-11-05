package api

import (
	"fmt"
	"net/url"
	"path"
	"strings"
)

const (
	DefaultAPIBase   = "https://api.test.hyperme.sh"
	DefaultChatBase  = "https://chat.test.hyperme.sh"
	DefaultVisorBase = "http://127.0.0.1:9847"

	// DefaultCatalogID seeds --catalog-id / --model when the flag is omitted.
	// It is not a catalog inventory; list ids with `hypermesh catalog`.
	DefaultCatalogID = "llama-3.1-8b-q4"

	// Control-plane paths. controlplane-edge forwards the /api/v1/hypermesh
	// prefix to hypermesh-plane (edge-routes.json, product_routes.json).
	// Route handlers: products/hypermesh/src/hypermesh_service/interfaces/api/routes.py
	PathCatalog     = "/api/v1/hypermesh/catalog"
	PathClasses     = "/api/v1/hypermesh/classes"
	PathRenterHosts = "/api/v1/hypermesh/renter/hosts"
	PathLeases      = "/api/v1/hypermesh/leases"

	// Chat is the gateway openai router, on the chat host, outside the
	// control-plane proxy. hypermesh-gateway routes.py openai_router
	// POST /v1/chat/completions, included with no prefix in app.py.
	PathChatCompletions = "/v1/chat/completions"

	// PathRenterChatStub is POST /api/v1/hypermesh/renter/chat/completions
	// on hypermesh-plane. That handler always returns 409. This client
	// never calls it.
	PathRenterChatStub = "/api/v1/hypermesh/renter/chat/completions"
)

func JoinBase(base, p string) (string, error) {
	if strings.TrimSpace(base) == "" {
		return "", fmt.Errorf("empty base URL")
	}
	u, err := url.Parse(strings.TrimRight(strings.TrimSpace(base), "/"))
	if err != nil {
		return "", fmt.Errorf("parse base URL: %w", err)
	}
	if u.Scheme == "" || u.Host == "" {
		return "", fmt.Errorf("base URL must include scheme and host")
	}
	u.Path = path.Join(u.Path, p)
	if !strings.HasPrefix(u.Path, "/") {
		u.Path = "/" + u.Path
	}
	return u.String(), nil
}

func CatalogURL(apiBase string) (string, error) {
	return JoinBase(apiBase, PathCatalog)
}

func ClassesURL(apiBase string) (string, error) {
	return JoinBase(apiBase, PathClasses)
}

// RenterHostsURL is GET /api/v1/hypermesh/renter/hosts.
// catalogID, when set, is the only query key this client sends (catalog_id).
func RenterHostsURL(apiBase, catalogID string) (string, error) {
	u, err := JoinBase(apiBase, PathRenterHosts)
	if err != nil {
		return "", err
	}
	catalogID = strings.TrimSpace(catalogID)
	if catalogID == "" {
		return u, nil
	}
	parsed, err := url.Parse(u)
	if err != nil {
		return "", err
	}
	q := url.Values{}
	q.Set("catalog_id", catalogID)
	parsed.RawQuery = q.Encode()
	return parsed.String(), nil
}

func LeasesURL(apiBase string) (string, error) {
	return JoinBase(apiBase, PathLeases)
}

func LeaseURL(apiBase, id string) (string, error) {
	if strings.TrimSpace(id) == "" {
		return "", fmt.Errorf("lease id is required")
	}
	return JoinBase(apiBase, PathLeases+"/"+url.PathEscape(id))
}

func LeaseCompleteURL(apiBase, id string) (string, error) {
	if strings.TrimSpace(id) == "" {
		return "", fmt.Errorf("lease id is required")
	}
	return JoinBase(apiBase, PathLeases+"/"+url.PathEscape(id)+"/complete")
}

// VisorStreamURL is POST /session/{id}/stream on the visor.
// The body is one NDJSON prompt. The API key is the X-Api-Key header, not this URL.
func VisorStreamURL(visorBase, sessionID string) (string, error) {
	sessionID = strings.TrimSpace(sessionID)
	if sessionID == "" || strings.Contains(sessionID, "/") || strings.Contains(sessionID, "..") {
		return "", fmt.Errorf("session id is required")
	}
	return JoinBase(visorBase, "/session/"+url.PathEscape(sessionID)+"/stream")
}

func ChatCompletionsURL(chatBase string) (string, error) {
	u, err := JoinBase(chatBase, PathChatCompletions)
	if err != nil {
		return "", err
	}
	if strings.Contains(u, PathRenterChatStub) {
		return "", fmt.Errorf("refusing renter chat stub path")
	}
	return u, nil
}
