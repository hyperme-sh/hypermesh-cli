//go:build linux

package cli

import (
	"bytes"
	"encoding/json"
	"errors"
	"os"
	"os/exec"
	"path/filepath"
	"strconv"
	"syscall"
	"testing"
	"time"
	"unsafe"

	"github.com/FyberLabs/hypermesh-cli/internal/terms"
)

var errTimeout = errors.New("pty command timed out")

func TestStaleStampVersionReprompts(t *testing.T) {
	home := t.TempDir()
	stale := terms.Stamp{
		Version:    "hypermesh-terms-old",
		SHA256:     terms.Hash(),
		AcceptedAt: time.Now().UTC().Format(time.RFC3339),
	}
	writeRawStamp(t, home, stale)

	out, code := runPTY(t, home, "n\n", "auth", "whoami")
	if code == 0 {
		t.Fatalf("declined acceptance exited 0: %s", out)
	}
	if !bytes.Contains(out, []byte("Accept the Hypermesh Terms? [y/N]")) {
		t.Fatalf("did not re-prompt: %s", out)
	}
	if !bytes.Contains(out, []byte("Fyber Labs TN LLC")) {
		t.Fatal("terms text not printed")
	}
	got, ok := loadTermsStamp(t, home)
	if !ok || got.Version != stale.Version {
		t.Fatalf("decline rewrote stamp: %+v ok=%v", got, ok)
	}

	out, code = runPTY(t, home, "y\n", "auth", "whoami")
	if code != 0 {
		t.Fatalf("accept exit %d: %s", code, out)
	}
	if !bytes.Contains(out, []byte("api_base")) {
		t.Fatalf("command did not continue: %s", out)
	}
	assertTermsStamp(t, home)
}

func writeRawStamp(t *testing.T, home string, st terms.Stamp) {
	t.Helper()
	path := termsStampPath(home)
	if err := os.MkdirAll(filepath.Dir(path), 0o700); err != nil {
		t.Fatal(err)
	}
	raw, err := json.Marshal(st)
	if err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(path, append(raw, '\n'), 0o644); err != nil {
		t.Fatal(err)
	}
}

func runPTY(t *testing.T, home, answer string, args ...string) ([]byte, int) {
	t.Helper()
	master, slave := openPTY(t)
	defer master.Close()

	cmd := exec.Command(testBinary(t), args...)
	cmd.Env = termsCLIEnv(home, nil)
	cmd.Stdin = slave
	cmd.Stdout = slave
	cmd.Stderr = slave
	cmd.SysProcAttr = &syscall.SysProcAttr{Setsid: true}
	if err := cmd.Start(); err != nil {
		t.Fatal(err)
	}
	slave.Close()

	var buf bytes.Buffer
	readDone := make(chan struct{})
	go func() {
		defer close(readDone)
		tmp := make([]byte, 4096)
		prompted := false
		needle := []byte("Accept the Hypermesh Terms? [y/N]")
		for {
			_ = master.SetReadDeadline(time.Now().Add(500 * time.Millisecond))
			n, err := master.Read(tmp)
			if n > 0 {
				buf.Write(tmp[:n])
				if !prompted && bytes.Contains(buf.Bytes(), needle) {
					prompted = true
					_, _ = master.Write([]byte(answer))
				}
			}
			if err != nil {
				if errors.Is(err, os.ErrDeadlineExceeded) {
					continue
				}
				return
			}
		}
	}()

	waitErr := make(chan error, 1)
	go func() { waitErr <- cmd.Wait() }()

	var err error
	select {
	case err = <-waitErr:
	case <-time.After(10 * time.Second):
		_ = cmd.Process.Kill()
		<-waitErr
		err = errTimeout
	}
	master.Close()
	<-readDone
	if errors.Is(err, errTimeout) {
		t.Fatalf("timeout waiting for command\n%s", buf.String())
	}

	code := 0
	if err != nil {
		var ee *exec.ExitError
		if !errors.As(err, &ee) {
			t.Fatalf("wait: %v\n%s", err, buf.String())
		}
		code = ee.ExitCode()
	}
	return buf.Bytes(), code
}

func openPTY(t *testing.T) (*os.File, *os.File) {
	t.Helper()
	master, err := os.OpenFile("/dev/ptmx", os.O_RDWR, 0)
	if err != nil {
		t.Fatalf("ptmx: %v", err)
	}
	var n uint32
	_, _, errno := syscall.Syscall(syscall.SYS_IOCTL, master.Fd(), syscall.TIOCGPTN, uintptr(unsafe.Pointer(&n)))
	if errno != 0 {
		master.Close()
		t.Fatalf("TIOCGPTN: %v", errno)
	}
	var unlock int32
	_, _, errno = syscall.Syscall(syscall.SYS_IOCTL, master.Fd(), syscall.TIOCSPTLCK, uintptr(unsafe.Pointer(&unlock)))
	if errno != 0 {
		master.Close()
		t.Fatalf("TIOCSPTLCK: %v", errno)
	}
	slave, err := os.OpenFile("/dev/pts/"+strconv.FormatUint(uint64(n), 10), os.O_RDWR|syscall.O_NOCTTY, 0)
	if err != nil {
		master.Close()
		t.Fatalf("pts: %v", err)
	}
	return master, slave
}
