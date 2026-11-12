package route

import (
	"fmt"
	"net/url"
	"os"
	"path/filepath"
	"sort"
	"strings"

	"github.com/pelletier/go-toml/v2"
	"gopkg.in/yaml.v3"
)

func captureRoot(t target) string { return filepath.Join(t.ConfigDir, "conductor", "captures") }

// enableCaptured includes the provider profile in the same backup/restore set
// as the tool configuration. Secret values never enter the provider profile.
func enableCaptured(t target, id string, row Finding, dry bool) (Result, error) {
	paths := editPaths(t, id, row)
	planned := map[string][]byte{}
	logical := map[string]string{}
	profilePath := filepath.Join(captureRoot(t), id+".json")
	var profile []byte
	for _, path := range paths {
		if err := underHome(t.Home, path); err != nil {
			return Result{}, err
		}
		resolved, err := canonicalPath(path)
		if err != nil {
			return Result{}, err
		}
		source := []byte(readFile(resolved))
		// Re-enable and migration always start from the saved original provider.
		if snap, ok, err := readSnap(t, resolved); err != nil {
			return Result{}, err
		} else if ok {
			source = snap.Body
		}
		next, p, err := capturedEdit(t, id, path, source)
		if err != nil {
			return Result{}, err
		}
		if profile != nil && string(profile) != string(p) {
			return Result{}, fmt.Errorf("conflicting captured provider profiles")
		}
		profile = p
		if introducedSecret(t.Environ, source, next) {
			return Result{}, fmt.Errorf("refusing to copy a credential")
		}
		planned[resolved], logical[resolved] = next, path
	}
	if err := underHome(t.Home, profilePath); err != nil {
		return Result{}, err
	}
	resolved, err := canonicalPath(profilePath)
	if err != nil {
		return Result{}, err
	}
	if existing, err := os.ReadFile(resolved); err == nil && string(existing) != string(profile) {
		state, managed, err := readManaged(t, id)
		if err != nil {
			return Result{}, err
		}
		owns := false
		for _, p := range state.Paths {
			if p == resolved {
				owns = true
			}
		}
		if !managed || !owns {
			return Result{}, fmt.Errorf("capture profile already exists; config unchanged")
		}
	}
	planned[resolved], logical[resolved] = profile, profilePath
	paths = nil
	var changes []Change
	for path, next := range planned {
		paths = append(paths, path)
		current := readFile(path)
		if current != string(next) {
			changes = append(changes, Change{Path: path, Diff: unified(path, current, string(next))})
		}
	}
	sort.Strings(paths)
	if dry {
		return Result{Tool: id, Action: "dry-run", Changes: changes}, nil
	}
	for _, path := range paths {
		if err := snapshotIfAbsent(t, path); err != nil {
			return Result{}, err
		}
	}
	for _, path := range paths {
		if err := writeFile(path, planned[path], 0600); err != nil {
			return Result{}, err
		}
	}
	if err := setManaged(t, id, paths, logical); err != nil {
		return Result{}, err
	}
	return Result{Tool: id, Action: "enabled", Changes: changes, Notes: "Provider credential preserved. Configure Conductor capture_dir as " + captureRoot(t) + " and restart Conductor. Clients must send X-Conductor-Token from CONDUCTOR_PROXY_TOKEN; the Codex profile adds its environment reference."}, nil
}

func captureProfile(endpoint, protocol string) ([]byte, error) {
	u, err := url.Parse(endpoint)
	if err != nil || u.Scheme != "https" || u.Host == "" || u.User != nil || u.RawQuery != "" || u.Fragment != "" {
		return nil, fmt.Errorf("captured provider requires an explicit HTTPS API root without credentials or query")
	}
	if strings.Contains(u.Path, "/capture/") {
		return nil, fmt.Errorf("original provider is missing; restore the original tool configuration first")
	}
	return marshalPretty(map[string]any{"schema": "hypermesh.conductor.capture.v1", "provider": protocol, "base_url": strings.TrimRight(endpoint, "/"), "model": "$client", "auth": "passthrough", "direct": true, "credential_origin": u.Scheme + "://" + u.Host})
}

