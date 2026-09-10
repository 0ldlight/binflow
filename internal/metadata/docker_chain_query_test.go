package metadata_test

// L002-1 (C10, ADR-0047): BlobInImageChain is the DigestChainGate oracle's
// ledger read — the image scoping is the semantic load this file pins. A
// digest named by ANOTHER image of the same repository is NOT in this
// image's chain (Artifactory's AQL answers `path matches <image>*` —
// prefix scope; the exact match is deliberately stricter, ADR-0047
// edge ②), and a digest named by the same image under another manifest IS
// (the chain is per image, not per manifest).

import (
	"context"
	"testing"

	"github.com/lzwzzy/binflow/internal/metadata"
)

func TestDockerBlobInImageChainScoping(t *testing.T) {
	st := open(t)
	ctx := context.Background()
	putDockerRepo(t, st, "r")

	appDigest, webDigest := fakeDigestHex(600), fakeDigestHex(601)
	sharedDigest := fakeDigestHex(602) // named by app's manifest AND web's
	for _, tc := range []struct {
		image, manifest, blob string
	}{
		{"app", fakeDigestHex(610), appDigest},
		{"app", fakeDigestHex(611), sharedDigest},
		{"web", fakeDigestHex(612), webDigest},
		{"web", fakeDigestHex(613), sharedDigest},
	} {
		if err := st.Docker().PutRefs(ctx, "r", tc.image, tc.manifest, []*metadata.DockerRef{
			{RepoKey: "r", Image: tc.image, ManifestDigest: tc.manifest, BlobDigest: tc.blob},
		}); err != nil {
			t.Fatalf("PutRefs %s/%s: %v", tc.image, tc.manifest, err)
		}
	}

	for _, tc := range []struct {
		name, image, blob string
		want              bool
	}{
		{"own image's chain", "app", appDigest, true},
		{"shared digest via this image's chain", "app", sharedDigest, true},
		{"other image's digest is out of chain (exact scope)", "app", webDigest, false},
		{"unknown digest", "web", fakeDigestHex(699), false},
		{"unknown image", "ghost", appDigest, false},
	} {
		got, err := st.Docker().BlobInImageChain(ctx, "r", tc.image, tc.blob)
		if err != nil {
			t.Fatalf("%s: %v", tc.name, err)
		}
		if got != tc.want {
			t.Errorf("%s = %v, want %v", tc.name, got, tc.want)
		}
	}

	// Repo scoping: the same image name under another repository never
	// answers (the ledger key is (repo_key, image)).
	got, err := st.Docker().BlobInImageChain(ctx, "other", "app", appDigest)
	if err != nil || got {
		t.Errorf("other repo's chain = (%v, %v), want (false, nil)", got, err)
	}
}
