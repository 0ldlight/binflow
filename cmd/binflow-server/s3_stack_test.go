package main

// T-178 tests: openStack must assemble the S3 data plane when
// storage.backend=s3 (the M6 gap — only the disk engine was ever opened),
// wire storage.migration per the T-164 MigrationEngine semantics, and keep
// the disk path byte-identical. The mock S3 server below reproduces the
// subset of the S3 API the engine exercises on the upload path (bucket
// probe, multipart begin/part/complete, copy, delete) — the same approach
// as internal/storage's s3_test mock, duplicated here because that mock is
// test-scoped to the storage package.

import (
	"bufio"
	"bytes"
	"context"
	"crypto/sha1"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"encoding/xml"
	"errors"
	"fmt"
	"io"
	"log/slog"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"sort"
	"strconv"
	"strings"
	"sync"
	"testing"
	"time"

	"github.com/lzwzzy/binflow/internal/adapter"
	"github.com/lzwzzy/binflow/internal/config"
	"github.com/lzwzzy/binflow/internal/httpapi"
	"github.com/lzwzzy/binflow/internal/metadata"
	"github.com/lzwzzy/binflow/internal/storage"
)

// ---------------------------------------------------------------------------
// mock S3 server (upload-path subset)
// ---------------------------------------------------------------------------

// s3Mock is an in-memory S3-compatible server. Buckets listed in the
// constructor exist; every other bucket answers 404 on HEAD (the
// bucket-exists probe), which lets the misconfiguration tests simulate a
// missing bucket without a second server.
type s3Mock struct {
	srv       *httptest.Server
	buckets   map[string]map[string][]byte // bucket -> key -> data
	mtimes    map[string]map[string]time.Time
	uploads   map[string]map[int][]byte // uploadID -> part number -> data
	uploadKey map[string]string         // uploadID -> object key
	mu        sync.Mutex
}

func newS3Mock(t *testing.T, buckets ...string) *s3Mock {
	t.Helper()
	m := &s3Mock{
		buckets:   map[string]map[string][]byte{},
		mtimes:    map[string]map[string]time.Time{},
		uploads:   map[string]map[int][]byte{},
		uploadKey: map[string]string{},
	}
	for _, b := range buckets {
		m.buckets[b] = map[string][]byte{}
		m.mtimes[b] = map[string]time.Time{}
	}
	m.srv = httptest.NewServer(http.HandlerFunc(m.handle))
	t.Cleanup(m.srv.Close)
	return m
}

// setObjectLastModified backdates one object's LastModified (the mtime the
// engine's inventory reports — the export path preserves it as the
// artifact's file mtime, W28/W32).
func (m *s3Mock) setObjectLastModified(bucket, key string, t time.Time) {
	m.mu.Lock()
	defer m.mu.Unlock()
	if mt, ok := m.mtimes[bucket]; ok {
		mt[key] = t
	}
}

// touchObjectLocked records an object's write time (callers hold m.mu).
func (m *s3Mock) touchObjectLocked(bucket, key string) {
	if mt, ok := m.mtimes[bucket]; ok {
		mt[key] = time.Now().UTC()
	}
}

// xmlEscapeText escapes one XML text node (list prefix / key).
func xmlEscapeText(s string) string {
	var buf bytes.Buffer
	xml.Escape(&buf, []byte(s))
	return buf.String()
}

// endpoint renders the documented config spelling: a URL with scheme.
func (m *s3Mock) endpoint() string { return "http://" + m.srv.Listener.Addr().String() }

// object returns the stored bytes for bucket/key (nil when absent).
func (m *s3Mock) object(bucket, key string) []byte {
	m.mu.Lock()
	defer m.mu.Unlock()
	return m.buckets[bucket][key]
}

