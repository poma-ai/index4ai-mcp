package tools

import (
	"fmt"
	"net/http"
	"net/http/httptest"
	"os"
	"sync"
	"testing"
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

	code := m.Run()
	dead.Close()
	if len(deadAPIHits) > 0 {
		fmt.Fprintf(os.Stderr, "FAIL: %d request(s) reached the default API host (would have gone to the live API):\n", len(deadAPIHits))
		for _, h := range deadAPIHits {
			fmt.Fprintln(os.Stderr, "  "+h)
		}
		code = 1
	}
	os.Exit(code)
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
