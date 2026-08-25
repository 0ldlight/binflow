package main

// T-281: the offline license toolchain — keygen / issue / inspect — plus
// the end-to-end chain through a REAL assembled server stack (the
// ADR-mandated constructor seam: license.Manager verifying against the
// keygen public key, exactly the world a binary lives in after the
// first-issuance verifykey.go swap). The stock-binary pre-swap posture is
// asserted too: the same document against EmbeddedVerifyKeys fails, which
// is the fail-safe direction T-279 left us on purpose.

import (
	"bytes"
	"context"
	"crypto/ed25519"
	"encoding/base64"
	"encoding/hex"
	"encoding/json"
	"encoding/pem"
	"fmt"
	"io"
	"log/slog"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"github.com/lzwzzy/binflow/internal/addons"
	"github.com/lzwzzy/binflow/internal/audit"
	"github.com/lzwzzy/binflow/internal/auth"
	"github.com/lzwzzy/binflow/internal/config"
	"github.com/lzwzzy/binflow/internal/httpapi"
	"github.com/lzwzzy/binflow/internal/license"
	"github.com/lzwzzy/binflow/internal/metadata"
	"github.com/lzwzzy/binflow/internal/repo"
	"github.com/lzwzzy/binflow/internal/storage"
)

// ---------------------------------------------------------------------------
// help faces and dispatch
// ---------------------------------------------------------------------------

func TestLicenseHelpFaces(t *testing.T) {
	tests := []struct {
		name string
		args []string
		want []string
	}{
		{name: "noun bare", args: []string{"license"}, want: []string{"bf license keygen", "First issuance", "--as-go-const"}},
		{name: "noun help", args: []string{"license", "--help"}, want: []string{"bf license keygen"}},
		{name: "keygen help", args: []string{"license", "keygen", "--help"}, want: []string{"--pub-out", "--kid", "0600"}},
		{name: "keygen help short", args: []string{"license", "keygen", "-h"}, want: []string{"--as-go-const"}},
		{name: "issue help", args: []string{"license", "issue", "--help"}, want: []string{"--licensee", "--tier", "--days", "--addons"}},
		{name: "inspect help", args: []string{"license", "inspect", "--help"}, want: []string{"--key", "stdin"}},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			var stdout, stderr bytes.Buffer
			if err := run(tt.args, &stdout, &stderr); err != nil {
				t.Fatalf("run(%v) error = %v, want nil (help exits zero)", tt.args, err)
			}
			for _, want := range tt.want {
				if !strings.Contains(stdout.String(), want) {
					t.Errorf("stdout missing %q, got:\n%s", want, stdout.String())
				}
			}
		})
	}
}

func TestLicenseUnknownVerb(t *testing.T) {
	var stdout, stderr bytes.Buffer
	err := run([]string{"license", "revoke"}, &stdout, &stderr)
	if err == nil || !strings.Contains(err.Error(), "unknown license verb") {
		t.Fatalf("run error = %v, want unknown license verb", err)
	}
}

// The license family must work with NO configuration at all — it is
// offline; a missing profile or absent server cannot be a failure mode.
func TestLicenseWorksWithoutConfig(t *testing.T) {
	t.Setenv("BF_CONFIG", filepath.Join(t.TempDir(), "nonexistent.yaml"))
	dir := t.TempDir()
	var stdout, stderr bytes.Buffer
	if err := run([]string{"license", "keygen", "--key", filepath.Join(dir, "k.pem")}, &stdout, &stderr); err != nil {
		t.Fatalf("keygen error = %v, want nil with no config file present", err)
	}
}

// ---------------------------------------------------------------------------
// keygen
// ---------------------------------------------------------------------------

