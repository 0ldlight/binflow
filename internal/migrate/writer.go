package migrate

import (
	"context"
	"io"
	"net/http"

	"github.com/lzwzzy/binflow/internal/client"
)

// TargetConfig configures the BinFlow target writer.
type TargetConfig struct {
	// BaseURL is the BinFlow server base URL (e.g. "http://localhost:8080").
	BaseURL string

	// Token is the admin API token sent as "Authorization: Bearer <token>".
	Token string

	// HTTP is the http.Client used for target writes. Nil selects
	// http.DefaultClient.
	HTTP *http.Client

	// RetryMax has internal/client semantics: 0 selects the client default
	// (5 attempts on transient 5xx/network errors), a negative value
	// disables retries (exactly one attempt). Tests inject -1 so injected
	// failures answer immediately instead of burning the backoff ladder.
	RetryMax int
}

// Writer writes migrated entities into the target BinFlow instance. It is
// a deliberate seam over internal/client: the migration pipeline depends
// on this narrow face, so client API drift (T-189 is realigning the
// client's decode faces against the real server) surfaces in one place.
type Writer struct {
	c *client.Client
}

// NewTargetWriter returns a Writer for the given target configuration.
func NewTargetWriter(cfg TargetConfig) *Writer {
	hc := cfg.HTTP
	if hc == nil {
		hc = http.DefaultClient
	}
	return &Writer{c: &client.Client{
		HTTP:     hc,
		BaseURL:  cfg.BaseURL,
		Token:    cfg.Token,
		RetryMax: cfg.RetryMax,
	}}
}

// EnsureRepo creates the repository, or replaces it when the key already
// exists (the real PUT /binflow/api/repositories/{key} plane is
// create-or-replace, which is what makes re-runs and --resume idempotent).
func (w *Writer) EnsureRepo(ctx context.Context, req client.RepoCreateRequest) error {
	if _, err := w.c.CreateRepo(ctx, req); err != nil {
		return err
	}
	return nil
}

// EnsureUser creates the user, or replaces the account when the name
// already exists (the real PUT /binflow/api/security/users/{name} plane is
// create-or-replace, answering 201 with no body in both outcomes).
func (w *Writer) EnsureUser(ctx context.Context, req client.UserCreateRequest) error {
	if _, err := w.c.CreateUser(ctx, req); err != nil {
		return err
	}
	return nil
}

// ListTargetRepos lists the target's repositories (GET
// /binflow/api/repositories, admin plane) — the empty-target guard's eyes
// (FR-63-AC1: an empty instance is one with ZERO repositories).
func (w *Writer) ListTargetRepos(ctx context.Context) ([]client.RepoInfo, error) {
	return w.c.ListRepos(ctx)
}

// UploadArtifact streams one file into a target repository path (PUT
// /binflow/{repo}/{path}, the plain-file content face the generic and maven
// adapters serve). Re-uploading an existing path replaces it, which is what
// makes artifact retries and --resume re-runs idempotent.
//
// Memory note: internal/client buffers a request body it intends to retry;
// the migration feeds it a disk-backed reader, so the peak cost is one
// artifact per in-flight worker, not the whole repository.
func (w *Writer) UploadArtifact(ctx context.Context, repo, path string, body io.Reader, size int64, contentType string) error {
	return w.c.UploadArtifact(ctx, repo, path, body, size, contentType)
}
