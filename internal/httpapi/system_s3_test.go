package httpapi

// T-178 tests: the /readyz S3 probe must derive the minio Secure flag from
// the endpoint URL scheme. minio-go v7 REJECTS an endpoint whose scheme
// disagrees with Secure at client construction ("Endpoint url scheme ...
// conflicts with the secure option"), so the previously hardcoded
// Secure:true made every plain-HTTP MinIO endpoint permanently fail
// readiness (T-170 smoke finding). The mock server below is the probe
// subset of the S3 API (HEAD bucket / PUT / DELETE) — the full multipart
// mock used by the storage package's own tests is unnecessary here.

import (
	"context"
	"crypto/sha256"
	"encoding/hex"
	"io"
	"net/http"
	"net/http/httptest"
	"path/filepath"
	"strings"
	"sync"
	"testing"

	"github.com/lzwzzy/binflow/internal/config"
	"github.com/lzwzzy/binflow/internal/metadata"
)

// TestSecureFromEndpoint pins the scheme→Secure decision table-driven: the
// flag must AGREE with an endpoint's scheme for minio.New to accept it, and
// a scheme-less endpoint keeps minio-go's TLS default. Since T-180 the
// helper is the one exported spelling cmd's S3 assembly also consumes.
func TestSecureFromEndpoint(t *testing.T) {
	cases := []struct {
		name     string
		endpoint string
		want     bool
	}{
		{"http endpoint is plain", "http://127.0.0.1:9000", false},
		{"http host endpoint is plain", "http://minio.internal:9000", false},
		{"https endpoint is TLS", "https://s3.amazonaws.com", true},
		{"scheme comparison is case-insensitive", "HTTPS://s3.example.com", true},
		{"scheme-less host keeps the TLS default", "s3.amazonaws.com", true},
		{"scheme-less host:port keeps the TLS default", "127.0.0.1:9000", true},
		{"empty endpoint keeps the TLS default", "", true},
		// A non-http(s) scheme maps to non-TLS, and minio.New then rejects
		// the endpoint outright with its own "unsupported" message — the
		// misconfiguration still surfaces, through the client, not silently.
		{"unsupported scheme is not TLS", "ftp://host", false},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			if got := SecureFromEndpoint(tc.endpoint); got != tc.want {
				t.Fatalf("SecureFromEndpoint(%q) = %v, want %v", tc.endpoint, got, tc.want)
			}
		})
	}
}

// probeMockS3 is the S3-API subset the readiness probe exercises: HEAD on
// the bucket (BucketExists), PUT (sentinel write) and DELETE (cleanup). It
// records the last written sentinel key so the test can assert the probe
// really round-tripped.
type probeMockS3 struct {
	srv     *httptest.Server
	buckets map[string]bool // bucket name -> exists
	mu      sync.Mutex
	putKeys []string
}

func newProbeMockS3(t *testing.T, tls bool, buckets ...string) *probeMockS3 {
	t.Helper()
	m := &probeMockS3{buckets: map[string]bool{}}
	for _, b := range buckets {
		m.buckets[b] = true
	}
	if tls {
		m.srv = httptest.NewTLSServer(http.HandlerFunc(m.handle))
	} else {
		m.srv = httptest.NewServer(http.HandlerFunc(m.handle))
	}
	t.Cleanup(m.srv.Close)
	return m
}

func (m *probeMockS3) handle(w http.ResponseWriter, r *http.Request) {
	path := strings.TrimPrefix(r.URL.Path, "/")
	bucket, key, _ := strings.Cut(path, "/")
	m.mu.Lock()
	exists := m.buckets[bucket]
	m.mu.Unlock()

	switch {
	case r.Method == http.MethodHead && key == "":
		if !exists {
			w.WriteHeader(http.StatusNotFound)
			return
		}
		w.Header().Set("x-amz-bucket-region", "us-east-1")
		w.WriteHeader(http.StatusOK)
	case r.Method == http.MethodPut && key != "":
		if !exists {
			w.WriteHeader(http.StatusNotFound)
			return
		}
		m.mu.Lock()
		m.putKeys = append(m.putKeys, key)
		m.mu.Unlock()
		w.Header().Set("ETag", etagOf(r))
		w.WriteHeader(http.StatusOK)
	case r.Method == http.MethodDelete && key != "":
		w.WriteHeader(http.StatusNoContent)
	default:
		w.WriteHeader(http.StatusNotFound)
		_, _ = w.Write([]byte(`<?xml version="1.0" encoding="UTF-8"?><Error><Code>NoSuchKey</Code></Error>`))
	}
}

// etagOf is a fixed dummy ETag — the probe never checks it.
func etagOf(*http.Request) string {
	sum := sha256.Sum256([]byte("etag"))
	return `"` + hex.EncodeToString(sum[:]) + `"`
}

