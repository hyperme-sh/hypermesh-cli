package route

import (
	"encoding/json"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"testing"

	"github.com/pelletier/go-toml/v2"
	"gopkg.in/yaml.v3"
)

func TestMatrixCoversRequestedTools(t *testing.T) {
	t.Parallel()
	want := []string{
		"claude-code", "codex", "gemini", "aider", "opencode", "continue",
		"cline", "roo", "cursor", "vscode-copilot", "hypermesh-vscode",
		"zed", "jetbrains", "lmstudio", "ollama", "openai-env", "anthropic-env",
	}
	got := map[string]Tool{}
	for _, tool := range Matrix() {
		got[tool.ID] = tool
	}
	if len(got) != len(want) {
		t.Fatalf("matrix len %d", len(got))
	}
	for _, id := range want {
		tool, ok := got[id]
		if !ok {
			t.Fatalf("missing %s", id)
		}
		if tool.ConfigKey == "" || tool.Protocol == "" {
			t.Fatalf("%s incomplete", id)
		}
	}
	if got["gemini"].Supported || got["cursor"].Supported || got["ollama"].Supported || got["lmstudio"].Supported || got["jetbrains"].Supported || got["vscode-copilot"].Supported {
		t.Fatal("unsupported tools must stay unsupported")
	}
	if !got["claude-code"].Supported || got["claude-code"].Protocol != ProtoAnthropic {
		t.Fatal("claude-code should use the anthropic surface")
	}
	if !got["codex"].Supported || got["codex"].Protocol != ProtoOpenAI {
		t.Fatal("codex should use the openai surface")
	}
}

