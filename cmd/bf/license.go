// License subcommand family (T-281, ADR-0032 decision 1's signing chain):
// the offline toolchain that generates the ed25519 key pair, issues signed
// license documents and inspects them. Everything here is LOCAL — no server
// contact, no profile resolution, no network — because issuance is an
// offline act by design: the private key must never live on a server.
//
// Document format v1 (architecture section 15.1.1, matching
// internal/license):
//
//	<base64url(payloadJSON)>.<base64url(ed25519Sig)>
//
// The signature covers the payload bytes exactly as serialized here — there
// is no re-canonicalization, so the byte string this tool signs is the one
// and only string a server verifies.
//
// First-issuance flow (the T-279 leftover 3 bootstrap swap):
//
//	1. bf license keygen                     # authoritative pair
//	2. swap internal/license/verifykey.go's two constants with the
//	   --as-go-const output and rebuild every binary
//	3. bf license issue --tier pro ...       # documents the new binaries
//	                                           accept
//
// Before step 2 a stock binary stays on the community floor for documents
// from a fresh key pair (the bootstrap key's private half was destroyed at
// T-279) — `bf license inspect` states that verdict honestly.

package main

import (
	"bytes"
	"crypto/ed25519"
	"crypto/rand"
	"crypto/x509"
	"encoding/base64"
	"encoding/hex"
	"encoding/json"
	"encoding/pem"
	"errors"
	"flag"
	"fmt"
	"io"
	"os"
	"strings"
	"time"

	"github.com/lzwzzy/binflow/internal/license"
)

// defaultLicenseKeyFile is where keygen writes (and issue reads) the
// private key when --key is absent. It is relative to the working
// directory on purpose — issuance happens in an operator-controlled
// directory, not in a shared location. The name is gitignored.
const defaultLicenseKeyFile = "binflow-license-private.pem"

// licenseKeyEnv is the environment fallback for issue's --key (ADR-0032:
// the private key arrives via --key or env, never via the config file).
const licenseKeyEnv = "BINFLOW_LICENSE_KEY"

const licenseUsage = `bf license — offline license document toolchain.

Everything here runs locally: no server contact, no profile, no network.
The signing private key never leaves the machine keygen wrote it on.

Usage:

	bf license keygen [flags]          Generate an ed25519 key pair
	bf license issue [flags]           Sign a license document
	bf license inspect <file|-> [flags]  Parse and verify a document

First issuance (bootstrap key swap, three steps):

	1. bf license keygen --as-go-const
	   Generate the authoritative pair. The private key lands in
	   ./binflow-license-private.pem (mode 0600, gitignored) and never
	   moves anywhere else.

	2. Swap internal/license/verifykey.go's two constants with the
	   --as-go-const output (EmbeddedVerifyKeyID and
	   embeddedVerifyKeyHex), then rebuild and redistribute every
	   binflow-server binary. Old documents signed by the destroyed
	   bootstrap key keep failing verification — that is the intended
	   fail-safe direction.

	3. bf license issue --licensee "Acme Corp" --tier pro --days 365 -o pro.lic
	   Documents issued after the swap verify on the new binaries.

Run "bf license keygen --help", "bf license issue --help" or
"bf license inspect --help" for the flags of each verb.
`

const licenseKeygenUsage = `bf license keygen — generate an ed25519 license key pair.

The private key is written mode 0600 and never overwritten (an existing
file is refused — regenerating over the authoritative key would silently
invalidate every outstanding license). The public key prints as raw hex
(the spelling internal/license/verifykey.go embeds) and can additionally
go to a file for bf-side verification with "bf license inspect --key".

Usage:

	bf license keygen [flags]

Flags:

	--key <path>        Private key output path (default
	                        ./binflow-license-private.pem, gitignored)
	--kid <id>          Key id stamped into issued documents (default
	                        "` + license.EmbeddedVerifyKeyID + `", the id the stock
	                        verify-key table carries; keep it in sync with
	                        whatever you paste into verifykey.go)
	--pub-out <path>    Optional file for the public key (raw hex)
	--as-go-const       Also print the two verifykey.go constants the
	                        first-issuance swap needs
	--help, -h          Show this help text

Example:

	bf license keygen --as-go-const
`

