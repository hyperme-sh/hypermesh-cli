package cli

import (
	"encoding/json"
	"errors"
	"net/http"
	"net/http/httptest"
	"os"
	"os/exec"
	"path/filepath"
	"runtime"
	"strings"
	"testing"
	"time"

	"github.com/hyperme-sh/hypermesh-cli/internal/terms"
)

func TestTermsAcceptedViaFlag(t *testing.T) {
	home := t.TempDir()
	got := runTermsCLI(t, home, nil, "--license", "accept", "auth", "whoami")
	if got.Code != 0 {
		t.Fatalf("exit %d stderr %q", got.Code, got.Stderr)
	}
	if !strings.Contains(got.Stdout, "api_base") {
		t.Fatalf("command did not continue: %q", got.Stdout)
	}
	assertTermsStamp(t, home)

	got = runTermsCLI(t, home, nil, "auth", "whoami")
	if got.Code != 0 {
		t.Fatalf("stamp not honored: exit %d stderr %q", got.Code, got.Stderr)
	}
}

func TestTermsAcceptedViaEnv(t *testing.T) {
	home := t.TempDir()
	got := runTermsCLI(t, home, []string{"HYPERMESH_LICENSE=accept"}, "auth", "whoami")
	if got.Code != 0 {
		t.Fatalf("exit %d stderr %q", got.Code, got.Stderr)
	}
	if !strings.Contains(got.Stdout, "api_base") {
		t.Fatalf("command did not continue: %q", got.Stdout)
	}
	assertTermsStamp(t, home)
}

func TestTermsNonTTYRefusal(t *testing.T) {
	called := false
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		called = true
		w.WriteHeader(http.StatusOK)
	}))
	t.Cleanup(srv.Close)

	home := t.TempDir()
	got := runTermsCLI(t, home, nil, "--api-base", srv.URL, "hosts")
	if got.Code == 0 {
		t.Fatal("expected non-zero exit")
	}
	if called {
		t.Fatal("network call before terms acceptance")
	}
	for _, needle := range []string{"--license accept", "HYPERMESH_LICENSE=accept", terms.Version} {
		if !strings.Contains(got.Stderr, needle) {
			t.Fatalf("stderr %q missing %q", got.Stderr, needle)
		}
	}
	if _, ok := loadTermsStamp(t, home); ok {
		t.Fatal("stamp written")
	}
}

func TestTermsExemptCommands(t *testing.T) {
	for _, args := range [][]string{
		{"--help"},
		{"help"},
		{"version"},
		{"terms"},
	} {
		t.Run(strings.Join(args, " "), func(t *testing.T) {
			home := t.TempDir()
			got := runTermsCLI(t, home, nil, args...)
			if got.Code != 0 {
				t.Fatalf("exit %d stderr %q stdout %q", got.Code, got.Stderr, got.Stdout)
			}
			if _, ok := loadTermsStamp(t, home); ok {
				t.Fatal("exempt command wrote a stamp")
			}
			if args[0] == "version" && !strings.Contains(got.Stdout, "hypermesh") {
				t.Fatalf("version stdout %q", got.Stdout)
			}
			if args[0] == "terms" {
				if !strings.Contains(got.Stdout, terms.Version) || !strings.Contains(got.Stdout, terms.Hash()) {
					t.Fatalf("terms stdout missing version or hash: %q", got.Stdout)
				}
				if !strings.Contains(got.Stdout, "Fyber Labs TN LLC") {
					t.Fatal("terms text missing")
				}
			}
		})
	}

	home := t.TempDir()
	got := runTermsCLI(t, home, nil, "terms", "--accept")
	if got.Code != 0 {
		t.Fatalf("exit %d stderr %q", got.Code, got.Stderr)
	}
	if !strings.Contains(got.Stdout, "accepted") || !strings.Contains(got.Stdout, terms.Hash()) {
		t.Fatalf("stdout %q", got.Stdout)
	}
	assertTermsStamp(t, home)
}

func TestTermsRejectsOtherLicenseValue(t *testing.T) {
	home := t.TempDir()
	srvCalled := false
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		srvCalled = true
	}))
	t.Cleanup(srv.Close)
	got := runTermsCLI(t, home, nil, "--license", "yes", "--api-base", srv.URL, "hosts")
	if got.Code == 0 {
		t.Fatal("expected error")
	}
	if !strings.Contains(got.Stderr, "invalid --license") {
		t.Fatalf("stderr %q", got.Stderr)
	}
	if srvCalled {
		t.Fatal("network call on invalid --license")
	}
	if _, ok := loadTermsStamp(t, home); ok {
		t.Fatal("stamp written")
	}

	got = runTermsCLI(t, home, []string{"HYPERMESH_LICENSE=yes"}, "version")
	if got.Code == 0 {
		t.Fatal("expected env error")
	}
	if !strings.Contains(got.Stderr, "HYPERMESH_LICENSE") {
		t.Fatalf("stderr %q", got.Stderr)
	}
}

