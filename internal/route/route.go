// Package route detects local AI tools and, on request, points their own
// config at Conductor. Enable is opt-in. Unsupported tools are reported
// and left untouched.
package route

import (
	"bytes"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"regexp"
	"runtime"
	"sort"
	"strings"

	"github.com/hyperme-sh/hypermesh-cli/internal/api"
	"gopkg.in/yaml.v3"
)

const (
	ProtoOpenAI    = "openai"
	ProtoAnthropic = "anthropic"
	ProtoBoth      = "openai+anthropic"
	ProtoNone      = "none"
	keyReference   = "HYPERMESH_API_KEY"
	continueTitle  = "Hypermesh Conductor"
	opencodeNPM    = "@ai-sdk/openai-compatible"
)

// Tool is one row of the support matrix. Supported means the tool has a
// documented OpenAI-compatible or Anthropic-compatible base URL this CLI
// can write. It does not mean a copy is installed.
type Tool struct {
	ID        string `json:"id"`
	Name      string `json:"name"`
	Protocol  string `json:"protocol"`
	ConfigKey string `json:"config_key"`
	Supported bool   `json:"supported"`
	Reason    string `json:"reason,omitempty"`
}

// Finding is a detect or status row.
type Finding struct {
	ID           string   `json:"id"`
	Name         string   `json:"name"`
	Protocol     string   `json:"protocol"`
	Supported    bool     `json:"supported"`
	Redirectable bool     `json:"redirectable"`
	Detected     bool     `json:"detected"`
	ConfigPath   string   `json:"config_path,omitempty"`
	ConfigKey    string   `json:"config_key"`
	Endpoint     string   `json:"endpoint,omitempty"`
	Redirected   bool     `json:"redirected"`
	Managed      bool     `json:"managed"`
	Evidence     []string `json:"evidence,omitempty"`
	Notes        string   `json:"notes,omitempty"`
}

// Change is one file a dry-run or apply would touch.
type Change struct {
	Path string `json:"path"`
	Diff string `json:"diff,omitempty"`
}

// Result is one enable or disable outcome.
type Result struct {
	Tool    string   `json:"tool"`
	Action  string   `json:"action"`
	Notes   string   `json:"notes,omitempty"`
	Changes []Change `json:"changes,omitempty"`
}

// Options selects the machine scan. Tests pass a fixture home.
type Options struct {
	Home          string
	ConfigDir     string
	ConductorBase string
	Path          string
	Environ       []string
	GOOS          string
}

type target struct {
	Options
	origin string
	openai string
}

func Matrix() []Tool {
	return []Tool{
		{ID: "claude-code", Name: "Claude Code", Protocol: ProtoAnthropic, ConfigKey: "~/.claude/settings.json env.ANTHROPIC_BASE_URL", Supported: true, Reason: "Anthropic client. Conductor serves POST /v1/messages."},
		{ID: "codex", Name: "OpenAI Codex CLI", Protocol: ProtoOpenAI, ConfigKey: "~/.codex/config.toml model_providers.hypermesh.base_url", Supported: true, Reason: "OpenAI Responses and chat. Conductor serves POST /v1/responses and POST /v1/chat/completions."},
		{ID: "gemini", Name: "Gemini CLI", Protocol: ProtoNone, ConfigKey: "~/.gemini/settings.json (no base URL key)", Supported: false, Reason: "Gemini CLI talks to the Gemini API. It has no documented OpenAI-compatible or Anthropic-compatible base URL."},
		{ID: "aider", Name: "Aider", Protocol: ProtoOpenAI, ConfigKey: "~/.aider.conf.yml openai-api-base", Supported: true},
		{ID: "opencode", Name: "OpenCode", Protocol: ProtoOpenAI, ConfigKey: "~/.config/opencode/opencode.json provider.hypermesh.options.baseURL", Supported: true},
		{ID: "continue", Name: "Continue", Protocol: ProtoOpenAI, ConfigKey: "~/.continue/config.yaml or config.json models apiBase", Supported: true, Reason: "config.ts is detected and left unchanged."},
		{ID: "cline", Name: "Cline", Protocol: ProtoOpenAI, ConfigKey: "VS Code User settings.json cline.openAiBaseUrl", Supported: true, Reason: "The API key stays in VS Code secret storage. This CLI does not write it."},
		{ID: "roo", Name: "Roo Code", Protocol: ProtoOpenAI, ConfigKey: "VS Code User settings.json roo-cline.openAiBaseUrl", Supported: true, Reason: "The API key stays in VS Code secret storage. This CLI does not write it."},
		{ID: "cursor", Name: "Cursor", Protocol: ProtoNone, ConfigKey: "Cursor model UI state (not a documented config file)", Supported: false, Reason: "The OpenAI base URL override lives in Cursor UI state. This CLI does not edit that database."},
		{ID: "vscode-copilot", Name: "VS Code Copilot", Protocol: ProtoNone, ConfigKey: "Copilot BYOK secret storage (no documented base URL)", Supported: false, Reason: "Bring-your-own-key settings are not a documented base URL in settings.json."},
		{ID: "hypermesh-vscode", Name: "Hypermesh for VS Code", Protocol: ProtoOpenAI, ConfigKey: "VS Code User settings.json hypermesh.apiBase", Supported: true, Reason: "The extension posts {apiBase}/v1/chat/completions with Authorization: Bearer. It does not send X-Conductor-Class, so Conductor still needs default_class."},
		{ID: "zed", Name: "Zed", Protocol: ProtoBoth, ConfigKey: "~/.config/zed/settings.json language_models.openai.api_url and language_models.anthropic.api_url", Supported: true, Reason: "API keys stay in the system keychain."},
		{ID: "jetbrains", Name: "JetBrains AI", Protocol: ProtoNone, ConfigKey: "JetBrains options (no documented base URL)", Supported: false, Reason: "AI Assistant does not document an OpenAI or Anthropic base URL file."},
		{ID: "lmstudio", Name: "LM Studio", Protocol: ProtoNone, ConfigKey: "~/.lmstudio (local server)", Supported: false, Reason: "LM Studio is a local OpenAI-compatible server. It has no upstream client base URL to retarget."},
		{ID: "ollama", Name: "Ollama", Protocol: ProtoNone, ConfigKey: "~/.ollama (local server)", Supported: false, Reason: "Ollama is a local server. OLLAMA_HOST is its listen address, not an upstream base URL."},
		{ID: "openai-env", Name: "OpenAI environment", Protocol: ProtoOpenAI, ConfigKey: "OPENAI_BASE_URL and OPENAI_API_BASE", Supported: true, Reason: "Writes a shell reference ${HYPERMESH_API_KEY}, not the key."},
		{ID: "anthropic-env", Name: "Anthropic environment", Protocol: ProtoAnthropic, ConfigKey: "ANTHROPIC_BASE_URL", Supported: true, Reason: "Writes a shell reference ${HYPERMESH_API_KEY}, not the key."},
	}
}

func toolByID(id string) (Tool, bool) {
	for _, tool := range Matrix() {
		if tool.ID == id {
			return tool, true
		}
	}
	return Tool{}, false
}

// Detect scans opts.Home and returns one row per matrix tool.
func Detect(opts Options) ([]Finding, error) {
	t, err := prepare(opts)
	if err != nil {
		return nil, err
	}
	return detect(t)
}

// Status returns tools whose config points at Conductor, or that this CLI is managing.
func Status(opts Options) ([]Finding, error) {
	found, err := Detect(opts)
	if err != nil {
		return nil, err
	}
	var out []Finding
	for _, row := range found {
		if row.Redirected || row.Managed {
			out = append(out, row)
		}
	}
	return out, nil
}

// Enable points one tool at Conductor. dryRun reports the diff and writes nothing.
func Enable(opts Options, id string, dryRun bool) (Result, error) {
	return applyOne(opts, id, dryRun, true)
}

