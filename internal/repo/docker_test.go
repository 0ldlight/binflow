package repo_test

import (
	"context"
	"errors"
	"fmt"
	"strings"
	"testing"
	"time"

	"github.com/lzwzzy/binflow/internal/metadata"
	"github.com/lzwzzy/binflow/internal/repo"
)

// ---- docker test harness ----

// digestOf builds a bare 64-hex digest out of any short seed (deterministic
// padding; these are NOT content hashes — the store layer keys on the string
// and this layer never re-derives it).
func digestOf(seed string) string {
	s := seed
	for len(s) < 64 {
		s += "."
	}
	var hexed []byte
	for i := 0; i < 64; i++ {
		c := s[i]
		switch {
		case c >= '0' && c <= '9':
			hexed = append(hexed, c)
		default:
			hexed = append(hexed, byte('a'+int(c)%6))
		}
	}
	return string(hexed)
}

// mustCreateDockerRepo creates a local docker repository or fails the test.
func mustCreateDockerRepo(t TB, e *env, key string) {
	t.Helper()
	if _, err := e.svc.CreateRepo(context.Background(), admin(), &metadata.Repo{
		RepoKey: key, Type: repo.TypeLocal, PackageType: repo.PackageDocker,
	}); err != nil {
		t.Fatalf("CreateRepo(docker %s): %v", key, err)
	}
}

// fakeDockerStack is the fake-stack column of the matrix: hookDocker wraps a
// real store and can fail selected docker sub-store calls. A fake with a nil
// fail hook is pass-through, which is exactly the real stack's behavior —
// the two stacks share the table rows.
func fakeDockerStack(md metadata.Store, fail func(op string) error) *hookDocker {
	return &hookDocker{Store: md, fail: fail}
}

// putManifest is the docker publish helper (blobs are assumed committed by
// the adapter — the service never re-derives them).
func putManifest(t TB, e *env, p *repo.Principal, repoKey, image, digest, tag string, blobs ...string) *repo.PutManifestResult {
	t.Helper()
	res, err := e.svc.PutManifest(context.Background(), p, repoKey, image, digest, tag,
		"application/vnd.docker.distribution.manifest.v2+json", 100, mkRefs(repoKey, image, digest, blobs...))
	if err != nil {
		t.Fatalf("PutManifest(%s/%s:%s): %v", repoKey, image, tag, err)
	}
	return res
}

// mkRefs builds the ref set out of bare blob digests.
func mkRefs(repoKey, image, digest string, blobs ...string) []*metadata.DockerRef {
	refs := make([]*metadata.DockerRef, len(blobs))
	for i, b := range blobs {
		refs[i] = &metadata.DockerRef{
			RepoKey: repoKey, Image: image, ManifestDigest: digest,
			BlobDigest: b, ChildMediaType: "application/vnd.docker.container.image.v1+json",
		}
	}
	return refs
}

// ---- AC ② matrix scaffolding ----

// dockerUseCase is one row of the fake/real-stack table below.
type dockerUseCase struct {
	name   string
	call   func(e *env) error
	want   error
	verify func(t *testing.T, e *env)
}

// runDockerUseCases executes the rows against BOTH stacks: every row runs
// once on the plain real store and once on a hookDocker-wrapped store whose
// fail hook is nil (pass-through decorator — exercising the same path
// through the fake's type). Rows that need actual fault injection build
// their own environments.
func runDockerUseCases(t *testing.T, rows []dockerUseCase) {
	t.Helper()
	stacks := []struct {
		name  string
		mount func(md metadata.Store) metadata.Store
	}{
		{"real", nil},
		{"fake", func(md metadata.Store) metadata.Store { return fakeDockerStack(md, nil) }},
	}
	for _, tt := range rows {
		for _, stack := range stacks {
			t.Run(tt.name+"/"+stack.name, func(t *testing.T) {
				e := newEnvCustom(t, stack.mount)
				mustCreateDockerRepo(t, e, "docker-local")
				err := tt.call(e)
				if tt.want == nil {
					if err != nil {
						t.Fatalf("unexpected error: %v", err)
					}
					if tt.verify != nil {
						tt.verify(t, e)
					}
					return
				}
				if !errors.Is(err, tt.want) {
					t.Fatalf("error = %v, want %v", err, tt.want)
				}
			})
		}
	}
}

// TestDockerUseCaseMatrix drives the AC ② surface through both stacks.
func TestDockerUseCaseMatrix(t *testing.T) {
	d := digestOf("matrix")
	rows := []dockerUseCase{
		{
			name: "publish and resolve",
			call: func(e *env) error {
				ctx := context.Background()
				res, err := e.svc.PutManifest(ctx, admin(), "docker-local", "app", d, "v1",
					"application/vnd.docker.distribution.manifest.v2+json", 42,
					mkRefs("docker-local", "app", d, digestOf("cfg")))
				if err != nil {
					return err
				}
				if res.TagRepointed != "" {
					return fmt.Errorf("fresh publish reported a repoint: %q", res.TagRepointed)
				}
				if _, err := e.svc.ResolveManifest(ctx, admin(), "docker-local", "app", d); err != nil {
					return err
				}
				if _, err := e.svc.ResolveTag(ctx, admin(), "docker-local", "app", "v1"); err != nil {
					return err
				}
				return nil
			},
		},
		{
			name: "list tags and images",
			call: func(e *env) error {
				ctx := context.Background()
				if _, err := e.svc.PutManifest(ctx, admin(), "docker-local", "app", d, "v1", "m", 1, nil); err != nil {
					return err
				}
				tags, err := e.svc.ListTags(ctx, admin(), "docker-local", "app", 0, "")
				if err != nil {
					return err
				}
				if len(tags) != 1 || tags[0].Tag != "v1" {
					return fmt.Errorf("tags = %v", tagNames(tags))
				}
				images, err := e.svc.ListImages(ctx, admin(), "docker-local", 0, "")
				if err != nil {
					return err
				}
				if len(images) != 1 || images[0] != "docker-local/app" {
					return fmt.Errorf("images = %v", images)
				}
				return nil
			},
		},
		{
			name: "delete manifest cascades",
			call: func(e *env) error {
				ctx := context.Background()
				if _, err := e.svc.PutManifest(ctx, admin(), "docker-local", "app", d, "v1",
					"m", 1, mkRefs("docker-local", "app", d, digestOf("cfg"))); err != nil {
					return err
				}
				if err := e.svc.DeleteManifest(ctx, admin(), "docker-local", "app", d); err != nil {
					return err
				}
				if _, err := e.svc.ResolveManifest(ctx, admin(), "docker-local", "app", d); !errors.Is(err, repo.ErrManifestNotFound) {
					return fmt.Errorf("manifest survived its own delete: %w", err)
				}
				if _, err := e.svc.ResolveTag(ctx, admin(), "docker-local", "app", "v1"); !errors.Is(err, repo.ErrTagNotFound) {
					return fmt.Errorf("tag survived the manifest delete: %w", err)
				}
				return nil
			},
		},
		{
			name: "repo teardown clears the docker tables",
			call: func(e *env) error {
				ctx := context.Background()
				if _, err := e.svc.PutManifest(ctx, admin(), "docker-local", "app", d, "v1",
					"m", 1, mkRefs("docker-local", "app", d, digestOf("cfg"))); err != nil {
					return err
				}
				return e.svc.DeleteRepo(ctx, admin(), "docker-local", true)
			},
			verify: func(t *testing.T, e *env) {
				assertDockerTablesEmpty(context.Background(), t, e, "docker-local")
			},
		},
		{
			name: "anonymous publish is unauthorized",
			call: func(e *env) error {
				_, err := e.svc.PutManifest(context.Background(), nil, "docker-local", "app", d, "v1", "m", 1, nil)
				return err
			},
			want: repo.ErrUnauthorized,
		},
		{
			name: "ungranted publish is forbidden",
			call: func(e *env) error {
				_, err := e.svc.PutManifest(context.Background(), alice(), "docker-local", "app", d, "v1", "m", 1, nil)
				return err
			},
			want: repo.ErrForbidden,
		},
		{
			name: "unknown repo",
			call: func(e *env) error {
				_, err := e.svc.PutManifest(context.Background(), admin(), "ghost", "app", d, "v1", "m", 1, nil)
				return err
			},
			want: repo.ErrRepoNotFound,
		},
		{
			name: "short digest",
			call: func(e *env) error {
				_, err := e.svc.PutManifest(context.Background(), admin(), "docker-local", "app", "abc", "v1", "m", 1, nil)
				return err
			},
			want: repo.ErrInvalidDigest,
		},
		{
			name: "bad tag",
			call: func(e *env) error {
				_, err := e.svc.PutManifest(context.Background(), admin(), "docker-local", "app", d, "1bad tag", "m", 1, nil)
				return err
			},
			want: repo.ErrInvalidTag,
		},
		{
			name: "bad image",
			call: func(e *env) error {
				_, err := e.svc.PutManifest(context.Background(), admin(), "docker-local", "a/../b", d, "v1", "m", 1, nil)
				return err
			},
			want: repo.ErrInvalidImage,
		},
	}
	runDockerUseCases(t, rows)
}

