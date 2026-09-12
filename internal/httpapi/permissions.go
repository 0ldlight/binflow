package httpapi

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"net/http"
	"net/url"
	"regexp"
	"strconv"
	"strings"
	"time"

	"github.com/lzwzzy/binflow/internal/audit"
	"github.com/lzwzzy/binflow/internal/auth"
	"github.com/lzwzzy/binflow/internal/metadata"
)

// BinFlow-owned permission target CRUD (PRD E-24, FR-5-AC8/AC10): the
// /binflow/api/v1/permissions plane over metadata.PermissionStore. Artifacts
// of the same capability exist at Artifactory's /api/v2/security/
// permissions; BinFlow deliberately does not promise that path (M4 will
// re-evaluate). Errors here are plain text — this is management plane.
//
// The replace operation is one store transaction (PutTarget replaces the
// target row and every principal row), so a failed update cannot leave half
// a grant behind.
//
// T-217 (FR-65, ADR-0026 decision 3 / architecture section 7.1 family 4
// exception): the write verbs' gate is evaluated HERE, not at the route —
// "CapSecurityWrite ∨ target.repos ⊆ the caller's manage coverage" is
// body-dependent (the repositories arrive in the POST body / from the stored
// target on DELETE), so the route carries only the authentication demand and
// this file owns the OR. The manage wire itself: principals action lists
// accept and echo "manage" (docs/reverse/auth-model.md section 4's action
// set subset; existing targets without the bit behave exactly as before).
//
// T-444 (M16, FR-146.1 / ADR-0044 K68 / architecture section 25.6): the
// action vocabulary renews to {read, deploy-cache, annotate, delete,
// manage} — the reference's five permission-matrix columns. "write" stays
// ACCEPTED on POST as a deploy-cache alias (legacy scripts keep working;
// removal evaluated M17) but the GET echo renders the canonical single
// form: the compat arm is receive-only. The DB column (can_write) and the
// internal code (w) are unchanged — deploy/cache stay one merged column;
// the split that happened is the property-write face (annotate, its own
// bit since migration 023), not a deploy/cache separation.
//
// T-254 (M9 E6, ADR-0030 / architecture section 14.1.6) completes the
// family's READ arm through the same move: GET /api/v1/permissions with a
// non-empty ?filter= rides a required-only route and handlePermissionList
// evaluates "CapSecurityRead full list ∨ non-empty manage coverage filtered
// subset ∨ the same 403", with the coverage question answered by
// auth.ManageCoverage — E9's single decision point, which the write arms'
// canManageAllRepos below also rides, so the read and write faces of the
// coverage can never disagree. The parameterless GET is frozen byte for
// byte (route, gate, error body and list rendering).
//
// T-491 (M17, FR-156.2 / M16 B-2.16): repos[] accepts the three preset
// wildcard buckets (auth.BucketAnyLocal / BucketAnyRemote /
// BucketAnyDistribution — the reference product's internal constants; the
// console renders them "Any Local" / "Any Remote" / "Any Distribution").
// The buckets name repository populations, so the existence check skips
// them while every other spelling still demands a repository row; the
// GET echo round-trips whatever was stored, verbatim as before. The
// evaluation semantics live in auth (wildcard.go) — this file only admits
// the spellings onto the wire.

// L009-3: the classic keyed face's two name validators, replicating the
// reference's own (wire-verified :8082, 2026-09-12). NameValidator applies
// to a name the BODY carries; XSSValidator applies to the path key that
// fills a nameless body. Both answers are errors envelopes.
const illegalNameChars = `/\:|?<>*"`

const illegalNameMessage = `Illegal name : '/,\,:,|,?,<,>,*,"' is not allowed`

// xssMarkupName is XSSValidator's own pattern, ported verbatim from the
// decompiled source (XSSValidator.java, Review A E1):
//
//	(.*)<(|/|[^/>][^>]+|/[^/>][^>]+)>(.*)
//
// evaluated with matches() — the anchors below carry the whole-string
// semantics. The bracketed content must be empty ("<>"→refuse), a bare
// slash ("</>"→refuse), or two-or-more characters that do not start with a
// slash and contain no ">" ("<em>", "<a href=x>", "</xy>" refuse; the
// one-character "<b>" and the one-letter closing "</x>" pass). The edge
// groups cannot cross a newline while the negated classes can (RE2 and
// Java agree on both defaults) — moot at the wire: a name carrying a
// literal newline is unroutable on the reference (routing-layer 404,
// probed 2026-09-12). Both directions corner-verified live: </x> 201,
// </> 400, plus the 25-arm black-box sweep of the first round.
var xssMarkupName = regexp.MustCompile(`^.*<(|/|[^/>][^>]+|/[^/>][^>]+)>.*$`)

// emptyLinkNames are NameValidator's three literal rejections (decompiled
// source, Review A E1; wire-verified :8082 2026-09-12): a name that is
// exactly ".", ".." or "&" answers "Name cannot be empty link: '<name>'".
// Exact match only — "a&b" and "ok.name" pass (probed: both fall through
// to the 409/201 arms).
var emptyLinkNames = map[string]bool{".": true, "..": true, "&": true}

// permissionBody is the wire shape of one permission target.
type permissionBody struct {
	Name            string                   `json:"name"`
	Repos           []string                 `json:"repos"`
	IncludePatterns []string                 `json:"includePatterns"`
	ExcludePatterns []string                 `json:"excludePatterns"`
	Principals      permissionPrincipalsBody `json:"principals"`
}

// permissionPrincipalsBody carries the two principal columns: per-user and
// per-group action lists (T-97 / SE-07: groups ride the same target, the
// authorizer unions both sides). Both maps render as {} when empty.
type permissionPrincipalsBody struct {
	Users  map[string][]string `json:"users"`
	Groups map[string][]string `json:"groups"`
}

