package metadata_test

import (
	"context"
	"errors"
	"fmt"
	"strings"
	"sync"
	"testing"

	"github.com/lzwzzy/binflow/internal/metadata"
)

// fakeDigestHex builds a 64-char hex-ish digest deterministically (values are
// unique per call within a test; only shape and uniqueness matter here).
func fakeDigestHex(seed int) string {
	return fmt.Sprintf("%064x", seed)
}

func putDockerRepo(t *testing.T, st metadata.Store, key string) {
	t.Helper()
	now := metadata.Now()
	err := st.Repos().Create(context.Background(), &metadata.Repo{
		RepoKey: key, Type: "local", PackageType: "docker",
		Description: "test", Config: "{}", CreatedAt: now, UpdatedAt: now,
	})
	if err != nil {
		t.Fatalf("create docker repo %s: %v", key, err)
	}
}

func manifest(repo, image, digest string, size int64) *metadata.DockerManifest {
	return &metadata.DockerManifest{
		RepoKey: repo, Image: image, Digest: digest,
		MediaType: "application/vnd.docker.distribution.manifest.v2+json",
		Size:      size, CreatedBy: "ci", CreatedAt: "2026-08-18T00:00:00Z",
	}
}

func tag(repo, image, tagName, digest string) *metadata.DockerTag {
	return &metadata.DockerTag{
		RepoKey: repo, Image: image, Tag: tagName, Digest: digest,
		UpdatedBy: "ci", UpdatedAt: "2026-08-18T00:00:00Z",
	}
}