const licenseIssueUsage = `bf license issue — sign a license document (format v1).

Reads the private key keygen wrote, builds the payload, signs it with
ed25519 and emits the two-segment document. Default output is stdout
(the document is the only line printed, so piping works); -o writes a
file instead and prints a summary.

Usage:

	bf license issue [flags]

Flags:

	--licensee <name>     License holder name (required)
	--tier <tier>         community | pro | enterprise (required)
	--days <n>            Validity in days counted from --not-before.
	                          Required for pro/enterprise (a paid tier
	                          without an expiry is a malformed grant);
	                          omitted for community = perpetual
	--addons <csv>        Explicit addon allowlist, e.g. "ha,go".
	                          Omitted = tier-wide unlock (the default)
	--kid <id>            Key id, overriding the one stored in the
	                          private key file (default: the keygen --kid)
	--key <path>          Private key path (default
	                          ./binflow-license-private.pem or $
	                          ` + licenseKeyEnv + `)
	--not-before <ts>     RFC3339 validity start (default: now, UTC)
	-o, --out <file>      Write the document to a file (mode 0600)
	--help, -h            Show this help text

Example:

	bf license issue --licensee "Acme Corp" --tier pro --days 365 -o pro.lic
`

const licenseInspectUsage = `bf license inspect — parse and verify a license document.

Prints the payload fields plus two verdicts: verification against the
supplied --key (or, by default, the verify keys embedded in this binary)
and the stock-binary verdict — whether a stock binflow-server accepts the
document. The exit code follows the primary verdict (--key's when given,
the embedded one otherwise), so scripts can branch on it. Inspection
never involves a private key.

Usage:

	bf license inspect <file|-> [flags]

Arguments:

	<file>    Document file, or "-" to read from stdin

Flags:

	--key <path>   Public key file (raw hex, the --pub-out spelling).
	                   Default: the embedded verify keys — i.e. "would a
	                   stock binary accept this?"
	--help, -h     Show this help text

Example:

	bf license inspect pro.lic --key pub.hex
`

// ---------------------------------------------------------------------------
// dispatch
// ---------------------------------------------------------------------------

// runLicense routes the license noun. The family is offline, so unlike the
// other nouns it never builds a client runtime; opts would be dead weight.
func runLicense(args []string, stdout, stderr io.Writer) error {
	if len(args) == 0 || args[0] == "help" || args[0] == "--help" || args[0] == "-h" {
		_, _ = fmt.Fprint(stdout, licenseUsage)
		return nil
	}
	switch args[0] {
	case "keygen":
		return licenseKeygen(args[1:], stdout)
	case "issue":
		return licenseIssue(args[1:], stdout, stderr)
	case "inspect":
		return licenseInspect(args[1:], stdout)
	default:
		return fmt.Errorf("unknown license verb %q, try 'bf license keygen | issue | inspect'", args[0])
	}
}

// ---------------------------------------------------------------------------
// keygen
// ---------------------------------------------------------------------------

