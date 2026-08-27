package keypair

import (
	"context"
	"errors"
	"fmt"
	"log/slog"
	"strings"
	"sync"
	"time"

	"github.com/lzwzzy/binflow/internal/metadata"
)

// The error taxonomy of the keypair plane. httpapi maps each to its status
// (spec section 1); the signing seam classifies its failures through the
// same vocabulary (spec section 3.6).
var (
	// ErrInvalidPairName: the pair name misses the charset rule
	// ([a-zA-Z][a-zA-Z0-9_-]{0,63}, spec section 3.2) — 400.
	ErrInvalidPairName = errors.New("invalid key pair name")
	// ErrPairExists: Generate refused because the name is taken (generation
	// never replaces; PUT is the replace face) — 409.
	ErrPairExists = errors.New("key pair already exists")
	// ErrNotFound: no pair carries the name — 404.
	ErrNotFound = errors.New("key pair not found")
	// ErrInUse: DELETE refused while repositories reference the pair — 400
	// (ADR-0038 decision 3 guard; the message names the referencing
	// repositories).
	ErrInUse = errors.New("key pair is in use by repositories")
	// ErrBadMaterial: the offered key material fails validation (unparsable
	// armor, public/private mismatch, a protected key without its
	// passphrase, or a passphrase that does not open the key) — 400.
	ErrBadMaterial = errors.New("key pair material rejected")
	// ErrNoMasterKey: writes are refused because the instance master key is
	// not configured (the enc:v1 sealing of both columns is mandatory;
	// spec section 3.1 posture) — 400 naming the environment variable.
	ErrNoMasterKey = errors.New("no master key configured for key pair sealing")
	// ErrNoKeypair: the repository carries no keypair association — the
	// signing seam's "unsigned mode" signal (spec section 3.6), never an
	// HTTP error by itself.
	ErrNoKeypair = errors.New("repository has no key pair association")
	// ErrUnavailable: the associated pair cannot be opened for signing
	// (missing master key, undecryptable seal, wrong stored passphrase) —
	// the signing seam's degraded signal; callers skip signing and clean
	// stale signature files (rpm.md section 4.3 posture).
	ErrUnavailable = errors.New("key pair is not usable for signing")
)

// PairTypeGPG is the one pairType of the M11 scope: armored OpenPGP
// material for repository-metadata signing (spec section 2.1 — the RSA PEM
// family serves Alpine-style index signing and is out of scope).
const PairTypeGPG = "GPG"

// RepoConfigField is the repository-config spelling of the association
// reference (spec section 3.3): local debian/rpm repositories carry
// {"keyPairName": "<pairName>"} in their config blob. The constant lives
// here — the single home — and the repo package's create/update validation
// consumes it; remote and virtual configs refuse the field by name
// (signing is a local write-path behavior).
const RepoConfigField = "keyPairName"

// maxPairNameLen / maxAliasLen bound the identifiers (spec section 3.2).
const (
	maxPairNameLen = 64
	maxAliasLen    = 128
)

// GenerateBitsDefault is the generation key size (spec section 2.3: the
// ADR-0038 interim RSA-4096 turned definitive — Artifactory publishes no
// keygen REST, so no default exists to flip to; apt/dnf legacy-client
// compatibility keeps the 4096 floor).
const GenerateBitsDefault = 4096

// SecretCipher is the enc:v1 seal/open seam (the *remote.Cipher the
// instance master key assembles; auth.SecretCipher's shape). A nil cipher
// means "no master key": every write refuses, existing rows fail the boot.
type SecretCipher interface {
	Encrypt(secret string) (string, error)
	Decrypt(stored string) (secret string, legacy bool, err error)
}

// RepoSource is the repository-configuration seam (consumer-side, cmd
// adapts metadata.Store's RepoStore which satisfies it structurally): the
// DELETE in-use guard, the GET repositories echo and the repo-keyed
// public-key lookup read repository rows through it.
type RepoSource interface {
	Get(ctx context.Context, repoKey string) (*metadata.Repo, error)
	List(ctx context.Context) ([]*metadata.Repo, error)
}

// Options assembles the Manager.
type Options struct {
	Store  metadata.GpgKeypairStore
	Cipher SecretCipher // nil = no master key (writes refuse; BootCheck decides the boot)
	Repos  RepoSource
	Log    *slog.Logger
	Now    func() time.Time
}

