// Authentication configuration sections (M11 T-305, ADR-0035 / FR-92): the
// three protocol descriptors stored in auth_configs (migration 015) and
// echoed by the /binflow/api/v1/admin/security/* REST plane.
//
// Field-name anchoring (auth-integration.md v2, the T-302 re-verified
// baseline — the single behavior anchor for this ticket):
//   - LDAP carries the §1.1/§1.2 LdapSetting/SearchPattern wire spellings
//     (key/enabled/ldapUrl/userDnPattern/search{searchFilter,searchBase,
//     searchSubTree,managerDn,managerPassword}/autoCreateUser/emailAttribute/
//     allowUserToAccessProfile/pagingSupportEnabled/ldapPoisoningProtection)
//     plus BinFlow operational extras with no Artifactory counterpart
//     (group filter family, admin/readonly group mappings, TLS posture,
//     pool size) — additive, camelCase, C-level.
//   - SAML carries the §3.1 Saml wire model verbatim (13 fields, including
//     the noAutoUserCreation NEGATED name kept as-is on the wire per §3.4's
//     naming-trap note; the Go side never renames it).
//   - OIDC is BinFlow's own C-level shape (issuer discovery model). The
//     Artifactory oauthSettings provider model (authUrl/tokenUrl/apiUrl per
//     provider, no issuer/JWKS) describes a different runtime; BinFlow's
//     five arms consume an issuer URL, so the section keeps the
//     binflow.yaml auth.oidc spellings it seeds from. Registered divergence
//     — see reports/agents/T-305.md.
//
// Secrets (LDAP managerPassword, OIDC client_secret) are stored in the doc
// as 'enc:v1:'-sealed text (the ADR-0012/ADR-0035 chain over
// BINFLOW_REMOTE_CREDENTIALS_KEY) and NEVER echo: the GET form renders the
// fixed 20-asterisk sentinel (auth-integration §1.6) when a secret is set
// and "" when it is not; PUT treats absent-or-sentinel as "keep" (ADR-0035
// decision 5 write-only mode), "" as clear, any other value as replace.

package auth

import (
	"crypto/x509"
	"encoding/json"
	"encoding/pem"
	"errors"
	"fmt"
	"net/url"
	"strings"
)

// Section names: the closed set of auth_configs rows (ADR-0035 decision 2).
// Wire spellings map at the REST layer (ldap → v1/admin/security/ldap, oidc
// → v1/admin/security/oauth, saml → v1/admin/security/saml/config).
const (
	SectionLDAP = "ldap"
	SectionOIDC = "oidc"
	SectionSAML = "saml"
)

// MaskedSecretEcho is the fixed sentinel a GET renders for a SET secret
// (auth-integration §1.6: the frontend's 20-asterisk placeholder; the exact
// spelling is anchored, the write-back semantics are ADR-0035's write-only
// keep — see the file comment).
const MaskedSecretEcho = "********************"

// ErrUnknownAuthConfigField is the strict-decode rejection (the config
// package decodeRaw posture carried into the DB descriptor: unknown keys
// refuse the write, they never silently drop).
var ErrUnknownAuthConfigField = errors.New("auth config: unknown field")

// ErrSectionUnknown names a section outside the closed set.
var ErrSectionUnknown = errors.New("auth config: unknown section")

// ---------------------------------------------------------------------------
// LDAP section
// ---------------------------------------------------------------------------

// LDAPSearchSection is the §1.2 SearchPattern sub-object. ManagerPassword is
// the SEALED stored form ("" = unset) — plaintext exists only inside a
// ConfigManager snapshot.
type LDAPSearchSection struct {
	SearchFilter    string `json:"searchFilter"`    // RFC 2254, {0} = username
	SearchBase      string `json:"searchBase"`      // relative to the ldapUrl base DN
	SearchSubTree   bool   `json:"searchSubTree"`   // default true
	ManagerDN       string `json:"managerDn"`       // "" = anonymous read-only bind
	ManagerPassword string `json:"managerPassword"` // sealed; sentinel on echo
}