func licenseKeygen(args []string, stdout io.Writer) error {
	fs := flag.NewFlagSet("bf license keygen", flag.ContinueOnError)
	fs.SetOutput(io.Discard)
	fs.Usage = func() { _, _ = fmt.Fprint(stdout, licenseKeygenUsage) }
	keyPath := fs.String("key", defaultLicenseKeyFile, "private key output path")
	kid := fs.String("kid", license.EmbeddedVerifyKeyID, "key id stamped into issued documents")
	pubOut := fs.String("pub-out", "", "public key hex output file")
	asGoConst := fs.Bool("as-go-const", false, "print the verifykey.go constant block")

	pos, err := parseArgs(fs, args)
	if err != nil {
		return fmt.Errorf("bf license keygen: %w", err)
	}
	if len(pos) != 0 {
		return fmt.Errorf("license keygen takes no positional arguments, got %d", len(pos))
	}
	if !validLicenseID(*kid) {
		return fmt.Errorf("invalid --kid %q: want 1-64 characters from letters, digits, '.', '_' or '-', starting alphanumeric", *kid)
	}

	pub, priv, err := ed25519.GenerateKey(rand.Reader)
	if err != nil {
		return fmt.Errorf("generating ed25519 key pair: %w", err)
	}

	// PKCS#8 PEM with the kid as a header: one file carries everything
	// issue needs, and the id travels with the key it names (no separate
	// registry to drift).
	pkcs8, err := x509.MarshalPKCS8PrivateKey(priv)
	if err != nil {
		return fmt.Errorf("encoding private key as PKCS#8: %w", err)
	}
	block := &pem.Block{
		Type:    "PRIVATE KEY",
		Headers: map[string]string{"kid": *kid},
		Bytes:   pkcs8,
	}
	// O_EXCL: never overwrite an existing private key — clobbering the
	// authoritative key would silently invalidate every issued license.
	f, err := os.OpenFile(*keyPath, os.O_WRONLY|os.O_CREATE|os.O_EXCL, 0o600) //nolint:gosec // the path is the operator-provided CLI argument; creating it is the command's purpose.
	if err != nil {
		if errors.Is(err, os.ErrExist) {
			return fmt.Errorf("private key %s already exists; refusing to overwrite (move it aside or pass --key with a new path)", *keyPath)
		}
		return fmt.Errorf("creating private key %s: %w", *keyPath, err)
	}
	if err := pem.Encode(f, block); err != nil {
		_ = f.Close()
		return fmt.Errorf("encoding private key %s: %w", *keyPath, err)
	}
	if err := f.Close(); err != nil {
		return fmt.Errorf("writing private key %s: %w", *keyPath, err)
	}

	pubHex := hex.EncodeToString(pub)
	_, _ = fmt.Fprintf(stdout, "kid: %s\n", *kid)
	_, _ = fmt.Fprintf(stdout, "private key: %s (mode 0600) — keep offline; never commit, copy or move it onto a server\n", *keyPath)
	_, _ = fmt.Fprintf(stdout, "public key (hex): %s\n", pubHex)
	if *asGoConst {
		_, _ = fmt.Fprint(stdout, "\n// internal/license/verifykey.go — first-issuance swap:\n"+
			"// replace the two constants below, rebuild every binary, then issue with this key.\n"+
			fmt.Sprintf("const EmbeddedVerifyKeyID = %q\n", *kid)+
			fmt.Sprintf("const embeddedVerifyKeyHex = %q\n", pubHex))
	}
	if *pubOut != "" {
		if err := os.WriteFile(*pubOut, []byte(pubHex+"\n"), 0o600); err != nil {
			return fmt.Errorf("writing public key %s: %w", *pubOut, err)
		}
		_, _ = fmt.Fprintf(stdout, "public key file: %s\n", *pubOut)
	}
	return nil
}

// ---------------------------------------------------------------------------
// issue
// ---------------------------------------------------------------------------

// licensePayloadWire is the payload JSON exactly as document format v1
// spells it (camelCase wire names, field order per architecture section
// 15.1.1's example). It mirrors internal/license's private payload struct;
// the duplication is the format contract, pinned by the round-trip tests
// and by issue's post-signing self-check through license.VerifyDocument.
type licensePayloadWire struct {
	Typ       string          `json:"typ"`
	Alg       string          `json:"alg"`
	Kid       string          `json:"kid"`
	Ver       int             `json:"ver"`
	LicenseID string          `json:"licenseId"`
	Licensee  string          `json:"licensee"`
	Tier      string          `json:"tier"`
	IssuedAt  string          `json:"issuedAt"`
	NotBefore string          `json:"notBefore"`
	ExpiresAt *string         `json:"expiresAt,omitempty"` // nil = perpetual (community only)
	Addons    []string        `json:"addons,omitempty"`    // nil = tier-wide unlock
	Limits    json.RawMessage `json:"limits,omitempty"`    // reserved, not issued yet
}

