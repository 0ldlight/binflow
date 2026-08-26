// The authentication ConfigManager (M11 T-305, ADR-0035 / FR-92): the
// runtime owner of the three protocol sections stored in auth_configs
// (migration 015). It is the license Manager's three-element pattern
// transplanted onto the auth plane (ADR-0035 decision 3):
//
//  1. runtime state  — one immutable snapshot behind an atomic pointer;
//     every authentication arm reads it per request, lock-free (the next
//     request after a swap already walks the new configuration — the ≤1s
//     "change takes effect" bar is structural: no restart, no ticker);
//  2. write path     — verify-then-replace: strict schema + section
//     validation + (OIDC) live discovery succeed BEFORE the row lands and
//     the snapshot swaps; any failure leaves the current configuration
//     exactly in force (D7's contract);
//  3. persistence    — one row per section, replayed at boot by Load
//     (providers rebuilt, secrets unsealed).
//
// Dual-source priority (K31, decision 4): a stored DB row is authoritative
// and a still-present binflow.yaml section only draws a WARN (naming the
// section and the REST path — an upgraded instance is never blocked by a
// leftover section); a missing row plus a configured file section seeds the
// DB once (secrets sealed from the env values, INFO logged) and the section
// is database-managed from then on. Non-IdP auth.* keys (argon2, token TTL
// family, step-up, hash concurrency) stay file/env-owned — the runtime
// variable surface converges on the three protocol sections only.

package auth

import (
	"context"
	"crypto/tls"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"log/slog"
	"net"
	"net/http"
	"net/url"
	"reflect"
	"strings"
	"sync"
	"sync/atomic"
	"time"

	oidc "github.com/coreos/go-oidc/v3/oidc"
	"github.com/go-ldap/ldap/v3"

	"github.com/lzwzzy/binflow/internal/metadata"
)

// SecretCipher is the consumer-side seam of the enc:v1 at-rest chain
// (ADR-0012 decision 4's machine reused per ADR-0035 decision 5: AES-256-GCM
// under BINFLOW_REMOTE_CREDENTIALS_KEY). *remote.Cipher satisfies it
// structurally; cmd wires the same instance the replication engine uses.
// A nil cipher means "no master key": writes carrying a NEW secret refuse,
// unsealing a stored secret fails the boot.
type SecretCipher interface {
	Encrypt(secret string) (string, error)
	Decrypt(stored string) (secret string, legacy bool, err error)
}

// StoredAuthConfig is one section row as the manager sees it.
type StoredAuthConfig struct {
	Section   string
	Doc       string
	UpdatedAt string
	UpdatedBy string
}

// ConfigStore is the persistence seam (consumer-side; the metadata
// sub-store adapter satisfies it — see NewConfigStoreAdapter).
type ConfigStore interface {
	GetAuthConfig(ctx context.Context, section string) (*StoredAuthConfig, error) // (nil, nil) when absent
	PutAuthConfig(ctx context.Context, rec *StoredAuthConfig) error
	ListAuthConfigs(ctx context.Context) ([]*StoredAuthConfig, error)
}

// GuardedDialer screens and dials one outbound connection — the M3 SSRF
// Guard machine's dial contract (ADR-0035 decision 6: scheme whitelist per
// protocol happens here, IP screening + DNS-rebinding pinning + Control
// re-check live in the Guard). remote.Guard.Dialer's return satisfies it.
type GuardedDialer func(ctx context.Context, network, addr string) (net.Conn, error)

// URLScreener runs the pre-connect scheme/host screen of the same machine
// (remote.Guard.CheckURL). nil legs skip it (the guarded dial still runs).
type URLScreener func(ctx context.Context, rawURL string) error

// LDAPProbeConn is the wire subset the LDAP test-connection probe needs
// (*ldap.Conn satisfies it; tests inject fakes at the same level as the
// provider's LDAPConn seam).
type LDAPProbeConn interface {
	Bind(username, password string) error
	Search(searchRequest *ldap.SearchRequest) (*ldap.SearchResult, error)
	StartTLS(config *tls.Config) error
	Close() error
	SetTimeout(timeout time.Duration)
}

// LDAPProbeDialer opens one probe connection: rawURL is the section's
// ldapUrl, tlsConf the derived TLS posture (nil for plaintext), and
// implicitTLS marks an ldaps:// URL. The production default rides the
// guarded dialer + ldap.NewConn; tests return fakes.
type LDAPProbeDialer func(ctx context.Context, rawURL string, tlsConf *tls.Config, implicitTLS bool) (LDAPProbeConn, error)

// ErrNoMasterKey names the missing-instance-key refusal of the write path.
var ErrNoMasterKey = errors.New("auth config: no master key: BINFLOW_REMOTE_CREDENTIALS_KEY is not set (required to store secrets)")

// ErrTestCredsIncomplete is the anchored §1.6 rejection of a user-bind LDAP
// test that carries only one of testUsername/testPassword.
var ErrTestCredsIncomplete = errors.New("Please enter test username and password to test the LDAP settings") //nolint:staticcheck // ST1005: the anchored §1.6 message text, spelled verbatim

// ErrSecretUnreadable marks a stored secret the cipher cannot open (wrong
// or missing master key) — a boot-time fail-fast per ADR-0035 decision 5.
var ErrSecretUnreadable = errors.New("auth config: stored secret cannot be unsealed (wrong or missing BINFLOW_REMOTE_CREDENTIALS_KEY)")

// probeTimeout bounds every outbound test connection (ADR-0035 decision 6:
// "short timeout").
const probeTimeout = 5 * time.Second

// probeMaxBody caps buffered probe responses (the ADR-0012 erratum-2 ③
// figure — 64 MiB).
const probeMaxBody = 64 << 20

// defaultPoolGrace is the drain window granted to a REPLACED LDAP pool:
// requests that captured the previous snapshot finish their binds inside
// their own request timeout, then the pool closes (auth-integration §5.2
// boundary: config changes do not cancel in-flight work).
const defaultPoolGrace = 30 * time.Second

// TestReport is the diagnostic verdict of one test-connection run. Message
// is operator-actionable and never carries credentials or URL userinfo
// (NFR-S60 / ADR-0035 decision 6).
type TestReport struct {
	OK       bool   `json:"ok"`
	Phase    string `json:"phase,omitempty"`
	Category string `json:"category,omitempty"`
	Message  string `json:"message,omitempty"`
}

