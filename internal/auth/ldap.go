// LDAP authentication extension (M6, ADR-0020): the LDAPProvider implements
// IdentityProvider using github.com/go-ldap/ldap/v3 for user search, bind
// verification, and group membership resolution.
//
// LDAP does NOT have its own Bearer arm (ADR-0020): LDAP authentication happens
// only at the login endpoint (POST /api/v1/session). The Authenticate method
// returns ErrInvalidCredentials because LDAP tokens are not supported. The Bind
// method is the primary entry point: it searches for the user DN, binds to
// verify the password, then searches for group memberships to build Claims.
//
// Configuration is carried by LDAPConfig. The bind_password is an env-only
// secret (BINFLOW_AUTH_LDAP_BIND_PASSWORD) and never appears in the YAML
// config file. Connection pooling uses a channel-based pool with a default size
// of 5 connections.

package auth

import (
	"context"
	"crypto/tls"
	"errors"
	"fmt"
	"log/slog"
	"net"
	"net/url"
	"strings"
	"sync"
	"time"

	ldap "github.com/go-ldap/ldap/v3"
)

// LDAPConfig carries the configuration for an LDAP identity provider. All
// fields are populated at construction time and remain immutable afterward.
//
// bind_password is NOT a field here — it is read at construction from the
// environment (BINFLOW_AUTH_LDAP_BIND_PASSWORD) so it never enters the YAML
// config file. The caller passes it explicitly to NewLDAPProvider.
type LDAPConfig struct {
	// Enabled controls whether LDAP authentication is active. When false,
	// NewLDAPProvider returns nil, nil (no-op provider).
	Enabled bool

	// URL is the LDAP server URL (e.g. "ldap://ldap.example.com:389" or
	// "ldaps://ldap.example.com:636"). Required when Enabled=true.
	URL string

	// BaseDN is the search base distinguished name (e.g. "dc=example,dc=com").
	// Required when Enabled=true.
	BaseDN string

	// BindDN is the DN of the service account used to bind for user search.
	// When empty, the provider skips the search-bind step and attempts direct
	// bind as the user (uid=<username>,<base_dn>).
	BindDN string

	// BindPassword is the password for the BindDN service account. This is
	// the value read from BINFLOW_AUTH_LDAP_BIND_PASSWORD; it is never
	// hardcoded in YAML.
	BindPassword string

	// UserFilter is the LDAP filter template for searching user entries. The
	// first %s is replaced with the login username. Example:
	// "(&(objectClass=posixAccount)(uid=%s))". Defaults to "(uid=%s)".
	UserFilter string

	// UserIDAttr is the LDAP attribute that maps to the BinFlow username
	// (Claims.Name). Defaults to "uid".
	UserIDAttr string

	// GroupFilter is the LDAP filter template for searching group entries.
	// The first %s is replaced with the user's DN or username. Example:
	// "(&(objectClass=posixGroup)(memberUid=%s))". When empty, no group
	// search is performed.
	GroupFilter string

	// GroupNameAttr is the LDAP attribute that holds the group name.
	// Defaults to "cn".
	GroupNameAttr string

	// AdminGroup is the DN of a group whose members are granted the admin
	// role. When empty, no group-inferred admin mapping is performed.
	AdminGroup string

	// ReadOnlyGroup is the DN of a group whose members are granted the
	// readonly_admin role (M7, ADR-0026 decision 6 — config key
	// auth.ldap.readonly_group). The admin mapping wins when both groups
	// match: the authority ladder is admin_group > readonly_group > user.
	// Empty (the default) keeps the pre-M7 behavior: no readonly mapping is
	// performed.
	ReadOnlyGroup string

	// PoolSize is the maximum number of idle connections kept in the pool.
	// Defaults to 5.
	PoolSize int

	// StartTLS enables the StartTLS extended operation on ldap://
	// connections: the connection is dialed on the plaintext port and
	// upgraded to TLS before any LDAP traffic (including the service bind)
	// is sent. Mutually exclusive with the ldaps:// URL scheme by
	// construction — ldaps:// already carries implicit TLS, so a StartTLS
	// upgrade there would fail ("already encrypted"); when both are
	// configured, ldaps wins and the flag is ignored with a WARN
	// (PRD FR-55: "start_tls: ldaps:// 时忽略").
	//
	// When the upgrade fails (server refuses StartTLS, certificate rejected),
	// the connection is torn down and the dial fails — BinFlow never falls
	// back to silently sending credentials in plaintext (T-174 D5).
	StartTLS bool

	// SkipTLSVerify disables TLS certificate verification for ldaps:// and
	// StartTLS connections (auth.ldap.skip_tls_verify, default false). It is
	// an evaluation escape hatch for self-signed directory certificates
	// (NFR-S36: "仅限评估，生产必须 false"); enabling it logs a WARN at
	// construction. When TLSConfig is also set, the flag is merged into the
	// clone used for dialing — the caller's config is never mutated.
	SkipTLSVerify bool

	// GroupBaseDN is the search base for group searches
	// (auth.ldap.group_base_dn). Empty falls back to BaseDN; directories
	// that keep groups in a dedicated subtree (e.g.
	// "ou=groups,dc=example,dc=org") set it so the group filter does not
	// scan the user tree (PRD FR-55 step 5).
	GroupBaseDN string

	// TLSConfig is the TLS configuration for LDAPS or StartTLS connections.
	// When nil, a default is built from the URL host with
	// InsecureSkipVerify taken from SkipTLSVerify and the system roots as
	// the trust store. The InsecureSkipVerify flag should be set only in
	// development.
	TLSConfig *tls.Config

	// ConnectTimeout is the maximum time to wait for a TCP connection.
	// Defaults to 10 seconds.
	ConnectTimeout time.Duration

	// RequestTimeout is the maximum time to wait for an LDAP operation.
	// Defaults to 30 seconds.
	RequestTimeout time.Duration
}