func capturedEdit(t target, id, path string, content []byte) ([]byte, []byte, error) {
	endpoint, protocol := "", "openai-compatible"
	origin := t.origin + "/capture/" + id
	var next []byte
	var err error
	switch id {
	case "codex":
		root := map[string]any{}
		if err = toml.Unmarshal(content, &root); err != nil {
			break
		}
		providers, _ := root["model_providers"].(map[string]any)
		if providers == nil {
			providers = map[string]any{}
		}
		name, _ := root["model_provider"].(string)
		source, _ := providers[name].(map[string]any)
		selected := map[string]any{}
		for k, v := range source {
			selected[k] = v
		}
		if selected["auth"] != nil || selected["query_params"] != nil || selected["experimental_bearer_token"] != nil {
			return nil, nil, fmt.Errorf("Codex provider uses unsupported authentication or query parameters")
		}
		endpoint, _ = selected["base_url"].(string)
		if name == "" || name == "openai" {
			if endpoint == "" {
				endpoint = "https://api.openai.com/v1"
			}
			if envValue(t.Environ, "OPENAI_API_KEY") == "" {
				return nil, nil, fmt.Errorf("Codex direct capture requires OPENAI_API_KEY; subscription sessions cannot be redirected")
			}
			selected["env_key"] = "OPENAI_API_KEY"
			selected["requires_openai_auth"] = false
		} else if key, _ := selected["env_key"].(string); key == "" {
			return nil, nil, fmt.Errorf("custom Codex provider requires env_key for direct capture")
		}
		headers, _ := selected["env_http_headers"].(map[string]any)
		if headers == nil {
			headers = map[string]any{}
		}
		headers["X-Conductor-Token"] = "CONDUCTOR_PROXY_TOKEN"
		selected["env_http_headers"] = headers
		selected["name"], selected["base_url"] = "Conductor direct provider", origin+"/v1"
		providers["conductor-direct"] = selected
		root["model_providers"], root["model_provider"] = providers, "conductor-direct"
		next, err = toml.Marshal(root)
	case "claude-code":
		root := map[string]any{}
		if len(content) > 0 {
			if err = unmarshalJSON(content, &root); err != nil {
				break
			}
		}
		env, _ := root["env"].(map[string]any)
		if env == nil {
			env = map[string]any{}
		}
		endpoint, _ = env["ANTHROPIC_BASE_URL"].(string)
		if endpoint == "" {
			endpoint = envValue(t.Environ, "ANTHROPIC_BASE_URL")
		}
		if endpoint == "" {
			endpoint = "https://api.anthropic.com"
		}
		// OAuth sessions are not provider API keys. Keep them out of capture.
		if env["CLAUDE_CODE_OAUTH_TOKEN"] != nil || envValue(t.Environ, "CLAUDE_CODE_OAUTH_TOKEN") != "" {
			return nil, nil, fmt.Errorf("Claude OAuth capture is unsupported; use an API-key configuration")
		}
		_, helper := root["apiKeyHelper"]
		if !helper && env["ANTHROPIC_API_KEY"] == nil && env["ANTHROPIC_AUTH_TOKEN"] == nil && envValue(t.Environ, "ANTHROPIC_API_KEY") == "" && envValue(t.Environ, "ANTHROPIC_AUTH_TOKEN") == "" {
			return nil, nil, fmt.Errorf("Claude direct capture requires an existing API-key reference")
		}
		protocol = "anthropic"
		endpoint = strings.TrimRight(endpoint, "/")
		if !strings.HasSuffix(endpoint, "/v1") {
			endpoint += "/v1"
		}
		env["ANTHROPIC_BASE_URL"] = origin
		root["env"] = env
		next, err = marshalPretty(root)
	case "openai-env", "anthropic-env":
		key := "OPENAI_BASE_URL"
		if id == "anthropic-env" {
			key = "ANTHROPIC_BASE_URL"
			protocol = "anthropic"
		}
		validationTarget := t
		validationTarget.PreserveProvider = false
		if _, validationErr := editPath(validationTarget, id, path, content); validationErr != nil {
			return nil, nil, validationErr
		}
		endpoint = envValue(t.Environ, key)
		for _, line := range strings.Split(string(content), "\n") {
			field, value, ok := strings.Cut(strings.TrimPrefix(strings.TrimSpace(line), "export "), "=")
			if ok && (field == key || (id == "openai-env" && field == "OPENAI_API_BASE")) {
				value = unquote(value)
				if strings.ContainsAny(value, "$` \t") {
					return nil, nil, fmt.Errorf("dynamic provider URL cannot be captured safely")
				}
				if endpoint != "" && endpoint != value {
					return nil, nil, fmt.Errorf("shell and environment provider URLs disagree")
				}
				endpoint = value
			}
		}
		if endpoint == "" && id == "openai-env" {
			endpoint = envValue(t.Environ, "OPENAI_API_BASE")
		}
		if endpoint == "" {
			if id == "openai-env" {
				endpoint = "https://api.openai.com/v1"
			} else {
				endpoint = "https://api.anthropic.com"
			}
		}
		if id == "openai-env" {
			next = []byte(editShell(string(content), id, []string{"OPENAI_BASE_URL", "OPENAI_API_BASE"}, []string{"export OPENAI_BASE_URL=" + shellQuote(origin+"/v1"), "export OPENAI_API_BASE=" + shellQuote(origin+"/v1")}))
		} else {
			endpoint = strings.TrimRight(endpoint, "/")
			if !strings.HasSuffix(endpoint, "/v1") {
				endpoint += "/v1"
			}
			next = []byte(editShell(string(content), id, []string{key}, []string{"export " + key + "=" + shellQuote(origin)}))
		}
	case "opencode":
		root := map[string]any{}
		if err = unmarshalJSON(content, &root); err != nil {
			break
		}
		model, _ := root["model"].(string)
		provider, _, ok := strings.Cut(model, "/")
		providers, _ := root["provider"].(map[string]any)
		selected, _ := providers[provider].(map[string]any)
		opts, _ := selected["options"].(map[string]any)
		if !ok || opts == nil || selected["npm"] != opencodeNPM {
			return nil, nil, fmt.Errorf("OpenCode direct capture requires an explicit OpenAI-compatible provider")
		}
		endpoint, _ = opts["baseURL"].(string)
		opts["baseURL"] = origin + "/v1"
		next, err = marshalPretty(root)
	case "continue":
		root := map[string]any{}
		if strings.HasSuffix(path, ".json") {
			err = unmarshalJSON(content, &root)
		} else {
			err = yaml.Unmarshal(content, &root)
		}
		if err != nil {
			break
		}
		models, _ := root["models"].([]any)
		count := 0
		for _, item := range models {
			model, ok := item.(map[string]any)
			if !ok || model["provider"] != "openai" {
				return nil, nil, fmt.Errorf("Continue direct capture requires explicit OpenAI-compatible model entries")
			}
			base, _ := model["apiBase"].(string)
			if base == "" {
				base = "https://api.openai.com/v1"
			}
			if endpoint != "" && endpoint != base {
				return nil, nil, fmt.Errorf("Continue models use different providers; configure separate capture profiles")
			}
			endpoint = base
			model["apiBase"] = origin + "/v1"
			count++
		}
		if count == 0 {
			return nil, nil, fmt.Errorf("Continue has no configured model to capture")
		}
		if strings.HasSuffix(path, ".json") {
			next, err = marshalPretty(root)
		} else {
			next, err = yaml.Marshal(root)
		}
	default:
		return nil, nil, fmt.Errorf("direct capture is unsupported for %s", id)
	}
	if err != nil {
		return nil, nil, err
	}
	profile, err := captureProfile(endpoint, protocol)
	return next, profile, err
}