func TestDetectParsesFixtureHome(t *testing.T) {
	home := t.TempDir()
	pathDir := t.TempDir()
	for _, name := range []string{"claude", "codex", "gemini", "aider", "opencode", "cursor", "code", "zed", "ollama"} {
		writeExe(t, filepath.Join(pathDir, name))
	}
	mustWrite(t, filepath.Join(home, ".claude", "settings.json"), `{
  "env": {"ANTHROPIC_BASE_URL": "https://api.anthropic.com"}
}
`)
	mustWrite(t, filepath.Join(home, ".codex", "config.toml"), "model_provider = \"openai\"\n\n[model_providers.openai]\nbase_url = \"https://api.openai.com/v1\"\n")
	mustWrite(t, filepath.Join(home, ".aider.conf.yml"), "# keep\nopenai-api-base: https://example.invalid/v1\n")
	mustWrite(t, filepath.Join(home, ".gemini", "settings.json"), "{}\n")
	mustWrite(t, filepath.Join(home, ".config", "opencode", "opencode.json"), `{"provider":{"other":{"options":{"baseURL":"https://api.openai.com/v1"}}}}`)
	mustWrite(t, filepath.Join(home, ".continue", "config.json"), `{"models":[{"title":"Other","provider":"openai","apiBase":"https://api.openai.com/v1"}]}`)
	mustWrite(t, filepath.Join(home, ".config", "Code", "User", "settings.json"), `{
  "cline.openAiBaseUrl": "https://api.openai.com/v1",
  "hypermesh.apiBase": "https://chat.test.hyperme.sh",
  "github.copilot.enable": {"*": true}
}
`)
	mustWrite(t, filepath.Join(home, ".vscode", "extensions", "rooveterinaryinc.roo-cline-1.0.0", "package.json"), `{}`)
	mustWrite(t, filepath.Join(home, ".config", "zed", "settings.json"), `{"language_models":{"openai":{"api_url":"https://api.openai.com/v1"}}}`)
	mustWrite(t, filepath.Join(home, ".lmstudio", "settings.json"), `{}`)
	if err := os.MkdirAll(filepath.Join(home, ".ollama"), 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.MkdirAll(filepath.Join(home, ".config", "JetBrains", "PyCharm"), 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.Mkdir(filepath.Join(home, ".cursor"), 0o755); err != nil {
		t.Fatal(err)
	}
	mustWrite(t, filepath.Join(home, ".profile"), "export OPENAI_BASE_URL=https://api.openai.com/v1\nexport ANTHROPIC_BASE_URL=https://api.anthropic.com\n")

	opts := Options{
		Home: home, ConfigDir: filepath.Join(home, ".config", "hypermesh"),
		ConductorBase: "https://127.0.0.1:8787", Path: pathDir, GOOS: "linux",
		Environ: []string{"OLLAMA_HOST=127.0.0.1:11434"},
	}
	found, err := Detect(opts)
	if err != nil {
		t.Fatal(err)
	}
	byID := map[string]Finding{}
	for _, row := range found {
		byID[row.ID] = row
	}
	if !byID["claude-code"].Detected || byID["claude-code"].Endpoint != "https://api.anthropic.com" || byID["claude-code"].Redirected {
		t.Fatalf("claude %+v", byID["claude-code"])
	}
	if byID["codex"].Endpoint != "https://api.openai.com/v1" || !byID["codex"].Redirectable {
		t.Fatalf("codex %+v", byID["codex"])
	}
	if !byID["gemini"].Detected || byID["gemini"].Supported || byID["gemini"].Redirectable {
		t.Fatalf("gemini %+v", byID["gemini"])
	}
	if byID["aider"].Endpoint != "https://example.invalid/v1" {
		t.Fatalf("aider %+v", byID["aider"])
	}
	if !byID["opencode"].Detected || !byID["continue"].Detected {
		t.Fatal("opencode or continue missing")
	}
	if byID["cline"].Redirectable || byID["cline"].Endpoint != "" || !byID["roo"].Detected || byID["roo"].Supported {
		t.Fatalf("editors cline=%+v roo=%+v", byID["cline"], byID["roo"])
	}
	if !byID["cursor"].Detected || byID["cursor"].Supported {
		t.Fatalf("cursor %+v", byID["cursor"])
	}
	if !byID["vscode-copilot"].Detected || byID["vscode-copilot"].Supported {
		t.Fatalf("copilot %+v", byID["vscode-copilot"])
	}
	if byID["hypermesh-vscode"].Endpoint != "https://chat.test.hyperme.sh" {
		t.Fatalf("extension %+v", byID["hypermesh-vscode"])
	}
	if !byID["zed"].Detected || !byID["jetbrains"].Detected || !byID["lmstudio"].Detected || !byID["ollama"].Detected {
		t.Fatal("expected local apps")
	}
	if byID["ollama"].Redirectable || byID["lmstudio"].Redirectable || byID["jetbrains"].Redirectable {
		t.Fatal("servers and jetbrains are not redirectable")
	}
	if byID["openai-env"].Endpoint != "https://api.openai.com/v1" || byID["anthropic-env"].Endpoint != "https://api.anthropic.com" {
		t.Fatalf("env openai=%+v anthropic=%+v", byID["openai-env"], byID["anthropic-env"])
	}
	raw, err := jsonMarshal(found)
	if err != nil {
		t.Fatal(err)
	}
	for _, key := range []string{`"id"`, `"protocol"`, `"supported"`, `"config_key"`, `"redirected"`} {
		if !strings.Contains(string(raw), key) {
			t.Fatalf("json missing %s", key)
		}
	}
}

func TestEnableDisableRoundTrip(t *testing.T) {
	home := t.TempDir()
	configDir := filepath.Join(home, ".config", "hypermesh")
	aiderPath := filepath.Join(home, ".continue", "config.yaml")
	original := []byte("models:\n  - name: Hypermesh Conductor\n    provider: openai\n    apiBase: https://example.invalid/v1\n    model: demo\n")
	mustWrite(t, aiderPath, string(original))
	mustWrite(t, filepath.Join(home, ".claude", "settings.json"), "{\n  \"theme\": \"dark\"\n}\n")
	claudeOriginal, err := os.ReadFile(filepath.Join(home, ".claude", "settings.json"))
	if err != nil {
		t.Fatal(err)
	}
	opts := Options{
		Home: home, ConfigDir: configDir, ConductorBase: "https://127.0.0.1:8787", GOOS: "linux",
		Environ: []string{"HYPERMESH_API_KEY=sk-live-secret-value"},
	}

	dry, err := Enable(opts, "continue", true)
	if err != nil {
		t.Fatal(err)
	}
	if dry.Action != "dry-run" || !strings.Contains(dry.Changes[0].Diff, "configuration content redacted") {
		t.Fatalf("dry-run %+v", dry)
	}
	if _, err := os.Stat(filepath.Join(configDir, "route-backups")); !os.IsNotExist(err) {
		t.Fatal("dry-run wrote a backup")
	}
	got, err := os.ReadFile(aiderPath)
	if err != nil || string(got) != string(original) {
		t.Fatalf("dry-run changed file %q", got)
	}

	if _, err := Enable(opts, "continue", false); err != nil {
		t.Fatal(err)
	}
	enabled, err := os.ReadFile(aiderPath)
	if err != nil {
		t.Fatal(err)
	}
	if !strings.Contains(string(enabled), "apiBase: https://127.0.0.1:8787/v1") || !strings.Contains(string(enabled), "model: demo") {
		t.Fatalf("enabled %s", enabled)
	}
	if strings.Contains(string(enabled), "sk-live-secret-value") {
		t.Fatal("secret pasted into aider config")
	}
	again, err := Enable(opts, "continue", false)
	if err != nil {
		t.Fatal(err)
	}
	if again.Action != "unchanged" {
		t.Fatalf("second enable %+v", again)
	}
	second, err := os.ReadFile(aiderPath)
	if err != nil || string(second) != string(enabled) {
		t.Fatal("enable was not idempotent")
	}

	if _, err := Enable(opts, "claude-code", false); err != nil {
		t.Fatal(err)
	}
	claude, err := os.ReadFile(filepath.Join(home, ".claude", "settings.json"))
	if err != nil {
		t.Fatal(err)
	}
	if !strings.Contains(string(claude), "https://127.0.0.1:8787") || !strings.Contains(string(claude), "printenv HYPERMESH_API_KEY") {
		t.Fatalf("claude %s", claude)
	}
	if strings.Contains(string(claude), "sk-live-secret-value") {
		t.Fatal("secret pasted into claude settings")
	}
	info, err := os.Stat(filepath.Join(configDir, "route-backups"))
	if err != nil {
		t.Fatal(err)
	}
	if info.Mode().Perm() != 0o700 {
		t.Fatalf("backup dir mode %o", info.Mode().Perm())
	}
	var backupMode os.FileMode
	err = filepath.Walk(filepath.Join(configDir, "route-backups", "files"), func(path string, st os.FileInfo, walkErr error) error {
		if walkErr != nil || st.IsDir() || !strings.HasSuffix(path, ".orig") {
			return walkErr
		}
		backupMode = st.Mode().Perm()
		return nil
	})
	if err != nil || backupMode != 0o600 {
		t.Fatalf("backup mode %o err %v", backupMode, err)
	}

	if _, err := Disable(opts, "continue", false); err != nil {
		t.Fatal(err)
	}
	restored, err := os.ReadFile(aiderPath)
	if err != nil {
		t.Fatal(err)
	}
	if string(restored) != string(original) {
		t.Fatalf("aider restore\n got %q\nwant %q", restored, original)
	}
	if _, err := Disable(opts, "claude-code", false); err != nil {
		t.Fatal(err)
	}
	restoredClaude, err := os.ReadFile(filepath.Join(home, ".claude", "settings.json"))
	if err != nil {
		t.Fatal(err)
	}
	if string(restoredClaude) != string(claudeOriginal) {
		t.Fatalf("claude restore\n got %q\nwant %q", restoredClaude, claudeOriginal)
	}
	unchanged, err := Disable(opts, "continue", false)
	if err != nil || unchanged.Action != "unchanged" {
		t.Fatalf("second disable %+v %v", unchanged, err)
	}
}

func TestCodexAndShellRoundTrip(t *testing.T) {
	home := t.TempDir()
	codexPath := filepath.Join(home, ".codex", "config.toml")
	codexOriginal := []byte("model_provider = \"openai\"\n\n[model_providers.openai]\nbase_url = \"https://api.openai.com/v1\"\n")
	mustWrite(t, codexPath, string(codexOriginal))
	profile := filepath.Join(home, ".profile")
	profileOriginal := []byte("# profile\nexport OPENAI_BASE_URL=https://api.openai.com/v1\nexport OPENAI_API_KEY=sk-already-there\n")
	mustWrite(t, profile, string(profileOriginal))
	opts := Options{Home: home, ConfigDir: t.TempDir(), ConductorBase: "https://127.0.0.1:8787", GOOS: "linux"}

	dry, err := Enable(opts, "openai-env", true)
	if err != nil {
		t.Fatal(err)
	}
	if strings.Contains(dry.Changes[0].Diff, "sk-already-there") {
		t.Fatalf("diff leaked the key:\n%s", dry.Changes[0].Diff)
	}
	if _, err := Enable(opts, "codex", false); err != nil {
		t.Fatal(err)
	}
	if _, err := Enable(opts, "openai-env", false); err != nil {
		t.Fatal(err)
	}
	codexNow, _ := os.ReadFile(codexPath)
	var codexConfig map[string]any
	if err := toml.Unmarshal(codexNow, &codexConfig); err != nil {
		t.Fatal(err)
	}
	provider := codexConfig["model_providers"].(map[string]any)["hypermesh"].(map[string]any)
	if provider["base_url"] != "https://127.0.0.1:8787/v1" || provider["env_key"] != "HYPERMESH_API_KEY" || provider["wire_api"] != "responses" {
		t.Fatalf("codex %s", codexNow)
	}

	if strings.Contains(string(codexNow), "sk-") {
		t.Fatal("codex gained a secret")
	}
	shellNow, _ := os.ReadFile(profile)
	if strings.Contains(string(shellNow), "sk-already-there") || !strings.Contains(string(shellNow), `${HYPERMESH_API_KEY}`) || !strings.Contains(string(shellNow), `OPENAI_BASE_URL='https://127.0.0.1:8787/v1'`) {
		t.Fatalf("shell %s", shellNow)
	}
	found, err := Detect(opts)
	if err != nil {
		t.Fatal(err)
	}
	for _, row := range found {
		if row.ID == "codex" && (!row.Redirected || !row.Managed) {
			t.Fatalf("codex status %+v", row)
		}
		if row.ID == "openai-env" && !row.Redirected {
			t.Fatalf("env status %+v", row)
		}
	}
	if _, err := Disable(opts, "codex", false); err != nil {
		t.Fatal(err)
	}
	if _, err := Disable(opts, "openai-env", false); err != nil {
		t.Fatal(err)
	}
	if got, _ := os.ReadFile(codexPath); string(got) != string(codexOriginal) {
		t.Fatalf("codex restore %q", got)
	}
	if got, _ := os.ReadFile(profile); string(got) != string(profileOriginal) {
		t.Fatalf("profile restore %q", got)
	}
}

func TestUnsupportedAndAll(t *testing.T) {
	home := t.TempDir()
	gemini := filepath.Join(home, ".gemini", "settings.json")
	mustWrite(t, gemini, "{}\n")
	aider := filepath.Join(home, ".aider.conf.yml")
	mustWrite(t, aider, "openai-api-base: https://example.invalid/v1\n")
	opts := Options{Home: home, ConfigDir: t.TempDir(), ConductorBase: "https://127.0.0.1:8787", GOOS: "linux"}
	if _, err := Enable(opts, "gemini", false); err == nil {
		t.Fatal("expected unsupported error")
	}
	got, err := os.ReadFile(gemini)
	if err != nil || string(got) != "{}\n" {
		t.Fatalf("gemini changed %q", got)
	}
	results, err := EnableAll(opts, false)
	if err != nil {
		t.Fatal(err)
	}
	sawGemini := false
	sawAider := false
	for _, result := range results {
		if result.Tool == "gemini" && result.Action == "unsupported" {
			sawGemini = true
		}
		if result.Tool == "aider" && result.Action == "unsupported" {
			sawAider = true
		}
	}
	if !sawGemini || !sawAider {
		t.Fatalf("results %+v", results)
	}
	if _, err := DisableAll(opts, false); err != nil {
		t.Fatal(err)
	}
	restored, err := os.ReadFile(aider)
	if err != nil || string(restored) != "openai-api-base: https://example.invalid/v1\n" {
		t.Fatalf("aider %q", restored)
	}
}

func TestContinueYAMLAndZed(t *testing.T) {
	home := t.TempDir()
	yamlPath := filepath.Join(home, ".continue", "config.yaml")
	original := []byte("models:\n  - name: Other\n    provider: openai\n    apiBase: https://api.openai.com/v1\n")
	mustWrite(t, yamlPath, string(original))
	zed := filepath.Join(home, ".config", "zed", "settings.json")
	zedOriginal := []byte("{\"theme\":\"dark\"}\n")
	mustWrite(t, zed, string(zedOriginal))
	opts := Options{Home: home, ConfigDir: t.TempDir(), ConductorBase: "https://127.0.0.1:8787", GOOS: "linux"}
	if _, err := Enable(opts, "continue", false); err != nil {
		t.Fatal(err)
	}
	if _, err := Enable(opts, "zed", false); err == nil {
		t.Fatal("Zed credential reuse must be refused")
	}
	got, err := os.ReadFile(yamlPath)
	if err != nil {
		t.Fatal(err)
	}
	if !strings.Contains(string(got), "name: Hypermesh Conductor") || !strings.Contains(string(got), "apiBase: https://127.0.0.1:8787/v1") || !strings.Contains(string(got), "name: Other") || strings.Contains(string(got), "sk-") {
		t.Fatalf("continue %s", got)
	}
	zedNow, err := os.ReadFile(zed)
	if err != nil {
		t.Fatal(err)
	}
	if string(zedNow) != string(zedOriginal) {
		t.Fatal("Zed config changed")
	}

	if _, err := Disable(opts, "continue", false); err != nil {
		t.Fatal(err)
	}
	if _, err := Disable(opts, "zed", false); err != nil {
		t.Fatal(err)
	}
	if restored, _ := os.ReadFile(yamlPath); string(restored) != string(original) {
		t.Fatalf("yaml restore %q", restored)
	}
	if restored, _ := os.ReadFile(zed); string(restored) != string(zedOriginal) {
		t.Fatalf("zed restore %q", restored)
	}
}

func writeExe(t *testing.T, path string) {
	t.Helper()
	if err := os.WriteFile(path, []byte("#!/bin/sh\n"), 0o755); err != nil {
		t.Fatal(err)
	}
}

func mustWrite(t *testing.T, path, content string) {
	t.Helper()
	if err := os.MkdirAll(filepath.Dir(path), 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(path, []byte(content), 0o644); err != nil {
		t.Fatal(err)
	}
}

func jsonMarshal(v any) ([]byte, error) {
	return marshalPretty(v)
}

func TestJSONCTrailingCommasPreserveStrings(t *testing.T) {
	input := []byte(`{
 // settings with delimiter text
 "prompt": ",}", "arrayText": ",]", "quoted": "\\\",}",
 "items": ["keep,]", /* trailing */],
}`)
	var got map[string]any
	if err := unmarshalJSON(input, &got); err != nil {
		t.Fatal(err)
	}
	if got["prompt"] != ",}" || got["arrayText"] != ",]" || got["quoted"] != `\",}` {
		t.Fatalf("string contents changed: %#v", got)
	}
	if got["items"].([]any)[0] != "keep,]" {
		t.Fatal("array string changed")
	}
}

func TestCodexEquivalentProviderHeaders(t *testing.T) {
	for _, header := range []string{`[model_providers.hypermesh] # custom`, `[model_providers."hypermesh"]`, `[ "model_providers" . 'hypermesh' ]`} {
		input := header + "\nbase_url = \"https://old/v1\"\n[profiles.work] # preserved\nmodel = \"keep\"\n"
		got, err := editCodex(input, "https://new/v1")
		if err != nil {
			t.Fatal(err)
		}
		var root map[string]any
		if err := toml.Unmarshal(got, &root); err != nil {
			t.Fatal(err)
		}
		if codexBaseURL(string(got)) != "https://new/v1" || root["profiles"].(map[string]any)["work"].(map[string]any)["model"] != "keep" {
			t.Fatalf("lost config: %s", got)
		}
	}
}

func TestContinueYAMLLists(t *testing.T) {
	for _, input := range []string{"models: [] # keep comment\n", "models: [{name: Other, provider: openai}]\n", "models: # keep comment\n- name: Other\n  provider: openai\n", "models:\n    - name: Other\n      provider: openai\n"} {
		got, err := editContinueYAML(input, "https://new/v1")
		if err != nil {
			t.Fatal(err)
		}
		var config struct {
			Models []map[string]string `yaml:"models"`
		}
		if err := yaml.Unmarshal(got, &config); err != nil {
			t.Fatalf("invalid YAML: %v\n%s", err, got)
		}
		want := 1
		if strings.Contains(input, "Other") {
			want = 2
		}
		if len(config.Models) != want || config.Models[want-1]["apiBase"] != "https://new/v1" {
			t.Fatalf("lost models: %s", got)
		}
		again, err := editContinueYAML(string(got), "https://updated/v1")
		if err != nil {
			t.Fatal(err)
		}
		if err := yaml.Unmarshal(again, &config); err != nil || len(config.Models) != want {
			t.Fatalf("duplicate model: %s", again)
		}
	}
}

func TestSymlinkedConfigRoundTrip(t *testing.T) {
	home := t.TempDir()
	target := filepath.Join(home, "dotfiles", "aider.yml")
	original := "models:\n  - name: Hypermesh Conductor\n    provider: openai\n    apiBase: https://original/v1\n"
	mustWrite(t, target, original)
	link := filepath.Join(home, ".continue", "config.yaml")
	if err := os.MkdirAll(filepath.Dir(link), 0700); err != nil {
		t.Fatal(err)
	}
	if err := os.Symlink(target, link); err != nil {
		t.Fatal(err)
	}
	opts := Options{Home: home, ConfigDir: t.TempDir(), ConductorBase: "https://localhost:8787", GOOS: "linux"}
	if _, err := Enable(opts, "continue", false); err != nil {
		t.Fatal(err)
	}
	if got, err := os.Readlink(link); err != nil || got != target {
		t.Fatal("symlink replaced")
	}
	if !strings.Contains(readFile(target), "localhost:8787") {
		t.Fatal("target not updated")
	}
	if _, err := Disable(opts, "continue", false); err != nil {
		t.Fatal(err)
	}
	if got, err := os.Readlink(link); err != nil || got != target {
		t.Fatal("symlink not preserved")
	}
	if readFile(target) != original {
		t.Fatal("original target not restored")
	}
}

func TestShellConditionalAssignmentRemainsValid(t *testing.T) {
	input := "if [ -n \"$USE_OPENAI\" ]; then\n  export OPENAI_BASE_URL=https://old\nfi\n"
	got := editShell(input, "openai-env", []string{"OPENAI_BASE_URL"}, []string{"export OPENAI_BASE_URL=https://new"})
	cmd := exec.Command("sh", "-n")
	cmd.Stdin = strings.NewReader(got)
	if output, err := cmd.CombinedOutput(); err != nil {
		t.Fatalf("invalid shell: %v %s\n%s", err, output, got)
	}
}

func TestSharedSymlinkConfigsRestoreOneOriginal(t *testing.T) {
	for _, firstDisable := range []string{"claude-code", "opencode"} {
		home := t.TempDir()
		target := filepath.Join(home, ".config", "opencode", "opencode.json")
		alias := filepath.Join(home, ".claude", "settings.json")
		original := `{"theme":"keep"}`
		mustWrite(t, target, original)
		if err := os.MkdirAll(filepath.Dir(alias), 0700); err != nil {
			t.Fatal(err)
		}
		if err := os.Symlink(target, alias); err != nil {
			t.Fatal(err)
		}
		opts := Options{Home: home, ConfigDir: t.TempDir(), ConductorBase: "https://first:1234", GOOS: "linux"}
		if _, err := Enable(opts, "claude-code", false); err != nil {
			t.Fatal(err)
		}
		opts.ConductorBase = "https://second:2345"
		if _, err := Enable(opts, "opencode", false); err != nil {
			t.Fatal(err)
		}
		opts.ConductorBase = "https://unrelated:3456"
		if _, err := Disable(opts, firstDisable, false); err != nil {
			t.Fatal(err)
		}
		remaining := "opencode"
		if firstDisable == "claude-code" {
			if jsonString(readFile(target), "provider", "hypermesh", "options", "baseURL") != "https://second:2345/v1" {
				t.Fatal("lost other tool")
			}
		} else {
			remaining = "claude-code"
			if jsonString(readFile(target), "env", "ANTHROPIC_BASE_URL") != "https://first:1234" {
				t.Fatal("lost other target")
			}
		}
		if _, err := Disable(opts, remaining, false); err != nil {
			t.Fatal(err)
		}
		if readFile(target) != original {
			t.Fatal("did not restore original")
		}
		if dest, err := os.Readlink(alias); err != nil || dest != target {
			t.Fatal("symlink lost")
		}
	}
}

func TestDiffRedactsLiteralFallbackSecrets(t *testing.T) {
	for _, input := range []string{
		`export OPENAI_API_KEY="${OPENAI_API_KEY:-sk-sensitive-fallback}"`,
		`"apiKey": "{env:KEY}-sk-sensitive-fallback",`,
		`apiKeyHelper = "printenv KEY; echo sk-sensitive-fallback"`,
	} {
		for _, diff := range []string{unified("config", input, ""), unified("config", "", input)} {
			if strings.Contains(diff, "sk-sensitive-fallback") {
				t.Fatalf("secret leaked: %s", diff)
			}
		}
	}
}

func TestMultilineShellAssignmentLeavesConfigUnchanged(t *testing.T) {
	for _, value := range []string{"\"https://example/\\\npath\"", "'https://example/\npath'", "https://example/\\\npath"} {
		home := t.TempDir()
		path := filepath.Join(home, ".profile")
		original := "export OPENAI_BASE_URL=" + value + "\n"
		mustWrite(t, path, original)
		opts := Options{Home: home, ConfigDir: t.TempDir(), ConductorBase: "https://localhost:8787", GOOS: "linux"}
		if _, err := Enable(opts, "openai-env", false); err == nil {
			t.Fatal("accepted multiline assignment")
		}
		if readFile(path) != original {
			t.Fatal("config modified on error")
		}
	}
}
func TestMissingConfigUnderExternalSymlinkDenied(t *testing.T) {
	home := t.TempDir()
	outside := t.TempDir()
	if err := os.Symlink(outside, filepath.Join(home, ".claude")); err != nil {
		t.Fatal(err)
	}
	if err := underHome(home, filepath.Join(home, ".claude", "missing", "settings.json")); err == nil {
		t.Fatal("outside ancestor accepted")
	}
	inside := filepath.Join(home, "dotfiles")
	if err := os.Mkdir(inside, 0700); err != nil {
		t.Fatal(err)
	}
	if err := os.Symlink(inside, filepath.Join(home, ".aider")); err != nil {
		t.Fatal(err)
	}
	if err := underHome(home, filepath.Join(home, ".aider", "missing.yml")); err != nil {
		t.Fatal(err)
	}
}
func TestVSCodeOriginDetectedAsRedirected(t *testing.T) {
	home := t.TempDir()
	mustWrite(t, filepath.Join(home, ".config", "Code", "User", "settings.json"), `{"hypermesh.apiBase":"https://localhost:8787"}`)
	opts := Options{Home: home, ConfigDir: t.TempDir(), ConductorBase: "https://localhost:8787", GOOS: "linux"}
	rows, err := Detect(opts)
	if err != nil {
		t.Fatal(err)
	}
	for _, row := range rows {
		if row.ID == "hypermesh-vscode" {
			if !row.Redirected {
				t.Fatal("origin not detected")
			}
			return
		}
	}
	t.Fatal("extension missing")
}
func TestChangedShellTargetRestoresEveryPath(t *testing.T) {
	home := t.TempDir()
	bash := filepath.Join(home, ".bashrc")
	profile := filepath.Join(home, ".profile")
	original := "export OPENAI_BASE_URL=https://original\n"
	next := "export OPENAI_BASE_URL=https://second\n"
	mustWrite(t, bash, original)
	opts := Options{Home: home, ConfigDir: t.TempDir(), ConductorBase: "https://localhost:8787", GOOS: "linux"}
	if _, err := Enable(opts, "openai-env", false); err != nil {
		t.Fatal(err)
	}
	mustWrite(t, profile, next)
	if _, err := Enable(opts, "openai-env", false); err != nil {
		t.Fatal(err)
	}
	if _, err := Disable(opts, "openai-env", false); err != nil {
		t.Fatal(err)
	}
	if readFile(bash) != original || readFile(profile) != next {
		t.Fatal("earlier config not restored")
	}
}

func TestDiffRedactsUnknownCredentialFields(t *testing.T) {
	for _, field := range []string{"GITHUB_PAT", "private_key", "DATABASE_URL", "custom"} {
		input := `{\"` + field + `\":\"sensitive-value\"}`
		if strings.Contains(unified("config", input, input+"\n"), "sensitive-value") {
			t.Fatal("unrecognized secret leaked")
		}
	}
}

func TestContinueDetectsFlowAndBlockEndpoints(t *testing.T) {
	for _, content := range []string{
		"models: [{name: Other, apiBase: https://other}, {name: Hypermesh Conductor, apiBase: https://127.0.0.1:8787/v1}]",
		"models:\n  - name: Hypermesh Conductor\n    apiBase: https://127.0.0.1:8787/v1\n",
	} {
		if got := continueEndpoint(content, false); got != "https://127.0.0.1:8787/v1" {
			t.Fatalf("endpoint %q", got)
		}
	}
}

func TestAiderReplacesCompleteYAMLValue(t *testing.T) {
	for _, input := range []string{
		"openai-api-base: >-\n  https://api.openai.com/v1\nmodel: keep\n",
		"openai-api-base: |\n  https://api.openai.com/v1\nmodel: keep\n",
		"{openai-api-base: https://api.openai.com/v1, model: keep}",
	} {
		got, err := editAider(input, "https://127.0.0.1:8787/v1")
		if err != nil {
			t.Fatal(err)
		}
		if yamlScalar(string(got), "openai-api-base") != "https://127.0.0.1:8787/v1" || yamlScalar(string(got), "model") != "keep" {
			t.Fatalf("invalid replacement: %s", got)
		}
		if strings.Contains(string(got), "api.openai.com") {
			t.Fatal("old scalar retained")
		}
	}
}

func TestExtensionUsesOwningEditorSettings(t *testing.T) {
	for _, entry := range []struct{ dir, editor string }{{".cursor", "Cursor"}, {".vscode-insiders", "Code - Insiders"}, {".vscode", "Code"}} {
		for _, tool := range []struct{ id, extension, key string }{{"hypermesh-vscode", "fyberlabs.hypermesh-vscode", "hypermesh.apiBase"}} {
			home := t.TempDir()
			mustWrite(t, filepath.Join(home, entry.dir, "extensions", tool.extension+"-1.0.0", "package.json"), "{}")
			opts := Options{Home: home, ConfigDir: t.TempDir(), ConductorBase: "https://localhost:8787", GOOS: "linux"}
			if _, err := Enable(opts, tool.id, false); err != nil {
				t.Fatal(err)
			}
			expected := filepath.Join(home, ".config", entry.editor, "User", "settings.json")
			if jsonString(readFile(expected), tool.key) == "" {
				t.Fatalf("wrong editor for %s in %s", tool.id, entry.editor)
			}
			if entry.editor != "Code" && exists(filepath.Join(home, ".config", "Code", "User", "settings.json")) {
				t.Fatal("modified unrelated stable Code")
			}
			if _, err := Disable(opts, tool.id, false); err != nil {
				t.Fatal(err)
			}
			if exists(expected) {
				t.Fatal("new settings file not removed on disable")
			}
		}
	}
}

func TestToolRoutesRejectPlainHTTPWithoutWriting(t *testing.T) {
	home := t.TempDir()
	path := filepath.Join(home, ".claude", "settings.json")
	original := `{"theme":"dark"}`
	mustWrite(t, path, original)
	opts := Options{Home: home, ConfigDir: t.TempDir(), ConductorBase: "http://127.0.0.1:8787", GOOS: "linux"}
	for _, dry := range []bool{true, false} {
		if _, err := Enable(opts, "claude-code", dry); err == nil || !strings.Contains(err.Error(), "HTTPS") {
			t.Fatalf("insecure route accepted: %v", err)
		}
		if readFile(path) != original {
			t.Fatal("config changed despite rejected transport")
		}
	}
}

func TestUnsupportedExtensionStateIsNotWritten(t *testing.T) {
	for _, id := range []string{"cline", "roo"} {
		home := t.TempDir()
		opts := Options{Home: home, ConfigDir: t.TempDir(), ConductorBase: "https://conductor.example", GOOS: "linux"}
		result, err := Enable(opts, id, false)
		if err == nil || result.Action != "unsupported" {
			t.Fatalf("claimed supported: %s", id)
		}
		if exists(filepath.Join(home, ".config", "Code", "User", "settings.json")) {
			t.Fatal("created fake config")
		}
	}
}
func TestOpenCodeSelectedModelIsDeclared(t *testing.T) {
	got, err := editOpenCode([]byte(`{}`), "https://conductor.example/v1")
	if err != nil {
		t.Fatal(err)
	}
	if jsonString(string(got), "model") != "hypermesh/conductor" || jsonString(string(got), "provider", "hypermesh", "models", "conductor", "name") == "" {
		t.Fatalf("unresolved model: %s", got)
	}
}

func TestContinueSymlinkRetainsLogicalFormat(t *testing.T) {
	for _, item := range []struct{ logical, target, body string }{{"config.json", "dotfile", `{"models":[]}`}, {"config.yaml", "dotfile.json", "models: []\n"}} {
		home := t.TempDir()
		target := filepath.Join(home, item.target)
		link := filepath.Join(home, ".continue", item.logical)
		mustWrite(t, target, item.body)
		if err := os.MkdirAll(filepath.Dir(link), 0700); err != nil {
			t.Fatal(err)
		}
		if err := os.Symlink(target, link); err != nil {
			t.Fatal(err)
		}
		opts := Options{Home: home, ConfigDir: t.TempDir(), ConductorBase: "https://conductor.example", GOOS: "linux"}
		if _, err := Enable(opts, "continue", false); err != nil {
			t.Fatal(err)
		}
		raw := readFile(target)
		if item.logical == "config.json" && !json.Valid([]byte(raw)) {
			t.Fatalf("not JSON: %s", raw)
		}
		if continueEndpoint(raw, item.logical == "config.json") != "https://conductor.example/v1" {
			t.Fatalf("bad endpoint: %s", raw)
		}
		if _, err := Disable(opts, "continue", false); err != nil {
			t.Fatal(err)
		}
		if readFile(target) != item.body {
			t.Fatal("original not restored")
		}
	}
}
func TestSharedReplayUsesContinueLogicalJSONFormat(t *testing.T) {
	home := t.TempDir()
	target := filepath.Join(home, "dotfile")
	original := `{"models":[]}`
	mustWrite(t, target, original)
	for _, rel := range []string{".continue/config.json", ".claude/settings.json"} {
		link := filepath.Join(home, rel)
		if err := os.MkdirAll(filepath.Dir(link), 0700); err != nil {
			t.Fatal(err)
		}
		if err := os.Symlink(target, link); err != nil {
			t.Fatal(err)
		}
	}
	opts := Options{Home: home, ConfigDir: t.TempDir(), ConductorBase: "https://conductor.example", GOOS: "linux"}
	for _, id := range []string{"continue", "claude-code"} {
		if _, err := Enable(opts, id, false); err != nil {
			t.Fatal(err)
		}
	}
	if _, err := Disable(opts, "claude-code", false); err != nil {
		t.Fatal(err)
	}
	raw := readFile(target)
	if !json.Valid([]byte(raw)) || continueEndpoint(raw, true) != "https://conductor.example/v1" {
		t.Fatalf("bad replay: %s", raw)
	}
	if _, err := Disable(opts, "continue", false); err != nil {
		t.Fatal(err)
	}
	if readFile(target) != original {
		t.Fatal("original not restored")
	}
}

func TestContinueAnchoredEndpointRejectedWithoutWriting(t *testing.T) {
	home := t.TempDir()
	path := filepath.Join(home, ".continue", "config.yaml")
	original := "models:\n  - name: Hypermesh Conductor\n    provider: openai\n    apiBase: &shared https://old.example/v1\n  - name: Other\n    provider: openai\n    apiBase: *shared\n"
	// Use the same model title as the route editor.
	original = strings.Replace(original, "Hypermesh Conductor", continueTitle, 1)
	mustWrite(t, path, original)
	opts := Options{Home: home, ConfigDir: t.TempDir(), ConductorBase: "https://localhost:8787", GOOS: "linux"}
	if _, err := Enable(opts, "continue", false); err == nil || !strings.Contains(err.Error(), "anchored") {
		t.Fatalf("expected anchor rejection, got %v", err)
	}
	if readFile(path) != original {
		t.Fatal("rejected edit changed YAML")
	}
}

func TestSymlinkedHomeRouteRoundTrip(t *testing.T) {
	realHome := t.TempDir()
	home := filepath.Join(t.TempDir(), "home")
	if err := os.Symlink(realHome, home); err != nil {
		t.Skipf("symlinks unavailable: %v", err)
	}
	path := filepath.Join(home, ".continue", "config.yaml")
	original := "models:\n  - name: Hypermesh Conductor\n    provider: openai\n    apiBase: https://original/v1\n"
	mustWrite(t, path, original)
	opts := Options{Home: home, ConfigDir: filepath.Join(home, ".config", "hypermesh"), ConductorBase: "https://localhost:8787", GOOS: "linux"}
	if _, err := Enable(opts, "continue", false); err != nil {
		t.Fatal(err)
	}
	if !strings.Contains(readFile(path), "localhost:8787") {
		t.Fatal("route was not enabled")
	}
	if _, err := Disable(opts, "continue", false); err != nil {
		t.Fatal(err)
	}
	if readFile(path) != original {
		t.Fatal("original config not restored")
	}
	if err := underHome(home, filepath.Join(home, "missing", "config")); err != nil {
		t.Fatal(err)
	}
	if err := os.Symlink(t.TempDir(), filepath.Join(realHome, "outside")); err != nil {
		t.Fatal(err)
	}
	if err := underHome(home, filepath.Join(home, "outside", "config")); err == nil {
		t.Fatal("external symlink allowed")
	}
	if err := underHome(home, filepath.Join(realHome, ".continue", "config.yaml")); err == nil {
		t.Fatal("lexical containment check lost")
	}
}

func TestCredentialSelectorsRebound(t *testing.T) {
	claude, err := editClaude([]byte(`{"env":{"ANTHROPIC_API_KEY":"old-key","ANTHROPIC_AUTH_TOKEN":"old-token","CLAUDE_CODE_OAUTH_TOKEN":"old-oauth","ANTHROPIC_CUSTOM_HEADERS":"Authorization: old-header"},"apiKeyHelper":"printenv OTHER_KEY"}`), "https://conductor.example")
	if err != nil {
		t.Fatal(err)
	}
	if strings.Contains(string(claude), "old-") || strings.Contains(string(claude), "OTHER_KEY") || !strings.Contains(string(claude), "printenv HYPERMESH_API_KEY") {
		t.Fatalf("unsafe Claude credentials: %s", claude)
	}
	for _, asJSON := range []bool{true, false} {
		var got []byte
		if asJSON {
			got, err = editContinueJSON([]byte(`{"models":[{"title":"Hypermesh Conductor","apiKey":"old-provider-key"},{"title":"Other","apiKey":"other-provider-key"}]}`), "https://conductor.example/v1")
		} else {
			got, err = editContinueYAML("models:\n  - name: Hypermesh Conductor\n    apiKey: old-provider-key\n  - name: Other\n    apiKey: other-provider-key\n", "https://conductor.example/v1")
		}
		if err != nil {
			t.Fatal(err)
		}
		if strings.Contains(string(got), "old-provider-key") || !strings.Contains(string(got), "other-provider-key") || !strings.Contains(string(got), "${{ secrets.HYPERMESH_API_KEY }}") {
			t.Fatalf("unsafe Continue credentials: %s", got)
		}
	}
}

func TestAiderAnchorRejected(t *testing.T) {
	if _, err := editAider("openai-api-base: &shared https://old.example/v1\nother: *shared\n", "https://conductor.example/v1"); err == nil {
		t.Fatal("anchored Aider endpoint replaced")
	}
}

func TestUnsafeCredentialToolsRefusedWithoutWrites(t *testing.T) {
	for _, entry := range []struct{ id, path, body string }{
		{"aider", ".aider.conf.yml", "openai-api-base: https://api.openai.com/v1\nopenai-api-key: old-provider-key\n"},
		{"zed", ".config/zed/settings.json", `{"language_models":{"openai":{"api_url":"https://api.openai.com/v1"}}}`},
	} {
		home := t.TempDir()
		path := filepath.Join(home, entry.path)
		mustWrite(t, path, entry.body)
		opts := Options{Home: home, ConfigDir: t.TempDir(), ConductorBase: "https://conductor.example", GOOS: "linux"}
		if _, err := Enable(opts, entry.id, false); err == nil {
			t.Fatalf("%s allowed unsafe redirect", entry.id)
		}
		if readFile(path) != entry.body {
			t.Fatalf("%s changed config", entry.id)
		}
	}
}

func TestShellRouteURLsAreLiteral(t *testing.T) {
	for _, id := range []string{"openai-env", "anthropic-env"} {
		for _, endpoint := range []string{"https://host/$(printf INJECTED)", "https://host/`printf INJECTED`", "https://host/a'b;$HOME\nnext"} {
			target := target{origin: endpoint, openai: endpoint}
			body, err := editPath(target, id, ".profile", nil)
			if err != nil {
				t.Fatal(err)
			}
			key := "OPENAI_BASE_URL"
			if id == "anthropic-env" {
				key = "ANTHROPIC_BASE_URL"
			}
			cmd := exec.Command("sh", "-c", string(body)+"\nprintf '%s' \"$"+key+"\"")
			got, err := cmd.CombinedOutput()
			if err != nil || string(got) != endpoint {
				t.Fatalf("URL evaluated: %q, err=%v; want %q", got, err, endpoint)
			}
		}
	}
}
