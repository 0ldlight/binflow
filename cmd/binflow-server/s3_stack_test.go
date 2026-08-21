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
	"context"
	"crypto/sha256"
	"encoding/hex"
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

	"github.com/lzwzzy/binflow/internal/config"
	"github.com/lzwzzy/binflow/internal/httpapi"
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
	uploads   map[string]map[int][]byte    // uploadID -> part number -> data
	uploadKey map[string]string            // uploadID -> object key
	mu        sync.Mutex
}

func newS3Mock(t *testing.T, buckets ...string) *s3Mock {
	t.Helper()
	m := &s3Mock{
		buckets:   map[string]map[string][]byte{},
		uploads:   map[string]map[int][]byte{},
		uploadKey: map[string]string{},
	}
	for _, b := range buckets {
		m.buckets[b] = map[string][]byte{}
	}
	m.srv = httptest.NewServer(http.HandlerFunc(m.handle))
	t.Cleanup(m.srv.Close)
	return m
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
		m.mu.Lock()
		data, ok := objects[key]
		m.mu.Unlock()
		if !ok {
			w.WriteHeader(http.StatusNotFound)
			return
		}
		w.Header().Set("Content-Length", fmt.Sprintf("%d", len(data)))
		w.WriteHeader(http.StatusOK)

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

	st, err := openStorageEngine(context.Background(), cfg, testSlogLogger(t))
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