func TestLicenseKeygen(t *testing.T) {
	dir := t.TempDir()
	key := filepath.Join(dir, "lic-private.pem")

	var stdout, stderr bytes.Buffer
	if err := run([]string{"license", "keygen", "--key", key, "--kid", "t281-key", "--pub-out", filepath.Join(dir, "pub.hex"), "--as-go-const"}, &stdout, &stderr); err != nil {
		t.Fatalf("keygen error = %v, err output %q", err, stderr.String())
	}
	out := stdout.String()
	if stderr.Len() != 0 {
		t.Errorf("stderr = %q, want empty on success", stderr.String())
	}

	// Default kid appears when not overridden; here the explicit one must.
	for _, want := range []string{"kid: t281-key", "public key (hex): ", "const EmbeddedVerifyKeyID = \"t281-key\"", "const embeddedVerifyKeyHex = "} {
		if !strings.Contains(out, want) {
			t.Errorf("stdout missing %q, got:\n%s", want, out)
		}
	}

	// Private key: mode 0600, PEM, PKCS#8, kid header, ed25519-sized.
	st, err := os.Stat(key)
	if err != nil {
		t.Fatalf("stat private key: %v", err)
	}
	if perm := st.Mode().Perm(); perm != 0o600 {
		t.Errorf("private key mode = %o, want 0600", perm)
	}
	raw, err := os.ReadFile(key)
	if err != nil {
		t.Fatalf("read private key: %v", err)
	}
	block, _ := pem.Decode(raw)
	if block == nil || block.Type != "PRIVATE KEY" {
		t.Fatalf("private key PEM type = %v, want PRIVATE KEY", block)
	}
	if got := block.Headers["kid"]; got != "t281-key" {
		t.Errorf("PEM kid header = %q, want t281-key", got)
	}

	// Public key hex: printed, filed, and a valid 32-byte key.
	hexStr := strings.TrimSpace(strings.SplitN(strings.Split(out, "public key (hex): ")[1], "\n", 2)[0])
	pubBytes, err := hex.DecodeString(hexStr)
	if err != nil || len(pubBytes) != ed25519.PublicKeySize {
		t.Fatalf("printed public key hex = %q (err %v), want 32 raw bytes", hexStr, err)
	}
	fileHex, err := os.ReadFile(filepath.Join(dir, "pub.hex"))
	if err != nil {
		t.Fatalf("read pub.hex: %v", err)
	}
	if strings.TrimSpace(string(fileHex)) != hexStr {
		t.Errorf("pub.hex = %q, want the printed hex %q", fileHex, hexStr)
	}

	// The --as-go-const hex must equal the printed public key — the paste
	// into verifykey.go and the anchor handed to inspect are the same key.
	if !strings.Contains(out, fmt.Sprintf("const embeddedVerifyKeyHex = %q", hexStr)) {
		t.Errorf("--as-go-const hex differs from the printed public key")
	}

	// Overwrite refusal: regenerating over the authoritative key must fail
	// without touching the file.
	before := string(raw)
	var stdout2, stderr2 bytes.Buffer
	err = run([]string{"license", "keygen", "--key", key}, &stdout2, &stderr2)
	if err == nil || !strings.Contains(err.Error(), "refusing to overwrite") {
		t.Fatalf("second keygen error = %v, want overwrite refusal", err)
	}
	after, _ := os.ReadFile(key)
	if string(after) != before {
		t.Error("private key file changed under a refused overwrite")
	}
}

// keygen's default path is ./binflow-license-private.pem relative to the
// working directory (the gitignored name).
func TestLicenseKeygenDefaultPath(t *testing.T) {
	dir := t.TempDir()
	t.Chdir(dir)
	var stdout, stderr bytes.Buffer
	if err := run([]string{"license", "keygen"}, &stdout, &stderr); err != nil {
		t.Fatalf("keygen default path error = %v", err)
	}
	if _, err := os.Stat(filepath.Join(dir, defaultLicenseKeyFile)); err != nil {
		t.Fatalf("default private key file missing: %v", err)
	}
	if !strings.Contains(stdout.String(), defaultLicenseKeyFile) {
		t.Errorf("stdout %q should name the default path", stdout.String())
	}
}

func TestLicenseKeygenBadKid(t *testing.T) {
	tests := []struct {
		name string
		kid  string
	}{
		{name: "empty", kid: ""},
		{name: "leading dash", kid: "-nope"},
		{name: "space inside", kid: "bad kid"},
		{name: "too long", kid: strings.Repeat("a", 65)},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			var stdout, stderr bytes.Buffer
			err := run([]string{"license", "keygen", "--key", filepath.Join(t.TempDir(), "k.pem"), "--kid", tt.kid}, &stdout, &stderr)
			if err == nil || !strings.Contains(err.Error(), "invalid --kid") {
				t.Fatalf("keygen --kid %q error = %v, want invalid --kid", tt.kid, err)
			}
		})
	}
}

// ---------------------------------------------------------------------------
// issue
// ---------------------------------------------------------------------------

// keygenFixture is one fresh key pair on disk, made through the real CLI.
type keygenFixture struct {
	dir    string
	key    string
	pubHex string
}

func newKeygenFixture(t *testing.T, kid string) *keygenFixture {
	t.Helper()
	dir := t.TempDir()
	key := filepath.Join(dir, "lic-private.pem")
	var stdout, stderr bytes.Buffer
	args := []string{"license", "keygen", "--key", key}
	if kid != "" {
		args = append(args, "--kid", kid)
	}
	if err := run(args, &stdout, &stderr); err != nil {
		t.Fatalf("keygen: %v (stderr %q)", err, stderr.String())
	}
	line := ""
	for _, l := range strings.Split(stdout.String(), "\n") {
		if strings.HasPrefix(l, "public key (hex): ") {
			line = strings.TrimPrefix(l, "public key (hex): ")
		}
	}
	if line == "" {
		t.Fatalf("keygen stdout missing public key hex:\n%s", stdout.String())
	}
	return &keygenFixture{dir: dir, key: key, pubHex: line}
}

