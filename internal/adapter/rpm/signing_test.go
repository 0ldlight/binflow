package rpm

// T-322 acceptance (docs/design/gpg-keypair.md section 3.6, rpm.md
// section 4.3): the rpm signing leg. The local reindex engine's repomd
// writes materialize the repomd.xml.asc (detached armor) + repomd.xml.key
// (armored public key) pair through the REAL keypair plane (the harness
// assembles keypair.SigningService over the stack's own rows and enc:v1
// cipher — the same collaborators cmd wires), the sweep clears the stale
// pair on dissociation (rpm.md 4.3), rotation re-signs at the fixed
// names, the error taxonomy drives the unsigned posture, and the virtual
// aggregate stays unsigned even over signed members (RP-3 / T-315 D-3).
// The library's own openpgp checkers verify every signature against the
// served public key; the real gpg client leg lives at the bottom (skip
// without the binary) and the real dnf container leg in
// client_e2e_test.go.

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
		Passphrase: "t322-passphrase",
	}, "root")
	if err != nil {
		t.Fatalf("Generate(%q): %v", name, err)
	}
	return summary.PublicKey
}

// signKeyRing parses one armored public key.
func signKeyRing(t *testing.T, publicArmored string) openpgp.EntityList {
	t.Helper()
	ring, err := openpgp.ReadArmoredKeyRing(strings.NewReader(publicArmored))
	if err != nil || len(ring) == 0 {
		t.Fatalf("public key parse: %v", err)
	}
	return ring
}

// rpmRepoRow renders one local rpm repository row carrying (optionally)
// the keypair association — the raw-seeded config shape the metadata
// upsert accepts (the validation matrix is the repo layer's own test
// face).
func rpmRepoRow(key, config string) *metadata.Repo {
	return &metadata.Repo{RepoKey: key, Type: repo.TypeLocal, PackageType: Protocol, Config: config}
}

// reindexSync fires the synchronous whole-repository recompute (the
// /api/yum face; the seeded configs leave calculateYumMetadata off, so
// the synchronous branch is reachable).
func (s *stack) reindexSync(t *testing.T, repoKey string) {
	t.Helper()
	if status, body, _ := s.post("/binflow/api/yum/" + repoKey + "?async=0"); status != http.StatusOK {
		t.Fatalf("reindex %s = (%d, %s)", repoKey, status, body)
	}
}

// seedSignedRepo seeds one local repository with the association and one
// indexed package, then recomputes synchronously — the signed state every
// leg starts from. Returns the served repomd.xml body.
func (s *stack) seedSignedRepo(t *testing.T, key, pairName, pkgName string) string {
	t.Helper()
	s.seedRepo(t, key, repo.TypeLocal, `{"keyPairName":"`+pairName+`"}`)
	pkg := pkgFixture(pkgName, "1.0.0", "1.el9", "noarch")
	if status, body, _ := s.put("/binflow/"+key+"/"+pkgName+"-1.0.0-1.el9.noarch.rpm", pkg, nil); status != http.StatusCreated {
		t.Fatalf("rpmPUT = (%d, %s)", status, body)
	}
	s.reindexSync(t, key)
	status, repomd, _ := s.get("/binflow/" + key + "/repodata/repomd.xml")
	if status != http.StatusOK {
		t.Fatalf("repomd = %d, want 200 (the recompute must succeed before the pair)", status)
	}
	return repomd
}

