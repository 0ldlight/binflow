package rpm

// The GPG repomd signing leg (T-322, docs/design/gpg-keypair.md section
// 3.6 / docs/reverse/rpm.md section 4.3): the metadata three-piece's
// signature pair — repomd.xml.asc (the RFC 4880 armored detached signature
// of the WHOLE repomd.xml) and repomd.xml.key (the associated pair's
// armored public key — the URL dnf's repo_gpgcheck=1 + gpgkey= consumes).
// The local reindex engine signs every repomd.xml it writes, at the fixed
// names, right after the flip write — so a rotation overwrites in place
// and a signed repository stays signed across generations.
//
// The virtual aggregate does NOT sign (RP-3 / T-315's registered 404
// posture): a virtual repository cannot carry a keyPairName association
// at all (the repo config validation refuses the field on non-local
// classes, ADR-0038 decision 5), and a MEMBER's signature would not
// verify against the merged document — rpm.md section 5's
// virtual-own-keypair clause is registered as diverged on that matrix.
//
// The posture is seam-driven everywhere (gpg-keypair.md 3.6's error
// table): ErrNoKeypair and ErrUnavailable mean "skip signing and remove
// the stale signature files" — rpm.md 4.3's rule that an old signature
// must not outlive the repomd.xml it signed; every other signing error is
// an operational failure the recompute surfaces. The passphrase never
// rides a request header (spec divergence D-8): X-GPG-PASSPHRASE is not
// collected — it is sealed in the keypair row and the seam unseals it at
// signing time.

import (
	"context"
	"errors"
	"fmt"
	"log/slog"
	"path"
	"strings"

	"github.com/lzwzzy/binflow/internal/keypair"
	"github.com/lzwzzy/binflow/internal/repo"
)

// RepomdSigner is the consumer-side seam over keypair.SigningService
// (the interface-at-the-consumer rule; cmd assembles the concrete service
// from the same store/cipher/repo collaborators the keypair REST plane
// uses and injects it through Options). nil = unsigned mode: every
// repository answers the unsigned posture, never a content-plane failure.
type RepomdSigner interface {
	// DetachedArmor renders the armored detached signature of data
	// (repomd.xml.asc).
	DetachedArmor(ctx context.Context, repoKey string, data []byte) (string, error)
	// PublicKey returns the repository's associated pair's armored public
	// key (repomd.xml.key).
	PublicKey(ctx context.Context, repoKey string) (string, error)
}

// ctypeRepomdSignature is the signature pair's served and stored content
// type (the pin repomdContentType serves .asc/.key under).
const ctypeRepomdSignature = "text/plain; charset=utf-8"

// repomdSignatures renders one repomd body's signature pair. signed=false
// with a nil error is the UNSIGNED posture: a repository with no key pair
// association (the ordinary unsigned mode, debug-logged) or a pair that
// cannot be opened (no master key / unopenable material — a
// configuration-state problem, WARN-logged) both leave the sweep to clear
// whatever stale signature files remain. Any other error is the caller's
// to surface.
func (h *Handler) repomdSignatures(ctx context.Context, repoKey string, repomd []byte) (asc, public string, signed bool, err error) {
	if h.signer == nil {
		return "", "", false, nil
	}
	asc, err = h.signer.DetachedArmor(ctx, repoKey, repomd)
	if err != nil {
		if unsignedPosture(err) {
			logSkipSigning(ctx, repoKey, err)
			return "", "", false, nil
		}
		return "", "", false, fmt.Errorf("sign repomd.xml of %s: %w", repoKey, err)
	}
	public, err = h.signer.PublicKey(ctx, repoKey)
	if err != nil {
		if unsignedPosture(err) {
			// Unreachable through the concrete seam (resolve precedes the
			// signature and the public read walks the same rows) — a
			// hand-rolled signer still gets the graceful degrade, never a
			// signed repomd without its public key file.
			logSkipSigning(ctx, repoKey, err)
			return "", "", false, nil
		}
		return "", "", false, fmt.Errorf("read public key of %s: %w", repoKey, err)
	}
	return asc, public, true, nil
}

// landRepomdSignatures lands (or sweeps) one root's signature pair right
// after the repomd.xml write: the pair's FIXED names overwrite in place —
// a rotation re-signs and a recompute of a signed repository keeps it
// signed (the retention sweep whitelists the names by construction: they
// are not digest-prefixed, so pruneGenerationsLocked never touches
// them). The unsigned posture writes nothing and removes whatever stale
// pair remains instead (rpm.md 4.3: an old signature must never verify a
// new repomd).
func (h *Handler) landRepomdSignatures(ctx context.Context, p *repo.Principal, repoKey, root string, repomd []byte) error {
	asc, public, signed, err := h.repomdSignatures(ctx, repoKey, repomd)
	if err != nil {
		return err
	}
	if !signed {
		return h.sweepStaleRepomdSignatures(ctx, p, repoKey, root)
	}
	pair := []struct {
		rel  string
		body string
	}{
		{joinRoot(root, fileRepomd+".asc"), asc},
		{joinRoot(root, fileRepomd+".key"), public},
	}
	for _, f := range pair {
		if _, err := h.svc.PutWithOptions(ctx, p, repoKey, f.rel,
			strings.NewReader(f.body), blobRefOf([]byte(f.body)), ctypeRepomdSignature,
			repo.PutOptions{SkipOverwriteCheck: true}); err != nil {
			return fmt.Errorf("write %s: %w", path.Base(f.rel), err)
		}
	}
	return nil
}

// sweepStaleRepomdSignatures removes the root's signature pair (the
// unsigned recompute's cleanup arm). The names are fixed, so the exact
// two deletes answer — a missing file is the ordinary case and passes.
func (h *Handler) sweepStaleRepomdSignatures(ctx context.Context, p *repo.Principal, repoKey, root string) error {
	for _, rel := range []string{joinRoot(root, fileRepomd+".asc"), joinRoot(root, fileRepomd+".key")} {
		if err := h.svc.Delete(ctx, p, repoKey, rel); err != nil && !errors.Is(err, repo.ErrNodeNotFound) {
			return fmt.Errorf("sweep stale signature %s: %w", rel, err)
		}
	}
	return nil
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
		slog.WarnContext(ctx, "rpm: repomd signing skipped — the associated key pair cannot be opened, serving an unsigned repomd",
			slog.String("repo", repoKey), slog.String("reason", err.Error()))
		return
	}
	slog.DebugContext(ctx, "rpm: repomd signing skipped — no key pair associated (unsigned mode)",
		slog.String("repo", repoKey))
}