// Disable restores one tool from the backup taken at the first enable.
func Disable(opts Options, id string, dryRun bool) (Result, error) {
	return applyOne(opts, id, dryRun, false)
}

// EnableAll enables every detected tool this CLI can redirect.
// Unsupported tools are reported and skipped.
func EnableAll(opts Options, dryRun bool) ([]Result, error) {
	return applyAll(opts, dryRun, true)
}

// DisableAll restores every tool this CLI is managing.
func DisableAll(opts Options, dryRun bool) ([]Result, error) {
	return applyAll(opts, dryRun, false)
}

func prepare(opts Options) (target, error) {
	opts.Home = strings.TrimSpace(opts.Home)
	if opts.Home == "" {
		return target{}, errors.New("home directory is required")
	}
	opts.Home = filepath.Clean(opts.Home)
	if opts.GOOS == "" {
		opts.GOOS = runtime.GOOS
	}
	if strings.TrimSpace(opts.ConductorBase) == "" {
		opts.ConductorBase = api.DefaultConductorBase
	}
	if strings.TrimSpace(opts.ConfigDir) == "" {
		opts.ConfigDir = filepath.Join(opts.Home, ".config", "hypermesh")
	}
	origin, err := api.ConductorOrigin(opts.ConductorBase)
	if err != nil {
		return target{}, err
	}
	openai, err := api.OpenAIRoot(opts.ConductorBase)
	if err != nil {
		return target{}, err
	}
	return target{Options: opts, origin: origin, openai: openai}, nil
}

func detect(t target) ([]Finding, error) {
	managed, err := managedIDs(t)
	if err != nil {
		return nil, err
	}
	out := make([]Finding, 0, len(Matrix()))
	for _, spec := range Matrix() {
		row := Finding{
			ID: spec.ID, Name: spec.Name, Protocol: spec.Protocol,
			Supported: spec.Supported, ConfigKey: spec.ConfigKey, Notes: spec.Reason,
		}
		ev, path, endpoint, redirectable, note := inspect(t, spec.ID)
		row.Evidence = ev
		row.ConfigPath = path
		row.Endpoint = endpoint
		row.Detected = len(ev) > 0
		row.Redirectable = spec.Supported && redirectable
		if note != "" {
			if row.Notes != "" {
				row.Notes += " "
			}
			row.Notes += note
		}
		row.Redirected = endpoint != "" && endpointMatches(spec.Protocol, endpoint, t)
		row.Managed = managed[spec.ID]
		out = append(out, row)
	}
	return out, nil
}

func endpointMatches(protocol, endpoint string, t target) bool {
	switch protocol {
	case ProtoAnthropic:
		return endpoint == t.origin
	case ProtoOpenAI:
		return endpoint == t.openai
	case ProtoBoth:
		return endpoint == t.openai+" "+t.origin
	default:
		return false
	}
}

func inspect(t target, id string) (evidence []string, path, endpoint string, redirectable bool, note string) {
	switch id {
	case "claude-code":
		return inspectClaude(t)
	case "codex":
		return inspectCodex(t)
	case "gemini":
		return inspectPathOrDir(t, "gemini", filepath.Join(t.Home, ".gemini"), false)
	case "aider":
		return inspectAider(t)
	case "opencode":
		return inspectOpenCode(t)
	case "continue":
		return inspectContinue(t)
	case "cline":
		return inspectEditor(t, "cline", []string{"saoudrizwan.claude-dev", "cline.cline"}, "cline.openAiBaseUrl", "cline.apiProvider")
	case "roo":
		return inspectEditor(t, "roo", []string{"rooveterinaryinc.roo-cline"}, "roo-cline.openAiBaseUrl", "roo-cline.apiProvider")
	case "cursor":
		return inspectCursor(t)
	case "vscode-copilot":
		return inspectCopilot(t)
	case "hypermesh-vscode":
		return inspectHypermeshVSCode(t)
	case "zed":
		return inspectZed(t)
	case "jetbrains":
		return inspectJetBrains(t)
	case "lmstudio":
		return inspectAnyDir(t, "lmstudio", []string{
			filepath.Join(t.Home, ".lmstudio"),
			filepath.Join(t.Home, ".cache", "lm-studio"),
			filepath.Join(t.Home, "Library", "Application Support", "LM Studio"),
		}, false)
	case "ollama":
		return inspectOllama(t)
	case "openai-env":
		return inspectEnv(t, []string{"OPENAI_BASE_URL", "OPENAI_API_BASE"}, t.openai)
	case "anthropic-env":
		return inspectEnv(t, []string{"ANTHROPIC_BASE_URL"}, t.origin)
	default:
		return nil, "", "", false, ""
	}
}

func inspectClaude(t target) (evidence []string, path, endpoint string, redirectable bool, note string) {
	if bin := lookup(t, "claude"); bin != "" {
		evidence = append(evidence, "PATH:"+bin)
	}
	settings := filepath.Join(t.Home, ".claude", "settings.json")
	legacy := filepath.Join(t.Home, ".claude.json")
	if exists(settings) {
		evidence = append(evidence, settings)
		path = settings
		endpoint = jsonString(readFile(settings), "env", "ANTHROPIC_BASE_URL")
	}
	if exists(legacy) {
		evidence = append(evidence, legacy)
		if path == "" {
			path = settings
		}
	}
	redirectable = len(evidence) > 0
	if path == "" && redirectable {
		path = settings
	}
	return evidence, path, endpoint, redirectable, ""
}

func inspectCodex(t target) (evidence []string, path, endpoint string, redirectable bool, note string) {
	if bin := lookup(t, "codex"); bin != "" {
		evidence = append(evidence, "PATH:"+bin)
	}
	cfg := filepath.Join(t.Home, ".codex", "config.toml")
	if exists(cfg) {
		evidence = append(evidence, cfg)
		path = cfg
		endpoint = codexBaseURL(readFile(cfg))
	}
	redirectable = len(evidence) > 0
	if path == "" && redirectable {
		path = cfg
	}
	return evidence, path, endpoint, redirectable, ""
}

func inspectAider(t target) (evidence []string, path, endpoint string, redirectable bool, note string) {
	if bin := lookup(t, "aider"); bin != "" {
		evidence = append(evidence, "PATH:"+bin)
	}
	for _, name := range []string{".aider.conf.yml", ".aider.conf.yaml"} {
		cfg := filepath.Join(t.Home, name)
		if exists(cfg) {
			evidence = append(evidence, cfg)
			if path == "" {
				path = cfg
				endpoint = yamlScalar(readFile(cfg), "openai-api-base")
			}
		}
	}
	redirectable = len(evidence) > 0
	if path == "" && redirectable {
		path = filepath.Join(t.Home, ".aider.conf.yml")
	}
	return evidence, path, endpoint, redirectable, ""
}

func inspectOpenCode(t target) (evidence []string, path, endpoint string, redirectable bool, note string) {
	if bin := lookup(t, "opencode"); bin != "" {
		evidence = append(evidence, "PATH:"+bin)
	}
	for _, cfg := range []string{
		filepath.Join(t.Home, ".config", "opencode", "opencode.json"),
		filepath.Join(t.Home, "Library", "Application Support", "opencode", "opencode.json"),
	} {
		if exists(cfg) {
			evidence = append(evidence, cfg)
			if path == "" {
				path = cfg
				endpoint = jsonString(readFile(cfg), "provider", "hypermesh", "options", "baseURL")
			}
		}
	}
	redirectable = len(evidence) > 0
	if path == "" && redirectable {
		path = filepath.Join(t.Home, ".config", "opencode", "opencode.json")
	}
	return evidence, path, endpoint, redirectable, ""
}