func (m *s3Mock) handle(w http.ResponseWriter, r *http.Request) {
	path := strings.TrimPrefix(r.URL.Path, "/")
	bucket, key, _ := strings.Cut(path, "/")
	query := r.URL.Query()
	uploadID := query.Get("uploadId")

	m.mu.Lock()
	objects, hasBucket := m.buckets[bucket]
	m.mu.Unlock()

	switch {
	case r.Method == http.MethodHead && key == "":
		// BucketExists probe.
		if !hasBucket {
			w.WriteHeader(http.StatusNotFound)
			return
		}
		w.Header().Set("x-amz-bucket-region", "us-east-1")
		w.WriteHeader(http.StatusOK)

	case r.Method == http.MethodHead && key != "":
		// StatObject. Last-Modified is load-bearing: minio-go's lazy
		// Object.Stat() satisfies itself through a HEAD and refuses to
		// parse a response without the header (the read paths then fail
		// with "Last-Modified time format is invalid").
		m.mu.Lock()
		data, ok := objects[key]
		lm := m.mtimes[bucket][key]
		m.mu.Unlock()
		if !ok {
			w.WriteHeader(http.StatusNotFound)
			return
		}
		if lm.IsZero() {
			lm = time.Now().UTC()
		}
		w.Header().Set("Content-Length", strconv.Itoa(len(data)))
		w.Header().Set("Last-Modified", lm.Format(http.TimeFormat))
		w.Header().Set("ETag", etag(data))
		w.WriteHeader(http.StatusOK)

	case r.Method == http.MethodGet && key == "" && query.Get("list-type") == "2":
		// ListObjectsV2 — the read-only inventory walk (BlobStats, GC,
		// migration scans). Non-truncated single page, keys sorted.
		if !hasBucket {
			w.WriteHeader(http.StatusNotFound)
			return
		}
		prefix := query.Get("prefix")
		m.mu.Lock()
		keys := make([]string, 0, len(objects))
		for k := range objects {
			if strings.HasPrefix(k, prefix) {
				keys = append(keys, k)
			}
		}
		sort.Strings(keys)
		var buf bytes.Buffer
		buf.WriteString(`<?xml version="1.0" encoding="UTF-8"?><ListBucketResult xmlns="http://s3.amazonaws.com/doc/2006-03-01/">`)
		buf.WriteString(`<Name>` + bucket + `</Name>`)
		buf.WriteString(`<Prefix>` + xmlEscapeText(prefix) + `</Prefix>`)
		buf.WriteString(`<KeyCount>` + strconv.Itoa(len(keys)) + `</KeyCount>`)
		buf.WriteString(`<MaxKeys>1000</MaxKeys>`)
		buf.WriteString(`<IsTruncated>false</IsTruncated>`)
		for _, k := range keys {
			lm := m.mtimes[bucket][k]
			if lm.IsZero() {
				lm = time.Now().UTC()
			}
			buf.WriteString(`<Contents>`)
			buf.WriteString(`<Key>` + xmlEscapeText(k) + `</Key>`)
			buf.WriteString(`<LastModified>` + lm.Format(time.RFC3339Nano) + `</LastModified>`)
			buf.WriteString(`<Size>` + strconv.Itoa(len(objects[k])) + `</Size>`)
			buf.WriteString(`<ETag>` + etag(objects[k]) + `</ETag>`)
			buf.WriteString(`<StorageClass>STANDARD</StorageClass>`)
			buf.WriteString(`</Contents>`)
		}
		buf.WriteString(`</ListBucketResult>`)
		m.mu.Unlock()
		w.Header().Set("Content-Type", "application/xml")
		w.WriteHeader(http.StatusOK)
		_, _ = w.Write(buf.Bytes())

	case r.Method == http.MethodGet && key != "":
		// GetObject — the blob read stream (engine Open: export, GC stat,
		// the import existence probe).
		m.mu.Lock()
		data, ok := objects[key]
		lm := m.mtimes[bucket][key]
		m.mu.Unlock()
		if !ok {
			w.WriteHeader(http.StatusNotFound)
			_, _ = w.Write([]byte(`<?xml version="1.0" encoding="UTF-8"?><Error><Code>NoSuchKey</Code><Message>Not Found</Message></Error>`))
			return
		}
		if lm.IsZero() {
			lm = time.Now().UTC()
		}
		w.Header().Set("Content-Length", strconv.Itoa(len(data)))
		w.Header().Set("Last-Modified", lm.Format(http.TimeFormat))
		w.Header().Set("ETag", etag(data))
		w.Header().Set("Accept-Ranges", "bytes")
		w.WriteHeader(http.StatusOK)
		_, _ = w.Write(data)

	case r.Method == http.MethodPost && key != "" && query.Has("uploads"):
		// NewMultipartUpload.
		id := fmt.Sprintf("upload-%d", time.Now().UnixNano())
		m.mu.Lock()
		m.uploads[id] = map[int][]byte{}
		m.uploadKey[id] = key
		m.mu.Unlock()
		writeXML(w, http.StatusOK, struct {
			XMLName  xml.Name `xml:"InitiateMultipartUploadResult"`
			UploadID string   `xml:"UploadId"`
		}{UploadID: id})

	case r.Method == http.MethodPut && key != "" && query.Has("partNumber") && uploadID != "":
		// PutObjectPart.
		data, err := readBody(r)
		if err != nil {
			w.WriteHeader(http.StatusBadRequest)
			return
		}
		var part int
		if _, err := fmt.Sscanf(query.Get("partNumber"), "%d", &part); err != nil {
			w.WriteHeader(http.StatusBadRequest)
			return
		}
		m.mu.Lock()
		m.uploads[uploadID][part] = data
		m.mu.Unlock()
		w.Header().Set("ETag", etag(data))
		w.WriteHeader(http.StatusOK)

	case r.Method == http.MethodPost && key != "" && uploadID != "":
		// CompleteMultipartUpload: concatenate parts in part-number order
		// at the upload key.
		m.mu.Lock()
		parts, ok := m.uploads[uploadID]
		delete(m.uploads, uploadID)
		delete(m.uploadKey, uploadID)
		m.mu.Unlock()
		if !ok {
			w.WriteHeader(http.StatusNotFound)
			return
		}
		nums := make([]int, 0, len(parts))
		for n := range parts {
			nums = append(nums, n)
		}
		sort.Ints(nums)
		var combined []byte
		for _, n := range nums {
			combined = append(combined, parts[n]...)
		}
		m.mu.Lock()
		objects[key] = combined
		m.touchObjectLocked(bucket, key)
		m.mu.Unlock()
		// Bucket and Key must be non-empty: minio-go treats a completion
		// response without them as an embedded <Error> document.
		writeXML(w, http.StatusOK, struct {
			XMLName  xml.Name `xml:"CompleteMultipartUploadResult"`
			Location string   `xml:"Location"`
			Bucket   string   `xml:"Bucket"`
			Key      string   `xml:"Key"`
			ETag     string   `xml:"ETag"`
		}{Location: "/" + key, Bucket: bucket, Key: key, ETag: etag(combined)})

	case r.Method == http.MethodPut && key != "" && r.Header.Get("x-amz-copy-source") != "":
		// CopyObject: copy the source object (same bucket) to key.
		src := strings.TrimPrefix(r.Header.Get("x-amz-copy-source"), "/")
		_, srcKey, _ := strings.Cut(src, "/")
		m.mu.Lock()
		data, ok := objects[srcKey]
		if ok {
			objects[key] = data
			// Server-side COPY writes a new object: LastModified is now
			// (real S3 semantics; the engine's Commit path relies on it
			// for GC grace — see T-173 D-5 for the metadata caveat).
			m.touchObjectLocked(bucket, key)
		}
		m.mu.Unlock()
		if !ok {
			w.WriteHeader(http.StatusNotFound)
			return
		}
		writeXML(w, http.StatusOK, struct {
			XMLName xml.Name `xml:"CopyObjectResult"`
			ETag    string   `xml:"ETag"`
		}{ETag: etag(data)})

	case r.Method == http.MethodPut && key != "":
		// PutObject (sentinel probe / empty blob).
		data, err := readBody(r)
		if err != nil {
			w.WriteHeader(http.StatusBadRequest)
			return
		}
		m.mu.Lock()
		objects[key] = data
		m.touchObjectLocked(bucket, key)
		m.mu.Unlock()
		w.Header().Set("ETag", etag(data))
		w.WriteHeader(http.StatusOK)

	case r.Method == http.MethodDelete && key != "" && uploadID != "":
		// AbortMultipartUpload.
		m.mu.Lock()
		delete(m.uploads, uploadID)
		delete(m.uploadKey, uploadID)
		m.mu.Unlock()
		w.WriteHeader(http.StatusNoContent)

	case r.Method == http.MethodDelete && key != "":
		// RemoveObject — idempotent 204 keeps minio-go happy.
		m.mu.Lock()
		delete(objects, key)
		m.mu.Unlock()
		w.WriteHeader(http.StatusNoContent)

	default:
		w.WriteHeader(http.StatusNotFound)
	}
}