// Manager owns the keypair plane: import (create-or-replace), update,
// query, generate, verify, delete. Safe for concurrent use.
type Manager struct {
	store  metadata.GpgKeypairStore
	cipher SecretCipher
	repos  RepoSource
	log    *slog.Logger
	now    func() time.Time

	mu sync.Mutex // serializes writers (one row per pair; read-modify-write faces)
}

// NewManager validates the assembly (a store is the only hard requirement).
func NewManager(opts Options) (*Manager, error) {
	if opts.Store == nil {
		return nil, errors.New("keypair: Options.Store is required")
	}
	log := opts.Log
	if log == nil {
		log = slog.Default()
	}
	now := opts.Now
	if now == nil {
		now = func() time.Time { return time.Now().UTC() }
	}
	return &Manager{store: opts.Store, cipher: opts.Cipher, repos: opts.Repos, log: log, now: now}, nil
}

// ImportInput is the KeyPairInput wire body (spec section 1.1): the
// import (POST) and update (PUT) faces share it.
type ImportInput struct {
	PairName   string `json:"pairName"`
	PairType   string `json:"pairType"`
	Alias      string `json:"alias"`
	PrivateKey string `json:"privateKey"`
	PublicKey  string `json:"publicKey"`
	Passphrase string `json:"passphrase"`
	// vaultKey / vaultPublicKey are refused by name in the handler (spec
	// section 2.1 — the inert-field trap rule); they never reach here.
}

// GenerateInput is the BinFlow-native generation body (spec section 2.2).
type GenerateInput struct {
	PairName   string `json:"pairName"`
	Alias      string `json:"alias"`
	Passphrase string `json:"passphrase"`
	KeyBits    int    `json:"keyBits"`
	UIDName    string `json:"uidName"`
	UIDComment string `json:"uidComment"`
	UIDEmail   string `json:"uidEmail"`
}

// Summary is the KeyPairSummary echo (spec section 1.1): the four
// Artifactory fields verbatim plus the BinFlow-native provenance and
// reference echo (additive, spec divergence D-3). The private key and the
// passphrase are structurally absent — no field exists to carry them out.
type Summary struct {
	PairName     string   `json:"pairName"`
	PairType     string   `json:"pairType"`
	Alias        string   `json:"alias"`
	PublicKey    string   `json:"publicKey"`
	Algorithm    string   `json:"algorithm"`
	CreatedAt    string   `json:"createdAt"`
	UpdatedAt    string   `json:"updatedAt"`
	UpdatedBy    string   `json:"updatedBy"`
	Repositories []string `json:"repositories"`
}

// ---------------------------------------------------------------------------
// Write faces

// Import installs a key pair, replacing any pair under the same name (the
// POST create-or-replace contract, spec section 1.1). Both sealed columns
// are written even when the passphrase is empty (spec section 2.4: static
// sealing never leans on S2K strength).
func (m *Manager) Import(ctx context.Context, in ImportInput, actor string) (*Summary, error) {
	if err := m.validateInput(ctx, &in); err != nil {
		return nil, err
	}
	material, err := ParseKeyPair(in.PublicKey, in.PrivateKey)
	if err != nil {
		return nil, err
	}
	if err := unlockForImport(material, in.Passphrase); err != nil {
		return nil, err
	}
	return m.persist(ctx, in, material, actor, false)
}

// Update replaces an existing pair's material under the same name (the PUT
// rotation face, spec section 1.1): a missing name refuses ErrNotFound.
func (m *Manager) Update(ctx context.Context, in ImportInput, actor string) (*Summary, error) {
	if err := m.validateInput(ctx, &in); err != nil {
		return nil, err
	}
	material, err := ParseKeyPair(in.PublicKey, in.PrivateKey)
	if err != nil {
		return nil, err
	}
	if err := unlockForImport(material, in.Passphrase); err != nil {
		return nil, err
	}
	return m.persist(ctx, in, material, actor, true)
}