func inspectContinue(t target) (evidence []string, path, endpoint string, redirectable bool, note string) {
	dir := filepath.Join(t.Home, ".continue")
	yamlPath := firstExisting(filepath.Join(dir, "config.yaml"), filepath.Join(dir, "config.yml"))
	jsonPath := filepath.Join(dir, "config.json")
	tsPath := filepath.Join(dir, "config.ts")
	if exists(yamlPath) {
		evidence = append(evidence, yamlPath)
		path = yamlPath
		endpoint = continueEndpoint(readFile(yamlPath), false)
		redirectable = true
	}
	if exists(jsonPath) {
		evidence = append(evidence, jsonPath)
		if path == "" {
			path = jsonPath
			endpoint = continueEndpoint(readFile(jsonPath), true)
			redirectable = true
		}
	}
	if exists(tsPath) {
		evidence = append(evidence, tsPath)
		if path == "" {
			note = "Continue config.ts is not edited."
			redirectable = false
			path = tsPath
		} else {
			note = "config.ts is present and was not selected."
		}
	}
	if exists(dir) && len(evidence) == 0 {
		evidence = append(evidence, dir)
		redirectable = true
		path = filepath.Join(dir, "config.json")
	}
	return evidence, path, endpoint, redirectable, note
}

func inspectEditor(t target, id string, prefixes []string, baseKey, providerKey string) (evidence []string, path, endpoint string, redirectable bool, note string) {
	settings := codeSettings(t)
	if exists(settings) {
		raw := readFile(settings)
		if jsonHasPrefix(raw, strings.TrimSuffix(baseKey, ".openAiBaseUrl")) || strings.Contains(raw, `"`+baseKey+`"`) || strings.Contains(raw, `"`+providerKey+`"`) {
			evidence = append(evidence, settings)
			endpoint = jsonString(raw, baseKey)
		}
	}
	for _, hit := range extensionHits(t, prefixes) {
		evidence = append(evidence, hit)
	}
	redirectable = len(evidence) > 0
	if redirectable {
		path = settings
	}
	return evidence, path, endpoint, redirectable, ""
}

func inspectCursor(t target) (evidence []string, path, endpoint string, redirectable bool, note string) {
	if bin := lookup(t, "cursor"); bin != "" {
		evidence = append(evidence, "PATH:"+bin)
	}
	for _, dir := range []string{
		filepath.Join(t.Home, ".cursor"),
		filepath.Join(t.Home, ".config", "Cursor"),
		filepath.Join(t.Home, "Library", "Application Support", "Cursor"),
	} {
		if exists(dir) {
			evidence = append(evidence, dir)
		}
	}
	return evidence, "", "", false, ""
}

func inspectCopilot(t target) (evidence []string, path, endpoint string, redirectable bool, note string) {
	settings := codeSettings(t)
	if exists(settings) && strings.Contains(readFile(settings), "github.copilot") {
		evidence = append(evidence, settings)
	}
	evidence = append(evidence, extensionHits(t, []string{"github.copilot"})...)
	if len(evidence) == 0 {
		return nil, "", "", false, ""
	}
	if bin := lookup(t, "code"); bin != "" {
		evidence = append(evidence, "PATH:"+bin)
	}
	return evidence, "", "", false, ""
}

func inspectHypermeshVSCode(t target) (evidence []string, path, endpoint string, redirectable bool, note string) {
	settings := codeSettings(t)
	if exists(settings) {
		raw := readFile(settings)
		if strings.Contains(raw, "hypermesh.") {
			evidence = append(evidence, settings)
			endpoint = jsonString(raw, "hypermesh.apiBase")
		}
	}
	evidence = append(evidence, extensionHits(t, []string{"fyberlabs.hypermesh-vscode"})...)
	if bin := lookup(t, "code"); bin != "" && len(evidence) > 0 {
		evidence = append(evidence, "PATH:"+bin)
	}
	redirectable = len(evidence) > 0
	if redirectable {
		path = settings
	}
	return evidence, path, endpoint, redirectable, ""
}

func inspectZed(t target) (evidence []string, path, endpoint string, redirectable bool, note string) {
	if bin := lookup(t, "zed"); bin != "" {
		evidence = append(evidence, "PATH:"+bin)
	}
	settings := zedSettings(t)
	if exists(settings) {
		evidence = append(evidence, settings)
		path = settings
		raw := readFile(settings)
		openai := jsonString(raw, "language_models", "openai", "api_url")
		anthropic := jsonString(raw, "language_models", "anthropic", "api_url")
		if openai != "" || anthropic != "" {
			endpoint = openai + " " + anthropic
			endpoint = strings.TrimSpace(endpoint)
		}
	}
	redirectable = len(evidence) > 0
	if path == "" && redirectable {
		path = settings
	}
	return evidence, path, endpoint, redirectable, ""
}

func inspectJetBrains(t target) (evidence []string, path, endpoint string, redirectable bool, note string) {
	roots := []string{
		filepath.Join(t.Home, ".config", "JetBrains"),
		filepath.Join(t.Home, "Library", "Application Support", "JetBrains"),
		filepath.Join(t.Home, "AppData", "Roaming", "JetBrains"),
	}
	for _, dir := range roots {
		if exists(dir) {
			evidence = append(evidence, dir)
		}
	}
	entries, _ := os.ReadDir(t.Home)
	for _, entry := range entries {
		name := entry.Name()
		if jetBrainsDir(name) && entry.IsDir() {
			evidence = append(evidence, filepath.Join(t.Home, name))
		}
	}
	sort.Strings(evidence)
	return evidence, "", "", false, ""
}

func inspectOllama(t target) (evidence []string, path, endpoint string, redirectable bool, note string) {
	if bin := lookup(t, "ollama"); bin != "" {
		evidence = append(evidence, "PATH:"+bin)
	}
	dir := filepath.Join(t.Home, ".ollama")
	if exists(dir) {
		evidence = append(evidence, dir)
	}
	if value := envValue(t.Environ, "OLLAMA_HOST"); value != "" {
		evidence = append(evidence, "env:OLLAMA_HOST")
		note = "OLLAMA_HOST is the listen address."
	}
	return evidence, "", "", false, note
}

func inspectPathOrDir(t target, bin, dir string, redirectable bool) ([]string, string, string, bool, string) {
	var evidence []string
	if found := lookup(t, bin); found != "" {
		evidence = append(evidence, "PATH:"+found)
	}
	if exists(dir) {
		evidence = append(evidence, dir)
	}
	return evidence, "", "", redirectable && len(evidence) > 0, ""
}

func inspectAnyDir(t target, bin string, dirs []string, redirectable bool) ([]string, string, string, bool, string) {
	var evidence []string
	if found := lookup(t, bin); found != "" {
		evidence = append(evidence, "PATH:"+found)
	}
	for _, dir := range dirs {
		if exists(dir) {
			evidence = append(evidence, dir)
		}
	}
	return evidence, "", "", redirectable && len(evidence) > 0, ""
}

func inspectEnv(t target, keys []string, want string) (evidence []string, path, endpoint string, redirectable bool, note string) {
	_ = want
	file, fileValue := shellAssignment(t, keys)
	envVal := ""
	for _, key := range keys {
		if value := envValue(t.Environ, key); value != "" {
			evidence = append(evidence, "env:"+key)
			if envVal == "" {
				envVal = value
			}
		}
	}
	if file != "" {
		evidence = append(evidence, file)
		path = file
		endpoint = fileValue
	}
	if endpoint == "" {
		endpoint = envVal
	} else if envVal != "" && envVal != endpoint {
		note = "The process environment differs from the shell file."
	}
	redirectable = true
	if path == "" {
		path = shellTarget(t, keys)
	}
	return evidence, path, endpoint, redirectable, note
}

func applyAll(opts Options, dryRun, enable bool) ([]Result, error) {
	t, err := prepare(opts)
	if err != nil {
		return nil, err
	}
	found, err := detect(t)
	if err != nil {
		return nil, err
	}
	var results []Result
	var failed error
	for _, row := range found {
		if enable {
			if !row.Detected || !row.Supported {
				if row.Detected && !row.Supported {
					results = append(results, Result{Tool: row.ID, Action: "unsupported", Notes: row.Notes})
				}
				continue
			}
			if !row.Redirectable {
				results = append(results, Result{Tool: row.ID, Action: "unsupported", Notes: row.Notes})
				continue
			}
		} else if !row.Managed {
			continue
		}
		result, err := applyPrepared(t, row.ID, dryRun, enable)
		results = append(results, result)
		if err != nil && failed == nil {
			failed = err
		}
	}
	return results, failed
}