func etag(data []byte) string {
	sum := sha256.Sum256(data)
	return `"` + hex.EncodeToString(sum[:]) + `"`
}

// readBody returns the request's decoded payload. minio-go signs uploads
// with SigV4 streaming chunks (Content-Encoding: aws-chunked) — real S3
// strips the framing before storage, so the mock must too; every other
// body passes through raw. (internal/storage's mock dodges this by
// authenticating with SigV2; the production client here is V4.)
func readBody(r *http.Request) ([]byte, error) {
	if !strings.Contains(r.Header.Get("Content-Encoding"), "aws-chunked") {
		return io.ReadAll(r.Body)
	}
	br := bufio.NewReader(r.Body)
	var out []byte
	for {
		header, err := br.ReadString('\n')
		if err != nil {
			return nil, err
		}
		sizeStr, _, _ := strings.Cut(strings.TrimSpace(header), ";")
		n, err := strconv.ParseInt(sizeStr, 16, 64)
		if err != nil {
			return nil, fmt.Errorf("aws-chunked header %q: %w", header, err)
		}
		if n == 0 {
			return out, nil
		}
		chunk := make([]byte, n)
		if _, err := io.ReadFull(br, chunk); err != nil {
			return nil, err
		}
		out = append(out, chunk...)
		if _, err := br.Discard(2); err != nil { // chunk-terminating CRLF
			return nil, err
		}
	}
}

