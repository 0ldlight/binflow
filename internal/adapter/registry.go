package adapter

import (
	"fmt"
	"sort"
	"sync"
)

// registry is the process-wide handler registry. Protocol packages register
// their Handler from an exported RegisterX func called by cmd assembly (the
// architecture sketch shows init(); deferring to explicit assembly keeps
// every dependency injected — no package-level singletons, architecture
// section 2 rule 3).
type registry struct {
	mu      sync.RWMutex
	byProto map[string]Handler
	byType  map[string]Handler // package type ("generic") -> handler; the ONLY key
	byMeta  map[string]MetadataProvider
	ordered []string // registration order, for stable All()
}

var reg = &registry{
	byProto: map[string]Handler{},
	byType:  map[string]Handler{},
	byMeta:  map[string]MetadataProvider{},
}

// Register adds h. Registration keys on the handler's package type —
// Protocol() ≡ the repositories.package_type value httpapi dispatches on —
// and a duplicate key panics: an assembly bug that must surface at startup,
// never at request time. A duplicate package type is the one collision a
// registry can actually catch: dispatch is by row.PackageType, so two
// handlers on one type would be a routing coin flip.
//
// RepoTypes is declarative metadata only (T-33 ruling, T-48 errata,
// collected by T-63): the repository CLASS is not and must never become a
// key — generic, docker, maven, npm and pypi all serve class=local, which a
// class-keyed map could not tell apart.
func Register(h Handler) {
	if h == nil {
		panic("adapter: Register(nil handler)")
	}
	proto := h.Protocol()
	if proto == "" {
		panic("adapter: Register: handler has an empty Protocol")
	}
	if len(h.RepoTypes()) == 0 {
		panic(fmt.Sprintf("adapter: Register(%s): RepoTypes is empty", proto))
	}
	reg.mu.Lock()
	defer reg.mu.Unlock()
	if _, dup := reg.byProto[proto]; dup {
		panic(fmt.Sprintf("adapter: Register: duplicate protocol %q", proto))
	}
	reg.byProto[proto] = h
	reg.byType[proto] = h
	reg.ordered = append(reg.ordered, proto)
}

// All returns every registered handler in registration order. httpapi
// mounts them all at startup.
func All() []Handler {
	reg.mu.RLock()
	defer reg.mu.RUnlock()
	out := make([]Handler, 0, len(reg.ordered))
	for _, p := range reg.ordered {
		out = append(out, reg.byProto[p])
	}
	return out
}

// ForRepoType resolves the handler serving repositories of that package
// type (architecture section 5.1 dispatch: repo row -> package_type ->
// adapter). ok is false when no handler serves the type. The lookup keys
// ONLY on the package type: a repository class ("local", "remote",
// "virtual") is not a lookup key (T-33/T-48 errata, collected by T-63) —
// several protocols legally share one class.
func ForRepoType(packageType string) (h Handler, ok bool) {
	reg.mu.RLock()
	defer reg.mu.RUnlock()
	h, ok = reg.byType[packageType]
	return h, ok
}

// Protocols returns the registered protocol names sorted (diagnostics).
func Protocols() []string {
	reg.mu.RLock()
	defer reg.mu.RUnlock()
	out := make([]string, 0, len(reg.byProto))
	for p := range reg.byProto {
		out = append(out, p)
	}
	sort.Strings(out)
	return out
}