// ---- AC ①: repository-type enablement ----

// TestCreateDockerRepoEnabled: FR-7-AC1 — local+docker creates; C06 reads the
// packageType back; remote/virtual docker stays M3; reserved keys and the
// generic matrix are unchanged.
func TestCreateDockerRepoEnabled(t *testing.T) {
	ctx := context.Background()

	t.Run("local docker creates and reads back (D01/C06)", func(t *testing.T) {
		e := newEnv(t)
		r, err := e.svc.CreateRepo(ctx, admin(), &metadata.Repo{
			RepoKey: "docker-local", Type: repo.TypeLocal, PackageType: repo.PackageDocker,
			Description: "registry",
		})
		if err != nil {
			t.Fatalf("CreateRepo(local docker): %v", err)
		}
		if r.PackageType != repo.PackageDocker {
			t.Fatalf("packageType = %q, want docker", r.PackageType)
		}
		got, err := e.svc.GetRepo(ctx, admin(), "docker-local")
		if err != nil {
			t.Fatalf("GetRepo: %v", err)
		}
		if got.PackageType != repo.PackageDocker || got.Type != repo.TypeLocal {
			t.Fatalf("round trip = %s/%s, want local/docker", got.Type, got.PackageType)
		}
		if got.Config != "{}" {
			t.Fatalf("default config = %q", got.Config)
		}
	})

	t.Run("remote and virtual docker remain M3", func(t *testing.T) {
		e := newEnv(t)
		for _, rclass := range []string{repo.TypeRemote, repo.TypeVirtual} {
			_, err := e.svc.CreateRepo(ctx, admin(), &metadata.Repo{
				RepoKey: "d-" + rclass, Type: rclass, PackageType: repo.PackageDocker,
			})
			if !errors.Is(err, repo.ErrRepoTypeNotSupported) {
				t.Fatalf("CreateRepo(%s docker) error = %v, want ErrRepoTypeNotSupported", rclass, err)
			}
			if !strings.Contains(err.Error(), "M3") {
				t.Fatalf("error does not mention M3: %v", err)
			}
		}
	})

	t.Run("reserved keys still refused", func(t *testing.T) {
		e := newEnv(t)
		for _, key := range []string{"api", "v2"} {
			_, err := e.svc.CreateRepo(ctx, admin(), &metadata.Repo{
				RepoKey: key, Type: repo.TypeLocal, PackageType: repo.PackageDocker,
			})
			if !errors.Is(err, repo.ErrReservedRepoKey) {
				t.Fatalf("CreateRepo(%q) error = %v, want ErrReservedRepoKey", key, err)
			}
		}
	})

	t.Run("generic matrix unchanged", func(t *testing.T) {
		e := newEnv(t)
		if _, err := e.svc.CreateRepo(ctx, admin(), &metadata.Repo{
			RepoKey: "g-one", Type: repo.TypeLocal, PackageType: repo.PackageGeneric,
		}); err != nil {
			t.Fatalf("local generic: %v", err)
		}
		if _, err := e.svc.CreateRepo(ctx, admin(), &metadata.Repo{
			RepoKey: "g-two", Type: repo.TypeRemote, PackageType: repo.PackageGeneric,
		}); !errors.Is(err, repo.ErrRepoTypeNotSupported) {
			t.Fatalf("remote generic error = %v", err)
		}
	})

	t.Run("package type immutable on docker repos too", func(t *testing.T) {
		e := newEnv(t)
		mustCreateDockerRepo(t, e, "docker-local")
		if _, err := e.svc.UpdateRepo(ctx, admin(), &metadata.Repo{
			RepoKey: "docker-local", PackageType: repo.PackageGeneric,
		}); !errors.Is(err, repo.ErrInvalidRepoType) {
			t.Fatalf("package flip error = %v, want ErrInvalidRepoType", err)
		}
	})
}

// ---- AC ②: PutManifest ----