// TestRepomdSigningServesVerifiablePair: the recompute materializes the
// signature pair beside the repomd.xml, the detached form verifying
// against the served public key over the wire bytes (the repo_gpgcheck
// chain's signing head), and the signature binds the bytes (a tampered
// repomd must fail).
func TestRepomdSigningServesVerifiablePair(t *testing.T) {
	s := newStack(t)
	public := s.generateSignKey(t, "t322-key")
	repomd := s.seedSignedRepo(t, "rpm-signed", "t322-key", "signpkg")
	ring := signKeyRing(t, public)

	status, asc, hdr := s.get("/binflow/rpm-signed/repodata/repomd.xml.asc")
	if status != http.StatusOK {
		t.Fatalf("repomd.xml.asc = %d, want 200 (the pair is the signing leg's primary face)", status)
	}
	if ct := hdr.Get("Content-Type"); ct != ctypeRepomdSignature {
		t.Errorf("repomd.xml.asc Content-Type = %q, want %q", ct, ctypeRepomdSignature)
	}
	status, keyBody, hdr := s.get("/binflow/rpm-signed/repodata/repomd.xml.key")
	if status != http.StatusOK {
		t.Fatalf("repomd.xml.key = %d, want 200", status)
	}
	if ct := hdr.Get("Content-Type"); ct != ctypeRepomdSignature {
		t.Errorf("repomd.xml.key Content-Type = %q, want %q", ct, ctypeRepomdSignature)
	}
	// The served key IS the associated pair's public half — what a client
	// would import for gpgkey=.
	if got, want := strings.TrimSpace(keyBody), strings.TrimSpace(public); got != want {
		t.Errorf("repomd.xml.key does not carry the associated pair's public key (len %d vs %d)", len(got), len(want))
	}

	// The detached form verifies against the repomd bytes served under the
	// canonical name — dnf's exact check.
	armorBlock, err := armor.Decode(strings.NewReader(asc))
	if err != nil {
		t.Fatalf("repomd.xml.asc is not an armored block: %v", err)
	}
	if _, err := openpgp.CheckDetachedSignature(ring, strings.NewReader(repomd), armorBlock.Body, nil); err != nil {
		t.Fatalf("repomd.xml.asc verification against the served repomd: %v", err)
	}
	// The signature binds bytes: a tampered repomd must fail the same check.
	if _, err := openpgp.CheckDetachedSignature(ring,
		strings.NewReader(repomd+"\n<!-- evil -->"), armorBlock.Body, nil); err == nil {
		t.Fatal("a tampered repomd verified — the signature does not bind the bytes")
	}
}

// TestRepomdSigningSweepAndRotation: dissociating the repository (the
// config update the v2 DELETE face writes) sweeps BOTH signature files on
// the next recompute (rpm.md 4.3 — a stale signature must not outlive the
// repomd.xml it signed), and re-associating a DIFFERENT pair re-signs at
// the same fixed names (the rotation path).
func TestRepomdSigningSweepAndRotation(t *testing.T) {
	s := newStack(t)
	publicA := s.generateSignKey(t, "t322-key-a")
	s.seedSignedRepo(t, "rpm-signed", "t322-key-a", "signpkg")

	reconfigure := func(config string) {
		t.Helper()
		if err := s.md.Repos().Update(context.Background(), rpmRepoRow("rpm-signed", config)); err != nil {
			t.Fatalf("update repo config: %v", err)
		}
	}

	// Dissociation → the sweep clears both files; the repomd.xml stays.
	reconfigure("{}")
	s.reindexSync(t, "rpm-signed")
	for _, path := range []string{
		"/binflow/rpm-signed/repodata/repomd.xml.asc",
		"/binflow/rpm-signed/repodata/repomd.xml.key",
	} {
		if status, _, _ := s.get(path); status != http.StatusNotFound {
			t.Errorf("after dissociation %s = %d, want 404 (rpm.md 4.3: the stale signature must not survive)", path, status)
		}
	}
	if status, _, _ := s.get("/binflow/rpm-signed/repodata/repomd.xml"); status != http.StatusOK {
		t.Errorf("repomd.xml after dissociation = %d, want 200 (unsigned mode still serves the index)", status)
	}

	// Rotation: a different pair re-signs, and the new .asc verifies
	// against the NEW key only.
	publicB := s.generateSignKey(t, "t322-key-b")
	reconfigure(`{"keyPairName":"t322-key-b"}`)
	s.reindexSync(t, "rpm-signed")
	status, asc, _ := s.get("/binflow/rpm-signed/repodata/repomd.xml.asc")
	if status != http.StatusOK {
		t.Fatalf("repomd.xml.asc after rotation = %d, want 200", status)
	}
	_, repomd, _ := s.get("/binflow/rpm-signed/repodata/repomd.xml")
	armorBlock, err := armor.Decode(strings.NewReader(asc))
	if err != nil {
		t.Fatalf("rotated .asc is not armored: %v", err)
	}
	if _, err := openpgp.CheckDetachedSignature(signKeyRing(t, publicB), strings.NewReader(repomd), armorBlock.Body, nil); err != nil {
		t.Fatalf("rotated .asc must verify against the new key: %v", err)
	}
	if _, err := openpgp.CheckDetachedSignature(signKeyRing(t, publicA), strings.NewReader(repomd), armorBlock.Body, nil); err == nil {
		t.Fatal("rotated .asc verified against the OLD key — rotation did not re-sign")
	}
}

