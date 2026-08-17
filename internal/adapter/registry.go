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
	byType  map[string]Handler // package type ("generic") -> handler
	ordered []string           // registration order, for stable All()
}

var reg = &registry{byProto: map[string]Handler{}, byType: map[string]Handler{}}

// Register adds h. A duplicate Protocol name or a duplicate claim on an
// already-served package type panics: both are assembly bugs that must
// surface at startup, never at request time.
func Register(h Handler) {
	if h == nil {
		panic("adapter: Register(nil handler)")
	}
	proto := h.Protocol()
	if proto == "" {
		panic("adapter: Register: handler has an empty Protocol")
	}
	types := h.RepoTypes()
	if len(types) == 0 {
		panic(fmt.Sprintf("adapter: Register(%s): RepoTypes is empty", proto))
	}
	reg.mu.Lock()
	defer reg.mu.Unlock()
	if _, dup := reg.byProto[proto]; dup {
		panic(fmt.Sprintf("adapter: Register: duplicate protocol %q", proto))
	}
	for _, t := range types {
		if _, dup := reg.byType[t]; dup {
			panic(fmt.Sprintf("adapter: Register(%s): repo type %q already served by %s", proto, t, reg.byType[t].Protocol()))
		}
	}
	reg.byProto[proto] = h
	// byType keys on the REPOSITORY CLASS for classes it serves AND on the
	// protocol's own package type (the dispatch key httpapi uses: a repo row
	// with package_type "generic" routes to the generic handler). Claiming a
	// class the protocol does not serve (docker on "remote") still reserves
	// it: two handlers both serving remote repos would be a routing coin
	// flip, which is exactly the startup bug this panic exists to catch.
	for _, t := range types {
		reg.byType[t] = h
	}
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
// adapter). ok is false when no handler serves the type.
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