func TestDockerPutManifest(t *testing.T) {
	ctx := context.Background()

	t.Run("publish writes node plus index rows", func(t *testing.T) {
		e := newEnv(t)
		mustCreateDockerRepo(t, e, "docker-local")
		d := digestOf("manifest-one")
		res := putManifest(t, e, admin(), "docker-local", "app", d, "latest", digestOf("config"), digestOf("layer"))
		if res.TagRepointed != "" {
			t.Fatalf("fresh tag reported a repoint: %q", res.TagRepointed)
		}
		// The index row.
		m, err := e.md.Docker().GetManifest(ctx, "docker-local", "app", d)
		if err != nil {
			t.Fatalf("manifest row: %v", err)
		}
		if m.MediaType != "application/vnd.docker.distribution.manifest.v2+json" || m.Size != 100 || m.CreatedBy != "admin" {
			t.Fatalf("manifest row = %+v", m)
		}
		// The node at the layout path, mime = manifest mediaType.
		n, err := e.md.Nodes().Get(ctx, "docker-local", "app/manifests/"+d)
		if err != nil {
			t.Fatalf("layout node: %v", err)
		}
		if n.Sha256 != d || n.Mime != m.MediaType {
			t.Fatalf("layout node = %+v", n)
		}
		// The tag pointer and the ref set.
		tag, err := e.md.Docker().GetTag(ctx, "docker-local", "app", "latest")
		if err != nil || tag.Digest != d {
			t.Fatalf("tag row = %+v, %v", tag, err)
		}
		refs, err := e.md.Docker().ListRefsByManifest(ctx, "docker-local", "app", d)
		if err != nil || len(refs) != 2 {
			t.Fatalf("refs = %+v, %v", refs, err)
		}
	})

	t.Run("nested image names keep their slash form", func(t *testing.T) {
		e := newEnv(t)
		mustCreateDockerRepo(t, e, "docker-local")
		d := digestOf("nested")
		putManifest(t, e, admin(), "docker-local", "acme/team/app", d, "v1")
		if _, err := e.md.Nodes().Get(ctx, "docker-local", "acme/team/app/manifests/"+d); err != nil {
			t.Fatalf("nested layout node: %v", err)
		}
	})

	t.Run("tag overwrite repoints (FR-9-AC2 / Q4)", func(t *testing.T) {
		e := newEnv(t)
		mustCreateDockerRepo(t, e, "docker-local")
		d1, d2 := digestOf("body-one"), digestOf("body-two")
		putManifest(t, e, admin(), "docker-local", "app", d1, "latest")
		e.clk.Advance(5 * time.Minute)
		res := putManifest(t, e, admin(), "docker-local", "app", d2, "latest")
		if res.TagRepointed != d1 {
			t.Fatalf("TagRepointed = %q, want %q", res.TagRepointed, d1)
		}
		tag, err := e.md.Docker().GetTag(ctx, "docker-local", "app", "latest")
		if err != nil || tag.Digest != d2 {
			t.Fatalf("tag after repoint = %+v, %v", tag, err)
		}
		// The old manifest is still resolvable by digest.
		if _, err := e.svc.ResolveManifest(ctx, admin(), "docker-local", "app", d1); err != nil {
			t.Fatalf("old manifest by digest: %v", err)
		}
		// And both nodes live at their own layout paths.
		for _, d := range []string{d1, d2} {
			if _, err := e.md.Nodes().Get(ctx, "docker-local", "app/manifests/"+d); err != nil {
				t.Fatalf("node %s: %v", d, err)
			}
		}
	})

	t.Run("same-digest repush is an idempotent republish", func(t *testing.T) {
		e := newEnv(t)
		mustCreateDockerRepo(t, e, "docker-local")
		d := digestOf("same")
		putManifest(t, e, admin(), "docker-local", "app", d, "v1", digestOf("cfg"))
		e.clk.Advance(5 * time.Minute)
		// alice holds no grants at all: the same-digest repush is an
		// idempotent republish that (like the generic Put's retransmit)
		// skips the write gate, and BOTH the node and the manifest index row
		// keep their provenance — re-announcing a digest must not be able to
		// drift created_by/media_type/size (review B2).
		res, err := e.svc.PutManifest(ctx, alice(), "docker-local", "app", d, "v1",
			"application/vnd.oci.image.manifest.v1+json", 999,
			mkRefs("docker-local", "app", d, digestOf("cfg")))
		if err != nil {
			t.Fatalf("idempotent repush by unprivileged user: %v", err)
		}
		if res.TagRepointed != "" {
			t.Fatalf("same-digest repush reported a repoint: %q", res.TagRepointed)
		}
		n, nodeErr := e.md.Nodes().Get(ctx, "docker-local", "app/manifests/"+d)
		if nodeErr != nil {
			t.Fatalf("node: %v", nodeErr)
		}
		if n.CreatedBy != "admin" {
			t.Fatalf("repush rewrote node provenance: %+v", n)
		}
		// The manifest row is untouched: provenance AND serving columns.
		m, mErr := e.md.Docker().GetManifest(ctx, "docker-local", "app", d)
		if mErr != nil {
			t.Fatalf("manifest row: %v", mErr)
		}
		if m.CreatedBy != "admin" {
			t.Fatalf("repush rewrote manifest provenance: %+v", m)
		}
		if m.MediaType != "application/vnd.docker.distribution.manifest.v2+json" || m.Size != 100 {
			t.Fatalf("repush drifted serving columns: %+v", m)
		}
		// The result reports the STORED row, not the caller's claim.
		if res.Manifest.CreatedBy != "admin" ||
			res.Manifest.MediaType != "application/vnd.docker.distribution.manifest.v2+json" ||
			res.Manifest.Size != 100 {
			t.Fatalf("result reports the re-announcement, not the stored row: %+v", res.Manifest)
		}
		// Tag freshness still moves (the tag upsert is the one write that
		// re-runs: repointing is legal and so is refreshing the pointer).
		tag, tErr := e.md.Docker().GetTag(ctx, "docker-local", "app", "v1")
		if tErr != nil || tag.UpdatedBy != "alice" {
			t.Fatalf("tag row after repush = %+v, %v", tag, tErr)
		}
	})

	t.Run("digest-only push (empty tag) skips the tag row", func(t *testing.T) {
		e := newEnv(t)
		mustCreateDockerRepo(t, e, "docker-local")
		d := digestOf("untagged")
		putManifest(t, e, admin(), "docker-local", "app", d, "")
		if _, err := e.md.Docker().GetTag(ctx, "docker-local", "app", ""); !errors.Is(err, metadata.ErrTagNotFound) {
			t.Fatalf("empty tag row visible: %v", err)
		}
		if _, err := e.svc.ListTags(ctx, admin(), "docker-local", "app", 0, ""); !errors.Is(err, repo.ErrImageNotFound) {
			t.Fatalf("image without tags error = %v, want ErrImageNotFound", err)
		}
	})

	t.Run("permission pair", func(t *testing.T) {
		e := newEnv(t)
		mustCreateDockerRepo(t, e, "docker-local")
		d := digestOf("perm")

		if _, err := e.svc.PutManifest(ctx, nil, "docker-local", "app", d, "latest", "m", 1, nil); !errors.Is(err, repo.ErrUnauthorized) {
			t.Fatalf("anonymous publish error = %v, want ErrUnauthorized", err)
		}
		if _, err := e.svc.PutManifest(ctx, alice(), "docker-local", "app", d, "latest", "m", 1, nil); !errors.Is(err, repo.ErrForbidden) {
			t.Fatalf("ungranted publish error = %v, want ErrForbidden", err)
		}
		// A write grant scoped to the image folder publishes; another image
		// stays closed (the ACL path is the image folder).
		e.az.add("alice", repo.ActionWrite, "app/")
		if _, err := e.svc.PutManifest(ctx, alice(), "docker-local", "app", d, "latest", "m", 1, nil); err != nil {
			t.Fatalf("granted publish: %v", err)
		}
		if _, err := e.svc.PutManifest(ctx, alice(), "docker-local", "other", d, "latest", "m", 1, nil); !errors.Is(err, repo.ErrForbidden) {
			t.Fatalf("out-of-scope publish error = %v, want ErrForbidden", err)
		}
	})

	t.Run("validation matrix", func(t *testing.T) {
		e := newEnv(t)
		mustCreateDockerRepo(t, e, "docker-local")
		rows := []struct {
			name    string
			repoKey string
			image   string
			digest  string
			tag     string
			want    error
		}{
			{"empty image", "docker-local", "", digestOf("x"), "v1", repo.ErrInvalidImage},
			{"dot segment image", "docker-local", "a/../b", digestOf("x"), "v1", repo.ErrInvalidImage},
			{"double slash image", "docker-local", "a//b", digestOf("x"), "v1", repo.ErrInvalidImage},
			{"short digest", "docker-local", "app", "abc", "v1", repo.ErrInvalidDigest},
			{"prefixed digest", "docker-local", "app", "sha256:" + digestOf("x"), "v1", repo.ErrInvalidDigest},
			{"uppercase digest", "docker-local", "app", strings.ToUpper(digestOf("x"))[:64], "v1", repo.ErrInvalidDigest},
			{"non-hex digest", "docker-local", "app", digestOf("x")[:63] + "z", "v1", repo.ErrInvalidDigest},
			{"bad tag first char", "docker-local", "app", digestOf("x"), ".latest", repo.ErrInvalidTag},
			{"bad tag charset", "docker-local", "app", digestOf("x"), "la test", repo.ErrInvalidTag},
			{"empty tag is digest-only", "docker-local", "app", digestOf("x"), "", nil},
			{"unknown repo", "ghost", "app", digestOf("x"), "v1", repo.ErrRepoNotFound},
			{"generic repo", "generic-local", "app", digestOf("x"), "v1", repo.ErrRepoTypeNotSupported},
		}
		for _, tt := range rows {
			t.Run(tt.name, func(t *testing.T) {
				if tt.name == "generic repo" {
					mustCreateRepo(t, e, "generic-local")
				}
				_, err := e.svc.PutManifest(ctx, admin(), tt.repoKey, tt.image, tt.digest, tt.tag, "m", 1, nil)
				if tt.want == nil {
					if err != nil {
						t.Fatalf("unexpected error: %v", err)
					}
					return
				}
				if !errors.Is(err, tt.want) {
					t.Fatalf("error = %v, want %v", err, tt.want)
				}
			})
		}
		// mediaType and size have their own guards (manifest-descriptor
		// sentinels, not image-name ones — review B3).
		if _, err := e.svc.PutManifest(ctx, admin(), "docker-local", "app", digestOf("x"), "v1", "", 1, nil); !errors.Is(err, repo.ErrInvalidManifest) {
			t.Fatalf("empty mediaType error = %v, want ErrInvalidManifest", err)
		}
		if _, err := e.svc.PutManifest(ctx, admin(), "docker-local", "app", digestOf("x"), "v1", "m", -1, nil); !errors.Is(err, repo.ErrInvalidManifest) {
			t.Fatalf("negative size error = %v, want ErrInvalidManifest", err)
		}
		if _, err := e.svc.PutManifest(ctx, admin(), "docker-local", "app", digestOf("x"), "v1", "m", 1,
			[]*metadata.DockerRef{{RepoKey: "docker-local", BlobDigest: "short"}}); !errors.Is(err, repo.ErrInvalidDigest) {
			t.Fatalf("short ref digest error = %v", err)
		}
	})
}