func TestLicenseIssueValidation(t *testing.T) {
	f := newKeygenFixture(t, "")
	base := []string{"license", "issue", "--key", f.key}
	tests := []struct {
		name string
		args []string
		want string
	}{
		{name: "missing licensee", args: append([]string{}, base...), want: "requires --licensee"},
		{name: "blank licensee", args: append(append([]string{}, base...), "--licensee", "  ", "--tier", "pro", "--days", "30"), want: "requires --licensee"},
		{name: "bad tier", args: append(append([]string{}, base...), "--licensee", "acme", "--tier", "platinum"), want: `invalid --tier "platinum"`},
		{name: "pro without days", args: append(append([]string{}, base...), "--licensee", "acme", "--tier", "pro"), want: "requires --days"},
		{name: "enterprise without days", args: append(append([]string{}, base...), "--licensee", "acme", "--tier", "enterprise"), want: "requires --days"},
		{name: "negative days", args: append(append([]string{}, base...), "--licensee", "acme", "--tier", "pro", "--days", "-5"), want: "invalid --days"},
		{name: "bad not-before", args: append(append([]string{}, base...), "--licensee", "acme", "--tier", "pro", "--days", "30", "--not-before", "2026-13-99"), want: "invalid --not-before"},
		{name: "bad addon id", args: append(append([]string{}, base...), "--licensee", "acme", "--tier", "pro", "--days", "30", "--addons", "ha,,bad id"), want: "invalid addon id"},
		{name: "bad kid override", args: append(append([]string{}, base...), "--licensee", "acme", "--tier", "pro", "--days", "30", "--kid", "no/kid"), want: "invalid --kid"},
		{name: "missing key file", args: []string{"license", "issue", "--key", filepath.Join(t.TempDir(), "absent.pem"), "--licensee", "acme", "--tier", "pro", "--days", "30"}, want: "reading private key"},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			var stdout, stderr bytes.Buffer
			err := run(tt.args, &stdout, &stderr)
			if err == nil || !strings.Contains(err.Error(), tt.want) {
				t.Fatalf("run(%v) error = %v, want it to contain %q", tt.args, err, tt.want)
			}
			if stdout.Len() != 0 {
				t.Errorf("stdout = %q, want empty on a refused issue", stdout.String())
			}
		})
	}
}

// A private key file that is not keygen's PEM gets a pointed error, not a
// crypto panic.
func TestLicenseIssueBadKeyFile(t *testing.T) {
	dir := t.TempDir()
	garbage := filepath.Join(dir, "garbage.pem")
	writeFile(t, garbage, "not a pem at all\n")
	var stdout, stderr bytes.Buffer
	err := run([]string{"license", "issue", "--key", garbage, "--licensee", "acme", "--tier", "pro", "--days", "30"}, &stdout, &stderr)
	if err == nil || !strings.Contains(err.Error(), "not PEM-encoded") {
		t.Fatalf("error = %v, want not-PEM refusal", err)
	}

	// The PUBLIC key handed where the private key belongs is a likely
	// operator slip — name it.
	pubFile := filepath.Join(dir, "pub.pem")
	writeFile(t, pubFile, "-----BEGIN PUBLIC KEY-----\nAAAA\n-----END PUBLIC KEY-----\n")
	err = run([]string{"license", "issue", "--key", pubFile, "--licensee", "acme", "--tier", "pro", "--days", "30"}, &stdout, &stderr)
	if err == nil || !strings.Contains(err.Error(), "PUBLIC key file") {
		t.Fatalf("error = %v, want the public-key-file pointer", err)
	}
}

func TestLicenseIssue(t *testing.T) {
	f := newKeygenFixture(t, "t281-issue")
	notBefore := "2026-09-01T00:00:00Z"

	var stdout, stderr bytes.Buffer
	args := []string{
		"license", "issue", "--key", f.key,
		"--licensee", "Acme Corp", "--tier", "pro", "--days", "365",
		"--addons", " ha, go ,,ha ", "--not-before", notBefore,
	}
	if err := run(args, &stdout, &stderr); err != nil {
		t.Fatalf("issue error = %v (stderr %q)", err, stderr.String())
	}
	out := stdout.String()
	if stderr.Len() != 0 {
		t.Errorf("stderr = %q, want empty", stderr.String())
	}

	// stdout mode: exactly one line, the document — pipe-clean.
	doc := strings.TrimSuffix(out, "\n")
	if strings.Count(out, "\n") != 1 {
		t.Fatalf("stdout mode printed %d lines, want exactly the document:\n%s", strings.Count(out, "\n"), out)
	}

	p := decodeDocForTest(t, doc)
	if p.Typ != "binflow-license" || p.Alg != "EdDSA" || p.Ver != 1 {
		t.Errorf("header = %s/%s/%d, want binflow-license/EdDSA/1", p.Typ, p.Alg, p.Ver)
	}
	if p.Kid != "t281-issue" {
		t.Errorf("kid = %q, want the keygen kid", p.Kid)
	}
	if p.Licensee != "Acme Corp" {
		t.Errorf("licensee = %q, want Acme Corp", p.Licensee)
	}
	if p.Tier != "pro" {
		t.Errorf("tier = %q, want pro", p.Tier)
	}
	if p.NotBefore != notBefore {
		t.Errorf("notBefore = %q, want %q (issued verbatim)", p.NotBefore, notBefore)
	}
	if p.ExpiresAt == nil || *p.ExpiresAt != "2027-09-01T00:00:00Z" {
		t.Errorf("expiresAt = %v, want notBefore + 365 calendar days", p.ExpiresAt)
	}
	// CSV parsed: trimmed, empties dropped, duplicate collapsed, order kept.
	if strings.Join(p.Addons, ",") != "ha,go" {
		t.Errorf("addons = %v, want [ha go]", p.Addons)
	}
	if len(p.Limits) != 0 {
		t.Errorf("limits = %s, want absent (reserved)", p.Limits)
	}

	// The signed document verifies through the REAL chain against the
	// keygen public key — the bytes this tool signs are the bytes
	// internal/license verifies.
	if _, err := license.VerifyDocument(doc, map[string]ed25519.PublicKey{
		p.Kid: mustPubKey(t, f.pubHex),
	}, time.Date(2026, 9, 2, 0, 0, 0, 0, time.UTC)); err != nil {
		t.Fatalf("VerifyDocument on issued doc: %v", err)
	}
}

