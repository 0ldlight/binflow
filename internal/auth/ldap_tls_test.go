// T-186 acceptance surface (T-174 defects D5/D6): the LDAP TLS posture.
//
// Two layers of evidence:
//
//   - TestLDAPProviderTLSPostures drives LDAPProvider over a REAL wire-level
//     LDAP server (BER over TCP, built on the asn1-ber module go-ldap itself
//     depends on — no new module enters the graph) that can listen in
//     plaintext or TLS and answers the StartTLS extended operation. This is
//     the only layer where the actual crypto plumbing (DialURL implicit TLS,
//     StartTLS upgrade, certificate verification) executes; the
//     LDAPConn-interface mocks of ldap_test.go cannot see any of it. The
//     osixia environment gotchas recorded in the T-174 QA report §1/§2
//     (olcTLSVerifyClient demand breaking Go clients, ACLs hiding groups)
//     are container-side problems and do not apply to this in-process
//     server — the Go TLS handshake here exercises the same client code
//     path the container runs.
//   - TestLDAPProviderStartTLSWiring / TestLDAPProviderGroupBaseDN* /
//     TestLDAPProviderTLSConfigLogs pin the dialer-level wiring with the
//     interface mock: which URLs trigger the upgrade, what a failed upgrade
//     does to the connection, and the WARN hygiene of the two new keys.

package auth_test

import (
	"bytes"
	"context"
	"crypto/ecdsa"
	"crypto/elliptic"
	"crypto/rand"
	"crypto/tls"
	"crypto/x509"
	"crypto/x509/pkix"
	"errors"
	"log/slog"
	"math/big"
	"net"
	"strings"
	"sync"
	"testing"
	"time"

	ber "github.com/go-asn1-ber/asn1-ber"

	"github.com/lzwzzy/binflow/internal/auth"
)

// ---------------------------------------------------------------------------
// Wire-level TLS-capable LDAP test server
// ---------------------------------------------------------------------------

// ldapServerStats counts what the wire server observed, under a mutex (one
// handler goroutine per connection).
type ldapServerStats struct {
	mu              sync.Mutex
	upgrades        int      // completed StartTLS handshakes
	refusedStartTLS int      // StartTLS requests answered protocolError
	extOnTLS        int      // StartTLS requested on an already encrypted conn (must stay 0)
	binds           []string // DNs of accepted binds, suffixed "@tls"/"@plaintext"
	bindFails       int      // binds answered invalidCredentials
	searchBases     []string // base DN of every search request
}

// tlsLDAPServer speaks just enough LDAP over BER for the provider's
// bind-search flow: simple bind, search (entries + done), unbind, and the
// StartTLS extended operation (RFC 4513). One instance owns a plaintext and
// a TLS listener on separate ports, so a table row picks its posture via
// the URL it dials.
type tlsLDAPServer struct {
	t *testing.T

	entries   map[string]map[string][]string // DN -> attributes (users and groups)
	passwords map[string]string              // DN -> bind password

	tlsConf *tls.Config
	ca      *x509.Certificate // the CA clients may put in RootCAs

	// startTLSSupport=false answers the StartTLS extended request with
	// protocolError (a directory that refuses the upgrade).
	startTLSSupport bool

	plainLn net.Listener
	tlsLn   net.Listener

	stats ldapServerStats
}