func licenseIssue(args []string, stdout, stderr io.Writer) error {
	fs := flag.NewFlagSet("bf license issue", flag.ContinueOnError)
	fs.SetOutput(io.Discard)
	fs.Usage = func() { _, _ = fmt.Fprint(stdout, licenseIssueUsage) }
	licensee := fs.String("licensee", "", "license holder name")
	tier := fs.String("tier", "", "community, pro or enterprise")
	days := fs.Int("days", 0, "validity in days from --not-before")
	addons := fs.String("addons", "", "explicit addon allowlist (CSV)")
	kid := fs.String("kid", "", "key id override (default: the private key file's)")
	keyPath := fs.String("key", "", "private key path (default "+defaultLicenseKeyFile+" or $"+licenseKeyEnv+")")
	notBefore := fs.String("not-before", "", "RFC3339 validity start (default: now, UTC)")
	out := fs.String("o", "", "output file (default: stdout)")
	fs.StringVar(out, "out", "", "output file (default: stdout)")

	pos, err := parseArgs(fs, args)
	if err != nil {
		return fmt.Errorf("bf license issue: %w", err)
	}
	if len(pos) != 0 {
		return fmt.Errorf("license issue takes no positional arguments, got %d", len(pos))
	}

	holder := strings.TrimSpace(*licensee)
	if holder == "" {
		return fmt.Errorf("license issue requires --licensee")
	}
	t, ok := license.ParseTier(*tier)
	if !ok {
		return fmt.Errorf("invalid --tier %q: want community, pro or enterprise", *tier)
	}
	if *days < 0 {
		return fmt.Errorf("invalid --days %d: want >= 0", *days)
	}
	if *days == 0 && t != license.TierCommunity {
		return fmt.Errorf("tier %s requires --days > 0 (a paid tier without expiresAt is a malformed grant)", t)
	}
	var addonList []string
	if strings.TrimSpace(*addons) != "" {
		addonList = parseAddonCSV(*addons)
		for _, id := range addonList {
			if !validLicenseID(id) {
				return fmt.Errorf("invalid addon id %q in --addons: want 1-64 characters from letters, digits, '.', '_' or '-'", id)
			}
		}
	}
	validFrom := time.Now().UTC()
	if *notBefore != "" {
		validFrom, err = time.Parse(time.RFC3339, *notBefore)
		if err != nil {
			return fmt.Errorf("invalid --not-before %q: want RFC3339, e.g. 2026-09-01T00:00:00Z", *notBefore)
		}
		validFrom = validFrom.UTC()
	}

	// The key path: flag > env > default (ADR-0032's --key/env injection;
	// the config file is deliberately not a source).
	path := *keyPath
	if path == "" {
		path = os.Getenv(licenseKeyEnv)
	}
	if path == "" {
		path = defaultLicenseKeyFile
	}
	priv, keyKid, err := loadLicensePrivateKey(path, stderr)
	if err != nil {
		return err
	}
	docKid := keyKid
	if *kid != "" {
		if !validLicenseID(*kid) {
			return fmt.Errorf("invalid --kid %q: want 1-64 characters from letters, digits, '.', '_' or '-', starting alphanumeric", *kid)
		}
		docKid = *kid
	}

	licenseID, err := newLicenseID()
	if err != nil {
		return err
	}
	now := time.Now().UTC().Truncate(time.Second)
	p := licensePayloadWire{
		Typ:       license.DocType,
		Alg:       license.DocAlg,
		Kid:       docKid,
		Ver:       license.DocVersion,
		LicenseID: licenseID,
		Licensee:  holder,
		Tier:      t.String(),
		IssuedAt:  now.Format(time.RFC3339),
		NotBefore: validFrom.Truncate(time.Second).Format(time.RFC3339),
		Addons:    addonList,
	}
	if *days > 0 {
		exp := validFrom.Truncate(time.Second).AddDate(0, 0, *days).Format(time.RFC3339)
		p.ExpiresAt = &exp
	}
	payloadBytes, err := json.Marshal(p)
	if err != nil {
		return fmt.Errorf("encoding license payload: %w", err)
	}
	sig := ed25519.Sign(priv, payloadBytes)
	doc := base64.RawURLEncoding.EncodeToString(payloadBytes) + "." +
		base64.RawURLEncoding.EncodeToString(sig)

	// Self-check through the REAL verification chain before handing the
	// document out: this pins that the bytes this tool signs are the bytes
	// internal/license verifies (format drift fails here, at the tool,
	// never at a customer's install). The clock sits one second past
	// notBefore so a future-dated --not-before still verifies.
	if _, err := license.VerifyDocument(doc,
		map[string]ed25519.PublicKey{docKid: priv.Public().(ed25519.PublicKey)},
		validFrom.Add(time.Second)); err != nil {
		return fmt.Errorf("issued document failed self-verification: %w", err)
	}

	if *out != "" {
		if err := os.WriteFile(*out, []byte(doc+"\n"), 0o600); err != nil { //nolint:gosec // the path is the operator-provided CLI argument; writing it is the command's purpose.
			return fmt.Errorf("writing license document %s: %w", *out, err)
		}
		expiry := "perpetual"
		if p.ExpiresAt != nil {
			expiry = *p.ExpiresAt
		}
		unlock := "all addons (tier-wide)"
		if len(p.Addons) > 0 {
			unlock = strings.Join(p.Addons, ",")
		}
		_, _ = fmt.Fprintf(stdout, "license document written to %s\n", *out)
		_, _ = fmt.Fprintf(stdout, "license id: %s\n", p.LicenseID)
		_, _ = fmt.Fprintf(stdout, "licensee: %s\n", p.Licensee)
		_, _ = fmt.Fprintf(stdout, "tier: %s\n", p.Tier)
		_, _ = fmt.Fprintf(stdout, "expires: %s\n", expiry)
		_, _ = fmt.Fprintf(stdout, "addons: %s\n", unlock)
		return nil
	}
	// stdout mode prints exactly the document: `bf license issue ... | pbcopy`
	// and install pipes stay clean.
	_, _ = fmt.Fprintln(stdout, doc)
	return nil
}