// Kid precedence: --kid beats the PEM header; the env var beats the
// default path; community without --days is perpetual.
func TestLicenseIssueKidAndPerpetual(t *testing.T) {
	f := newKeygenFixture(t, "pem-kid")
	var stdout, stderr bytes.Buffer

	if err := run([]string{"license", "issue", "--key", f.key, "--kid", "override-kid",
		"--licensee", "c", "--tier", "community"}, &stdout, &stderr); err != nil {
		t.Fatalf("issue community: %v", err)
	}
	p := decodeDocForTest(t, stdout.String())
	if p.Kid != "override-kid" {
		t.Errorf("kid = %q, want the --kid override", p.Kid)
	}
	if p.ExpiresAt != nil {
		t.Errorf("community without --days has expiresAt = %v, want perpetual (absent)", *p.ExpiresAt)
	}
	if len(p.Addons) != 0 {
		t.Errorf("addons = %v, want absent (tier-wide default)", p.Addons)
	}

	// BINFLOW_LICENSE_KEY substitutes for --key (ADR-0032's env injection).
	t.Setenv(licenseKeyEnv, f.key)
	stdout.Reset()
	stderr.Reset()
	if err := run([]string{"license", "issue", "--licensee", "c", "--tier", "community", "--days", "10"}, &stdout, &stderr); err != nil {
		t.Fatalf("issue via env key: %v", err)
	}
	p = decodeDocForTest(t, stdout.String())
	if p.Kid != "pem-kid" {
		t.Errorf("kid = %q, want the PEM header kid (no --kid given)", p.Kid)
	}
	if p.ExpiresAt == nil {
		t.Error("community with --days should carry an expiresAt")
	}
}

// Output mode: -o writes the file (0600) and prints a summary instead of
// the raw document; --out is the long spelling.
func TestLicenseIssueOutputFile(t *testing.T) {
	f := newKeygenFixture(t, "")
	dir := t.TempDir()
	outFile := filepath.Join(dir, "pro.lic")

	var stdout, stderr bytes.Buffer
	if err := run([]string{"license", "issue", "--key", f.key, "-o", outFile,
		"--licensee", "Acme Corp", "--tier", "enterprise", "--days", "90"}, &stdout, &stderr); err != nil {
		t.Fatalf("issue -o: %v", err)
	}
	for _, want := range []string{"license document written to " + outFile, "tier: enterprise", "expires:"} {
		if !strings.Contains(stdout.String(), want) {
			t.Errorf("stdout %q missing %q", stdout.String(), want)
		}
	}
	raw, err := os.ReadFile(outFile)
	if err != nil {
		t.Fatalf("read issued file: %v", err)
	}
	if st, err := os.Stat(outFile); err != nil || st.Mode().Perm() != 0o600 {
		t.Errorf("issued file mode = %v (err %v), want 0600", st, err)
	}
	// The file body is the document (one line); it verifies.
	doc := strings.TrimSpace(string(raw))
	if _, err := license.VerifyDocument(doc, map[string]ed25519.PublicKey{
		decodeDocForTest(t, doc).Kid: mustPubKey(t, f.pubHex),
	}, time.Now().UTC()); err != nil {
		t.Fatalf("VerifyDocument on filed doc: %v", err)
	}

	stdout.Reset()
	if err := run([]string{"license", "issue", "--key", f.key, "--out", filepath.Join(dir, "pro2.lic"),
		"--licensee", "Acme Corp", "--tier", "pro", "--days", "1"}, &stdout, &stderr); err != nil {
		t.Fatalf("issue --out: %v", err)
	}
	if _, err := os.Stat(filepath.Join(dir, "pro2.lic")); err != nil {
		t.Errorf("--out long spelling did not write the file: %v", err)
	}
}