// newTLSLDAPServer builds the server with the T-174-shaped directory: a
// service account, one user under ou=people, one group under ou=groups (so
// group_base_dn is a genuinely different subtree).
func newTLSLDAPServer(t *testing.T, startTLSSupport bool) *tlsLDAPServer {
	s := &tlsLDAPServer{
		t:       t,
		entries: map[string]map[string][]string{},
		passwords: map[string]string{
			"cn=admin,dc=example,dc=org":           "admin-secret",
			"uid=jdoe,ou=people,dc=example,dc=org": "jdoe-secret",
		},
		startTLSSupport: startTLSSupport,
	}
	s.entries["cn=admin,dc=example,dc=org"] = map[string][]string{
		"uid": {"admin"}, "objectClass": {"inetOrgPerson"},
	}
	s.entries["uid=jdoe,ou=people,dc=example,dc=org"] = map[string][]string{
		"uid": {"jdoe"}, "objectClass": {"inetOrgPerson"},
	}
	s.entries["cn=developers,ou=groups,dc=example,dc=org"] = map[string][]string{
		"cn": {"developers"}, "objectClass": {"groupOfNames"}, "memberUid": {"jdoe"},
	}

	// Test CA + server certificate with SAN localhost + 127.0.0.1: the
	// provider derives the TLS ServerName from the URL host, and the tests
	// dial 127.0.0.1, so the IP SAN is the one that matters — a DNS-only
	// SAN is exactly the mismatch real deployments hit.
	caKey := genKey(t)
	caTemplate := &x509.Certificate{
		SerialNumber:          big.NewInt(1),
		Subject:               pkix.Name{CommonName: "T-186 Test CA"},
		NotBefore:             time.Now().Add(-time.Hour),
		NotAfter:              time.Now().Add(time.Hour),
		IsCA:                  true,
		KeyUsage:              x509.KeyUsageCertSign | x509.KeyUsageDigitalSignature,
		BasicConstraintsValid: true,
	}
	caDER, err := x509.CreateCertificate(rand.Reader, caTemplate, caTemplate, &caKey.PublicKey, caKey)
	if err != nil {
		t.Fatalf("create CA cert: %v", err)
	}
	s.ca, err = x509.ParseCertificate(caDER)
	if err != nil {
		t.Fatalf("parse CA cert: %v", err)
	}

	serverKey := genKey(t)
	serverTemplate := &x509.Certificate{
		SerialNumber: big.NewInt(2),
		Subject:      pkix.Name{CommonName: "localhost"},
		NotBefore:    time.Now().Add(-time.Hour),
		NotAfter:     time.Now().Add(time.Hour),
		KeyUsage:     x509.KeyUsageDigitalSignature | x509.KeyUsageKeyEncipherment,
		ExtKeyUsage:  []x509.ExtKeyUsage{x509.ExtKeyUsageServerAuth},
		DNSNames:     []string{"localhost"},
		IPAddresses:  []net.IP{net.ParseIP("127.0.0.1")},
	}
	serverDER, err := x509.CreateCertificate(rand.Reader, serverTemplate, s.ca, &serverKey.PublicKey, caKey)
	if err != nil {
		t.Fatalf("create server cert: %v", err)
	}
	s.tlsConf = &tls.Config{
		Certificates: []tls.Certificate{{
			Certificate: [][]byte{serverDER},
			PrivateKey:  serverKey,
		}},
		MinVersion: tls.VersionTLS12,
	}

	s.plainLn = s.listen(func() (net.Listener, error) { return net.Listen("tcp", "127.0.0.1:0") })
	s.tlsLn = s.listen(func() (net.Listener, error) {
		return tls.Listen("tcp", "127.0.0.1:0", s.tlsConf)
	})
	t.Cleanup(func() {
		_ = s.plainLn.Close()
		_ = s.tlsLn.Close()
	})
	return s
}

func genKey(t *testing.T) *ecdsa.PrivateKey {
	key, err := ecdsa.GenerateKey(elliptic.P256(), rand.Reader)
	if err != nil {
		t.Fatalf("generate key: %v", err)
	}
	return key
}

func (s *tlsLDAPServer) listen(open func() (net.Listener, error)) net.Listener {
	ln, err := open()
	if err != nil {
		s.t.Fatalf("listen: %v", err)
	}
	go func() {
		for {
			conn, err := ln.Accept()
			if err != nil {
				return
			}
			go s.serveConn(conn)
		}
	}()
	return ln
}

func (s *tlsLDAPServer) plainURL() string { return "ldap://" + s.plainLn.Addr().String() }
func (s *tlsLDAPServer) tlsURL() string   { return "ldaps://" + s.tlsLn.Addr().String() }

// trustedRoots returns a pool containing the server's CA — the client-side
// "believes the CA" posture of the H35 legs.
func (s *tlsLDAPServer) trustedRoots() *x509.CertPool {
	pool := x509.NewCertPool()
	pool.AddCert(s.ca)
	return pool
}