// errPermissionEditDenied is the write-verb denial of the family-4 exception
// gate. One wording serves both arms: the caller holds no security:write
// capability AND the target's repositories do not sit inside its manage
// coverage (or it holds no manage bit at all).
const errPermissionEditDenied = "administrator privileges required (or, for manage holders: every repository the target names must sit inside your manage coverage)"

// canManageAllRepos reports whether every named repository sits inside the
// caller's manage coverage — the coverage half of family 4's exception OR.
// Since T-254 it rides auth.ManageCoverage (E9's single decision point,
// architecture section 14.1.9) instead of a per-repo CanManageRepo walk:
// for role=user the coverage set is exactly {r : Can(r, "", "m")} — same
// predicate, one evaluation instead of two store reads per repository —
// readonly_admin fails it (the role holds no m anywhere, and CanManageRepo's
// write arm denied it the same way), admin passes through the universe
// sentinel. The empty set DENIES: the arm must certify a non-empty subset
// (create validation demands at least one repository anyway), so a caller
// with no manage bit anywhere — or a body naming none — fails like
// everyone else.
func (s *Server) canManageAllRepos(ctx context.Context, p *auth.Principal, repos []string) bool {
	if p == nil || len(repos) == 0 {
		return false
	}
	coverage, universe := manageCoverageOf(ctx, s.deps.Authz, p)
	if universe {
		return true
	}
	for _, repoKey := range repos {
		if _, ok := coverage[repoKey]; !ok {
			return false
		}
	}
	return true
}

// manageCoverageAuthorizer is the E9 facet of the authorizer this plane
// consumes (M9, T-254, architecture section 14.1.9): the manage-coverage
// question behind E6's filter arm and the family-4 write arms. It is
// defined here, at the consumer, so the Authorizer interface and its
// existing fakes stay untouched — the same discovery pattern as
// ManagementAuthorizer.
type manageCoverageAuthorizer interface {
	ManageCoverage(ctx context.Context, p *auth.Principal) (map[string]struct{}, bool)
}

// manageCoverageOf resolves the authorizer's coverage facet and asks one
// question. A nil authorizer, or one without the facet, is the empty
// non-universe set — the fail-closed posture managementAllowed established
// for the capability facet: a missing decision point is a deny, never a
// pass. (Store failures deny the same way inside the seam, Can's posture.)
func manageCoverageOf(ctx context.Context, a auth.Authorizer, p *auth.Principal) (map[string]struct{}, bool) {
	m, ok := a.(manageCoverageAuthorizer)
	if !ok {
		return map[string]struct{}{}, false
	}
	return m.ManageCoverage(ctx, p)
}

// handlePermissionCreate serves POST /api/v1/permissions (create or wholly
// replace the named target, FR-5-AC8).
//
// The family-4 exception gate (T-217, ADR-0026 decision 3): security writers
// pass as before; everyone else must hold the manage bit on EVERY repository
// the body names. A body that cannot decode cannot name a coverage subset,
// so an unprivileged caller with garbage still meets the 403 the pre-T-217
// route gate answered before any parsing.
func (s *Server) handlePermissionCreate(w http.ResponseWriter, r *http.Request) {
	var body permissionBody
	s.permissionCreateOrReplace(w, r, "", decodeJSONBodyOf(r, &body), body, false)
}

// ---- L006-B Review A rework: the classic face's v1 dialect acceptance arm ----
//
// The reference's v1 PUT body is a DIFFERENT dialect than BinFlow's rich
// face (decompiled double evidence: PermissionTargetConfigurationImpl — the
// wire model — plus RestSecurityRequestHandler#createOrReplacePermissionTarget):
//
//   - the repositories key spells the member set ("repos" is BinFlow's own);
//   - includesPattern/excludesPattern are FLAT comma-separated strings;
//   - principal action lists carry the backend LETTERS r/w/n/d/m (the
//     ArtifactoryPermission enum's string column; mxm and x exist there too),
//     and a token that is no exact letter match contributes NO bit —
//     AceImpl#setPermissionsFromStrings silently clears it, no error.
//
// The keyed face accepts BOTH dialects (the alias posture every other dual
// spelling gets): either key spelling works alone, and two spellings of one
// knob that disagree refuse. Letters map onto the wire words; mxm/x and any
// unknown token drop silently (照抄 the reference's silent-clear arm — the
// reference has no error to mirror); the RICH face stays byte-frozen on its
// word vocabulary and array patterns.
type keyedPermissionBody struct {
	permissionBody
	Repositories    *[]string `json:"repositories"`    // v1 spelling of repos
	IncludesPattern *string   `json:"includesPattern"` // v1 flat string (default "**")
	ExcludesPattern *string   `json:"excludesPattern"` // v1 flat string (default "")
}

// v1ActionLetters is the backend letter → wire word map (ArtifactoryPermission
// string column minus the two with no BinFlow seat: mxm = managedXrayMeta,
// x = distribute — both silently dropped, a registered dialect residual).
var v1ActionLetters = map[string]string{
	"r": "read",
	"w": "write", // the receive-only deploy-cache alias word — same column
	"n": "annotate",
	"d": "delete",
	"m": "manage",
}

// v1AcceptedWords are the rich-face action words the keyed face also admits
// verbatim (the hybrid arm: BinFlow-native bodies on the classic path keep
// working; everything else — the reference's own behavior for non-letters —
// silently contributes nothing).
var v1AcceptedWords = map[string]bool{
	"read": true, "write": true, "deploy-cache": true,
	"annotate": true, "delete": true, "manage": true,
}