func applyOne(opts Options, id string, dryRun, enable bool) (Result, error) {
	t, err := prepare(opts)
	if err != nil {
		return Result{}, err
	}
	spec, ok := toolByID(id)
	if !ok {
		return Result{Tool: id, Action: "error"}, fmt.Errorf("unknown tool %q", id)
	}
	if enable && !spec.Supported {
		return Result{Tool: id, Action: "unsupported", Notes: spec.Reason}, fmt.Errorf("%s is not redirected: %s", spec.Name, spec.Reason)
	}
	return applyPrepared(t, id, dryRun, enable)
}

func applyPrepared(t target, id string, dryRun, enable bool) (Result, error) {
	spec, ok := toolByID(id)
	if !ok {
		return Result{Tool: id, Action: "error"}, fmt.Errorf("unknown tool %q", id)
	}
	if !enable {
		return disableTool(t, spec, dryRun)
	}
	found, err := detect(t)
	if err != nil {
		return Result{}, err
	}
	var row Finding
	for _, item := range found {
		if item.ID == id {
			row = item
			break
		}
	}
	if !row.Redirectable {
		return Result{Tool: id, Action: "unsupported", Notes: row.Notes}, fmt.Errorf("%s is not redirected: %s", spec.Name, strings.TrimSpace(row.Notes))
	}
	if !row.Detected && !envTool(id) {
		return Result{Tool: id, Action: "absent"}, fmt.Errorf("%s was not detected", spec.Name)
	}
	paths := editPaths(t, id, row)
	if len(paths) == 0 {
		return Result{Tool: id, Action: "error"}, fmt.Errorf("%s has no config path", spec.Name)
	}
	for _, path := range paths {
		if err := underHome(t.Home, path); err != nil {
			return Result{}, err
		}
	}
	changes := make([]Change, 0, len(paths))
	anyDiff := false
	planned := map[string][]byte{}
	for _, path := range paths {
		current := readFile(path)
		next, err := editPath(t, id, path, []byte(current))
		if err != nil {
			return Result{}, err
		}
		if introducedSecret(t.Environ, []byte(current), next) {
			return Result{}, fmt.Errorf("refusing to write a secret into %s", path)
		}
		planned[path] = next
		if string(next) != current {
			anyDiff = true
			changes = append(changes, Change{Path: path, Diff: unified(path, current, string(next))})
		}
	}
	if !anyDiff {
		return Result{Tool: id, Action: "unchanged", Changes: changes}, nil
	}
	if dryRun {
		return Result{Tool: id, Action: "dry-run", Changes: changes}, nil
	}
	for _, path := range paths {
		if err := snapshotIfAbsent(t, path); err != nil {
			return Result{}, err
		}
	}
	for _, path := range paths {
		mode := snapMode(t, path)
		if err := writeFile(path, planned[path], mode); err != nil {
			return Result{}, err
		}
	}
	if err := setManaged(t, id, paths); err != nil {
		return Result{}, err
	}
	return Result{Tool: id, Action: "enabled", Changes: changes}, nil
}

func disableTool(t target, spec Tool, dryRun bool) (Result, error) {
	state, ok, err := readManaged(t, spec.ID)
	if err != nil {
		return Result{}, err
	}
	if !ok {
		return Result{Tool: spec.ID, Action: "unchanged", Notes: "not redirected by this CLI"}, nil
	}
	others, err := managedStates(t)
	if err != nil {
		return Result{}, err
	}
	delete(others, spec.ID)
	var changes []Change
	for _, path := range state.Paths {
		snap, snapOK, err := readSnap(t, path)
		if err != nil {
			return Result{}, err
		}
		if !snapOK {
			return Result{}, fmt.Errorf("backup missing for %s", path)
		}
		current := readFile(path)
		var next []byte
		remaining := toolsForPath(others, path)
		if len(remaining) == 0 {
			if snap.Existed {
				next = snap.Body
			} else {
				next = nil
			}
		} else {
			next = snap.Body
			ordered := append([]string{}, remaining...)
			sort.Strings(ordered)
			for _, other := range ordered {
				opts := t.Options
				opts.ConductorBase = others[other].ConductorBase
				if opts.ConductorBase == "" {
					// Older state files did not record a target. Recover it from
					// the remaining tool, never from the disable invocation.
					_, _, opts.ConductorBase, _, _ = inspect(t, other)
					if opts.ConductorBase == "" {
						return Result{}, fmt.Errorf("cannot determine saved endpoint for %s", other)
					}
				}
				otherTarget, targetErr := prepare(opts)
				if targetErr != nil {
					return Result{}, targetErr
				}
				next, err = editPath(otherTarget, other, path, next)
				if err != nil {
					return Result{}, err
				}
			}
		}
		diffCurrent := current
		diffNext := string(next)
		if next == nil {
			diffNext = ""
		}
		if diffCurrent != diffNext {
			changes = append(changes, Change{Path: path, Diff: unified(path, diffCurrent, diffNext)})
		}
		if dryRun {
			continue
		}
		if len(remaining) == 0 {
			if err := restoreSnap(t, path, snap); err != nil {
				return Result{}, err
			}
			continue
		}
		if err := writeFile(path, next, os.FileMode(snap.Mode)); err != nil {
			return Result{}, err
		}
	}
	if dryRun {
		return Result{Tool: spec.ID, Action: "dry-run", Changes: changes}, nil
	}
	if err := clearManaged(t, spec.ID); err != nil {
		return Result{}, err
	}
	for _, path := range state.Paths {
		if len(toolsForPath(others, path)) == 0 {
			_ = removeSnap(t, path)
		}
	}
	return Result{Tool: spec.ID, Action: "disabled", Changes: changes}, nil
}

func toolsForPath(states map[string]managedState, path string) []string {
	var ids []string
	for id, state := range states {
		for _, candidate := range state.Paths {
			if candidate == path {
				ids = append(ids, id)
			}
		}
	}
	return ids
}

func editPaths(t target, id string, row Finding) []string {
	switch id {
	case "openai-env", "anthropic-env":
		keys := []string{"OPENAI_BASE_URL", "OPENAI_API_BASE"}
		if id == "anthropic-env" {
			keys = []string{"ANTHROPIC_BASE_URL"}
		}
		return []string{shellTarget(t, keys)}
	default:
		if row.ConfigPath == "" {
			return nil
		}
		return []string{row.ConfigPath}
	}
}

func editPath(t target, id, path string, content []byte) ([]byte, error) {
	switch id {
	case "claude-code":
		return editClaude(content, t.origin)
	case "codex":
		return []byte(editCodex(string(content), t.openai)), nil
	case "aider":
		return []byte(editAider(string(content), t.openai)), nil
	case "opencode":
		return editOpenCode(content, t.openai)
	case "continue":
		if strings.HasSuffix(path, ".json") {
			return editContinueJSON(content, t.openai)
		}
		return editContinueYAML(string(content), t.openai)
	case "cline":
		return editJSONMap(content, func(root map[string]any) {
			root["cline.apiProvider"] = "openai"
			root["cline.openAiBaseUrl"] = t.openai
		})
	case "roo":
		return editJSONMap(content, func(root map[string]any) {
			root["roo-cline.apiProvider"] = "openai"
			root["roo-cline.openAiBaseUrl"] = t.openai
		})
	case "hypermesh-vscode":
		return editJSONMap(content, func(root map[string]any) {
			root["hypermesh.apiBase"] = t.origin
		})
	case "zed":
		return editZed(content, t.origin, t.openai)
	case "openai-env":
		return []byte(editShell(string(content), id, []string{
			"OPENAI_BASE_URL", "OPENAI_API_BASE", "OPENAI_API_KEY",
		}, []string{
			fmt.Sprintf("export OPENAI_BASE_URL=%q", t.openai),
			fmt.Sprintf("export OPENAI_API_BASE=%q", t.openai),
			`export OPENAI_API_KEY="${HYPERMESH_API_KEY}"`,
		})), nil
	case "anthropic-env":
		return []byte(editShell(string(content), id, []string{
			"ANTHROPIC_BASE_URL", "ANTHROPIC_API_KEY",
		}, []string{
			fmt.Sprintf("export ANTHROPIC_BASE_URL=%q", t.origin),
			`export ANTHROPIC_API_KEY="${HYPERMESH_API_KEY}"`,
		})), nil
	default:
		return nil, fmt.Errorf("no editor for %s", id)
	}
}