// LDAPSection is the §1.1 LdapSetting wire model plus BinFlow's operational
// extras. Key is the setting id — BinFlow's single-section plane pins it to
// "ldap"; a PUT carrying any other key is refused.
type LDAPSection struct {
	Key                      string            `json:"key"`
	Enabled                  bool              `json:"enabled"`
	LDAPURL                  string            `json:"ldapUrl"`
	UserDNPattern            string            `json:"userDnPattern"`
	Search                   LDAPSearchSection `json:"search"`
	AutoCreateUser           bool              `json:"autoCreateUser"`
	EmailAttribute           string            `json:"emailAttribute"`
	AllowUserToAccessProfile bool              `json:"allowUserToAccessProfile"`
	PagingSupportEnabled     bool              `json:"pagingSupportEnabled"`
	LDAPPoisoningProtection  bool              `json:"ldapPoisoningProtection"`
	// BinFlow operational extras (C-level, no Artifactory counterpart).
	GroupFilter        string `json:"groupFilter"`
	GroupBaseDN        string `json:"groupBaseDn"`
	GroupNameAttribute string `json:"groupNameAttribute"`
	AdminGroup         string `json:"adminGroup"`
	ReadOnlyGroup      string `json:"readOnlyGroup"`
	StartTLS           bool   `json:"startTls"`
	SkipTLSVerify      bool   `json:"skipTlsVerify"`
	PoolSize           int    `json:"poolSize"`
}

// ldapFieldSet is the strict key closure of the LDAP doc (top level).
var ldapFieldSet = map[string]bool{
	"key": true, "enabled": true, "ldapUrl": true, "userDnPattern": true,
	"search": true, "autoCreateUser": true, "emailAttribute": true,
	"allowUserToAccessProfile": true, "pagingSupportEnabled": true,
	"ldapPoisoningProtection": true,
	"groupFilter":             true, "groupBaseDn": true, "groupNameAttribute": true,
	"adminGroup": true, "readOnlyGroup": true, "startTls": true,
	"skipTlsVerify": true, "poolSize": true,
}

// ldapSearchFieldSet is the strict key closure of the search sub-object.
var ldapSearchFieldSet = map[string]bool{
	"searchFilter": true, "searchBase": true, "searchSubTree": true,
	"managerDn": true, "managerPassword": true,
}

// defaultLDAPSection is the defaults-applied zero section (§1.1/§1.2 default
// column): every boolean with a true default reads true on an unset doc, and
// the optional fields carry their documented defaults so the shape equals
// what the decoder produces from {}.
func defaultLDAPSection() *LDAPSection {
	return &LDAPSection{
		Key:                     SectionLDAP,
		Enabled:                 true,
		AutoCreateUser:          true,
		EmailAttribute:          "mail",
		PagingSupportEnabled:    true,
		LDAPPoisoningProtection: true,
		Search: LDAPSearchSection{
			SearchSubTree: true,
		},
		GroupNameAttribute: "cn",
		PoolSize:           5,
	}
}

