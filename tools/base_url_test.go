package tools

import "testing"

// The index4.ai API is /v1 on the index4.ai hosts (the gateway rewrites it to the
// /index4ai/v1 mount) and /index4ai/v1 on any other host, where /v1 is the retired POMA
// API v1. The status SSE service stays at /status/v1 on the same host. A
// POMA_API_BASE_URL given with or without a version path must resolve to the same URLs.
func TestAPIAndStatusBaseURL(t *testing.T) {
	tests := []struct {
		name, api, status, wantAPI, wantStatus string
	}{
		{"defaults", "", "", "https://api.index4.ai/v1", "https://api.index4.ai/status/v1"},
		{"origin only", "https://api-dev.index4.ai", "", "https://api-dev.index4.ai/v1", "https://api-dev.index4.ai/status/v1"},
		{"v1 base", "https://api-dev.index4.ai/v1/", "", "https://api-dev.index4.ai/v1", "https://api-dev.index4.ai/status/v1"},
		{"mounted base on index4.ai", "https://api-dev.index4.ai/index4ai/v1/", "", "https://api-dev.index4.ai/v1", "https://api-dev.index4.ai/status/v1"},
		{"grill-era version path is replaced", "https://api.poma-ai.com/v3", "", "https://api.poma-ai.com/index4ai/v1", "https://api.poma-ai.com/status/v1"},
		{"local api uses the mount", "http://127.0.0.1:8080", "", "http://127.0.0.1:8080/index4ai/v1", "http://127.0.0.1:8080/status/v1"},
		{"lookalike host uses the mount", "https://api.notindex4.ai", "", "https://api.notindex4.ai/index4ai/v1", "https://api.notindex4.ai/status/v1"},
		{"status override", "", "http://127.0.0.1:9090", "https://api.index4.ai/v1", "http://127.0.0.1:9090/status/v1"},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			t.Setenv("POMA_API_BASE_URL", tt.api)
			t.Setenv("POMA_STATUS_API_BASE_URL", tt.status)
			if got := apiBaseURL(); got != tt.wantAPI {
				t.Errorf("apiBaseURL() = %q, want %q", got, tt.wantAPI)
			}
			if got := statusAPIBaseURL(); got != tt.wantStatus {
				t.Errorf("statusAPIBaseURL() = %q, want %q", got, tt.wantStatus)
			}
		})
	}
}