func envTool(id string) bool {
	return id == "openai-env" || id == "anthropic-env"
}

func introducedSecret(environ []string, before, after []byte) bool {
	for _, key := range []string{"HYPERMESH_API_KEY", "OPENAI_API_KEY", "ANTHROPIC_API_KEY", api.EnvConductorProxyToken} {
		value := envValue(environ, key)
		if len(value) < 8 {
			continue
		}
		if bytes.Contains(after, []byte(value)) && !bytes.Contains(before, []byte(value)) {
			return true
		}
	}
	return false
}

func editClaude(content []byte, origin string) ([]byte, error) {
	root := map[string]any{}
	if len(bytes.TrimSpace(content)) > 0 {
		if err := unmarshalJSON(content, &root); err != nil {
			return nil, err
		}
	}
	env, _ := root["env"].(map[string]any)
	if env == nil {
		env = map[string]any{}
	}
	env["ANTHROPIC_BASE_URL"] = origin
	root["env"] = env
	if _, hasKey := env["ANTHROPIC_API_KEY"]; !hasKey {
		if _, hasHelper := root["apiKeyHelper"]; !hasHelper {
			root["apiKeyHelper"] = "printenv " + keyReference
		}
	}
	return marshalPretty(root)
}

func editCodex(content, openai string) string {
	text := strings.ReplaceAll(content, "\r\n", "\n")
	lines := strings.Split(text, "\n")
	var kept []string
	skipping := false
	inTable := false
	set := false
	for _, line := range lines {
		trim := strings.TrimSpace(line)
		header := tomlTableHeader(trim)
		if header != "" {
			inTable = true
			skipping = header == "[model_providers.hypermesh]"
			if skipping {
				continue
			}
		}
		if skipping {
			continue
		}
		if !inTable && tomlKey(trim) == "model_provider" {
			kept = append(kept, `model_provider = "hypermesh"`)
			set = true
			continue
		}
		kept = append(kept, line)
	}
	body := strings.TrimRight(strings.Join(kept, "\n"), "\n")
	table := fmt.Sprintf("[model_providers.hypermesh]\nname = %q\nbase_url = %q\nenv_key = %q\nwire_api = \"responses\"\n", continueTitle, openai, keyReference)
	if !set {
		if body != "" {
			body += "\n"
		}
		body = "model_provider = \"hypermesh\"\n\n" + body
	}
	if body != "" && !strings.HasSuffix(body, "\n") {
		body += "\n"
	}
	return body + "\n" + table
}

func tomlTableHeader(line string) string {
	var quote byte
	escaped := false
	for i := 0; i < len(line); i++ {
		ch := line[i]
		if quote != 0 {
			if escaped {
				escaped = false
				continue
			}
			if quote == '"' && ch == '\\' {
				escaped = true
				continue
			}
			if ch == quote {
				quote = 0
			}
		} else if ch == '"' || ch == '\'' {
			quote = ch
		} else if ch == '#' {
			line = line[:i]
			break
		}
	}
	line = strings.TrimSpace(line)
	if strings.HasPrefix(line, "[") && strings.HasSuffix(line, "]") {
		return line
	}
	return ""
}

func editAider(content, openai string) string {
	text := strings.ReplaceAll(content, "\r\n", "\n")
	if strings.TrimSpace(text) == "" {
		return "openai-api-base: " + openai + "\n"
	}
	lines := strings.Split(text, "\n")
	found := false
	for i, line := range lines {
		trim := strings.TrimSpace(line)
		if trim == "" || strings.HasPrefix(trim, "#") {
			continue
		}
		key, _, ok := strings.Cut(trim, ":")
		if ok && strings.TrimSpace(key) == "openai-api-base" {
			lines[i] = "openai-api-base: " + openai
			found = true
		}
	}
	out := strings.Join(lines, "\n")
	if !found {
		if !strings.HasSuffix(out, "\n") {
			out += "\n"
		}
		out += "openai-api-base: " + openai + "\n"
	}
	if !strings.HasSuffix(out, "\n") {
		out += "\n"
	}
	return out
}

func editOpenCode(content []byte, openai string) ([]byte, error) {
	root := map[string]any{}
	if len(bytes.TrimSpace(content)) > 0 {
		if err := unmarshalJSON(content, &root); err != nil {
			return nil, err
		}
	}
	providers, _ := root["provider"].(map[string]any)
	if providers == nil {
		providers = map[string]any{}
	}
	providers["hypermesh"] = map[string]any{
		"npm":  opencodeNPM,
		"name": continueTitle,
		"options": map[string]any{
			"baseURL": openai,
			"apiKey":  "{env:" + keyReference + "}",
		},
	}
	root["provider"] = providers
	root["model"] = "hypermesh/conductor"
	return marshalPretty(root)
}

func editContinueJSON(content []byte, openai string) ([]byte, error) {
	root := map[string]any{}
	if len(bytes.TrimSpace(content)) > 0 {
		if err := unmarshalJSON(content, &root); err != nil {
			return nil, err
		}
	}
	var models []any
	if existing, ok := root["models"].([]any); ok {
		models = existing
	}
	fresh := map[string]any{
		"title":    continueTitle,
		"provider": "openai",
		"model":    "conductor",
		"apiBase":  openai,
		"apiKey":   "${{ secrets." + keyReference + " }}",
	}
	replaced := false
	for i, item := range models {
		obj, ok := item.(map[string]any)
		if !ok {
			continue
		}
		title, _ := obj["title"].(string)
		name, _ := obj["name"].(string)
		if title == continueTitle || name == continueTitle {
			obj["provider"] = "openai"
			obj["apiBase"] = openai
			if _, exists := obj["apiKey"]; !exists {
				obj["apiKey"] = fresh["apiKey"]
			}
			models[i] = obj
			replaced = true
		}
	}
	if !replaced {
		models = append(models, fresh)
	}
	root["models"] = models
	return marshalPretty(root)
}

func editContinueYAML(content, openai string) ([]byte, error) {
	var doc yaml.Node
	if err := yaml.Unmarshal([]byte(content), &doc); err != nil {
		return nil, err
	}
	if len(doc.Content) == 0 {
		doc = yaml.Node{Kind: yaml.DocumentNode, Content: []*yaml.Node{{Kind: yaml.MappingNode}}}
	}
	root := doc.Content[0]
	if root.Kind != yaml.MappingNode {
		return nil, fmt.Errorf("Continue config must be a YAML mapping")
	}
	var models *yaml.Node
	for i := 0; i < len(root.Content); i += 2 {
		if root.Content[i].Value == "models" {
			if models != nil {
				return nil, fmt.Errorf("duplicate models key")
			}
			models = root.Content[i+1]
		}
	}
	if models == nil {
		models = &yaml.Node{Kind: yaml.SequenceNode}
		root.Content = append(root.Content, &yaml.Node{Kind: yaml.ScalarNode, Value: "models"}, models)
	}
	if models.Tag == "!!null" {
		models.Kind = yaml.SequenceNode
		models.Tag = "!!seq"
		models.Value = ""
	}
	if models.Kind != yaml.SequenceNode {
		return nil, fmt.Errorf("Continue models must be a sequence")
	}
	var model *yaml.Node
	for _, item := range models.Content {
		if item.Kind != yaml.MappingNode {
			continue
		}
		for i := 0; i < len(item.Content); i += 2 {
			if (item.Content[i].Value == "name" || item.Content[i].Value == "title") && item.Content[i+1].Value == continueTitle {
				model = item
			}
		}
	}
	if model == nil {
		model = &yaml.Node{Kind: yaml.MappingNode}
		models.Content = append(models.Content, model)
	}
	set := func(key, value string, preserve bool) {
		for i := 0; i < len(model.Content); i += 2 {
			if model.Content[i].Value == key {
				if !preserve {
					model.Content[i+1] = &yaml.Node{Kind: yaml.ScalarNode, Tag: "!!str", Value: value}
				}
				return
			}
		}
		model.Content = append(model.Content, &yaml.Node{Kind: yaml.ScalarNode, Value: key}, &yaml.Node{Kind: yaml.ScalarNode, Tag: "!!str", Value: value})
	}
	set("name", continueTitle, true)
	set("provider", "openai", false)
	set("model", "conductor", true)
	set("apiBase", openai, false)
	set("apiKey", "${{ secrets."+keyReference+" }}", true)
	return yaml.Marshal(&doc)
}

