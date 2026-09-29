package nuget

// T-575 / BIN-57 (L037 Arm 2, the conductor's two-face ruling): the
// X-Checksum-Sha256 header belongs to the BARE content plane alone.
//
//   - the bare-content PUT 201 ALWAYS renders X-Checksum-Sha256 = the
//     artifact's effective sha256: with a client header that passes
//     validation the value is the client's own; without one the server's
//     measurement fills in (both legs same value — a declared
//     disagreement dies at storage's 409 and never reaches the 201).
//   - the push faces' 201 (v3 addressed / v3 direct / v2 root / v2 path)
//     renders NO X-Checksum-Sha256 — the A face's v2/v3 push 201 carries
//     none (live, double round); the former node.Sha256 render was B-only.
//   - a well-formed WRONG declared sha256 on the bare PUT is the 409 with
//     NO checksum header on the refusal (the 201's render never fires);
//     the 409's BODY is the A face's CLIENT-policy rejection family
//     (T-579, TestBarePutChecksum409Wording below).

import (
	"crypto/md5"  //nolint:gosec // G501: fixture digest, not a security primitive
	"crypto/sha1" //nolint:gosec // G401: fixture digest, not a security primitive
	"crypto/sha256"
	"encoding/hex"
	"net/http"
	"strings"
	"testing"

	"github.com/lzwzzy/binflow/internal/repo"
)

// sha256Hex is the fixture body's digest (the effective sha256).
func sha256Hex(b []byte) string {
	sum := sha256.Sum256(b)
	return hex.EncodeToString(sum[:])
}

// TestBarePutCreatedChecksumHeader: the bare-content PUT 201 always
// carries X-Checksum-Sha256 — the client's validated value when declared,
// the server's measurement when not.
func TestBarePutCreatedChecksumHeader(t *testing.T) {
	s := newStack(t)
	s.seedRepo(t, "ng-sum", repo.TypeLocal)

	body := []byte("t575 bare bytes")
	sum := sha256Hex(body)

	tests := []struct {
		name       string
		declared   string // the X-Checksum-Sha256 request header ("" = none)
		wantSum    string // the 201's expected header value
		wantStatus int
		wantHeader bool
	}{
		{
			name:       "client header, correct value echoes it",
			declared:   sum,
			wantSum:    sum,
			wantStatus: http.StatusCreated,
			wantHeader: true,
		},
		{
			name:       "no client header, server computes",
			declared:   "",
			wantSum:    sum,
			wantStatus: http.StatusCreated,
			wantHeader: true,
		},
	}
	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			hdr := map[string]string{}
			if tc.declared != "" {
				hdr["X-Checksum-Sha256"] = tc.declared
			}
			// Distinct paths per leg: the overwrite arm must not color the
			// second leg (a same-path re-PUT walks the permission pair).
			path := "/binflow/ng-sum/t575-" + strings.ToLower(strings.ReplaceAll(tc.name, " ", "-")) + ".bin"
			status, respBody, rhdr := s.put(path, body, hdr)
			if status != tc.wantStatus {
				t.Fatalf("bare PUT = (%d, %s), want %d", status, respBody, tc.wantStatus)
			}
			got := rhdr.Get("X-Checksum-Sha256")
			if !tc.wantHeader {
				if got != "" {
					t.Errorf("X-Checksum-Sha256 = %q, want none", got)
				}
				return
			}
			if got != tc.wantSum {
				t.Errorf("X-Checksum-Sha256 = %q, want the effective sha256 %q", got, tc.wantSum)
			}
		})
	}
}

// TestBarePutWrongChecksumIs409WithoutHeader: a well-formed but WRONG
// declared sha256 dies at storage's 409, and the refusal carries no
// X-Checksum-Sha256 (the 201's render never fires).
func TestBarePutWrongChecksumIs409WithoutHeader(t *testing.T) {
	s := newStack(t)
	s.seedRepo(t, "ng-sum", repo.TypeLocal)

	wrong := strings.Repeat("0", 64) // well-formed hex, wrong value (the probe's leg)
	status, body, hdr := s.put("/binflow/ng-sum/t575-wrong.bin", []byte("never lands"),
		map[string]string{"X-Checksum-Sha256": wrong})
	if status != http.StatusConflict {
		t.Fatalf("bare PUT with wrong sha256 = (%d, %s), want 409", status, body)
	}
	if got := hdr.Get("X-Checksum-Sha256"); got != "" {
		t.Errorf("409 X-Checksum-Sha256 = %q, want none (the refusal carries no checksum header)", got)
	}
}

// sha1Hex and md5Hex are the fixture body's companion digests (the 409
// ChecksumsInfo dump and the envelope's triple).
func sha1Hex(b []byte) string {
	sum := sha1.Sum(b) //nolint:gosec // G401: wire-compat digest fixture, not a security primitive
	return hex.EncodeToString(sum[:])
}

func md5Hex(b []byte) string {
	sum := md5.Sum(b) //nolint:gosec // G501: wire-compat digest fixture, not a security primitive
	return hex.EncodeToString(sum[:])
}