// decodeLDAPSection strict-decodes one LDAP doc with defaults applied. The
// secret stays in its stored (sealed) form.
func decodeLDAPSection(doc []byte) (*LDAPSection, error) {
	m, err := strictMap(doc, "ldap")
	if err != nil {
		return nil, err
	}
	s := defaultLDAPSection()
	for _, f := range []struct {
		key string
		dst *string
	}{
		{"key", &s.Key}, {"ldapUrl", &s.LDAPURL}, {"userDnPattern", &s.UserDNPattern},
		{"emailAttribute", &s.EmailAttribute}, {"groupFilter", &s.GroupFilter},
		{"groupBaseDn", &s.GroupBaseDN}, {"groupNameAttribute", &s.GroupNameAttribute},
		{"adminGroup", &s.AdminGroup}, {"readOnlyGroup", &s.ReadOnlyGroup},
	} {
		if v, ok, err := mapString(m, f.key); err != nil {
			return nil, err
		} else if ok {
			*f.dst = v
		}
	}
	for _, f := range []struct {
		key string
		dst *bool
	}{
		{"enabled", &s.Enabled}, {"autoCreateUser", &s.AutoCreateUser},
		{"allowUserToAccessProfile", &s.AllowUserToAccessProfile},
		{"pagingSupportEnabled", &s.PagingSupportEnabled},
		{"ldapPoisoningProtection", &s.LDAPPoisoningProtection},
		{"startTls", &s.StartTLS}, {"skipTlsVerify", &s.SkipTLSVerify},
	} {
		if v, ok, err := mapBool(m, f.key); err != nil {
			return nil, err
		} else if ok {
			*f.dst = v
		}
	}
	if v, ok, err := mapInt(m, "poolSize"); err != nil {
		return nil, err
	} else if ok {
		s.PoolSize = v
	}
	// Optional fields with a documented default normalize "" / non-positive
	// to the default (the runtime LDAPConfig.defaults posture): an explicit
	// empty spelling is indistinguishable from "unset" on the wire, and the
	// seed/defaults comparison must not see a difference either. Fields
	// where "" is MEANINGFUL (managerDn = anonymous bind, userDnPattern =
	// the AD recommendation, filters, the secret itself) keep "" verbatim.
	if s.EmailAttribute == "" {
		s.EmailAttribute = "mail"
	}
	if s.GroupNameAttribute == "" {
		s.GroupNameAttribute = "cn"
	}
	if s.PoolSize <= 0 {
		s.PoolSize = 5
	}
	if raw, ok := m["search"]; ok {
		sm, err := strictMap(raw, "ldap.search")
		if err != nil {
			return nil, err
		}
		for _, f := range []struct {
			key string
			dst *string
		}{
			{"searchFilter", &s.Search.SearchFilter}, {"searchBase", &s.Search.SearchBase},
			{"managerDn", &s.Search.ManagerDN}, {"managerPassword", &s.Search.ManagerPassword},
		} {
			if v, ok, err := mapString(sm, f.key); err != nil {
				return nil, err
			} else if ok {
				*f.dst = v
			}
		}
		if v, ok, err := mapBool(sm, "searchSubTree"); err != nil {
			return nil, err
		} else if ok {
			s.Search.SearchSubTree = v
		}
	}
	return s, nil
}

// masked renders the GET echo: a set secret collapses to the sentinel, an
// unset one stays "".
func (s *LDAPSection) masked() *LDAPSection {
	out := *s
	out.Search = s.Search
	if out.Search.ManagerPassword != "" {
		out.Search.ManagerPassword = MaskedSecretEcho
	}
	return &out
}

// ---------------------------------------------------------------------------
// OIDC section (BinFlow C-level shape — see the file comment)
// ---------------------------------------------------------------------------

// OIDCSection mirrors the binflow.yaml auth.oidc section it seeds from.
// ClientSecret is the SEALED stored form ("" = unset).
type OIDCSection struct {
	Enabled         bool     `json:"enabled"`
	IssuerURL       string   `json:"issuer_url"`
	ClientID        string   `json:"client_id"`
	ClientSecret    string   `json:"client_secret"` //nolint:gosec // G117: carries only the enc:v1-sealed value or the masked sentinel — plaintext exists inside a snapshot, never on this wire
	RedirectURL     string   `json:"redirect_url"`
	Scopes          []string `json:"scopes"`
	UserClaim       string   `json:"user_claim"`
	GroupClaim      string   `json:"group_claim"`
	AdminGroup      string   `json:"admin_group"`
	ReadOnlyGroup   string   `json:"readonly_group"`
	AutoCreateUsers bool     `json:"auto_create_users"`
}