// ConfigOptions configures the ConfigManager.
type ConfigOptions struct {
	Store  ConfigStore  // required
	Cipher SecretCipher // nil = no master key
	// LDAPResolver/OIDCResolver feed provider rebuilds (cmd wires the same
	// store-backed seams the static M6 wiring used).
	LDAPResolver LDAPUserResolver
	OIDCResolver OIDCUserResolver
	// LDAPDialer is the provider-construction dial seam (nil = the real
	// dialer; tests inject mocks — the hot A→B directory switch rides it).
	LDAPDialer LDAPDialer
	// HTTPClient is the client OIDC discovery uses on rebuild and the
	// probes use for http legs (nil = a default client with probeTimeout;
	// production wires the Guard-backed client so the write path opens no
	// new SSRF face either).
	HTTPClient *http.Client
	// Screen is the pre-connect URL screen of the probes (http legs).
	Screen URLScreener
	// Dial is the guarded dialer of the probes (all legs).
	Dial GuardedDialer
	// LDAPProbeDial opens the LDAP probe's connection (nil = the guarded
	// default; tests inject fakes).
	LDAPProbeDial LDAPProbeDialer
	Log           *slog.Logger
	Now           func() time.Time
	// PoolGrace is the replaced-LDAP-pool drain window. 0 = the 30s
	// default; NEGATIVE = close immediately (tests).
	PoolGrace time.Duration
}

// ConfigManager owns the auth configuration plane. Safe for concurrent use.
type ConfigManager struct {
	store  ConfigStore
	cipher SecretCipher
	log    *slog.Logger
	now    func() time.Time
	grace  time.Duration

	ldapResolver LDAPUserResolver
	oidcResolver OIDCUserResolver
	ldapDialer   LDAPDialer
	httpClient   *http.Client
	screen       URLScreener
	dial         GuardedDialer
	ldapProbe    LDAPProbeDialer

	mu   sync.Mutex // serializes Put writers (one row per section)
	snap atomic.Pointer[authSnapshot]
}

// authSnapshot is the immutable runtime state: effective sections (secrets
// unsealed, in memory only) plus the rebuilt providers.
type authSnapshot struct {
	ldap     *LDAPSection
	oidc     *OIDCSection
	saml     *SAMLSection
	ldapProv *hotLDAPProvider // nil when the section is absent/disabled
	oidcProv *OIDCProvider    // nil when the section is absent/disabled
}

// NewAuthConfigManager builds the manager on the empty snapshot. Call Load
// before serving (startup replay + dual-source resolution).
func NewAuthConfigManager(opts ConfigOptions) (*ConfigManager, error) {
	if opts.Store == nil {
		return nil, errors.New("auth config: Options.Store is required")
	}
	log := opts.Log
	if log == nil {
		log = slog.Default()
	}
	now := opts.Now
	if now == nil {
		now = func() time.Time { return time.Now().UTC() }
	}
	grace := opts.PoolGrace
	if grace == 0 {
		grace = defaultPoolGrace
	}
	m := &ConfigManager{
		store:        opts.Store,
		cipher:       opts.Cipher,
		log:          log,
		now:          now,
		grace:        grace,
		ldapResolver: opts.LDAPResolver,
		oidcResolver: opts.OIDCResolver,
		ldapDialer:   opts.LDAPDialer,
		httpClient:   opts.HTTPClient,
		screen:       opts.Screen,
		dial:         opts.Dial,
		ldapProbe:    opts.LDAPProbeDial,
	}
	m.snap.Store(&authSnapshot{})
	return m, nil
}

// ---------------------------------------------------------------------------
// Snapshot facets (the hot seams the auth Service consumes per request)
// ---------------------------------------------------------------------------

// CurrentOIDC returns the live OIDC provider, or nil when the section is
// absent or disabled (the arm's inert posture — byte-identical to the
// unwired pre-M6 service).
func (m *ConfigManager) CurrentOIDC() *OIDCProvider {
	s := m.snap.Load()
	if s == nil {
		return nil
	}
	return s.oidcProv
}

// CurrentLDAP returns the live LDAP provider wrapper, or nil when the
// section is absent or disabled.
func (m *ConfigManager) CurrentLDAP() IdentityProvider {
	s := m.snap.Load()
	if s == nil || s.ldapProv == nil {
		return nil
	}
	return s.ldapProv
}

// OIDCAutoCreate reports the section's auto_create_users verdict (default
// true — the M6 wired posture).
func (m *ConfigManager) OIDCAutoCreate() bool {
	s := m.snap.Load()
	return s == nil || s.oidc == nil || s.oidc.AutoCreateUsers
}

// LDAPAutoCreate reports the section's autoCreateUser verdict (§1.1 #6,
// default true).
func (m *ConfigManager) LDAPAutoCreate() bool {
	s := m.snap.Load()
	return s == nil || s.ldap == nil || s.ldap.AutoCreateUser
}

// SectionConfigured reports whether a section currently holds an ENABLED
// configuration (the live capability answer).
func (m *ConfigManager) SectionConfigured(section string) bool {
	s := m.snap.Load()
	if s == nil {
		return false
	}
	switch section {
	case SectionLDAP:
		return s.ldap != nil && s.ldap.Enabled
	case SectionOIDC:
		return s.oidc != nil && s.oidc.Enabled
	case SectionSAML:
		return s.saml != nil && s.saml.EnableIntegration
	default:
		return false
	}
}

// Close drains the current LDAP pool (shutdown path).
func (m *ConfigManager) Close() {
	if s := m.snap.Load(); s != nil && s.ldapProv != nil {
		s.ldapProv.Close()
	}
}

// hotLDAPProvider delegates to the snapshot's rebuilt provider and carries
// the section's policy facets. The wrapper exists so the Service consumes
// ONE identity per snapshot — no mix of old provider and new section.
type hotLDAPProvider struct {
	prov *LDAPProvider
	sec  *LDAPSection
}

func (h *hotLDAPProvider) ProviderName() Provider { return ProviderLDAP }

func (h *hotLDAPProvider) Authenticate(ctx context.Context, token string) (*Claims, error) {
	return h.prov.Authenticate(ctx, token)
}

func (h *hotLDAPProvider) Resolve(ctx context.Context, provider Provider, providerID string) (*ProviderUser, error) {
	return h.prov.Resolve(ctx, provider, providerID)
}

