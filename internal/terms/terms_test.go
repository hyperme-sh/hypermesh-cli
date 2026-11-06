package terms

import (
	"bytes"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"
)

func TestEmbeddedTermsHash(t *testing.T) {
	sum := sha256.Sum256(Text)
	got := hex.EncodeToString(sum[:])
	const want = "46620be8f650c44e8188b0e6143be0193f70f6bd901a47d3d5169785e66c7045"
	if got != want {
		t.Fatalf("embedded terms sha256 %s", got)
	}
	if Hash() != want {
		t.Fatalf("Hash() %s", Hash())
	}
	if Version != "hypermesh-terms-inline-2026-11-05-r1" {
		t.Fatalf("version %s", Version)
	}
}

func TestAcceptedViaFlag(t *testing.T) {
	isolate(t)
	var out bytes.Buffer
	if err := Ensure(strings.NewReader("n\n"), &out, true, "accept"); err != nil {
		t.Fatal(err)
	}
	if out.Len() != 0 {
		t.Fatalf("prompted despite --license accept: %q", out.String())
	}
	assertStamp(t)
}

func TestAcceptedViaEnv(t *testing.T) {
	isolate(t)
	t.Setenv(EnvLicense, "accept")
	var out bytes.Buffer
	if err := Ensure(strings.NewReader(""), &out, false, ""); err != nil {
		t.Fatal(err)
	}
	if out.Len() != 0 {
		t.Fatalf("wrote output on env accept: %q", out.String())
	}
	assertStamp(t)
}

func TestNonTTYRefusal(t *testing.T) {
	isolate(t)
	var out bytes.Buffer
	err := Ensure(strings.NewReader("y\n"), &out, false, "")
	if err == nil {
		t.Fatal("expected refusal")
	}
	msg := err.Error()
	for _, needle := range []string{"--license accept", EnvLicense + "=accept", Version} {
		if !strings.Contains(msg, needle) {
			t.Fatalf("error %q missing %q", msg, needle)
		}
	}
	if out.Len() != 0 {
		t.Fatalf("printed terms on non-tty refusal: %q", out.String())
	}
	if _, err := os.Stat(mustPath(t)); !os.IsNotExist(err) {
		t.Fatal("stamp written")
	}
}

func TestStaleStampVersionReprompts(t *testing.T) {
	isolate(t)
	writeStamp(t, Stamp{
		Version:    "hypermesh-terms-old",
		SHA256:     Hash(),
		AcceptedAt: time.Now().UTC().Format(time.RFC3339),
	})

	var out bytes.Buffer
	err := Ensure(strings.NewReader("n\n"), &out, true, "")
	if err == nil {
		t.Fatal("expected decline")
	}
	if !strings.Contains(out.String(), "Accept the Hypermesh Terms? [y/N]") {
		t.Fatalf("did not re-prompt: %q", out.String())
	}
	if !bytes.Contains(out.Bytes(), Text) {
		t.Fatal("terms text not printed")
	}
	st := readStamp(t)
	if st.Version != "hypermesh-terms-old" {
		t.Fatalf("decline rewrote stamp: %+v", st)
	}

	out.Reset()
	if err := Ensure(strings.NewReader("yes\n"), &out, true, ""); err != nil {
		t.Fatal(err)
	}
	assertStamp(t)
}

func TestStaleHashReprompts(t *testing.T) {
	isolate(t)
	writeStamp(t, Stamp{
		Version:    Version,
		SHA256:     strings.Repeat("ab", 32),
		AcceptedAt: time.Now().UTC().Format(time.RFC3339),
	})
	var out bytes.Buffer
	err := Ensure(strings.NewReader("\n"), &out, true, "")
	if err == nil {
		t.Fatal("empty answer accepted")
	}
	if !strings.Contains(out.String(), "Accept the Hypermesh Terms? [y/N]") {
		t.Fatalf("did not re-prompt: %q", out.String())
	}
}

func TestOtherAnswersDecline(t *testing.T) {
	for _, answer := range []string{"n\n", "yeah\n", "yess\n", "\n"} {
		name := strings.TrimSpace(answer)
		if name == "" {
			name = "empty"
		}
		t.Run(name, func(t *testing.T) {
			isolate(t)
			var out bytes.Buffer
			if err := Ensure(strings.NewReader(answer), &out, true, ""); err == nil {
				t.Fatal("expected decline")
			}
		})
	}
}

func TestYesIsCaseInsensitive(t *testing.T) {
	for _, answer := range []string{"y\n", "yes\n", "Y\n", "YES\n"} {
		t.Run(strings.TrimSpace(answer), func(t *testing.T) {
			isolate(t)
			if err := Ensure(strings.NewReader(answer), &bytes.Buffer{}, true, ""); err != nil {
				t.Fatal(err)
			}
			assertStamp(t)
		})
	}
}

func TestInvalidLicense(t *testing.T) {
	isolate(t)
	if err := ValidateLicense("yes"); err == nil {
		t.Fatal("expected flag error")
	}
	t.Setenv(EnvLicense, "true")
	if err := ValidateLicense(""); err == nil {
		t.Fatal("expected env error")
	}
	if err := Ensure(strings.NewReader(""), &bytes.Buffer{}, false, "nope"); err == nil {
		t.Fatal("expected ensure error")
	}
	if _, err := os.Stat(mustPath(t)); !os.IsNotExist(err) {
		t.Fatal("stamp written")
	}
}

func TestCurrentStampSkipsPrompt(t *testing.T) {
	isolate(t)
	if err := Write(); err != nil {
		t.Fatal(err)
	}
	before := readStamp(t)
	var out bytes.Buffer
	if err := Ensure(strings.NewReader("n\n"), &out, true, ""); err != nil {
		t.Fatal(err)
	}
	if out.Len() != 0 {
		t.Fatalf("prompted with a current stamp: %q", out.String())
	}
	after := readStamp(t)
	if after != before {
		t.Fatalf("stamp changed\nbefore %+v\nafter %+v", before, after)
	}
}

func isolate(t *testing.T) {
	t.Helper()
	home := t.TempDir()
	t.Setenv("HOME", home)
	t.Setenv("XDG_CONFIG_HOME", filepath.Join(home, "xdg"))
	t.Setenv("AppData", filepath.Join(home, "AppData"))
	t.Setenv("home", home)
	t.Setenv(EnvLicense, "")
}

func mustPath(t *testing.T) string {
	t.Helper()
	p, err := StampPath()
	if err != nil {
		t.Fatal(err)
	}
	return p
}

func writeStamp(t *testing.T, st Stamp) {
	t.Helper()
	path := mustPath(t)
	if err := os.MkdirAll(filepath.Dir(path), 0o700); err != nil {
		t.Fatal(err)
	}
	raw, err := json.Marshal(st)
	if err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(path, raw, 0o644); err != nil {
		t.Fatal(err)
	}
}

func readStamp(t *testing.T) Stamp {
	t.Helper()
	raw, err := os.ReadFile(mustPath(t))
	if err != nil {
		t.Fatal(err)
	}
	var st Stamp
	if err := json.Unmarshal(raw, &st); err != nil {
		t.Fatal(err)
	}
	return st
}

func assertStamp(t *testing.T) {
	t.Helper()
	st := readStamp(t)
	if st.Version != Version {
		t.Fatalf("version %s", st.Version)
	}
	if st.SHA256 != Hash() {
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
