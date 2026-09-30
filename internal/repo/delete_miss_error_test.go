package repo_test

// BIN-94/T-612: the unified delete's miss verdict is the deletion engine's
// family, spoken ONCE in the service layer (deleteMissError) for the LOCAL
// file/folder faces and the REMOTE cache face alike: a *repo.StatusError
// 404 carrying "Artifact deletion error: Item <repo>/<path> does not exist"
// verbatim (no trailing period; folder spellings keep their slash) with the
// ErrNodeNotFound chain preserved for sentinel-testing callers. The VIRTUAL
// own-storage miss keeps the plain sentinel — that face is undecided (no A
// probe) and adapters still render it from their wording arms.

import (
	"context"
	"errors"
	"testing"

	"github.com/lzwzzy/binflow/internal/repo"
)

func TestDeleteMissErrorFamily(t *testing.T) {
	ctx := context.Background()
	e := newEnv(t)
	mustCreateRepo(t, e, "generic-local")

	cases := []struct {
		name string
		path string
	}{
		{"file miss", "never/existed.bin"},
		{"folder miss keeps the trailing slash", "never-dir/"},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			err := e.svc.Delete(ctx, admin(), "generic-local", tc.path)
			var se *repo.StatusError
			if !errors.As(err, &se) {
				t.Fatalf("Delete(%s) = %v (%T), want *repo.StatusError", tc.path, err, err)
			}
			if se.Code != 404 {
				t.Fatalf("Delete(%s) code = %d, want 404", tc.path, se.Code)
			}
			want := "Artifact deletion error: Item generic-local/" + tc.path + " does not exist"
			if se.Message != want {
				t.Fatalf("Delete(%s) message = %q, want %q", tc.path, se.Message, want)
			}
			if !errors.Is(err, repo.ErrNodeNotFound) {
				t.Fatalf("Delete(%s) chain lost ErrNodeNotFound: %v", tc.path, err)
			}
		})
	}

	// The virtual face keeps the plain sentinel (undecided; adapters own
	// its wording until an A probe rules it).
	buildVirtual(t, e, "virt", "", []memberSpec{{key: "virt-loc"}})
	verr := e.svc.Delete(ctx, admin(), "virt", "a/b.bin")
	if !errors.Is(verr, repo.ErrNodeNotFound) {
		t.Fatalf("virtual Delete miss = %v (%T), want the plain ErrNodeNotFound sentinel", verr, verr)
	}
	var se *repo.StatusError
	if errors.As(verr, &se) {
		t.Fatalf("virtual Delete miss must not carry the engine StatusError: %v", verr)
	}
}
