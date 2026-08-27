package deb

// T-321 acceptance (docs/design/gpg-keypair.md section 3.6, debian.md
// section 4.2): the debian signing leg. The local index engine's Release
// recomputes materialize the InRelease (clearsign) + Release.gpg
// (detached armor) pair through the REAL keypair plane (the harness
// assembles keypair.SigningService over the stack's own rows and enc:v1
// cipher — the same collaborators cmd wires), the sweep clears stale
// signatures on dissociation (DB-1), rotation re-signs, the error
// taxonomy drives the unsigned posture, and the virtual aggregate serves
// its own signature family only when its seam resolves a key. The
// library's own openpgp checkers verify every signature against the
// served public key; the real gpg client leg lives at the bottom (skip
// without the binary) and the real apt container leg in client_e2e_test.

import (
	"context"
	"errors"
	"fmt"
	"net/http"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"github.com/ProtonMail/go-crypto/openpgp"
	"github.com/ProtonMail/go-crypto/openpgp/armor"
	pgpclearsign "github.com/ProtonMail/go-crypto/openpgp/clearsign"

	"github.com/lzwzzy/binflow/internal/keypair"
	"github.com/lzwzzy/binflow/internal/metadata"
	"github.com/lzwzzy/binflow/internal/repo"
)

// generateSignKey generates one pair through the REAL manager face
// (RSA-2048 for speed — the 4096 default is the plane's own unit pin)
// and returns its served public key.
func (s *stack) generateSignKey(t *testing.T, name string) string {
	t.Helper()
	summary, err := s.keypairs.Generate(context.Background(), keypair.GenerateInput{
		PairName:   name,
		Alias:      name,
		KeyBits:    2048,
		Passphrase: "t321-passphrase",
	}, "root")
	if err != nil {
		t.Fatalf("Generate(%q): %v", name, err)
	}
	return summary.PublicKey
}

// keyRing parses one armored public key.
func keyRing(t *testing.T, publicArmored string) openpgp.EntityList {
	t.Helper()
	ring, err := openpgp.ReadArmoredKeyRing(strings.NewReader(publicArmored))
	if err != nil || len(ring) == 0 {
		t.Fatalf("public key parse: %v", err)
	}
	return ring
}

// signTestEnv is the signed-repository setup every local leg starts from:
// one generated pair, one associated local repository, one indexed
// package.
func signTestEnv(t *testing.T, pairName string) (*stack, string, string) {
	t.Helper()
	s := newStack(t)
	public := s.generateSignKey(t, pairName)
	s.seedRepo(t, "deb-signed", repo.TypeLocal, `{"keyPairName":"`+pairName+`"}`)
	pkg := helloDeb("signpkg", "1.0", "amd64")
	if status, body, _ := s.debPut(t, "/binflow/deb-signed/pool/main/s/signpkg/signpkg_1.0_amd64.deb",
		pkg, "stable", []string{"main"}, []string{"amd64"}); status != http.StatusCreated {
		t.Fatalf("debPUT = (%d, %s)", status, body)
	}
	// The signature pair lands AFTER the Release (the signing step follows
	// the Release write, InRelease then Release.gpg) — poll the pair, the
	// chain's last artifacts, not the Release.
	s.waitIndex(t, "/binflow/deb-signed/dists/stable/InRelease")
	s.waitIndex(t, "/binflow/deb-signed/dists/stable/Release.gpg")
	release := s.waitIndex(t, "/binflow/deb-signed/dists/stable/Release")
	return s, public, release
}

