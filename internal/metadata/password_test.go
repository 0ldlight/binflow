package metadata_test

import (
	"strings"
	"testing"

	"github.com/lzwzzy/binflow/internal/metadata"
)

func TestHashPasswordPHCShape(t *testing.T) {
	hash, err := metadata.HashPassword("correct horse battery staple")
	if err != nil {
		t.Fatalf("HashPassword: %v", err)
	}
	if !strings.HasPrefix(hash, "$argon2id$v=19$m=65536,t=1,p=4$") {
		t.Fatalf("hash %q lacks the M1 argon2id parameter prefix", hash)
	}
	parts := strings.Split(hash, "$")
	if len(parts) != 6 {
		t.Fatalf("hash %q should have 6 '$'-separated fields, got %d", hash, len(parts))
	}
}

func TestHashPasswordSaltsVary(t *testing.T) {
	h1, err := metadata.HashPassword("same-password")
	if err != nil {
		t.Fatalf("HashPassword #1: %v", err)
	}
	h2, err := metadata.HashPassword("same-password")
	if err != nil {
		t.Fatalf("HashPassword #2: %v", err)
	}
	if h1 == h2 {
		t.Fatal("two hashes of the same password must differ (random salt)")
	}
	if !metadata.VerifyPassword("same-password", h1) || !metadata.VerifyPassword("same-password", h2) {
		t.Fatal("both hashes must verify against the same password")
	}
}

func TestVerifyPasswordMatrix(t *testing.T) {
	hash, err := metadata.HashPassword("unit-test-password")
	if err != nil {
		t.Fatalf("HashPassword: %v", err)
	}
	tests := []struct {
		name     string
		password string
		hash     string
		want     bool
	}{
		{"correct password", "unit-test-password", hash, true},
		{"wrong password", "unit-test-wrong", hash, false},
		{"empty password vs hash of empty", "", mustHash(t, ""), true},
		{"malformed hash garbage", "x", "not-a-hash", false},
		{"malformed hash empty", "x", "", false},
		{"malformed hash wrong variant", "x", "$argon2i$v=19$m=65536,t=1,p=4$c2FsdA$dGFn", false},
		{"malformed hash bad params", "x", "$argon2id$v=19$m=abc,t=1,p=4$c2FsdA$dGFn", false},
		{"malformed hash bad salt b64", "x", "$argon2id$v=19$m=65536,t=1,p=4$!!$dGFn", false},
		{"malformed hash truncated", "x", "$argon2id$v=19$m=65536,t=1", false},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			if got := metadata.VerifyPassword(tt.password, tt.hash); got != tt.want {
				t.Fatalf("VerifyPassword(%q, %q) = %v, want %v", tt.password, tt.hash, got, tt.want)
			}
		})
	}
}

func mustHash(t *testing.T, pw string) string {
	t.Helper()
	h, err := metadata.HashPassword(pw)
	if err != nil {
		t.Fatalf("HashPassword(%q): %v", pw, err)
	}
	return h
}