// Bind is the LDAP login arm's entry (the session plane's structural
// assertion target).
func (h *hotLDAPProvider) Bind(ctx context.Context, username, password string) (*Claims, error) {
	return h.prov.Bind(ctx, username, password)
}

// AutoCreateUsers is the §1.1 autoCreateUser gate (default true when the
// section is somehow absent).
func (h *hotLDAPProvider) AutoCreateUsers() bool { return h.sec == nil || h.sec.AutoCreateUser }

// Close drains the wrapped pool.
func (h *hotLDAPProvider) Close() { h.prov.Close() }

// ---------------------------------------------------------------------------
// Boot: dual-source resolution + startup replay
// ---------------------------------------------------------------------------

// Load resolves the dual-source rules (K31) and replays the stored rows
// into the opening snapshot. seeds carries the binflow.yaml-derived docs
// (secrets already resolved from env, plaintext) keyed by section; a seed
// counts as "configured" when it differs from the pure defaults (enabled or
// any non-default key).
//
// Fail-fast conditions (returned as errors — cmd refuses the boot): a
// stored secret that cannot be unsealed (missing/wrong master key), or an
// enabled OIDC section whose issuer does not answer discovery (the M6 boot
// posture for seeded instances, unchanged).
func (m *ConfigManager) Load(ctx context.Context, seeds map[string][]byte) error {
	m.mu.Lock()
	defer m.mu.Unlock()

	stored := map[string]*StoredAuthConfig{}
	for _, section := range authSections {
		rec, err := m.store.GetAuthConfig(ctx, section)
		if err != nil {
			return fmt.Errorf("auth config: reading section %s: %w", section, err)
		}
		stored[section] = rec
	}

	for _, section := range authSections {
		seed, hasSeed := seeds[section]
		seedActive := hasSeed && seedConfigured(section, seed)
		switch {
		case stored[section] != nil:
			if seedActive {
				// K31 rule ①: DB wins; the leftover file section keeps its
				// runtime meaning only as a WARN (upgrade-friendliness — an
				// instance is never blocked by a section it migrated past).
				m.log.WarnContext(ctx,
					"auth config: binflow.yaml section is overridden by the database configuration; edit it via the REST API",
					"section", section, "rest_path", restPathOf(section))
			}
			m.log.InfoContext(ctx, "auth config source: database", "section", section)
		case seedActive:
			// K31 rule ②: first-boot seed — seal the secrets into the row,
			// then the section is database-managed for good.
			merged, err := m.mergeSecrets(ctx, section, seed)
			if err != nil {
				return fmt.Errorf("auth config: seeding section %s: %w", section, err)
			}
			if err := ValidateAuthSection(section, merged); err != nil {
				return fmt.Errorf("auth config: seeding section %s: %w", section, err)
			}
			if err := m.store.PutAuthConfig(ctx, &StoredAuthConfig{
				Section:   section,
				Doc:       string(merged),
				UpdatedAt: m.now().Format(time.RFC3339),
				UpdatedBy: "system-seed",
			}); err != nil {
				return fmt.Errorf("auth config: seeding section %s: %w", section, err)
			}
			m.log.InfoContext(ctx,
				"auth config: seeded section from binflow.yaml; the section is database-managed from now on",
				"section", section, "rest_path", restPathOf(section))
			m.log.InfoContext(ctx, "auth config source: file seed", "section", section)
		default:
			m.log.InfoContext(ctx, "auth config source: unset", "section", section)
		}
	}

	snap, err := m.rebuildSnapshot(ctx)
	if err != nil {
		return err
	}
	m.swapSnapshot(snap)
	return nil
}

// authSections is the closed set, in its stable iteration order.
var authSections = []string{SectionLDAP, SectionOIDC, SectionSAML}

// rebuildSnapshot reads every stored row, unseals secrets and rebuilds the
// providers. A section without a row stays nil (the arm's inert posture).
func (m *ConfigManager) rebuildSnapshot(ctx context.Context) (*authSnapshot, error) {
	snap := &authSnapshot{}
	rows, err := m.store.ListAuthConfigs(ctx)
	if err != nil {
		return nil, fmt.Errorf("auth config: listing sections: %w", err)
	}
	for _, rec := range rows {
		switch rec.Section {
		case SectionLDAP:
			sec, err := m.sectionLDAPUnsealed(rec.Doc)
			if err != nil {
				return nil, err
			}
			snap.ldap = sec
		case SectionOIDC:
			sec, err := m.sectionOIDCUnsealed(rec.Doc)
			if err != nil {
				return nil, err
			}
			snap.oidc = sec
		case SectionSAML:
			sec, err := decodeSAMLSection([]byte(rec.Doc))
			if err != nil {
				return nil, fmt.Errorf("auth config: decoding saml section: %w", err)
			}
			snap.saml = sec
		default:
			// Unknown section rows are ignored (forward compatibility); the
			// closed set governs the write plane only.
		}
	}
	if snap.ldap != nil && snap.ldap.Enabled {
		prov, err := m.buildLDAPProvider(snap.ldap)
		if err != nil {
			return nil, err
		}
		snap.ldapProv = prov
	}
	if snap.oidc != nil && snap.oidc.Enabled {
		prov, err := m.buildOIDCProvider(ctx, snap.oidc)
		if err != nil {
			return nil, fmt.Errorf("auth config: oidc discovery (section %s): %w", SectionOIDC, err)
		}
		snap.oidcProv = prov
	}
	return snap, nil
}

// sectionLDAPUnsealed decodes and unseals the stored LDAP doc.
func (m *ConfigManager) sectionLDAPUnsealed(doc string) (*LDAPSection, error) {
	sec, err := decodeLDAPSection([]byte(doc))
	if err != nil {
		return nil, fmt.Errorf("auth config: decoding ldap section: %w", err)
	}
	if sec.Search.ManagerPassword != "" {
		plain, err := m.unseal(sec.Search.ManagerPassword)
		if err != nil {
			return nil, fmt.Errorf("auth config: ldap section: %w", err)
		}
		sec.Search.ManagerPassword = plain
	}
	return sec, nil
}

