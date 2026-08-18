package docker

import (
	"context"
	"strings"

	"github.com/lzwzzy/binflow/internal/auth"
)

// Token-flow scope vocabulary (distribution token authentication): one scope
// token is "<resourceType>:<resourceName>:<action[,action...]>". BinFlow
// understands "repository:<repoKey>/<image>:pull|push|delete" (ADR-0010
// clause 5: the subject's first segment is the BinFlow repo key, the
// remainder is the image path) and the registry-level "registry:catalog:*".
// Every other shape is tolerated and dropped — the token endpoint does not
// validate scopes (docker-registry.md section 5.2; enforcement lives on the
// resource endpoints), and the AC's invalid-scope row pins tolerance over
// rejection.
const (
	scopeTypeRepository = "repository"
	scopeTypeRegistry   = "registry"

	scopeActionPull   = "pull"
	scopeActionPush   = "push"
	scopeActionDelete = "delete"
	scopeActionAll    = "*"

	// scopeRegistryCatalog is the catalog endpoint's scope (DE-13/AC2:
	// catalog -> registry:catalog:*).
	scopeRegistryCatalog = "registry:catalog:*"

	// scopeCatalogResource is the registry-level resource name the catalog
	// scope addresses.
	scopeCatalogResource = "catalog"
)

// Method literals for the scope/derivation tables (kept local so the tables
// read as protocol data, not net/http calls).
const (
	httpMethodGet    = "GET"
	httpMethodHead   = "HEAD"
	httpMethodPut    = "PUT"
	httpMethodPost   = "POST"
	httpMethodPatch  = "PATCH"
	httpMethodDelete = "DELETE"
)

// requestedScope is one parsed scope token of a token-endpoint request.
type requestedScope struct {
	resourceType string
	resourceName string
	actions      []string
}

// parseScopes splits the raw scope parameter values (docker clients send one
// space-separated parameter; multiple parameters are joined first) into
// parsed scopes, DROPPING every token that is not the three-part shape or
// carries no known action. Drop, never 400: a client asking for
// "repository:x:pull gibberish" still gets its valid half (invalid-scope
// tolerance, AC3's matrix).
func parseScopes(raw ...string) []requestedScope {
	tokens := strings.Fields(strings.Join(raw, " "))
	out := make([]requestedScope, 0, len(tokens))
	for _, tok := range tokens {
		typ, name, actions, ok := splitScopeToken(tok)
		if !ok {
			continue
		}
		out = append(out, requestedScope{resourceType: typ, resourceName: name, actions: actions})
	}
	return out
}

// splitScopeToken validates one scope token and splits it. ok=false marks a
// tolerated-and-dropped token: wrong part count, empty type/name, or no
// recognized action at all. The NAME may itself contain ':' (rejoined
// verbatim) — spec scope grammars differ on escaping and a registry name
// never legitimately does, but the token grammar has exactly two separators
// with the actions being the LAST segment.
func splitScopeToken(tok string) (typ, name string, actions []string, ok bool) {
	parts := strings.Split(tok, ":")
	if len(parts) < 3 {
		return "", "", nil, false
	}
	typ = parts[0]
	name = strings.Join(parts[1:len(parts)-1], ":")
	if typ == "" || name == "" {
		return "", "", nil, false
	}
	for _, a := range strings.Split(parts[len(parts)-1], ",") {
		switch strings.TrimSpace(a) {
		case scopeActionPull, scopeActionPush, scopeActionDelete, scopeActionAll:
			actions = append(actions, strings.TrimSpace(a))
		}
	}
	if len(actions) == 0 {
		return "", "", nil, false
	}
	return typ, name, actions, true
}

// deriveChallengeScope maps one protected name-route request onto the scope
// the 401 challenge advertises (AC2, docker-registry.md section 5.1's
// endpoint table): reads -> pull, writes -> pull,push, deletes ->
// pull,delete (T-32 AC2: "DELETE 含 delete"). The scope's subject is the
// FULL name (<repoKey>/<image>) exactly as the client addressed it, so the
// token the client negotiates matches the resource it was denied.
func deriveChallengeScope(method string, ref nameRef) string {
	actions := scopeActionPull
	switch method {
	case httpMethodPut, httpMethodPost, httpMethodPatch:
		actions = scopeActionPull + "," + scopeActionPush
	case httpMethodDelete:
		actions = scopeActionPull + "," + scopeActionDelete
	}
	return scopeTypeRepository + ":" + ref.repoKey + "/" + ref.image + ":" + actions
}