var oidcFieldSet = map[string]bool{
	"enabled": true, "issuer_url": true, "client_id": true,
	"client_secret": true, "redirect_url": true, "scopes": true,
	"user_claim": true, "group_claim": true, "admin_group": true,
	"readonly_group": true, "auto_create_users": true,
}

// defaultOIDCSection applies the M6 defaults — auto_create_users defaults
// TRUE because that is the wired M6 posture (NewFromStore always arms the
// creator), so a seeded or unset section changes nothing.
func defaultOIDCSection() *OIDCSection {
	return &OIDCSection{
		Scopes:          []string{"openid", "profile", "email"},
		UserClaim:       "preferred_username",
		GroupClaim:      "groups",
		AutoCreateUsers: true,
	}
}

func decodeOIDCSection(doc []byte) (*OIDCSection, error) {
	m, err := strictMap(doc, "oidc")
	if err != nil {
		return nil, err
	}
	s := defaultOIDCSection()
	for _, f := range []struct {
		key string
		dst *string
	}{
		{"issuer_url", &s.IssuerURL}, {"client_id", &s.ClientID},
		{"client_secret", &s.ClientSecret}, {"redirect_url", &s.RedirectURL},
		{"user_claim", &s.UserClaim}, {"group_claim", &s.GroupClaim},
		{"admin_group", &s.AdminGroup}, {"readonly_group", &s.ReadOnlyGroup},
	} {
		if v, ok, err := mapString(m, f.key); err != nil {
			return nil, err
		} else if ok {
			*f.dst = v
		}
	}
	for _, f := range []struct {
		key string
		dst *bool
	}{
		{"enabled", &s.Enabled}, {"auto_create_users", &s.AutoCreateUsers},
	} {
		if v, ok, err := mapBool(m, f.key); err != nil {
			return nil, err
		} else if ok {
			*f.dst = v
		}
	}
	if raw, ok := m["scopes"]; ok {
		var scopes []string
		if len(raw) > 0 && string(raw) != "null" {
			if err := json.Unmarshal(raw, &scopes); err != nil {
				return nil, fmt.Errorf("auth config: oidc scopes: %w", err)
			}
		}
		if len(scopes) > 0 {
			s.Scopes = scopes
		}
	}
	// Empty claim spellings normalize to their defaults (same posture as
	// the LDAP optional fields above; the secret keeps "" verbatim — it is
	// the clear operation).
	if s.UserClaim == "" {
		s.UserClaim = "preferred_username"
	}
	if s.GroupClaim == "" {
		s.GroupClaim = "groups"
	}
	return s, nil
}

func (s *OIDCSection) masked() *OIDCSection {
	out := *s
	if out.ClientSecret != "" {
		out.ClientSecret = MaskedSecretEcho
	}
	return &out
}

// ---------------------------------------------------------------------------
// SAML section (§3.1 wire model verbatim)
// ---------------------------------------------------------------------------

// SAMLSection is the Saml UI wire model. The negated noAutoUserCreation name
// is kept as anchored (§3.4 naming trap: wire carries the negation, default
// true = do NOT auto-create); nothing on the Go side renames or flips it.
type SAMLSection struct {
	EnableIntegration         bool   `json:"enableIntegration"`
	LoginURL                  string `json:"loginUrl"`
	LogoutURL                 string `json:"logoutUrl"`
	ServiceProviderName       string `json:"serviceProviderName"`
	Certificate               string `json:"certificate"`
	UseEncryptedAssertion     bool   `json:"useEncryptedAssertion"`
	SyncGroups                bool   `json:"syncGroups"`
	GroupAttribute            string `json:"groupAttribute"`
	EmailAttribute            string `json:"emailAttribute"`
	NoAutoUserCreation        bool   `json:"noAutoUserCreation"`
	AllowUserToAccessProfile  bool   `json:"allowUserToAccessProfile"`
	AutoRedirect              bool   `json:"autoRedirect"`
	VerifyAudienceRestriction bool   `json:"verifyAudienceRestriction"`
}

