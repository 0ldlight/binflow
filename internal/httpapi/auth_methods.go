// Public auth-methods endpoint (M6, T-179, FR-56 surface): GET
// /binflow/api/v1/auth/methods answers {"password":true,"oidc":b,"ldap":b}.
//
// The endpoint is deliberately anonymous — it is product self-description in
// the /healthz posture, not configuration disclosure: the console's login
// page needs to know which entry points exist BEFORE any credential exists
// (T-158's opaque-redirect probe of /oidc/login is the interim workaround
// this replaces), and the three booleans reveal nothing an attacker could
// not learn by probing the login flow itself. The OIDC bit mirrors Deps.OIDC
// — the same wiring that decides whether the /oidc routes answer or 404
// (FR-54-AC6) — so the endpoint can never advertise a login entry point the
// instance does not actually serve.

package httpapi

import (
	"net/http"

	"github.com/lzwzzy/binflow/internal/auth"
)

// ldapWiring is the consumer-side facet of the authenticator the endpoint
// needs for the LDAP bit (defined here at the consumer, per project
// convention — the same discovery pattern as sessionRegistry and
// permissionViewer, performed per request because the facet lives on the
// auth.Service behind the injected Authenticator interface). auth.Service
// satisfies it via LDAPWired (T-179); a unit-test fake stays facet-less and
// the bit reads false.
type ldapWiring interface {
	LDAPWired() bool
}

// authMethodsBody is the response shape of GET /api/v1/auth/methods. password
// is constant true: local users always exist (the seeded admin at minimum),
// so the password login form is always offered.
type authMethodsBody struct {
	Password bool `json:"password"`
	OIDC     bool `json:"oidc"`
	LDAP     bool `json:"ldap"`
}

// handleAuthMethods serves GET /api/v1/auth/methods: which authentication
// entry points this instance offers. Anonymous by design (empty routeAuth in
// the router); every other verb on the path falls to the E-26 404.
func (s *Server) handleAuthMethods(w http.ResponseWriter, _ *http.Request) {
	ldap := false
	if lw, ok := s.deps.Auth.(ldapWiring); ok {
		ldap = lw.LDAPWired()
	}
	writeJSONBody(w, http.StatusOK, authMethodsBody{
		Password: true,
		OIDC:     s.deps.OIDC != nil,
		LDAP:     ldap,
	})
}

// principalSource renders a Principal's provider for the whoami body: the
// real service always fills Source; hand-built principals (fakes) collapse
// to local so the field never serializes empty.
func principalSource(p *auth.Principal) string {
	if p == nil || p.Source == "" {
		return string(auth.ProviderLocal)
	}
	return string(p.Source)
}