// serveConn answers one connection's request stream. encrypted marks conns
// that arrived through the TLS listener or completed a StartTLS upgrade.
func (s *tlsLDAPServer) serveConn(conn net.Conn) {
	// Closure defer: the StartTLS upgrade reassigns conn to the TLS wrapper,
	// and the close must hit whatever connection is current when the loop
	// ends (a deferred method call would pin the original plaintext conn).
	defer func() { _ = conn.Close() }()
	encrypted := false
	for {
		pkt, err := ber.ReadPacket(conn)
		if err != nil {
			return // closed, EOF, or a failed TLS handshake surfacing as a read error
		}
		if len(pkt.Children) < 2 {
			return
		}
		msgID := ber.NewInteger(ber.ClassUniversal, ber.TypePrimitive, ber.TagInteger,
			pkt.Children[0].Value.(int64), "Message ID")
		op := pkt.Children[1]
		if op.ClassType != ber.ClassApplication || op.TagType != ber.TypeConstructed {
			return
		}
		switch op.Tag {
		case 0: // bindRequest
			s.handleBind(conn, msgID, op, encrypted)
		case 2: // unbindRequest — the client is going away
			return
		case 3: // searchRequest
			s.handleSearch(conn, msgID, op)
		case 23: // extendedRequest (StartTLS)
			if encrypted {
				// go-ldap refuses this client-side ("already encrypted"); a
				// request arriving here means the ldaps/start_tls exclusion
				// regressed.
				s.stats.mu.Lock()
				s.stats.extOnTLS++
				s.stats.mu.Unlock()
			}
			oid := ""
			if len(op.Children) > 0 {
				oid = op.Children[0].Data.String()
			}
			if oid != "1.3.6.1.4.1.1466.20037" || !s.startTLSSupport {
				// protocolError: the directory refuses the upgrade. The
				// client must give up, not continue in plaintext.
				s.stats.mu.Lock()
				s.stats.refusedStartTLS++
				s.stats.mu.Unlock()
				_ = s.writeResponse(conn, msgID, ldapResultOp(24, 2))
				continue
			}
			if err := s.writeResponse(conn, msgID, ldapResultOp(24, 0)); err != nil {
				return
			}
			tlsConn := tls.Server(conn, s.tlsConf)
			if err := tlsConn.Handshake(); err != nil {
				return
			}
			conn = tlsConn
			encrypted = true
			s.stats.mu.Lock()
			s.stats.upgrades++
			s.stats.mu.Unlock()
		default:
			_ = s.writeResponse(conn, msgID, ldapResultOp(op.Tag, 2))
		}
	}
}

func (s *tlsLDAPServer) handleBind(conn net.Conn, msgID *ber.Packet, op *ber.Packet, encrypted bool) {
	// bindRequest ::= [APPLICATION 0] SEQUENCE { version, name, authentication }
	if len(op.Children) < 3 {
		_ = s.writeResponse(conn, msgID, ldapResultOp(1, 2))
		return
	}
	dn := op.Children[1].Data.String()
	password := op.Children[2].Data.String()
	want, ok := s.passwords[dn]
	if !ok || want != password {
		s.stats.mu.Lock()
		s.stats.bindFails++
		s.stats.mu.Unlock()
		_ = s.writeResponse(conn, msgID, ldapResultOp(1, 49)) // invalidCredentials
		return
	}
	marker := "plaintext"
	if encrypted {
		marker = "tls"
	}
	s.stats.mu.Lock()
	s.stats.binds = append(s.stats.binds, dn+"@"+marker)
	s.stats.mu.Unlock()
	_ = s.writeResponse(conn, msgID, ldapResultOp(1, 0))
}

func (s *tlsLDAPServer) handleSearch(conn net.Conn, msgID *ber.Packet, op *ber.Packet) {
	if len(op.Children) < 8 {
		_ = s.writeResponse(conn, msgID, ldapResultOp(5, 2))
		return
	}
	base := op.Children[0].Data.String()
	filter := filterString(op.Children[6])
	var requested []string
	for _, attr := range op.Children[7].Children {
		requested = append(requested, attr.Data.String())
	}

	s.stats.mu.Lock()
	s.stats.searchBases = append(s.stats.searchBases, base)
	s.stats.mu.Unlock()

	for dn, attrs := range s.entries {
		if !strings.HasSuffix(dn, base) {
			continue
		}
		if !matchesFilter(dn, attrs, filter) {
			continue
		}
		if err := s.writeResponse(conn, msgID, searchEntryOp(dn, attrs, requested)); err != nil {
			return
		}
	}
	_ = s.writeResponse(conn, msgID, ldapResultOp(5, 0))
}

func (s *tlsLDAPServer) writeResponse(conn net.Conn, msgID *ber.Packet, op *ber.Packet) error {
	resp := ber.Encode(ber.ClassUniversal, ber.TypeConstructed, ber.TagSequence, nil, "LDAPMessage")
	resp.AppendChild(msgID)
	resp.AppendChild(op)
	_, err := conn.Write(resp.Bytes())
	return err
}