// Generate creates a fresh key pair server-side (the BinFlow-native keygen
// face, spec section 2.2): a taken name refuses ErrPairExists — generation
// never replaces.
func (m *Manager) Generate(ctx context.Context, in GenerateInput, actor string) (*Summary, error) {
	if err := validatePairName(in.PairName); err != nil {
		return nil, err
	}
	if len(in.Alias) > maxAliasLen {
		return nil, fmt.Errorf("%w: alias exceeds %d characters", ErrInvalidPairName, maxAliasLen)
	}
	bits := in.KeyBits
	if bits == 0 {
		bits = GenerateBitsDefault
	}
	if bits != 2048 && bits != 3072 && bits != 4096 {
		return nil, fmt.Errorf("%w: keyBits must be one of 2048, 3072, 4096 (got %d)", ErrBadMaterial, bits)
	}
	m.mu.Lock()
	defer m.mu.Unlock()
	existing, err := m.store.GetKeypair(ctx, in.PairName)
	if err != nil {
		return nil, fmt.Errorf("keypair %q: %w", in.PairName, err)
	}
	if existing != nil {
		return nil, fmt.Errorf("%w: %q", ErrPairExists, in.PairName)
	}
	material, err := GenerateKey(GenerateParams{
		KeyBits:    bits,
		Passphrase: in.Passphrase,
		UIDName:    in.UIDName,
		UIDComment: in.UIDComment,
		UIDEmail:   in.UIDEmail,
	})
	if err != nil {
		return nil, err
	}
	imp := ImportInput{
		PairName:   in.PairName,
		PairType:   PairTypeGPG,
		Alias:      in.Alias,
		PrivateKey: material.PrivateArmored,
		PublicKey:  material.PublicArmored,
		Passphrase: in.Passphrase,
	}
	rec, err := m.sealAndBuild(&imp, material, actor)
	if err != nil {
		return nil, err
	}
	if err := m.store.PutKeypair(ctx, rec); err != nil {
		return nil, fmt.Errorf("keypair %q: %w", in.PairName, err)
	}
	m.log.InfoContext(ctx, "keypair: generated",
		"pairName", in.PairName, "algorithm", rec.Algorithm, "actor", actor)
	return m.summary(ctx, rec), nil
}

// Delete removes a pair, refusing while any repository references it (the
// guard names the referencing repositories, ADR-0038 decision 3).
func (m *Manager) Delete(ctx context.Context, pairName string) error {
	m.mu.Lock()
	defer m.mu.Unlock()
	existing, err := m.store.GetKeypair(ctx, pairName)
	if err != nil {
		return fmt.Errorf("keypair %q: %w", pairName, err)
	}
	if existing == nil {
		return fmt.Errorf("%w: %q", ErrNotFound, pairName)
	}
	refs, err := m.ReferencingRepos(ctx, pairName)
	if err != nil {
		return err
	}
	if len(refs) > 0 {
		return fmt.Errorf("%w: %q is referenced by %s", ErrInUse, pairName, strings.Join(refs, ", "))
	}
	if err := m.store.DeleteKeypair(ctx, pairName); err != nil {
		return fmt.Errorf("keypair %q: %w", pairName, err)
	}
	return nil
}

// ---------------------------------------------------------------------------
// Read faces

// Get returns one pair's summary (ErrNotFound when absent).
func (m *Manager) Get(ctx context.Context, pairName string) (*Summary, error) {
	rec, err := m.store.GetKeypair(ctx, pairName)
	if err != nil {
		return nil, fmt.Errorf("keypair %q: %w", pairName, err)
	}
	if rec == nil {
		return nil, fmt.Errorf("%w: %q", ErrNotFound, pairName)
	}
	return m.summary(ctx, rec), nil
}

// List returns every pair's summary, ordered by name (the store's order).
func (m *Manager) List(ctx context.Context) ([]*Summary, error) {
	recs, err := m.store.ListKeypairs(ctx)
	if err != nil {
		return nil, fmt.Errorf("keypair list: %w", err)
	}
	out := make([]*Summary, 0, len(recs))
	for _, rec := range recs {
		out = append(out, m.summary(ctx, rec))
	}
	return out, nil
}

// PublicKeyOfRepo resolves a repository's associated pair and returns its
// armored public key (the repo-keyed public-key endpoint, spec section
// 1.1): an unknown repository or an unassociated one refuses ErrNotFound.
func (m *Manager) PublicKeyOfRepo(ctx context.Context, repoKey string) (string, error) {
	if m.repos == nil {
		return "", fmt.Errorf("keypair %q: %w", repoKey, ErrUnavailable)
	}
	repoRow, err := m.repos.Get(ctx, repoKey)
	if err != nil {
		return "", fmt.Errorf("keypair repo %q: %w", repoKey, err)
	}
	if repoRow == nil {
		return "", fmt.Errorf("%w: repository %q", ErrNotFound, repoKey)
	}
	name, _, err := RepoConfigReference(repoRow.Config)
	if err != nil {
		return "", fmt.Errorf("keypair repo %q: %w", repoKey, err)
	}
	if name == "" {
		return "", fmt.Errorf("%w: repository %q has no key pair association", ErrNotFound, repoKey)
	}
	rec, err := m.store.GetKeypair(ctx, name)
	if err != nil {
		return "", fmt.Errorf("keypair %q: %w", name, err)
	}
	if rec == nil {
		return "", fmt.Errorf("%w: %q", ErrNotFound, name)
	}
	return rec.PublicKey, nil
}