// defaults populates zero-valued fields with their documented defaults.
func (c *LDAPConfig) defaults() {
	if c.UserFilter == "" {
		c.UserFilter = "(uid=%s)"
	}
	if c.UserIDAttr == "" {
		c.UserIDAttr = "uid"
	}
	if c.GroupNameAttr == "" {
		c.GroupNameAttr = "cn"
	}
	if c.GroupBaseDN == "" {
		// Group searches default to the user search base (PRD FR-55:
		// group_base_dn is optional; empty = base_dn).
		c.GroupBaseDN = c.BaseDN
	}
	if c.PoolSize <= 0 {
		c.PoolSize = 5
	}
	if c.ConnectTimeout <= 0 {
		c.ConnectTimeout = 10 * time.Second
	}
	if c.RequestTimeout <= 0 {
		c.RequestTimeout = 30 * time.Second
	}
}

// ---------------------------------------------------------------------------
// LDAPConn is the consumer-side interface for LDAP operations. It mirrors the
// subset of *ldap.Conn methods that LDAPProvider needs. The real
// implementation wraps *ldap.Conn; tests replace it with a mock.
// ---------------------------------------------------------------------------

// LDAPConn is the LDAP connection interface consumed by LDAPProvider. It
// abstracts the subset of *ldap.Conn operations needed for the bind-search
// flow, allowing tests to swap in a mock. Exported for use by external test
// packages.
type LDAPConn interface {
	Bind(username, password string) error
	Search(searchRequest *ldap.SearchRequest) (*ldap.SearchResult, error)
	StartTLS(config *tls.Config) error
	Close() error
	SetTimeout(timeout time.Duration)
}

// LDAPConnWrapper wraps a real *ldap.Conn to satisfy LDAPConn.
type LDAPConnWrapper struct{ conn *ldap.Conn }

// Bind authenticates the connection against the directory (see *ldap.Conn.Bind).
func (w *LDAPConnWrapper) Bind(username, password string) error {
	return w.conn.Bind(username, password)
}

// Search performs a directory search (see *ldap.Conn.Search).
func (w *LDAPConnWrapper) Search(searchRequest *ldap.SearchRequest) (*ldap.SearchResult, error) {
	return w.conn.Search(searchRequest)
}

// StartTLS upgrades the plaintext connection to TLS (see *ldap.Conn.StartTLS).
func (w *LDAPConnWrapper) StartTLS(config *tls.Config) error {
	return w.conn.StartTLS(config)
}