// A group/other-readable private key warns on stderr but still issues.
func TestLicenseIssueModeWarning(t *testing.T) {
	f := newKeygenFixture(t, "")
	if err := os.Chmod(f.key, 0o644); err != nil {
		t.Fatalf("chmod: %v", err)
	}
	var stdout, stderr bytes.Buffer
	if err := run([]string{"license", "issue", "--key", f.key, "--licensee", "c", "--tier", "pro", "--days", "5"}, &stdout, &stderr); err != nil {
		t.Fatalf("issue: %v", err)
	}
	if !strings.Contains(stderr.String(), "chmod 600") {
		t.Errorf("stderr = %q, want the loose-mode warning", stderr.String())
	}
}

// ---------------------------------------------------------------------------
// inspect
// ---------------------------------------------------------------------------

// issuedDocForTest runs the real keygen + issue and returns the document,
// its public key hex and the fixture directory.
func issuedDocForTest(t *testing.T, tier string, days int) (doc, pubHex, dir string) {
	t.Helper()
	f := newKeygenFixture(t, "")
	args := []string{"license", "issue", "--key", f.key, "--licensee", "Acme Corp", "--tier", tier}
	if days > 0 {
		args = append(args, "--days", fmt.Sprint(days))
	}
	var stdout, stderr bytes.Buffer
	if err := run(args, &stdout, &stderr); err != nil {
		t.Fatalf("issue: %v (stderr %q)", err, stderr.String())
	}
	return strings.TrimSpace(stdout.String()), f.pubHex, f.dir
}