// ReferencingRepos lists the repositories whose config references the pair
// (the DELETE guard's evidence and the GET echo's repositories field).
func (m *Manager) ReferencingRepos(ctx context.Context, pairName string) ([]string, error) {
	if m.repos == nil {
		return nil, nil
	}
	rows, err := m.repos.List(ctx)
	if err != nil {
		return nil, fmt.Errorf("keypair reference scan: %w", err)
	}
	var refs []string
	for _, row := range rows {
		name, _, err := RepoConfigReference(row.Config)
		if err != nil {
			continue // a foreign-shape config is not a reference; the repo plane owns its validation
		}
		if name == pairName {
			refs = append(refs, row.RepoKey)
		}
	}
	return refs, nil
}

// ---------------------------------------------------------------------------
// Verification

// VerifyMaterial checks caller-supplied material (the Artifactory verify
// shape, spec section 1.1): armor parses, the public and private halves
// belong to the same primary key, and the passphrase opens the key when
// the material is protected.
func (m *Manager) VerifyMaterial(in ImportInput) error {
	material, err := ParseKeyPair(in.PublicKey, in.PrivateKey)
	if err != nil {
		return err
	}
	return unlockForImport(material, in.Passphrase)
}

// VerifyStored checks a stored pair end to end (the BinFlow extension,
// spec divergence D-5): unseal both columns, parse, open with the stored
// passphrase, and prove the signing path with a sign/verify self-test.
func (m *Manager) VerifyStored(ctx context.Context, pairName string) error {
	rec, err := m.store.GetKeypair(ctx, pairName)
	if err != nil {
		return fmt.Errorf("keypair %q: %w", pairName, err)
	}
	if rec == nil {
		return fmt.Errorf("%w: %q", ErrNotFound, pairName)
	}
	entity, err := m.unlockStored(ctx, rec, pairName)
	if err != nil {
		return err
	}
	return SelfTestSign(entity)
}

// ---------------------------------------------------------------------------
// Boot posture

// BootCheck enforces the static-secret posture (spec section 3.1): rows
// exist while no master key is configured → the instance cannot serve the
// plane, so the boot refuses (the auth_configs/replication family rule).
func (m *Manager) BootCheck(ctx context.Context) error {
	recs, err := m.store.ListKeypairs(ctx)
	if err != nil {
		return fmt.Errorf("keypair boot check: %w", err)
	}
	if len(recs) == 0 {
		return nil
	}
	if m.cipher == nil {
		return fmt.Errorf("gpg keypair rows exist (%d) but %w", len(recs), ErrNoMasterKey)
	}
	return nil
}

// ---------------------------------------------------------------------------
// internals

// validateInput runs the shared wire validation of the import/update
// faces: name charset, the GPG-only pairType, alias bound.
func (m *Manager) validateInput(_ context.Context, in *ImportInput) error {
	if err := validatePairName(in.PairName); err != nil {
		return err
	}
	if in.PairType != PairTypeGPG {
		return fmt.Errorf("%w: pairType %q is not supported (BinFlow serves GPG key pairs for debian/rpm metadata signing; the RSA PEM family is out of M11 scope)",
			ErrBadMaterial, in.PairType)
	}
	if len(in.Alias) > maxAliasLen {
		return fmt.Errorf("%w: alias exceeds %d characters", ErrInvalidPairName, maxAliasLen)
	}
	return nil
}

// persist is the import/update common tail: seal, build the row, write.
func (m *Manager) persist(ctx context.Context, in ImportInput, material *Material, actor string, mustExist bool) (*Summary, error) {
	m.mu.Lock()
	defer m.mu.Unlock()
	existing, err := m.store.GetKeypair(ctx, in.PairName)
	if err != nil {
		return nil, fmt.Errorf("keypair %q: %w", in.PairName, err)
	}
	if mustExist && existing == nil {
		return nil, fmt.Errorf("%w: %q", ErrNotFound, in.PairName)
	}
	rec, err := m.sealAndBuild(&in, material, actor)
	if err != nil {
		return nil, err
	}
	if existing != nil {
		rec.CreatedAt = existing.CreatedAt // the pair's birth survives material rotation
	}
	if err := m.store.PutKeypair(ctx, rec); err != nil {
		return nil, fmt.Errorf("keypair %q: %w", in.PairName, err)
	}
	return m.summary(ctx, rec), nil
}