// Close tears the connection down (see *ldap.Conn.Close).
func (w *LDAPConnWrapper) Close() error {
	return w.conn.Close()
}

// SetTimeout applies the per-operation timeout (see *ldap.Conn.SetTimeout).
func (w *LDAPConnWrapper) SetTimeout(timeout time.Duration) {
	w.conn.SetTimeout(timeout)
}

// LDAPDialer is a function that creates a new LDAPConn. Tests replace this
// with a mock factory. Exported for use by external test packages.
type LDAPDialer func(ctx context.Context, urlStr string, opts ...ldap.DialOpt) (LDAPConn, error)

// defaultDialer creates a real LDAP connection using DialURL. Dial failures
// carry the classification sentinels (T-187 / T-174 O-2): a TLS-shaped
// failure (ldaps:// certificate rejection) wraps ErrTLSHandshake, everything
// else wraps ErrProviderUnreachable — the login path folds them into
// provider_error / tls_handshake audit reasons while the caller still sees
// the uniform 401.
func defaultDialer(_ context.Context, urlStr string, opts ...ldap.DialOpt) (LDAPConn, error) {
	conn, err := ldap.DialURL(urlStr, opts...)
	if err != nil {
		// Two %w verbs: the sentinel carries the classification, the
		// original error stays reachable for errors.As/Is.
		if isTLSFailure(err) {
			return nil, fmt.Errorf("auth: ldap dial %s: %w: %w", urlStr, ErrTLSHandshake, err)
		}
		return nil, fmt.Errorf("auth: ldap dial %s: %w: %w", urlStr, ErrProviderUnreachable, err)
	}
	return &LDAPConnWrapper{conn: conn}, nil
}

// ---------------------------------------------------------------------------
// Connection pool
// ---------------------------------------------------------------------------

// ldapPool is a simple channel-based connection pool. It holds up to maxSize
// idle connections. When the pool is empty, a new connection is dialed; when
// full, excess connections are closed when returned.
type ldapPool struct {
	pool    chan LDAPConn
	dial    LDAPDialer
	url     string
	opts    []ldap.DialOpt
	maxSize int
	mu      sync.Mutex
	closed  bool
}

// newLDAPPool creates a new connection pool. Call Close() when done.
func newLDAPPool(dial LDAPDialer, urlStr string, maxSize int, opts ...ldap.DialOpt) *ldapPool {
	if maxSize <= 0 {
		maxSize = 5
	}
	return &ldapPool{
		pool:    make(chan LDAPConn, maxSize),
		dial:    dial,
		url:     urlStr,
		opts:    opts,
		maxSize: maxSize,
	}
}

// get retrieves a connection from the pool or dials a new one.
func (p *ldapPool) get(ctx context.Context) (LDAPConn, error) {
	p.mu.Lock()
	if p.closed {
		p.mu.Unlock()
		return nil, errors.New("auth: ldap pool is closed")
	}
	p.mu.Unlock()

	select {
	case conn := <-p.pool:
		// Got an idle connection from the pool.
		return conn, nil
	default:
		// Pool is empty, dial a new connection.
		return p.dial(ctx, p.url, p.opts...)
	}
}

// put returns a connection to the pool. If the pool is full, the connection
// is closed instead.
func (p *ldapPool) put(conn LDAPConn) {
	if conn == nil {
		return
	}
	select {
	case p.pool <- conn:
		// Returned to pool.
	default:
		// Pool is full, close the connection.
		_ = conn.Close()
	}
}

// Close drains and closes all connections in the pool.
func (p *ldapPool) Close() {
	p.mu.Lock()
	defer p.mu.Unlock()
	if p.closed {
		return
	}
	p.closed = true
	close(p.pool)
	for conn := range p.pool {
		_ = conn.Close()
	}
}

// ---------------------------------------------------------------------------
// LDAPUserResolver
// ---------------------------------------------------------------------------

// LDAPUserResolver is the consumer-side seam for looking up a user row by
// (provider, providerID). LDAPProvider.Resolve delegates to this interface.
type LDAPUserResolver interface {
	// GetByProvider returns the user identified by the given (provider,
	// providerID) pair. It returns ErrProviderUserNotFound when no row
	// matches.
	GetByProvider(ctx context.Context, provider Provider, providerID string) (*ProviderUser, error)
}