// ldapResultOp builds an [APPLICATION tag] SEQUENCE with resultCode,
// matchedDN and diagnosticMessage — the shared shape of bindResponse,
// searchResDone and extendedResp.
func ldapResultOp(tag ber.Tag, code int) *ber.Packet {
	op := ber.Encode(ber.ClassApplication, ber.TypeConstructed, tag, nil, "response")
	op.AppendChild(ber.NewInteger(ber.ClassUniversal, ber.TypePrimitive, ber.TagEnumerated, int64(code), "resultCode"))
	op.AppendChild(ber.NewString(ber.ClassUniversal, ber.TypePrimitive, ber.TagOctetString, "", "matchedDN"))
	op.AppendChild(ber.NewString(ber.ClassUniversal, ber.TypePrimitive, ber.TagOctetString, "", "diagnosticMessage"))
	return op
}

// searchEntryOp builds a SearchResultEntry carrying the requested subset of
// attrs (the DN rides in objectName, mirroring the interface mock).
func searchEntryOp(dn string, attrs map[string][]string, requested []string) *ber.Packet {
	op := ber.Encode(ber.ClassApplication, ber.TypeConstructed, ber.Tag(4), nil, "Search Result Entry")
	op.AppendChild(ber.NewString(ber.ClassUniversal, ber.TypePrimitive, ber.TagOctetString, dn, "Object Name"))
	attrsSeq := ber.Encode(ber.ClassUniversal, ber.TypeConstructed, ber.TagSequence, nil, "Attributes")
	for name, values := range filterAttributes(attrs, requested) {
		pa := ber.Encode(ber.ClassUniversal, ber.TypeConstructed, ber.TagSequence, nil, "Partial Attribute")
		pa.AppendChild(ber.NewString(ber.ClassUniversal, ber.TypePrimitive, ber.TagOctetString, name, "Type"))
		vals := ber.Encode(ber.ClassUniversal, ber.TypeConstructed, ber.TagSet, nil, "Values")
		for _, v := range values {
			vals.AppendChild(ber.NewString(ber.ClassUniversal, ber.TypePrimitive, ber.TagOctetString, v, "Value"))
		}
		pa.AppendChild(vals)
		attrsSeq.AppendChild(pa)
	}
	op.AppendChild(attrsSeq)
	return op
}

// filterString renders the BER filter subtree back into the string shape
// matchesFilter understands: and-lists, equality matches, and present
// filters. Anything else degrades to match-everything, which the flow's
// duplicate-entry check turns into a loud failure instead of a silent one.
func filterString(p *ber.Packet) string {
	switch {
	case p.ClassType == ber.ClassContext && p.Tag == 0: // and
		var parts strings.Builder
		parts.WriteString("(&")
		for _, child := range p.Children {
			parts.WriteString(filterString(child))
		}
		parts.WriteByte(')')
		return parts.String()
	case p.ClassType == ber.ClassContext && p.Tag == 3 && len(p.Children) == 2: // equalityMatch
		return "(" + p.Children[0].Data.String() + "=" + p.Children[1].Data.String() + ")"
	case p.ClassType == ber.ClassContext && p.Tag == 7: // present
		if len(p.Children) == 1 {
			return "(" + p.Children[0].Data.String() + "=*)"
		}
		return "(" + p.Data.String() + "=*)"
	}
	return "(objectClass=*)"
}

func bindSeen(binds []string, dn string) bool {
	for _, b := range binds {
		if strings.HasPrefix(b, dn+"@") {
			return true
		}
	}
	return false
}

func plainBindSeen(binds []string) bool {
	for _, b := range binds {
		if strings.HasSuffix(b, "@plaintext") {
			return true
		}
	}
	return false
}

// ---------------------------------------------------------------------------
// AC 3: the TLS posture table over the real wire
// ---------------------------------------------------------------------------