// translate folds the v1 dialect keys onto the rich body: alias resolution
// (disagreement refuses, the house posture for dual spellings), flat pattern
// strings split on "," (empty segments dropped; segments otherwise verbatim,
// so the v1 detail's join round-trips), and the letters→words action mapping
// with the reference's silent-clear for unknown tokens.
func (b keyedPermissionBody) translate() (permissionBody, error) {
	out := b.permissionBody
	if b.Repositories != nil {
		switch {
		case out.Repos == nil:
			out.Repos = *b.Repositories
		case !strSlicesEqualAny(*b.Repositories, out.Repos):
			return out, fmt.Errorf(
				"repositories and repos are two spellings of one knob and disagree (%v vs %v)",
				*b.Repositories, out.Repos)
		}
	}
	if b.IncludesPattern != nil {
		flat := splitPatternString(*b.IncludesPattern)
		switch {
		case out.IncludePatterns == nil:
			out.IncludePatterns = flat
		case !strSlicesEqualAny(flat, out.IncludePatterns):
			return out, fmt.Errorf(
				"includesPattern and includePatterns are two spellings of one knob and disagree (%q vs %v)",
				*b.IncludesPattern, out.IncludePatterns)
		}
	}
	if b.ExcludesPattern != nil {
		flat := splitPatternString(*b.ExcludesPattern)
		switch {
		case out.ExcludePatterns == nil:
			out.ExcludePatterns = flat
		case !strSlicesEqualAny(flat, out.ExcludePatterns):
			return out, fmt.Errorf(
				"excludesPattern and excludePatterns are two spellings of one knob and disagree (%q vs %v)",
				*b.ExcludesPattern, out.ExcludePatterns)
		}
	}
	out.Principals.Users = translateV1Actions(out.Principals.Users)
	out.Principals.Groups = translateV1Actions(out.Principals.Groups)
	return out, nil
}

// translateV1Actions maps one principal column's action lists onto the wire
// words. A nil map stays nil (absent); an empty list stays empty (the
// reference stores a zero-mask ACE, BinFlow a grant-less row).
func translateV1Actions(m map[string][]string) map[string][]string {
	if m == nil {
		return nil
	}
	out := make(map[string][]string, len(m))
	for principal, actions := range m {
		words := make([]string, 0, len(actions))
		for _, a := range actions {
			if w, ok := v1ActionLetters[a]; ok {
				words = append(words, w)
				continue
			}
			if v1AcceptedWords[a] {
				words = append(words, a)
			}
			// Anything else — the reference's own setPermissionsFromStrings
			// arm — silently contributes no bit. mxm/x land here too.
		}
		out[principal] = words
	}
	return out
}

// splitPatternString splits one v1 flat pattern string on "," dropping the
// empty segments (the trailing-comma tolerance); segments otherwise verbatim.
func splitPatternString(s string) []string {
	parts := strings.Split(s, ",")
	out := make([]string, 0, len(parts))
	for _, p := range parts {
		if p != "" {
			out = append(out, p)
		}
	}
	return out
}

// strSlicesEqualAny compares two string slices element-wise (the keyed
// merge's agreement check; a copy of repo's local strSlicesEqual posture,
// kept here so the httpapi plane does not reach into repo internals).
func strSlicesEqualAny(a, b []string) bool {
	if len(a) != len(b) {
		return false
	}
	for i := range a {
		if a[i] != b[i] {
			return false
		}
	}
	return true
}

// handlePermissionPostV1 serves POST /api/security/permissions/{name}: the
// reference's own answer for the permissions entity type — the addon layer's
// updateSecurityEntity handles only users and groups, so "permissions" falls
// through to a bare 400 (decompiled RestSecurityRequestHandler:336-352;
// wire-verified 2026-09-12: the errors envelope carrying "Bad Request").
// Mounting the same answer IS the wire alignment (Review B rework: the
// earlier "dead verb, do not mount" call was over-strong).
func (s *Server) handlePermissionPostV1(w http.ResponseWriter, _ *http.Request, name string) {
	_ = name // the reference's fall-through never reads the key
	writeError(w, http.StatusBadRequest, "Bad Request")
}

// handlePermissionPutV1 serves PUT /api/security/permissions/{name} (L006-B,
// D04-R18 — the classic v1 alias of the same create-or-replace operation).
// The PATH key governs: a body naming a different non-empty target refuses
// with the reference's 409 wording (decompile-verified: the isNotBlank guard
// skips the check for a nameless body, which then creates the entityKey
// target), and the v1 dialect keys ride alongside the rich spellings.
func (s *Server) handlePermissionPutV1(w http.ResponseWriter, r *http.Request, name string) {
	var body keyedPermissionBody
	decodeErr := decodeJSONBodyOf(r, &body)
	var plain permissionBody
	if decodeErr == nil {
		var terr error
		plain, terr = body.translate()
		if terr != nil {
			decodeErr = terr
		}
	}
	s.permissionCreateOrReplace(w, r, name, decodeErr, plain, true)
}