func TestLicenseInspect(t *testing.T) {
	doc, pubHex, dir := issuedDocForTest(t, "pro", 365)
	pubFile := filepath.Join(dir, "pub.hex")
	writeFile(t, pubFile, pubHex+"\n")
	docFile := filepath.Join(dir, "pro.lic")
	writeFile(t, docFile, doc+"\n")

	t.Run("round trip with --key", func(t *testing.T) {
		var stdout, stderr bytes.Buffer
		if err := run([]string{"license", "inspect", docFile, "--key", pubFile}, &stdout, &stderr); err != nil {
			t.Fatalf("inspect error = %v (stdout below)\n%s", err, stdout.String())
		}
		out := stdout.String()
		for _, want := range []string{
			"licensee: Acme Corp", "tier: pro", "addons: all (tier-wide unlock)",
			"verification (--key " + pubFile + "): OK",
			`stock binary (embedded key "` + license.EmbeddedVerifyKeyID + `"): REJECTED`,
		} {
			if !strings.Contains(out, want) {
				t.Errorf("stdout missing %q, got:\n%s", want, out)
			}
		}
		// The pre-swap truth, stated: a fresh pair is not the embedded key.
		if !strings.Contains(out, "signature verification failed") {
			t.Errorf("stock-binary rejection should name the failed signature:\n%s", out)
		}
	})

	t.Run("stdin leg", func(t *testing.T) {
		old := os.Stdin
		r, w, err := os.Pipe()
		if err != nil {
			t.Fatalf("pipe: %v", err)
		}
		go func() { _, _ = w.WriteString(doc + "\n"); _ = w.Close() }()
		os.Stdin = r
		defer func() { os.Stdin = old }()
		var stdout, stderr bytes.Buffer
		if err := run([]string{"license", "inspect", "-", "--key", pubFile}, &stdout, &stderr); err != nil {
			t.Fatalf("inspect stdin: %v", err)
		}
		if !strings.Contains(stdout.String(), "tier: pro") {
			t.Errorf("stdin inspect output:\n%s", stdout.String())
		}
	})

	t.Run("tampered signature fails and exits non-zero", func(t *testing.T) {
		// Flip the FIRST signature character: unlike a trailing character
		// (whose low bits are discarded padding in unpadded base64url),
		// this changes decoded bytes, so ed25519 must fail.
		head, tail, _ := strings.Cut(doc, ".")
		if tail[0] == 'A' {
			tail = "B" + tail[1:]
		} else {
			tail = "A" + tail[1:]
		}
		bad := filepath.Join(dir, "tampered.lic")
		writeFile(t, bad, head+"."+tail+"\n")
		var stdout, stderr bytes.Buffer
		err := run([]string{"license", "inspect", bad, "--key", pubFile}, &stdout, &stderr)
		if err == nil {
			t.Fatal("inspect on a tampered document must exit non-zero")
		}
		if !strings.Contains(err.Error(), "verification failed") || !strings.Contains(err.Error(), "signature") {
			t.Errorf("error = %v, want the signature failure surfaced", err)
		}
		if !strings.Contains(stdout.String(), "verification (--key "+pubFile+"): FAILED") {
			t.Errorf("stdout should carry the FAILED verdict before the error:\n%s", stdout.String())
		}
		// Fields still printed: the operator sees WHAT failed to verify.
		if !strings.Contains(stdout.String(), "licensee: Acme Corp") {
			t.Errorf("stdout should still print the decoded fields:\n%s", stdout.String())
		}
	})

	t.Run("tampered payload fails", func(t *testing.T) {
		head, tail, _ := strings.Cut(doc, ".")
		seg := []byte(head)
		if seg[0] == 'e' {
			seg[0] = 'f'
		} else {
			seg[0] = 'e'
		}
		bad := filepath.Join(dir, "tampered-payload.lic")
		writeFile(t, bad, string(seg)+"."+tail+"\n")
		var stdout, stderr bytes.Buffer
		if err := run([]string{"license", "inspect", bad, "--key", pubFile}, &stdout, &stderr); err == nil {
			t.Fatal("payload-tampered document must fail inspection")
		}
	})

	t.Run("foreign key rejects", func(t *testing.T) {
		other := newKeygenFixture(t, "")
		var stdout, stderr bytes.Buffer
		err := run([]string{"license", "inspect", docFile, "--key", writePubFile(t, other.dir, other.pubHex)}, &stdout, &stderr)
		if err == nil || !strings.Contains(stdout.String(), "FAILED") {
			t.Fatalf("inspect under a foreign key: err = %v, stdout:\n%s", err, stdout.String())
		}
	})

	t.Run("default verifies against the embedded keys", func(t *testing.T) {
		var stdout, stderr bytes.Buffer
		err := run([]string{"license", "inspect", docFile}, &stdout, &stderr)
		if err == nil {
			t.Fatal("a pre-swap document must fail the embedded verdict")
		}
		if !strings.Contains(stdout.String(), "verification (embedded): FAILED") {
			t.Errorf("stdout should carry the embedded FAILED verdict:\n%s", stdout.String())
		}
	})

	t.Run("PEM private key refused for --key", func(t *testing.T) {
		var stdout, stderr bytes.Buffer
		err := run([]string{"license", "inspect", docFile, "--key", newKeygenFixture(t, "").key}, &stdout, &stderr)
		if err == nil || !strings.Contains(err.Error(), "inspection never uses a private key") {
			t.Fatalf("error = %v, want the private-key refusal", err)
		}
	})

	t.Run("malformed documents", func(t *testing.T) {
		tests := []struct {
			name string
			body string
			want string
		}{
			{name: "not a document", body: "not-a-license\n", want: "not a two-segment document"},
			{name: "three segments", body: "a.b.c\n", want: "not a two-segment document"},
			{name: "payload not base64url", body: "!!!.AAA\n", want: "payload segment is not base64url"},
			{name: "payload not JSON", body: base64.RawURLEncoding.EncodeToString([]byte("plain text")) + ".AAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAA\n", want: "payload is not JSON"},
		}
		for _, tt := range tests {
			t.Run(tt.name, func(t *testing.T) {
				p := filepath.Join(dir, "bad.lic")
				writeFile(t, p, tt.body)
				var stdout, stderr bytes.Buffer
				err := run([]string{"license", "inspect", p, "--key", pubFile}, &stdout, &stderr)
				if err == nil || !strings.Contains(err.Error(), tt.want) {
					t.Fatalf("error = %v, want it to contain %q", err, tt.want)
				}
			})
		}
	})

	t.Run("missing file and arity", func(t *testing.T) {
		var stdout, stderr bytes.Buffer
		if err := run([]string{"license", "inspect", filepath.Join(dir, "absent.lic")}, &stdout, &stderr); err == nil ||
			!strings.Contains(err.Error(), "reading document") {
			t.Fatalf("error = %v, want reading-document failure", err)
		}
		if err := run([]string{"license", "inspect"}, &stdout, &stderr); err == nil ||
			!strings.Contains(err.Error(), "exactly one <file>") {
			t.Fatalf("error = %v, want the arity error", err)
		}
	})
}

// ---------------------------------------------------------------------------
// end-to-end: keygen -> issue -> inspect -> REAL server stack loading
// ---------------------------------------------------------------------------

// e2eGate mirrors cmd/binflow-server's packageTypeGate (private there):
// the one adapter where the addon registry and the license Manager meet
// repo.Service's consumer-side seam.
type e2eGate struct {
	reg *addons.Registry
	ev  addons.Evaluator
}

func (g e2eGate) Verdict(ctx context.Context, packageType string) repo.PackageTypeVerdict {
	st, ok := g.reg.StatusOf(ctx, g.ev, packageType)
	if !ok {
		return repo.PackageTypeVerdict{}
	}
	v := repo.PackageTypeVerdict{Known: true, Unlocked: st.Enable}
	if !st.Enable {
		if state := g.ev.State(); state.Tier < st.Addon.MinTier {
			v.Refusal = fmt.Sprintf("license tier '%s' < '%s'", state.Tier, st.Addon.MinTier)
		} else {
			v.Refusal = "not named in the license addon allowlist"
		}
	}
	return v
}