// ---------------------------------------------------------------------------
// LDAPProvider
// ---------------------------------------------------------------------------

// LDAPProvider implements IdentityProvider for LDAP directories. It handles
// user search, password bind verification, and group membership resolution.
//
// The Authenticate method always returns ErrInvalidCredentials because LDAP
// does not support Bearer tokens (ADR-0020). The Bind method is the primary
// entry point for LDAP authentication at the login endpoint.
type LDAPProvider struct {
	config   *LDAPConfig
	resolver LDAPUserResolver
	pool     *ldapPool
}

// NewLDAPProvider constructs an LDAPProvider. When cfg.Enabled is false, it
// returns (nil, nil) — the caller should treat this as "LDAP not configured".
//
// resolver is the seam for looking up local user rows by provider+providerID;
// it can be nil when only Bind (no Resolve) is needed.
//
// The dialer parameter is the factory for creating LDAP connections. Pass nil
// to use the default (real) LDAP dialer. Tests pass a mock dialer.
func NewLDAPProvider(cfg *LDAPConfig, resolver LDAPUserResolver, dialer LDAPDialer) (*LDAPProvider, error) {
	if !cfg.Enabled {
		return nil, nil
	}
	cfg.defaults()

	if cfg.URL == "" {
		return nil, errors.New("auth: ldap url is required")
	}
	if cfg.BaseDN == "" {
		return nil, errors.New("auth: ldap base_dn is required")
	}

	// The URL scheme selects the TLS posture (fail fast on anything the
	// config layer already rejects): ldaps:// dials implicit TLS, ldap://
	// stays plaintext unless StartTLS upgrades it.
	u, err := url.Parse(cfg.URL)
	if err != nil || u.Host == "" {
		return nil, fmt.Errorf("auth: ldap url %q: must be an ldap(s) URL like ldap://host:389", cfg.URL)
	}
	scheme := strings.ToLower(u.Scheme)
	if scheme != "ldap" && scheme != "ldaps" {
		return nil, fmt.Errorf("auth: ldap url %q: scheme must be ldap or ldaps, got %q", cfg.URL, u.Scheme)
	}

	if dialer == nil {
		dialer = defaultDialer
	}

	// Resolve the single effective TLS configuration shared by both TLS
	// legs — ldaps:// through DialWithTLSConfig at dial time, start_tls via
	// the StartTLS extended operation — so the verification posture cannot
	// differ between them. A caller-provided TLSConfig is cloned, never
	// mutated.
	tlsConf := cfg.TLSConfig
	if tlsConf == nil {
		tlsConf = &tls.Config{}
	} else {
		tlsConf = tlsConf.Clone()
	}
	// ServerName defaults from the URL host: ldaps:// would derive it from
	// the dial address anyway, but the StartTLS upgrade wraps an existing
	// connection, where Go cannot infer a name and would refuse the
	// handshake ("either ServerName or InsecureSkipVerify must be
	// specified").
	if tlsConf.ServerName == "" {
		tlsConf.ServerName = u.Hostname()
	}
	if tlsConf.MinVersion == 0 {
		tlsConf.MinVersion = tls.VersionTLS12
	}
	if cfg.SkipTLSVerify && !tlsConf.InsecureSkipVerify {
		tlsConf.InsecureSkipVerify = true
		slog.Warn("auth: ldap skip_tls_verify=true disables TLS certificate verification — evaluation only, never use in production",
			slog.String("url", cfg.URL))
	}

	switch {
	case scheme == "ldaps":
		// Implicit TLS: the URL scheme owns the encryption. start_tls is
		// mutually exclusive by construction (RFC 4513 StartTLS extends a
		// plaintext session; go-ldap refuses the upgrade on an already
		// encrypted connection), so ldaps wins and the flag is ignored
		// with a WARN instead of producing a broken double upgrade.
		if cfg.StartTLS {
			slog.Warn("auth: ldap start_tls is ignored for ldaps:// URLs — the connection already uses implicit TLS; remove start_tls from auth.ldap",
				slog.String("url", cfg.URL))
		}
	case cfg.StartTLS:
		// Explicit upgrade on the plaintext port (T-174 D5): the upgrade
		// runs inside the dialer, so every connection entering the pool is
		// already TLS and the service bind never leaves in the clear. A
		// failed upgrade (server refuses StartTLS, certificate rejected)
		// tears the connection down — no silent plaintext fallback.
		inner := dialer
		dialer = func(ctx context.Context, urlStr string, opts ...ldap.DialOpt) (LDAPConn, error) {
			conn, err := inner(ctx, urlStr, opts...)
			if err != nil {
				return nil, err
			}
			if err := conn.StartTLS(tlsConf); err != nil {
				_ = conn.Close()
				// ErrTLSHandshake carries the O-2 classification: the audit
				// trail records tls_handshake, not bad_credentials. The
				// double %w keeps the underlying cause reachable too.
				return nil, fmt.Errorf("auth: ldap starttls upgrade %s: %w: %w", urlStr, ErrTLSHandshake, err)
			}
			return conn, nil
		}
	}

	// Build DialOpts from config. The TLS configuration only takes effect
	// for ldaps:// URLs (go-ldap ignores it on the plaintext port, where
	// the StartTLS wrapper above owns the upgrade).
	var opts []ldap.DialOpt
	if cfg.ConnectTimeout > 0 {
		opts = append(opts, ldap.DialWithDialer(&net.Dialer{
			Timeout: cfg.ConnectTimeout,
		}))
	}
	opts = append(opts, ldap.DialWithTLSConfig(tlsConf))

	p := &LDAPProvider{
		config:   cfg,
		resolver: resolver,
		pool:     newLDAPPool(dialer, cfg.URL, cfg.PoolSize, opts...),
	}
	return p, nil
}