var samlFieldSet = map[string]bool{
	"enableIntegration": true, "loginUrl": true, "logoutUrl": true,
	"serviceProviderName": true, "certificate": true,
	"useEncryptedAssertion": true, "syncGroups": true, "groupAttribute": true,
	"emailAttribute": true, "noAutoUserCreation": true,
	"allowUserToAccessProfile": true, "autoRedirect": true,
	"verifyAudienceRestriction": true,
}

func defaultSAMLSection() *SAMLSection {
	return &SAMLSection{
		NoAutoUserCreation:        true,
		VerifyAudienceRestriction: true,
	}
}

func decodeSAMLSection(doc []byte) (*SAMLSection, error) {
	m, err := strictMap(doc, "saml")
	if err != nil {
		return nil, err
	}
	s := defaultSAMLSection()
	for _, f := range []struct {
		key string
		dst *string
	}{
		{"loginUrl", &s.LoginURL}, {"logoutUrl", &s.LogoutURL},
		{"serviceProviderName", &s.ServiceProviderName}, {"certificate", &s.Certificate},
		{"groupAttribute", &s.GroupAttribute}, {"emailAttribute", &s.EmailAttribute},
	} {
		if v, ok, err := mapString(m, f.key); err != nil {
			return nil, err
		} else if ok {
			*f.dst = v
		}
	}
	for _, f := range []struct {
		key string
		dst *bool
	}{
		{"enableIntegration", &s.EnableIntegration},
		{"useEncryptedAssertion", &s.UseEncryptedAssertion},
		{"syncGroups", &s.SyncGroups},
		{"noAutoUserCreation", &s.NoAutoUserCreation},
		{"allowUserToAccessProfile", &s.AllowUserToAccessProfile},
		{"autoRedirect", &s.AutoRedirect},
		{"verifyAudienceRestriction", &s.VerifyAudienceRestriction},
	} {
		if v, ok, err := mapBool(m, f.key); err != nil {
			return nil, err
		} else if ok {
			*f.dst = v
		}
	}
	return s, nil
}

func (s *SAMLSection) masked() *SAMLSection {
	out := *s // the IdP certificate is public material — no secret on this plane
	return &out
}

// ---------------------------------------------------------------------------
// Generic section plumbing
// ---------------------------------------------------------------------------

// ValidateAuthSection runs the write-path validation of one section doc.
// Validation is posture-independent of secrets: the merge with the stored
// secret happens before this runs (the merged doc is what gets checked).
func ValidateAuthSection(section string, doc []byte) error {
	switch section {
	case SectionLDAP:
		s, err := decodeLDAPSection(doc)
		if err != nil {
			return err
		}
		return s.validate()
	case SectionOIDC:
		s, err := decodeOIDCSection(doc)
		if err != nil {
			return err
		}
		return s.validate()
	case SectionSAML:
		s, err := decodeSAMLSection(doc)
		if err != nil {
			return err
		}
		return s.validate()
	default:
		return fmt.Errorf("%w: %q", ErrSectionUnknown, section)
	}
}

