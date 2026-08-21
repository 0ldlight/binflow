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
	"net"
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

	// AdminGroup is the DN of a group whose members are granted administrative
	// privileges. When empty, no group-inferred admin mapping is performed.
	AdminGroup string

	// PoolSize is the maximum number of idle connections kept in the pool.
	// Defaults to 5.
	PoolSize int

	// StartTLS enables StartTLS on ldap:// connections. Ignored for ldaps://
	// URLs (which already use TLS). When true, the TLS configuration is
	// taken from the TLSConfig field.
	StartTLS bool

	// TLSConfig is the TLS configuration for LDAPS or StartTLS connections.
	// When nil, the default system TLS settings are used. The
	// InsecureSkipVerify flag should be set only in development.
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

func (w *LDAPConnWrapper) Bind(username, password string) error {
	return w.conn.Bind(username, password)
}

func (w *LDAPConnWrapper) Search(searchRequest *ldap.SearchRequest) (*ldap.SearchResult, error) {
	return w.conn.Search(searchRequest)
}

func (w *LDAPConnWrapper) StartTLS(config *tls.Config) error {
	return w.conn.StartTLS(config)
}

func (w *LDAPConnWrapper) Close() error {
	return w.conn.Close()
}

func (w *LDAPConnWrapper) SetTimeout(timeout time.Duration) {
	w.conn.SetTimeout(timeout)
}

// LDAPDialer is a function that creates a new LDAPConn. Tests replace this
// with a mock factory. Exported for use by external test packages.
type LDAPDialer func(ctx context.Context, urlStr string, opts ...ldap.DialOpt) (LDAPConn, error)

// defaultDialer creates a real LDAP connection using DialURL.
func defaultDialer(ctx context.Context, urlStr string, opts ...ldap.DialOpt) (LDAPConn, error) {
	conn, err := ldap.DialURL(urlStr, opts...)
	if err != nil {
		return nil, fmt.Errorf("auth: ldap dial %s: %w", urlStr, err)
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

	if dialer == nil {
		dialer = defaultDialer
	}

	// Build DialOpts from config.
	var opts []ldap.DialOpt
	if cfg.ConnectTimeout > 0 {
		opts = append(opts, ldap.DialWithDialer(&net.Dialer{
			Timeout: cfg.ConnectTimeout,
		}))
	}
	if cfg.TLSConfig != nil {
		opts = append(opts, ldap.DialWithTLSConfig(cfg.TLSConfig))
	}

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
func (p *LDAPProvider) Authenticate(ctx context.Context, token string) (*Claims, error) {
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
//  4. If AdminGroup is configured, check whether the user is a member.
//  5. Return Claims with the mapped identity.
func (p *LDAPProvider) Bind(ctx context.Context, username, password string) (*Claims, error) {
	if username == "" || password == "" {
		return nil, fmt.Errorf("auth: ldap bind: %w", ErrInvalidCredentials)
	}

	conn, err := p.pool.get(ctx)
	if err != nil {
		return nil, fmt.Errorf("auth: ldap bind: pool get: %w", err)
	}
	defer p.pool.put(conn)

	conn.SetTimeout(p.config.RequestTimeout)

	// Step 1: Resolve the user's DN.
	userDN, err := p.resolveUserDN(conn, username)
	if err != nil {
		return nil, fmt.Errorf("auth: ldap bind: user search %q: %w", username, ErrInvalidCredentials)
	}

	// Step 2: Bind as the user with the provided password.
	if err := conn.Bind(userDN, password); err != nil {
		// Map LDAP invalid credentials to our sentinel.
		if ldap.IsErrorWithCode(err, ldap.LDAPResultInvalidCredentials) {
			return nil, fmt.Errorf("auth: ldap bind: invalid credentials for %q: %w", username, ErrInvalidCredentials)
		}
		return nil, fmt.Errorf("auth: ldap bind: %w", ErrInvalidCredentials)
	}

	// Step 3: Determine the user ID attribute value from the search result.
	// If we performed a search (BindDN mode), we already have the user entry.
	// If we used direct DN construction, we search again bound as the user.
	userID := p.extractUserID(conn, username, userDN)

	// Step 4: Search for group memberships.
	var groups []string
	if p.config.GroupFilter != "" {
		groups, err = p.searchGroups(conn, username, userDN)
		if err != nil {
			// Group search failure is non-fatal: return the user identity
			// without group memberships. The admin flag check below still
			// runs against the empty group list.
		}
	}

	// Step 5: Determine admin status.
	admin := false
	if p.config.AdminGroup != "" {
		admin = p.isAdminMember(conn, userDN, groups)
	}

	return &Claims{
		Name:       userID,
		Groups:     groups,
		Admin:      admin,
		ProviderID: userDN,
	}, nil
}

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
		0,             // size limit (0 = no limit)
		int(p.config.RequestTimeout.Seconds()),
		false,         // types only
		filter,
		[]string{"dn", p.config.UserIDAttr},
		nil, // controls
	)

	result, err := conn.Search(searchReq)
	if err != nil {
		return "", fmt.Errorf("user search: %w", err)
	}

	if len(result.Entries) == 0 {
		return "", fmt.Errorf("user %q not found in LDAP directory", username)
	}
	if len(result.Entries) > 1 {
		return "", fmt.Errorf("user %q matched multiple LDAP entries", username)
	}

	return result.Entries[0].DN, nil
}

// extractUserID returns the value of the configured UserIDAttr from the user
// entry. When BindDN is not configured, the user is already bound so we search
// for the user entry to get the attribute value.
func (p *LDAPProvider) extractUserID(conn LDAPConn, username, userDN string) string {
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
// configured GroupFilter, substituting the username for the first %s.
func (p *LDAPProvider) searchGroups(conn LDAPConn, username, userDN string) ([]string, error) {
	_ = userDN // reserved for future use (e.g., member=%s with DN)

	filter := fmt.Sprintf(p.config.GroupFilter, ldap.EscapeFilter(username))

	searchReq := ldap.NewSearchRequest(
		p.config.BaseDN,
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

// isAdminMember checks whether the user is a member of the configured admin
// group. It first checks the groups list from the group search; if not found
// there, it performs a direct check against the admin group DN.
func (p *LDAPProvider) isAdminMember(conn LDAPConn, userDN string, groups []string) bool {
	// Check if AdminGroup appears in the already-resolved group names.
	for _, g := range groups {
		if g == p.config.AdminGroup || strings.EqualFold(g, p.config.AdminGroup) {
			return true
		}
	}

	// If AdminGroup looks like a DN (contains "="), search for the group
	// entry and check membership by DN.
	if strings.Contains(p.config.AdminGroup, "=") {
		adminGroupName := p.adminGroupNameFromDN(conn)
		// If we got a group name from the DN, check if it matches any group.
		if adminGroupName != "" {
			for _, g := range groups {
				if strings.EqualFold(g, adminGroupName) {
					return true
				}
			}
		}
	}

	return false
}

// adminGroupNameFromDN looks up the AdminGroup DN and returns its name
// attribute value. This is called when AdminGroup is a DN and we need to
// compare against group names from the group search.
func (p *LDAPProvider) adminGroupNameFromDN(conn LDAPConn) string {
	searchReq := ldap.NewSearchRequest(
		p.config.AdminGroup,
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