// sectionOIDCUnsealed decodes and unseals the stored OIDC doc.
func (m *ConfigManager) sectionOIDCUnsealed(doc string) (*OIDCSection, error) {
	sec, err := decodeOIDCSection([]byte(doc))
	if err != nil {
		return nil, fmt.Errorf("auth config: decoding oidc section: %w", err)
	}
	if sec.ClientSecret != "" {
		plain, err := m.unseal(sec.ClientSecret)
		if err != nil {
			return nil, fmt.Errorf("auth config: oidc section: %w", err)
		}
		sec.ClientSecret = plain
	}
	return sec, nil
}

// unseal opens one stored secret. Legacy plaintext (no enc:v1: prefix)
// passes through — the remote-credentials chain's upgrade posture.
func (m *ConfigManager) unseal(stored string) (string, error) {
	if m.cipher == nil {
		return "", ErrSecretUnreadable
	}
	plain, _, err := m.cipher.Decrypt(stored)
	if err != nil {
		return "", fmt.Errorf("%w: %w", ErrSecretUnreadable, err)
	}
	return plain, nil
}

// ---------------------------------------------------------------------------
// Provider rebuilds
// ---------------------------------------------------------------------------

// buildLDAPProvider maps the section onto the LDAPConfig the runtime arm
// consumes. Construction is offline (the pool dials lazily) — see ldap.go.
func (m *ConfigManager) buildLDAPProvider(sec *LDAPSection) (*hotLDAPProvider, error) {
	cfg, err := ldapConfigFromSection(sec)
	if err != nil {
		return nil, err
	}
	prov, err := NewLDAPProvider(cfg, m.ldapResolver, m.ldapDialer)
	if err != nil {
		return nil, fmt.Errorf("auth config: building ldap provider: %w", err)
	}
	return &hotLDAPProvider{prov: prov, sec: sec}, nil
}

// ldapConfigFromSection projects the wire section onto the runtime config:
// the ldapUrl path IS the base DN (§1.1 #3), searchFilter's {0} becomes the
// runtime's %s, a relative searchBase is joined onto the URL base, and the
// manager DN/password pair drives the service bind (empty DN = anonymous
// read-only bind, §1.2 #4).
func ldapConfigFromSection(sec *LDAPSection) (*LDAPConfig, error) {
	u, err := url.Parse(sec.LDAPURL)
	if err != nil || u.Host == "" {
		return nil, fmt.Errorf("auth config: ldap ldapUrl %q: must be an ldap(s) URL", sec.LDAPURL)
	}
	baseDN := strings.Trim(u.Path, "/")
	searchURL := u.Scheme + "://" + u.Host
	effectiveBase := baseDN
	if sb := strings.TrimSpace(sec.Search.SearchBase); sb != "" && baseDN != "" {
		effectiveBase = sb + "," + baseDN
	} else if sb != "" {
		effectiveBase = sb
	}
	return &LDAPConfig{
		Enabled:       true,
		URL:           searchURL,
		BaseDN:        effectiveBase,
		BindDN:        sec.Search.ManagerDN,
		BindPassword:  sec.Search.ManagerPassword,
		UserFilter:    strings.ReplaceAll(sec.Search.SearchFilter, "{0}", "%s"),
		UserIDAttr:    "uid",
		GroupFilter:   strings.ReplaceAll(sec.GroupFilter, "{0}", "%s"),
		GroupBaseDN:   sec.GroupBaseDN,
		GroupNameAttr: sec.GroupNameAttribute,
		AdminGroup:    sec.AdminGroup,
		ReadOnlyGroup: sec.ReadOnlyGroup,
		PoolSize:      sec.PoolSize,
		StartTLS:      sec.StartTLS,
		SkipTLSVerify: sec.SkipTLSVerify,
	}, nil
}

// buildOIDCProvider performs live discovery against the section's issuer —
// the write path's network verification (ADR-0035 decision 3 ②: a failed
// discovery is a failed write). The discovery rides the injected client
// when one is configured (the Guard-backed transport).
func (m *ConfigManager) buildOIDCProvider(ctx context.Context, sec *OIDCSection) (*OIDCProvider, error) {
	if m.httpClient != nil {
		ctx = oidc.ClientContext(ctx, m.httpClient)
	}
	return NewOIDCProvider(ctx, &OIDCConfig{
		IssuerURL:     sec.IssuerURL,
		ClientID:      sec.ClientID,
		ClientSecret:  sec.ClientSecret,
		RedirectURL:   sec.RedirectURL,
		Scopes:        sec.Scopes,
		UserClaim:     sec.UserClaim,
		GroupClaim:    sec.GroupClaim,
		AdminGroup:    sec.AdminGroup,
		ReadOnlyGroup: sec.ReadOnlyGroup,
	}, m.oidcResolver)
}

// ---------------------------------------------------------------------------
// Read path
// ---------------------------------------------------------------------------

// GetAuthSection renders the masked echo of one stored section. A missing
// row answers (nil, nil) — the HTTP layer serves DefaultAuthSectionDoc.
func (m *ConfigManager) GetAuthSection(ctx context.Context, section string) (json.RawMessage, error) {
	if err := knownSection(section); err != nil {
		return nil, err
	}
	rec, err := m.store.GetAuthConfig(ctx, section)
	if err != nil {
		return nil, fmt.Errorf("auth config: reading section %s: %w", section, err)
	}
	if rec == nil {
		return nil, nil
	}
	return maskedEcho(section, []byte(rec.Doc))
}

// maskedEcho decodes a stored doc and renders its masked canonical form.
func maskedEcho(section string, doc []byte) (json.RawMessage, error) {
	v, err := DecodeAuthSection(section, doc)
	if err != nil {
		return nil, err
	}
	switch s := v.(type) {
	case *LDAPSection:
		return marshalSectionDoc(s.masked())
	case *OIDCSection:
		return marshalSectionDoc(s.masked())
	case *SAMLSection:
		return marshalSectionDoc(s.masked())
	default:
		return nil, fmt.Errorf("%w: %q", ErrSectionUnknown, section)
	}
}

// ---------------------------------------------------------------------------
// Write path: verify-then-replace
// ---------------------------------------------------------------------------