// ---- AC ②: resolve by digest / by tag ----

func TestDockerResolve(t *testing.T) {
	ctx := context.Background()
	e := newEnv(t)
	mustCreateDockerRepo(t, e, "docker-local")
	d1, d2 := digestOf("one"), digestOf("two")
	putManifest(t, e, admin(), "docker-local", "app", d1, "v1")
	putManifest(t, e, admin(), "docker-local", "app", d2, "latest") // repointed from v1's neighbor

	t.Run("by digest", func(t *testing.T) {
		m, err := e.svc.ResolveManifest(ctx, admin(), "docker-local", "app", d1)
		if err != nil || m.Digest != d1 {
			t.Fatalf("ResolveManifest = %+v, %v", m, err)
		}
		if _, err := e.svc.ResolveManifest(ctx, admin(), "docker-local", "app", digestOf("missing")); !errors.Is(err, repo.ErrManifestNotFound) {
			t.Fatalf("missing digest error = %v, want ErrManifestNotFound", err)
		}
	})

	t.Run("by tag", func(t *testing.T) {
		tag, err := e.svc.ResolveTag(ctx, admin(), "docker-local", "app", "latest")
		if err != nil || tag.Digest != d2 {
			t.Fatalf("ResolveTag = %+v, %v", tag, err)
		}
		if _, err := e.svc.ResolveTag(ctx, admin(), "docker-local", "app", "nope"); !errors.Is(err, repo.ErrTagNotFound) {
			t.Fatalf("missing tag error = %v, want ErrTagNotFound", err)
		}
	})

	t.Run("read gate", func(t *testing.T) {
		if _, err := e.svc.ResolveTag(ctx, nil, "docker-local", "app", "latest"); !errors.Is(err, repo.ErrUnauthorized) {
			t.Fatalf("anonymous resolve error = %v", err)
		}
		if _, err := e.svc.ResolveManifest(ctx, alice(), "docker-local", "app", d1); !errors.Is(err, repo.ErrForbidden) {
			t.Fatalf("alice resolve error = %v", err)
		}
		e.az.add("alice", repo.ActionRead, "app/")
		if _, err := e.svc.ResolveManifest(ctx, alice(), "docker-local", "app", d1); err != nil {
			t.Fatalf("granted resolve: %v", err)
		}
	})

	t.Run("generic repo refuses the docker plane", func(t *testing.T) {
		mustCreateRepo(t, e, "generic-local")
		if _, err := e.svc.ResolveManifest(ctx, admin(), "generic-local", "app", d1); !errors.Is(err, repo.ErrRepoTypeNotSupported) {
			t.Fatalf("generic resolve error = %v", err)
		}
	})

	t.Run("audit records the deploy and the pulls", func(t *testing.T) {
		_ = ctx
		got := e.au.actions()
		var deploys, downloads int
		for _, a := range got {
			switch a {
			case repo.AuditActionDeploy:
				deploys++
			case repo.AuditActionDownload:
				downloads++
			}
		}
		if deploys != 2 {
			t.Fatalf("deploys = %d, want 2 (actions %v)", deploys, got)
		}
		// 2 anonymous refusals emit nothing; the granted by-digest and
		// by-tag resolves emit downloads.
		if downloads != 3 {
			t.Fatalf("downloads = %d, want 3 (actions %v)", downloads, got)
		}
	})
}

// ---- AC ②: ListTags / ListImages ----