func writeXML(w http.ResponseWriter, code int, v any) {
	w.Header().Set("Content-Type", "application/xml")
	w.WriteHeader(code)
	_, _ = w.Write([]byte(xml.Header))
	_ = xml.NewEncoder(w).Encode(v)
}

// ---------------------------------------------------------------------------
// helpers
// ---------------------------------------------------------------------------

// s3TestConfig builds a backend=s3 config against the mock. The secret is
// deliberately left to the environment unless the caller sets it, so the
// env leg of openS3Engine is the exercised path.
func s3TestConfig(t *testing.T, endpoint, bucket string) *config.Config {
	t.Helper()
	cfg := configDefaults()
	cfg.Storage.Backend = config.StorageBackendS3
	// The sqlite database still lives in data_dir; the disk half of
	// dual-write lands there too.
	cfg.Storage.DataDir = t.TempDir()
	cfg.Storage.S3 = config.S3Config{
		Bucket:       bucket,
		Region:       "us-east-1",
		Endpoint:     endpoint,
		AccessKeyID:  "test-access-key",
		UsePathStyle: true,
	}
	return cfg
}

// putBlobViaSession streams content through one full engine session — the
// upload path serve actually drives — and returns the blob's sha256.
func putBlobViaSession(ctx context.Context, t *testing.T, st storage.Engine, content string) string {
	t.Helper()
	sess, err := st.BeginSession(ctx)
	if err != nil {
		t.Fatalf("BeginSession: %v", err)
	}
	if _, err := sess.Append(ctx, strings.NewReader(content)); err != nil {
		t.Fatalf("Append: %v", err)
	}
	sum := sha256.Sum256([]byte(content))
	want := hex.EncodeToString(sum[:])
	ref, err := sess.Commit(ctx, storage.BlobRef{Sha256: want, Size: int64(len(content))})
	if err != nil {
		t.Fatalf("Commit: %v", err)
	}
	if ref.Sha256 != want {
		t.Fatalf("commit sha256 = %s, want %s", ref.Sha256, want)
	}
	return want
}

// blobKey is the S3Engine key shape (mirrors objectKey in storage/s3.go).
func blobKey(sha string) string { return "blobs/" + sha[:2] + "/" + sha }

// testSlogLogger is a discard logger — openStack's log lines are not under
// test here (the s3/dual-write lines are smoke-visible via -v when needed).
func testSlogLogger(t *testing.T) *slog.Logger {
	t.Helper()
	return slog.New(slog.NewTextHandler(io.Discard, nil))
}

// ---------------------------------------------------------------------------
// tests
// ---------------------------------------------------------------------------

// TestOpenStackS3BackendUploadsToBucket (T-178 AC 1 + AC 3①): with
// storage.backend=s3, openStack assembles the S3 engine — an upload session
// lands the blob IN THE BUCKET, not on local disk. The secret arrives only
// through BINFLOW_STORAGE_S3_SECRET_ACCESS_KEY (the env leg of the
// construction).
func TestOpenStackS3BackendUploadsToBucket(t *testing.T) {
	mock := newS3Mock(t, "binflow")
	withEnv(t, map[string]string{config.S3SecretEnvVar: "test-secret"})

	cfg := s3TestConfig(t, mock.endpoint(), "binflow")
	stack, err := openStack(context.Background(), cfg, testSlogLogger(t))
	if err != nil {
		t.Fatalf("openStack(backend=s3): %v", err)
	}
	defer stack.close(testSlogLogger(t))

	sha := putBlobViaSession(context.Background(), t, stack.st, "s3-backend-blob")

	if got := mock.object("binflow", blobKey(sha)); string(got) != "s3-backend-blob" {
		t.Fatalf("bucket object %q = %q, want the uploaded content", blobKey(sha), got)
	}
	// The disk blob tree must NOT carry the blob — the S3 data plane is the
	// active one, this is the whole point of the wiring.
	if _, err := os.Stat(filepath.Join(cfg.Storage.DataDir, "blobs")); !errors.Is(err, os.ErrNotExist) {
		t.Errorf("backend=s3 created a local blobs/ tree at %s, stat err = %v", cfg.Storage.DataDir, err)
	}
}