// canActions maps one scope action onto the Authorizer.Can actions it stands
// for; "*" claims every action BinFlow can grant.
var canActions = map[string][]string{
	scopeActionPull:   {auth.ActionRead},
	scopeActionPush:   {auth.ActionWrite},
	scopeActionDelete: {auth.ActionDelete},
	scopeActionAll:    {auth.ActionRead, auth.ActionWrite, auth.ActionDelete},
}

// actionNames is the inverse map: a Can action's scope spelling.
var actionNames = map[string]string{
	auth.ActionRead:   scopeActionPull,
	auth.ActionWrite:  scopeActionPush,
	auth.ActionDelete: scopeActionDelete,
}

// narrowScopes computes the granted subset of the requested scopes for one
// principal (AC4: the response's scope field is narrowed by the ACL;
// enforcement itself stays per-request Can — the narrowed field is a client
// hint, never an authorization). Rules:
//
//   - repository scopes: each action maps onto Authorizer.Can questions
//     (pull->r, push->w, delete->d, * expands) over (repoKey, image-path)
//     per ADR-0010 clause 5. A "*" action is answered with the concrete
//     granted subset, never a blanket "*". Actions that fail Can are
//     stripped; a scope with no surviving action disappears from the grant.
//   - registry:catalog:*: granted to any authenticated principal. The
//     catalog's rows are filtered by read-ACL at the endpoint itself (Q5
//     interim ruling; the endpoint is T-40's).
//   - A nil Authorizer (assembly without one) grants nothing: the token is
//     still issued, the scope field comes back empty, and per-request
//     enforcement is untouched — fail closed on the hint, not on the token.
func (h *Handler) narrowScopes(ctx context.Context, p *Principal, scopes []requestedScope) []string {
	granted := make([]string, 0, len(scopes))
	for _, sc := range scopes {
		switch sc.resourceType {
		case scopeTypeRepository:
			repoKey, path := splitScopeSubject(sc.resourceName)
			var kept []string
			if p != nil && p.Admin {
				// Admin bypasses Can (the Authorizer's own rule 1): a lookup
				// through the store would be wasted work AND wrong under a
				// nil-authorizer assembly.
				for _, a := range sc.actions {
					if a == scopeActionAll {
						kept = append(kept, scopeActionPull, scopeActionPush, scopeActionDelete)
					} else {
						kept = appendUnique(kept, a)
					}
				}
			} else {
				for _, mapped := range canMapActions(sc.actions) {
					if h.authz != nil && h.authz.Can(ctx, p, repoKey, path, mapped) {
						kept = appendUnique(kept, actionNames[mapped])
					}
				}
			}
			if len(kept) > 0 {
				granted = appendUnique(granted,
					scopeTypeRepository+":"+sc.resourceName+":"+strings.Join(kept, ","))
			}
		case scopeTypeRegistry:
			if sc.resourceName == scopeCatalogResource && p != nil {
				granted = appendUnique(granted, scopeRegistryCatalog)
			}
		}
	}
	return granted
}

// canMapActions flattens the requested actions into their Can questions,
// deduplicated in stable order.
func canMapActions(actions []string) []string {
	var out []string
	for _, a := range actions {
		for _, mapped := range canActions[a] {
			out = appendUnique(out, mapped)
		}
	}
	return out
}

// splitScopeSubject splits a repository scope's subject into the BinFlow
// repo key and the repo-relative image path (ADR-0010 clause 5). A
// single-segment subject ("repository:ubuntu:pull", the spec's official-
// name form) leaves the path empty: BinFlow's /v2 plane cannot address a
// repository without the repo-key prefix anyway (single-segment names 404),
// so the narrowing degrades to a repo-key-only ACL question rather than
// inventing a path.
func splitScopeSubject(name string) (repoKey, path string) {
	key, rest, found := strings.Cut(name, "/")
	if !found {
		return key, ""
	}
	return key, rest
}

// appendUnique appends v when not already present, preserving order.
func appendUnique(list []string, v string) []string {
	for _, e := range list {
		if e == v {
			return list
		}
	}
	return append(list, v)
}