// TestLDAPProviderTLSPostures pins the three-state contract (PRD FR-55-AC6 /
// LD-04 / NFR-S36, T-174 H35):
//
//   - ldaps:// with a trusted CA binds; an untrusted CA is rejected;
//   - ldap:// + start_tls upgrades on the wire and then binds — the upgrade
//     counter proves StartTLS actually ran (the D5 regression: it used to be
//     silently skipped) and the bind markers prove no credential left in
//     the clear;
//   - a refused or untrusted upgrade fails the login — never a silent
//     plaintext fallback;
//   - ldaps wins over start_tls (no double upgrade);
//   - skip_tls_verify=true accepts the self-signed certificate;
//   - plain ldap:// without start_tls keeps working (the operator's
//     explicit choice — the pre-T-186 posture, minus the silent-ignore bug).
func TestLDAPProviderTLSPostures(t *testing.T) {
	for _, tt := range []struct {
		name            string
		tlsURL          bool // dial the ldaps:// listener instead of ldap://
		startTLS        bool
		skipTLSVerify   bool
		trustCA         bool // put the server's CA in the client RootCAs
		startTLSSupport bool // the directory answers StartTLS
		wantBind        bool
	}{
		{
			name: "ldaps with trusted CA succeeds", tlsURL: true, trustCA: true,
			wantBind: true,
		},
		{
			name: "ldaps with untrusted CA is rejected", tlsURL: true,
			wantBind: false,
		},
		{
			name: "ldaps with skip_tls_verify accepts self-signed", tlsURL: true, skipTLSVerify: true,
			wantBind: true,
		},
		{
			name: "start_tls upgrades the plaintext connection", startTLS: true, trustCA: true, startTLSSupport: true,
			wantBind: true,
		},
		{
			name: "start_tls with untrusted CA is rejected", startTLS: true, startTLSSupport: true,
			wantBind: false,
		},
		{
			name: "start_tls refused by the directory fails closed", startTLS: true,
			wantBind: false,
		},
		{
			name: "ldaps wins over start_tls (no double upgrade)", tlsURL: true, startTLS: true, trustCA: true, startTLSSupport: true,
			wantBind: true,
		},
		{
			name: "plaintext without start_tls keeps working", trustCA: true,
			wantBind: true,
		},
	} {
		t.Run(tt.name, func(t *testing.T) {
			srv := newTLSLDAPServer(t, tt.startTLSSupport)

			cfg := &auth.LDAPConfig{
				Enabled:       true,
				URL:           srv.plainURL(),
				BaseDN:        "dc=example,dc=org",
				BindDN:        "cn=admin,dc=example,dc=org",
				BindPassword:  "admin-secret",
				UserFilter:    "(uid=%s)",
				UserIDAttr:    "uid",
				GroupBaseDN:   "ou=groups,dc=example,dc=org",
				GroupFilter:   "(&(objectClass=groupOfNames)(memberUid=%s))",
				GroupNameAttr: "cn",
				PoolSize:      2,
				StartTLS:      tt.startTLS,
				SkipTLSVerify: tt.skipTLSVerify,
			}
			if tt.tlsURL {
				cfg.URL = srv.tlsURL()
			}
			if tt.trustCA {
				cfg.TLSConfig = &tls.Config{RootCAs: srv.trustedRoots()}
			}

			// dialer nil = the real DialURL path; that is the point.
			prov, err := auth.NewLDAPProvider(cfg, nil, nil)
			if err != nil {
				t.Fatalf("NewLDAPProvider: %v", err)
			}
			defer prov.Close()

			ctx, cancel := context.WithTimeout(context.Background(), 15*time.Second)
			defer cancel()
			claims, err := prov.Bind(ctx, "jdoe", "jdoe-secret")

			srv.stats.mu.Lock()
			upgrades := srv.stats.upgrades
			refused := srv.stats.refusedStartTLS
			bindFails := srv.stats.bindFails
			extOnTLS := srv.stats.extOnTLS
			binds := append([]string(nil), srv.stats.binds...)
			srv.stats.mu.Unlock()

			if tt.wantBind {
				if err != nil {
					t.Fatalf("Bind(jdoe) = %v, want success", err)
				}
				if claims.Name != "jdoe" || claims.ProviderID != "uid=jdoe,ou=people,dc=example,dc=org" {
					t.Fatalf("claims = %+v, want jdoe over the mapped DN", claims)
				}
				if len(claims.Groups) != 1 || claims.Groups[0] != "developers" {
					t.Fatalf("claims.Groups = %v, want [developers] via group_base_dn", claims.Groups)
				}
				if !bindSeen(binds, "uid=jdoe,ou=people,dc=example,dc=org") {
					t.Fatalf("server never saw the user bind; binds=%v", binds)
				}
			} else if err == nil {
				t.Fatalf("Bind(jdoe) succeeded, want the TLS posture to reject it (claims=%+v)", claims)
			}

			// Cross-row invariants.
			switch tt.name {
			case "start_tls upgrades the plaintext connection":
				if upgrades == 0 {
					t.Fatal("server saw no StartTLS upgrade — the flag is being ignored again (D5)")
				}
				if plainBindSeen(binds) {
					t.Fatalf("a bind arrived in plaintext; binds=%v", binds)
				}
			case "start_tls refused by the directory fails closed":
				if refused == 0 {
					t.Fatal("server never received the StartTLS request")
				}
				if len(binds) != 0 || bindFails != 0 {
					t.Fatalf("traffic continued after the refused upgrade; binds=%v fails=%d", binds, bindFails)
				}
			case "ldaps wins over start_tls (no double upgrade)":
				if upgrades != 0 || extOnTLS != 0 {
					t.Fatalf("start_tls ran on an ldaps connection: upgrades=%d extOnTLS=%d", upgrades, extOnTLS)
				}
			case "plaintext without start_tls keeps working":
				if upgrades != 0 {
					t.Fatalf("unexpected StartTLS upgrade without the flag: %d", upgrades)
				}
			case "ldaps with trusted CA succeeds", "ldaps with untrusted CA is rejected",
				"ldaps with skip_tls_verify accepts self-signed":
				if upgrades != 0 {
					t.Fatalf("ldaps connection attempted a StartTLS upgrade: %d", upgrades)
				}
			}
		})
	}
}