// TestReleaseSigningServesVerifiablePair: the automatic recompute
// materializes the signature pair beside the Release, both forms
// verifying against the served public key over the wire bytes (the
// section 7 chain's signing head), and the signature binds the bytes (a
// tampered Release must fail).
func TestReleaseSigningServesVerifiablePair(t *testing.T) {
	s, public, release := signTestEnv(t, "t321-key")
	ring := keyRing(t, public)

	status, inRelease, hdr := s.get("/binflow/deb-signed/dists/stable/InRelease")
	if status != http.StatusOK {
		t.Fatalf("InRelease = %d, want 200 (the pair is the signing leg's primary face)", status)
	}
	if ct := hdr.Get("Content-Type"); ct != ctypeInRelease {
		t.Errorf("InRelease Content-Type = %q, want %q", ct, ctypeInRelease)
	}
	status, releaseGpg, hdr := s.get("/binflow/deb-signed/dists/stable/Release.gpg")
	if status != http.StatusOK {
		t.Fatalf("Release.gpg = %d, want 200", status)
	}
	if ct := hdr.Get("Content-Type"); ct != ctypeReleaseGpg {
		t.Errorf("Release.gpg Content-Type = %q, want %q", ct, ctypeReleaseGpg)
	}

	// InRelease: the cleartext form — plaintext equals the served Release
	// body (modulo the trailing newline the framework normalizes) and the
	// embedded signature verifies.
	block, rest := pgpclearsign.Decode([]byte(inRelease))
	if block == nil {
		t.Fatalf("InRelease is not a cleartext signature (head %q)", inRelease[:min(60, len(inRelease))])
	}
	if len(rest) != 0 {
		t.Errorf("InRelease carries %d trailing bytes after the signature block", len(rest))
	}
	if got, want := string(block.Plaintext), strings.TrimRight(release, "\n"); strings.TrimRight(got, "\n") != want {
		t.Errorf("InRelease plaintext ≠ the served Release:\n%q\n%q", got, want)
	}
	if _, err := block.VerifySignature(ring, nil); err != nil {
		t.Fatalf("InRelease signature verification against the served public key: %v", err)
	}

	// Release.gpg: the detached form — verifies against the Release bytes
	// served under the canonical name.
	armorBlock, err := armor.Decode(strings.NewReader(releaseGpg))
	if err != nil {
		t.Fatalf("Release.gpg is not an armored block: %v", err)
	}
	if _, err := openpgp.CheckDetachedSignature(ring, strings.NewReader(release), armorBlock.Body, nil); err != nil {
		t.Fatalf("Release.gpg verification against the served Release: %v", err)
	}
	// The signature binds bytes: a tampered Release must fail the same check.
	if _, err := openpgp.CheckDetachedSignature(ring,
		strings.NewReader(release+"Origin: evil\n"), armorBlock.Body, nil); err == nil {
		t.Fatal("a tampered Release verified — the signature does not bind the bytes")
	}
}

// TestReleaseSigningSweepAndRotation: dissociating the repository (the
// config update the v2 DELETE face writes) sweeps BOTH signature files on
// the next recompute (DB-1 — a stale signature must not outlive the
// Release it signed), and re-associating a DIFFERENT pair re-signs (the
// rotation path of spec section 2.5).
func TestReleaseSigningSweepAndRotation(t *testing.T) {
	s, publicA, _ := signTestEnv(t, "t321-key-a")

	dissociate := func(pair string) {
		t.Helper()
		if err := s.md.Repos().Update(context.Background(), repoRow("deb-signed", pair)); err != nil {
			t.Fatalf("update repo config: %v", err)
		}
	}
	reindex := func() {
		t.Helper()
		if status, body, _ := s.post("/binflow/api/deb/reindex/deb-signed?async=0"); status != http.StatusOK {
			t.Fatalf("reindex = (%d, %s)", status, body)
		}
	}

	// Dissociation → the sweep clears both files; the Release stays.
	dissociate("")
	reindex()
	for _, path := range []string{
		"/binflow/deb-signed/dists/stable/InRelease",
		"/binflow/deb-signed/dists/stable/Release.gpg",
	} {
		if status, _, _ := s.get(path); status != http.StatusNotFound {
			t.Errorf("after dissociation %s = %d, want 404 (DB-1: the stale signature must not survive)", path, status)
		}
	}
	if status, _, _ := s.get("/binflow/deb-signed/dists/stable/Release"); status != http.StatusOK {
		t.Errorf("Release after dissociation = %d, want 200 (unsigned mode serves the Release)", status)
	}

	// Rotation: a different pair re-signs, and the new InRelease verifies
	// against the NEW key only.
	publicB := s.generateSignKey(t, "t321-key-b")
	dissociate("t321-key-b")
	reindex()
	status, inRelease, _ := s.get("/binflow/deb-signed/dists/stable/InRelease")
	if status != http.StatusOK {
		t.Fatalf("InRelease after rotation = %d, want 200", status)
	}
	block, _ := pgpclearsign.Decode([]byte(inRelease))
	if block == nil {
		t.Fatal("rotated InRelease is not a cleartext signature")
	}
	if _, err := block.VerifySignature(keyRing(t, publicB), nil); err != nil {
		t.Fatalf("rotated InRelease must verify against the new key: %v", err)
	}
	if _, err := block.VerifySignature(keyRing(t, publicA), nil); err == nil {
		t.Fatal("rotated InRelease verified against the OLD key — rotation did not re-sign")
	}
}