// TestLicenseEndToEndServerInstall walks the full chain the conductor's
// acceptance sketches: keygen a pair, issue a pro document, place it in a
// license-dir (the BINFLOW_M10_LICENSE_DIR mount-point shape the tier
// matrix consumes), inspect it green, then install it on a REAL assembled
// httpapi stack — sqlite metadata, real auth/repo/storage, license.Manager
// verifying against the keygen public key (the post-first-issuance-swap
// world; the constructor seam T-279 mandated) — and watch the go addon
// gate flip from D3 refusal to create.
func TestLicenseEndToEndServerInstall(t *testing.T) {
	// 1. keygen + issue + inspect (the offline chain).
	doc, pubHex, dir := issuedDocForTest(t, "pro", 365)
	licenseDir := filepath.Join(dir, "license-dir")
	if err := os.Mkdir(licenseDir, 0o755); err != nil {
		t.Fatalf("mkdir license-dir: %v", err)
	}
	docFile := filepath.Join(licenseDir, "pro.lic")
	writeFile(t, docFile, doc+"\n")
	pubFile := writePubFile(t, dir, pubHex)

	var stdout, stderr bytes.Buffer
	if err := run([]string{"license", "inspect", docFile, "--key", pubFile}, &stdout, &stderr); err != nil {
		t.Fatalf("inspect before install: %v (stdout:\n%s)", err, stdout.String())
	}
	if !strings.Contains(stdout.String(), "verification ("+"--key "+pubFile+"): OK") {
		t.Fatalf("inspect did not verify green:\n%s", stdout.String())
	}
	pubKey := mustPubKey(t, pubHex)

	// 2. Assemble the server stack with the keygen public key embedded —
	// byte for byte the world after the verifykey.go first-issuance swap.
	ctx := context.Background()
	dataDir := t.TempDir()
	st, err := storage.OpenEngine(dataDir, storage.Options{})
	if err != nil {
		t.Fatalf("storage.OpenEngine: %v", err)
	}
	t.Cleanup(func() { _ = st.Close() })
	// AdminPassword rides metadata.Options (cmd reads BINFLOW_ADMIN_PASSWORD
	// into it; the store itself never touches the environment).
	md, err := metadata.Open(ctx, metadata.Options{Driver: "sqlite", Path: dataDir + "/binflow.db", AdminPassword: "t281-e2e-admin"})
	if err != nil {
		t.Fatalf("metadata.Open: %v", err)
	}
	t.Cleanup(func() { _ = md.Close() })

	cfg := config.Defaults()
	cfg.Storage.DataDir = dataDir
	authSvc := auth.NewFromStore(md, cfg.Security.AnonymousAccess)
	logger := slog.New(slog.NewTextHandler(io.Discard, nil))
	mgr, err := license.New(license.Options{
		Store:      md.Licenses(),
		VerifyKeys: map[string]ed25519.PublicKey{license.EmbeddedVerifyKeyID: pubKey},
		Audit:      audit.BestEffort(audit.New(md, true)),
		Log:        logger,
	})
	if err != nil {
		t.Fatalf("license.New: %v", err)
	}
	if err := mgr.Load(ctx); err != nil {
		t.Fatalf("license Load: %v", err)
	}

	reg := addons.New(
		addons.Generic(), addons.Docker(), addons.Maven(), addons.Npm(), addons.Pypi(),
		addons.Go(), addons.NuGet(), addons.Cargo(),
		addons.Properties(), addons.HA(), addons.XrayIntegration(),
	)
	svc := repo.New(st, md, authSvc, audit.New(md, true))
	repo.AttachPackageTypeGate(svc, e2eGate{reg: reg, ev: mgr})

	srv := httpapi.New(httpapi.Deps{
		Config:   cfg,
		Auth:     authSvc,
		Authz:    authSvc,
		Metadata: md,
		Repos:    md.Repos(),
		ReposSvc: svc,
		License:  mgr,
		Addons:   reg,
		DataDir:  dataDir,
	}, logger)
	ts := httptest.NewServer(srv.Handler())
	t.Cleanup(ts.Close)

	admin := &adminCreds{user: "admin", pass: "t281-e2e-admin"}

	// 3. Pre-install: the go slot is D3-refused at repo-create.
	code, body := doReq(t, ts, http.MethodPut, "/binflow/api/repositories/go-e2e",
		`{"rclass":"local","packageType":"go"}`, admin)
	if code != http.StatusBadRequest {
		t.Fatalf("go repo create before license: status %d, want 400 (D3), body %s", code, body)
	}
	// The refusal names the slot and the deciding clause (the '<' itself
	// arrives JSON-escaped as <, so assert the clause fragments).
	for _, want := range []string{"package type 'go'", "license tier 'community'", "'pro'"} {
		if !strings.Contains(body, want) {
			t.Errorf("D3 body missing %q: %s", want, body)
		}
	}

	// 4. Install the document through the real REST plane.
	code, body = doReq(t, ts, http.MethodPost, "/binflow/api/system/license", doc, admin)
	if code != http.StatusCreated {
		t.Fatalf("license install: status %d, want 201, body %s", code, body)
	}

	// 5. GET shows the pro grant.
	status := getLicenseStatus(t, ts, admin)
	if !status.Licensed || status.Tier != "pro" || status.Licensee != "Acme Corp" {
		t.Errorf("license status = %+v, want licensed pro for Acme Corp", status)
	}

	// 6. The go slot now creates (the gate flipped with the tier).
	code, body = doReq(t, ts, http.MethodPut, "/binflow/api/repositories/go-e2e",
		`{"rclass":"local","packageType":"go"}`, admin)
	if code != http.StatusOK {
		t.Fatalf("go repo create after license: status %d, want 200, body %s", code, body)
	}

	// 7. D7: a tampered install leaves the current license in force.
	head, tail, _ := strings.Cut(doc, ".")
	if tail[0] == 'A' {
		tail = "B" + tail[1:]
	} else {
		tail = "A" + tail[1:]
	}
	code, body = doReq(t, ts, http.MethodPost, "/binflow/api/system/license", head+"."+tail, admin)
	if code != http.StatusBadRequest {
		t.Fatalf("tampered install: status %d, want 400, body %s", code, body)
	}
	code, body = doReq(t, ts, http.MethodGet, "/binflow/api/system/license", "", admin)
	if code != http.StatusOK {
		t.Fatalf("post-tamper license GET: status %d, body %s", code, body)
	}
	if status := getLicenseStatus(t, ts, admin); !status.Licensed || status.Tier != "pro" {
		t.Fatalf("post-tamper license status = %+v, want pro intact (D7)", status)
	}

	// 8. Uninstall drops to the community floor and the go slot re-locks.
	code, body = doReq(t, ts, http.MethodDelete, "/binflow/api/system/license", "", admin)
	if code != http.StatusOK {
		t.Fatalf("license delete: status %d, want 200, body %s", code, body)
	}
	code, body = doReq(t, ts, http.MethodPut, "/binflow/api/repositories/go-e2e-2",
		`{"rclass":"local","packageType":"go"}`, admin)
	if code != http.StatusBadRequest || !strings.Contains(body, "license tier 'community'") {
		t.Fatalf("go repo create after uninstall: %d %s, want the D3 refusal back", code, body)
	}
}