// TestLDAPProviderGroupBaseDNOnTheWire: the group search leaves with
// group_base_dn as its base while the user search keeps base_dn (PRD FR-55
// step 5) — asserted from the server's recorded search bases.
func TestLDAPProviderGroupBaseDNOnTheWire(t *testing.T) {
	srv := newTLSLDAPServer(t, true)
	cfg := &auth.LDAPConfig{
		Enabled:       true,
		URL:           srv.plainURL(),
		BaseDN:        "dc=example,dc=org",
		BindDN:        "cn=admin,dc=example,dc=org",
		BindPassword:  "admin-secret",
		UserFilter:    "(uid=%s)",
		UserIDAttr:    "uid",
		GroupBaseDN:   "ou=groups,dc=example,dc=org",
		GroupFilter:   "(&(objectClass=groupOfNames)(memberUid=%s))",
		GroupNameAttr: "cn",
		PoolSize:      1,
	}
	prov, err := auth.NewLDAPProvider(cfg, nil, nil)
	if err != nil {
		t.Fatalf("NewLDAPProvider: %v", err)
	}
	defer prov.Close()

	if _, err := prov.Bind(context.Background(), "jdoe", "jdoe-secret"); err != nil {
		t.Fatalf("Bind: %v", err)
	}
	srv.stats.mu.Lock()
	searchBases := append([]string(nil), srv.stats.searchBases...)
	srv.stats.mu.Unlock()
	sawUser, sawGroup := false, false
	for _, b := range searchBases {
		switch b {
		case "dc=example,dc=org":
			sawUser = true
		case "ou=groups,dc=example,dc=org":
			sawGroup = true
		}
	}
	if !sawUser || !sawGroup {
		t.Fatalf("search bases = %v, want both base_dn (users) and group_base_dn (groups)", searchBases)
	}
}

// ---------------------------------------------------------------------------
// Dialer-level wiring (interface mock)
// ---------------------------------------------------------------------------

// TestLDAPProviderStartTLSWiring: the upgrade is applied per dial exactly
// when the URL is ldap:// and start_tls is set; ldaps:// never upgrades; a
// failed upgrade tears the connection down instead of pooling a plaintext
// one; skip_tls_verify is merged into the upgrade's tls.Config.
func TestLDAPProviderStartTLSWiring(t *testing.T) {
	for _, tt := range []struct {
		name          string
		url           string
		startTLS      bool
		skipTLSVerify bool
		startTLSErr   error
		wantBindOK    bool
		wantUpgrade   bool
	}{
		{name: "ldap url upgrades", url: "ldap://dir.example.com:389", startTLS: true,
			wantBindOK: true, wantUpgrade: true},
		{name: "ldaps url never upgrades", url: "ldaps://dir.example.com:636", startTLS: true,
			wantBindOK: true, wantUpgrade: false},
		{name: "ldap url without flag stays plaintext", url: "ldap://dir.example.com:389",
			wantBindOK: true, wantUpgrade: false},
		{name: "failed upgrade closes the connection", url: "ldap://dir.example.com:389", startTLS: true,
			startTLSErr: errors.New("directory refuses StartTLS"), wantBindOK: false, wantUpgrade: true},
		{name: "skip_tls_verify merges into the upgrade config", url: "ldap://dir.example.com:389",
			startTLS: true, skipTLSVerify: true, wantBindOK: true, wantUpgrade: true},
	} {
		t.Run(tt.name, func(t *testing.T) {
			mock := newMockLDAPConn()
			mock.addUser("uid=alice,dc=example,dc=com", "alicepass", map[string][]string{
				"uid": {"alice"}, "objectClass": {"posixAccount"},
			})
			mock.startTLSErr = tt.startTLSErr

			dialer := mockDialer(mock)
			cfg := &auth.LDAPConfig{
				Enabled: true, URL: tt.url, BaseDN: "dc=example,dc=com",
				UserFilter: "(uid=%s)", UserIDAttr: "uid", PoolSize: 1,
				StartTLS: tt.startTLS, SkipTLSVerify: tt.skipTLSVerify,
			}
			prov, err := auth.NewLDAPProvider(cfg, nil, dialer)
			if err != nil {
				t.Fatalf("NewLDAPProvider: %v", err)
			}
			defer prov.Close()

			_, bindErr := prov.Bind(context.Background(), "alice", "alicepass")
			if tt.wantBindOK && bindErr != nil {
				t.Fatalf("Bind: %v, want success", bindErr)
			}
			if !tt.wantBindOK && bindErr == nil {
				t.Fatal("Bind succeeded, want the failed upgrade to reject it")
			}
			if got, want := mock.startTLSCalls > 0, tt.wantUpgrade; got != want {
				t.Fatalf("startTLSCalls = %d, want upgrades=%v", mock.startTLSCalls, want)
			}
			if tt.startTLSErr != nil && !mock.closed {
				t.Fatal("connection with a failed upgrade was not closed (would pool plaintext)")
			}
			if tt.wantUpgrade && mock.startTLSErr == nil {
				if mock.startTLSConfig == nil {
					t.Fatal("upgrade ran without a tls.Config")
				}
				if mock.startTLSConfig.MinVersion < tls.VersionTLS12 {
					t.Fatalf("upgrade MinVersion = %v, want >= TLS 1.2", mock.startTLSConfig.MinVersion)
				}
				if got, want := mock.startTLSConfig.InsecureSkipVerify, tt.skipTLSVerify; got != want {
					t.Fatalf("upgrade InsecureSkipVerify = %v, want %v", got, want)
				}
			}
		})
	}
}