// AC ②: manifests/tags/refs CRUD round-trip, upsert repoint, missing-row
// sentinels — table-driven over the three row kinds.
func TestDockerRowRoundTrip(t *testing.T) {
	st := open(t)
	ctx := context.Background()
	putDockerRepo(t, st, "docker-local")

	const image = "team1/app"
	dA, dB := fakeDigestHex(1), fakeDigestHex(2)
	now := metadata.Now()

	if err := st.Docker().PutManifest(ctx, manifest("docker-local", image, dA, 100)); err != nil {
		t.Fatalf("PutManifest: %v", err)
	}
	got, err := st.Docker().GetManifest(ctx, "docker-local", image, dA)
	if err != nil {
		t.Fatalf("GetManifest: %v", err)
	}
	if got.MediaType != "application/vnd.docker.distribution.manifest.v2+json" || got.Size != 100 {
		t.Fatalf("manifest roundtrip mismatch: %+v", got)
	}

	// Manifest upsert refreshes metadata columns, no conflict.
	m2 := manifest("docker-local", image, dA, 222)
	m2.CreatedBy = "someone-else"
	m2.CreatedAt = now
	if err := st.Docker().PutManifest(ctx, m2); err != nil {
		t.Fatalf("PutManifest upsert: %v", err)
	}
	got, err = st.Docker().GetManifest(ctx, "docker-local", image, dA)
	if err != nil {
		t.Fatalf("GetManifest after upsert: %v", err)
	}
	if got.Size != 222 || got.CreatedBy != "someone-else" {
		t.Fatalf("manifest upsert did not refresh: %+v", got)
	}

	// Tags: upsert repoints (same tag, new digest).
	for _, tt := range []struct {
		name   string
		digest string
	}{
		{"initial", dA},
		{"repoint", dB},
	} {
		t.Run("tag "+tt.name, func(t *testing.T) {
			if err := st.Docker().PutTag(ctx, tag("docker-local", image, "latest", tt.digest)); err != nil {
				t.Fatalf("PutTag: %v", err)
			}
			got, err := st.Docker().GetTag(ctx, "docker-local", image, "latest")
			if err != nil {
				t.Fatalf("GetTag: %v", err)
			}
			if got.Digest != tt.digest {
				t.Fatalf("tag digest = %q, want %q (repoint)", got.Digest, tt.digest)
			}
		})
	}

	// Refs: PutRefs replaces the set of one manifest atomically.
	refs := []*metadata.DockerRef{
		{RepoKey: "docker-local", Image: image, ManifestDigest: dA, BlobDigest: fakeDigestHex(10), ChildMediaType: "application/vnd.docker.container.image.v1+json"},
		{RepoKey: "docker-local", Image: image, ManifestDigest: dA, BlobDigest: fakeDigestHex(11), ChildMediaType: "application/vnd.docker.image.rootfs.diff.tar.gzip"},
	}
	if err := st.Docker().PutRefs(ctx, "docker-local", image, dA, refs); err != nil {
		t.Fatalf("PutRefs: %v", err)
	}
	gotRefs, err := st.Docker().ListRefsByManifest(ctx, "docker-local", image, dA)
	if err != nil {
		t.Fatalf("ListRefsByManifest: %v", err)
	}
	if len(gotRefs) != 2 || gotRefs[0].BlobDigest != fakeDigestHex(10) || gotRefs[1].BlobDigest != fakeDigestHex(11) {
		t.Fatalf("refs roundtrip mismatch: %+v", gotRefs)
	}
	// Replace with a smaller set: old edges vanish.
	if err := st.Docker().PutRefs(ctx, "docker-local", image, dA, refs[:1]); err != nil {
		t.Fatalf("PutRefs replace: %v", err)
	}
	gotRefs, err = st.Docker().ListRefsByManifest(ctx, "docker-local", image, dA)
	if err != nil {
		t.Fatalf("ListRefsByManifest after replace: %v", err)
	}
	if len(gotRefs) != 1 {
		t.Fatalf("refs replace left %d rows, want 1", len(gotRefs))
	}
	// Empty slice clears the set.
	if err := st.Docker().PutRefs(ctx, "docker-local", image, dA, nil); err != nil {
		t.Fatalf("PutRefs clear: %v", err)
	}
	gotRefs, err = st.Docker().ListRefsByManifest(ctx, "docker-local", image, dA)
	if err != nil {
		t.Fatalf("ListRefsByManifest after clear: %v", err)
	}
	if len(gotRefs) != 0 {
		t.Fatalf("refs clear left %d rows, want 0", len(gotRefs))
	}

	// Missing-row sentinels.
	if _, err := st.Docker().GetManifest(ctx, "docker-local", image, fakeDigestHex(99)); !errors.Is(err, metadata.ErrManifestNotFound) {
		t.Fatalf("GetManifest missing err = %v, want ErrManifestNotFound", err)
	}
	if _, err := st.Docker().GetTag(ctx, "docker-local", image, "nope"); !errors.Is(err, metadata.ErrTagNotFound) {
		t.Fatalf("GetTag missing err = %v, want ErrTagNotFound", err)
	}
	if err := st.Docker().DeleteTag(ctx, "docker-local", image, "nope"); !errors.Is(err, metadata.ErrTagNotFound) {
		t.Fatalf("DeleteTag missing err = %v, want ErrTagNotFound", err)
	}
}

// AC ②: image-scoped listing is lexicographic and repo-scoped.
func TestDockerListByImageLexicographic(t *testing.T) {
	st := open(t)
	ctx := context.Background()
	putDockerRepo(t, st, "r1")
	putDockerRepo(t, st, "r2")

	// tags across images within one repo
	tagNames := []string{"v2", "v10", "v1", "latest", "Alpha"}
	wantOrder := []string{"Alpha", "latest", "v1", "v10", "v2"} // byte order, uppercase first
	for i, tn := range tagNames {
		if err := st.Docker().PutTag(ctx, tag("r1", "app", tn, fakeDigestHex(i))); err != nil {
			t.Fatalf("PutTag %s: %v", tn, err)
		}
	}
	tags, err := st.Docker().ListTagsByImage(ctx, "r1", "app")
	if err != nil {
		t.Fatalf("ListTagsByImage: %v", err)
	}
	var gotTags []string
	for _, tg := range tags {
		gotTags = append(gotTags, tg.Tag)
	}
	if strings.Join(gotTags, ",") != strings.Join(wantOrder, ",") {
		t.Fatalf("tags order = %v, want %v (lexicographic)", gotTags, wantOrder)
	}

	// manifests by digest order
	digests := []string{fakeDigestHex(30), fakeDigestHex(10), fakeDigestHex(20)}
	for _, d := range digests {
		if err := st.Docker().PutManifest(ctx, manifest("r1", "app", d, 1)); err != nil {
			t.Fatalf("PutManifest %s: %v", d, err)
		}
	}
	manifests, err := st.Docker().ListManifestsByImage(ctx, "r1", "app")
	if err != nil {
		t.Fatalf("ListManifestsByImage: %v", err)
	}
	var gotDigests []string
	for _, m := range manifests {
		gotDigests = append(gotDigests, m.Digest)
	}
	if strings.Join(gotDigests, ",") != strings.Join([]string{fakeDigestHex(10), fakeDigestHex(20), fakeDigestHex(30)}, ",") {
		t.Fatalf("manifests order = %v, want digest-ordered", gotDigests)
	}

	// repo scoping: same image name in r2 stays invisible
	if err := st.Docker().PutTag(ctx, tag("r2", "app", "only-r2", fakeDigestHex(40))); err != nil {
		t.Fatalf("PutTag r2: %v", err)
	}
	tagsAgain, err := st.Docker().ListTagsByImage(ctx, "r1", "app")
	if err != nil {
		t.Fatalf("ListTagsByImage r1 again: %v", err)
	}
	for _, tg := range tagsAgain {
		if tg.Tag == "only-r2" {
			t.Fatal("tag of r2 leaked into r1 listing")
		}
	}
}