// ---------------------------------------------------------------------------
// inspect
// ---------------------------------------------------------------------------

// licenseDocDisplay is the decode-only view inspect prints. It carries the
// same wire names as licensePayloadWire but no signing role.
type licenseDocDisplay struct {
	Typ       string          `json:"typ"`
	Alg       string          `json:"alg"`
	Kid       string          `json:"kid"`
	Ver       int             `json:"ver"`
	LicenseID string          `json:"licenseId"`
	Licensee  string          `json:"licensee"`
	Tier      string          `json:"tier"`
	IssuedAt  string          `json:"issuedAt"`
	NotBefore string          `json:"notBefore"`
	ExpiresAt *string         `json:"expiresAt"`
	Addons    []string        `json:"addons"`
	Limits    json.RawMessage `json:"limits"`
}

func licenseInspect(args []string, stdout io.Writer) error {
	fs := flag.NewFlagSet("bf license inspect", flag.ContinueOnError)
	fs.SetOutput(io.Discard)
	fs.Usage = func() { _, _ = fmt.Fprint(stdout, licenseInspectUsage) }
	keyFile := fs.String("key", "", "public key hex file (default: embedded verify keys)")

	pos, err := parseArgs(fs, args)
	if err != nil {
		return fmt.Errorf("bf license inspect: %w", err)
	}
	if len(pos) != 1 || pos[0] == "" {
		return fmt.Errorf("license inspect requires exactly one <file> argument (or - for stdin), got %d", len(pos))
	}
	var raw []byte
	if pos[0] == "-" {
		raw, err = io.ReadAll(os.Stdin)
		if err != nil {
			return fmt.Errorf("reading document from stdin: %w", err)
		}
	} else {
		raw, err = os.ReadFile(pos[0]) //nolint:gosec // the path is the operator-provided CLI argument; reading it is the command's purpose.
		if err != nil {
			return fmt.Errorf("reading document %s: %w", pos[0], err)
		}
	}
	doc := strings.TrimSpace(string(raw))

	disp, err := decodeLicenseDocument(doc)
	if err != nil {
		return fmt.Errorf("bf license inspect: %w", err)
	}

	embedded, err := license.EmbeddedVerifyKeys()
	if err != nil {
		return fmt.Errorf("loading embedded verify keys: %w", err)
	}
	now := time.Now().UTC()

	// Primary verdict: --key's table when given, the embedded table
	// otherwise. A supplied key is trusted under the DOCUMENT's own kid —
	// the operator pinned the anchor explicitly; kid enforcement is the
	// embedded path's job (that is what "stock binary" answers).
	primary, primarySrc := embedded, "embedded"
	if *keyFile != "" {
		pub, err := loadLicensePublicKey(*keyFile)
		if err != nil {
			return err
		}
		primary = map[string]ed25519.PublicKey{disp.Kid: pub}
		primarySrc = "--key " + *keyFile
	}
	_, primaryErr := license.VerifyDocument(doc, primary, now)

	// Stock-binary verdict: always reported — with --key it answers the
	// separate question "beyond this key, what does an unmodified binary
	// do with this document?".
	_, stockErr := license.VerifyDocument(doc, embedded, now)

	printLicenseDocument(stdout, disp)
	if primaryErr != nil {
		_, _ = fmt.Fprintf(stdout, "verification (%s): FAILED — %v\n", primarySrc, primaryErr)
	} else {
		_, _ = fmt.Fprintf(stdout, "verification (%s): OK\n", primarySrc)
	}
	if stockErr != nil {
		_, _ = fmt.Fprintf(stdout, "stock binary (embedded key %q): REJECTED — %v\n", license.EmbeddedVerifyKeyID, stockErr)
	} else {
		_, _ = fmt.Fprintf(stdout, "stock binary (embedded key %q): accepted\n", license.EmbeddedVerifyKeyID)
	}

	if primaryErr != nil {
		// Fields and verdicts are already on stdout; the error makes the
		// exit code non-zero for scripts (AC 1's tamper leg).
		return fmt.Errorf("license inspect: document verification failed: %w", primaryErr)
	}
	return nil
}

