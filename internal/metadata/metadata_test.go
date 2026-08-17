package metadata_test

import (
	"context"
	"crypto/sha256"
	"encoding/hex"
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"regexp"
	"strings"
	"testing"

	"github.com/lzwzzy/binflow/internal/metadata"
)

// open opens a store on a fresh temp database with a known admin password.
func open(t *testing.T) metadata.Store {
	t.Helper()
	return openOpts(t, metadata.Options{AdminPassword: "it-admin-pw"})
}

func openOpts(t *testing.T, opts metadata.Options) metadata.Store {
	t.Helper()
	opts.Driver = "sqlite"
	if opts.Path == "" {
		opts.Path = filepath.Join(t.TempDir(), "binflow.db")
	}
	st, err := metadata.Open(context.Background(), opts)
	if err != nil {
		t.Fatalf("metadata.Open: %v", err)
	}
	t.Cleanup(func() { _ = st.Close() })
	return st
}

func putRepo(t *testing.T, st metadata.Store, key string) {
	t.Helper()
	now := metadata.Now()
	err := st.Repos().Create(context.Background(), &metadata.Repo{
		RepoKey: key, Type: "local", PackageType: "generic",
		Description: "test", Config: "{}", CreatedAt: now, UpdatedAt: now,
	})
	if err != nil {
		t.Fatalf("create repo %s: %v", key, err)
	}
}

func fakeBlob(i int) (sha string, sum int64) {
	b := make([]byte, 64)
	copy(b, fmt.Sprintf("%04d", i)) // deterministic prefix; content irrelevant here
	h := sha256.Sum256(b)
	return hex.EncodeToString(h[:]), int64(len(b))
}

// AC: admin seeding — env-supplied password and documented default.
func TestAdminSeed(t *testing.T) {
	ctx := context.Background()
	tests := []struct {
		name       string
		adminPass  string
		expectPass string
	}{
		{"explicit env-equivalent password", "s3cret-hunter2", "s3cret-hunter2"},
		{"empty falls back to documented default", "", "password"},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			st := openOpts(t, metadata.Options{AdminPassword: tt.adminPass})
			u, err := st.Users().Get(ctx, "admin")
			if err != nil {
				t.Fatalf("get admin: %v", err)
			}
			if !u.IsAdmin || !u.Enabled {
				t.Fatalf("admin flags: isAdmin=%v enabled=%v", u.IsAdmin, u.Enabled)
			}
			if !metadata.VerifyPassword(tt.expectPass, u.PasswordHash) {
				t.Fatalf("expected password %q to verify against stored hash", tt.expectPass)
			}
			if metadata.VerifyPassword("wrong", u.PasswordHash) {
				t.Fatal("wrong password must not verify")
			}
			if plaintextInHash(u.PasswordHash, tt.expectPass) {
				t.Fatalf("hash %q leaks the plaintext", u.PasswordHash)
			}
		})
	}
}

func plaintextInHash(hash, plaintext string) bool {
	return strings.Contains(hash, plaintext) && len(plaintext) > 4
}

// The stored hash must look like an argon2id PHC string with the M1
// parameters (t=1, m=64MiB, p=4).
func TestAdminSeedHashParameters(t *testing.T) {
	st := open(t)
	u, err := st.Users().Get(context.Background(), "admin")
	if err != nil {
		t.Fatalf("get admin: %v", err)
	}
	re := regexp.MustCompile(`^\$argon2id\$v=19\$m=65536,t=1,p=4\$[A-Za-z0-9+/]+\$[A-Za-z0-9+/]+$`)
	if !re.MatchString(u.PasswordHash) {
		t.Fatalf("hash %q is not an argon2id PHC string with t=1,m=65536,p=4", u.PasswordHash)
	}
}

// AC: TokenStore stores only sha256 digests — scanning the database file must
// not find the plaintext token.
func TestTokenStoreDigestOnly(t *testing.T) {
	st := open(t)
	ctx := context.Background()

	plaintext := "bf-live-token-plaintext-7f3a2b"
	digest := sha256.Sum256([]byte(plaintext))
	digestHex := hex.EncodeToString(digest[:])

	id, err := st.Tokens().Create(ctx, &metadata.Token{
		Username:    "admin",
		TokenSHA256: digestHex,
		ExpiresAt:   metadata.NeverExpires,
		CreatedAt:   metadata.Now(),
	})
	if err != nil {
		t.Fatalf("token create: %v", err)
	}
	if id <= 0 {
		t.Fatalf("token id = %d, want > 0", id)
	}

	got, err := st.Tokens().GetBySHA256(ctx, digestHex)
	if err != nil {
		t.Fatalf("token get-by-digest: %v", err)
	}
	if got.Username != "admin" || got.ID != id {
		t.Fatalf("token roundtrip mismatch: %+v", got)
	}
	if err := st.Tokens().Touch(ctx, id, metadata.Now()); err != nil {
		t.Fatalf("token touch: %v", err)
	}

	// NFR-S2: the raw database file must not contain the plaintext anywhere
	// (tables, indexes, freelist, WAL).
	dbPath := st.(interface{ DBPath() string }).DBPath()
	for _, suffix := range []string{"", "-wal", "-shm"} {
		data, err := os.ReadFile(dbPath + suffix) // fixed suffix over the test temp database (G304 excluded globally)
		if errors.Is(err, os.ErrNotExist) {
			continue
		}
		if err != nil {
			t.Fatalf("reading %s: %v", dbPath+suffix, err)
		}
		if strings.Contains(string(data), plaintext) {
			t.Fatalf("plaintext token found in %s — tokens must be stored as sha256 only", dbPath+suffix)
		}
	}

	if err := st.Tokens().Delete(ctx, id); err != nil {
		t.Fatalf("token delete: %v", err)
	}
	if _, err := st.Tokens().GetBySHA256(ctx, digestHex); !errors.Is(err, metadata.ErrTokenNotFound) {
		t.Fatalf("get after delete err = %v, want ErrTokenNotFound", err)
	}
}