// permissionCreateOrReplace is the create-or-replace core both faces share
// (POST /api/v1/permissions and the classic PUT /api/security/permissions/
// {name}); pathName is empty on the body-keyed face and carries the
// URL-decoded path key on the classic one. keyedV1 marks the classic face:
// its repos-absence/empty 400s and unknown-repository wording follow the
// reference's handler verbatim (RestSecurityRequestHandler:527-570), while
// the rich face keeps its frozen single-wording posture.
func (s *Server) permissionCreateOrReplace(w http.ResponseWriter, r *http.Request, pathName string, decodeErr error, body permissionBody, keyedV1 bool) {
	// The classic face's path key fills a nameless body BEFORE the gate:
	// the coverage arm's replace-time union check (B1) looks the existing
	// row up by name, so the keyed face must carry its real identity from
	// the start — a holder cannot dodge the union check by omitting the
	// body name on the path-keyed spelling. nameFromPath remembers which
	// of the two validators (L009-3) the final name must clear.
	nameFromPath := false
	if pathName != "" && decodeErr == nil && strings.TrimSpace(body.Name) == "" {
		body.Name = pathName
		nameFromPath = true
	}
	p := principalFrom(r.Context())
	if s.canManage(r.Context(), p, auth.CapSecurityWrite) {
		if decodeErr != nil {
			// The keyed face carries its decode 400 in the errors envelope
			// too (the reference's own is a Jackson message in the same
			// envelope; BinFlow keeps its own wording, L009-3).
			if keyedV1 {
				writeError(w, http.StatusBadRequest, decodeErr.Error())
			} else {
				writePlainError(w, http.StatusBadRequest, decodeErr.Error())
			}
			return
		}
	} else {
		// Family-4 exception, non-writer arm: the body's repositories must
		// sit inside the caller's manage coverage — and, when a target of the
		// same name already exists, that row's too (review round B1): POST is
		// create-or-REPLACE, so a holder covering only the body could
		// otherwise revoke or rewrite grants on out-of-coverage repositories
		// by replacing the target. The union check is both subsets passing.
		// A body that cannot decode cannot name a coverage subset — the
		// pre-T-217 route gate's 403-before-parsing posture is kept, and a
		// body-side denial skips the store lookup entirely.
		covered := decodeErr == nil && s.canManageAllRepos(r.Context(), p, body.Repos)
		if covered && strings.TrimSpace(body.Name) != "" {
			if existing, _, gerr := s.deps.Metadata.Permissions().GetTarget(r.Context(), body.Name); gerr == nil {
				covered = s.canManageAllRepos(r.Context(), p, unmarshalStrings(existing.Repos))
			} else if !errors.Is(gerr, metadata.ErrNotFound) {
				writePlainError(w, http.StatusInternalServerError, "lookup permission target: "+gerr.Error())
				return
			}
		}
		if decodeErr != nil || !covered {
			writePlainError(w, http.StatusForbidden, errPermissionEditDenied)
			return
		}
	}
	if strings.TrimSpace(body.Name) == "" {
		writePlainError(w, http.StatusBadRequest, "permission target name is required")
		return
	}
	// L009-3, the classic face's two name validators (wire-verified :8082
	// 2026-09-12): a name the BODY carries must clear NameValidator — the
	// illegal-character set answers the verbatim "Illegal name" 400, the
	// three literal names (".", "..", "&") answer the "empty link" 400 —
	// and both fire BEFORE the 409 mismatch check (a malicious body name
	// against a benign path key answers the validator, not the 409). A
	// nameless body's path key skips NameValidator entirely (an entity-key
	// "n1/n2" and even "." are accepted, probed) and instead clears
	// XSSValidator's source pattern — see xssMarkupName. Both run before
	// the repositories validation. The rich face keeps its frozen posture.
	if keyedV1 {
		if nameFromPath {
			if xssMarkupName.MatchString(body.Name) {
				writeError(w, http.StatusBadRequest, "Name may contains a Cross-Site Scripting expression")
				return
			}
		} else if strings.ContainsAny(body.Name, illegalNameChars) {
			writeError(w, http.StatusBadRequest, illegalNameMessage)
			return
		} else if emptyLinkNames[body.Name] {
			writeError(w, http.StatusBadRequest, "Name cannot be empty link: '"+body.Name+"'")
			return
		}
	}
	// The classic face's one keyed-family rule (L006-B, live reference
	// 2026-09-12): the path key and a non-empty body name may not disagree
	// — the reference's 409 wording, verbatim. (After the gate: BinFlow's
	// security-first plane answers the family-4 403 before the 409 for a
	// non-writer.)
	if pathName != "" && body.Name != pathName {
		// L009-3: the reference carries this 409 in the errors envelope
		// (wire-verified :8082 2026-09-12), like every other 4xx of the
		// keyed face — the wording itself is unchanged.
		writeError(w, http.StatusConflict,
			"The permission target name that was provided in the request path does not match the permission name in the provided permission configuration object.")
		return
	}
	if len(body.Repos) == 0 {
		if keyedV1 {
			// The reference handler's dual wording (decompiled, lines
			// 545-550): a null repositories field and an empty one are two
			// different 400s — carried in the errors envelope (L009-3).
			if body.Repos == nil {
				writeError(w, http.StatusBadRequest, "Permission target request missing repositories.")
			} else {
				writeError(w, http.StatusBadRequest, "Permission target must contain at least one repository.")
			}
			return
		}
		writePlainError(w, http.StatusBadRequest, "Permission target request missing repositories: repos must contain at least one repository")
		return
	}
	for _, repo := range body.Repos {
		// T-491 (FR-156.2 / M16 B-2.16): the three preset wildcard buckets
		// are legal repos[] entries — they name a repository POPULATION
		// (every local / every remote repository, or the bundle-domain
		// pseudo-key channel of ADR-0046 point 3), so no repository row can
		// back them and the existence check must skip them. Everything else
		// still demands a real row; a literal outside the closed bucket set
		// (including the reference's fourth family member "ANY", which
		// BinFlow does not implement — wildcard.go's spec-pending note)
		// keeps the unknown-repository 400.
		if auth.IsWildcardBucket(repo) {
			continue
		}
		if _, err := s.deps.Repos.Get(r.Context(), repo); err != nil {
			if keyedV1 {
				// The reference's own wording (decompiled line 554; the
				// wildcard buckets above ride the same exemption it gives
				// ANY/ANY LOCAL/ANY REMOTE/ANY DISTRIBUTION) — in the
				// errors envelope (L009-3).
				writeError(w, http.StatusBadRequest, fmt.Sprintf(
					"Permission target contains a reference to a non-existing repository '%s'.", repo))
				return
			}
			writePlainError(w, http.StatusBadRequest, fmt.Sprintf("permission target references an unknown repository %q", repo))
			return
		}
	}
	users, err := s.deps.Metadata.Users().List(r.Context())
	if err != nil {
		writePlainError(w, http.StatusInternalServerError, "list users for validation: "+err.Error())
		return
	}
	known := make(map[string]*metadata.User, len(users))
	for _, u := range users {
		known[u.Username] = u
	}
	if keyedV1 {
		// L007-1 arm 3 (live :8082, wire-verified 2026-09-12): the classic
		// face refuses an admin-privileged user as a principal — the
		// reference's wording verbatim, including its own doubled quote
		// mark. The scan runs BEFORE the unknown-user check (probed: an
		// admin plus an unknown user in one body answers the admin
		// message) and after the repository validation (probed: an
		// unknown repository plus admin answers the repository message).
		//
		// L010-2 (ledger rest/permissions-v1-empty-actions-validation-skip,
		// live :8082 arms wire-verified 2026-09-12): the validation walks
		// only principals whose action list is NON-EMPTY — an empty list
		// grants nothing, and its holder skips this guard (an admin with
		// [] answers 201; mixed admin["m"]+ghost[] still answers the
		// admin message above, so the ordering claims stand untouched).
		for name := range body.Principals.Users {
			if len(body.Principals.Users[name]) == 0 {
				continue
			}
			if u := known[name]; u != nil && u.IsAdmin {
				writeError(w, http.StatusBadRequest, fmt.Sprintf(
					"The user: '%s'' has admin privileges, and cannot be added to a Permission Target.", name))
				return
			}
		}
	}
	for name := range body.Principals.Users {
		// The keyed face's empty-actions exemption (L010-2 — see the admin
		// scan above for the probe evidence): no actions, no validation.
		// The rich face (keyedV1=false) keeps its frozen 400 posture.
		if keyedV1 && len(body.Principals.Users[name]) == 0 {
			continue
		}
		if _, ok := known[name]; !ok {
			if keyedV1 {
				// The reference's wording, pinned by the L009-3/L010-2
				// live-wire differential (seven-arm status+message parity,
				// T-L010-2 ②); the rich face keeps the frozen plane's.
				// Errors envelope (L009-3).
				writeError(w, http.StatusBadRequest, fmt.Sprintf(
					"Permission target contains a reference to a non-existing user: '%s'.", name))
				return
			}
			writePlainError(w, http.StatusBadRequest, fmt.Sprintf("Unable to find user by name '%s'.", name))
			return
		}
	}
	groups, err := s.deps.Metadata.Groups().List(r.Context())
	if err != nil {
		writePlainError(w, http.StatusInternalServerError, "list groups for validation: "+err.Error())
		return
	}
	knownGroups := make(map[string]bool, len(groups))
	for _, g := range groups {
		knownGroups[g.Name] = true
	}
	for name := range body.Principals.Groups {
		// Same empty-actions exemption on the group column (the probed
		// unknown-group-with-[] arm answers 201, L010-2).
		if keyedV1 && len(body.Principals.Groups[name]) == 0 {
			continue
		}
		if !knownGroups[name] {
			if keyedV1 {
				// The reference's own wording (live :8082, 2026-09-12 —
				// no colon before the quoted name, unlike the user arm).
				writePlainError(w, http.StatusBadRequest, fmt.Sprintf(
					"Permission target contains a reference to a non-existing group '%s'.", name))
				return
			}
			// Same family wording as the users arm (auth-model.md section 4:
			// a principal referencing an unknown user/group is a 400).
			writePlainError(w, http.StatusBadRequest, fmt.Sprintf("Unable to find group by name '%s'.", name))
			return
		}
	}

	principals := make([]*metadata.PermissionPrincipal, 0, len(body.Principals.Users)+len(body.Principals.Groups))
	appendRows := func(entries map[string][]string, principalType string) bool {
		for name, actions := range entries {
			row := &metadata.PermissionPrincipal{
				TargetName: body.Name, Principal: name, PrincipalType: principalType,
			}
			for _, a := range actions {
				switch strings.ToLower(strings.TrimSpace(a)) {
				case "read":
					row.CanRead = true
				case "deploy-cache":
					// T-444 (FR-146.1 / ADR-0044 K68): the canonical write
					// word — deploy/cache stay one merged column (can_write),
					// the reference's Deploy/Cache parity.
					row.CanWrite = true
				case "write":
					// T-444: the legacy spelling, accepted as the
					// deploy-cache alias for one compat round (K68 point 2:
					// existing scripts and the pre-T-455 console keep
					// working; removal evaluated M17). It grants deploy-cache
					// ONLY — annotate is its own word now, and the alias does
					// not silently carry the property-write face.
					row.CanWrite = true
				case "annotate":
					// T-444: the property-write action (the ?properties
					// family's PUT/DELETE gate) — its own grant, no longer
					// the write action's shadow.
					row.CanAnnotate = true
				case "delete":
					row.CanDelete = true
				case "manage":
					// T-217 (FR-65): the repo-scoped admin bit joins the wire
					// action set — auth-model.md section 4's manage action.
					row.CanManage = true
				default:
					writePlainError(w, http.StatusBadRequest, fmt.Sprintf(
						"unknown permission action %q (supported: read, deploy-cache, annotate, delete, manage; write is accepted as a deploy-cache alias)", a))
					return false
				}
			}
			principals = append(principals, row)
		}
		return true
	}
	if !appendRows(body.Principals.Users, "user") || !appendRows(body.Principals.Groups, "group") {
		return
	}

	now := time.Now().UTC().Format(time.RFC3339)
	target := &metadata.PermissionTarget{
		Name:      body.Name,
		Repos:     marshalStrings(body.Repos),
		Includes:  marshalStrings(body.IncludePatterns),
		Excludes:  marshalStrings(body.ExcludePatterns),
		UpdatedAt: now,
	}
	existing, _, err := s.deps.Metadata.Permissions().GetTarget(r.Context(), body.Name)
	switch {
	case err == nil:
		target.CreatedAt = existing.CreatedAt
	case errors.Is(err, metadata.ErrNotFound):
		target.CreatedAt = now
	default:
		writePlainError(w, http.StatusInternalServerError, "lookup permission target: "+err.Error())
		return
	}
	if err := s.deps.Metadata.Permissions().PutTarget(r.Context(), target, principals); err != nil {
		writePlainError(w, http.StatusInternalServerError, "store permission target: "+err.Error())
		return
	}
	// Authorization-change audit (NFR-S25: every grant change leaves a
	// trail): the vocabulary distinguishes a fresh target from a replace.
	action := audit.ActionPermissionUpdate
	if existing == nil {
		action = audit.ActionPermissionCreate
	}
	s.audit.Record(r.Context(), audit.Event{
		Actor:  actorName(r),
		Action: action,
		Detail: auditDetail("name", body.Name, "principals", strconv.Itoa(len(principals))),
	})
	w.WriteHeader(http.StatusCreated)
}

