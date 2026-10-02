package tools

import (
	"context"
	"fmt"
	"net"
	"net/http"
	"net/http/httptest"
	"os"
	"sync"
	"testing"

	"github.com/poma-ai/poma-cli/pkg/client"
)

// No test in this package may reach a live index4ai API. The package reads its
// hosts from POMA_API_BASE_URL and POMA_STATUS_API_BASE_URL (common.go) and its
// token only from the call or the request context — there is deliberately no
// env-var key fallback here. TestMain still clears the key and project
// variables a developer typically has exported (POMA_API_KEY,
// INDEX4AI_API_KEY, POMA_PROJECT_ID, INDEX4AI_PROJECT_ID), so a fallback added
// later cannot pick them up, clears the status host so it follows
// POMA_API_BASE_URL, and points the API host at a dead stub. A test that wants
// an API sets its own stub with t.Setenv, which restores the dead one
// afterwards; a test that forgets falls through to the dead stub, and the run
// fails listing the stray requests — instead of the request silently going to
// https://api.index4.ai. (2026-10-01, in poma-grill-mcp: a test that called the
// upload handler without a stub uploaded a real document to production.)
//
// The env reset alone can be undone by a test (base_url_test.go clears
// POMA_API_BASE_URL to check the default), so TestMain also installs
// testClientHook: every client grillClient builds — the API calls, redirects
// and the status SSE stream all share its transport — refuses to dial anything
// but a loopback address, and the refused destination fails the run.

var (
	deadAPIMu   sync.Mutex
	deadAPIHits []string
)

// liveAPIEnv lists the variables TestMain clears.
var liveAPIEnv = []string{
	"POMA_API_KEY", "INDEX4AI_API_KEY",
	"POMA_PROJECT_ID", "INDEX4AI_PROJECT_ID",
	"POMA_STATUS_API_BASE_URL",
}

func TestMain(m *testing.M) {
	dead := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		deadAPIMu.Lock()
		deadAPIHits = append(deadAPIHits, r.Method+" "+r.URL.Path)
		deadAPIMu.Unlock()
		w.WriteHeader(http.StatusTeapot)
		_, _ = w.Write([]byte(`{"error":"test reached the default API host; set POMA_API_BASE_URL to a stub"}`))
	}))
	for _, k := range liveAPIEnv {
		_ = os.Unsetenv(k)
	}
	_ = os.Setenv("POMA_API_BASE_URL", dead.URL)
	testClientHook = loopbackOnly

	code := m.Run()
	dead.Close()
	if len(deadAPIHits) > 0 {
		fmt.Fprintf(os.Stderr, "FAIL: %d request(s) reached the dead API stub or tried to dial a non-loopback host (would have gone to a live API):\n", len(deadAPIHits))
		for _, h := range deadAPIHits {
			fmt.Fprintln(os.Stderr, "  "+h)
		}
		code = 1
	}
	os.Exit(code)
}

// loopbackOnly replaces the client's dialer with one that refuses any
// non-loopback destination before a connection is opened, and records it.
func loopbackOnly(c *client.Client) {
	tr, ok := c.HTTP.Transport.(*http.Transport)
	if !ok {
		tr = &http.Transport{}
	} else {
		tr = tr.Clone()
	}
	d := &net.Dialer{}
	tr.DialContext = func(ctx context.Context, network, addr string) (net.Conn, error) {
		if !isLoopbackAddr(addr) {
			deadAPIMu.Lock()
			deadAPIHits = append(deadAPIHits, "dial "+addr)
			deadAPIMu.Unlock()
			return nil, fmt.Errorf("test refused a non-loopback dial to %s", addr)
		}
		return d.DialContext(ctx, network, addr)
	}
	tr.Proxy = nil
	c.HTTP.Transport = tr
}

func isLoopbackAddr(addr string) bool {
	host, _, err := net.SplitHostPort(addr)
	if err != nil {
		host = addr
	}
	ip := net.ParseIP(host)
	return ip != nil && ip.IsLoopback()
}

// The guard itself: a client built while POMA_API_BASE_URL is cleared (so it
// resolves to the live default host) must fail before any connection opens.
func TestNoLiveAPIGuardRefusesDefaultHost(t *testing.T) {
	t.Setenv("POMA_API_BASE_URL", "")
	c := grillClient("tok")
	tr, _ := c.HTTP.Transport.(*http.Transport)
	if tr == nil || tr.DialContext == nil {
		t.Fatal("guard not installed on grillClient's transport")
	}
	conn, err := tr.DialContext(context.Background(), "tcp", "api.index4.ai:443")
	if err == nil {
		conn.Close()
		t.Fatal("dial to api.index4.ai:443 was allowed")
	}
	// The refusal was recorded and would fail the run; take it back out.
	deadAPIMu.Lock()
	if n := len(deadAPIHits); n == 0 || deadAPIHits[n-1] != "dial api.index4.ai:443" {
		deadAPIMu.Unlock()
		t.Fatalf("refused dial not recorded: %v", deadAPIHits)
	}
	deadAPIHits = deadAPIHits[:len(deadAPIHits)-1]
	deadAPIMu.Unlock()
}

// noLiveAPI makes a test that must not talk to any API explicit about it: no
// key in the environment, and an API host that fails the test on any request.
func noLiveAPI(t *testing.T) {
	t.Helper()
	for _, k := range liveAPIEnv {
		t.Setenv(k, "")
	}
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		t.Errorf("unexpected API request: %s %s", r.Method, r.URL.Path)
		w.WriteHeader(http.StatusTeapot)
	}))
	t.Cleanup(srv.Close)
	t.Setenv("POMA_API_BASE_URL", srv.URL)
}