func (s *LDAPSection) validate() error {
	if s.Key != SectionLDAP {
		return fmt.Errorf("auth config: ldap key must be %q, got %q", SectionLDAP, s.Key)
	}
	if !s.Enabled {
		return nil // a disabled section only needs to round-trip
	}
	if s.LDAPURL == "" {
		return errors.New("auth config: ldap ldapUrl is required when enabled")
	}
	u, err := url.Parse(s.LDAPURL)
	if err != nil || u.Host == "" {
		return fmt.Errorf("auth config: ldap ldapUrl %q: must be an ldap(s) URL like ldap://host:389/dc=example,dc=com", s.LDAPURL)
	}
	if scheme := strings.ToLower(u.Scheme); scheme != "ldap" && scheme != "ldaps" {
		return fmt.Errorf("auth config: ldap ldapUrl %q: scheme must be ldap or ldaps, got %q", s.LDAPURL, u.Scheme)
	}
	if strings.Trim(u.Path, "/") == "" {
		return fmt.Errorf("auth config: ldap ldapUrl %q: the URL path is the search base DN and is required (e.g. ldap://host/dc=example,dc=com)", s.LDAPURL)
	}
	for field, val := range map[string]string{
		"userDnPattern": s.UserDNPattern, "search.searchFilter": s.Search.SearchFilter,
	} {
		if val != "" && !strings.Contains(val, "{0}") {
			return fmt.Errorf("auth config: ldap %s %q: must carry the {0} username placeholder", field, val)
		}
	}
	return nil
}

func (s *OIDCSection) validate() error {
	if !s.Enabled {
		return nil
	}
	if s.IssuerURL == "" {
		return errors.New("auth config: oidc issuer_url is required when enabled")
	}
	if err := validateHTTPURL(s.IssuerURL, "oidc issuer_url"); err != nil {
		return err
	}
	if s.ClientID == "" {
		return errors.New("auth config: oidc client_id is required when enabled")
	}
	if s.RedirectURL == "" {
		return errors.New("auth config: oidc redirect_url is required when enabled")
	}
	return nil
}

func (s *SAMLSection) validate() error {
	if !s.EnableIntegration {
		return nil
	}
	if s.LoginURL == "" {
		return errors.New("auth config: saml loginUrl is required when enableIntegration is true")
	}
	if err := validateHTTPURL(s.LoginURL, "saml loginUrl"); err != nil {
		return err
	}
	if s.ServiceProviderName == "" {
		return errors.New("auth config: saml serviceProviderName is required when enableIntegration is true")
	}
	if s.Certificate == "" {
		return errors.New("auth config: saml certificate is required when enableIntegration is true")
	}
	// §3.3 flow 1: enable forces the certificate through the parser — a
	// malformed PEM is a write rejection, never a runtime surprise.
	if _, err := parseCertificatePEM(s.Certificate); err != nil {
		return err
	}
	return nil
}

// validateHTTPURL asserts the http(s) shape the OIDC/SAML planes require.
func validateHTTPURL(raw, field string) error {
	u, err := url.Parse(raw)
	if err != nil || u.Host == "" {
		return fmt.Errorf("auth config: %s %q: must be an http(s) URL", field, raw)
	}
	if scheme := strings.ToLower(u.Scheme); scheme != "http" && scheme != "https" {
		return fmt.Errorf("auth config: %s %q: scheme must be http or https, got %q", field, raw, u.Scheme)
	}
	return nil
}

// parseCertificatePEM decodes one PEM X.509 certificate (the IdP's public
// key material — SAML's only certificate consumer in M11 is validation; the
// SP assertion-consumption runtime is out of FR-92 scope, ADR-0035 §7).
func parseCertificatePEM(pemText string) (*x509.Certificate, error) {
	block, _ := pem.Decode([]byte(pemText))
	if block == nil || block.Type != "CERTIFICATE" {
		return nil, errors.New("auth config: saml certificate: not a PEM CERTIFICATE block")
	}
	cert, err := x509.ParseCertificate(block.Bytes)
	if err != nil {
		return nil, fmt.Errorf("auth config: saml certificate: %w", err)
	}
	return cert, nil
}