// TestLDAPProviderGroupBaseDNDefaults: group_base_dn empty resolves to
// base_dn at construction (the pre-T-186 behavior); a dedicated subtree
// scopes only the group search — the user entry keeps resolving from the
// user base either way.
func TestLDAPProviderGroupBaseDNDefaults(t *testing.T) {
	bindForGroups := func(t *testing.T, groupBaseDN string) []string {
		t.Helper()
		mock := newMockLDAPConn()
		mock.addUser("uid=alice,ou=people,dc=example,dc=com", "alicepass", map[string][]string{
			"uid": {"alice"}, "objectClass": {"posixAccount"},
		})
		mock.addGroup("cn=dev,ou=groups,dc=example,dc=com", map[string][]string{
			"cn": {"dev"}, "objectClass": {"groupOfNames"}, "memberUid": {"alice"},
		})
		dialer := mockDialer(mock, mockDialKeepState())
		cfg := &auth.LDAPConfig{
			Enabled: true, URL: "ldap://dir.example.com:389", BaseDN: "dc=example,dc=com",
			UserFilter: "(uid=%s)", UserIDAttr: "uid",
			GroupBaseDN:   groupBaseDN,
			GroupFilter:   "(&(objectClass=groupOfNames)(memberUid=%s))",
			GroupNameAttr: "cn",
			PoolSize:      1,
		}
		prov, err := auth.NewLDAPProvider(cfg, nil, dialer)
		if err != nil {
			t.Fatalf("NewLDAPProvider: %v", err)
		}
		defer prov.Close()
		if groupBaseDN == "" && cfg.GroupBaseDN != cfg.BaseDN {
			t.Fatalf("GroupBaseDN not defaulted to base_dn: %q", cfg.GroupBaseDN)
		}
		claims, err := prov.Bind(context.Background(), "alice", "alicepass")
		if err != nil {
			t.Fatalf("Bind: %v", err)
		}
		return claims.Groups
	}

	t.Run("empty defaults to base_dn", func(t *testing.T) {
		// ou=groups sits under dc=example,dc=com, so the defaulted base
		// still finds the group (subtree search).
		if got := bindForGroups(t, ""); len(got) != 1 || got[0] != "dev" {
			t.Fatalf("groups = %v, want [dev] under the defaulted base", got)
		}
	})
	t.Run("dedicated group base scopes the group search", func(t *testing.T) {
		if got := bindForGroups(t, "ou=groups,dc=example,dc=com"); len(got) != 1 || got[0] != "dev" {
			t.Fatalf("groups = %v, want [dev] via group_base_dn", got)
		}
	})
	t.Run("group outside the dedicated base is not found", func(t *testing.T) {
		// Same directory, but the group now lives under ou=other — a
		// group_base_dn of ou=groups must not see it.
		mock := newMockLDAPConn()
		mock.addUser("uid=alice,ou=people,dc=example,dc=com", "alicepass", map[string][]string{
			"uid": {"alice"}, "objectClass": {"posixAccount"},
		})
		mock.addGroup("cn=legacy,ou=other,dc=example,dc=com", map[string][]string{
			"cn": {"legacy"}, "objectClass": {"groupOfNames"}, "memberUid": {"alice"},
		})
		dialer := mockDialer(mock, mockDialKeepState())
		cfg := &auth.LDAPConfig{
			Enabled: true, URL: "ldap://dir.example.com:389", BaseDN: "dc=example,dc=com",
			UserFilter: "(uid=%s)", UserIDAttr: "uid",
			GroupBaseDN:   "ou=groups,dc=example,dc=com",
			GroupFilter:   "(&(objectClass=groupOfNames)(memberUid=%s))",
			GroupNameAttr: "cn",
			PoolSize:      1,
		}
		prov, err := auth.NewLDAPProvider(cfg, nil, dialer)
		if err != nil {
			t.Fatalf("NewLDAPProvider: %v", err)
		}
		defer prov.Close()
		claims, err := prov.Bind(context.Background(), "alice", "alicepass")
		if err != nil {
			t.Fatalf("Bind: %v", err)
		}
		if len(claims.Groups) != 0 {
			t.Fatalf("groups = %v, want none (outside group_base_dn)", claims.Groups)
		}
	})
}