// TestOpenStackS3Misconfigurations pins the two fail-fast refusals (plus the
// disk+migration combination refusal) table-driven.
func TestOpenStackS3Misconfigurations(t *testing.T) {
	mock := newS3Mock(t, "binflow")

	cases := []struct {
		name    string
		mutate  func(t *testing.T, cfg *config.Config)
		wantErr string
	}{
		{
			name: "secret absent everywhere",
			mutate: func(t *testing.T, cfg *config.Config) {
				t.Helper()
				cfg.Storage.S3.SecretAccessKey = ""
				_ = os.Unsetenv(config.S3SecretEnvVar)
			},
			wantErr: config.S3SecretEnvVar,
		},
		{
			name: "bucket does not exist",
			mutate: func(_ *testing.T, cfg *config.Config) {
				cfg.Storage.S3.Bucket = "not-created"
				cfg.Storage.S3.SecretAccessKey = "test-secret"
			},
			wantErr: "does not exist",
		},
		{
			name: "migration enabled under backend disk",
			mutate: func(_ *testing.T, cfg *config.Config) {
				cfg.Storage.Backend = config.StorageBackendDisk
				cfg.Storage.Migration.Enabled = true
				cfg.Storage.Migration.Concurrency = 2
			},
			wantErr: "storage.migration.enabled requires storage.backend=s3",
		},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			withEnv(t, map[string]string{config.S3SecretEnvVar: "test-secret"})
			cfg := s3TestConfig(t, mock.endpoint(), "binflow")
			tc.mutate(t, cfg)
			_, err := openStack(context.Background(), cfg, testSlogLogger(t))
			if err == nil {
				t.Fatalf("openStack succeeded, want refusal containing %q", tc.wantErr)
			}
			if !strings.Contains(err.Error(), tc.wantErr) {
				t.Fatalf("openStack error = %v, want it to contain %q", err, tc.wantErr)
			}
		})
	}
}

// TestOpenStackMigrationDualWrite (T-178 AC 1 migration leg): enabled &&
// !completed boots the MigrationEngine — one upload lands in BOTH the disk
// tree and the bucket, and the engine satisfies the httpapi seam the REST
// migration endpoints consume.
func TestOpenStackMigrationDualWrite(t *testing.T) {
	mock := newS3Mock(t, "binflow")
	withEnv(t, map[string]string{config.S3SecretEnvVar: "test-secret"})

	cfg := s3TestConfig(t, mock.endpoint(), "binflow")
	cfg.Storage.Migration.Enabled = true
	cfg.Storage.Migration.Completed = false
	cfg.Storage.Migration.Concurrency = 2

	stack, err := openStack(context.Background(), cfg, testSlogLogger(t))
	if err != nil {
		t.Fatalf("openStack(dual-write): %v", err)
	}
	defer stack.close(testSlogLogger(t))

	mig, ok := stack.st.(*storage.MigrationEngine)
	if !ok {
		t.Fatalf("engine type = %T, want *storage.MigrationEngine in dual-write mode", stack.st)
	}
	// The seam newAssembledServer wires into Deps.Migration, exercised via
	// the engine directly — since T-180 the seam's signatures ARE the
	// engine's method set, so the engine plugs in without an adapter (a
	// direct behavior round-trip through newAssembledServer is impossible
	// in tests: a second assembly panics the process-wide adapter registry,
	// T-168's known full-suite red).
	var starter httpapi.MigrationStarter = mig
	if starter.StatusView() == nil {
		t.Fatal("StatusView() = nil, want a view (the JSON shape T-160 renders)")
	}
	if view := starter.StatusView(); view.Running {
		t.Error("freshly booted dual-write reports a running migration, want idle")
	}

	sha := putBlobViaSession(context.Background(), t, stack.st, "dual-write-blob")

	if got := mock.object("binflow", blobKey(sha)); string(got) != "dual-write-blob" {
		t.Fatalf("s3 copy of dual-write blob = %q, want the uploaded content", got)
	}
	diskPath, err := storage.BlobPath(cfg.Storage.DataDir, sha)
	if err != nil {
		t.Fatalf("BlobPath: %v", err)
	}
	if data, err := os.ReadFile(diskPath); err != nil || string(data) != "dual-write-blob" {
		t.Fatalf("disk copy of dual-write blob: read = %v (%q), want the uploaded content", err, data)
	}
}