// actorName resolves the audit actor from the request principal ("" lets
// audit's Append stamp "anonymous"; the management routes are authenticated
// so this is the admin's name in practice).
func actorName(r *http.Request) string {
	if p := principalFrom(r.Context()); p != nil {
		return p.Name
	}
	return ""
}

// marshalStrings renders a string slice as a JSON array column value
// ("[]" for empty).
func marshalStrings(vals []string) string {
	if len(vals) == 0 {
		return "[]"
	}
	b, err := json.Marshal(vals)
	if err != nil {
		return "[]"
	}
	return string(b)
}

// handlePermissionList serves GET /api/v1/permissions: every target with its
// principals expanded (FR-5-AC10). The no-filter branch is FROZEN by M9's
// additive-only decree (architecture section 14.1.6: byte-identical
// behavior, gate and error body) — the E6 filter arm lives beside it in
// handlePermissionListManage, and only the body construction is shared.
func (s *Server) handlePermissionList(w http.ResponseWriter, r *http.Request) {
	targets, err := s.deps.Metadata.Permissions().ListTargets(r.Context())
	if err != nil {
		writePlainError(w, http.StatusInternalServerError, "list permission targets: "+err.Error())
		return
	}
	out := make([]permissionBody, 0, len(targets))
	for _, t := range targets {
		_, principalRows, err := s.deps.Metadata.Permissions().GetTarget(r.Context(), t.Name)
		// A per-target read failure renders that target with empty
		// principals — the list is a read plane, one bad row must not 500
		// the inventory (pre-T-254 posture, kept verbatim).
		if err != nil {
			principalRows = nil
		}
		out = append(out, permissionBodyOf(t, unmarshalStrings(t.Repos), principalRows))
	}
	writeJSONBody(w, http.StatusOK, out)
}

