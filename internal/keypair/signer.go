package keypair

import (
	"context"
	"errors"
	"fmt"

	"github.com/lzwzzy/binflow/internal/metadata"
)

// SigningService is the seam the debian (T-321: InRelease clearsign,
// Release.gpg detached) and rpm (T-322: repomd.xml.asc detached) metadata
// legs consume (spec section 3.6). The signing sequence per use is
// resolve → unseal → open → sign → discard: key material is held only for
// the call, never cached, never logged, never echoed.
//
// Consumers define their own narrow interface over these methods at their
// side (the Go-interface-at-the-consumer rule); this concrete service is
// the single implementation. The error taxonomy drives the consumers'
// unsigned posture: ErrNoKeypair and ErrUnavailable mean "skip signing and
// remove stale signature files" (rpm.md section 4.3's rule), other errors
// are operational failures the signing legs may surface.
type SigningService struct {
	store  metadata.GpgKeypairStore
	cipher SecretCipher
	repos  RepoSource
}

// NewSigningService assembles the signing seam; a nil cipher degrades
// every resolve to ErrUnavailable (the unsigned posture — the instance
// lost its master key, not its repositories).
func NewSigningService(store metadata.GpgKeypairStore, cipher SecretCipher, repos RepoSource) (*SigningService, error) {
	if store == nil {
		return nil, fmt.Errorf("keypair: signing service: store is required")
	}
	return &SigningService{store: store, cipher: cipher, repos: repos}, nil
}

// resolve is the signing-time first half: repository → association → row →
// unsealed, opened entity (spec section 3.6). An unknown repository maps to
// the same unsigned signal as an unassociated one (errors.Is on
// metadata.ErrRepoNotFound): the seam degrades to "no key", never to a
// content-plane failure.
func (s *SigningService) resolve(ctx context.Context, repoKey string) (*Entity, error) {
	if s.repos == nil {
		return nil, fmt.Errorf("repository %q: %w", repoKey, ErrNoKeypair)
	}
	repoRow, err := s.repos.Get(ctx, repoKey)
	if err != nil {
		if errors.Is(err, metadata.ErrRepoNotFound) {
			return nil, fmt.Errorf("repository %q: %w", repoKey, ErrNoKeypair)
		}
		return nil, fmt.Errorf("keypair signing: repository %q: %w", repoKey, err)
	}
	if repoRow == nil {
		return nil, fmt.Errorf("repository %q: %w", repoKey, ErrNoKeypair)
	}
	name, _, err := RepoConfigReference(repoRow.Config)
	if err != nil {
		return nil, fmt.Errorf("keypair signing: repository %q: %w", repoKey, err)
	}
	if name == "" {
		return nil, fmt.Errorf("repository %q: %w", repoKey, ErrNoKeypair)
	}
	rec, err := s.store.GetKeypair(ctx, name)
	if err != nil {
		return nil, fmt.Errorf("keypair signing: %q: %w", name, err)
	}
	if rec == nil {
		// Unreachable through the managed paths (repo validation refuses a
		// dangling reference; the delete guard refuses deleting a referenced
		// pair) — a hand-edited store still answers the unsigned signal, not
		// a crash.
		return nil, fmt.Errorf("repository %q: %w", repoKey, ErrNoKeypair)
	}
	if s.cipher == nil {
		return nil, fmt.Errorf("keypair %q: %w", name, ErrUnavailable)
	}
	privateArmored, _, err := s.cipher.Decrypt(rec.PrivateKeyEnc)
	if err != nil {
		return nil, fmt.Errorf("keypair %q: %w", name, ErrUnavailable)
	}
	passphrase, _, err := s.cipher.Decrypt(rec.PassphraseEnc)
	if err != nil {
		return nil, fmt.Errorf("keypair %q: %w", name, ErrUnavailable)
	}
	entity, err := ParsePrivateKey(privateArmored)
	if err != nil {
		return nil, fmt.Errorf("keypair %q: %w", name, ErrUnavailable)
	}
	if err := UnlockKey(entity, passphrase); err != nil {
		return nil, fmt.Errorf("keypair %q: %w", name, ErrUnavailable)
	}
	return entity, nil
}

// DetachedArmor signs data with the repository's associated pair and
// returns the armored detached signature (the Release.gpg / repomd.xml.asc
// form, RFC 4880).
func (s *SigningService) DetachedArmor(ctx context.Context, repoKey string, data []byte) (string, error) {
	entity, err := s.resolve(ctx, repoKey)
	if err != nil {
		return "", err
	}
	return DetachedArmor(entity, data)
}

// Clearsign signs data into the cleartext-signature form (the InRelease
// form, RFC 4880 cleartext framework).
func (s *SigningService) Clearsign(ctx context.Context, repoKey string, data []byte) ([]byte, error) {
	entity, err := s.resolve(ctx, repoKey)
	if err != nil {
		return nil, err
	}
	return ClearsignBody(entity, data)
}

// PublicKey returns the repository's associated pair's armored public key
// (the repomd.xml.key / repo-keyed REST face) — the public half needs no
// unsealing, so this works even when the master key is gone.
func (s *SigningService) PublicKey(ctx context.Context, repoKey string) (string, error) {
	if s.repos == nil {
		return "", fmt.Errorf("repository %q: %w", repoKey, ErrNoKeypair)
	}
	repoRow, err := s.repos.Get(ctx, repoKey)
	if err != nil {
		if errors.Is(err, metadata.ErrRepoNotFound) {
			return "", fmt.Errorf("repository %q: %w", repoKey, ErrNoKeypair)
		}
		return "", fmt.Errorf("keypair public key: repository %q: %w", repoKey, err)
	}
	if repoRow == nil {
		return "", fmt.Errorf("repository %q: %w", repoKey, ErrNoKeypair)
	}
	name, _, err := RepoConfigReference(repoRow.Config)
	if err != nil {
		return "", fmt.Errorf("keypair public key: repository %q: %w", repoKey, err)
	}
	if name == "" {
		return "", fmt.Errorf("repository %q: %w", repoKey, ErrNoKeypair)
	}
	rec, err := s.store.GetKeypair(ctx, name)
	if err != nil {
		return "", fmt.Errorf("keypair public key: %q: %w", name, err)
	}
	if rec == nil {
		return "", fmt.Errorf("repository %q: %w", repoKey, ErrNoKeypair)
	}
	return rec.PublicKey, nil
}