// AC ② + ③: DeleteManifest cascades tags and refs atomically; repointed
// tags die with their new manifest, not the old one.
func TestDockerDeleteManifestCascades(t *testing.T) {
	st := open(t)
	ctx := context.Background()
	putDockerRepo(t, st, "r")
	const image = "app"
	dA, dB := fakeDigestHex(1), fakeDigestHex(2)

	for _, d := range []string{dA, dB} {
		if err := st.Docker().PutManifest(ctx, manifest("r", image, d, 10)); err != nil {
			t.Fatalf("PutManifest %s: %v", d, err)
		}
	}
	if err := st.Docker().PutRefs(ctx, "r", image, dA, []*metadata.DockerRef{
		{RepoKey: "r", Image: image, ManifestDigest: dA, BlobDigest: fakeDigestHex(50)},
		{RepoKey: "r", Image: image, ManifestDigest: dA, BlobDigest: fakeDigestHex(51)},
	}); err != nil {
		t.Fatalf("PutRefs dA: %v", err)
	}
	if err := st.Docker().PutRefs(ctx, "r", image, dB, []*metadata.DockerRef{
		{RepoKey: "r", Image: image, ManifestDigest: dB, BlobDigest: fakeDigestHex(52)},
	}); err != nil {
		t.Fatalf("PutRefs dB: %v", err)
	}
	// latest -> dA, v1 -> dA, stable -> dB
	for _, tt := range []struct{ tagName, digest string }{
		{"latest", dA}, {"v1", dA}, {"stable", dB},
	} {
		if err := st.Docker().PutTag(ctx, tag("r", image, tt.tagName, tt.digest)); err != nil {
			t.Fatalf("PutTag %s: %v", tt.tagName, err)
		}
	}

	if err := st.Docker().DeleteManifest(ctx, "r", image, dA); err != nil {
		t.Fatalf("DeleteManifest: %v", err)
	}

	// manifest gone
	if _, err := st.Docker().GetManifest(ctx, "r", image, dA); !errors.Is(err, metadata.ErrManifestNotFound) {
		t.Fatalf("manifest survived delete: %v", err)
	}
	// tags pointing at dA are gone; stable (dB) survives
	if _, err := st.Docker().GetTag(ctx, "r", image, "latest"); !errors.Is(err, metadata.ErrTagNotFound) {
		t.Fatalf("tag 'latest' survived manifest delete: %v", err)
	}
	if _, err := st.Docker().GetTag(ctx, "r", image, "v1"); !errors.Is(err, metadata.ErrTagNotFound) {
		t.Fatalf("tag 'v1' survived manifest delete: %v", err)
	}
	if _, err := st.Docker().GetTag(ctx, "r", image, "stable"); err != nil {
		t.Fatalf("tag 'stable' must survive sibling delete: %v", err)
	}
	// refs of dA gone, refs of dB survive
	refsA, err := st.Docker().ListRefsByManifest(ctx, "r", image, dA)
	if err != nil {
		t.Fatalf("ListRefsByManifest dA after delete: %v", err)
	}
	if len(refsA) != 0 {
		t.Fatalf("refs of deleted manifest survived: %+v", refsA)
	}
	refsB, err := st.Docker().ListRefsByManifest(ctx, "r", image, dB)
	if err != nil {
		t.Fatalf("ListRefsByManifest dB: %v", err)
	}
	if len(refsB) != 1 {
		t.Fatalf("refs of surviving manifest = %d, want 1", len(refsB))
	}
	// blob reference check follows the cascade
	used, err := st.Docker().RefsByBlob(ctx, "r", fakeDigestHex(50))
	if err != nil {
		t.Fatalf("RefsByBlob after cascade: %v", err)
	}
	if used {
		t.Fatal("blob 50 still reported referenced after manifest delete")
	}
	used, err = st.Docker().RefsByBlob(ctx, "r", fakeDigestHex(52))
	if err != nil {
		t.Fatalf("RefsByBlob surviving: %v", err)
	}
	if !used {
		t.Fatal("blob 52 must still be referenced by dB")
	}

	// deleting a missing manifest is an explicit sentinel
	if err := st.Docker().DeleteManifest(ctx, "r", image, dA); !errors.Is(err, metadata.ErrManifestNotFound) {
		t.Fatalf("double delete err = %v, want ErrManifestNotFound", err)
	}
}