// newReadyServer builds the smallest Server whose /readyz answers: the
// probe chain needs Config and a pingable metadata store, nothing else.
func newReadyServer(t *testing.T, mutate func(*config.Config)) *httptest.Server {
	t.Helper()
	md, err := metadata.Open(context.Background(), metadata.Options{
		Driver: "sqlite",
		Path:   filepath.Join(t.TempDir(), "probe.db"),
	})
	if err != nil {
		t.Fatalf("metadata.Open: %v", err)
	}
	t.Cleanup(func() { _ = md.Close() })

	cfg := config.Defaults()
	cfg.Storage.DataDir = t.TempDir()
	if mutate != nil {
		mutate(cfg)
	}
	s := New(Deps{Config: cfg, Metadata: md, DataDir: cfg.Storage.DataDir}, nil)
	ts := httptest.NewServer(s.Handler())
	t.Cleanup(ts.Close)
	return ts
}

// TestReadinessS3ProbeScheme exercises both scheme branches of the S3
// readiness probe end-to-end (table-driven, T-178 AC 2):
//
//	http   against a live mock        -> 200 (the fixed bug; was a permanent
//	                                  503 while Secure was hardcoded true)
//	http   against a missing bucket   -> 503 naming the bucket
//	https  against an untrusted TLS   -> 503 failing at BucketExists — the
//	          mock                      client was BUILT (scheme/Secure agree)
//	                                  rather than rejected by minio.New
func TestReadinessS3ProbeScheme(t *testing.T) {
	mock := newProbeMockS3(t, false, "probe-bucket")
	tlsMock := newProbeMockS3(t, true, "probe-bucket")

	cases := []struct {
		name       string
		scheme     string
		server     *probeMockS3
		bucket     string
		wantStatus int
		wantDetail string // substring of the 503 body
		notDetail  string // must-NOT-appear substring
	}{
		{
			name:       "http endpoint ready",
			scheme:     "http",
			server:     mock,
			bucket:     "probe-bucket",
			wantStatus: http.StatusOK,
		},
		{
			name:       "http endpoint missing bucket",
			scheme:     "http",
			server:     mock,
			bucket:     "missing-bucket",
			wantStatus: http.StatusServiceUnavailable,
			wantDetail: "bucket does not exist",
			notDetail:  "conflicts with the secure option",
		},
		{
			name:       "https endpoint dials TLS",
			scheme:     "https",
			server:     tlsMock,
			bucket:     "probe-bucket",
			wantStatus: http.StatusServiceUnavailable,
			// The self-signed test certificate fails verification at
			// BucketExists — the detail prefix proves the client was
			// constructed (scheme and Secure agree) and the failure is on
			// the wire, not in minio.New.
			wantDetail: "s3 BucketExists",
			notDetail:  "conflicts with the secure option",
		},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			ts := newReadyServer(t, func(cfg *config.Config) {
				cfg.Storage.Backend = config.StorageBackendS3
				cfg.Storage.S3.Bucket = tc.bucket
				cfg.Storage.S3.Region = "us-east-1"
				cfg.Storage.S3.Endpoint = tc.server.srv.Listener.Addr().String()
				if tc.scheme != "" {
					cfg.Storage.S3.Endpoint = tc.scheme + "://" + cfg.Storage.S3.Endpoint
				}
				cfg.Storage.S3.AccessKeyID = "test"
				cfg.Storage.S3.SecretAccessKey = "test"
			})
			resp, err := ts.Client().Get(ts.URL + "/readyz")
			if err != nil {
				t.Fatalf("GET /readyz: %v", err)
			}
			defer func() { _ = resp.Body.Close() }()
			raw, _ := io.ReadAll(resp.Body)
			detail := string(raw)
			if resp.StatusCode != tc.wantStatus {
				t.Fatalf("/readyz status = %d, want %d (body: %s)", resp.StatusCode, tc.wantStatus, detail)
			}
			if tc.wantDetail != "" && !strings.Contains(detail, tc.wantDetail) {
				t.Errorf("body %q, want it to contain %q", detail, tc.wantDetail)
			}
			if tc.notDetail != "" && strings.Contains(detail, tc.notDetail) {
				t.Errorf("body %q must not contain %q (the pre-T-178 bug's minio.New rejection)", detail, tc.notDetail)
			}
		})
	}

	// The ready branch also round-tripped a sentinel write on the mock.
	mock.mu.Lock()
	wrote := len(mock.putKeys)
	mock.mu.Unlock()
	if wrote == 0 {
		t.Fatal("http-ready case never wrote a sentinel object to the mock bucket")
	}
}

// TestReadinessDiskProbeUnchanged is the disk-path zero-regression leg for
// the same probe switch: backend disk (the default) keeps answering /readyz
// 200 from the data directory, with no S3 client involved.
func TestReadinessDiskProbeUnchanged(t *testing.T) {
	ts := newReadyServer(t, nil) // config.Defaults(): backend disk
	resp, err := ts.Client().Get(ts.URL + "/readyz")
	if err != nil {
		t.Fatalf("GET /readyz: %v", err)
	}
	defer func() { _ = resp.Body.Close() }()
	if resp.StatusCode != http.StatusOK {
		t.Fatalf("/readyz status = %d, want 200 (disk backend regression)", resp.StatusCode)
	}
}