func replaceNearbyAPIBase(content, openai string) string {
	lines := strings.Split(content, "\n")
	anchor := -1
	for i, line := range lines {
		if strings.Contains(line, continueTitle) {
			anchor = i
			break
		}
	}
	if anchor < 0 {
		return content
	}
	limit := anchor + 8
	if limit > len(lines) {
		limit = len(lines)
	}
	for i := anchor; i < limit; i++ {
		trim := strings.TrimSpace(lines[i])
		if strings.HasPrefix(trim, "apiBase:") {
			indent := lines[i][:len(lines[i])-len(strings.TrimLeft(lines[i], " \t"))]
			lines[i] = indent + "apiBase: " + openai
			out := strings.Join(lines, "\n")
			if !strings.HasSuffix(out, "\n") {
				out += "\n"
			}
			return out
		}
	}
	return content
}

func editZed(content []byte, origin, openai string) ([]byte, error) {
	return editJSONMap(content, func(root map[string]any) {
		models, _ := root["language_models"].(map[string]any)
		if models == nil {
			models = map[string]any{}
		}
		openAIModel, _ := models["openai"].(map[string]any)
		if openAIModel == nil {
			openAIModel = map[string]any{}
		}
		openAIModel["api_url"] = openai
		models["openai"] = openAIModel
		anthropic, _ := models["anthropic"].(map[string]any)
		if anthropic == nil {
			anthropic = map[string]any{}
		}
		anthropic["api_url"] = origin
		models["anthropic"] = anthropic
		root["language_models"] = models
	})
}

func editJSONMap(content []byte, mutate func(map[string]any)) ([]byte, error) {
	root := map[string]any{}
	if len(bytes.TrimSpace(content)) > 0 {
		if err := unmarshalJSON(content, &root); err != nil {
			return nil, err
		}
	}
	mutate(root)
	return marshalPretty(root)
}

func editShell(content, id string, keys, exports []string) string {
	text := removeBlock(strings.ReplaceAll(content, "\r\n", "\n"), id)
	text = stripAssignments(text, keys)
	text = strings.TrimRight(text, "\n")
	if text != "" {
		text += "\n"
	}
	var block strings.Builder
	fmt.Fprintf(&block, "# hypermesh-route %s begin\n", id)
	for _, line := range exports {
		block.WriteString(line)
		block.WriteByte('\n')
	}
	fmt.Fprintf(&block, "# hypermesh-route %s end\n", id)
	return text + block.String()
}

func removeBlock(content, id string) string {
	begin := fmt.Sprintf("# hypermesh-route %s begin", id)
	end := fmt.Sprintf("# hypermesh-route %s end", id)
	lines := strings.Split(content, "\n")
	var kept []string
	skipping := false
	for _, line := range lines {
		trim := strings.TrimSpace(line)
		if trim == begin {
			skipping = true
			continue
		}
		if skipping {
			if trim == end {
				skipping = false
			}
			continue
		}
		kept = append(kept, line)
	}
	return strings.Join(kept, "\n")
}

func stripAssignments(content string, keys []string) string {
	want := map[string]bool{}
	for _, key := range keys {
		want[key] = true
	}
	lines := strings.Split(content, "\n")
	var kept []string
	for _, line := range lines {
		trim := strings.TrimSpace(line)
		body := strings.TrimPrefix(trim, "export ")
		body = strings.TrimSpace(body)
		key, _, ok := strings.Cut(body, "=")
		if ok && want[strings.TrimSpace(key)] && !strings.HasPrefix(trim, "#") {
			indent := line[:len(line)-len(strings.TrimLeft(line, " \t"))]
			kept = append(kept, indent+": # assignment managed by hypermesh-route below")
			continue
		}
		kept = append(kept, line)
	}
	return strings.Join(kept, "\n")
}

func shellTarget(t target, keys []string) string {
	if path, _ := shellAssignment(t, keys); path != "" {
		return path
	}
	for _, name := range []string{".profile", ".bashrc", ".zshrc"} {
		path := filepath.Join(t.Home, name)
		if exists(path) {
			return path
		}
	}
	return filepath.Join(t.Home, ".profile")
}

func shellAssignment(t target, keys []string) (string, string) {
	for _, name := range []string{".profile", ".bashrc", ".zshrc", ".bash_profile"} {
		path := filepath.Join(t.Home, name)
		if !exists(path) {
			continue
		}
		if value, ok := lastAssignment(readFile(path), keys); ok {
			return path, value
		}
	}
	return "", ""
}

func lastAssignment(content string, keys []string) (string, bool) {
	want := map[string]bool{}
	for _, key := range keys {
		want[key] = true
	}
	var value string
	found := false
	for _, line := range strings.Split(content, "\n") {
		trim := strings.TrimSpace(line)
		if trim == "" || strings.HasPrefix(trim, "#") {
			continue
		}
		body := strings.TrimSpace(strings.TrimPrefix(trim, "export "))
		key, raw, ok := strings.Cut(body, "=")
		if !ok || !want[strings.TrimSpace(key)] {
			continue
		}
		value = unquote(strings.TrimSpace(raw))
		found = true
	}
	return value, found
}

func codexBaseURL(content string) string {
	provider := "openai"
	lines := strings.Split(content, "\n")
	for _, line := range lines {
		trim := strings.TrimSpace(line)
		if strings.HasPrefix(trim, "[") {
			break
		}
		if tomlKey(trim) == "model_provider" {
			provider = unquote(tomlValue(trim))
		}
	}
	section := "[model_providers." + provider + "]"
	active := false
	for _, line := range lines {
		trim := strings.TrimSpace(line)
		if strings.HasPrefix(trim, "[") && strings.HasSuffix(trim, "]") {
			active = trim == section
			continue
		}
		if active && tomlKey(trim) == "base_url" {
			return unquote(tomlValue(trim))
		}
	}
	return ""
}

func tomlKey(line string) string {
	key, _, ok := strings.Cut(line, "=")
	if !ok {
		return ""
	}
	return strings.TrimSpace(key)
}

func tomlValue(line string) string {
	_, value, _ := strings.Cut(line, "=")
	return strings.TrimSpace(value)
}

func yamlScalar(content, key string) string {
	for _, line := range strings.Split(content, "\n") {
		trim := strings.TrimSpace(line)
		if trim == "" || strings.HasPrefix(trim, "#") {
			continue
		}
		name, value, ok := strings.Cut(trim, ":")
		if ok && strings.TrimSpace(name) == key {
			return unquote(strings.TrimSpace(value))
		}
	}
	return ""
}