// PutAuthSection validates, verifies and installs one section. On success
// it returns the masked canonical echo plus the names of the keys that
// changed against the previous stored doc (audit's summary — values never
// travel). On failure the current configuration stays fully in force.
func (m *ConfigManager) PutAuthSection(ctx context.Context, section string, body []byte, actor string) (echo json.RawMessage, changed []string, err error) {
	if err := knownSection(section); err != nil {
		return nil, nil, err
	}
	m.mu.Lock()
	defer m.mu.Unlock()

	prev, err := m.store.GetAuthConfig(ctx, section)
	if err != nil {
		return nil, nil, fmt.Errorf("auth config: reading section %s: %w", section, err)
	}

	// Secret merge (write-only mode): absent or sentinel keeps the stored
	// value, "" clears, anything else is sealed fresh.
	merged, err := m.mergeSecrets(ctx, section, body)
	if err != nil {
		return nil, nil, err
	}
	if err := ValidateAuthSection(section, merged); err != nil {
		return nil, nil, err
	}

	// Rebuild-and-verify the candidate BEFORE anything lands (D7: a
	// refusal leaves the live configuration untouched). Only an ENABLED
	// section rebuilds a provider — a disabled section's write is pure
	// persistence (no discovery dial for an inert section).
	next := m.snap.Load().clone()
	switch section {
	case SectionLDAP:
		sec, err := m.sectionLDAPUnsealed(string(merged))
		if err != nil {
			return nil, nil, err
		}
		next.ldap = sec
		next.ldapProv = nil
		if sec.Enabled {
			prov, err := m.buildLDAPProvider(sec)
			if err != nil {
				return nil, nil, err
			}
			next.ldapProv = prov
		}
	case SectionOIDC:
		sec, err := m.sectionOIDCUnsealed(string(merged))
		if err != nil {
			return nil, nil, err
		}
		next.oidc = sec
		next.oidcProv = nil
		if sec.Enabled {
			prov, err := m.buildOIDCProvider(ctx, sec)
			if err != nil {
				return nil, nil, fmt.Errorf("auth config: oidc discovery failed for the candidate configuration: %w", err)
			}
			next.oidcProv = prov
		}
	case SectionSAML:
		sec, err := decodeSAMLSection(merged)
		if err != nil {
			return nil, nil, err
		}
		next.saml = sec
	}

	if err := m.store.PutAuthConfig(ctx, &StoredAuthConfig{
		Section:   section,
		Doc:       string(merged),
		UpdatedAt: m.now().Format(time.RFC3339),
		UpdatedBy: actor,
	}); err != nil {
		return nil, nil, fmt.Errorf("auth config: persisting section %s: %w", section, err)
	}

	m.swapSnapshot(next)
	m.log.InfoContext(ctx, "auth config: section updated (effective on the next authentication)",
		"section", section, "actor", actor)

	echo, err = maskedEcho(section, merged)
	if err != nil {
		return nil, nil, err
	}
	return echo, changedKeys(prev, merged), nil
}

// mergeSecrets folds the submitted body onto the stored sealed secret and
// returns the canonical stored form of the candidate.
func (m *ConfigManager) mergeSecrets(ctx context.Context, section string, body []byte) ([]byte, error) {
	var parentKey, field string
	switch section {
	case SectionLDAP:
		parentKey, field = "search", "managerPassword"
	case SectionOIDC:
		parentKey, field = "", "client_secret"
	case SectionSAML:
		// No secret on this plane (the IdP certificate is public material).
		return canonicalizeSection(section, body), nil
	default:
		return nil, fmt.Errorf("%w: %q", ErrSectionUnknown, section)
	}

	raw, err := strictMap(body, section)
	if err != nil {
		return nil, err
	}
	holder := raw
	if parentKey != "" {
		sub, ok := raw[parentKey]
		if !ok {
			return canonicalizeSection(section, body), nil // no search object: nothing to merge
		}
		if holder, err = strictMap(sub, section+"."+parentKey); err != nil {
			return nil, err
		}
	}

	// The stored secret in its sealed form.
	stored := ""
	if rec, err := m.store.GetAuthConfig(ctx, section); err != nil {
		return nil, err
	} else if rec != nil {
		if v, err := DecodeAuthSection(section, []byte(rec.Doc)); err == nil {
			switch s := v.(type) {
			case *LDAPSection:
				stored = s.Search.ManagerPassword
			case *OIDCSection:
				stored = s.ClientSecret
			}
		}
	}

	effective := stored
	if submitted, present := holder[field]; present && string(submitted) != "null" {
		var plain string
		if err := json.Unmarshal(submitted, &plain); err != nil {
			return nil, fmt.Errorf("auth config: field %q: want a string: %w", field, err)
		}
		switch plain {
		case MaskedSecretEcho:
			// Sentinel echoed back: keep the stored secret (write-only mode).
		case "":
			effective = ""
		default:
			if m.cipher == nil {
				return nil, fmt.Errorf("auth config: field %q: %w", field, ErrNoMasterKey)
			}
			sealed, err := m.cipher.Encrypt(plain)
			if err != nil {
				return nil, fmt.Errorf("auth config: sealing %q: %w", field, err)
			}
			effective = sealed
		}
	}
	return withSecretField(section, body, parentKey, field, effective)
}

// withSecretField rewrites one secret field of the doc (creating the parent
// object when needed) and returns the canonical stored form.
func withSecretField(section string, body []byte, parentKey, field, value string) ([]byte, error) {
	var doc map[string]any
	trimmed := strings.TrimSpace(string(body))
	if trimmed == "" || trimmed == "null" {
		doc = map[string]any{}
	} else if err := json.Unmarshal(body, &doc); err != nil {
		return nil, fmt.Errorf("auth config: %s: not a JSON object: %w", section, err)
	}
	if parentKey == "" {
		doc[field] = value
	} else {
		sub, ok := doc[parentKey].(map[string]any)
		if !ok {
			sub = map[string]any{}
		}
		sub[field] = value
		doc[parentKey] = sub
	}
	b, err := json.Marshal(doc)
	if err != nil {
		return nil, fmt.Errorf("auth config: encoding %s section: %w", section, err)
	}
	return canonicalizeSection(section, b), nil
}

// canonicalizeSection decodes (defaults applied) and re-encodes one doc so
// the stored form always carries the full explicit shape.
func canonicalizeSection(section string, body []byte) []byte {
	v, err := DecodeAuthSection(section, body)
	if err != nil {
		return body // undecodable docs never reach the store (validation first)
	}
	var masked any
	switch s := v.(type) {
	case *LDAPSection:
		masked = s
	case *OIDCSection:
		masked = s
	case *SAMLSection:
		masked = s
	default:
		return body
	}
	if b, err := marshalSectionDoc(masked); err == nil {
		return b
	}
	return body
}

