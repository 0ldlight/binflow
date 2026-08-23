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

// TestEscapePathSegments pins the exact wire spelling of the escaping
// contract (T-231). The table is the 5-character regression matrix of the
// T-228 D-1 defect plus the structural edges: separating slashes stay
// literal, a '/' inside a segment encodes, and the sub-delims Artifactory
// paths commonly carry ($ '& etc.) stay raw so the escaped form matches the
// migrate reader's spelling byte for byte (the isomorphism the shared
// contract demands).
func TestEscapePathSegments(t *testing.T) {
	tests := []struct {
		name string
		in   string
		want string
	}{
		{name: "plain path unchanged", in: "doc/readme.md", want: "doc/readme.md"},
		{name: "literal percent (D-1 reproducer)", in: "sym'bols$/percent%.txt", want: "sym%27bols$/percent%25.txt"},
		{name: "fragment marker", in: "frag#ment.txt", want: "frag%23ment.txt"},
		{name: "query marker", in: "query?name.txt", want: "query%3Fname.txt"},
		{name: "space", in: "with space.txt", want: "with%20space.txt"},
		{name: "utf-8 chinese, nested", in: "中文/文件 名.txt", want: "%E4%B8%AD%E6%96%87/%E6%96%87%E4%BB%B6%20%E5%90%8D.txt"},
		{name: "slash inside a segment encodes", in: "a%2Falready.txt", want: "a%252Falready.txt"},
		{name: "empty stays empty", in: "", want: ""},
		{name: "trailing slash preserved literally", in: "dir/", want: "dir/"},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			if got := EscapePathSegments(tt.in); got != tt.want {
				t.Errorf("EscapePathSegments(%q) = %q, want %q", tt.in, got, tt.want)
			}
		})
	}
}