// repoRow renders one local debian repository row carrying (optionally)
// the keypair association — the raw-seeded config shape the metadata
// upsert accepts (the validation matrix is the repo layer's own test
// face).
func repoRow(key, pair string) *metadata.Repo {
	return &metadata.Repo{RepoKey: key, Type: repo.TypeLocal, PackageType: Protocol,
		Config: `{"keyPairName":"` + pair + `"}`}
}

// fakeSigner drives the taxonomy legs: the seam's answers are injected
// verbatim (the classification under test is the deb leg's, not the
// keypair plane's — that one has its own suite).
type fakeSigner struct {
	clearErr  error
	detachErr error
}

func (f fakeSigner) DetachedArmor(context.Context, string, []byte) (string, error) {
	return "", f.detachErr
}

func (f fakeSigner) Clearsign(context.Context, string, []byte) ([]byte, error) {
	return nil, f.clearErr
}

// TestReleaseSigningErrorTaxonomy: the seam's error table (gpg-keypair.md
// 3.6) drives the leg — the two sentinels degrade to the unsigned posture
// (recompute succeeds, no signature files), anything else surfaces as the
// recompute's own failure. The sentinels arrive WRAPPED, the shape the
// concrete seam answers (resolve wraps with the repository/key name).
func TestReleaseSigningErrorTaxonomy(t *testing.T) {
	cases := []struct {
		name    string
		signer  ReleaseSigner
		want500 bool
	}{
		{name: "no key pair", signer: fakeSigner{clearErr: fmt.Errorf(`repository "deb-tax": %w`, keypair.ErrNoKeypair)}},
		{name: "unavailable pair", signer: fakeSigner{clearErr: fmt.Errorf(`keypair "k": %w`, keypair.ErrUnavailable)}},
		{name: "signing execution failure", signer: fakeSigner{clearErr: errors.New("openpgp exploded")}, want500: true},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			s := newStackOpt(t, stackOptions{signer: tc.signer})
			s.seedRepo(t, "deb-tax", repo.TypeLocal, `{}`)
			pkg := helloDeb("taxpkg", "1.0", "amd64")
			if status, body, _ := s.debPut(t, "/binflow/deb-tax/pool/main/t/taxpkg/taxpkg_1.0_amd64.deb",
				pkg, "stable", []string{"main"}, []string{"amd64"}); status != http.StatusCreated {
				t.Fatalf("debPUT = (%d, %s)", status, body)
			}
			s.waitIndex(t, "/binflow/deb-tax/dists/stable/Release")
			status, body, _ := s.post("/binflow/api/deb/reindex/deb-tax?async=0")
			if tc.want500 {
				if status != http.StatusInternalServerError {
					t.Fatalf("reindex = %d, want 500 (the signing failure surfaces)", status)
				}
				if !strings.Contains(body, "sign InRelease") {
					t.Errorf("reindex 500 body does not carry the signing context: %q", body)
				}
				return
			}
			if status != http.StatusOK {
				t.Fatalf("reindex = (%d, %s), want 200 (the unsigned posture never fails the recompute)", status, body)
			}
			for _, path := range []string{
				"/binflow/deb-tax/dists/stable/InRelease",
				"/binflow/deb-tax/dists/stable/Release.gpg",
			} {
				if st, _, _ := s.get(path); st != http.StatusNotFound {
					t.Errorf("%s = %d, want 404 (unsigned posture)", path, st)
				}
			}
		})
	}
}