func TestDockerListTags(t *testing.T) {
	e := newEnv(t)
	mustCreateDockerRepo(t, e, "docker-local")
	app := digestOf("app")
	putManifest(t, e, admin(), "docker-local", "app", app, "v2")
	putManifest(t, e, admin(), "docker-local", "app", digestOf("app-2"), "v10")
	putManifest(t, e, admin(), "docker-local", "app", digestOf("app-3"), "v1")
	putManifest(t, e, admin(), "docker-local", "app", digestOf("app-4"), "Beta") // uppercase sorts first

	t.Run("lexicographic order", func(t *testing.T) {
		tags, err := e.svc.ListTags(context.Background(), admin(), "docker-local", "app", 0, "")
		if err != nil {
			t.Fatalf("ListTags: %v", err)
		}
		want := []string{"Beta", "v1", "v10", "v2"}
		if len(tags) != len(want) {
			t.Fatalf("tags = %v, want %v", tagNames(tags), want)
		}
		for i := range want {
			if tags[i].Tag != want[i] {
				t.Fatalf("tags = %v, want %v (byte order: v10 < v2)", tagNames(tags), want)
			}
		}
	})

	t.Run("n slices, last is exclusive", func(t *testing.T) {
		ctx := context.Background()
		p1, err := e.svc.ListTags(ctx, admin(), "docker-local", "app", 2, "")
		if err != nil || len(p1) != 2 || p1[0].Tag != "Beta" || p1[1].Tag != "v1" {
			t.Fatalf("page 1 = %v, %v", tagNames(p1), err)
		}
		p2, err := e.svc.ListTags(ctx, admin(), "docker-local", "app", 2, "v1")
		if err != nil || len(p2) != 2 || p2[0].Tag != "v10" || p2[1].Tag != "v2" {
			t.Fatalf("page 2 = %v, %v", tagNames(p2), err)
		}
		// The cursor itself is NOT part of the page (official semantics).
		p3, err := e.svc.ListTags(ctx, admin(), "docker-local", "app", 2, "Beta")
		if err != nil || len(p3) != 2 || p3[0].Tag != "v1" {
			t.Fatalf("page after Beta = %v, %v", tagNames(p3), err)
		}
		// A cursor past the tail yields an empty page, not an error.
		p4, err := e.svc.ListTags(ctx, admin(), "docker-local", "app", 2, "zz")
		if err != nil || len(p4) != 0 {
			t.Fatalf("page past tail = %v, %v", tagNames(p4), err)
		}
	})

	t.Run("unknown image vs image without tags", func(t *testing.T) {
		ctx := context.Background()
		if _, err := e.svc.ListTags(ctx, admin(), "docker-local", "ghost", 0, ""); !errors.Is(err, repo.ErrImageNotFound) {
			t.Fatalf("unknown image error = %v, want ErrImageNotFound", err)
		}
		// untagged has a manifest but no tags: NAME_UNKNOWN per the spec row
		// (the catalog still lists it).
		putManifest(t, e, admin(), "docker-local", "untagged", digestOf("u"), "")
		if _, err := e.svc.ListTags(ctx, admin(), "docker-local", "untagged", 0, ""); !errors.Is(err, repo.ErrImageNotFound) {
			t.Fatalf("untagged image error = %v, want ErrImageNotFound", err)
		}
	})

	t.Run("read gate", func(t *testing.T) {
		if _, err := e.svc.ListTags(context.Background(), nil, "docker-local", "app", 0, ""); !errors.Is(err, repo.ErrUnauthorized) {
			t.Fatalf("anonymous ListTags error = %v", err)
		}
		if _, err := e.svc.ListTags(context.Background(), alice(), "docker-local", "app", 0, ""); !errors.Is(err, repo.ErrForbidden) {
			t.Fatalf("alice ListTags error = %v", err)
		}
		// last is validated like a tag but reported as a cursor error.
		if _, err := e.svc.ListTags(context.Background(), admin(), "docker-local", "app", 0, "not a cursor"); !errors.Is(err, repo.ErrInvalidCursor) {
			t.Fatalf("bad cursor error = %v, want ErrInvalidCursor", err)
		}
	})
}

func TestDockerListImages(t *testing.T) {
	ctx := context.Background()
	e := newEnv(t)
	mustCreateDockerRepo(t, e, "docker-local")
	mustCreateDockerRepo(t, e, "docker-two")
	putManifest(t, e, admin(), "docker-local", "zulu", digestOf("z"), "v1")
	putManifest(t, e, admin(), "docker-local", "acme/team/app", digestOf("n1"), "v1")
	putManifest(t, e, admin(), "docker-local", "acme/api", digestOf("n2"), "v1")
	putManifest(t, e, admin(), "docker-two", "other", digestOf("n3"), "v1")

	t.Run("repoKey/image form, lexicographic, per-repo scope", func(t *testing.T) {
		got, err := e.svc.ListImages(ctx, admin(), "docker-local", 0, "")
		if err != nil {
			t.Fatalf("ListImages: %v", err)
		}
		want := []string{"docker-local/acme/api", "docker-local/acme/team/app", "docker-local/zulu"}
		if len(got) != len(want) {
			t.Fatalf("images = %v, want %v", got, want)
		}
		for i := range want {
			if got[i] != want[i] {
				t.Fatalf("images = %v, want %v", got, want)
			}
		}
	})

	t.Run("n and last slice on the full-name key", func(t *testing.T) {
		p1, err := e.svc.ListImages(ctx, admin(), "docker-local", 2, "")
		if err != nil || len(p1) != 2 || p1[1] != "docker-local/acme/team/app" {
			t.Fatalf("page 1 = %v, %v", p1, err)
		}
		p2, err := e.svc.ListImages(ctx, admin(), "docker-local", 2, "docker-local/acme/team/app")
		if err != nil || len(p2) != 1 || p2[0] != "docker-local/zulu" {
			t.Fatalf("page 2 = %v, %v", p2, err)
		}
	})

	t.Run("a cursor from another repo is refused", func(t *testing.T) {
		if _, err := e.svc.ListImages(ctx, admin(), "docker-local", 2, "docker-two/other"); !errors.Is(err, repo.ErrInvalidCursor) {
			t.Fatalf("foreign cursor error = %v, want ErrInvalidCursor", err)
		}
	})

	t.Run("read gate", func(t *testing.T) {
		if _, err := e.svc.ListImages(ctx, nil, "docker-local", 0, ""); !errors.Is(err, repo.ErrUnauthorized) {
			t.Fatalf("anonymous catalog error = %v", err)
		}
		if _, err := e.svc.ListImages(ctx, alice(), "docker-local", 0, ""); !errors.Is(err, repo.ErrForbidden) {
			t.Fatalf("alice catalog error = %v", err)
		}
		// A read grant on the repo root opens the catalog.
		e.az.add("alice", repo.ActionRead, "")
		if got, err := e.svc.ListImages(ctx, alice(), "docker-local", 0, ""); err != nil || len(got) != 3 {
			t.Fatalf("granted catalog = %v, %v", got, err)
		}
	})

	t.Run("generic repo refuses", func(t *testing.T) {
		mustCreateRepo(t, e, "generic-local")
		if _, err := e.svc.ListImages(ctx, admin(), "generic-local", 0, ""); !errors.Is(err, repo.ErrRepoTypeNotSupported) {
			t.Fatalf("generic catalog error = %v", err)
		}
	})
}

// ---- AC ②: DeleteManifest ----

