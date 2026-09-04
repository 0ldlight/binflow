package adapter

import (
	"errors"
	"fmt"
	"os"
)

// ErrStagingUnavailable marks the upload-staging refusals (T-474's
// family): the staging root could not be resolved, created, or written.
// Adapters that shape their own error faces map it (507 Insufficient
// Storage); the wrapped text always names the root the server attempted,
// so a refusal read off the client output is diagnosable — never the
// pre-fix bare 500.
var ErrStagingUnavailable = errors.New("upload staging unavailable")

// StagingDir resolves the upload-spool root for dir. The production
// posture is a non-empty dir: the cmd assembly passes
// <storage data_dir>/staging so upload bodies stage on the SAME volume
// the blob store writes on — hardened deployments (read-only rootfs, the
// kubernetes norm) mount no writable /tmp, and the pre-T-474 spool
// assumed one. dir "" keeps the OS-temp fallback (the bare test-harness
// posture) — never silent: every refusal below names the resolved root.
//
// The root is (re)created per call: a volume mounted after boot
// self-heals on the next upload, and an unwritable root surfaces as that
// upload's own refusal instead of a boot-time dependency.
func StagingDir(dir string) (string, error) {
	root := dir
	if root == "" {
		root = os.TempDir()
	}
	// 0o700 is stricter than gosec G301's 0750 baseline; the staging root
	// is server-internal scratch, never served.
	if err := os.MkdirAll(root, 0o700); err != nil {
		return "", fmt.Errorf("%w under %s: %w", ErrStagingUnavailable, root, err)
	}
	return root, nil
}

// StagingLabel renders the attempted staging root for a refusal face: the
// configured root quoted verbatim, or the OS-temp fallback named honestly.
// This is the face's ONLY disclosure (T-476's bounded-disclosure ruling,
// T-474's precedent): the root is the operator's own configuration and the
// one actionable fact a client output can carry; the os-error internals
// and the temp file name ride the server log instead. The four spool
// adapters render through this one helper — helm carries its own
// pre-T-476 method of the same semantics (its area, untouched).
func StagingLabel(dir string) string {
	if dir != "" {
		return fmt.Sprintf("%q", dir)
	}
	return "the OS temp directory"
}

// StageFile resolves the staging root (StagingDir) and creates one temp
// file named per pattern inside it. The caller owns the file: it drains
// the upload into it and closes/removes it on every path.
func StageFile(dir, pattern string) (*os.File, error) {
	root, err := StagingDir(dir)
	if err != nil {
		return nil, err
	}
	f, err := os.CreateTemp(root, pattern)
	if err != nil {
		return nil, fmt.Errorf("%w under %s: %w", ErrStagingUnavailable, root, err)
	}
	return f, nil
}