func continueEndpoint(content string, asJSON bool) string {
	if asJSON {
		var root map[string]any
		if unmarshalJSON([]byte(content), &root) != nil {
			return ""
		}
		models, _ := root["models"].([]any)
		for _, item := range models {
			obj, _ := item.(map[string]any)
			title, _ := obj["title"].(string)
			name, _ := obj["name"].(string)
			if title == continueTitle || name == continueTitle {
				base, _ := obj["apiBase"].(string)
				return base
			}
		}
		if len(models) > 0 {
			obj, _ := models[0].(map[string]any)
			base, _ := obj["apiBase"].(string)
			return base
		}
		return ""
	}
	if strings.Contains(content, continueTitle) {
		lines := strings.Split(content, "\n")
		anchor := -1
		for i, line := range lines {
			if strings.Contains(line, continueTitle) {
				anchor = i
				break
			}
		}
		if anchor >= 0 {
			limit := anchor + 8
			if limit > len(lines) {
				limit = len(lines)
			}
			for _, line := range lines[anchor:limit] {
				trim := strings.TrimSpace(line)
				if strings.HasPrefix(trim, "apiBase:") {
					return unquote(strings.TrimSpace(strings.TrimPrefix(trim, "apiBase:")))
				}
			}
		}
	}
	return yamlScalar(content, "apiBase")
}

func jsonString(content string, path ...string) string {
	if strings.TrimSpace(content) == "" {
		return ""
	}
	var root any
	if unmarshalJSON([]byte(content), &root) != nil {
		return ""
	}
	cur := root
	for _, key := range path {
		obj, ok := cur.(map[string]any)
		if !ok {
			return ""
		}
		cur = obj[key]
	}
	text, _ := cur.(string)
	return text
}

func jsonHasPrefix(content, prefix string) bool {
	var root map[string]any
	if unmarshalJSON([]byte(content), &root) != nil {
		return false
	}
	for key := range root {
		if key == prefix || strings.HasPrefix(key, prefix+".") {
			return true
		}
	}
	return false
}

func unmarshalJSON(content []byte, dest any) error {
	if err := json.Unmarshal(content, dest); err == nil {
		return nil
	}
	return json.Unmarshal(stripJSONC(content), dest)
}

func stripJSONC(content []byte) []byte {
	var b strings.Builder
	inString := false
	escape := false
	for i := 0; i < len(content); i++ {
		ch := content[i]
		if inString {
			b.WriteByte(ch)
			if escape {
				escape = false
				continue
			}
			if ch == '\\' {
				escape = true
				continue
			}
			if ch == '"' {
				inString = false
			}
			continue
		}
		if ch == '"' {
			inString = true
			b.WriteByte(ch)
			continue
		}
		if ch == '/' && i+1 < len(content) && content[i+1] == '/' {
			for i < len(content) && content[i] != '\n' {
				i++
			}
			if i < len(content) {
				b.WriteByte('\n')
			}
			continue
		}
		if ch == '/' && i+1 < len(content) && content[i+1] == '*' {
			i += 2
			for i+1 < len(content) && !(content[i] == '*' && content[i+1] == '/') {
				i++
			}
			i++
			continue
		}
		b.WriteByte(ch)
	}
	clean := b.String()
	var out strings.Builder
	inString, escape = false, false
	for i := 0; i < len(clean); i++ {
		ch := clean[i]
		if inString {
			out.WriteByte(ch)
			if escape {
				escape = false
			} else if ch == '\\' {
				escape = true
			} else if ch == '"' {
				inString = false
			}
			continue
		}
		if ch == '"' {
			inString = true
		}
		if ch == ',' {
			j := i + 1
			for j < len(clean) && strings.ContainsRune(" \t\r\n", rune(clean[j])) {
				j++
			}
			if j < len(clean) && (clean[j] == '}' || clean[j] == ']') {
				continue
			}
		}
		out.WriteByte(ch)
	}
	return []byte(out.String())
}

func marshalPretty(v any) ([]byte, error) {
	raw, err := json.MarshalIndent(v, "", "  ")
	if err != nil {
		return nil, err
	}
	return append(raw, '\n'), nil
}

func lookup(t target, name string) string {
	if t.Path == "" {
		return ""
	}
	names := []string{name}
	if t.GOOS == "windows" {
		names = []string{name + ".exe", name}
	}
	for _, dir := range filepath.SplitList(t.Path) {
		for _, candidate := range names {
			full := filepath.Join(dir, candidate)
			info, err := os.Stat(full)
			if err != nil || info.IsDir() {
				continue
			}
			if t.GOOS != "windows" && info.Mode()&0o111 == 0 {
				continue
			}
			return full
		}
	}
	return ""
}

func codeSettings(t target) string {
	switch t.GOOS {
	case "darwin":
		return filepath.Join(t.Home, "Library", "Application Support", "Code", "User", "settings.json")
	case "windows":
		return filepath.Join(t.Home, "AppData", "Roaming", "Code", "User", "settings.json")
	default:
		return filepath.Join(t.Home, ".config", "Code", "User", "settings.json")
	}
}

func zedSettings(t target) string {
	switch t.GOOS {
	case "darwin":
		return filepath.Join(t.Home, "Library", "Application Support", "Zed", "settings.json")
	case "windows":
		return filepath.Join(t.Home, "AppData", "Roaming", "Zed", "settings.json")
	default:
		return filepath.Join(t.Home, ".config", "zed", "settings.json")
	}
}

func extensionHits(t target, prefixes []string) []string {
	roots := []string{
		filepath.Join(t.Home, ".vscode", "extensions"),
		filepath.Join(t.Home, ".vscode-insiders", "extensions"),
		filepath.Join(t.Home, ".cursor", "extensions"),
	}
	var hits []string
	for _, root := range roots {
		entries, err := os.ReadDir(root)
		if err != nil {
			continue
		}
		for _, entry := range entries {
			name := entry.Name()
			for _, prefix := range prefixes {
				if name == prefix || strings.HasPrefix(name, prefix+"-") {
					hits = append(hits, filepath.Join(root, name))
				}
			}
		}
	}
	sort.Strings(hits)
	return hits
}

func jetBrainsDir(name string) bool {
	bases := []string{"Idea", "PyCharm", "GoLand", "WebStorm", "CLion", "PhpStorm", "Rider", "RubyMine", "DataGrip", "IntelliJ"}
	for _, base := range bases {
		if name == base || strings.HasPrefix(name, "."+base) || strings.HasPrefix(name, base) {
			if name == base || strings.HasPrefix(name, "."+base) {
				return true
			}
		}
	}
	for _, base := range bases {
		if strings.HasPrefix(name, "."+base) {
			return true
		}
	}
	return false
}

func exists(path string) bool {
	if path == "" {
		return false
	}
	_, err := os.Stat(path)
	return err == nil
}

func firstExisting(paths ...string) string {
	for _, path := range paths {
		if exists(path) {
			return path
		}
	}
	if len(paths) == 0 {
		return ""
	}
	return paths[0]
}

func readFile(path string) string {
	raw, err := os.ReadFile(path)
	if err != nil {
		return ""
	}
	return string(raw)
}

func envValue(environ []string, key string) string {
	prefix := key + "="
	for _, item := range environ {
		if strings.HasPrefix(item, prefix) {
			return strings.TrimPrefix(item, prefix)
		}
	}
	return ""
}

func unquote(value string) string {
	value = strings.TrimSpace(value)
	if len(value) >= 2 {
		if (value[0] == '"' && value[len(value)-1] == '"') || (value[0] == '\'' && value[len(value)-1] == '\'') {
			return value[1 : len(value)-1]
		}
	}
	if i := strings.Index(value, " #"); i >= 0 {
		value = strings.TrimSpace(value[:i])
	}
	return value
}

func underHome(home, path string) error {
	home = filepath.Clean(home)
	path = filepath.Clean(path)
	rel, err := filepath.Rel(home, path)
	if err != nil || rel == ".." || strings.HasPrefix(rel, ".."+string(filepath.Separator)) {
		return fmt.Errorf("refusing to edit %s outside the home directory", path)
	}
	if resolved, err := filepath.EvalSymlinks(path); err == nil {
		rel, err = filepath.Rel(home, resolved)
		if err != nil || rel == ".." || strings.HasPrefix(rel, ".."+string(filepath.Separator)) {
			return fmt.Errorf("refusing to edit %s outside the home directory", path)
		}
	}
	return nil
}

