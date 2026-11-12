package route

import (
	"encoding/json"
	"os"
	"path/filepath"
	"strings"
	"testing"
)

func TestCapturedCodexBackupProfileAndRestore(t *testing.T) {
	home := t.TempDir()
	path := filepath.Join(home, ".codex", "config.toml")
	original := `model = "original-model"
model_provider = "private"
[model_providers.private]
name = "Provider"
base_url = "https://provider.example/v1"
env_key = "MY_PROVIDER_KEY"
wire_api = "responses"
`
	mustWrite(t, path, original)
	opts := Options{Home: home, ConfigDir: filepath.Join(home, ".config", "hypermesh"), ConductorBase: "https://localhost:8787", Environ: []string{"MY_PROVIDER_KEY=do-not-copy-this-value"}, PreserveProvider: true}
	dry, err := Enable(opts, "codex", true)
	if err != nil || len(dry.Changes) != 2 {
		t.Fatal(dry, err)
	}
	if readFile(path) != original {
		t.Fatal("dry run modified file")
	}
	for i := 0; i < 2; i++ {
		if _, err := Enable(opts, "codex", false); err != nil {
			t.Fatal(err)
		}
	}
	updated := readFile(path)
	if !strings.Contains(updated, "/capture/codex/v1") || !strings.Contains(updated, "MY_PROVIDER_KEY") || strings.Contains(updated, "HYPERMESH_API_KEY") {
		t.Fatal(updated)
	}
	profilePath := filepath.Join(opts.ConfigDir, "conductor", "captures", "codex.json")
	profile := readFile(profilePath)
	if strings.Contains(profile, "do-not-copy") || strings.Contains(profile, "MY_PROVIDER_KEY") {
		t.Fatal("credential serialized")
	}
	var obj map[string]any
	if json.Unmarshal([]byte(profile), &obj) != nil || obj["base_url"] != "https://provider.example/v1" || obj["direct"] != true {
		t.Fatal(profile)
	}
	if _, err := Disable(opts, "codex", false); err != nil {
		t.Fatal(err)
	}
	if readFile(path) != original {
		t.Fatal("original config not restored")
	}
	if _, err := os.Stat(profilePath); !os.IsNotExist(err) {
		t.Fatal("profile not removed", err)
	}
}

func TestCapturedEditorsPreserveProviderReferences(t *testing.T) {
	t1, _ := prepare(Options{Home: t.TempDir(), ConductorBase: "https://localhost:8787", PreserveProvider: true})
	for _, tc := range []struct{ id, path, body, reference string }{
		{"claude-code", "settings.json", `{"apiKeyHelper":"printenv MY_ANTHROPIC_KEY","env":{"ANTHROPIC_BASE_URL":"https://anthropic.example"}}`, "printenv MY_ANTHROPIC_KEY"},
		{"opencode", "opencode.json", `{"model":"custom/original-model","provider":{"custom":{"npm":"@ai-sdk/openai-compatible","options":{"baseURL":"https://provider.example/v1","apiKey":"{env:MY_KEY}"}}}}`, "{env:MY_KEY}"},
		{"continue", "config.json", `{"models":[{"provider":"openai","model":"original-model","apiBase":"https://provider.example/v1","apiKey":"MY_KEY"}]}`, "MY_KEY"},
		{"openai-env", ".profile", "export OPENAI_BASE_URL='https://provider.example/v1'\nexport OPENAI_API_KEY=\"${MY_KEY}\"\n", "${MY_KEY}"},
	} {
		t.Run(tc.id, func(t *testing.T) {
			next, profile, err := capturedEdit(t1, tc.id, tc.path, []byte(tc.body))
			if err != nil {
				t.Fatal(err)
			}
			if !strings.Contains(string(next), tc.reference) || !strings.Contains(string(next), "/capture/"+tc.id) || strings.Contains(string(next), "HYPERMESH_API_KEY") {
				t.Fatal(string(next))
			}
			if strings.Contains(string(profile), tc.reference) {
				t.Fatal("profile copied credential reference")
			}
		})
	}
}

func TestCapturedOriginFailuresWriteNothing(t *testing.T) {
	target, _ := prepare(Options{Home: t.TempDir(), ConductorBase: "https://localhost:8787", PreserveProvider: true})
	for _, body := range []string{"export OPENAI_BASE_URL=$UNKNOWN/v1\n", "export OPENAI_BASE_URL='https://user:secret@provider.example/v1'\n"} {
		if _, _, err := capturedEdit(target, "openai-env", ".profile", []byte(body)); err == nil {
			t.Fatal("unsafe source accepted")
		}
	}
}

func TestCapturedSharedShellPreservesOtherTool(t *testing.T) {
	home := t.TempDir()
	path := filepath.Join(home, ".profile")
	original := "export OPENAI_BASE_URL='https://openai.example/v1'\nexport ANTHROPIC_BASE_URL='https://anthropic.example'\nexport OPENAI_API_KEY=\"${OPENAI_SECRET}\"\nexport ANTHROPIC_API_KEY=\"${ANTHROPIC_SECRET}\"\n"
	mustWrite(t, path, original)
	opts := Options{Home: home, ConfigDir: filepath.Join(home, ".config", "hypermesh"), ConductorBase: "https://localhost:8787", PreserveProvider: true}
	for _, id := range []string{"openai-env", "anthropic-env"} {
		if _, err := Enable(opts, id, false); err != nil {
			t.Fatal(err)
		}
	}
	current := readFile(path)
	if !strings.Contains(current, "/capture/openai-env/v1") || !strings.Contains(current, "/capture/anthropic-env") {
		t.Fatal(current)
	}
	if _, err := Disable(opts, "openai-env", false); err != nil {
		t.Fatal(err)
	}
	current = readFile(path)
	if strings.Contains(current, "/capture/openai-env") || !strings.Contains(current, "/capture/anthropic-env") {
		t.Fatal(current)
	}
	if _, err := Disable(opts, "anthropic-env", false); err != nil {
		t.Fatal(err)
	}
	if readFile(path) != original {
		t.Fatal("original shell not restored")
	}
}