// swapSnapshot installs the next snapshot and schedules the replaced LDAP
// pool's drain (grace window; PoolGrace<0 closes immediately — tests).
func (m *ConfigManager) swapSnapshot(next *authSnapshot) {
	old := m.snap.Swap(next)
	if old == nil || old.ldapProv == nil || old.ldapProv == next.ldapProv {
		return
	}
	if m.grace < 0 {
		old.ldapProv.Close()
		return
	}
	time.AfterFunc(m.grace, old.ldapProv.Close)
}

// clone copies the snapshot for one-section replacement.
func (s *authSnapshot) clone() *authSnapshot {
	c := *s
	return &c
}

// ---------------------------------------------------------------------------
// Test connection
// ---------------------------------------------------------------------------

// TestAuthSection probes a candidate configuration (body) or, when body is
// empty, the stored one. The probes (ADR-0035 decision 6): LDAP = guarded
// dial + manager bind + searchFilter trial search (optional user bind when
// testUsername/testPassword both ride the body); OIDC = discovery fetch;
// SAML = loginUrl fetch. Secrets of a candidate body ride the same
// write-only merge (sentinel/absent = stored value, §1.6: the server
// decrypts before the real connection).
func (m *ConfigManager) TestAuthSection(ctx context.Context, section string, body []byte) (TestReport, error) {
	if err := knownSection(section); err != nil {
		return TestReport{}, err
	}
	var merged []byte
	switch {
	case len(strings.TrimSpace(string(body))) == 0:
		rec, err := m.store.GetAuthConfig(ctx, section)
		if err != nil {
			return TestReport{}, fmt.Errorf("auth config: reading section %s: %w", section, err)
		}
		if rec == nil {
			return TestReport{OK: false, Phase: "config", Category: "unset",
				Message: "no stored configuration for this section; submit the candidate form first"}, nil
		}
		merged = []byte(rec.Doc)
	default:
		// The §1.6 test envelope: the submitted form values PLUS the two
		// test fields — strip them before the strict section merge (they
		// are probe parameters, not section keys).
		sectionBody := body
		if section == SectionLDAP {
			sectionBody = stripLDAPTestFields(body)
		}
		var err error
		if merged, err = m.mergeSecrets(ctx, section, sectionBody); err != nil {
			return TestReport{}, err
		}
	}
	switch section {
	case SectionLDAP:
		return m.testLDAP(ctx, merged, body), nil
	case SectionOIDC:
		return m.testOIDC(ctx, merged), nil
	case SectionSAML:
		return m.testSAML(ctx, merged), nil
	default:
		return TestReport{}, fmt.Errorf("%w: %q", ErrSectionUnknown, section)
	}
}

// ldapTestCreds is the anchored §1.6 test envelope riding the section form.
type ldapTestCreds struct {
	TestUsername string `json:"testUsername"`
	TestPassword string `json:"testPassword"`
}

// stripLDAPTestFields removes the two test envelope fields from a submitted
// LDAP form body (returning the bare section document).
func stripLDAPTestFields(body []byte) []byte {
	var doc map[string]json.RawMessage
	if err := json.Unmarshal(body, &doc); err != nil {
		return body
	}
	delete(doc, "testUsername")
	delete(doc, "testPassword")
	b, err := json.Marshal(doc)
	if err != nil {
		return body
	}
	return b
}

func (m *ConfigManager) testLDAP(ctx context.Context, merged, rawBody []byte) TestReport {
	sec, err := m.sectionLDAPUnsealed(string(merged))
	if err != nil {
		return TestReport{OK: false, Phase: "config", Category: "invalid", Message: err.Error()}
	}
	if !sec.Enabled {
		return TestReport{OK: false, Phase: "config", Category: "disabled",
			Message: "ldap settings are disabled; enable them to test the connection"}
	}
	var test ldapTestCreds
	if len(strings.TrimSpace(string(rawBody))) > 0 {
		_ = json.Unmarshal(rawBody, &test)
	}
	if (test.TestUsername == "") != (test.TestPassword == "") {
		// Anchored §1.6 rejection — the user-bind form needs both halves.
		return TestReport{OK: false, Phase: "config", Category: "invalid", Message: ErrTestCredsIncomplete.Error()}
	}

	u, err := url.Parse(sec.LDAPURL)
	if err != nil || u.Host == "" {
		return TestReport{OK: false, Phase: "config", Category: "invalid",
			Message: "ldapUrl must be an ldap(s) URL like ldap://host:389/dc=example,dc=com"}
	}
	host := u.Hostname()
	isTLS := strings.EqualFold(u.Scheme, "ldaps")
	tlsConf := &tls.Config{
		ServerName: host,
		// Operator opt-out (auth.ldap.skip_tls_verify posture) mirrored
		// from the runtime arm — evaluation-only escape hatch.
		InsecureSkipVerify: sec.SkipTLSVerify, //nolint:gosec // see above
	}
	lconn, err := m.probeLDAPConn(ctx, sec.LDAPURL, tlsConf, isTLS)
	if err != nil {
		return probeDialFailure(err)
	}
	defer lconn.Close() //nolint:errcheck // best-effort probe teardown
	if !isTLS && sec.StartTLS {
		if err := lconn.StartTLS(tlsConf); err != nil {
			return TestReport{OK: false, Phase: "starttls", Category: "tls",
				Message: "starttls upgrade failed: the server refused the upgrade or the certificate was rejected"}
		}
	}

	// Manager (or anonymous) bind — §1.2 #4: an empty managerDn binds
	// anonymously read-only.
	if err := lconn.Bind(sec.Search.ManagerDN, sec.Search.ManagerPassword); err != nil {
		return TestReport{OK: false, Phase: "bind", Category: "bad_credentials",
			Message: "manager bind failed: the directory rejected the credentials (or anonymous bind is disallowed)"}
	}

	// Trial search through the configured filter (ADR-0035 decision 6).
	probeUser := test.TestUsername
	if probeUser == "" {
		probeUser = "binflow-connectivity-probe"
	}
	filter := strings.ReplaceAll(sec.Search.SearchFilter, "{0}", ldap.EscapeFilter(probeUser))
	if filter == "" {
		filter = "(uid=" + ldap.EscapeFilter(probeUser) + ")"
	}
	base := strings.Trim(u.Path, "/")
	if sb := strings.TrimSpace(sec.Search.SearchBase); sb != "" && base != "" {
		base = sb + "," + base
	}
	scope := ldap.ScopeWholeSubtree
	if !sec.Search.SearchSubTree {
		scope = ldap.ScopeSingleLevel
	}
	res, err := lconn.Search(ldap.NewSearchRequest(
		base, scope, ldap.NeverDerefAliases, 2, int(probeTimeout.Seconds()), false,
		filter, []string{"dn"}, nil,
	))
	if err != nil {
		return TestReport{OK: false, Phase: "search", Category: "invalid",
			Message: "trial search failed: check the search filter and base DN"}
	}

	// Optional user bind (the anchored full form): both halves present and
	// the filter resolved the entry.
	if test.TestUsername != "" {
		if len(res.Entries) == 0 {
			return TestReport{OK: false, Phase: "search", Category: "no_entries",
				Message: "the trial search matched no entries for the test username"}
		}
		if err := lconn.Bind(res.Entries[0].DN, test.TestPassword); err != nil {
			return TestReport{OK: false, Phase: "user_bind", Category: "bad_credentials",
				Message: "the directory rejected the test user's credentials"}
		}
		return TestReport{OK: true, Phase: "user_bind", Category: "ok",
			Message: "Successfully connected and authenticated the test user"}
	}
	return TestReport{OK: true, Phase: "search", Category: "ok",
		Message: "Successfully connected and searched the directory"}
}

