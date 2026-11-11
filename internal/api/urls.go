package api

import (
	"fmt"
	"net/url"
	"path"
	"strings"
)

const (
	DefaultAPIBase = "https://api.test.hyperme.sh"

	// DefaultConductorBase is the loopback origin conductor serve binds.
	// hypermesh-conductor internal/config/config.go DefaultListen is
	// 127.0.0.1:8787, and examples/endpoints.json sets the same listen.
	// ValidateListen refuses any non-loopback address. controlplane-edge
	// src/controlplane_edge/product_routes.json has no conductor product
	// and no conductor hostname.
	DefaultConductorBase = "http://127.0.0.1:8787"

	DefaultVisorBase = "http://127.0.0.1:9847"

	// DefaultCatalogID seeds --catalog-id / --model when the flag is omitted.
	// It is not a catalog inventory; list ids with `hypermesh catalog`.
	// Conductor replaces this value with the tier model before the destination
	// sees the body (internal/proxy/proxy.go forceModel, internal/translate
	// setModelPreserve).
	DefaultCatalogID = "llama-3.1-8b-q4"

	// Control-plane paths. controlplane-edge forwards the /api/v1/hypermesh
	// prefix to hypermesh-plane (edge-routes.json, product_routes.json).
	// Route handlers: products/hypermesh/src/hypermesh_service/interfaces/api/routes.py
	PathCatalog     = "/api/v1/hypermesh/catalog"
	PathClasses     = "/api/v1/hypermesh/classes"
	PathRenterHosts = "/api/v1/hypermesh/renter/hosts"
	PathLeases      = "/api/v1/hypermesh/leases"

	// Conductor HTTP surface. internal/proxy/proxy.go protocolOf.
	// Same-protocol bodies are forwarded with the model replaced.
	PathChatCompletions    = "/v1/chat/completions"
	PathConductorResponses = "/v1/responses"
	PathConductorMessages  = "/v1/messages"
	PathConductorEmbeds    = "/v1/embeddings"
	PathConductorModels    = "/v1/models"

	// Headers conductor's proxy reads. internal/proxy/proxy.go identity,
	// authorizeProxy, and clientCredential. X-Api-Key, X-Tenant-ID, and the
	// lease headers are not forwarded.
	HeaderConductorClass   = "X-Conductor-Class"
	HeaderConductorSuggest = "X-Conductor-Suggest"
	HeaderConductorTask    = "X-Conductor-Task"
	HeaderConductorToken   = "X-Conductor-Token"

	// EnvConductorProxyToken is the conductor process secret. The proxy
	// compares X-Conductor-Token to it. This CLI forwards the variable when
	// it is set and does not write it into config.toml.
	EnvConductorProxyToken = "CONDUCTOR_PROXY_TOKEN"

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
	u.RawQuery = ""
	u.Fragment = ""
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

// ConductorOrigin is the conductor listen origin. A trailing /v1 is removed
// so callers can append /v1/chat/completions or /v1/messages once.
func ConductorOrigin(conductorBase string) (string, error) {
	u, err := url.Parse(strings.TrimRight(strings.TrimSpace(conductorBase), "/"))
	if err != nil {
		return "", fmt.Errorf("parse conductor base URL: %w", err)
	}
	if (u.Scheme != "http" && u.Scheme != "https") || u.Host == "" || u.User != nil {
		return "", fmt.Errorf("conductor base URL must be http or https with a host and no userinfo")
	}
	u.RawQuery = ""
	u.Fragment = ""
	u.Path = strings.TrimRight(u.Path, "/")
	u.Path = strings.TrimSuffix(u.Path, "/v1")
	u.Path = strings.TrimRight(u.Path, "/")
	return u.String(), nil
}

// OpenAIRoot is the API root OpenAI-compatible clients append
// /chat/completions, /responses, or /embeddings to. Conductor serves those
// under /v1 (internal/proxy/proxy.go protocolOf).
func OpenAIRoot(conductorBase string) (string, error) {
	origin, err := ConductorOrigin(conductorBase)
	if err != nil {
		return "", err
	}
	return origin + "/v1", nil
}

// ChatCompletionsURL is POST {conductor origin}/v1/chat/completions.
func ChatCompletionsURL(conductorBase string) (string, error) {
	origin, err := ConductorOrigin(conductorBase)
	if err != nil {
		return "", err
	}
	u, err := JoinBase(origin, PathChatCompletions)
	if err != nil {
		return "", err
	}
	if strings.Contains(u, PathRenterChatStub) {
		return "", fmt.Errorf("refusing renter chat stub path")
	}
	return u, nil
}
