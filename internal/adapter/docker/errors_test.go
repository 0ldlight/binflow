package docker

// T-54 regression: busy-class metadata contention (SQLITE_BUSY that outlived
// busy_timeout) renders 503 UNAVAILABLE with Retry-After on the upload and
// manifest-publish paths — the retryable class the closed engine already
// occupies — instead of 500 UNKNOWN. Load shedding is not a fault, so the
// busy arm logs WARN, not ERROR.

import (
	"context"
	"errors"
	"fmt"
	"log/slog"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"

	"github.com/lzwzzy/binflow/internal/metadata"
	"github.com/lzwzzy/binflow/internal/repo"
	"github.com/lzwzzy/binflow/internal/storage"
)

// busyErr builds the error exactly as production would hand it to the
// adapter: wrapExec's sentinel + driver text inside a repo-layer re-wrap.
func busyErr() error {
	return fmt.Errorf("blob row abc: %w: %w",
		metadata.ErrStoreBusy, errors.New("database is locked (5) (SQLITE_BUSY)"))
}

func newMappingHandler(t *testing.T) *Handler {
	t.Helper()
	return New(nil, NewStaticRepoLookup(nil), nil, nil, nil, Options{}, slog.New(slog.NewTextHandler(&strings.Builder{}, nil)))
}

func TestWriteStoreFailureBusyMaps503(t *testing.T) {
	tests := []struct {
		name       string
		err        error
		wantStatus int
		wantCode   string
		wantRetry  bool
	}{
		{
			name:       "busy metadata contention",
			err:        busyErr(),
			wantStatus: http.StatusServiceUnavailable,
			wantCode:   ErrCodeUnavailable,
			wantRetry:  true,
		},
		{
			name:       "closed engine stays 503",
			err:        fmt.Errorf("push: %w", storage.ErrEngineClosed),
			wantStatus: http.StatusServiceUnavailable,
			wantCode:   ErrCodeUnavailable,
			wantRetry:  false,
		},
		{
			name:       "plain store failure stays 500 UNKNOWN",
			err:        errors.New("disk on fire"),
			wantStatus: http.StatusInternalServerError,
			wantCode:   ErrCodeUnknown,
			wantRetry:  false,
		},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			h := newMappingHandler(t)
			w := httptest.NewRecorder()
			r := httptest.NewRequest(http.MethodPost, "/v2/x/y/blobs/uploads/?digest=sha256:x", nil)
			h.writeStoreFailure(w, r, "commit upload test", tt.err)
			resp := w.Result()
			if resp.StatusCode != tt.wantStatus {
				t.Fatalf("status = %d, want %d", resp.StatusCode, tt.wantStatus)
			}
			assertSpecCode(t, readBody(t, resp), tt.wantCode)
			if got := resp.Header.Get("Retry-After"); tt.wantRetry && got == "" {
				t.Fatal("busy arm must carry Retry-After")
			} else if !tt.wantRetry && got != "" {
				t.Fatalf("non-busy failure carries Retry-After %q", got)
			}
		})
	}
}

// busyLandedService fails PutLandedBlob with the busy error, driving
// registerBlobNode — writeBlobCreated's real failure seam since T-64 (the
// registration no longer opens the blob; the service call is the whole
// metadata landing) — into the T-54 arm.
type busyLandedService struct {
	repo.Service
}

func (busyLandedService) PutLandedBlob(context.Context, *Principal, string, string, storage.BlobRef, string) (*metadata.Node, error) {
	return nil, fmt.Errorf("land blob row: %w", busyErr())
}

// TestWriteBlobCreatedBusyMaps503 pins the F1 failure site itself: the blob
// committed, the registration hit busy — the client gets 503 + retry cue with
// the digest detail, not 500 UNKNOWN.
func TestWriteBlobCreatedBusyMaps503(t *testing.T) {
	h := New(busyLandedService{}, NewStaticRepoLookup(nil), nil, nil, nil, Options{},
		slog.New(slog.NewTextHandler(&strings.Builder{}, nil)))
	w := httptest.NewRecorder()
	r := httptest.NewRequest(http.MethodPost, "/v2/docker-local/acme/app/blobs/uploads/", nil)
	ref := nameRef{repoKey: "docker-local", image: "acme/app"}
	h.writeBlobCreated(w, r, ref, storage.BlobRef{Sha256: strings.Repeat("a", 64)})
	resp := w.Result()
	if resp.StatusCode != http.StatusServiceUnavailable {
		t.Fatalf("status = %d, want 503", resp.StatusCode)
	}
	if resp.Header.Get("Retry-After") == "" {
		t.Fatal("busy arm must carry Retry-After")
	}
	body := readBody(t, resp)
	assertSpecCode(t, body, ErrCodeUnavailable)
	if !strings.Contains(string(body), "safe to retry") {
		t.Fatalf("body lacks the retry guidance: %s", body)
	}
	if !strings.Contains(string(body), "sha256:"+strings.Repeat("a", 64)) {
		t.Fatalf("body lacks the digest detail: %s", body)
	}
}