// TestVirtualReleaseRenderIsDeterministic: the aggregate Release renders
// byte-identically across requests within one member generation (the
// Date rides the newest member's own Release Date, not the render
// clock) — the property the detached-signature fallback and the
// Release/Packages checksum chain depend on when the faces are fetched
// in separate requests. The sleep crosses a second boundary, where the
// render-clock spelling would move.
func TestVirtualReleaseRenderIsDeterministic(t *testing.T) {
	s := newStack(t)
	s.seedRepo(t, "deb-det", repo.TypeLocal, `{}`)
	pkg := helloDeb("detpkg", "1.0", "amd64")
	if status, body, _ := s.debPut(t, "/binflow/deb-det/pool/main/d/detpkg/detpkg_1.0_amd64.deb",
		pkg, "stable", []string{"main"}, []string{"amd64"}); status != http.StatusCreated {
		t.Fatalf("debPUT = (%d, %s)", status, body)
	}
	s.waitIndex(t, "/binflow/deb-det/dists/stable/Release")
	s.seedVirtualRepo(t, "deb-virt-det", []string{"deb-det"}, "")

	first := mustGet(t, s, "/binflow/deb-virt-det/dists/stable/Release")
	if !strings.Contains(first, "Date: ") {
		t.Fatalf("aggregate Release carries no Date field:\n%s", first)
	}
	time.Sleep(1100 * time.Millisecond)
	second := mustGet(t, s, "/binflow/deb-virt-det/dists/stable/Release")
	if first != second {
		t.Fatal("two GETs of the same aggregate generation rendered different bytes — the Date must ride the member Release, not the render clock")
	}
}

// TestVirtualSignatureFaces: the virtual aggregate serves its own
// signature family ONLY when its seam resolves a key. A managed-path
// virtual carries no association (the repo config validation refuses
// keyPairName on non-local classes — seeded the managed shape here), so
// InRelease/Release.gpg answer 404 even with SIGNED members (a member's
// signature covers the member's Release, never the re-render); a
// hand-seeded association (the unmanaged shape) signs the aggregate's
// own bytes.
func TestVirtualSignatureFaces(t *testing.T) {
	s := newStack(t)
	public := s.generateSignKey(t, "t321-virt")
	s.seedRepo(t, "deb-member", repo.TypeLocal, `{"keyPairName":"t321-virt"}`)
	pkg := helloDeb("virtpkg", "1.0", "amd64")
	if status, body, _ := s.debPut(t, "/binflow/deb-member/pool/main/v/virtpkg/virtpkg_1.0_amd64.deb",
		pkg, "stable", []string{"main"}, []string{"amd64"}); status != http.StatusCreated {
		t.Fatalf("debPUT = (%d, %s)", status, body)
	}
	memberRelease := s.waitIndex(t, "/binflow/deb-member/dists/stable/Release")
	s.waitIndex(t, "/binflow/deb-member/dists/stable/InRelease") // the chain's last artifacts (see signTestEnv)
	s.waitIndex(t, "/binflow/deb-member/dists/stable/Release.gpg")

	// The managed-shape virtual: members signed, the virtual unsigned.
	s.seedVirtualRepo(t, "deb-virt", []string{"deb-member"}, "")
	aggReleasePath := "/binflow/deb-virt/dists/stable/Release"
	s.waitIndex(t, aggReleasePath) // served on demand; the poll just proves the member chain
	for _, path := range []string{"/binflow/deb-virt/dists/stable/InRelease", "/binflow/deb-virt/dists/stable/Release.gpg"} {
		if status, _, _ := s.get(path); status != http.StatusNotFound {
			t.Errorf("%s = %d, want 404 (the aggregate carries no association)", path, status)
		}
	}

	// The unmanaged shape: the association seeded straight into the row
	// (the config validation is the repo layer's face — bypassed here the
	// way a hand-edited store would).
	if err := s.md.Repos().Update(context.Background(), &metadata.Repo{
		RepoKey: "deb-virt", Type: repo.TypeVirtual, PackageType: Protocol,
		Config: `{"repositories":["deb-member"],"keyPairName":"t321-virt"}`,
	}); err != nil {
		t.Fatalf("update virtual config: %v", err)
	}
	status, aggRelease, _ := s.get(aggReleasePath)
	if status != http.StatusOK {
		t.Fatalf("virtual Release = %d", status)
	}
	if aggRelease == memberRelease {
		t.Fatal("the aggregate Release must differ from the member's (it is the re-render)")
	}
	ring := keyRing(t, public)

	status, inRelease, _ := s.get("/binflow/deb-virt/dists/stable/InRelease")
	if status != http.StatusOK {
		t.Fatalf("virtual InRelease (associated) = %d, want 200", status)
	}
	block, _ := pgpclearsign.Decode([]byte(inRelease))
	if block == nil {
		t.Fatal("virtual InRelease is not a cleartext signature")
	}
	if _, err := block.VerifySignature(ring, nil); err != nil {
		t.Fatalf("virtual InRelease verification: %v", err)
	}
	if got := string(block.Plaintext); strings.TrimRight(got, "\n") != strings.TrimRight(aggRelease, "\n") {
		t.Errorf("virtual InRelease signs the wrong bytes (plaintext ≠ the aggregate Release)")
	}

	status, releaseGpg, _ := s.get("/binflow/deb-virt/dists/stable/Release.gpg")
	if status != http.StatusOK {
		t.Fatalf("virtual Release.gpg (associated) = %d, want 200", status)
	}
	armorBlock, err := armor.Decode(strings.NewReader(releaseGpg))
	if err != nil {
		t.Fatalf("virtual Release.gpg is not armored: %v", err)
	}
	if _, err := openpgp.CheckDetachedSignature(ring, strings.NewReader(aggRelease), armorBlock.Body, nil); err != nil {
		t.Fatalf("virtual Release.gpg verification against the aggregate Release: %v", err)
	}
	// And it must NOT verify against a member's Release (it signs the
	// aggregate's bytes, never a member's).
	if _, err := openpgp.CheckDetachedSignature(ring, strings.NewReader(memberRelease), armorBlock.Body, nil); err == nil {
		t.Fatal("the aggregate signature verified against the member's Release — wrong bytes signed")
	}
}

