//go:build linux

package api

import (
	"github.com/hyperme-sh/hypermesh-cli/internal/localtransport"
	"net/http"
	"net/http/httptest"
	"path/filepath"
	"testing"
)

func TestChatUsesProtectedSocket(t *testing.T) {
	path := filepath.Join(t.TempDir(), "run", "conductor.sock")
	ln, err := localtransport.Listen(path)
	if err != nil {
		t.Fatal(err)
	}
	srv := httptest.NewUnstartedServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.Header.Get("Authorization") != "Bearer org_key" {
			t.Error("missing credential")
		}
		w.Write([]byte(`{"choices":[]}`))
	}))
	srv.Listener.Close()
	srv.Listener = ln
	srv.Start()
	defer srv.Close()
	c := NewClient(DefaultAPIBase, DefaultConductorBase, "org_key", "tenant")
	c.ConductorSocket = path
	if _, err := c.ChatCompletions("lease", ChatRequest{Messages: []ChatMessage{{Role: "user", Content: "hello"}}}); err != nil {
		t.Fatal(err)
	}
}
func TestChatRejectsUnauthenticatedTCP(t *testing.T) {
	called := false
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) { called = true }))
	defer srv.Close()
	c := NewClient(DefaultAPIBase, srv.URL, "org_key", "tenant")
	if _, err := c.ChatCompletions("lease", ChatRequest{Messages: []ChatMessage{{Role: "user", Content: "secret"}}}); err == nil {
		t.Fatal("accepted HTTP")
	}
	if called {
		t.Fatal("sent request to unauthenticated listener")
	}
}
func TestChatDoesNotFollowRedirect(t *testing.T) {
	called := false
	target := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) { called = true }))
	defer target.Close()
	source := httptest.NewTLSServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) { http.Redirect(w, r, target.URL, 307) }))
	defer source.Close()
	c := NewClient(DefaultAPIBase, source.URL, "org_key", "tenant")
	c.HTTP = source.Client()
	if _, err := c.ChatCompletions("lease", ChatRequest{Messages: []ChatMessage{{Role: "user", Content: "secret"}}}); err == nil {
		t.Fatal("accepted redirect")
	}
	if called {
		t.Fatal("forwarded secrets through redirect")
	}
}