// fakeSigner drives the taxonomy legs: the seam's answers are injected
// verbatim (the classification under test is the rpm leg's, not the
// keypair plane's — that one has its own suite).
type fakeSigner struct {
	detachErr error
	pubErr    error
}

func (f fakeSigner) DetachedArmor(context.Context, string, []byte) (string, error) {
	return "", f.detachErr
}

func (f fakeSigner) PublicKey(context.Context, string) (string, error) {
	return "", f.pubErr
}

// TestRepomdSigningErrorTaxonomy: the seam's error table (gpg-keypair.md
// 3.6) drives the leg — the two sentinels degrade to the unsigned
// posture (recompute succeeds, no signature files), anything else
// surfaces as the recompute's own failure. The sentinels arrive WRAPPED,
// the shape the concrete seam answers (resolve wraps with the
// repository/key name).
func TestRepomdSigningErrorTaxonomy(t *testing.T) {
	cases := []struct {
		name    string
		signer  RepomdSigner
		want500 bool
	}{
		{name: "no key pair", signer: fakeSigner{detachErr: fmt.Errorf(`repository "rpm-tax": %w`, keypair.ErrNoKeypair)}},
		{name: "unavailable pair", signer: fakeSigner{detachErr: fmt.Errorf(`keypair "k": %w`, keypair.ErrUnavailable)}},
		{name: "public key read fails unclassified", signer: fakeSigner{pubErr: errors.New("store exploded")}, want500: true},
		{name: "signing execution failure", signer: fakeSigner{detachErr: errors.New("openpgp exploded")}, want500: true},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			s := newStackOpt(t, stackOptions{dataDir: t.TempDir(), signer: tc.signer})
			s.seedRepo(t, "rpm-tax", repo.TypeLocal, "{}")
			pkg := pkgFixture("taxpkg", "1.0.0", "1.el9", "noarch")
			if status, body, _ := s.put("/binflow/rpm-tax/taxpkg-1.0.0-1.el9.noarch.rpm", pkg, nil); status != http.StatusCreated {
				t.Fatalf("rpmPUT = (%d, %s)", status, body)
			}
			status, body, _ := s.post("/binflow/api/yum/rpm-tax?async=0")
			if tc.want500 {
				if status != http.StatusInternalServerError {
					t.Fatalf("reindex = %d, want 500 (the signing failure surfaces)", status)
				}
				if !strings.Contains(body, "sign repomd.xml") && !strings.Contains(body, "read public key") {
					t.Errorf("reindex 500 body does not carry the signing context: %q", body)
				}
				return
			}
			if status != http.StatusOK {
				t.Fatalf("reindex = (%d, %s), want 200 (the unsigned posture never fails the recompute)", status, body)
			}
			for _, path := range []string{
				"/binflow/rpm-tax/repodata/repomd.xml.asc",
				"/binflow/rpm-tax/repodata/repomd.xml.key",
			} {
				if st, _, _ := s.get(path); st != http.StatusNotFound {
					t.Errorf("%s = %d, want 404 (unsigned posture)", path, st)
				}
			}
		})
	}
}