// AC ②: repo+image-prefix queries and catalog facts.
func TestDockerListImagesCatalog(t *testing.T) {
	st := open(t)
	ctx := context.Background()
	putDockerRepo(t, st, "r")

	images := []string{"app", "team1/app", "team1/web", "team2/app", "zeta"}
	for i, img := range images {
		if err := st.Docker().PutManifest(ctx, manifest("r", img, fakeDigestHex(100+i), 1)); err != nil {
			t.Fatalf("PutManifest %s: %v", img, err)
		}
	}
	// a tag-only image must NOT appear in the catalog (source is docker_manifests)
	if err := st.Docker().PutTag(ctx, tag("r", "tag-only", "latest", fakeDigestHex(200))); err != nil {
		t.Fatalf("PutTag tag-only: %v", err)
	}

	all, err := st.Docker().ListImages(ctx, "r", "", 0)
	if err != nil {
		t.Fatalf("ListImages: %v", err)
	}
	want := []string{"app", "team1/app", "team1/web", "team2/app", "zeta"}
	if strings.Join(all, ",") != strings.Join(want, ",") {
		t.Fatalf("ListImages = %v, want %v (lexicographic, manifests only)", all, want)
	}

	// keyset paging: exclusive cursor, one at a time walks everything
	var walked []string
	after := ""
	for {
		page, err := st.Docker().ListImages(ctx, "r", after, 2)
		if err != nil {
			t.Fatalf("ListImages page after=%q: %v", after, err)
		}
		if len(page) == 0 {
			break
		}
		walked = append(walked, page...)
		after = page[len(page)-1]
	}
	if strings.Join(walked, ",") != strings.Join(want, ",") {
		t.Fatalf("keyset walk = %v, want %v", walked, want)
	}

	// the cursor is exclusive: the page after "team1/app" starts at team1/web
	page, err := st.Docker().ListImages(ctx, "r", "team1/app", 1)
	if err != nil {
		t.Fatalf("ListImages cursor: %v", err)
	}
	if len(page) != 1 || page[0] != "team1/web" {
		t.Fatalf("page after team1/app = %v, want [team1/web] (exclusive cursor)", page)
	}

	// unknown repo: empty, not an error
	none, err := st.Docker().ListImages(ctx, "ghost", "", 0)
	if err != nil {
		t.Fatalf("ListImages ghost: %v", err)
	}
	if len(none) != 0 {
		t.Fatalf("ListImages(ghost) = %v, want empty", none)
	}
}

