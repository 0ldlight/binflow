package client

import "testing"

// TestAbsURLJoining covers the pure base URL + path joining logic for cases
// the external client_test.go cannot reach over a real round trip: the
// zero-value default base URL and non-routable custom base URLs. The
// behavioral trailing-slash cases are additionally verified end-to-end in
// TestClientAbsURL against an httptest server.
func TestAbsURLJoining(t *testing.T) {
	tests := []struct {
		name    string
		baseURL string
		path    string
		want    string
	}{
		{
			name:    "default base URL",
			baseURL: "",
			path:    "/binflow/api/repositories",
			want:    "http://localhost:8080/binflow/api/repositories",
		},
		{
			name:    "custom base URL",
			baseURL: "https://binflow.example.com",
			path:    "/binflow/api/system/ping",
			want:    "https://binflow.example.com/binflow/api/system/ping",
		},
		{
			name:    "ipv4 host with port",
			baseURL: "http://127.0.0.1:8080",
			path:    "/binflow/api/security/users",
			want:    "http://127.0.0.1:8080/binflow/api/security/users",
		},
		{
			name:    "one trailing slash trimmed",
			baseURL: "http://localhost:8080/",
			path:    "/binflow/api/repositories",
			want:    "http://localhost:8080/binflow/api/repositories",
		},
		{
			name:    "two trailing slashes trimmed",
			baseURL: "http://localhost:8080//",
			path:    "/binflow/api/repositories",
			want:    "http://localhost:8080/binflow/api/repositories",
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			c := &Client{BaseURL: tt.baseURL}
			if got := c.absURL(tt.path); got != tt.want {
				t.Errorf("absURL(%q) = %q, want %q", tt.path, got, tt.want)
			}
		})
	}
}

// TestRetryMaxSemantics pins the RetryMax field semantics: zero selects the
// default, negative disables retries, positive passes through.
func TestRetryMaxSemantics(t *testing.T) {
	tests := []struct {
		name  string
		retry int
		want  int
	}{
		{name: "zero selects default", retry: 0, want: DefaultRetryMax},
		{name: "negative disables retries", retry: -1, want: 0},
		{name: "positive passes through", retry: 3, want: 3},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			c := &Client{RetryMax: tt.retry}
			if got := c.retryMax(); got != tt.want {
				t.Errorf("retryMax() = %d, want %d", got, tt.want)
			}
		})
	}
}