// permissionManageFilterPresent reports whether the request carries a
// non-empty ?filter= ask (M9 E6): an empty value is no ask — the T-253
// optional-parameter convention — so ?filter= rides the FROZEN no-filter
// route for every caller, and ANY non-empty value (valid or not) takes the
// filter branch where the handler validates it. Whitespace-only values are
// empties here and tolerated-then-ignored there, one predicate on both
// sides of the route split.
func permissionManageFilterPresent(r *http.Request) bool {
	for _, v := range r.URL.Query()["filter"] {
		if strings.TrimSpace(v) != "" {
			return true
		}
	}
	return false
}

// handlePermissionListManage serves GET /api/v1/permissions?filter=manage —
// E6, the m-holder readability face (M9, ADR-0030 / architecture section
// 14.1.6, FR-79.2). The route demands only authentication (the gate is
// query-dependent, so it is evaluated HERE, the family-4 write-verb
// pattern): security readers (admin, readonly_admin) receive the full list,
// byte-equivalent to the no-filter response; a plain user with a non-empty
// manage coverage receives exactly the targets whose every repository sits
// inside that coverage (partially covered targets are HIDDEN — B1's replace
// arm demands union(body, stored) ⊆ coverage, so a partially covered target
// is not editable and showing it would only invite a doomed save); the
// empty coverage answers the SAME 403 the no-filter route gate answers, so
// a principal without the manage bit cannot distinguish the two branches —
// zero new distinguishability (NFR-S49's information isolation: no
// out-of-coverage target's name, repository, pattern or principal appears
// anywhere in the response). An unknown filter value is the governance
// family's explicit 400 (refused, never silently ignored — T-253's
// ?include convention).
func (s *Server) handlePermissionListManage(w http.ResponseWriter, r *http.Request) {
	for _, v := range r.URL.Query()["filter"] {
		switch strings.TrimSpace(v) {
		case "":
			// An empty value carries no ask (?filter=&...): tolerated, like
			// every other optional parameter's empty spelling. The router
			// only sends non-empty asks down this branch; a repeated
			// parameter's empty entries land here.
		case "manage":
		default:
			writeError(w, http.StatusBadRequest,
				`filter must be "manage" (unknown filter value: `+strconv.Quote(strings.TrimSpace(v))+")")
			return
		}
	}
	p := principalFrom(r.Context())
	if s.canManage(r.Context(), p, auth.CapSecurityRead) {
		// The full list, rendered through the same single-trip renderer —
		// byte-equality with the no-filter response is pinned by test
		// (TestT254FilterManageRoleMatrix), which is what "equivalent"
		// means here.
		s.writePermissionTargets(w, r, nil)
		return
	}
	coverage, universe := manageCoverageOf(r.Context(), s.deps.Authz, p)
	if !universe && len(coverage) == 0 {
		// The same status, content type and body the route gate answers for
		// the no-filter request — writeError is the very helper that renders
		// "administrator privileges required".
		writeError(w, http.StatusForbidden, "administrator privileges required")
		return
	}
	s.writePermissionTargets(w, r, coverage)
}