// ProviderName returns ProviderLDAP ("ldap").
func (p *LDAPProvider) ProviderName() Provider { return ProviderLDAP }

// Authenticate always returns ErrInvalidCredentials because LDAP does not
// support Bearer token authentication (ADR-0020). LDAP authentication happens
// only at the login endpoint via the Bind method.
func (p *LDAPProvider) Authenticate(_ context.Context, _ string) (*Claims, error) {
	return nil, fmt.Errorf("auth: ldap does not support bearer tokens: %w", ErrInvalidCredentials)
}

// Resolve looks up an existing user row by (ProviderLDAP, providerID). It
// returns ErrProviderUserNotFound when no row matches.
func (p *LDAPProvider) Resolve(ctx context.Context, provider Provider, providerID string) (*ProviderUser, error) {
	if p.resolver == nil {
		return nil, errProviderUserNotFound(provider, providerID)
	}
	pu, err := p.resolver.GetByProvider(ctx, provider, providerID)
	if err != nil {
		if errors.Is(err, ErrProviderUserNotFound) {
			return nil, err
		}
		return nil, fmt.Errorf("auth: ldap resolve %s/%s: %w", provider, providerID, err)
	}
	return pu, nil
}

// Bind authenticates a user against the LDAP directory and returns Claims
// with the user's identity, group memberships, and admin status.
//
// The bind flow:
//  1. If BindDN is configured, bind as the service account to search for the
//     user's DN. Otherwise, construct the user DN directly from the username
//     and BaseDN.
//  2. Bind as the user DN with the provided password to verify credentials.
//  3. If GroupFilter is configured, search for groups the user belongs to.
//  4. If AdminGroup/ReadOnlyGroup is configured, check group membership
//     (admin wins over readonly, ADR-0026 decision 6).
//  5. Return Claims with the mapped identity and role.
func (p *LDAPProvider) Bind(ctx context.Context, username, password string) (*Claims, error) {
	if username == "" || password == "" {
		return nil, newFailure(ProviderLDAP, ReasonBadCredentials,
			fmt.Errorf("auth: ldap bind: empty username or password"))
	}

	conn, err := p.pool.get(ctx)
	if err != nil {
		// Dial, StartTLS upgrade or pool failure: provider-side
		// infrastructure (O-2) — tls_handshake / provider_error, never
		// bad_credentials.
		return nil, classifyProviderConnErr(fmt.Errorf("auth: ldap bind: pool get: %w", err))
	}
	defer p.pool.put(conn)

	conn.SetTimeout(p.config.RequestTimeout)

	// Step 1: Resolve the user's DN.
	userDN, err := p.resolveUserDN(conn, username)
	if err != nil {
		if errors.Is(err, errNoLDAPEntries) {
			return nil, newFailure(ProviderLDAP, ReasonUserNotFound, err)
		}
		// Service-bind or search failure (misconfigured service account,
		// directory dropped mid-flow): the directory could not answer for
		// this user — provider_error.
		return nil, newFailure(ProviderLDAP, ReasonProviderError, err)
	}

	// Step 2: Bind as the user with the provided password.
	if err := conn.Bind(userDN, password); err != nil {
		// Map LDAP invalid credentials to our sentinel.
		if ldap.IsErrorWithCode(err, ldap.LDAPResultInvalidCredentials) {
			return nil, newFailure(ProviderLDAP, ReasonBadCredentials,
				fmt.Errorf("auth: ldap bind: invalid credentials for %q: %w", username, ErrInvalidCredentials))
		}
		return nil, newFailure(ProviderLDAP, ReasonProviderError,
			fmt.Errorf("auth: ldap bind: %w", ErrInvalidCredentials))
	}

	// Step 3: Determine the user ID attribute value from the search result.
	// If we performed a search (BindDN mode), we already have the user entry.
	// If we used direct DN construction, we search again bound as the user.
	userID := p.extractUserID(conn, username)

	// Step 4: Search for group memberships. A failure here is non-fatal: the
	// user identity returns without group memberships, and the role check
	// below runs against the empty group list.
	var groups []string
	if p.config.GroupFilter != "" {
		groups, err = p.searchGroups(conn, username, userDN)
		if err != nil {
			slog.Debug("auth: ldap group search failed; continuing without groups",
				slog.String("user", username),
				slog.String("error", err.Error()))
		}
	}

	// Step 5: Determine the group-inferred role (ADR-0026 decision 6):
	// admin_group membership > readonly_group membership > user.
	role := RoleUser
	if p.config.AdminGroup != "" && p.isGroupMember(conn, groups, p.config.AdminGroup) {
		role = RoleAdmin
	} else if p.config.ReadOnlyGroup != "" && p.isGroupMember(conn, groups, p.config.ReadOnlyGroup) {
		role = RoleReadOnlyAdmin
	}

	return &Claims{
		Name:       userID,
		Groups:     groups,
		Admin:      role == RoleAdmin,
		Role:       role,
		ProviderID: userDN,
	}, nil
}