// sealAndBuild assembles the record: both secret columns sealed under the
// master key (refusing without one), the public column verbatim.
func (m *Manager) sealAndBuild(in *ImportInput, material *Material, actor string) (*metadata.GpgKeypairRecord, error) {
	if m.cipher == nil {
		return nil, fmt.Errorf("keypair %q: %w", in.PairName, ErrNoMasterKey)
	}
	privateEnc, err := m.cipher.Encrypt(in.PrivateKey)
	if err != nil {
		return nil, fmt.Errorf("keypair %q: sealing private key: %w", in.PairName, err)
	}
	passEnc, err := m.cipher.Encrypt(in.Passphrase)
	if err != nil {
		return nil, fmt.Errorf("keypair %q: sealing passphrase: %w", in.PairName, err)
	}
	now := m.now().Format(time.RFC3339)
	return &metadata.GpgKeypairRecord{
		PairName:      in.PairName,
		PairType:      PairTypeGPG,
		Alias:         in.Alias,
		PublicKey:     in.PublicKey,
		PrivateKeyEnc: privateEnc,
		PassphraseEnc: passEnc,
		Algorithm:     material.Algorithm,
		CreatedAt:     now,
		UpdatedAt:     now,
		UpdatedBy:     actor,
	}, nil
}

// summary projects a stored row to the wire echo, attaching the
// referencing repositories (a best-effort scan: a nil RepoSource keeps the
// list empty, never failing the read).
func (m *Manager) summary(ctx context.Context, rec *metadata.GpgKeypairRecord) *Summary {
	s := &Summary{
		PairName:  rec.PairName,
		PairType:  rec.PairType,
		Alias:     rec.Alias,
		PublicKey: rec.PublicKey,
		Algorithm: rec.Algorithm,
		CreatedAt: rec.CreatedAt,
		UpdatedAt: rec.UpdatedAt,
		UpdatedBy: rec.UpdatedBy,
	}
	s.Repositories = []string{} // never null on the wire (the additive field's array form)
	if refs, err := m.ReferencingRepos(ctx, rec.PairName); err == nil && refs != nil {
		s.Repositories = refs
	}
	return s
}

// unlockStored is the signing-time first half (spec section 3.6): fetch →
// unseal → parse → open with the stored passphrase → hand the entity back
// for immediate use (the caller drops it right after; nothing caches).
func (m *Manager) unlockStored(_ context.Context, rec *metadata.GpgKeypairRecord, pairName string) (*Entity, error) {
	if m.cipher == nil {
		return nil, fmt.Errorf("keypair %q: %w", pairName, ErrUnavailable)
	}
	privateArmored, _, err := m.cipher.Decrypt(rec.PrivateKeyEnc)
	if err != nil {
		return nil, fmt.Errorf("keypair %q: %w", pairName, ErrUnavailable)
	}
	passphrase, _, err := m.cipher.Decrypt(rec.PassphraseEnc)
	if err != nil {
		return nil, fmt.Errorf("keypair %q: %w", pairName, ErrUnavailable)
	}
	entity, err := ParsePrivateKey(privateArmored)
	if err != nil {
		return nil, fmt.Errorf("keypair %q: %w", pairName, ErrUnavailable)
	}
	if err := UnlockKey(entity, passphrase); err != nil {
		return nil, fmt.Errorf("keypair %q: %w", pairName, ErrUnavailable)
	}
	return entity, nil
}

// validatePairName enforces the charset rule (spec section 3.2):
// [a-zA-Z][a-zA-Z0-9_-]{0,63}.
func validatePairName(name string) error {
	if name == "" {
		return fmt.Errorf("%w: pairName is required", ErrInvalidPairName)
	}
	if len(name) > maxPairNameLen {
		return fmt.Errorf("%w: %d characters exceeds the %d limit", ErrInvalidPairName, len(name), maxPairNameLen)
	}
	c := name[0]
	isLetter := (c >= 'a' && c <= 'z') || (c >= 'A' && c <= 'Z')
	if !isLetter {
		return fmt.Errorf("%w: %q must start with a letter", ErrInvalidPairName, name)
	}
	for i := 1; i < len(name); i++ {
		c := name[i]
		ok := (c >= 'a' && c <= 'z') || (c >= 'A' && c <= 'Z') || (c >= '0' && c <= '9') || c == '_' || c == '-'
		if !ok {
			return fmt.Errorf("%w: %q has illegal character %q at offset %d", ErrInvalidPairName, name, c, i)
		}
	}
	return nil
}
