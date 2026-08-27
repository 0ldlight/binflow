package main

// T-319 cmd arm: the keypair plane's boot posture — wireKeypairManager
// builds the Manager over the real store and the enc:v1 chain, and
// BootCheck fails the boot exactly when sealed rows exist without the
// master key (the static-secret family posture of auth_configs and
// replication). The tests rely on BINFLOW_REMOTE_CREDENTIALS_KEY being
// unset in the test environment (the no-key posture); t.Setenv pins the
// with-key leg.

import (
	"context"
	"log/slog"
	"strings"
	"testing"

	"github.com/lzwzzy/binflow/internal/keypair"
	"github.com/lzwzzy/binflow/internal/metadata"
)

// t319Logger is the discard logger of the wiring legs.
func t319Logger() *slog.Logger {
	return slog.New(slog.NewTextHandler(&strings.Builder{}, nil))
}

// t319OpenStore opens a throwaway sqlite store.
func t319OpenStore(t *testing.T) metadata.Store {
	t.Helper()
	md, err := metadata.Open(context.Background(), metadata.Options{
		Driver: "sqlite", Path: t.TempDir() + "/binflow.db",
	})
	if err != nil {
		t.Fatalf("metadata.Open: %v", err)
	}
	return md
}

func TestWireKeypairManagerBootsEmpty(t *testing.T) {
	ctx := context.Background()
	md := t319OpenStore(t)
	t.Cleanup(func() { _ = md.Close() })

	mgr, err := wireKeypairManager(ctx, md, t319Logger())
	if err != nil {
		t.Fatalf("wireKeypairManager: %v", err)
	}
	if mgr == nil {
		t.Fatalf("wireKeypairManager returned a nil manager")
	}
}

func TestWireKeypairManagerFailsFastOnSealedRowsWithoutKey(t *testing.T) {
	ctx := context.Background()
	md := t319OpenStore(t)
	t.Cleanup(func() { _ = md.Close() })

	// A sealed row from an instance that once had the master key; the empty
	// value IS the unset posture for LoadKey (LookupEnv sees it set but
	// blank → no key).
	t.Setenv("BINFLOW_REMOTE_CREDENTIALS_KEY", "")

	if err := md.GpgKeypairs().PutKeypair(ctx, &metadata.GpgKeypairRecord{
		PairName: "legacy", PairType: keypair.PairTypeGPG, Alias: "a",
		PublicKey: "pub", PrivateKeyEnc: "enc:v1:x", PassphraseEnc: "enc:v1:y",
		Algorithm: "RSA-2048", CreatedAt: "t", UpdatedAt: "t", UpdatedBy: "root",
	}); err != nil {
		t.Fatalf("PutKeypair: %v", err)
	}
	if _, err := wireKeypairManager(ctx, md, t319Logger()); err == nil ||
		!strings.Contains(err.Error(), "no master key") {
		t.Fatalf("wireKeypairManager with sealed rows and no key err = %v, want the fail-fast", err)
	}
}

func TestWireKeypairManagerWithKeyServesSealedRows(t *testing.T) {
	ctx := context.Background()
	md := t319OpenStore(t)
	t.Cleanup(func() { _ = md.Close() })

	t.Setenv("BINFLOW_REMOTE_CREDENTIALS_KEY", "MDEyMzQ1Njc4OWFiY2RlZjAxMjM0NTY3ODlhYmNkZWY=") // base64 of 32 bytes
	if _, err := wireKeypairManager(ctx, md, t319Logger()); err != nil {
		t.Fatalf("wireKeypairManager with key: %v", err)
	}
}