// printLicenseDocument renders the decoded payload. Timestamps print
// verbatim (RFC3339, the wire form) — re-formatting would invite timezone
// drift between what was signed and what is displayed.
func printLicenseDocument(w io.Writer, d licenseDocDisplay) {
	_, _ = fmt.Fprintf(w, "license id: %s\n", d.LicenseID)
	_, _ = fmt.Fprintf(w, "licensee: %s\n", d.Licensee)
	_, _ = fmt.Fprintf(w, "tier: %s\n", d.Tier)
	_, _ = fmt.Fprintf(w, "kid: %s\n", d.Kid)
	_, _ = fmt.Fprintf(w, "typ/alg/ver: %s / %s / %d\n", d.Typ, d.Alg, d.Ver)
	_, _ = fmt.Fprintf(w, "issued at: %s\n", d.IssuedAt)
	_, _ = fmt.Fprintf(w, "not before: %s\n", d.NotBefore)
	if d.ExpiresAt != nil {
		_, _ = fmt.Fprintf(w, "expires at: %s\n", *d.ExpiresAt)
	} else {
		_, _ = fmt.Fprintln(w, "expires at: (perpetual)")
	}
	if len(d.Addons) == 0 {
		_, _ = fmt.Fprintln(w, "addons: all (tier-wide unlock)")
	} else {
		_, _ = fmt.Fprintf(w, "addons: %s\n", strings.Join(d.Addons, ", "))
	}
	if len(d.Limits) == 0 || string(d.Limits) == "null" {
		_, _ = fmt.Fprintln(w, "limits: (none)")
	} else {
		_, _ = fmt.Fprintf(w, "limits: %s\n", string(d.Limits))
	}
}

// ---------------------------------------------------------------------------
// shared helpers
// ---------------------------------------------------------------------------

// validLicenseID is the closed charset for kids and addon ids: 1-64
// characters of [A-Za-z0-9._-], starting alphanumeric. It exists so the
// signed fields cannot carry control characters, whitespace or injection
// fodder into logs and error strings downstream.
func validLicenseID(s string) bool {
	if s == "" || len(s) > 64 {
		return false
	}
	for i := 0; i < len(s); i++ {
		c := s[i]
		switch {
		case c >= 'a' && c <= 'z', c >= 'A' && c <= 'Z', c >= '0' && c <= '9':
		case c == '.' || c == '_' || c == '-':
			if i == 0 {
				return false
			}
		default:
			return false
		}
	}
	return true
}

// parseAddonCSV splits a comma-separated allowlist: trimmed, empties
// dropped, duplicates collapsed (order preserved — the allowlist is a
// handful of ids, not a ranking).
func parseAddonCSV(csv string) []string {
	seen := map[string]bool{}
	var out []string
	for _, part := range strings.Split(csv, ",") {
		if part = strings.TrimSpace(part); part != "" && !seen[part] {
			seen[part] = true
			out = append(out, part)
		}
	}
	return out
}

// newLicenseID mints an RFC 4122 v4 UUID from crypto/rand — the licenseId
// is an opaque unique handle, and the stdlib-only rule (ADR-0005) keeps the
// fifteen-line spelling cheaper than a dependency.
func newLicenseID() (string, error) {
	var b [16]byte
	if _, err := rand.Read(b[:]); err != nil {
		return "", fmt.Errorf("license id entropy: %w", err)
	}
	b[6] = (b[6] & 0x0f) | 0x40 // version 4
	b[8] = (b[8] & 0x3f) | 0x80 // variant 10
	return fmt.Sprintf("%x-%x-%x-%x-%x", b[0:4], b[4:6], b[6:8], b[8:10], b[10:16]), nil
}

