package docker

// L000-F: the upstream WIRE PATH spellings of the /v2 remote plane. The
// registry-v2 spec mounts every endpoint under /v2/, and the Artifactory
// reference (E4: X-Artifactory-Origin-Remote-Path on a repo whose url is
// the registry root) fetches <root>/v2/<name>/manifests/<ref> — the wire
// path itself must carry the /v2/ prefix (the L000 diff's DIVERGENT#1:
// upstream registries answered 404 to /<name>/manifests/<ref>).

import "testing"

// TestV2WirePathPrefix pins the /v2/ prefix across the image-namespace
// shapes (single-segment, nested) and the reference shapes (tag, digest):
// the storage layout's bare hex becomes the wire's sha256: form; tags ride
// verbatim.
func TestV2WirePathPrefix(t *testing.T) {
	hex := sha256HexOf([]byte("wire-path-probe"))
	tests := []struct {
		name string
		got  string
		want string
	}{
		{"manifest by tag, flat image", v2WireManifestPath("hello-world", "latest"), "/v2/hello-world/manifests/latest"},
		{"manifest by tag, nested image", v2WireManifestPath("library/hello-world", "t1"), "/v2/library/hello-world/manifests/t1"},
		{"manifest by digest", v2WireManifestPath("team/mychart", "sha256:"+hex), "/v2/team/mychart/manifests/sha256:" + hex},
		{"blob", v2WireBlobPath("mychart", hex), "/v2/mychart/blobs/sha256:" + hex},
		{"blob, nested image", v2WireBlobPath("library/hello-world", hex), "/v2/library/hello-world/blobs/sha256:" + hex},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			if tt.got != tt.want {
				t.Errorf("wire path = %q, want %q", tt.got, tt.want)
			}
		})
	}
}
