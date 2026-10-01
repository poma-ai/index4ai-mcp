package oauth

import (
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"testing"
)

// The api answers OAuth discovery on api.index4.ai with issuer https://api.poma-ai.com,
// so the advertised authorization server must be that issuer, never the index4ai API
// host from POMA_API_BASE_URL.
func TestProtectedResourceMetaAuthServer(t *testing.T) {
	tests := []struct{ name, env, want string }{
		{"default is the prod issuer", "", "https://api.poma-ai.com"},
		{"dev override, trailing slash trimmed", "https://api-dev.poma-ai.com/", "https://api-dev.poma-ai.com"},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			t.Setenv("POMA_API_BASE_URL", "https://api.index4.ai/index4ai/v1")
			t.Setenv("POMA_AUTH_SERVER_URL", tt.env)
			t.Setenv("POMA_MCP_PUBLIC_URL", "https://mcp.index4.ai")
			rec := httptest.NewRecorder()
			handleProtectedResourceMeta(rec, httptest.NewRequest(http.MethodGet, "/.well-known/oauth-protected-resource", nil))
			var meta protectedResourceMeta
			if err := json.Unmarshal(rec.Body.Bytes(), &meta); err != nil {
				t.Fatal(err)
			}
			if len(meta.AuthorizationServers) != 1 || meta.AuthorizationServers[0] != tt.want {
				t.Fatalf("authorization_servers = %v, want [%s]", meta.AuthorizationServers, tt.want)
			}
			if meta.Resource != "https://mcp.index4.ai" {
				t.Fatalf("resource = %q", meta.Resource)
			}
		})
	}
}
