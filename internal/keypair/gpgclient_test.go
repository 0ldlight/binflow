package keypair_test

// T-319 real-client interop (the ticket's verification mandate): the keys
// BinFlow generates and the signatures the seam produces must round-trip
// through the GnuPG client itself — the exact verifier apt/dnf drive.
// Both directions:
//
//   - outbound: BinFlow generates → gpg imports the PUBLIC key → gpg
//     verifies the seam's clearsign (InRelease form) and detached armor
//     (Release.gpg / repomd.xml.asc form).
//   - inbound: gpg generates a key (with passphrase protection) → exports
//     the armored pair → BinFlow imports and stores it → the seam signs →
//     gpg verifies with its own key.
//
// The test skips when no gpg binary is on PATH (CI legs without the
// client); the container-based apt/dnf end-to-end belongs to T-321/T-322.

import (
	"context"
	"os"
	"os/exec"
	"path/filepath"
	"testing"

	"github.com/lzwzzy/binflow/internal/keypair"
	"github.com/lzwzzy/binflow/internal/metadata"
)

// gpgBin resolves the client or reports absence.
func gpgBin(t *testing.T) string {
	t.Helper()
	for _, name := range []string{"gpg", "gpg2"} {
		if path, err := exec.LookPath(name); err == nil {
			return path
		}
	}
	t.Skip("no gpg client on PATH — the interop leg needs GnuPG (brew install gnupg / apt install gnupg)")
	return ""
}

// gpgHomeDir makes a SHORT GNUPGHOME: the agent's socket path is bound by
// the platform's unix-socket name limit (~104 chars), which the testing
// temp tree overflows — the Go-side t.TempDir stays for the Go artifacts,
// the gpg home lives under a brief /tmp path.
func gpgHomeDir(t *testing.T) string {
	t.Helper()
	dir, err := os.MkdirTemp("/tmp", "t319-")
	if err != nil {
		t.Fatalf("mktemp gpg home: %v", err)
	}
	t.Cleanup(func() { _ = os.RemoveAll(dir) })
	if err := os.Chmod(dir, 0o700); err != nil {
		t.Fatalf("chmod gpg home: %v", err)
	}
	return dir
}

// gpgHome runs gpg with an isolated, non-tty, unbeeping home.
func gpgHome(t *testing.T, bin, home string, args ...string) (string, error) {
	t.Helper()
	cmd := exec.Command(bin, args...)
	cmd.Env = append(os.Environ(),
		"GNUPGHOME="+home,
		"PINENTRY_USER_DATA=USE_LOOPBACK=1",
		"LOOPBACK_PASSPHRASE="+gpgTestPassphrase,
	)
	cmd.Dir = home
	out, err := cmd.CombinedOutput()
	return string(out), err
}

const gpgTestPassphrase = "t319-loopback"

// newInteropEnv builds the manager with a cipher-backed store.
func newInteropEnv(t *testing.T) *keypairEnv {
	t.Helper()
	e := newKeypairEnv(t)
	return e
}

func TestGPGClientVerifiesBinFlowSignatures(t *testing.T) {
	bin := gpgBin(t)
	e := newInteropEnv(t)
	ctx := context.Background()
	home := gpgHomeDir(t)

	// BinFlow generates (passphrase-protected, as an operator would).
	if _, err := e.mgr.Generate(ctx, keypair.GenerateInput{
		PairName: "interop", Alias: "interop", KeyBits: 2048,
		Passphrase: gpgTestPassphrase,
	}, "root"); err != nil {
		t.Fatalf("Generate: %v", err)
	}
	e.repos.put(&metadata.Repo{RepoKey: "deb-l", Type: "local", PackageType: "debian", Config: `{"keyPairName":"interop"}`})
	svc, err := keypair.NewSigningService(e.md.GpgKeypairs(), seam(e.cipher), e.repos)
	if err != nil {
		t.Fatalf("NewSigningService: %v", err)
	}

	// The PUBLIC key alone is what the verifier needs (the private half
	// never leaves the store — this leg proves the plane's distribution
	// shape: REST echo → gpg --import).
	summary, err := e.mgr.Get(ctx, "interop")
	if err != nil {
		t.Fatalf("Get: %v", err)
	}
	pubFile := filepath.Join(home, "public.asc")
	if err := os.WriteFile(pubFile, []byte(summary.PublicKey), 0o600); err != nil {
		t.Fatalf("write public key: %v", err)
	}
	if out, err := gpgHome(t, bin, home, "--batch", "--import", pubFile); err != nil {
		t.Fatalf("gpg --import: %v\n%s", err, out)
	}

	// Detached armor (repomd.xml.asc / Release.gpg form).
	release := []byte("Origin: BinFlow\nSuite: bookworm\nCodename: bookworm\nArchitectures: amd64\n")
	sig, err := svc.DetachedArmor(ctx, "deb-l", release)
	if err != nil {
		t.Fatalf("DetachedArmor: %v", err)
	}
	dataFile := filepath.Join(home, "release.txt")
	sigFile := filepath.Join(home, "release.txt.asc")
	if err := os.WriteFile(dataFile, release, 0o600); err != nil {
		t.Fatalf("write release: %v", err)
	}
	if err := os.WriteFile(sigFile, []byte(sig), 0o600); err != nil {
		t.Fatalf("write signature: %v", err)
	}
	// gpg --verify exits 0 exactly on a good signature (the untrusted-key
	// warning is not a failure; the output text is locale-dependent and not
	// asserted).
	if out, err := gpgHome(t, bin, home, "--batch", "--verify", sigFile, dataFile); err != nil {
		t.Fatalf("gpg --verify (detached): %v\n%s", err, out)
	}

	// Clearsign (the InRelease form).
	inrelease := []byte("Origin: BinFlow\nSuite: bookworm\nDate: Thu, 27 Aug 2026 12:00:00 UTC\n")
	signed, err := svc.Clearsign(ctx, "deb-l", inrelease)
	if err != nil {
		t.Fatalf("Clearsign: %v", err)
	}
	inFile := filepath.Join(home, "InRelease")
	if err := os.WriteFile(inFile, signed, 0o600); err != nil {
		t.Fatalf("write InRelease: %v", err)
	}
	if out, err := gpgHome(t, bin, home, "--batch", "--verify", inFile); err != nil {
		t.Fatalf("gpg --verify (clearsign): %v\n%s", err, out)
	}
}