// TestOpenStackMigrationCompletedS3Only: completed=true boots S3 alone (the
// migration's terminal state — S3 IS the source of truth), so an upload
// lands in the bucket and NOT on disk.
func TestOpenStackMigrationCompletedS3Only(t *testing.T) {
	mock := newS3Mock(t, "binflow")
	withEnv(t, map[string]string{config.S3SecretEnvVar: "test-secret"})

	cfg := s3TestConfig(t, mock.endpoint(), "binflow")
	cfg.Storage.Migration.Enabled = true
	cfg.Storage.Migration.Completed = true

	stack, err := openStack(context.Background(), cfg, testSlogLogger(t))
	if err != nil {
		t.Fatalf("openStack(migration completed): %v", err)
	}
	defer stack.close(testSlogLogger(t))

	if _, ok := stack.st.(*storage.MigrationEngine); ok {
		t.Fatal("completed migration must not wrap the engines: S3 alone is the source of truth")
	}
	sha := putBlobViaSession(context.Background(), t, stack.st, "completed-blob")
	if got := mock.object("binflow", blobKey(sha)); string(got) != "completed-blob" {
		t.Fatalf("bucket object = %q, want the uploaded content", got)
	}
	if _, err := os.Stat(filepath.Join(cfg.Storage.DataDir, "blobs")); !errors.Is(err, os.ErrNotExist) {
		t.Errorf("completed migration wrote to the local blobs/ tree, stat err = %v", err)
	}
}

// TestOpenStorageEngineDiskDefaultZeroRegression (T-178 AC 3③): the default
// (disk) branch of the new selector keeps the M1 behavior — same engine,
// blob file at the documented path shape.
func TestOpenStorageEngineDiskDefaultZeroRegression(t *testing.T) {
	cfg := configDefaults()
	cfg.Storage.DataDir = t.TempDir()

	md, err := metadata.Open(context.Background(), metadata.Options{
		Driver: "sqlite",
		DSN:    sqlitePath(cfg),
	})
	if err != nil {
		t.Fatalf("metadata.Open: %v", err)
	}
	defer func() { _ = md.Close() }()

	st, err := openStorageEngine(context.Background(), cfg, testSlogLogger(t), md)
	if err != nil {
		t.Fatalf("openStorageEngine(disk): %v", err)
	}
	defer func() { _ = st.Close() }()

	sha := putBlobViaSession(context.Background(), t, st, "disk-blob")
	path, err := storage.BlobPath(cfg.Storage.DataDir, sha)
	if err != nil {
		t.Fatalf("BlobPath: %v", err)
	}
	if data, err := os.ReadFile(path); err != nil || string(data) != "disk-blob" {
		t.Fatalf("disk blob at %s: read = %v (%q)", path, err, data)
	}
}

// ---- T-289: the MPU REST seam's assembly wiring ----

// mpuTestServer builds the HTTP surface over a real openStack result with
// the T-289 Deps entry wired the way newAssembledServer does (the
// full-assembly-once constraint keeps this on the light Deps shape —
// metrics_wiring_test.go's note).
func mpuTestServer(t *testing.T, cfg *config.Config, st *stack) *httptest.Server {
	t.Helper()
	deps := httpapi.Deps{
		Config: cfg, Auth: st.authSvc, Authz: st.authSvc,
		Metadata: st.md, Repos: st.md.Repos(), ReposSvc: st.svc,
		Passwords: st.authSvc, Tokens: st.authSvc,
		GC: st.st, DataDir: cfg.Storage.DataDir,
		// The stack's own generic handler — the complete leg's artifact GET
		// rides the content plane (npm/maven/pypi stay unmounted: the
		// process-wide registry cannot register them twice in one process).
		Adapters: []adapter.Handler{st.genericHandler},
	}
	if mpu, ok := st.st.(storage.MultipartUploads); ok {
		deps.Uploads = mpu
	}
	s := httpapi.New(deps, testSlogLogger(t))
	ts := httptest.NewServer(s.Handler())
	t.Cleanup(ts.Close)
	return ts
}