// writePermissionTargets renders the permissions list. coverage == nil
// means no filtering (every target); otherwise a target renders iff it
// names at least one repository and every named repository sits inside the
// coverage — the editable set, PRD FR-79.2's gloss of the subset predicate.
// (An empty repos list cannot pass the write arm's non-empty certification,
// so the vacuous "empty set ⊆ coverage" reading would show a target the
// holder cannot edit; the wire's own validation keeps such targets from
// existing, the guard is for hand-seeded rows.) Rows arrive through ONE
// Principals query bucketed per target — the single-trip shape; the
// no-filter handler above deliberately keeps its historical
// GetTarget-per-target walk, frozen with its bytes.
func (s *Server) writePermissionTargets(w http.ResponseWriter, r *http.Request, coverage map[string]struct{}) {
	targets, err := s.deps.Metadata.Permissions().ListTargets(r.Context())
	if err != nil {
		writePlainError(w, http.StatusInternalServerError, "list permission targets: "+err.Error())
		return
	}
	allRows, err := s.deps.Metadata.Permissions().Principals(r.Context())
	if err != nil {
		writePlainError(w, http.StatusInternalServerError, "list permission principals: "+err.Error())
		return
	}
	rowsByTarget := make(map[string][]*metadata.PermissionPrincipal, len(targets))
	for _, row := range allRows {
		rowsByTarget[row.TargetName] = append(rowsByTarget[row.TargetName], row)
	}
	out := make([]permissionBody, 0, len(targets))
	for _, t := range targets {
		repos := unmarshalStrings(t.Repos)
		if coverage != nil {
			covered := len(repos) > 0
			for _, repoKey := range repos {
				if _, ok := coverage[repoKey]; !ok {
					covered = false
					break
				}
			}
			if !covered {
				continue
			}
		}
		out = append(out, permissionBodyOf(t, repos, rowsByTarget[t.Name]))
	}
	writeJSONBody(w, http.StatusOK, out)
}

// permissionBodyOf builds one list row: the target's decoded columns plus
// its principal rows expanded into the wire's action letters. The shared
// construction of the frozen no-filter list and E6's filtered list — the
// two faces must render field for field identically (14.1.6: item fields
// identical to the full list).
func permissionBodyOf(t *metadata.PermissionTarget, repos []string, rows []*metadata.PermissionPrincipal) permissionBody {
	body := permissionBody{
		Name:            t.Name,
		Repos:           repos,
		IncludePatterns: unmarshalStrings(t.Includes),
		ExcludePatterns: unmarshalStrings(t.Excludes),
		Principals: permissionPrincipalsBody{
			Users:  map[string][]string{},
			Groups: map[string][]string{},
		},
	}
	for _, row := range rows {
		// T-217 (FR-65): the manage bit echoes in the same r/w/d order
		// plus m. T-444 (ADR-0044 K68): the write word echoes its canonical
		// deploy-cache form (the alias arm is receive-only) and annotate
		// takes its own column — the reference's five-column order.
		actions := make([]string, 0, 5)
		if row.CanRead {
			actions = append(actions, "read")
		}
		if row.CanWrite {
			actions = append(actions, "deploy-cache")
		}
		if row.CanAnnotate {
			actions = append(actions, "annotate")
		}
		if row.CanDelete {
			actions = append(actions, "delete")
		}
		if row.CanManage {
			actions = append(actions, "manage")
		}
		if row.PrincipalType == "group" {
			body.Principals.Groups[row.Principal] = actions
			continue
		}
		body.Principals.Users[row.Principal] = actions
	}
	return body
}

// unmarshalStrings parses a JSON array column value ("[]" for empty).
func unmarshalStrings(v string) []string {
	var out []string
	if err := json.Unmarshal([]byte(v), &out); err != nil {
		return []string{}
	}
	return out
}

// handlePermissionDelete serves DELETE /binflow/api/v1/permissions/{name}
// (and its /api/v1/permissions sibling): the grants vanish with the target
// (the store deletes both tables in one transaction); an unknown name is a
// 404 for security writers. Success is the plane's frozen 204.
//
// Family 4's exception gate (T-217): a non-security-writer may delete only a
// target whose every repository sits inside its manage coverage. For such
// callers an UNKNOWN name answers the coverage 403, not the 404 — an unknown
// target is definitionally outside any coverage, and hiding its existence
// keeps the permission plane's inventory (security:read data) away from
// principals who cannot list it.
func (s *Server) handlePermissionDelete(w http.ResponseWriter, r *http.Request, name string) {
	s.permissionDelete(w, r, name, false)
}

// handlePermissionDeleteV1 serves DELETE /api/security/permissions/{name}
// (L007-1, D04-R18 — the classic face): the reference answers 200 with the
// plain-text confirmation "Successfully deleted permission Target 'x'" and
// wraps the unknown name in the errors-envelope "Not Found" (live :8082,
// wire-verified 2026-09-12) — the keyed rendering pair this face now
// mirrors, where the rich face keeps its frozen 204.
func (s *Server) handlePermissionDeleteV1(w http.ResponseWriter, r *http.Request, name string) {
	s.permissionDelete(w, r, name, true)
}