// TestLDAPProviderTLSConfigLogs: enabling skip_tls_verify and setting
// start_tls on an ldaps:// URL each log exactly one WARN naming the setting
// (NFR-S36 / PRD FR-55 "ldaps:// 时忽略"); the clean posture stays silent.
func TestLDAPProviderTLSConfigLogs(t *testing.T) {
	for _, tt := range []struct {
		name      string
		cfg       *auth.LDAPConfig
		wantWarns []string
	}{
		{
			name:      "skip_tls_verify warns",
			cfg:       &auth.LDAPConfig{Enabled: true, URL: "ldaps://dir:636", BaseDN: "dc=x", SkipTLSVerify: true},
			wantWarns: []string{"skip_tls_verify"},
		},
		{
			name:      "start_tls on ldaps warns",
			cfg:       &auth.LDAPConfig{Enabled: true, URL: "ldaps://dir:636", BaseDN: "dc=x", StartTLS: true},
			wantWarns: []string{"start_tls"},
		},
		{
			name:      "clean posture is silent",
			cfg:       &auth.LDAPConfig{Enabled: true, URL: "ldap://dir:389", BaseDN: "dc=x"},
			wantWarns: nil,
		},
	} {
		t.Run(tt.name, func(t *testing.T) {
			var buf bytes.Buffer
			prev := slog.Default()
			slog.SetDefault(slog.New(slog.NewTextHandler(&buf, nil)))
			t.Cleanup(func() { slog.SetDefault(prev) })

			mock := newMockLDAPConn()
			mock.addUser("uid=alice,dc=example,dc=com", "alicepass", map[string][]string{"uid": {"alice"}})
			dialer := mockDialer(mock, mockDialKeepState())
			prov, err := auth.NewLDAPProvider(tt.cfg, nil, dialer)
			if err != nil {
				t.Fatalf("NewLDAPProvider: %v", err)
			}
			defer prov.Close()

			out := buf.String()
			for _, want := range tt.wantWarns {
				if !strings.Contains(out, want) {
					t.Errorf("log output %q does not mention %q", out, want)
				}
				if !strings.Contains(out, "level=WARN") {
					t.Errorf("log output %q is not at WARN level", out)
				}
			}
			if tt.wantWarns == nil && strings.Contains(out, "WARN") {
				t.Errorf("clean posture logged a WARN: %q", out)
			}
		})
	}
}

// TestLDAPProviderURLSchemeFailFast: a URL without the ldap/ldaps scheme is
// rejected at construction instead of failing obscurely on the first dial.
func TestLDAPProviderURLSchemeFailFast(t *testing.T) {
	for _, tt := range []struct {
		url    string
		wantIn string
	}{
		{"http://dir.example.com:389", "scheme must be ldap or ldaps"},
		{"dir.example.com:389", "must be an ldap(s) URL"},
	} {
		_, err := auth.NewLDAPProvider(&auth.LDAPConfig{
			Enabled: true, URL: tt.url, BaseDN: "dc=x",
		}, nil, nil)
		if err == nil {
			t.Fatalf("NewLDAPProvider(%q) succeeded, want scheme rejection", tt.url)
		}
		if !strings.Contains(err.Error(), tt.wantIn) {
			t.Errorf("error = %v, want it to contain %q", err, tt.wantIn)
		}
	}
}
