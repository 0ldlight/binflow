package repo

// The service layer's upload-staging root (T-477 — the T-474/T-476 spool
// family's service-layer arm). The protocol adapters stage their PUT
// bodies through internal/adapter/spool.go under <data_dir>/staging so
// uploads never depend on a writable OS temp directory (the read-only
// rootfs incident family); the exploded-upload face (ExplodeArchive)
// spooled its §4.2 to_extract_ file to OS temp and died the same death.
//
// internal/repo cannot import internal/adapter — the dependency direction
// is one-way (the adapters consume repo.Service, architecture section
// 5.4), so this file carries the SAME semantics as a service-side twin:
// resolve-or-create the root per use, 0o700, and a refusal that names the
// attempted root (bounded disclosure, the T-476 ruling: the root is the
// operator's own configuration and the one actionable fact a client
// output can carry; the os-error internals ride the server log). Folding
// both twins into one bottom package both may import is a REGISTERED
// follow-up — it would rewrite internal/adapter/spool.go's call sites,
// outside this ticket's area.

import (
	"context"
	"fmt"
	"log/slog"
	"net/http"
	"os"
)

// ConfigureStaging installs the upload-staging root on a Service built by
// New/NewWithClock (the ConfigureFolderDownload posture: assembly or
// tests call it before the first request is served). The production
// wiring is <storage data_dir>/staging — the same volume the blob store
// writes on and the same root the adapter spool family shares (cmd
// assembly passes the identical path to every Options.SpoolDir). dir ""
// keeps the OS-temp fallback (the bare test-harness posture); a staging
// refusal under it is honestly labeled as such on its face.
func ConfigureStaging(s Service, dir string) {
	impl, ok := s.(*service)
	if !ok {
		slog.Warn("repo: ConfigureStaging: service is not the concrete implementation; staging root not applied")
		return
	}
	impl.stagingDir = dir
}

// stagingRoot resolves the upload-staging root: dir verbatim when
// configured, the OS temp directory otherwise. The root is (re)created
// per call (0o700 — stricter than gosec G301's 0750 baseline; the root is
// server-internal scratch, never served): a volume mounted after boot
// self-heals on the next upload, and an unwritable root surfaces as that
// upload's own refusal instead of a boot-time dependency. On failure the
// RESOLVED root still returns — the refusal face and log name it.
func stagingRoot(dir string) (string, error) {
	root := dir
	if root == "" {
		root = os.TempDir()
	}
	if err := os.MkdirAll(root, 0o700); err != nil {
		return root, err
	}
	return root, nil
}

// stagingLabel renders the attempted staging root for the refusal face:
// the configured root quoted verbatim, or the OS-temp fallback named
// honestly (adapter.StagingLabel's twin semantics).
func stagingLabel(dir string) string {
	if dir != "" {
		return fmt.Sprintf("%q", dir)
	}
	return "the OS temp directory"
}

// stageUpload opens one temp file named per pattern under the
// upload-staging root; the caller owns the file and closes/removes it on
// every path. A staging refusal answers 507 Insufficient Storage — the
// T-474/T-476 family's mapping (the server cannot accept ANY upload in
// this environment; the pre-fix bare error surfaced as an opaque 500).
// The message is bounded disclosure: it names the attempted root only;
// the os-error detail rides the server log and the wrapped cause.
func (s *service) stageUpload(ctx context.Context, pattern string) (*os.File, error) {
	root, err := stagingRoot(s.stagingDir)
	if err != nil {
		return nil, s.stagingRefusal(ctx, root, err)
	}
	f, err := os.CreateTemp(root, pattern)
	if err != nil {
		return nil, s.stagingRefusal(ctx, root, err)
	}
	return f, nil
}

// stagingRefusal renders the 507 face and logs the diagnosable detail.
func (s *service) stagingRefusal(ctx context.Context, root string, err error) error {
	slog.ErrorContext(ctx, "repo: upload staging unavailable",
		"root", root, "error", err.Error())
	return &StatusError{
		Code:    http.StatusInsufficientStorage,
		Message: "Explode archive deployment failed: upload staging unavailable under " + stagingLabel(s.stagingDir) + ".",
		cause:   fmt.Errorf("stage upload under %s: %w", root, err),
	}
}
