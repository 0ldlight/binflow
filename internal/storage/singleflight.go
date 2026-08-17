package storage

import "sync"

// singleflight collapses concurrent calls with the same key into one
// execution: the first caller runs fn, the others block until it finishes
// and receive the same result. It is the per-checksum mutex layer that makes
// concurrent uploads of the same blob converge to a single physical write
// (ADR-0006 decision 4).
//
// This is deliberately not golang.org/x/sync/singleflight: the dependency
// whitelist is stdlib-only (ADR-0005) and the engine needs the caller's
// context to be the only cancellation source.
type singleflight struct {
	mu sync.Mutex
	m  map[string]*call
}

type call struct {
	wg  sync.WaitGroup
	err error
}

// do runs fn at most once per in-flight key. Concurrent callers with the
// same key share one execution; callers arriving after it completed simply
// run fn themselves (by then fn is expected to hit its fast path — for the
// engine that is "blob already exists"). The returned error is fn's own
// error, shared verbatim with waiters so errors.Is matching on wrapped
// sentinels behaves identically for everyone.
func (g *singleflight) do(key string, fn func() error) error {
	g.mu.Lock()
	if g.m == nil {
		g.m = make(map[string]*call)
	}
	if c, ok := g.m[key]; ok {
		g.mu.Unlock()
		c.wg.Wait()
		return c.err
	}
	c := &call{}
	c.wg.Add(1)
	g.m[key] = c
	g.mu.Unlock()

	c.err = fn()

	g.mu.Lock()
	delete(g.m, key)
	g.mu.Unlock()
	c.wg.Done()
	return c.err
}