type managedState struct {
	ID            string   `json:"id"`
	Paths         []string `json:"paths"`
	ConductorBase string   `json:"conductor_base,omitempty"`
}

type fileSnap struct {
	Path    string `json:"path"`
	Existed bool   `json:"existed"`
	Mode    uint32 `json:"mode"`
	Body    []byte `json:"-"`
}

func backupRoot(t target) string {
	return filepath.Join(t.ConfigDir, "route-backups")
}

func managedPath(t target, id string) string {
	return filepath.Join(backupRoot(t), "tools", id+".json")
}

func snapPaths(t target, path string) (meta string, body string) {
	sum := sha256.Sum256([]byte(path))
	name := hex.EncodeToString(sum[:8])
	dir := filepath.Join(backupRoot(t), "files")
	return filepath.Join(dir, name+".json"), filepath.Join(dir, name+".orig")
}

func managedIDs(t target) (map[string]bool, error) {
	states, err := managedStates(t)
	if err != nil {
		return nil, err
	}
	out := map[string]bool{}
	for id := range states {
		out[id] = true
	}
	return out, nil
}

func managedStates(t target) (map[string]managedState, error) {
	dir := filepath.Join(backupRoot(t), "tools")
	entries, err := os.ReadDir(dir)
	if os.IsNotExist(err) {
		return map[string]managedState{}, nil
	}
	if err != nil {
		return nil, err
	}
	out := map[string]managedState{}
	for _, entry := range entries {
		if entry.IsDir() || !strings.HasSuffix(entry.Name(), ".json") {
			continue
		}
		raw, err := os.ReadFile(filepath.Join(dir, entry.Name()))
		if err != nil {
			return nil, err
		}
		var state managedState
		if err := json.Unmarshal(raw, &state); err != nil {
			return nil, err
		}
		out[state.ID] = state
	}
	return out, nil
}

func readManaged(t target, id string) (managedState, bool, error) {
	raw, err := os.ReadFile(managedPath(t, id))
	if os.IsNotExist(err) {
		return managedState{}, false, nil
	}
	if err != nil {
		return managedState{}, false, err
	}
	var state managedState
	if err := json.Unmarshal(raw, &state); err != nil {
		return managedState{}, false, err
	}
	return state, true, nil
}

func setManaged(t target, id string, paths []string) error {
	if err := os.MkdirAll(filepath.Join(backupRoot(t), "tools"), 0o700); err != nil {
		return err
	}
	_ = os.Chmod(backupRoot(t), 0o700)
	raw, err := marshalPretty(managedState{ID: id, Paths: paths, ConductorBase: t.ConductorBase})
	if err != nil {
		return err
	}
	return writeFile(managedPath(t, id), raw, 0o600)
}

func clearManaged(t target, id string) error {
	err := os.Remove(managedPath(t, id))
	if os.IsNotExist(err) {
		return nil
	}
	return err
}

func snapshotIfAbsent(t target, path string) error {
	meta, body := snapPaths(t, path)
	if exists(meta) {
		return nil
	}
	if err := os.MkdirAll(filepath.Dir(meta), 0o700); err != nil {
		return err
	}
	_ = os.Chmod(backupRoot(t), 0o700)
	info, err := os.Stat(path)
	snap := fileSnap{Path: path, Mode: uint32(0o644)}
	var raw []byte
	if err == nil {
		snap.Existed = true
		snap.Mode = uint32(info.Mode().Perm())
		raw, err = os.ReadFile(path)
		if err != nil {
			return err
		}
	} else if !os.IsNotExist(err) {
		return err
	}
	encoded, err := json.MarshalIndent(snap, "", "  ")
	if err != nil {
		return err
	}
	if err := writeFile(meta, append(encoded, '\n'), 0o600); err != nil {
		return err
	}
	return writeFile(body, raw, 0o600)
}

func readSnap(t target, path string) (fileSnap, bool, error) {
	meta, body := snapPaths(t, path)
	raw, err := os.ReadFile(meta)
	if os.IsNotExist(err) {
		return fileSnap{}, false, nil
	}
	if err != nil {
		return fileSnap{}, false, err
	}
	var snap fileSnap
	if err := json.Unmarshal(raw, &snap); err != nil {
		return fileSnap{}, false, err
	}
	if snap.Existed {
		snap.Body, err = os.ReadFile(body)
		if err != nil {
			return fileSnap{}, false, err
		}
	}
	return snap, true, nil
}

func restoreSnap(t target, path string, snap fileSnap) error {
	if !snap.Existed {
		err := os.Remove(path)
		if os.IsNotExist(err) {
			return nil
		}
		return err
	}
	return writeFile(path, snap.Body, os.FileMode(snap.Mode))
}

func removeSnap(t target, path string) error {
	meta, body := snapPaths(t, path)
	_ = os.Remove(meta)
	err := os.Remove(body)
	if os.IsNotExist(err) {
		return nil
	}
	return err
}

func snapMode(t target, path string) os.FileMode {
	snap, ok, err := readSnap(t, path)
	if err != nil || !ok || snap.Mode == 0 {
		return 0o644
	}
	return os.FileMode(snap.Mode)
}

func writeFile(path string, data []byte, mode os.FileMode) error {
	// Keep dotfile symlinks intact by atomically replacing their resolved target.
	if info, err := os.Lstat(path); err == nil && info.Mode()&os.ModeSymlink != 0 {
		resolved, err := filepath.EvalSymlinks(path)
		if err != nil {
			return err
		}
		path = resolved
	}
	if err := os.MkdirAll(filepath.Dir(path), 0o755); err != nil {
		return err
	}
	tmp := path + ".hypermesh-tmp"
	if err := os.WriteFile(tmp, data, mode); err != nil {
		return err
	}
	if err := os.Chmod(tmp, mode); err != nil {
		_ = os.Remove(tmp)
		return err
	}
	if err := os.Rename(tmp, path); err != nil {
		_ = os.Remove(tmp)
		return err
	}
	return nil
}

func unified(path, before, after string) string {
	if before == after {
		return ""
	}
	var b strings.Builder
	fmt.Fprintf(&b, "--- %s\n+++ %s\n", path, path)
	b.WriteString(lineDiff(before, after))
	return redactDiff(b.String())
}

func lineDiff(before, after string) string {
	b := strings.Split(before, "\n")
	a := strings.Split(after, "\n")
	prefix := 0
	for prefix < len(b) && prefix < len(a) && b[prefix] == a[prefix] {
		prefix++
	}
	suffix := 0
	for suffix < len(b)-prefix && suffix < len(a)-prefix && b[len(b)-1-suffix] == a[len(a)-1-suffix] {
		suffix++
	}
	var out strings.Builder
	for _, line := range b[prefix : len(b)-suffix] {
		out.WriteString("-")
		out.WriteString(line)
		out.WriteByte('\n')
	}
	for _, line := range a[prefix : len(a)-suffix] {
		out.WriteString("+")
		out.WriteString(line)
		out.WriteByte('\n')
	}
	return out.String()
}

var secretLine = regexp.MustCompile(`(?i)(api[_-]?key|token|secret|password|authorization)`)

func redactDiff(text string) string {
	lines := strings.Split(text, "\n")
	for i, line := range lines {
		if !secretLine.MatchString(line) {
			continue
		}
		lines[i] = redactValues(line)
	}
	return strings.Join(lines, "\n")
}

func redactValues(line string) string {
	cut := strings.Index(line, "=")
	if colon := strings.Index(line, ":"); colon >= 0 && (cut < 0 || colon < cut) {
		cut = colon
	}
	if cut < 0 {
		return line
	}
	return line[:cut+1] + " ***"
}