// errNoLDAPEntries marks a user search the directory ANSWERED with zero
// matches — the user_not_found classification, distinct from search failures
// (provider_error). Unexported: only resolveUserDN produces it, only Bind
// matches it.
var errNoLDAPEntries = errors.New("auth: ldap search matched no entries")

// resolveUserDN finds the user's DN. When BindDN is configured, it binds as
// the service account and searches for the user. Otherwise, it constructs the
// DN directly from the username and BaseDN.
func (p *LDAPProvider) resolveUserDN(conn LDAPConn, username string) (string, error) {
	if p.config.BindDN != "" {
		// Bind as the service account first.
		if err := conn.Bind(p.config.BindDN, p.config.BindPassword); err != nil {
			return "", fmt.Errorf("service bind: %w", err)
		}
	}

	// Build the user search filter.
	filter := fmt.Sprintf(p.config.UserFilter, ldap.EscapeFilter(username))

	searchReq := ldap.NewSearchRequest(
		p.config.BaseDN,
		ldap.ScopeWholeSubtree,
		ldap.NeverDerefAliases,
		0, // size limit (0 = no limit)
		int(p.config.RequestTimeout.Seconds()),
		false, // types only
		filter,
		[]string{"dn", p.config.UserIDAttr},
		nil, // controls
	)

	result, err := conn.Search(searchReq)
	if err != nil {
		return "", fmt.Errorf("user search: %w", err)
	}

	if len(result.Entries) == 0 {
		return "", fmt.Errorf("user %q: %w", username, errNoLDAPEntries)
	}
	if len(result.Entries) > 1 {
		return "", fmt.Errorf("user %q matched multiple LDAP entries", username)
	}

	return result.Entries[0].DN, nil
}