// TestRepomdSigningUploadAutoRecompute: the opt-in upload chain
// (calculateYumMetadata=true, RP-2) signs through its BACKGROUND
// recompute too — the pair is the chain's last write, so the poll waits
// on it (not the repomd.xml).
func TestRepomdSigningUploadAutoRecompute(t *testing.T) {
	s := newStack(t)
	public := s.generateSignKey(t, "t322-auto")
	s.seedRepo(t, "rpm-auto", repo.TypeLocal, `{"calculateYumMetadata":true,"keyPairName":"t322-auto"}`)
	pkg := pkgFixture("autopkg", "1.0.0", "1.el9", "noarch")
	if status, body, _ := s.put("/binflow/rpm-auto/autopkg-1.0.0-1.el9.noarch.rpm", pkg, nil); status != http.StatusCreated {
		t.Fatalf("rpmPUT = (%d, %s)", status, body)
	}

	deadline := time.Now().Add(15 * time.Second)
	var asc, repomd string
	for time.Now().Before(deadline) {
		stA, a, _ := s.get("/binflow/rpm-auto/repodata/repomd.xml.asc")
		stR, r, _ := s.get("/binflow/rpm-auto/repodata/repomd.xml")
		if stA == http.StatusOK && stR == http.StatusOK {
			asc, repomd = a, r
			break
		}
		time.Sleep(50 * time.Millisecond)
	}
	if asc == "" {
		t.Fatal("the background recompute never wrote repomd.xml.asc (15s deadline)")
	}
	armorBlock, err := armor.Decode(strings.NewReader(asc))
	if err != nil {
		t.Fatalf("auto-chain .asc is not armored: %v", err)
	}
	if _, err := openpgp.CheckDetachedSignature(signKeyRing(t, public), strings.NewReader(repomd), armorBlock.Body, nil); err != nil {
		t.Fatalf("auto-chain .asc verification: %v", err)
	}
}

// TestVirtualRepomdUnsignedWithSignedMember: the virtual aggregate never
// serves a signature pair — not its own (a virtual cannot carry the
// association) and never a MEMBER's (a member's signature covers the
// member's repomd, not the merged document) — the T-315 D-3 posture,
// unchanged now that local legs can sign.
func TestVirtualRepomdUnsignedWithSignedMember(t *testing.T) {
	s := newStack(t)
	s.generateSignKey(t, "t322-virt")
	s.seedSignedRepo(t, "rpm-m1", "t322-virt", "virtpkg")
	// The second member makes the aggregate rule (>=2 members with
	// repodata) fire; it stays unsigned.
	s.seedRepo(t, "rpm-m2", repo.TypeLocal, "{}")
	pkg := pkgFixture("plainpkg", "1.0.0", "1.el9", "noarch")
	if status, body, _ := s.put("/binflow/rpm-m2/plainpkg-1.0.0-1.el9.noarch.rpm", pkg, nil); status != http.StatusCreated {
		t.Fatalf("rpmPUT m2 = (%d, %s)", status, body)
	}
	s.reindexSync(t, "rpm-m2")
	s.seedVirtualRepo(t, "rpm-virt", "", []string{"rpm-m1", "rpm-m2"}, nil)

	// The member's pair serves; the aggregate's face 404s.
	if status, _, _ := s.get("/binflow/rpm-m1/repodata/repomd.xml.asc"); status != http.StatusOK {
		t.Errorf("signed member .asc = %d, want 200 (the local leg signs)", status)
	}
	if status, _, _ := s.get("/binflow/rpm-virt/repodata/repomd.xml"); status != http.StatusOK {
		t.Fatalf("virtual repomd.xml = %d, want 200 (the aggregate serves unsigned)", status)
	}
	for _, path := range []string{
		"/binflow/rpm-virt/repodata/repomd.xml.asc",
		"/binflow/rpm-virt/repodata/repomd.xml.key",
	} {
		status, body, _ := s.get(path)
		if status != http.StatusNotFound {
			t.Errorf("%s = %d, want 404 (the aggregate is never signed)", path, status)
			continue
		}
		if !strings.Contains(body, "not signed") {
			t.Errorf("%s 404 body does not explain the unsigned posture: %q", path, body)
		}
	}
}