// loadLicensePrivateKey reads the keygen PEM (PKCS#8, kid in the headers)
// and warns — without failing — when the file is readable beyond its
// owner: the failure belongs to the operator's setup, not to issuance.
func loadLicensePrivateKey(path string, stderr io.Writer) (ed25519.PrivateKey, string, error) {
	raw, err := os.ReadFile(path) //nolint:gosec // the path is the operator-provided CLI argument; reading it is the command's purpose.
	if err != nil {
		return nil, "", fmt.Errorf("reading private key %s: %w", path, err)
	}
	if st, serr := os.Stat(path); serr == nil && st.Mode().Perm()&0o077 != 0 { //nolint:gosec // the path is the operator-provided CLI argument; the mode warning is the check's purpose.
		_, _ = fmt.Fprintf(stderr, "warning: private key %s is group/other readable (mode %v); chmod 600 %s recommended\n", path, st.Mode().Perm(), path)
	}
	block, _ := pem.Decode(bytes.TrimSpace(raw))
	if block == nil {
		return nil, "", fmt.Errorf("private key %s is not PEM-encoded", path)
	}
	if block.Type != "PRIVATE KEY" {
		return nil, "", fmt.Errorf("private key %s has PEM type %q, want %q (this looks like the PUBLIC key file)", path, block.Type, "PRIVATE KEY")
	}
	parsed, err := x509.ParsePKCS8PrivateKey(block.Bytes)
	if err != nil {
		return nil, "", fmt.Errorf("parsing private key %s: %w", path, err)
	}
	key, ok := parsed.(ed25519.PrivateKey)
	if !ok || len(key) != ed25519.PrivateKeySize {
		return nil, "", fmt.Errorf("private key %s is %T, want an ed25519 key (generate it with bf license keygen)", path, parsed)
	}
	kid := strings.TrimSpace(block.Headers["kid"])
	if kid == "" {
		kid = license.EmbeddedVerifyKeyID
	}
	return key, kid, nil
}

// loadLicensePublicKey reads the --pub-out spelling: raw hex, whitespace
// tolerated. A PEM private key handed by mistake gets a pointed refusal —
// inspection never touches private keys (the tool's own contract).
func loadLicensePublicKey(path string) (ed25519.PublicKey, error) {
	raw, err := os.ReadFile(path) //nolint:gosec // the path is the operator-provided CLI argument; reading it is the command's purpose.
	if err != nil {
		return nil, fmt.Errorf("reading public key %s: %w", path, err)
	}
	text := strings.TrimSpace(string(raw))
	if strings.HasPrefix(text, "-----BEGIN") {
		return nil, fmt.Errorf("%s is a PEM key file; inspect --key wants the raw hex spelling bf license keygen --pub-out writes (inspection never uses a private key)", path)
	}
	fields := strings.Fields(text)
	hexStr := text
	if len(fields) == 2 && strings.HasPrefix(fields[0], "kid:") {
		hexStr = fields[1] // tolerate a "kid: <id> / <hex>" scratch file
	} else if len(fields) > 1 {
		hexStr = strings.Join(fields, "")
	}
	rawKey, err := hex.DecodeString(hexStr)
	if err != nil {
		return nil, fmt.Errorf("public key %s is not hex: %w", path, err)
	}
	if len(rawKey) != ed25519.PublicKeySize {
		return nil, fmt.Errorf("public key %s is %d bytes, want %d (raw ed25519 public key, hex encoded)", path, len(rawKey), ed25519.PublicKeySize)
	}
	return ed25519.PublicKey(rawKey), nil
}

// decodeLicenseDocument performs the display-side decode: two-segment
// split, base64url payload, JSON unmarshal. It mirrors internal/license's
// split rules so a document this fails on is a document the server also
// refuses (the verdict lines then never get a chance to disagree with the
// field display).
func decodeLicenseDocument(doc string) (licenseDocDisplay, error) {
	var d licenseDocDisplay
	head, tail, ok := strings.Cut(strings.TrimSpace(doc), ".")
	if !ok || head == "" || tail == "" || strings.Contains(tail, ".") {
		return d, errors.New("not a two-segment document")
	}
	payloadBytes, err := base64.RawURLEncoding.DecodeString(head)
	if err != nil {
		return d, fmt.Errorf("payload segment is not base64url: %w", err)
	}
	if _, err := base64.RawURLEncoding.DecodeString(tail); err != nil {
		return d, fmt.Errorf("signature segment is not base64url: %w", err)
	}
	if err := json.Unmarshal(payloadBytes, &d); err != nil {
		return d, fmt.Errorf("payload is not JSON: %w", err)
	}
	return d, nil
}