// AC ②: DeleteImage drops the three row kinds together and is idempotent.
func TestDockerDeleteImageTeardown(t *testing.T) {
	st := open(t)
	ctx := context.Background()
	putDockerRepo(t, st, "r")

	for _, img := range []string{"app", "web"} {
		for i := 0; i < 2; i++ {
			d := fakeDigestHex(300 + i + len(img))
			if err := st.Docker().PutManifest(ctx, manifest("r", img, d, 1)); err != nil {
				t.Fatalf("PutManifest %s: %v", img, err)
			}
			if err := st.Docker().PutRefs(ctx, "r", img, d, []*metadata.DockerRef{
				{RepoKey: "r", Image: img, ManifestDigest: d, BlobDigest: fakeDigestHex(400 + i)},
			}); err != nil {
				t.Fatalf("PutRefs %s: %v", img, err)
			}
		}
		if err := st.Docker().PutTag(ctx, tag("r", img, "latest", fakeDigestHex(300+len(img)))); err != nil {
			t.Fatalf("PutTag %s: %v", img, err)
		}
	}

	n, err := st.Docker().DeleteImage(ctx, "r", "app")
	if err != nil {
		t.Fatalf("DeleteImage: %v", err)
	}
	if n != 2 {
		t.Fatalf("DeleteImage removed %d manifests, want 2", n)
	}
	if tags, err := st.Docker().ListTagsByImage(ctx, "r", "app"); err != nil || len(tags) != 0 {
		t.Fatalf("app tags after teardown = %v (err %v), want none", tags, err)
	}
	if manifests, err := st.Docker().ListManifestsByImage(ctx, "r", "app"); err != nil || len(manifests) != 0 {
		t.Fatalf("app manifests after teardown = %v (err %v), want none", manifests, err)
	}
	if refs, err := st.Docker().ListRefsByManifest(ctx, "r", "app", fakeDigestHex(300+1+len("app"))); err != nil || len(refs) != 0 {
		t.Fatalf("app refs after teardown = %v (err %v), want none", refs, err)
	}
	// web untouched
	if manifests, err := st.Docker().ListManifestsByImage(ctx, "r", "web"); err != nil || len(manifests) != 2 {
		t.Fatalf("web manifests = %v (err %v), want 2", manifests, err)
	}
	// idempotent, not an error
	if n, err := st.Docker().DeleteImage(ctx, "r", "app"); err != nil || n != 0 {
		t.Fatalf("DeleteImage again = (%d, %v), want (0, nil)", n, err)
	}
}

// AC ③: docker_refs has no DB-level FK (architecture 11.12). Writing refs
// for a manifest that does not exist must succeed at the store layer — the
// consistency contract is the service layer's — and DeleteRepoRefs is the
// teardown hook that keeps those rows from outliving the repository.
func TestDockerRefsNoFKContract(t *testing.T) {
	st := open(t)
	ctx := context.Background()
	putDockerRepo(t, st, "r")

	// No manifest row, no blobs rows: the insert still lands.
	if err := st.Docker().PutRefs(ctx, "r", "app", fakeDigestHex(500), []*metadata.DockerRef{
		{RepoKey: "r", Image: "app", ManifestDigest: fakeDigestHex(500), BlobDigest: fakeDigestHex(501)},
	}); err != nil {
		t.Fatalf("PutRefs without backing rows must succeed (no FK): %v", err)
	}

	// The repository teardown path: FKs cascade manifests/tags, refs need the
	// explicit call.
	if err := st.Docker().PutManifest(ctx, manifest("r", "app", fakeDigestHex(502), 1)); err != nil {
		t.Fatalf("PutManifest: %v", err)
	}
	if err := st.Docker().PutTag(ctx, tag("r", "app", "latest", fakeDigestHex(502))); err != nil {
		t.Fatalf("PutTag: %v", err)
	}
	n, err := st.Docker().DeleteRepoRefs(ctx, "r")
	if err != nil {
		t.Fatalf("DeleteRepoRefs: %v", err)
	}
	if n != 1 {
		t.Fatalf("DeleteRepoRefs removed %d rows, want 1", n)
	}
	if err := st.Repos().Delete(ctx, "r"); err != nil {
		t.Fatalf("repo delete: %v", err)
	}
	// manifests/tags cascaded via FK; the refs would have lingered without
	// DeleteRepoRefs — the doc.go contract.
	if _, err := st.Docker().GetManifest(ctx, "r", "app", fakeDigestHex(502)); !errors.Is(err, metadata.ErrManifestNotFound) {
		t.Fatalf("manifest survived repo delete: %v", err)
	}
}

