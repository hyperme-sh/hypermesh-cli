package terms

import (
	"bufio"
	"crypto/sha256"
	_ "embed"
	"encoding/hex"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"os"
	"path/filepath"
	"strings"
	"time"
)

//go:embed hypermesh-terms-inline-2026-11-05-r1.md
var Text []byte

const (
	Version     = "hypermesh-terms-inline-2026-11-05-r1"
	EnvLicense  = "HYPERMESH_LICENSE"
	acceptValue = "accept"
	stampName   = "terms-accepted.json"
	promptLine  = "Accept the Hypermesh Terms? [y/N] "
)

type Stamp struct {
	Version    string `json:"version"`
	SHA256     string `json:"sha256"`
	AcceptedAt string `json:"accepted_at"`
}

func Hash() string {
	sum := sha256.Sum256(Text)
	return hex.EncodeToString(sum[:])
}

func StampPath() (string, error) {
	dir, err := os.UserConfigDir()
	if err != nil {
		return "", err
	}
	return filepath.Join(dir, "hypermesh", stampName), nil
}

func (s Stamp) current() bool {
	return s.Version == Version && s.SHA256 == Hash() && strings.TrimSpace(s.AcceptedAt) != ""
}

func ValidateLicense(flag string) error {
	if v := strings.TrimSpace(flag); v != "" && v != acceptValue {
		return fmt.Errorf("invalid --license value %q", v)
	}
	if v := strings.TrimSpace(os.Getenv(EnvLicense)); v != "" && v != acceptValue {
		return fmt.Errorf("invalid %s value %q", EnvLicense, v)
	}
	return nil
}

func WantsAccept(flag string) bool {
	if strings.TrimSpace(flag) == acceptValue {
		return true
	}
	return strings.TrimSpace(os.Getenv(EnvLicense)) == acceptValue
}

func Accepted() (bool, error) {
	st, found, err := load()
	if err != nil || !found {
		return false, err
	}
	return st.current(), nil
}

// RecordIfNeeded writes the stamp when the current one is missing or stale.
func RecordIfNeeded() error {
	ok, err := Accepted()
	if err != nil || ok {
		return err
	}
	return Write()
}

// Ensure applies the gate. license is the --license flag value.
// interactive is true only when stdin and stdout are both terminals.
func Ensure(in io.Reader, out io.Writer, interactive bool, license string) error {
	if err := ValidateLicense(license); err != nil {
		return err
	}
	ok, err := Accepted()
	if err != nil {
		return err
	}
	if ok {
		return nil
	}
	if WantsAccept(license) {
		return Write()
	}
	if interactive {
		return prompt(in, out)
	}
	return fmt.Errorf("Hypermesh Terms %s are not accepted; pass --license accept or set %s=accept", Version, EnvLicense)
}

func Write() error {
	path, err := StampPath()
	if err != nil {
		return err
	}
	if err := os.MkdirAll(filepath.Dir(path), 0o700); err != nil {
		return err
	}
	st := Stamp{
		Version:    Version,
		SHA256:     Hash(),
		AcceptedAt: time.Now().UTC().Format(time.RFC3339),
	}
	raw, err := json.MarshalIndent(st, "", "  ")
	if err != nil {
		return err
	}
	raw = append(raw, '\n')
	return os.WriteFile(path, raw, 0o644)
}

func WriteText(w io.Writer) error {
	if _, err := w.Write(Text); err != nil {
		return err
	}
	if len(Text) == 0 || Text[len(Text)-1] != '\n' {
		_, err := io.WriteString(w, "\n")
		return err
	}
	return nil
}

func prompt(in io.Reader, out io.Writer) error {
	if err := WriteText(out); err != nil {
		return err
	}
	if _, err := io.WriteString(out, promptLine); err != nil {
		return err
	}
	line, err := bufio.NewReader(in).ReadString('\n')
	if err != nil && !errors.Is(err, io.EOF) {
		return err
	}
	if !affirmed(line) {
		return fmt.Errorf("Hypermesh Terms not accepted")
	}
	return Write()
}

func affirmed(line string) bool {
	switch strings.ToLower(strings.TrimSpace(line)) {
	case "y", "yes":
		return true
	default:
		return false
	}
}

func load() (Stamp, bool, error) {
	path, err := StampPath()
	if err != nil {
		return Stamp{}, false, err
	}
	raw, err := os.ReadFile(path)
	if errors.Is(err, os.ErrNotExist) {
		return Stamp{}, false, nil
	}
	if err != nil {
		return Stamp{}, false, err
	}
	var st Stamp
	if err := json.Unmarshal(raw, &st); err != nil {
		return Stamp{}, false, nil
	}
	return st, true, nil
}
