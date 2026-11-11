package route

import (
	"fmt"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"testing"

	"github.com/hyperme-sh/hypermesh-cli/internal/api"
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
		ConductorBase: api.DefaultConductorBase, Path: pathDir, GOOS: "linux",
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
	if byID["cline"].Endpoint != "https://api.openai.com/v1" || !byID["roo"].Detected {
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
	aiderPath := filepath.Join(home, ".aider.conf.yml")
	original := []byte("# keep\nopenai-api-base: https://example.invalid/v1\nmodel: demo\n")
	mustWrite(t, aiderPath, string(original))
	mustWrite(t, filepath.Join(home, ".claude", "settings.json"), "{\n  \"theme\": \"dark\"\n}\n")
	claudeOriginal, err := os.ReadFile(filepath.Join(home, ".claude", "settings.json"))
	if err != nil {
		t.Fatal(err)
	}
	opts := Options{
		Home: home, ConfigDir: configDir, ConductorBase: api.DefaultConductorBase, GOOS: "linux",
		Environ: []string{"HYPERMESH_API_KEY=sk-live-secret-value"},
	}

	dry, err := Enable(opts, "aider", true)
	if err != nil {
		t.Fatal(err)
	}
	if dry.Action != "dry-run" || !strings.Contains(dry.Changes[0].Diff, "http://127.0.0.1:8787/v1") {
		t.Fatalf("dry-run %+v", dry)
	}
	if _, err := os.Stat(filepath.Join(configDir, "route-backups")); !os.IsNotExist(err) {
		t.Fatal("dry-run wrote a backup")
	}
	got, err := os.ReadFile(aiderPath)
	if err != nil || string(got) != string(original) {
		t.Fatalf("dry-run changed file %q", got)
	}

	if _, err := Enable(opts, "aider", false); err != nil {
		t.Fatal(err)
	}
	enabled, err := os.ReadFile(aiderPath)
	if err != nil {
		t.Fatal(err)
	}
	if !strings.Contains(string(enabled), "openai-api-base: http://127.0.0.1:8787/v1") || !strings.Contains(string(enabled), "model: demo") {
		t.Fatalf("enabled %s", enabled)
	}
	if strings.Contains(string(enabled), "sk-live-secret-value") {
		t.Fatal("secret pasted into aider config")
	}
	again, err := Enable(opts, "aider", false)
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
	if !strings.Contains(string(claude), "http://127.0.0.1:8787") || !strings.Contains(string(claude), "printenv HYPERMESH_API_KEY") {
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

	if _, err := Disable(opts, "aider", false); err != nil {
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
	unchanged, err := Disable(opts, "aider", false)
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
	opts := Options{Home: home, ConfigDir: t.TempDir(), ConductorBase: api.DefaultConductorBase, GOOS: "linux"}

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
	if !strings.Contains(string(codexNow), `base_url = "http://127.0.0.1:8787/v1"`) || !strings.Contains(string(codexNow), `env_key = "HYPERMESH_API_KEY"`) || !strings.Contains(string(codexNow), `wire_api = "responses"`) {
		t.Fatalf("codex %s", codexNow)
	}
	if strings.Contains(string(codexNow), "sk-") {
		t.Fatal("codex gained a secret")
	}
	shellNow, _ := os.ReadFile(profile)
	if strings.Contains(string(shellNow), "sk-already-there") || !strings.Contains(string(shellNow), `${HYPERMESH_API_KEY}`) || !strings.Contains(string(shellNow), `OPENAI_BASE_URL="http://127.0.0.1:8787/v1"`) {
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

func TestSharedVSCodeSettingsRoundTrip(t *testing.T) {
	home := t.TempDir()
	settings := filepath.Join(home, ".config", "Code", "User", "settings.json")
	original := []byte("{\n  // keep me\n  \"editor.fontSize\": 14,\n  \"cline.openAiBaseUrl\": \"https://api.openai.com/v1\"\n}\n")
	mustWrite(t, settings, string(original))
	mustWrite(t, filepath.Join(home, ".vscode", "extensions", "fyberlabs.hypermesh-vscode-0.1.0", "package.json"), `{}`)
	opts := Options{Home: home, ConfigDir: t.TempDir(), ConductorBase: api.DefaultConductorBase, GOOS: "linux"}
	if _, err := Enable(opts, "cline", false); err != nil {
		t.Fatal(err)
	}
	if _, err := Enable(opts, "hypermesh-vscode", false); err != nil {
		t.Fatal(err)
	}
	both, err := os.ReadFile(settings)
	if err != nil {
		t.Fatal(err)
	}
	if !strings.Contains(string(both), "http://127.0.0.1:8787/v1") || !strings.Contains(string(both), "http://127.0.0.1:8787") {
		t.Fatalf("settings %s", both)
	}
	if _, err := Disable(opts, "hypermesh-vscode", false); err != nil {
		t.Fatal(err)
	}
	clineOnly, err := os.ReadFile(settings)
	if err != nil {
		t.Fatal(err)
	}
	if strings.Contains(string(clineOnly), "hypermesh.apiBase") || !strings.Contains(string(clineOnly), "http://127.0.0.1:8787/v1") {
		t.Fatalf("after vscode disable %s", clineOnly)
	}
	if _, err := Disable(opts, "cline", false); err != nil {
		t.Fatal(err)
	}
	restored, err := os.ReadFile(settings)
	if err != nil {
		t.Fatal(err)
	}
	if string(restored) != string(original) {
		t.Fatalf("settings restore\n got %q\nwant %q", restored, original)
	}
}

func TestUnsupportedAndAll(t *testing.T) {
	home := t.TempDir()
	gemini := filepath.Join(home, ".gemini", "settings.json")
	mustWrite(t, gemini, "{}\n")
	aider := filepath.Join(home, ".aider.conf.yml")
	mustWrite(t, aider, "openai-api-base: https://example.invalid/v1\n")
	opts := Options{Home: home, ConfigDir: t.TempDir(), ConductorBase: api.DefaultConductorBase, GOOS: "linux"}
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
		if result.Tool == "aider" && result.Action == "enabled" {
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
	opts := Options{Home: home, ConfigDir: t.TempDir(), ConductorBase: api.DefaultConductorBase, GOOS: "linux"}
	if _, err := Enable(opts, "continue", false); err != nil {
		t.Fatal(err)
	}
	if _, err := Enable(opts, "zed", false); err != nil {
		t.Fatal(err)
	}
	got, err := os.ReadFile(yamlPath)
	if err != nil {
		t.Fatal(err)
	}
	if !strings.Contains(string(got), "name: Hypermesh Conductor") || !strings.Contains(string(got), "apiBase: http://127.0.0.1:8787/v1") || !strings.Contains(string(got), "name: Other") || strings.Contains(string(got), "sk-") {
		t.Fatalf("continue %s", got)
	}
	zedNow, err := os.ReadFile(zed)
	if err != nil {
		t.Fatal(err)
	}
	if !strings.Contains(string(zedNow), `"api_url": "http://127.0.0.1:8787/v1"`) || !strings.Contains(string(zedNow), `"api_url": "http://127.0.0.1:8787"`) {
		t.Fatalf("zed %s", zedNow)
	}
	if strings.Contains(string(zedNow), "max_tokens") || strings.Contains(string(zedNow), "tok/s") {
		t.Fatal("zed config invented a token figure")
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

func TestCodexTableCommentsDoNotDuplicateProvider(t *testing.T) {
	input := "[model_providers.hypermesh] # custom\nbase_url = \"http://old/v1\"\n[profiles.work] # preserved\nmodel = \"keep\"\n"
	got := editCodex(input, "http://new/v1")
	if strings.Count(got, "[model_providers.hypermesh]") != 1 || strings.Contains(got, "http://old") || !strings.Contains(got, "[profiles.work] # preserved\nmodel = \"keep\"") {
		t.Fatalf("invalid replacement: %s", got)
	}
	if tomlTableHeader(`[profiles."a#b"] # note`) != `[profiles."a#b"]` {
		t.Fatal("quoted hash treated as comment")
	}
}

func TestContinueYAMLUsesExistingSequenceIndent(t *testing.T) {
	for _, indent := range []string{"", "  ", "    "} {
		t.Run(fmt.Sprintf("indent-%d", len(indent)), func(t *testing.T) {
			old := indent + "- name: Other\n" + indent + "  provider: openai\n"
			got := editContinueYAML("models:\n# keep comment\n"+old+"context: []\n", "http://new/v1")
			if !strings.Contains(got, "models:\n"+indent+"- name: Hypermesh Conductor\n"+indent+"  provider: openai\n") || !strings.Contains(got, old+"context: []\n") {
				t.Fatalf("mixed indentation: %s", got)
			}
		})
	}
}

func TestShellConditionalAssignmentRemainsValid(t *testing.T) {
	input := "if [ -n \"$USE_OPENAI\" ]; then\n  export OPENAI_BASE_URL=http://old\nfi\n"
	got := editShell(input, "openai-env", []string{"OPENAI_BASE_URL"}, []string{"export OPENAI_BASE_URL=http://new"})
	cmd := exec.Command("sh", "-n")
	cmd.Stdin = strings.NewReader(got)
	if output, err := cmd.CombinedOutput(); err != nil {
		t.Fatalf("invalid shell: %v %s\n%s", err, output, got)
	}
}

func TestSharedSettingsRetainIndependentTargets(t *testing.T) {
	home := t.TempDir()
	settings := filepath.Join(home, ".config", "Code", "User", "settings.json")
	mustWrite(t, settings, `{"cline.openAiBaseUrl":"http://original/v1"}`)
	mustWrite(t, filepath.Join(home, ".vscode", "extensions", "fyberlabs.hypermesh-vscode-0.1.0", "package.json"), `{}`)
	opts := Options{Home: home, ConfigDir: t.TempDir(), ConductorBase: "http://first:1234", GOOS: "linux"}
	if _, err := Enable(opts, "cline", false); err != nil {
		t.Fatal(err)
	}
	opts.ConductorBase = "http://second:2345"
	if _, err := Enable(opts, "hypermesh-vscode", false); err != nil {
		t.Fatal(err)
	}
	opts.ConductorBase = "http://unrelated:3456"
	if _, err := Disable(opts, "hypermesh-vscode", false); err != nil {
		t.Fatal(err)
	}
	got := readFile(settings)
	if jsonString(got, "cline.openAiBaseUrl") != "http://first:1234/v1" || strings.Contains(got, "hypermesh.apiBase") {
		t.Fatalf("remaining tool retargeted: %s", got)
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