// TestBarePutChecksum409Wording (T-579 / BIN-61): the declared-digest
// disagreement answers the A face's CLIENT-policy rejection — the errors[]
// JSON envelope under CT application/json;charset=ISO-8859-1, its message
// verbatim `Checksum policy 'LocalRepoChecksumPolicy: CLIENT' rejected the
// artifact '<repo>:<path>'. Checksums info: ChecksumsInfo{…}` with the
// dump's member order SHA-1, MD5, SHA-256 and original='null' for the
// undeclared. The former B wording leaked the storage session id — the
// negative legs pin that nothing internal (session ids, the wrap chain)
// can appear.
func TestBarePutChecksum409Wording(t *testing.T) {
	s := newStack(t)
	s.seedRepo(t, "ng-c409", repo.TypeLocal)

	wrong256 := strings.Repeat("0", 64)
	wrong1 := strings.Repeat("f", 40)
	tests := []struct {
		name       string
		rel        string
		hdr        map[string]string
		origSHA1   string
		origMD5    string
		origSHA256 string
	}{
		{
			name:       "wrong sha256 declared (the A raw's leg)",
			rel:        "t579/wrong256/1.0.0/t579.wrong256.1.0.0.nupkg",
			hdr:        map[string]string{"X-Checksum-Sha256": wrong256},
			origSHA1:   "null",
			origMD5:    "null",
			origSHA256: wrong256,
		},
		{
			name:       "wrong sha1 declared only",
			rel:        "t579/wrong1/1.0.0/t579.wrong1.1.0.0.nupkg",
			hdr:        map[string]string{"X-Checksum-Sha1": wrong1},
			origSHA1:   wrong1,
			origMD5:    "null",
			origSHA256: "null",
		},
	}
	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			body := []byte("t579 checksum rejection fixture")
			status, respBody, hdr := s.put("/binflow/ng-c409/"+tc.rel, body, tc.hdr)
			if status != http.StatusConflict {
				t.Fatalf("bare PUT = (%d, %s), want 409", status, respBody)
			}
			if ct := hdr.Get("Content-Type"); ct != contentTypeErrorJSON {
				t.Errorf("409 Content-Type = %q, want %q", ct, contentTypeErrorJSON)
			}
			if got := hdr.Get("X-Checksum-Sha256"); got != "" {
				t.Errorf("409 X-Checksum-Sha256 = %q, want none", got)
			}
			msg := "Checksum policy 'LocalRepoChecksumPolicy: CLIENT' rejected the artifact " +
				"'ng-c409:" + tc.rel + "'. Checksums info: ChecksumsInfo{checksums={" +
				"SHA-1=ChecksumInfo{type=SHA-1, original='" + tc.origSHA1 + "', actual='" + sha1Hex(body) + "'}, " +
				"MD5=ChecksumInfo{type=MD5, original='" + tc.origMD5 + "', actual='" + md5Hex(body) + "'}, " +
				"SHA-256=ChecksumInfo{type=SHA-256, original='" + tc.origSHA256 + "', actual='" + sha256Hex(body) + "'}}}"
			want := "{\n  \"errors\" : [ {\n    \"status\" : 409,\n    \"message\" : \"" +
				strings.ReplaceAll(strings.ReplaceAll(msg, `\`, `\\`), `"`, `\"`) +
				"\"\n  } ]\n}"
			if respBody != want {
				t.Errorf("409 body mismatch\n got: %q\nwant: %q", respBody, want)
			}
			// Information-exposure negatives: no storage session id, no
			// internal wrap-chain fragment may surface.
			for _, leak := range []string{"session", "commit upload", "storage:", "Checksum error for"} {
				if strings.Contains(respBody, leak) {
					t.Errorf("409 body leaks internal detail %q: %q", leak, respBody)
				}
			}
		})
	}
}

// TestPushCreatedHasNoChecksumHeader: every push entrance's 201 carries
// NO X-Checksum-Sha256 (L037 Arm 2: the A face's v2/v3 push 201 carries
// none — the header is the bare-content plane's own).
func TestPushCreatedHasNoChecksumHeader(t *testing.T) {
	s := newStack(t)
	s.seedRepo(t, "ng-sum", repo.TypeLocal)

	tests := []struct {
		name string
		id   string // the nuspec identity
		path string
	}{
		{
			name: "v3 ADDRESSED form",
			id:   "Sum.Addr",
			path: pushPath("ng-sum", "sum.addr", "1.0.0"),
		},
		{
			name: "v3 DIRECT form (publish base)",
			id:   "Sum.Direct",
			path: apiPath("ng-sum") + "/" + segFlat,
		},
		{
			name: "v2 ROOT form",
			id:   "Sum.Root",
			path: apiV2Path("ng-sum") + "/",
		},
		{
			name: "v2 PATH form",
			id:   "Sum.Path",
			path: apiV2Path("ng-sum") + "/sum.path/1.0.0",
		},
	}
	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			pkg := buildNupkg(t, tc.id, "1.0.0", flatDeps("none"))
			status, body, hdr := s.do(http.MethodPut, tc.path, adminUser, adminPass, bytesReader(pkg.body), nil)
			if status != http.StatusCreated {
				t.Fatalf("push = (%d, %s), want 201", status, body)
			}
			if got := hdr.Get("X-Checksum-Sha256"); got != "" {
				t.Errorf("201 X-Checksum-Sha256 = %q, want none (the push faces carry no checksum header)", got)
			}
		})
	}
}
