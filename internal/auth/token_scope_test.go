package auth_test

// T-349 (FR-113.3): the narrow checksum-deploy scope on minted tokens —
// roundtrip through the real store, the principal's scope field, and the
// fail-closed reading of a corrupt scope row.

import (
	"crypto/sha256"
	"encoding/hex"
	"testing"
	"time"

	"github.com/lzwzzy/binflow/internal/auth"
	"github.com/lzwzzy/binflow/internal/metadata"
)

func TestIssueChecksumDeployScopedRoundtrip(t *testing.T) {
	f := newFixture(t, true)

	tok, err := f.svc.IssueChecksumDeployScoped(f.ctx, "ci-bot", 5*time.Minute,
		[]string{"gen-local", "gen-virtual"}, "big/pkg.bin")
	if err != nil {
		t.Fatalf("IssueChecksumDeployScoped: %v", err)
	}
	if tok.Scope != auth.ScopeAPI {
		t.Errorf("IssuedToken.Scope = %q, want %q (the narrowing rides the row)", tok.Scope, auth.ScopeAPI)
	}

	p, err := f.svc.Verify(f.ctx, tok.AccessToken)
	if err != nil {
		t.Fatalf("Verify: %v", err)
	}
	if p.DeployScope == nil {
		t.Fatal("Verify dropped the deploy scope")
	}
	if p.DeployScope.Kind != auth.DeployScopeChecksumDeploy {
		t.Errorf("kind = %q", p.DeployScope.Kind)
	}
	if len(p.DeployScope.Repos) != 2 || p.DeployScope.Repos[0] != "gen-local" || p.DeployScope.Repos[1] != "gen-virtual" {
		t.Errorf("repos = %v, want both landing spellings", p.DeployScope.Repos)
	}
	if p.DeployScope.Path != "big/pkg.bin" {
		t.Errorf("path = %q", p.DeployScope.Path)
	}
	// Identity is untouched: the narrow token still IS the owner's
	// credential — the HTTP layer narrows the surface, not the identity.
	if p.Name != "ci-bot" || p.TokenID == 0 {
		t.Errorf("principal = %v (id %d), want the owner's identity with its token id", p.Name, p.TokenID)
	}

	// A plain Issue carries NO scope: unrestricted is still the default.
	plain, err := f.svc.Issue(f.ctx, "ci-bot", time.Hour)
	if err != nil {
		t.Fatalf("Issue: %v", err)
	}
	p, err = f.svc.Verify(f.ctx, plain.AccessToken)
	if err != nil {
		t.Fatalf("Verify(plain): %v", err)
	}
	if p.DeployScope != nil {
		t.Errorf("plain token resolved a scope (%v), want none", p.DeployScope)
	}

	// Minting with no usable coordinates is refused (never a scopeless
	// "narrow" token, which would silently be full-power).
	if _, err := f.svc.IssueChecksumDeployScoped(f.ctx, "ci-bot", time.Minute, nil, "a.bin"); err == nil {
		t.Error("empty repo list minted, want refusal")
	}
	if _, err := f.svc.IssueChecksumDeployScoped(f.ctx, "ci-bot", time.Minute, []string{"r"}, ""); err == nil {
		t.Error("empty path minted, want refusal")
	}
}

// TestVerifyScopeCorruptRowFailsClosed: a scope value the parser cannot
// read must REFUSE the token outright — reading it as unrestricted would
// widen a corrupt row back into a full-power credential. The corrupt rows
// are seeded straight through the public store (a hand-built row per shape),
// exactly the way an operational corruption would present.
func TestVerifyScopeCorruptRowFailsClosed(t *testing.T) {
	f := newFixture(t, true)

	seed := func(scope string) string {
		t.Helper()
		plaintext := "corrupt-scope-token-" + scope
		digest := sha256.Sum256([]byte(plaintext))
		_, err := f.st.Tokens().Create(f.ctx, &metadata.Token{
			Username:    "ci-bot",
			TokenSHA256: hex.EncodeToString(digest[:]),
			ExpiresAt:   metadata.NeverExpires,
			CreatedAt:   metadata.Now(),
			DeployScope: scope,
		})
		if err != nil {
			t.Fatalf("seeding scope %q: %v", scope, err)
		}
		return plaintext
	}

	for name, scope := range map[string]string{
		"not json":            "{{{",
		"unknown kind":        `{"kind":"root","repos":["r"],"path":"a.bin"}`,
		"missing coordinates": `{"kind":"checksum-deploy","repos":[],"path":""}`,
		"empty repo entry":    `{"kind":"checksum-deploy","repos":[""],"path":"a.bin"}`,
	} {
		if _, verr := f.svc.Verify(f.ctx, seed(scope)); verr == nil {
			t.Errorf("scope %q verified, want the fail-closed refusal", name)
		}
	}
}