// permissionDelete is the delete core both faces share: the T-217 gate pair
// and the store transaction are one behavior, only the terminal renderings
// split — keyedV1 answers the reference's 200 confirmation text and the
// envelope 404, the rich face its frozen 204 and plain 404.
func (s *Server) permissionDelete(w http.ResponseWriter, r *http.Request, name string, keyedV1 bool) {
	p := principalFrom(r.Context())
	secWrite := s.canManage(r.Context(), p, auth.CapSecurityWrite)
	target, _, err := s.deps.Metadata.Permissions().GetTarget(r.Context(), name)
	switch {
	case err == nil:
	case errors.Is(err, metadata.ErrNotFound):
		if keyedV1 && secWrite {
			writeError(w, http.StatusNotFound, "Not Found")
			return
		}
		if secWrite {
			writePlainError(w, http.StatusNotFound, "permission target not found: "+name)
			return
		}
		writePlainError(w, http.StatusForbidden, errPermissionEditDenied)
		return
	default:
		writePlainError(w, http.StatusInternalServerError, "lookup permission target: "+err.Error())
		return
	}
	if !secWrite && !s.canManageAllRepos(r.Context(), p, unmarshalStrings(target.Repos)) {
		writePlainError(w, http.StatusForbidden, errPermissionEditDenied)
		return
	}
	if err := s.deps.Metadata.Permissions().DeleteTarget(r.Context(), name); err != nil {
		writePlainError(w, http.StatusInternalServerError, "delete permission target: "+err.Error())
		return
	}
	s.audit.Record(r.Context(), audit.Event{
		Actor:  actorName(r),
		Action: audit.ActionPermissionDelete,
		Detail: auditDetail("name", name),
	})
	if keyedV1 {
		writeText(w, http.StatusOK, fmt.Sprintf("Successfully deleted permission Target '%s'", name))
		return
	}
	w.WriteHeader(http.StatusNoContent)
}

// ---- L006-B (D04-R17/R18): the classic v1 read faces ----
//
// /api/security/permissions is Artifactory's original permission-target
// plane (SecurityResource.java; gap-endpoints section 4, high confidence):
// the list answers the {name, uri} skeleton (admin only; the uri carries
// the URL-escaped name), the detail answers the v1 shape — flat patterns
// and single-letter actions (r/w/n/d/m; annotate is n, rest-api.md's
// ArtifactoryPermission set). Both read the SAME store rows the rich
// /api/v1/permissions face serves; only the projection differs.

// permissionTargetRefV1 is the list entry's wire shape.
type permissionTargetRefV1 struct {
	Name string `json:"name"`
	URI  string `json:"uri"`
}

// permissionTargetV1 is the v1 detail's wire shape: patterns render as the
// reference's flat default-carrying strings (empty includes = "**", the
// match-everything default; empty excludes = ""), and principals render as
// single-letter action lists with only the present kind's key.
type permissionTargetV1 struct {
	Name            string                         `json:"name"`
	IncludesPattern string                         `json:"includesPattern"`
	ExcludesPattern string                         `json:"excludesPattern"`
	Repositories    []string                       `json:"repositories"`
	Principals      map[string]map[string][]string `json:"principals"`
}

// handlePermissionListV1 serves GET /api/security/permissions: the
// {name, uri} skeleton over the same ListTargets read the rich face uses.
// Gate = security:read (the family's read capability; the reference's
// admin-only role maps onto it the way every other security read does).
func (s *Server) handlePermissionListV1(w http.ResponseWriter, r *http.Request) {
	targets, err := s.deps.Metadata.Permissions().ListTargets(r.Context())
	if err != nil {
		writePlainError(w, http.StatusInternalServerError, "list permission targets: "+err.Error())
		return
	}
	base := contextURL(r) + "/api/security/permissions/"
	out := make([]permissionTargetRefV1, 0, len(targets))
	for _, t := range targets {
		out = append(out, permissionTargetRefV1{Name: t.Name, URI: base + url.PathEscape(t.Name)})
	}
	writeJSONBody(w, http.StatusOK, out)
}

// handlePermissionGetV1 serves GET /api/security/permissions/{name}: the
// v1 detail. Unknown name answers the reference's errors envelope carrying
// the generic "Not Found" (live :8082, wire-verified 2026-09-12; L007-1
// arm 2 — the plane's earlier plain-text posture retired on this face
// only, the rich /api/v1/permissions face keeps its frozen wording).
func (s *Server) handlePermissionGetV1(w http.ResponseWriter, r *http.Request, name string) {
	target, rows, err := s.deps.Metadata.Permissions().GetTarget(r.Context(), name)
	if err != nil {
		if errors.Is(err, metadata.ErrNotFound) {
			writeError(w, http.StatusNotFound, "Not Found")
			return
		}
		writePlainError(w, http.StatusInternalServerError, "lookup permission target: "+err.Error())
		return
	}
	body := permissionTargetV1{
		Name:         target.Name,
		Repositories: unmarshalStrings(target.Repos),
		Principals:   map[string]map[string][]string{},
	}
	// The flat pattern strings: BinFlow stores pattern ARRAYS (the rich
	// face's model); the v1 face renders them joined with "," (the
	// reference's separator), with the match-everything default materialized
	// for an empty includes list.
	includes := unmarshalStrings(target.Includes)
	if len(includes) == 0 {
		includes = []string{"**"}
	}
	body.IncludesPattern = strings.Join(includes, ",")
	body.ExcludesPattern = strings.Join(unmarshalStrings(target.Excludes), ",")
	for _, row := range rows {
		// The single-letter action set (r/w/n/d/m) — the reference's own
		// detail rendering; the words the write faces accept map onto it.
		letters := make([]string, 0, 5)
		if row.CanRead {
			letters = append(letters, "r")
		}
		if row.CanWrite {
			letters = append(letters, "w")
		}
		if row.CanAnnotate {
			letters = append(letters, "n")
		}
		if row.CanDelete {
			letters = append(letters, "d")
		}
		if row.CanManage {
			letters = append(letters, "m")
		}
		kind := "users"
		if row.PrincipalType == "group" {
			kind = "groups"
		}
		if body.Principals[kind] == nil {
			body.Principals[kind] = map[string][]string{}
		}
		body.Principals[kind][row.Principal] = letters
	}
	writeJSONBody(w, http.StatusOK, body)
}