func TestDockerDeleteManifest(t *testing.T) {
	ctx := context.Background()

	t.Run("cascade clears tags and refs, spares blobs (FR-9-AC6/AC7)", func(t *testing.T) {
		e := newEnv(t)
		mustCreateDockerRepo(t, e, "docker-local")
		d1, d2 := digestOf("m1"), digestOf("m2")
		layer := digestOf("shared-layer")
		putManifest(t, e, admin(), "docker-local", "app", d1, "v1", layer, digestOf("cfg1"))
		putManifest(t, e, admin(), "docker-local", "app", d2, "v2", layer, digestOf("cfg2"))

		if err := e.svc.DeleteManifest(ctx, admin(), "docker-local", "app", d1); err != nil {
			t.Fatalf("DeleteManifest: %v", err)
		}
		// Index row gone; its tag with it; the OTHER manifest's tag and the
		// shared ref survive.
		if _, err := e.md.Docker().GetManifest(ctx, "docker-local", "app", d1); !errors.Is(err, metadata.ErrManifestNotFound) {
			t.Fatalf("manifest row survived: %v", err)
		}
		if _, err := e.md.Docker().GetTag(ctx, "docker-local", "app", "v1"); !errors.Is(err, metadata.ErrTagNotFound) {
			t.Fatalf("tag v1 survived: %v", err)
		}
		if tag, err := e.md.Docker().GetTag(ctx, "docker-local", "app", "v2"); err != nil || tag.Digest != d2 {
			t.Fatalf("tag v2 damaged: %+v, %v", tag, err)
		}
		refs, err := e.md.Docker().ListRefsByManifest(ctx, "docker-local", "app", d1)
		if err != nil || len(refs) != 0 {
			t.Fatalf("refs of deleted manifest survived: %+v, %v", refs, err)
		}
		if ok, err := e.md.Docker().RefsByBlob(ctx, "docker-local", layer); err != nil || !ok {
			t.Fatalf("shared layer lost its surviving reference: %v, %v", ok, err)
		}
		// The node is gone; the layer's node (had the adapter created one)
		// and the blob itself are untouched — deleting a manifest never
		// reclaims bytes.
		if _, err := e.md.Nodes().Get(ctx, "docker-local", "app/manifests/"+d1); !errors.Is(err, metadata.ErrNodeNotFound) {
			t.Fatalf("layout node survived: %v", err)
		}
		if _, err := e.svc.ResolveManifest(ctx, admin(), "docker-local", "app", d1); !errors.Is(err, repo.ErrManifestNotFound) {
			t.Fatalf("resolve after delete = %v", err)
		}
		if _, err := e.svc.ResolveTag(ctx, admin(), "docker-local", "app", "v1"); !errors.Is(err, repo.ErrTagNotFound) {
			t.Fatalf("tag resolve after delete = %v", err)
		}
	})

	t.Run("deleting the last manifest empties the catalog entry", func(t *testing.T) {
		e := newEnv(t)
		mustCreateDockerRepo(t, e, "docker-local")
		d := digestOf("solo")
		putManifest(t, e, admin(), "docker-local", "solo", d, "v1")
		if err := e.svc.DeleteManifest(ctx, admin(), "docker-local", "solo", d); err != nil {
			t.Fatalf("DeleteManifest: %v", err)
		}
		got, err := e.svc.ListImages(ctx, admin(), "docker-local", 0, "")
		if err != nil || len(got) != 0 {
			t.Fatalf("catalog after last delete = %v, %v", got, err)
		}
	})

	t.Run("permission gate", func(t *testing.T) {
		e := newEnv(t)
		mustCreateDockerRepo(t, e, "docker-local")
		d := digestOf("perm-del")
		putManifest(t, e, admin(), "docker-local", "app", d, "v1")
		if err := e.svc.DeleteManifest(ctx, nil, "docker-local", "app", d); !errors.Is(err, repo.ErrUnauthorized) {
			t.Fatalf("anonymous delete error = %v", err)
		}
		// alice holds read+write but not delete.
		e.az.add("alice", repo.ActionRead, "")
		e.az.add("alice", repo.ActionWrite, "")
		if err := e.svc.DeleteManifest(ctx, alice(), "docker-local", "app", d); !errors.Is(err, repo.ErrForbidden) {
			t.Fatalf("alice delete error = %v, want ErrForbidden", err)
		}
		e.az.add("alice", repo.ActionDelete, "")
		if err := e.svc.DeleteManifest(ctx, alice(), "docker-local", "app", d); err != nil {
			t.Fatalf("alice delete with grant: %v", err)
		}
	})

	t.Run("missing manifest leaves foreign rows alone", func(t *testing.T) {
		e := newEnv(t)
		mustCreateDockerRepo(t, e, "docker-local")
		d := digestOf("here")
		putManifest(t, e, admin(), "docker-local", "app", d, "v1")
		ghost := digestOf("ghost")
		foreign := digestOf("foreign-sha")
		// Pre-plant a node at the ghost digest's layout path whose sha256 is
		// NOT the digest: a row this use case does not own (the heal path
		// only drops nodes whose sha matches — anything else at the path is
		// somebody else's). It must SURVIVE the refused delete.
		if err := e.md.Blobs().Put(ctx, &metadata.Blob{Sha256: foreign, Size: 1, CreatedAt: "2026-08-18T00:00:00Z"}); err != nil {
			t.Fatalf("plant foreign blob row: %v", err)
		}
		if err := e.md.Nodes().Put(ctx, &metadata.Node{
			RepoKey: "docker-local", Path: "app/manifests/" + ghost, Sha256: foreign, Size: 1,
			Mime:      "application/vnd.docker.distribution.manifest.v2+json",
			CreatedBy: "planter", CreatedAt: "2026-08-18T00:00:00Z", UpdatedAt: "2026-08-18T00:00:00Z",
		}); err != nil {
			t.Fatalf("plant foreign node: %v", err)
		}
		if err := e.svc.DeleteManifest(ctx, admin(), "docker-local", "app", ghost); !errors.Is(err, repo.ErrManifestNotFound) {
			t.Fatalf("missing manifest error = %v, want ErrManifestNotFound", err)
		}
		if _, err := e.md.Nodes().Get(ctx, "docker-local", "app/manifests/"+ghost); err != nil {
			t.Fatalf("foreign node dropped on a refused delete: %v", err)
		}
		if tag, err := e.md.Docker().GetTag(ctx, "docker-local", "app", "v1"); err != nil || tag.Digest != d {
			t.Fatalf("unrelated tag damaged: %+v, %v", tag, err)
		}
	})

	t.Run("residue node heals on the retry", func(t *testing.T) {
		// The crash window between the store's cascade commit and the node
		// delete leaves a node whose sha IS the digest. A retry answers
		// 404 but must sweep the residue.
		e := newEnv(t)
		mustCreateDockerRepo(t, e, "docker-local")
		d := digestOf("residue")
		putManifest(t, e, admin(), "docker-local", "app", d, "v1", digestOf("cfg"))
		// Simulate the crash: the cascade ran, the node delete did not.
		if err := e.md.Docker().DeleteManifest(ctx, "docker-local", "app", d); err != nil {
			t.Fatalf("simulate cascade: %v", err)
		}
		if err := e.svc.DeleteManifest(ctx, admin(), "docker-local", "app", d); !errors.Is(err, repo.ErrManifestNotFound) {
			t.Fatalf("residue retry error = %v, want ErrManifestNotFound", err)
		}
		if _, err := e.md.Nodes().Get(ctx, "docker-local", "app/manifests/"+d); !errors.Is(err, metadata.ErrNodeNotFound) {
			t.Fatalf("residue node survived the healing retry: %v", err)
		}
	})
}

// ---- AC ③: DeleteRepo cascades the three docker tables ----