// TestMPUSeamWiringS3ChainAndDisk501 pins the seam's presence rule where
// it is decided: a pure-S3 stack carries storage.MultipartUploads and the
// /api/v1/uploads plane drives the T-332 (ADR-0039) chain — create
// (POST + QueryParam -> the session token), one part on the relay target,
// complete?sha1= 202, the async task polled to Finished, the client's
// checksum-deploy landing — whose blob lands IN THE BUCKET; a disk stack
// discovers no seam and the data endpoints answer the honest plain-text
// 501 (FR-90-AC3). A dual-write MigrationEngine is covered by the type
// assertion's miss arm on the disk leg — it fronts the disk path and
// deliberately does not implement the capability.
func TestMPUSeamWiringS3ChainAndDisk501(t *testing.T) {
	// --- S3 leg ---
	mock := newS3Mock(t, "binflow")
	withEnv(t, map[string]string{config.S3SecretEnvVar: "test-secret"})
	cfg := s3TestConfig(t, mock.endpoint(), "binflow")
	st := testStack(t, cfg)

	if _, ok := st.st.(storage.MultipartUploads); !ok {
		t.Fatal("pure-S3 stack does not carry storage.MultipartUploads")
	}
	ts := mpuTestServer(t, cfg, st)

	// Repository through the real REST plane.
	code, body := httpDo(t, ts, http.MethodPut, "/binflow/api/repositories/mpu-s3",
		`{"rclass":"local","packageType":"generic"}`)
	if code != http.StatusOK {
		t.Fatalf("PUT repository = %d: %s", code, body)
	}

	// create (the flipped wire): POST + QueryParam -> 200 + the token.
	code, body = httpDo(t, ts, http.MethodPost,
		"/binflow/api/v1/uploads/create?repoKey=mpu-s3&repoPath=deep/large.bin&partSizeMB=2", "")
	if code != http.StatusOK {
		t.Fatalf("create = %d: %s", code, body)
	}
	var created struct {
		Token string `json:"token"`
	}
	if err := json.Unmarshal([]byte(body), &created); err != nil || created.Token == "" {
		t.Fatalf("create body carries no token: %v (%s)", err, body)
	}
	_, sessionID, found := strings.Cut(created.Token, ".")
	if !found || sessionID == "" {
		t.Fatalf("token %q carries no session id", created.Token)
	}

	// One short final part (the only part may be short, S3 contract) on
	// the manual-driver lane (shared credentials + `w`). 200 — the S3
	// PutObject shape the presigned-part clients require.
	payload := []byte("mpu!")
	code, body = httpDoRaw(t, ts, http.MethodPut,
		"/binflow/api/v1/uploads/part/"+sessionID+"/1", payload)
	if code != http.StatusOK {
		t.Fatalf("part PUT = %d: %s", code, body)
	}

	// complete?sha1= (the algorithm flip): 202, the task model async.
	sum1 := sha1.Sum(payload)
	code, body = httpDoBearer(t, ts, http.MethodPost,
		"/binflow/api/v1/uploads/complete?sha1="+hex.EncodeToString(sum1[:]), created.Token, nil)
	if code != http.StatusAccepted {
		t.Fatalf("complete = %d: %s", code, body)
	}

	// Poll the task to Finished: progress 100 + the checksum-deploy token.
	var finished struct {
		Status        string  `json:"status"`
		Progress      int     `json:"progress"`
		ChecksumToken *string `json:"checksumToken"`
	}
	deadline := time.Now().Add(10 * time.Second)
	for {
		code, body = httpDoBearer(t, ts, http.MethodPost, "/binflow/api/v1/uploads/status", created.Token, nil)
		if code != http.StatusOK {
			t.Fatalf("status = %d: %s", code, body)
		}
		if err := json.Unmarshal([]byte(body), &finished); err != nil {
			t.Fatalf("status body: %v (%s)", err, body)
		}
		if finished.Status == "NON_RETRYABLE_ERROR" {
			t.Fatalf("task failed: %s", body)
		}
		if finished.Status == "FINISHED" {
			break
		}
		if time.Now().After(deadline) {
			t.Fatalf("task never finished (last: %s)", body)
		}
		time.Sleep(20 * time.Millisecond)
	}
	if finished.Progress != 100 || finished.ChecksumToken == nil || *finished.ChecksumToken == "" {
		t.Fatalf("Finished body = %s, want progress 100 + the checksum token", body)
	}

	// The blob is IN THE BUCKET the moment the assembly finishes; the node
	// is the client's checksum-deploy away.
	sum := sha256.Sum256(payload)
	sha := hex.EncodeToString(sum[:])
	if got := mock.object("binflow", blobKey(sha)); string(got) != "mpu!" {
		t.Fatalf("bucket object = %q, want the uploaded content at %s", got, blobKey(sha))
	}
	code, _ = httpDoRaw(t, ts, http.MethodGet, "/binflow/mpu-s3/deep/large.bin", nil)
	if code != http.StatusNotFound {
		t.Fatalf("artifact before checksum-deploy = %d, want 404 (the node is the client's landing)", code)
	}
	req, err := http.NewRequest(http.MethodPut, ts.URL+"/binflow/mpu-s3/deep/large.bin", nil)
	if err != nil {
		t.Fatalf("build deploy request: %v", err)
	}
	req.Header.Set("Authorization", "Bearer "+*finished.ChecksumToken)
	req.Header.Set("X-Checksum-Deploy", "true")
	req.Header.Set("X-Checksum-Sha1", hex.EncodeToString(sum1[:]))
	resp, err := http.DefaultClient.Do(req)
	if err != nil {
		t.Fatalf("checksum-deploy PUT: %v", err)
	}
	raw, _ := io.ReadAll(resp.Body)
	_ = resp.Body.Close()
	if resp.StatusCode != http.StatusCreated {
		t.Fatalf("checksum-deploy PUT = %d: %s", resp.StatusCode, raw)
	}
	code, body = httpDoRaw(t, ts, http.MethodGet, "/binflow/mpu-s3/deep/large.bin", nil)
	if code != http.StatusOK || string(body) != "mpu!" {
		t.Fatalf("artifact GET = %d (%q), want 200 mpu!", code, body)
	}

	// --- disk leg: the honest 501 (config answers the probe's false) ---
	diskCfg := config.Defaults()
	diskCfg.Storage.DataDir = t.TempDir()
	diskSt := testStack(t, diskCfg)
	if _, ok := diskSt.st.(storage.MultipartUploads); ok {
		t.Fatal("disk stack must NOT carry storage.MultipartUploads")
	}
	diskTS := mpuTestServer(t, diskCfg, diskSt)
	code, body = httpDo(t, diskTS, http.MethodPost,
		"/binflow/api/v1/uploads/create?repoKey=any&repoPath=a.bin", "")
	if code != http.StatusNotImplemented || !strings.Contains(body, "not supported on this backend") {
		t.Fatalf("disk create = %d: %s, want the plain-text 501", code, body)
	}
	code, body = httpDo(t, diskTS, http.MethodGet, "/binflow/api/v1/uploads/config", "")
	if code != http.StatusOK || !strings.Contains(body, `"supported": false`) {
		t.Fatalf("disk config = %d: %s, want 200 supported:false (the probe)", code, body)
	}
}