// TestReleaseSigningGPGClientRoundTrip: the REAL client leg of the
// library-verified faces — the served InRelease / Release.gpg pair
// round-trips through gpg itself (the exact verifier apt drives) against
// the public key the plane serves. Skips without the binary.
func TestReleaseSigningGPGClientRoundTrip(t *testing.T) {
	bin := gpgBinOf(t)
	s, public, release := signTestEnv(t, "t321-gpg")
	inRelease := mustGet(t, s, "/binflow/deb-signed/dists/stable/InRelease")
	releaseGpg := mustGet(t, s, "/binflow/deb-signed/dists/stable/Release.gpg")

	home := gpgHomeDirT321(t)
	write := func(name string, body string) string {
		t.Helper()
		path := filepath.Join(home, name)
		if err := os.WriteFile(path, []byte(body), 0o600); err != nil {
			t.Fatalf("write %s: %v", name, err)
		}
		return path
	}
	pubFile := write("public.asc", public)
	relFile := write("Release", release)
	inFile := write("InRelease", inRelease)
	sigFile := write("Release.gpg", releaseGpg)

	if out, err := runGPGT321(t, bin, home, "--batch", "--import", pubFile); err != nil {
		t.Fatalf("gpg --import: %v\n%s", err, out)
	}
	if out, err := runGPGT321(t, bin, home, "--batch", "--verify", inFile); err != nil {
		t.Fatalf("gpg --verify InRelease: %v\n%s", err, out)
	}
	if out, err := runGPGT321(t, bin, home, "--batch", "--verify", sigFile, relFile); err != nil {
		t.Fatalf("gpg --verify Release.gpg Release: %v\n%s", err, out)
	}
}

// mustGet fetches a body that must answer 200.
func mustGet(t *testing.T, s *stack, path string) string {
	t.Helper()
	status, body, _ := s.get(path)
	if status != http.StatusOK {
		t.Fatalf("GET %s = %d", path, status)
	}
	return body
}

// gpgBinOf resolves the client or skips.
func gpgBinOf(t *testing.T) string {
	t.Helper()
	for _, name := range []string{"gpg", "gpg2"} {
		if path, err := exec.LookPath(name); err == nil {
			return path
		}
	}
	t.Skip("no gpg client on PATH — the interop leg needs GnuPG")
	return ""
}

// gpgHomeDirT321 makes a SHORT GNUPGHOME (the socket name limit the
// testing temp tree overflows — the keypair suite's posture).
func gpgHomeDirT321(t *testing.T) string {
	t.Helper()
	dir, err := os.MkdirTemp("/tmp", "t321-")
	if err != nil {
		t.Fatalf("mktemp gpg home: %v", err)
	}
	t.Cleanup(func() { _ = os.RemoveAll(dir) })
	if err := os.Chmod(dir, 0o700); err != nil {
		t.Fatalf("chmod gpg home: %v", err)
	}
	return dir
}

// runGPGT321 runs gpg with an isolated, non-tty home.
func runGPGT321(t *testing.T, bin, home string, args ...string) (string, error) {
	t.Helper()
	cmd := exec.Command(bin, args...)
	cmd.Env = append(os.Environ(), "GNUPGHOME="+home)
	cmd.Dir = home
	out, err := cmd.CombinedOutput()
	return string(out), err
}
