package deb

// The GPG release-signing leg (T-321, docs/design/gpg-keypair.md section
// 3.6 / docs/reverse/debian.md section 4.2): the Release family's
// signature pair — InRelease (the RFC 4880 cleartext-signature form) and
// Release.gpg (the armored detached signature). The local index engine
// signs every Release it recomputes; the virtual aggregate signs its own
// re-rendered Release the same way when the seam resolves a key (a
// managed-path virtual cannot carry the association at all — the repo
// config validation refuses keyPairName on non-local classes — so the
// aggregate's practical posture stays the registered unsigned one).
//
// The posture is seam-driven everywhere (gpg-keypair.md 3.6's error
// table): ErrNoKeypair and ErrUnavailable mean "skip signing and remove
// the stale signature files" — debian.md 4.2's rule that an old signature
// must not outlive the Release it signed; every other signing error is an
// operational failure the recompute surfaces. The passphrase never rides
// a request header (spec divergence D-8): it is sealed in the keypair row
// and the seam unseals it at signing time.

import (
	"context"
	"errors"
	"fmt"
	"log/slog"

	"github.com/lzwzzy/binflow/internal/keypair"
)

// ReleaseSigner is the consumer-side seam over keypair.SigningService
// (the interface-at-the-consumer rule; cmd assembles the concrete service
// from the same store/cipher/repo collaborators the keypair REST plane
// uses and injects it through Options). nil = unsigned mode: every
// repository answers the unsigned posture, never a content-plane failure.
type ReleaseSigner interface {
	// DetachedArmor renders the armored detached signature (Release.gpg).
	DetachedArmor(ctx context.Context, repoKey string, data []byte) (string, error)
	// Clearsign renders the cleartext-signature form (InRelease).
	Clearsign(ctx context.Context, repoKey string, data []byte) ([]byte, error)
}

// The signature files' served content types.
const (
	ctypeInRelease  = "text/plain; charset=utf-8"
	ctypeReleaseGpg = "application/pgp-signature"
)

// releaseSignatures renders one Release body's signature pair. signed=false
// with a nil error is the UNSIGNED posture: a repository with no key pair
// association (the ordinary unsigned mode, debug-logged) or a pair that
// cannot be opened (no master key / unopenable material — a
// configuration-state problem, WARN-logged) both leave the sweep to clear
// whatever stale signature files remain. Any other error is the caller's
// to surface.
func (h *Handler) releaseSignatures(ctx context.Context, repoKey string, release []byte) (inRelease []byte, releaseGpg string, signed bool, err error) {
	if h.signer == nil {
		return nil, "", false, nil
	}
	inRelease, err = h.signer.Clearsign(ctx, repoKey, release)
	if err != nil {
		if unsignedPosture(err) {
			logSkipSigning(ctx, repoKey, err)
			return nil, "", false, nil
		}
		return nil, "", false, fmt.Errorf("sign InRelease of %s: %w", repoKey, err)
	}
	releaseGpg, err = h.signer.DetachedArmor(ctx, repoKey, release)
	if err != nil {
		if unsignedPosture(err) {
			// Unreachable through the concrete seam (resolve precedes both
			// arms and answers identically) — a hand-rolled signer still
			// gets the graceful degrade, not a half-signed Release.
			logSkipSigning(ctx, repoKey, err)
			return nil, "", false, nil
		}
		return nil, "", false, fmt.Errorf("sign Release.gpg of %s: %w", repoKey, err)
	}
	return inRelease, releaseGpg, true, nil
}

// unsignedPosture classifies one seam error: the two sentinels drive
// "skip signing + sweep the stale signatures" (gpg-keypair.md section
// 3.6's table), everything else surfaces as an operational failure.
func unsignedPosture(err error) bool {
	return errors.Is(err, keypair.ErrNoKeypair) || errors.Is(err, keypair.ErrUnavailable)
}

// logSkipSigning logs one skipped-signing outcome at the level its cause
// merits: a repository without an association is the ordinary unsigned
// mode (debug — every unsigned recompute would otherwise spam), a pair
// that cannot be opened is a configuration problem the operator believed
// was solved (warn).
func logSkipSigning(ctx context.Context, repoKey string, err error) {
	if errors.Is(err, keypair.ErrUnavailable) {
		slog.WarnContext(ctx, "deb: release signing skipped — the associated key pair cannot be opened, serving an unsigned Release",
			slog.String("repo", repoKey), slog.String("reason", err.Error()))
		return
	}
	slog.DebugContext(ctx, "deb: release signing skipped — no key pair associated (unsigned mode)",
		slog.String("repo", repoKey))
}
