package npm

import "strings"

// routeKind enumerates the npm protocol addresses this adapter serves.
// Anything outside the list is a strict 404 (NE-08/M58: search, audits,
// attestations, keys, the legacy /-/all dump — deliberately not built).
type routeKind int

const (
	routeInvalid      routeKind = iota
	routeRoot                   // ""            — domain root probe (200 empty)
	routePing                   // -/ping
	routeWhoami                 // -/whoami
	routeUserLogin              // -/user/org.couchdb.user:<name>[-rev/<rev>]
	routeDistTags               // -/package/<name>/dist-tags
	routeDistTag                // -/package/<name>/dist-tags/<tag>
	routePackument              // <name>                  (GET packument / PUT publish)
	routeTagOrVersion           // <name>/<tail>           (GET version / legacy PUT tag)
	routePackageRev             // <name>/-rev/<rev>       (PUT fake-success / DELETE whole)
	routeTarball                // <name>/-/<file>.tgz     (GET/HEAD)
	routeTarballRev             // <name>/-/<file>/-rev/<rev> (DELETE single version)
)

// route is one parsed npm protocol address. relPath arrives DECODED (the
// layout peeled the repo key and percent-decoded %2f/%2F — the two scoped
// spellings are equivalent by the time parsing runs, C8 ruling).
type route struct {
	kind   routeKind
	name   string // package name ("pkg" or "@scope/pkg"); "" on service routes
	tail   string // single segment after the name (tag or version spelling)
	tag    string // one dist-tag (the /-/package/... family)
	file   string // tarball filename ("<name>-<version>.tgz" shape)
	rev    string // the -rev placeholder (opaque; the spec pins it unused)
	userID string // couch user id ("org.couchdb.user:<name>")
}

// couchUserPrefix is the login endpoint's user-id family (npm legacy auth
// types; npm >= 9 needs --auth-type=legacy to walk this path).
const couchUserPrefix = "org.couchdb.user:"

// parseRoute maps a decoded repo-relative path onto a npm route. ok is false
// for every address outside the npm protocol shape — the caller answers the
// strict 404. A single trailing slash is tolerated (the domain-root spelling
// "GET /api/npm/<repo>/" ends in one; Artifactory's router is equally lax).
func parseRoute(rel string) (route, bool) {
	rel = strings.TrimSuffix(rel, "/")
	if rel == "" {
		return route{kind: routeRoot}, true
	}
	segs := strings.Split(rel, "/")

	if segs[0] == "-" {
		return parseServiceRoute(segs)
	}

	// Package name: one segment, or two when scoped ("@scope/name"). A scope
	// without a name ("@scope" alone) is not a package address.
	var name string
	var rest []string
	if strings.HasPrefix(segs[0], "@") {
		if len(segs) < 2 {
			return route{}, false
		}
		name, rest = segs[0]+"/"+segs[1], segs[2:]
	} else {
		name, rest = segs[0], segs[1:]
	}

	switch {
	case len(rest) == 0:
		return route{kind: routePackument, name: name}, true
	case len(rest) == 1:
		return route{kind: routeTagOrVersion, name: name, tail: rest[0]}, true
	case rest[0] == "-rev" && len(rest) == 2:
		return route{kind: routePackageRev, name: name, rev: rest[1]}, true
	case rest[0] == "-" && len(rest) >= 2:
		// Everything after "-" is the FILENAME, rejoined with "/" — a scoped
		// tarball filename carries its own scope segment
		// ("@scope/pkg-1.0.0.tgz" is two path segments). A trailing
		// "-rev/<rev>" pair marks the unpublish DELETE.
		if n := len(rest); n >= 4 && rest[n-2] == "-rev" {
			return route{kind: routeTarballRev, name: name, file: strings.Join(rest[1:n-2], "/")}, true
		}
		return route{kind: routeTarball, name: name, file: strings.Join(rest[1:], "/")}, true
	default:
		return route{}, false
	}
}

// parseServiceRoute handles the "/-/..." service namespace. The dist-tags
// family finds its "dist-tags" marker from the END so a package literally
// named "dist-tags" cannot shift the boundary.
func parseServiceRoute(segs []string) (route, bool) {
	switch {
	case len(segs) == 2 && segs[1] == "ping":
		return route{kind: routePing}, true
	case len(segs) == 2 && segs[1] == "whoami":
		return route{kind: routeWhoami}, true
	case len(segs) == 3 && segs[1] == "user" && strings.HasPrefix(segs[2], couchUserPrefix):
		return route{kind: routeUserLogin, userID: segs[2]}, true
	case len(segs) == 5 && segs[1] == "user" && strings.HasPrefix(segs[2], couchUserPrefix) &&
		segs[3] == "-rev" && segs[4] != "":
		// The couch E409 retry spelling: PUT /-/user/<id>/-rev/<rev>. Same
		// handler as the plain login (the rev is opaque).
		return route{kind: routeUserLogin, userID: segs[2]}, true
	case len(segs) >= 4 && segs[1] == "package":
		return parseDistTagsRoute(segs[2:])
	default:
		return route{}, false
	}
}

// parseDistTagsRoute parses the segments after "-/package/":
//
//	<name...>/dist-tags            -> the collection (GET/PUT bulk)
//	<name...>/dist-tags/<tag>      -> one tag (PUT/DELETE)
//
// The name is one segment, or two when scoped.
func parseDistTagsRoute(segs []string) (route, bool) {
	// Locate the LAST "dist-tags" segment; everything before it is the name,
	// everything after is at most one tag.
	marker := -1
	for i, s := range segs {
		if s == "dist-tags" {
			marker = i
		}
	}
	if marker < 0 {
		return route{}, false
	}
	nameSegs, tail := segs[:marker], segs[marker+1:]
	if len(tail) > 1 {
		return route{}, false
	}
	var name string
	switch {
	case len(nameSegs) == 1:
		name = nameSegs[0]
	case len(nameSegs) == 2 && strings.HasPrefix(nameSegs[0], "@"):
		name = nameSegs[0] + "/" + nameSegs[1]
	default:
		return route{}, false
	}
	if len(tail) == 0 {
		return route{kind: routeDistTags, name: name}, true
	}
	return route{kind: routeDistTag, name: name, tag: tail[0]}, true
}