// TestRepomdSigningGPGClientRoundTrip: the REAL client leg of the
// library-verified faces — the served repomd.xml.asc / repomd.xml.key
// pair round-trips through gpg itself (the exact verifier dnf's
// repo_gpgcheck drives). Skips without the binary.
func TestRepomdSigningGPGClientRoundTrip(t *testing.T) {
	bin := gpgClientBin(t)
	s := newStack(t)
	public := s.generateSignKey(t, "t322-gpg")
	repomd := s.seedSignedRepo(t, "rpm-signed", "t322-gpg", "gpgpkg")
	asc := mustGetBody(t, s, "/binflow/rpm-signed/repodata/repomd.xml.asc")

	home := shortGPGHome(t)
	write := func(name, body string) string {
		t.Helper()
		p := filepath.Join(home, name)
		if err := os.WriteFile(p, []byte(body), 0o600); err != nil {
			t.Fatalf("write %s: %v", name, err)
		}
		return p
	}
	pubFile := write("public.asc", public)
	repomdFile := write("repomd.xml", repomd)
	sigFile := write("repomd.xml.asc", asc)

	if out, err := runGPGClient(t, bin, home, "--batch", "--import", pubFile); err != nil {
		t.Fatalf("gpg --import: %v\n%s", err, out)
	}
	if out, err := runGPGClient(t, bin, home, "--batch", "--verify", sigFile, repomdFile); err != nil {
		t.Fatalf("gpg --verify repomd.xml.asc repomd.xml: %v\n%s", err, out)
	}
	// The served repomd.xml.key is the same importable key.
	servedKey := mustGetBody(t, s, "/binflow/rpm-signed/repodata/repomd.xml.key")
	servedFile := write("served.key", servedKey)
	if out, err := runGPGClient(t, bin, home, "--batch", "--show-keys", servedFile); err != nil {
		t.Fatalf("gpg --show-keys on the served repomd.xml.key: %v\n%s", err, out)
	}
}

// mustGetBody fetches a body that must answer 200.
func mustGetBody(t *testing.T, s *stack, path string) string {
	t.Helper()
	status, body, _ := s.get(path)
	if status != http.StatusOK {
		t.Fatalf("GET %s = %d", path, status)
	}
	return body
}

// gpgClientBin resolves the client or skips.
func gpgClientBin(t *testing.T) string {
	t.Helper()
	for _, name := range []string{"gpg", "gpg2"} {
		if p, err := exec.LookPath(name); err == nil {
			return p
		}
	}
	t.Skip("no gpg client on PATH — the interop leg needs GnuPG")
	return ""
}

// shortGPGHome makes a SHORT GNUPGHOME (the socket name limit the
// testing temp tree overflows — the keypair suite's posture).
func shortGPGHome(t *testing.T) string {
	t.Helper()
	dir, err := os.MkdirTemp("/tmp", "t322-")
	if err != nil {
		t.Fatalf("mktemp gpg home: %v", err)
	}
	t.Cleanup(func() { _ = os.RemoveAll(dir) })
	if err := os.Chmod(dir, 0o700); err != nil {
		t.Fatalf("chmod gpg home: %v", err)
	}
	return dir
}

// runGPGClient runs gpg with an isolated, non-tty home.
func runGPGClient(t *testing.T, bin, home string, args ...string) (string, error) {
	t.Helper()
	cmd := exec.Command(bin, args...)
	cmd.Env = append(os.Environ(), "GNUPGHOME="+home)
	cmd.Dir = home
	out, err := cmd.CombinedOutput()
	return string(out), err
}
