package repo

import (
	"encoding/json"
	"fmt"
	"net/http"
	"strings"

	"github.com/lzwzzy/binflow/internal/metadata"
)

// The blackedOut write gate (FR-156.1/T-490 — the M16 T-439 drift item's
// behavior half: the field finally round-trips through the local config
// plane, and a `true` finally refuses the write plane).
//
// Spec anchors (both medium confidence, implemented and pinned in tests per
// the confidence discipline):
//
//   - rest-api.md section 1.2 step 6: blackedOut=true refuses the upload —
//     BlackedOutException's message rides RepoRejectException's DEFAULT
//     status 404: "The repository '<key>' is blacked out and cannot serve
//     artifact '<repoPath>'."
//   - repo-semantics.md section 2 step 2: the refusal sits in
//     assertValidPath — BEFORE the permission pair (step 3) and the
//     pattern gate's siblings — so an unauthorized principal probing a
//     blacked-out repository learns the blackout, not the ACL. The gate
//     below therefore runs right after the write plane's repository
//     resolution and before parseGovernance/authorizeContentPut, exactly
//     where the call sites place the pattern refusal's siblings.
//
// Scope ruling (the AC's own boundary): the gate covers the DEPLOY plane —
// Put/PutFromBlob/PutLandedBlob (every adapter's upload path, the MPU
// checksum-deploy landing, the docker layer/config finalizes), PutManifest
// (the docker manifest push) and the exploded-archive staging write.
// Deletes and every read face are deliberately untouched; the spec's
// read-side note (repo-semantics section 6: a blacked-out repository's
// file listing answers 404) is registered as spec-pending in the T-490
// report, not implemented.

// blackedOut reports the blackedOut mark off one stored config blob. The
// local config is caller-owned JSON (the M1 passthrough contract); a
// hand-mangled blob reads as false — the product default — so config
// surgery can never brick a repository into refusing its own reads. The
// CONFIG plane types the field (validateLocalConfig): a mistyped value is
// refused at PUT time with the field named, never discovered here.
func blackedOut(config string) bool {
	if !strings.Contains(config, "blackedOut") {
		return false // fast path: the vast majority of blobs never carry the key
	}
	var probe struct {
		BlackedOut bool `json:"blackedOut"`
	}
	if err := json.Unmarshal([]byte(config), &probe); err != nil {
		return false
	}
	return probe.BlackedOut
}

// refuseBlackedOut is the write plane's blackout arm: nil when the
// repository is not blacked out, the spec's exact 404 otherwise. repoPath
// is the write's own addressing path (BinFlow's repoPath convention —
// "<repoKey>/<path>", the spelling every other client-facing message in
// this package uses for that placeholder family).
func refuseBlackedOut(row *metadata.Repo, repoPath string) error {
	if row == nil || !blackedOut(row.Config) {
		return nil
	}
	return NewStatusError(http.StatusNotFound, fmt.Sprintf(
		"The repository '%s' is blacked out and cannot serve artifact '%s/%s'.",
		row.RepoKey, row.RepoKey, repoPath), nil, ErrBlackedOut)
}