func TestDockerDeleteRepoCascades(t *testing.T) {
	ctx := context.Background()

	t.Run("deleteContent=true empties the three docker tables", func(t *testing.T) {
		e := newEnv(t)
		mustCreateDockerRepo(t, e, "docker-local")
		putManifest(t, e, admin(), "docker-local", "app", digestOf("a1"), "v1", digestOf("l1"))
		putManifest(t, e, admin(), "docker-local", "acme/team/app", digestOf("a2"), "v2", digestOf("l2"))

		if err := e.svc.DeleteRepo(ctx, admin(), "docker-local", true); err != nil {
			t.Fatalf("DeleteRepo(deleteContent): %v", err)
		}
		assertDockerTablesEmpty(ctx, t, e, "docker-local")
		// The catalog no longer serves the repo (its row is gone).
		if _, err := e.svc.ListImages(ctx, admin(), "docker-local", 0, ""); !errors.Is(err, repo.ErrRepoNotFound) {
			t.Fatalf("catalog of deleted repo error = %v, want ErrRepoNotFound", err)
		}
	})

	t.Run("docker rows alone block a no-flag delete", func(t *testing.T) {
		e := newEnv(t)
		mustCreateDockerRepo(t, e, "docker-local")
		putManifest(t, e, admin(), "docker-local", "app", digestOf("a"), "", digestOf("l")) // digest-only
		if err := e.md.Nodes().Delete(ctx, "docker-local", "app/manifests/"+digestOf("a")); err != nil {
			t.Fatalf("strip node row: %v", err)
		}
		if err := e.svc.DeleteRepo(ctx, admin(), "docker-local", false); !errors.Is(err, repo.ErrRepoNotEmpty) {
			t.Fatalf("docker-rows-only repo no-flag delete error = %v, want ErrRepoNotEmpty", err)
		}
		if err := e.svc.DeleteRepo(ctx, admin(), "docker-local", true); err != nil {
			t.Fatalf("flagged delete: %v", err)
		}
		assertDockerTablesEmpty(ctx, t, e, "docker-local")
	})

	t.Run("empty docker repo deletes without the flag", func(t *testing.T) {
		e := newEnv(t)
		mustCreateDockerRepo(t, e, "docker-local")
		if err := e.svc.DeleteRepo(ctx, admin(), "docker-local", false); err != nil {
			t.Fatalf("DeleteRepo(empty docker): %v", err)
		}
	})

	t.Run("generic repos keep their plain delete path", func(t *testing.T) {
		e := newEnv(t)
		mustCreateRepo(t, e, "generic-local")
		put(t, e, admin(), "generic-local", "a.bin", "x")
		if err := e.svc.DeleteRepo(ctx, admin(), "generic-local", true); err != nil {
			t.Fatalf("generic DeleteRepo: %v", err)
		}
	})

	t.Run("delete failure leaves the repo row behind", func(t *testing.T) {
		e := newEnv(t)
		mustCreateDockerRepo(t, e, "docker-local")
		layer := digestOf("l")
		putManifest(t, e, admin(), "docker-local", "app", digestOf("a"), "v1", layer)

		// Fail the repositories delete. The manifests/tags teardown ran
		// (first half, before the row) and swept that half's refs with it
		// (DeleteImage clears an image's three row kinds together) — but the
		// final repo-wide refs sweep must NOT have run: it only fires after
		// the repositories row is gone (review B1 — sweeping early would
		// strand the refs of any publish racing the delete). Observable
		// here: a ref written into the still-living repo after the failed
		// delete survives alongside the repo row, i.e. the state is
		// consistent and retryable rather than half-deleted.
		e.svc = newServiceWithClock(e.st, failRepoDeleteStore{e.md}, nil, nil, e.clk.Now)
		if err := e.svc.DeleteRepo(ctx, admin(), "docker-local", true); err == nil {
			t.Fatalf("DeleteRepo unexpectedly survived the injected failure")
		}
		if _, err := e.svc.GetRepo(ctx, admin(), "docker-local"); err != nil {
			t.Fatalf("repo row lost on a failed delete: %v", err)
		}
		// The repo row surviving means the service-level contract held: a
		// publish against the still-existing repository still works and its
		// refs are visible (nothing was swept out from under the repo).
		d := digestOf("post-failure")
		if _, err := e.svc.PutManifest(ctx, admin(), "docker-local", "app", d, "v2",
			"application/vnd.docker.distribution.manifest.v2+json", 1,
			mkRefs("docker-local", "app", d, layer)); err != nil {
			t.Fatalf("publish after failed delete: %v", err)
		}
		if ok, err := e.md.Docker().RefsByBlob(ctx, "docker-local", layer); err != nil || !ok {
			t.Fatalf("refs invisible in the surviving repo (half-deleted state): %v, %v", ok, err)
		}
		// And the retry completes: row gone, refs swept after it.
		e.svc = newServiceWithClock(e.st, e.md, nil, nil, e.clk.Now)
		if err := e.svc.DeleteRepo(ctx, admin(), "docker-local", true); err != nil {
			t.Fatalf("retry DeleteRepo: %v", err)
		}
		if ok, err := e.md.Docker().RefsByBlob(ctx, "docker-local", layer); err != nil || ok {
			t.Fatalf("refs outlived the completed delete: %v, %v", ok, err)
		}
	})

	t.Run("refs of a racing publish do not outlive the repo (B1)", func(t *testing.T) {
		// The B1 window: a PutRefs landing between the manifests/tags
		// teardown and the repositories delete. The refs sweep runs AFTER
		// the row is gone, so the straggler is collected — not stranded as
		// a permanent GC pin / ghost reference for a recreated repo.
		e := newEnv(t)
		mustCreateDockerRepo(t, e, "docker-local")
		d := digestOf("race")
		putManifest(t, e, admin(), "docker-local", "app", d, "v1", digestOf("l"))

		// Drive the two halves manually with the racing write injected
		// exactly between them: DeleteRepoDocker (first half, manifests/
		// tags gone, repository row still present) → straggler PutRefs →
		// the DeleteRepo that completes the teardown.
		if _, err := e.svc.DeleteRepoDocker(ctx, "docker-local"); err != nil {
			t.Fatalf("first half: %v", err)
		}
		straggler := digestOf("straggler")
		if err := e.md.Docker().PutRefs(ctx, "docker-local", "app", d,
			[]*metadata.DockerRef{{RepoKey: "docker-local", Image: "app", ManifestDigest: d, BlobDigest: straggler}}); err != nil {
			t.Fatalf("racing PutRefs: %v", err)
		}
		if err := e.svc.DeleteRepo(context.Background(), admin(), "docker-local", true); err != nil {
			t.Fatalf("DeleteRepo with straggler refs: %v", err)
		}
		if ok, err := e.md.Docker().RefsByBlob(ctx, "docker-local", straggler); err != nil || ok {
			t.Fatalf("racing publish's refs outlived the repository: %v, %v", ok, err)
		}
	})
}

// assertDockerTablesEmpty fails unless every docker index row of repoKey is
// gone (the service-level FR-7-AC5 assertion).
func assertDockerTablesEmpty(ctx context.Context, t *testing.T, e *env, repoKey string) {
	t.Helper()
	if images, err := e.md.Docker().ListImages(ctx, repoKey, "", 0); err != nil || len(images) != 0 {
		t.Fatalf("docker_manifests residue in %s: %v, %v", repoKey, images, err)
	}
	// docker_manifests and docker_tags cascade through their FKs once the
	// repositories row is gone; probe one image's tag set directly for
	// rows that survived a HALF-completed teardown (repo still present).
	if repos, err := e.md.Repos().List(ctx); err == nil {
		for _, r := range repos {
			if r.RepoKey == repoKey {
				// Repo row still there: the FK has not fired, so the explicit
				// DeleteRepoDocker pass is what must have cleared the tables.
				if tags, err := e.md.Docker().ListTagsByImage(ctx, repoKey, "app"); err != nil || len(tags) != 0 {
					t.Fatalf("docker_tags residue: %v, %v", tags, err)
				}
			}
		}
	}
	// docker_refs is the FK-less table — the explicit DeleteRepoRefs call is
	// its only cleanup path; probe through the blob side.
	if ok, err := e.md.Docker().RefsByBlob(ctx, repoKey, digestOf("l")); err != nil || ok {
		t.Fatalf("docker_refs residue: %v, %v", ok, err)
	}
}