func TestBinFlowImportsGPGClientKey(t *testing.T) {
	bin := gpgBin(t)
	e := newInteropEnv(t)
	ctx := context.Background()
	home := gpgHomeDir(t)

	// The client generates a protected key (the operator's own key flows
	// in through the import face).
	if out, err := gpgHome(t, bin, home,
		"--batch", "--passphrase", gpgTestPassphrase, "--quick-gen-key",
		"interop-client (t319) <t319@binflow.test>", "rsa2048", "sign", "never"); err != nil {
		t.Fatalf("gpg --quick-gen-key: %v\n%s", err, out)
	}
	privFile := filepath.Join(home, "priv.asc")
	pubFile := filepath.Join(home, "pub.asc")
	if out, err := gpgHome(t, bin, home,
		"--batch", "--pinentry-mode", "loopback", "--passphrase", gpgTestPassphrase,
		"--armor", "--export-secret-keys", "t319@binflow.test"); err != nil {
		t.Fatalf("gpg --export-secret-keys: %v\n%s", err, out)
	} else if err := os.WriteFile(privFile, []byte(out), 0o600); err != nil {
		t.Fatalf("write private export: %v", err)
	}
	if out, err := gpgHome(t, bin, home,
		"--batch", "--armor", "--export", "t319@binflow.test"); err != nil {
		t.Fatalf("gpg --export: %v\n%s", err, out)
	} else if err := os.WriteFile(pubFile, []byte(out), 0o600); err != nil {
		t.Fatalf("write public export: %v", err)
	}
	privateArmored, err := os.ReadFile(privFile)
	if err != nil {
		t.Fatalf("read private export: %v", err)
	}
	publicArmored, err := os.ReadFile(pubFile)
	if err != nil {
		t.Fatalf("read public export: %v", err)
	}

	// BinFlow imports the client's pair.
	if _, err := e.mgr.Import(ctx, keypair.ImportInput{
		PairName:   "client-key",
		PairType:   keypair.PairTypeGPG,
		Alias:      "client",
		PrivateKey: string(privateArmored),
		PublicKey:  string(publicArmored),
		Passphrase: gpgTestPassphrase,
	}, "root"); err != nil {
		t.Fatalf("Import of the gpg client key: %v", err)
	}

	// The seam signs with the imported key and the client verifies against
	// its own public key (fresh verifier home so only that key is trusted
	// material — same as the outbound leg's distribution shape).
	e.repos.put(&metadata.Repo{RepoKey: "rpm-l", Type: "local", PackageType: "rpm", Config: `{"keyPairName":"client-key"}`})
	svc, err := keypair.NewSigningService(e.md.GpgKeypairs(), seam(e.cipher), e.repos)
	if err != nil {
		t.Fatalf("NewSigningService: %v", err)
	}
	repomd := []byte("<?xml version=\"1.0\"?><repomd xmlns=\"rpm\"></repomd>\n")
	sig, err := svc.DetachedArmor(ctx, "rpm-l", repomd)
	if err != nil {
		t.Fatalf("DetachedArmor with the imported key: %v", err)
	}
	verifyHome := gpgHomeDir(t)
	if out, err := gpgHome(t, bin, verifyHome, "--batch", "--import", pubFile); err != nil {
		t.Fatalf("gpg --import (verifier home): %v\n%s", err, out)
	}
	dataFile := filepath.Join(verifyHome, "repomd.xml")
	sigFile := filepath.Join(verifyHome, "repomd.xml.asc")
	if err := os.WriteFile(dataFile, repomd, 0o600); err != nil {
		t.Fatalf("write repomd: %v", err)
	}
	if err := os.WriteFile(sigFile, []byte(sig), 0o600); err != nil {
		t.Fatalf("write repomd signature: %v", err)
	}
	if out, err := gpgHome(t, bin, verifyHome, "--batch", "--verify", sigFile, dataFile); err != nil {
		t.Fatalf("gpg --verify (imported key): %v\n%s", err, out)
	}
}