// AC ①: docker_manifests/docker_tags DO carry the repositories FK — an
// unknown repo key must be rejected under foreign_keys=ON.
func TestDockerFKEnforcedForManifestsAndTags(t *testing.T) {
	st := open(t)
	ctx := context.Background()
	tests := []struct {
		name string
		run  func() error
	}{
		{"manifest", func() error {
			return st.Docker().PutManifest(ctx, manifest("ghost", "app", fakeDigestHex(1), 1))
		}},
		{"tag", func() error {
			return st.Docker().PutTag(ctx, tag("ghost", "app", "latest", fakeDigestHex(1)))
		}},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			if err := tt.run(); err == nil {
				t.Fatal("row for nonexistent repo must fail under foreign_keys=ON")
			}
		})
	}
}

// AC ②: -race coverage — concurrent upserts (tag repoint races), cascade
// deletes and catalog walks on one store must not trip the race detector or
// SQLITE_BUSY.
func TestDockerConcurrentUpsertAndRead(t *testing.T) {
	st := open(t)
	ctx := context.Background()
	putDockerRepo(t, st, "race")

	const images = 3
	const digestsPerImage = 4
	for i := 0; i < images; i++ {
		img := fmt.Sprintf("app%d", i)
		for d := 0; d < digestsPerImage; d++ {
			if err := st.Docker().PutManifest(ctx, manifest("race", img, fakeDigestHex(i*100+d), 1)); err != nil {
				t.Fatalf("PutManifest %s: %v", img, err)
			}
		}
	}

	var wg sync.WaitGroup
	errs := make(chan error, 64)
	// writers: repoint the shared tag and rewrite refs concurrently
	for w := 0; w < 4; w++ {
		wg.Add(1)
		go func(w int) {
			defer wg.Done()
			for i := 0; i < 10; i++ {
				img := fmt.Sprintf("app%d", i%images)
				d := fakeDigestHex((i%images)*100 + (i+w)%digestsPerImage)
				if err := st.Docker().PutTag(ctx, tag("race", img, "latest", d)); err != nil {
					errs <- fmt.Errorf("PutTag w%d: %w", w, err)
					return
				}
				if err := st.Docker().PutRefs(ctx, "race", img, d, []*metadata.DockerRef{
					{RepoKey: "race", Image: img, ManifestDigest: d, BlobDigest: fakeDigestHex(900 + w)},
				}); err != nil {
					errs <- fmt.Errorf("PutRefs w%d: %w", w, err)
					return
				}
			}
		}(w)
	}
	// readers: walk tags, manifests and the paged catalog
	for r := 0; r < 3; r++ {
		wg.Add(1)
		go func() {
			defer wg.Done()
			for i := 0; i < 10; i++ {
				img := fmt.Sprintf("app%d", i%images)
				if _, err := st.Docker().ListTagsByImage(ctx, "race", img); err != nil {
					errs <- fmt.Errorf("ListTagsByImage: %w", err)
					return
				}
				if _, err := st.Docker().ListManifestsByImage(ctx, "race", img); err != nil {
					errs <- fmt.Errorf("ListManifestsByImage: %w", err)
					return
				}
				if _, err := st.Docker().ListImages(ctx, "race", "", 2); err != nil {
					errs <- fmt.Errorf("ListImages: %w", err)
					return
				}
			}
		}()
	}
	// one deleter cascading manifests that still have live tags
	wg.Add(1)
	go func() {
		defer wg.Done()
		for d := 0; d < digestsPerImage; d++ {
			_ = st.Docker().DeleteManifest(ctx, "race", "app0", fakeDigestHex(d))
		}
	}()
	wg.Wait()
	close(errs)
	for err := range errs {
		t.Error(err)
	}

	// The store must still be consistent: every surviving tag resolves to
	// some digest shape (the value may have raced, the row must be well-formed).
	tags, err := st.Docker().ListTagsByImage(ctx, "race", "app1")
	if err != nil {
		t.Fatalf("final ListTagsByImage: %v", err)
	}
	for _, tg := range tags {
		if len(tg.Digest) != 64 {
			t.Fatalf("tag %s has malformed digest %q", tg.Tag, tg.Digest)
		}
	}
}