// httpDo issues a JSON-body request with the default admin credential and
// returns (status, body).
func httpDo(t *testing.T, ts *httptest.Server, method, path, body string) (int, string) {
	t.Helper()
	resp, err := doRawRequest(ts, method, path, []byte(body))
	if err != nil {
		t.Fatalf("%s %s: %v", method, path, err)
	}
	return resp.StatusCode, resp.Body
}

// httpDoRaw is httpDo for raw payloads (empty body when nil).
func httpDoRaw(t *testing.T, ts *httptest.Server, method, path string, body []byte) (int, string) {
	t.Helper()
	resp, err := doRawRequest(ts, method, path, body)
	if err != nil {
		t.Fatalf("%s %s: %v", method, path, err)
	}
	return resp.StatusCode, resp.Body
}

// httpDoBearer is httpDoRaw on the MPU capability lane (the session token
// as the Bearer credential, the T-332 wire).
func httpDoBearer(t *testing.T, ts *httptest.Server, method, path, token string, body []byte) (int, string) {
	t.Helper()
	resp, err := doBearerRequest(ts, method, path, token, body)
	if err != nil {
		t.Fatalf("%s %s: %v", method, path, err)
	}
	return resp.StatusCode, resp.Body
}

// doBearerRequest is doRawRequest with the Authorization header swapped to
// the presented capability token.
func doBearerRequest(ts *httptest.Server, method, path, token string, body []byte) (*struct {
	StatusCode int
	Body       string
}, error) {
	var rdr io.Reader
	if body != nil {
		rdr = bytes.NewReader(body)
	}
	req, err := http.NewRequest(method, ts.URL+path, rdr)
	if err != nil {
		return nil, err
	}
	req.Header.Set("Authorization", "Bearer "+token)
	resp, err := http.DefaultClient.Do(req)
	if err != nil {
		return nil, err
	}
	defer func() { _ = resp.Body.Close() }()
	raw, _ := io.ReadAll(resp.Body)
	return &struct {
		StatusCode int
		Body       string
	}{resp.StatusCode, string(raw)}, nil
}

// doRawRequest performs the round trip with Basic admin credentials.
func doRawRequest(ts *httptest.Server, method, path string, body []byte) (*struct {
	StatusCode int
	Body       string
}, error) {
	var rdr io.Reader
	if body != nil {
		rdr = bytes.NewReader(body)
	}
	req, err := http.NewRequest(method, ts.URL+path, rdr)
	if err != nil {
		return nil, err
	}
	req.SetBasicAuth("admin", "password")
	resp, err := http.DefaultClient.Do(req)
	if err != nil {
		return nil, err
	}
	defer func() { _ = resp.Body.Close() }()
	raw, _ := io.ReadAll(resp.Body)
	return &struct {
		StatusCode int
		Body       string
	}{resp.StatusCode, string(raw)}, nil
}