// extractUserID returns the value of the configured UserIDAttr from the user
// entry. When BindDN is not configured, the user is already bound so we search
// for the user entry to get the attribute value.
func (p *LDAPProvider) extractUserID(conn LDAPConn, username string) string {
	// If BindDN was used, we already searched for the user entry during
	// resolveUserDN. However, we need to search again because the connection
	// was rebound as the user. We search for the user by DN to get the
	// attribute value.
	filter := fmt.Sprintf(p.config.UserFilter, ldap.EscapeFilter(username))

	searchReq := ldap.NewSearchRequest(
		p.config.BaseDN,
		ldap.ScopeWholeSubtree,
		ldap.NeverDerefAliases,
		1, // size limit
		int(p.config.RequestTimeout.Seconds()),
		false,
		filter,
		[]string{p.config.UserIDAttr},
		nil,
	)

	result, err := conn.Search(searchReq)
	if err != nil || len(result.Entries) == 0 {
		// Fallback to the username if the attribute is unavailable.
		return username
	}

	val := result.Entries[0].GetAttributeValue(p.config.UserIDAttr)
	if val == "" {
		return username
	}
	return val
}

// searchGroups searches for LDAP groups that contain the user. It uses the
// configured GroupFilter, substituting the username for the first %s, over
// GroupBaseDN (defaults to BaseDN).
func (p *LDAPProvider) searchGroups(conn LDAPConn, username, userDN string) ([]string, error) {
	_ = userDN // reserved for future use (e.g., member=%s with DN)

	filter := fmt.Sprintf(p.config.GroupFilter, ldap.EscapeFilter(username))

	searchReq := ldap.NewSearchRequest(
		p.config.GroupBaseDN,
		ldap.ScopeWholeSubtree,
		ldap.NeverDerefAliases,
		0, // no size limit for groups
		int(p.config.RequestTimeout.Seconds()),
		false,
		filter,
		[]string{p.config.GroupNameAttr},
		nil,
	)

	result, err := conn.Search(searchReq)
	if err != nil {
		return nil, fmt.Errorf("group search: %w", err)
	}

	groups := make([]string, 0, len(result.Entries))
	for _, entry := range result.Entries {
		name := entry.GetAttributeValue(p.config.GroupNameAttr)
		if name != "" {
			groups = append(groups, name)
		}
	}
	return groups, nil
}

// isGroupMember checks whether the user is a member of the given group (the
// generalized admin-group check, reused for the readonly group by T-212). It
// first checks the groups list from the group search; if not found there, it
// performs a direct check against the group DN.
func (p *LDAPProvider) isGroupMember(conn LDAPConn, groups []string, group string) bool {
	// Check if the group appears in the already-resolved group names.
	for _, g := range groups {
		if g == group || strings.EqualFold(g, group) {
			return true
		}
	}

	// If the group looks like a DN (contains "="), search for the group
	// entry and check membership by DN.
	if strings.Contains(group, "=") {
		groupName := p.groupNameFromDN(conn, group)
		// If we got a group name from the DN, check if it matches any group.
		if groupName != "" {
			for _, g := range groups {
				if strings.EqualFold(g, groupName) {
					return true
				}
			}
		}
	}

	return false
}

// groupNameFromDN looks up a group DN and returns its name attribute value.
// This is called when a configured group is a DN and we need to compare
// against group names from the group search.
func (p *LDAPProvider) groupNameFromDN(conn LDAPConn, groupDN string) string {
	searchReq := ldap.NewSearchRequest(
		groupDN,
		ldap.ScopeBaseObject,
		ldap.NeverDerefAliases,
		1,
		int(p.config.RequestTimeout.Seconds()),
		false,
		"(objectClass=*)",
		[]string{p.config.GroupNameAttr},
		nil,
	)

	result, err := conn.Search(searchReq)
	if err != nil || len(result.Entries) == 0 {
		return ""
	}
	return result.Entries[0].GetAttributeValue(p.config.GroupNameAttr)
}

// Close shuts down the LDAP connection pool. It is idempotent.
func (p *LDAPProvider) Close() {
	if p.pool != nil {
		p.pool.Close()
	}
}

// package-level helpers
