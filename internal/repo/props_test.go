package repo_test

import (
	"context"
	"errors"
	"io"
	"strings"
	"testing"

	"github.com/lzwzzy/binflow/internal/metadata"
	"github.com/lzwzzy/binflow/internal/repo"
	"github.com/lzwzzy/binflow/internal/storage"
)

// Deploy-time matrix properties through PutOptions.Properties (M10 T-286,
// architecture section 15.3.1): the landing, the merge law on redeploy,
// the folder arm and the early refusal of an illegal set.

func seedPropsRepo(t *testing.T, e *env) {
	t.Helper()
	if _, err := e.svc.CreateRepo(context.Background(), admin(), &metadata.Repo{
		RepoKey: "generic-local", Type: repo.TypeLocal, PackageType: repo.PackageGeneric,
	}); err != nil {
		t.Fatalf("CreateRepo: %v", err)
	}
}

func TestPutWithOptionsPropertiesLand(t *testing.T) {
	e := newEnv(t)
	ctx := context.Background()
	seedPropsRepo(t, e)

	n, err := e.svc.PutWithOptions(ctx, admin(), "generic-local", "ci/app.bin",
		strings.NewReader("bytes"), storage.BlobRef{}, "application/octet-stream",
		repo.PutOptions{Properties: map[string][]string{
			"build": {"77"},
			"env":   {"prod", "dev"},
		}})
	if err != nil {
		t.Fatalf("PutWithOptions: %v", err)
	}
	if n.Path != "ci/app.bin" {
		t.Fatalf("node path = %q", n.Path)
	}

	got, err := e.md.NodeProps().List(ctx, "generic-local", "ci/app.bin")
	if err != nil {
		t.Fatalf("list props: %v", err)
	}
	if len(got) != 2 || got["build"][0] != "77" || len(got["env"]) != 2 {
		t.Fatalf("props after deploy = %+v", got)
	}

	// The redeploy merge law (section 11.40): the same key's set is
	// replaced, other keys survive — a redeploy ANNOTATES, never clobbers.
	if _, err := e.svc.PutWithOptions(ctx, admin(), "generic-local", "ci/app.bin",
		strings.NewReader("bytes2"), storage.BlobRef{}, "application/octet-stream",
		repo.PutOptions{Properties: map[string][]string{"env": {"staging"}}}); err != nil {
		t.Fatalf("redeploy PutWithOptions: %v", err)
	}
	got, err = e.md.NodeProps().List(ctx, "generic-local", "ci/app.bin")
	if err != nil {
		t.Fatalf("list props after redeploy: %v", err)
	}
	if len(got) != 2 || len(got["env"]) != 1 || got["env"][0] != "staging" || got["build"][0] != "77" {
		t.Fatalf("merge law broken: %+v", got)
	}

	// A plain re-PUT (no options) writes no properties and keeps the
	// existing ones — properties are not deploy-coupled state.
	if _, err := e.svc.Put(ctx, admin(), "generic-local", "ci/app.bin",
		strings.NewReader("bytes3"), storage.BlobRef{}, "application/octet-stream"); err != nil {
		t.Fatalf("plain Put: %v", err)
	}
	got, err = e.md.NodeProps().List(ctx, "generic-local", "ci/app.bin")
	if err != nil || len(got) != 2 {
		t.Fatalf("plain redeploy disturbed properties: %+v err=%v", got, err)
	}
}

func TestPutWithOptionsFolderProperties(t *testing.T) {
	// Folder deploys carry matrix properties too (section 15.3.2: folder
	// rows are legal property carriers; the REST PUT arm relies on it).
	e := newEnv(t)
	ctx := context.Background()
	seedPropsRepo(t, e)

	if _, err := e.svc.PutWithOptions(ctx, admin(), "generic-local", "releases/",
		io.Reader(nil), storage.BlobRef{}, "", repo.PutOptions{Properties: map[string][]string{
			"owner": {"team-a"},
		}}); err != nil {
		t.Fatalf("folder PutWithOptions: %v", err)
	}
	got, err := e.md.NodeProps().List(ctx, "generic-local", "releases/")
	if err != nil {
		t.Fatalf("list folder props: %v", err)
	}
	if len(got) != 1 || got["owner"][0] != "team-a" {
		t.Fatalf("folder props = %+v", got)
	}
}

func TestPutWithOptionsIllegalProperties(t *testing.T) {
	e := newEnv(t)
	ctx := context.Background()
	seedPropsRepo(t, e)

	_, err := e.svc.PutWithOptions(ctx, admin(), "generic-local", "ci/app.bin",
		strings.NewReader("bytes"), storage.BlobRef{}, "",
		repo.PutOptions{Properties: map[string][]string{"bad key": {"1"}}})
	if !isPropsInvalid(err) {
		t.Fatalf("illegal properties err = %v, want ErrInvalidProperties", err)
	}
	// Zero side effects: the node must not have landed (the validation
	// precedes the repository resolution and the body drain).
	if _, err := e.md.Nodes().Get(ctx, "generic-local", "ci/app.bin"); err == nil {
		t.Fatal("node landed despite the property refusal")
	}
}

func TestValidatePropSetRules(t *testing.T) {
	if err := repo.ValidatePropSet(nil); err != nil {
		t.Fatalf("nil set: %v", err)
	}
	if err := repo.ValidatePropSet(map[string][]string{"k": {"v"}}); err != nil {
		t.Fatalf("legal set: %v", err)
	}
	big := map[string][]string{}
	for i := 0; i < metadata.MaxNodePropKeys+1; i++ {
		big["k"+string(rune('a'+i%26))+string(rune('a'+i/26))] = []string{"v"}
	}
	if err := repo.ValidatePropSet(big); !isPropsInvalid(err) {
		t.Fatalf("key-count cap err = %v", err)
	}
	values := make([]string, metadata.MaxPropValues+1)
	for i := range values {
		values[i] = strings.Repeat("v", i+1)
	}
	if err := repo.ValidatePropSet(map[string][]string{"k": values}); !isPropsInvalid(err) {
		t.Fatalf("value-count cap err = %v", err)
	}
}

func isPropsInvalid(err error) bool {
	return errors.Is(err, repo.ErrInvalidProperties)
}