func TestTermsAcceptOnExemptCommandRecords(t *testing.T) {
	home := t.TempDir()
	got := runTermsCLI(t, home, nil, "--license", "accept", "version")
	if got.Code != 0 {
		t.Fatalf("exit %d stderr %q", got.Code, got.Stderr)
	}
	assertTermsStamp(t, home)
}

func termsAcceptEnv(t *testing.T) []string {
	t.Helper()
	home := t.TempDir()
	return []string{
		"HOME=" + home,
		"XDG_CONFIG_HOME=" + filepath.Join(home, "xdg"),
		"AppData=" + filepath.Join(home, "AppData"),
		"HYPERMESH_LICENSE=accept",
	}
}

func acceptTermsForTest(t *testing.T) {
	t.Helper()
	home := t.TempDir()
	t.Setenv("HOME", home)
	t.Setenv("XDG_CONFIG_HOME", filepath.Join(home, "xdg"))
	t.Setenv("AppData", filepath.Join(home, "AppData"))
	t.Setenv("HYPERMESH_LICENSE", "accept")
}

func runTermsCLI(t *testing.T, home string, extra []string, args ...string) scriptResult {
	t.Helper()
	cmd := exec.Command(testBinary(t), args...)
	cmd.Env = termsCLIEnv(home, extra)
	var stdout, stderr strings.Builder
	cmd.Stdout = &stdout
	cmd.Stderr = &stderr
	err := cmd.Run()
	code := 0
	if err != nil {
		var ee *exec.ExitError
		if !errors.As(err, &ee) {
			t.Fatalf("run: %v\nstderr: %s", err, stderr.String())
		}
		code = ee.ExitCode()
	}
	return scriptResult{Stdout: stdout.String(), Stderr: stderr.String(), Code: code}
}

func termsCLIEnv(home string, extra []string) []string {
	env := []string{
		"PATH=" + os.Getenv("PATH"),
		"HOME=" + home,
		"XDG_CONFIG_HOME=" + filepath.Join(home, "xdg"),
		"AppData=" + filepath.Join(home, "AppData"),
		"HYPERMESH_CONFIG_DIR=" + filepath.Join(home, "cfg"),
		"HYPERMESH_API_KEY=org_key",
		"HYPERMESH_TENANT_ID=ten",
	}
	return append(env, extra...)
}

func termsStampPath(home string) string {
	return filepath.Join(userHypermeshDir(home), "terms-accepted.json")
}

func userHypermeshDir(home string) string {
	switch runtime.GOOS {
	case "windows":
		return filepath.Join(home, "AppData", "hypermesh")
	case "darwin", "ios":
		return filepath.Join(home, "Library", "Application Support", "hypermesh")
	default:
		return filepath.Join(home, "xdg", "hypermesh")
	}
}

func loadTermsStamp(t *testing.T, home string) (terms.Stamp, bool) {
	t.Helper()
	raw, err := os.ReadFile(termsStampPath(home))
	if errors.Is(err, os.ErrNotExist) {
		return terms.Stamp{}, false
	}
	if err != nil {
		t.Fatal(err)
	}
	var st terms.Stamp
	if err := json.Unmarshal(raw, &st); err != nil {
		t.Fatal(err)
	}
	return st, true
}

func assertTermsStamp(t *testing.T, home string) {
	t.Helper()
	st, ok := loadTermsStamp(t, home)
	if !ok {
		t.Fatal("missing stamp")
	}
	if st.Version != terms.Version {
		t.Fatalf("version %s", st.Version)
	}
	if st.SHA256 != terms.Hash() {
		t.Fatalf("sha256 %s", st.SHA256)
	}
	if !strings.HasSuffix(st.AcceptedAt, "Z") {
		t.Fatalf("accepted_at %q", st.AcceptedAt)
	}
	ts, err := time.Parse(time.RFC3339, st.AcceptedAt)
	if err != nil {
		t.Fatal(err)
	}
	if ts.Location() != time.UTC {
		t.Fatalf("location %v", ts.Location())
	}
}