func (m *ConfigManager) testOIDC(ctx context.Context, merged []byte) TestReport {
	sec, err := m.sectionOIDCUnsealed(string(merged))
	if err != nil {
		return TestReport{OK: false, Phase: "config", Category: "invalid", Message: err.Error()}
	}
	if !sec.Enabled {
		return TestReport{OK: false, Phase: "config", Category: "disabled",
			Message: "oauth/oidc settings are disabled; enable them to test the connection"}
	}
	discoveryURL := strings.TrimRight(sec.IssuerURL, "/") + "/.well-known/openid-configuration"
	if m.screen != nil {
		if err := m.screen(ctx, discoveryURL); err != nil {
			return TestReport{OK: false, Phase: "screen", Category: "rejected",
				Message: "discovery target rejected by the outbound guard: " + guardCategory(err)}
		}
	}
	body, err := m.probeFetch(ctx, discoveryURL)
	if err != nil {
		return probeFetchFailure("discovery", err)
	}
	var doc struct {
		Issuer string `json:"issuer"`
	}
	if err := json.Unmarshal(body, &doc); err != nil || doc.Issuer == "" {
		return TestReport{OK: false, Phase: "discovery", Category: "invalid",
			Message: "the discovery document is not valid OIDC metadata (missing issuer)"}
	}
	return TestReport{OK: true, Phase: "discovery", Category: "ok",
		Message: "Successfully fetched and validated the OIDC discovery document"}
}

func (m *ConfigManager) testSAML(ctx context.Context, merged []byte) TestReport {
	sec, err := decodeSAMLSection(merged)
	if err != nil {
		return TestReport{OK: false, Phase: "config", Category: "invalid", Message: err.Error()}
	}
	if !sec.EnableIntegration {
		return TestReport{OK: false, Phase: "config", Category: "disabled",
			Message: "saml settings are disabled; enable them to test the connection"}
	}
	if m.screen != nil {
		if err := m.screen(ctx, sec.LoginURL); err != nil {
			return TestReport{OK: false, Phase: "screen", Category: "rejected",
				Message: "login target rejected by the outbound guard: " + guardCategory(err)}
		}
	}
	if _, err := m.probeFetch(ctx, sec.LoginURL); err != nil {
		return probeFetchFailure("fetch", err)
	}
	return TestReport{OK: true, Phase: "fetch", Category: "ok",
		Message: "Successfully reached the SAML login endpoint"}
}

// probeLDAPConn opens the LDAP probe connection through the seam (guarded
// default; test fakes ride Options.LDAPProbeDial).
func (m *ConfigManager) probeLDAPConn(ctx context.Context, rawURL string, tlsConf *tls.Config, implicitTLS bool) (LDAPProbeConn, error) {
	if m.ldapProbe != nil {
		return m.ldapProbe(ctx, rawURL, tlsConf, implicitTLS)
	}
	u, err := url.Parse(rawURL)
	if err != nil || u.Host == "" {
		return nil, fmt.Errorf("auth config: ldap probe target %q: must be an ldap(s) URL", rawURL)
	}
	port := u.Port()
	if port == "" {
		if implicitTLS {
			port = "636"
		} else {
			port = "389"
		}
	}
	dialCtx, cancel := context.WithTimeout(ctx, probeTimeout)
	defer cancel()
	var raw net.Conn
	if m.dial != nil {
		raw, err = m.dial(dialCtx, "tcp", net.JoinHostPort(u.Hostname(), port))
	} else {
		var d net.Dialer
		raw, err = d.DialContext(dialCtx, "tcp", net.JoinHostPort(u.Hostname(), port))
	}
	if err != nil {
		return nil, err
	}
	if implicitTLS {
		raw = tls.Client(raw, tlsConf)
	}
	conn := ldap.NewConn(raw, implicitTLS)
	conn.SetTimeout(probeTimeout)
	conn.Start()
	return conn, nil
}

// probeFetch performs one capped, short-timeout GET through the probe
// client (no redirect following — one hop is the whole probe).
func (m *ConfigManager) probeFetch(ctx context.Context, rawURL string) ([]byte, error) {
	reqCtx, cancel := context.WithTimeout(ctx, probeTimeout)
	defer cancel()
	req, err := http.NewRequestWithContext(reqCtx, http.MethodGet, rawURL, nil)
	if err != nil {
		return nil, fmt.Errorf("building request: %w", err)
	}
	client := m.httpClient
	if client == nil {
		client = &http.Client{}
	}
	resp, err := client.Do(req)
	if err != nil {
		return nil, err
	}
	defer func() { _ = resp.Body.Close() }()
	if resp.StatusCode < 200 || resp.StatusCode >= 300 {
		return nil, fmt.Errorf("http status %d", resp.StatusCode)
	}
	return io.ReadAll(io.LimitReader(resp.Body, probeMaxBody))
}

// ---------------------------------------------------------------------------
// Small helpers
// ---------------------------------------------------------------------------