// ---------------------------------------------------------------------------
// small helpers
// ---------------------------------------------------------------------------

type adminCreds struct{ user, pass string }

// licenseStatusJSON is the GET /api/system/license body shape the e2e
// asserts on.
type licenseStatusJSON struct {
	Licensed bool   `json:"licensed"`
	Tier     string `json:"tier"`
	Licensee string `json:"licensee"`
}

func getLicenseStatus(t *testing.T, ts *httptest.Server, admin *adminCreds) licenseStatusJSON {
	t.Helper()
	code, body := doReq(t, ts, http.MethodGet, "/binflow/api/system/license", "", admin)
	if code != http.StatusOK {
		t.Fatalf("license GET: status %d, want 200, body %s", code, body)
	}
	var st licenseStatusJSON
	if err := json.Unmarshal([]byte(body), &st); err != nil {
		t.Fatalf("license GET body not JSON: %v (%s)", err, body)
	}
	return st
}

func doReq(t *testing.T, ts *httptest.Server, method, path, body string, creds *adminCreds) (int, string) {
	t.Helper()
	var rd io.Reader
	if body != "" {
		rd = strings.NewReader(body)
	}
	req, err := http.NewRequest(method, ts.URL+path, rd)
	if err != nil {
		t.Fatalf("new request %s %s: %v", method, path, err)
	}
	req.SetBasicAuth(creds.user, creds.pass)
	if body != "" {
		req.Header.Set("Content-Type", "application/json")
	}
	resp, err := ts.Client().Do(req)
	if err != nil {
		t.Fatalf("do %s %s: %v", method, path, err)
	}
	defer func() { _ = resp.Body.Close() }()
	raw, _ := io.ReadAll(resp.Body)
	return resp.StatusCode, string(raw)
}

func writePubFile(t *testing.T, dir, pubHex string) string {
	t.Helper()
	p := filepath.Join(dir, "pub.hex")
	writeFile(t, p, pubHex+"\n")
	return p
}

func mustPubKey(t *testing.T, hexStr string) ed25519.PublicKey {
	t.Helper()
	raw, err := hex.DecodeString(strings.TrimSpace(hexStr))
	if err != nil || len(raw) != ed25519.PublicKeySize {
		t.Fatalf("public key hex %q: err %v, len %d", hexStr, err, len(raw))
	}
	return ed25519.PublicKey(raw)
}

// decodeDocForTest splits and decodes a document's payload for field
// assertions (the test-side twin of the CLI's display decode).
func decodeDocForTest(t *testing.T, doc string) licenseDocDisplay {
	t.Helper()
	d, err := decodeLicenseDocument(doc)
	if err != nil {
		t.Fatalf("decode document: %v", err)
	}
	return d
}