// DefaultAuthSectionDoc renders the GET body of a section with NO stored
// row: SAML answers the anchored empty object {} (auth-integration §3.2 —
// the frontend tolerates and pre-defaults the form); LDAP/OIDC answer their
// defaults-applied zero docs (no anchored single-section empty shape
// exists — BinFlow C-level, registered in the T-305 report).
func DefaultAuthSectionDoc(section string) json.RawMessage {
	switch section {
	case SectionSAML:
		return json.RawMessage(`{}`)
	case SectionLDAP:
		b, _ := json.Marshal(defaultLDAPSection().masked())
		return b
	case SectionOIDC:
		// G117: the section struct carries a client_secret field, but the
		// marshaled value here is always "" (the default doc of an unset
		// section — the masked echo path renders the sentinel, never
		// plaintext).
		b, _ := json.Marshal(defaultOIDCSection().masked()) //nolint:gosec // see above
		return b
	default:
		return nil
	}
}

// DecodeAuthSection strict-decodes a stored or submitted doc into its
// section value (masked forms rejected: the sentinel is never valid input —
// the merge layer consumes it before decode).
func DecodeAuthSection(section string, doc []byte) (any, error) {
	switch section {
	case SectionLDAP:
		return decodeLDAPSection(doc)
	case SectionOIDC:
		return decodeOIDCSection(doc)
	case SectionSAML:
		return decodeSAMLSection(doc)
	default:
		return nil, fmt.Errorf("%w: %q", ErrSectionUnknown, section)
	}
}

// marshalSectionDoc renders the canonical stored form.
func marshalSectionDoc(v any) ([]byte, error) {
	b, err := json.Marshal(v)
	if err != nil {
		return nil, fmt.Errorf("auth config: encoding section doc: %w", err)
	}
	return b, nil
}

// ---------------------------------------------------------------------------
// strict map extractors
// ---------------------------------------------------------------------------

// strictMap unmarshals one JSON object and rejects unknown keys against the
// section's closure (the decodeRaw posture: unknown keys refuse the write).
func strictMap(doc []byte, what string) (map[string]json.RawMessage, error) {
	trimmed := strings.TrimSpace(string(doc))
	if trimmed == "" || trimmed == "null" {
		return map[string]json.RawMessage{}, nil
	}
	var m map[string]json.RawMessage
	if err := json.Unmarshal(doc, &m); err != nil {
		return nil, fmt.Errorf("auth config: %s: not a JSON object: %w", what, err)
	}
	var allowed map[string]bool
	switch what {
	case "ldap":
		allowed = ldapFieldSet
	case "ldap.search":
		allowed = ldapSearchFieldSet
	case "oidc":
		allowed = oidcFieldSet
	case "saml":
		allowed = samlFieldSet
	default:
		return nil, fmt.Errorf("%w: %s", ErrSectionUnknown, what)
	}
	for k := range m {
		if !allowed[k] {
			return nil, fmt.Errorf("%w %q in %s auth config", ErrUnknownAuthConfigField, k, what)
		}
	}
	return m, nil
}

func mapString(m map[string]json.RawMessage, key string) (string, bool, error) {
	raw, ok := m[key]
	if !ok || string(raw) == "null" {
		return "", false, nil
	}
	var v string
	if err := json.Unmarshal(raw, &v); err != nil {
		return "", false, fmt.Errorf("auth config: field %q: want a string: %w", key, err)
	}
	return v, true, nil
}

func mapBool(m map[string]json.RawMessage, key string) (bool, bool, error) {
	raw, ok := m[key]
	if !ok || string(raw) == "null" {
		return false, false, nil
	}
	var v bool
	if err := json.Unmarshal(raw, &v); err != nil {
		return false, false, fmt.Errorf("auth config: field %q: want a boolean: %w", key, err)
	}
	return v, true, nil
}

func mapInt(m map[string]json.RawMessage, key string) (int, bool, error) {
	raw, ok := m[key]
	if !ok || string(raw) == "null" {
		return 0, false, nil
	}
	var v int
	if err := json.Unmarshal(raw, &v); err != nil {
		return 0, false, fmt.Errorf("auth config: field %q: want an integer: %w", key, err)
	}
	return v, true, nil
}