// knownSection validates the closed set.
func knownSection(section string) error {
	switch section {
	case SectionLDAP, SectionOIDC, SectionSAML:
		return nil
	default:
		return fmt.Errorf("%w: %q", ErrSectionUnknown, section)
	}
}

// restPathOf names the REST home of one section (WARN/log payloads).
func restPathOf(section string) string {
	switch section {
	case SectionLDAP:
		return "/binflow/api/v1/admin/security/ldap"
	case SectionOIDC:
		return "/binflow/api/v1/admin/security/oauth"
	case SectionSAML:
		return "/binflow/api/v1/admin/security/saml/config"
	default:
		return "/binflow/api/v1/admin/security"
	}
}

// seedConfigured reports whether a seed doc carries enabled or any
// non-default key (K31 rule ②'s "configured" test — compared against the
// defaults-applied zero shape with the enabled flag ignored).
func seedConfigured(section string, doc []byte) bool {
	v, err := DecodeAuthSection(section, doc)
	if err != nil {
		// An undecodable seed is a config-plane error the boot surfaces
		// elsewhere (Load seeds only after ValidateAuthSection passes);
		// as a seed it counts as configured (loud, never silently dropped).
		return true
	}
	switch s := v.(type) {
	case *LDAPSection:
		d := defaultLDAPSection()
		s.Enabled, s.Key = d.Enabled, d.Key
		return !reflect.DeepEqual(*s, *d)
	case *OIDCSection:
		d := defaultOIDCSection()
		s.Enabled = d.Enabled
		return !reflect.DeepEqual(*s, *d)
	case *SAMLSection:
		d := defaultSAMLSection()
		s.EnableIntegration = d.EnableIntegration
		return !reflect.DeepEqual(*s, *d)
	default:
		return false
	}
}

// changedKeys diffs two canonical docs' top-level keys (audit summary:
// names only, never values).
func changedKeys(prev *StoredAuthConfig, next []byte) []string {
	if prev == nil {
		return []string{"*"} // creation: every key is new
	}
	var a, b map[string]json.RawMessage
	_ = json.Unmarshal([]byte(prev.Doc), &a)
	_ = json.Unmarshal(next, &b)
	var out []string
	for k, vb := range b {
		va, ok := a[k]
		if !ok || string(va) != string(vb) {
			out = append(out, k)
		}
	}
	for k := range a {
		if _, ok := b[k]; !ok {
			out = append(out, "-"+k)
		}
	}
	return out
}

// probeDialFailure classifies a probe dial error (phase + category word;
// the message never carries the target's userinfo).
func probeDialFailure(err error) TestReport {
	if isTLSShaped(err) {
		return TestReport{OK: false, Phase: "dial", Category: "tls",
			Message: "tls handshake failed: the certificate was rejected"}
	}
	if isGuardRejection(err) {
		return TestReport{OK: false, Phase: "dial", Category: "rejected",
			Message: "target rejected by the outbound guard: " + guardCategory(err)}
	}
	return TestReport{OK: false, Phase: "dial", Category: "unreachable",
		Message: "could not connect to the target (dial failed or timed out)"}
}

// probeFetchFailure classifies a probe fetch error.
func probeFetchFailure(phase string, err error) TestReport {
	if isGuardRejection(err) {
		return TestReport{OK: false, Phase: phase, Category: "rejected",
			Message: "target rejected by the outbound guard: " + guardCategory(err)}
	}
	return TestReport{OK: false, Phase: phase, Category: "unreachable",
		Message: "could not fetch the target (" + phase + " failed or timed out)"}
}

// isGuardRejection reports whether err smells of the M3 Guard's rejection
// (the remote.RejectionError text; matched by prefix because auth must not
// import the remote package — the Guard seams are function-typed).
func isGuardRejection(err error) bool {
	return err != nil && strings.Contains(err.Error(), "rejected")
}

// isTLSShaped reports whether err smells of a TLS failure (x509/certificate
// wording — the same family ldap.go's ErrTLSHandshake classifies).
func isTLSShaped(err error) bool {
	if err == nil {
		return false
	}
	msg := err.Error()
	return strings.Contains(msg, "certificate") || strings.Contains(msg, "x509") ||
		strings.Contains(msg, "tls handshake")
}

// guardCategory reduces a guard-shaped error to its category word for the
// diagnostic message.
func guardCategory(err error) string {
	msg := err.Error()
	for _, cat := range []string{"scheme_not_http", "no_host", "loopback", "private_rfc1918",
		"private_ula", "link_local", "unspecified", "multicast", "broadcast", "reserved", "teredo"} {
		if strings.Contains(msg, cat) {
			return cat
		}
	}
	return "policy"
}

// NewConfigStoreAdapter adapts the metadata sub-store onto the
// manager's consumer-side seam.
func NewConfigStoreAdapter(s metadata.AuthConfigStore) ConfigStore {
	return &authConfigStoreAdapter{s: s}
}

type authConfigStoreAdapter struct{ s metadata.AuthConfigStore }

func (a *authConfigStoreAdapter) GetAuthConfig(ctx context.Context, section string) (*StoredAuthConfig, error) {
	rec, err := a.s.GetAuthConfig(ctx, section)
	if err != nil || rec == nil {
		return nil, err
	}
	return &StoredAuthConfig{Section: rec.Section, Doc: rec.Doc, UpdatedAt: rec.UpdatedAt, UpdatedBy: rec.UpdatedBy}, nil
}

func (a *authConfigStoreAdapter) PutAuthConfig(ctx context.Context, rec *StoredAuthConfig) error {
	return a.s.PutAuthConfig(ctx, &metadata.AuthConfigRecord{
		Section: rec.Section, Doc: rec.Doc, UpdatedAt: rec.UpdatedAt, UpdatedBy: rec.UpdatedBy,
	})
}

func (a *authConfigStoreAdapter) ListAuthConfigs(ctx context.Context) ([]*StoredAuthConfig, error) {
	rows, err := a.s.ListAuthConfigs(ctx)
	if err != nil {
		return nil, err
	}
	out := make([]*StoredAuthConfig, 0, len(rows))
	for _, r := range rows {
		out = append(out, &StoredAuthConfig{Section: r.Section, Doc: r.Doc, UpdatedAt: r.UpdatedAt, UpdatedBy: r.UpdatedBy})
	}
	return out, nil
}