// ---- error-injection branches (fake stack) ----

// TestDockerWritePathFailures: each index write failing leaves a consistent
// prefix state — manifest row without its tag is recoverable by re-push, and
// no half state ever shows up on the read path as a ghost.
func TestDockerWritePathFailures(t *testing.T) {
	for _, tt := range []struct {
		name   string
		failOp string
	}{
		{"manifest row fails", "docker.manifests.put"},
		{"tag row fails", "docker.tags.put"},
		{"refs put fails", "docker.refs.put"},
	} {
		t.Run(tt.name, func(t *testing.T) {
			dataDir := t.TempDir()
			dbDir := t.TempDir()
			ctx := context.Background()
			base, err := metadata.Open(ctx, metadata.Options{Driver: "sqlite", Path: dbDir + "/seed.db"})
			if err != nil {
				t.Fatalf("metadata.Open: %v", err)
			}
			t.Cleanup(func() { _ = base.Close() })
			if err := base.Repos().Create(ctx, &metadata.Repo{
				RepoKey: "docker-local", Type: repo.TypeLocal, PackageType: repo.PackageDocker,
				Config: "{}", CreatedAt: "2026-08-18T00:00:00Z", UpdatedAt: "2026-08-18T00:00:00Z",
			}); err != nil {
				t.Fatalf("seed repo: %v", err)
			}

			var failed bool
			hooked := wrapDockerHooks(base, func(op string) error {
				if op == tt.failOp && !failed {
					failed = true
					return fmt.Errorf("injected %s failure", op)
				}
				return nil
			})
			e := newEnvAt(t, dataDir, dbDir, hooked)

			d := digestOf("fail-" + tt.failOp)
			if _, err := e.svc.PutManifest(ctx, admin(), "docker-local", "app", d, "v1", "m", 1,
				mkRefs("docker-local", "app", d, digestOf("cfg"))); err == nil {
				t.Fatalf("PutManifest unexpectedly survived %s failing", tt.failOp)
			}
			// The read path never serves a ghost tag: either the tag row
			// landed (with its manifest — the row order) or it did not.
			if _, err := e.svc.ResolveTag(ctx, admin(), "docker-local", "app", "v1"); err == nil {
				if _, err := e.svc.ResolveManifest(ctx, admin(), "docker-local", "app", d); err != nil {
					t.Fatalf("tag resolves but its manifest does not: %v", err)
				}
			}
			// Re-push heals every partial state (all writes are upserts).
			hooked.fail = nil
			if _, err := e.svc.PutManifest(ctx, admin(), "docker-local", "app", d, "v1", "m", 1,
				mkRefs("docker-local", "app", d, digestOf("cfg"))); err != nil {
				t.Fatalf("healing re-push: %v", err)
			}
			if _, err := e.svc.ResolveTag(ctx, admin(), "docker-local", "app", "v1"); err != nil {
				t.Fatalf("tag after healing re-push: %v", err)
			}
			refs, err := base.Docker().ListRefsByManifest(ctx, "docker-local", "app", d)
			if err != nil || len(refs) != 1 {
				t.Fatalf("refs after healing re-push: %+v, %v", refs, err)
			}
		})
	}
}

// TestDockerDeleteManifestFailureIsAtomic: a failure inside the store's
// cascade transaction (node delete here) leaves the manifest+tag+refs set
// either fully present or fully gone — never a tag whose manifest vanished.
func TestDockerDeleteManifestFailureIsAtomic(t *testing.T) {
	dataDir := t.TempDir()
	dbDir := t.TempDir()
	ctx := context.Background()
	base, err := metadata.Open(ctx, metadata.Options{Driver: "sqlite", Path: dbDir + "/seed.db"})
	if err != nil {
		t.Fatalf("metadata.Open: %v", err)
	}
	t.Cleanup(func() { _ = base.Close() })
	if err := base.Repos().Create(ctx, &metadata.Repo{
		RepoKey: "docker-local", Type: repo.TypeLocal, PackageType: repo.PackageDocker,
		Config: "{}", CreatedAt: "2026-08-18T00:00:00Z", UpdatedAt: "2026-08-18T00:00:00Z",
	}); err != nil {
		t.Fatalf("seed repo: %v", err)
	}

	// Publish through the plain store first.
	seed := newEnvAt(t, dataDir, dbDir, base)
	d := digestOf("atomic")
	putManifest(t, seed, admin(), "docker-local", "app", d, "v1", digestOf("cfg"))

	// Now delete with the node delete failing: the store's cascade must have
	// committed (or rolled back) as one unit, so the visible state is either
	// everything or nothing — and a retry completes.
	var failed bool
	hooked := wrapDockerHooks(base, func(op string) error {
		if op == "nodes.delete" && !failed {
			failed = true
			return fmt.Errorf("injected node delete failure")
		}
		return nil
	})
	e := newEnvAt(t, dataDir, dbDir, hooked)
	if err := e.svc.DeleteManifest(ctx, admin(), "docker-local", "app", d); err == nil {
		t.Fatalf("DeleteManifest unexpectedly survived the injected failure")
	}
	// The cascade unit is all-or-nothing: a present manifest row means its
	// tag and refs are present too.
	if m, merr := base.Docker().GetManifest(ctx, "docker-local", "app", d); merr == nil {
		_ = m
		if _, terr := base.Docker().GetTag(ctx, "docker-local", "app", "v1"); terr != nil {
			t.Fatalf("manifest %s survived but its tag did not: %v", d, terr)
		}
	}
	// The retry (the failure is one-shot) heals the residue: the store's
	// cascade already committed, so the retry answers ErrManifestNotFound —
	// but its heal path must drop the leftover node (the crash window's
	// entire observable residue) rather than leave it dangling forever.
	if err := e.svc.DeleteManifest(ctx, admin(), "docker-local", "app", d); !errors.Is(err, repo.ErrManifestNotFound) {
		t.Fatalf("retry delete error = %v, want ErrManifestNotFound (cascade already committed)", err)
	}
	if _, err := base.Docker().GetManifest(ctx, "docker-local", "app", d); !errors.Is(err, metadata.ErrManifestNotFound) {
		t.Fatalf("manifest row after retry: %v", err)
	}
	if _, err := base.Docker().GetTag(ctx, "docker-local", "app", "v1"); !errors.Is(err, metadata.ErrTagNotFound) {
		t.Fatalf("tag row after retry: %v", err)
	}
	if _, err := base.Nodes().Get(ctx, "docker-local", "app/manifests/"+d); !errors.Is(err, metadata.ErrNodeNotFound) {
		t.Fatalf("residue node survived the healing retry: %v", err)
	}
}

// tagNames extracts the tag strings for failure messages.
func tagNames(tags []*metadata.DockerTag) []string {
	out := make([]string, len(tags))
	for i, t := range tags {
		out[i] = t.Tag
	}
	return out
}
