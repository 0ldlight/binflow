package remote

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"log/slog"
	"net/http"
	"net/netip"
	"net/url"
	"strings"
	"sync"
	"sync/atomic"
	"time"

	"github.com/lzwzzy/binflow/internal/metadata"
	"github.com/lzwzzy/binflow/internal/storage"
)

// The pull-through fetcher (FR-20, RE-04's six-step flow over repo-semantics
// section 7.2 with the T-79 errata fault semantics):
//
//	1. blocked-out mask -> 404 (rest-api.md 1.2 step 6 wording);
//	2. checksum sidecar suffix -> 404 "Checksums are not downloadable."
//	   (never proxied, zero upstream traffic);
//	3. negative cache within missedRetrievalCachePeriodSecs -> 404, zero
//	   upstream traffic;
//	4. local copy within its TTL (content vs metadata class via the
//	   MetadataProvider registry) -> served, zero upstream traffic;
//	5. expired or missing -> upstream via the T-65 guarded client:
//	     200  -> streamed through a storage session, node landed in the
//	             remote repository's own namespace, served (MISS);
//	     404  -> negative cache written; an expired local copy is still
//	             served ("expired but serving", repo-semantics 7.2 step 5);
//	     401/403 -> resource unfound, message carries the upstream summary
//	             (repo-semantics 7.6);
//	     5xx/timeout/transport -> repository marked assumed-offline for
//	             assumedOfflinePeriodSecs (zero upstream traffic inside the
//	             window); an expired copy is served with
//	             X-Binflow-Upstream-Error (STALE), without a copy the answer
//	             is 404 naming the offline state, or 502 on hardFail:true;
//	6. writes never reach the engine: the service layer answers 405 before
//	   this API is consulted (RE-05).
//
// Concurrency follows repo-semantics 7.3: one in-flight fetch per
// (repo, path); waitors re-run the lookup when the winner lands (the
// double-check), so a miss stampede costs one upstream request and waitors
// never see a 5xx of their own.

// FetchError is the engine's classified failure. The service layer maps it
// verbatim onto the client response (status + exact message); Unfound marks
// the not-found family so ErrNodeNotFound-based mappings keep working.
type FetchError struct {
	// Status is the client-facing HTTP status: 404 (unfound family), 502
	// (hardFail / oversized buffered body) or 400 (SSRF chain denial).
	Status int
	// Message is the EXACT response body message (M45 compares the checksum
	// refusal for equality).
	Message string
	// Unfound reports not-found semantics (upstream miss, negative cache,
	// checksum sidecar, offline without a copy): the service wraps
	// ErrNodeNotFound so /api/storage-style callers keep their 404 mapping.
	Unfound bool
}

func (e *FetchError) Error() string { return e.Message }

// FetchResult is one successful pull-through read. HasCopy is the explicit
// stale/miss distinction T-71's member resolution consumes (R10): true
// whenever THIS repository holds a copy of the path — fresh, freshly landed
// or expired-but-serving. A true miss never returns a result at all: it is a
// *FetchError with Unfound set.
type FetchResult struct {
	// Node is the cached node row in the remote repository's namespace.
	Node *metadata.Node
	// Body is the opened blob; the caller must Close it. The concrete type
	// also carries the response hints (X-BinFlow-Cache, and
	// X-Binflow-Upstream-Error on stale service) for the serving adapter's
	// structural probe.
	Body io.ReadSeekCloser
	// CacheState is the X-BinFlow-Cache token (HIT/MISS/STALE).
	CacheState string
	// UpstreamError is the X-Binflow-Upstream-Error summary, set only when a
	// stale copy was served because of an upstream fault.
	UpstreamError string
	// HasCopy: see the type comment.
	HasCopy bool
}

// RepoStats is the RE-11 statistics snapshot of one remote repository (the
// /api/v1/remote/stats endpoint is P2; this is the data surface it reads).
type RepoStats struct {
	RepoKey       string
	Hits          int64 // fresh-copy serves
	Misses        int64 // upstream fetches that landed a copy
	Negatives     int64 // negative-cache serves
	Stales        int64 // expired-copy serves (upstream unfound or unavailable)
	UpstreamBytes int64 // body bytes pulled from upstreams
	CachedNodes   int64 // node rows currently cached
	CachedBytes   int64 // sum of cached node sizes
}

// repoPolicy is the remote policy that lives only in the canonical
// repositories.config JSON (T-64 handover note 2): the remote_configs row
// carries url/username/password/dual TTL/exemption/mask, these four ride the
// JSON. The field spellings MUST stay in sync with repo.remoteConfig's tags.
type repoPolicy struct {
	MissedRetrievalCachePeriodSecs int64 `json:"missedRetrievalCachePeriodSecs"`
	SocketTimeoutSecs              int64 `json:"socketTimeoutSecs"`
	AssumedOfflinePeriodSecs       int64 `json:"assumedOfflinePeriodSecs"`
	HardFail                       bool  `json:"hardFail"`
}

// defaultPolicy mirrors the PRD v1.2 C4 product defaults, used for rows
// whose config JSON predates a field or omits it.
var defaultPolicy = repoPolicy{
	MissedRetrievalCachePeriodSecs: 1800,
	SocketTimeoutSecs:              15,
	AssumedOfflinePeriodSecs:       300,
}

// defaultMetadataWait is the singleflight wait cap for metadata-class paths
// (repo-semantics 7.1, metadataRetrievalTimeoutSecs 60): a waitor blocked
// that long falls back to the stale copy / unfound answer instead of
// queueing behind a stuck winner.
const defaultMetadataWait = 60 * time.Second

// waitReason names what ended a singleflight wait.
type waitReason int

const (
	waitDone waitReason = iota
	waitCanceled
	waitTimeout
)

// errUpstreamBody marks a failure that happened while READING the upstream
// body (the session's Append) — the upstream's fault, eligible for the
// assumed-offline downgrade. Local write failures (Commit, metadata rows)
// carry no marker: they surface as plain 500s and are never blamed on the
// upstream (review side-fix).
var errUpstreamBody = errors.New("upstream body read")

// EngineOptions configures the Engine.
type EngineOptions struct {
	// Now is the TTL/offline clock; nil means UTC time.Now. Tests inject a
	// controllable one (shared with the repo service's clock).
	Now func() time.Time
	// Logger receives the fetch outcome lines; nil means slog.Default().
	Logger *slog.Logger
	// Key is the credentials master key. nil means "read the environment"
	// (BINFLOW_REMOTE_CREDENTIALS_KEY); a key present there but malformed is
	// a construction error either way.
	Key []byte
	// MetadataWait caps how long a metadata-class waitor blocks on the
	// singleflight before falling back (<= 0 means 60s).
	MetadataWait time.Duration
	// Resolve is the SSRF guard's resolver override (the T-65 test seam,
	// passed through to every per-repository client).
	Resolve func(ctx context.Context, host string) ([]netip.Addr, error)
}

// Engine is the process-wide remote proxy engine (architecture section 2's
// remote.Fetch facade). It is safe for concurrent use.
type Engine struct {
	st       storage.Engine
	md       metadata.Store
	nowFn    func() time.Time
	log      *slog.Logger
	cipher   *Cipher
	metaWait time.Duration
	resolve  func(ctx context.Context, host string) ([]netip.Addr, error)

	mu      sync.Mutex
	clients map[string]*cachedClient // repoKey -> client + signature
	offline map[string]time.Time     // repoKey -> assumed-offline until
	stats   map[string]*repoCounters
	flights map[string]chan struct{} // singleflight, repoKey + "/" + path
}

// repoCounters is the atomic counter block behind RepoStats.
type repoCounters struct {
	hits, misses, negatives, stales, bytes atomic.Int64
}

// cachedClient holds one repository's outbound client plus the signature it
// was built from (a config change rebuilds, dropping the old pool).
type cachedClient struct {
	sig    string
	client *Client
}

// NewEngine assembles the engine and runs the STARTUP credential pass
// (ADR-0012 decision 4 / FR-15-AC9): every remote_configs row that carries a
// password must be decryptable — ciphertext without a key refuses to boot,
// legacy plaintext is encrypted in place (the one-time migration, key
// required). The returned error is a startup refusal.
func NewEngine(st storage.Engine, md metadata.Store, opts EngineOptions) (*Engine, error) {
	if st == nil || md == nil {
		return nil, fmt.Errorf("remote engine: storage and metadata are required")
	}
	key := opts.Key
	if key == nil {
		var err error
		if key, err = LoadKey(); err != nil {
			return nil, err
		}
	}
	var cipher *Cipher
	if key != nil {
		c, err := NewCipher(key)
		if err != nil {
			return nil, err
		}
		cipher = c
	}
	e := &Engine{
		st:       st,
		md:       md,
		nowFn:    opts.Now,
		log:      opts.Logger,
		cipher:   cipher,
		metaWait: opts.MetadataWait,
		resolve:  opts.Resolve,
		clients:  map[string]*cachedClient{},
		offline:  map[string]time.Time{},
		stats:    map[string]*repoCounters{},
		flights:  map[string]chan struct{}{},
	}
	if e.nowFn == nil {
		e.nowFn = func() time.Time { return time.Now().UTC() }
	}
	if e.log == nil {
		e.log = slog.Default()
	}
	if e.metaWait <= 0 {
		e.metaWait = defaultMetadataWait
	}
	if err := e.migrateCredentials(context.Background()); err != nil {
		return nil, err
	}
	return e, nil
}

// Cipher exposes the credentials cipher (nil when no key is configured); the
// service layer uses it to encrypt passwords on repository create/update.
func (e *Engine) Cipher() *Cipher { return e.cipher }

// migrateCredentials is the startup pass: enumerate remote repositories,
// demand decryptability of every stored password, encrypt legacy plaintext
// in place. BinFlow M1/M2 never wrote these rows (T-62's grep evidence), so
// in practice the pass is empty; it exists for the upgrade scenario the AC
// mandates (FR-15-AC9-4).
func (e *Engine) migrateCredentials(ctx context.Context) error {
	repos, err := e.md.Repos().List(ctx)
	if err != nil {
		return fmt.Errorf("remote credentials startup pass: listing repositories: %w", err)
	}
	migrated, withCreds := 0, 0
	for _, r := range repos {
		if r.Type != "remote" {
			continue
		}
		cfg, err := e.md.Remote().GetConfig(ctx, r.RepoKey)
		if err != nil {
			if errors.Is(err, metadata.ErrRemoteConfigNotFound) {
				continue // the T-64 crash-window residue; UpdateRepo heals it
			}
			return fmt.Errorf("remote credentials startup pass: %s: %w", r.RepoKey, err)
		}
		if cfg.Password == "" {
			continue
		}
		withCreds++
		if !IsEncrypted(cfg.Password) {
			if e.cipher == nil {
				return fmt.Errorf(
					"remote repository %q stores credentials but no master key is configured: set %s (base64 of exactly 32 bytes) and restart: %w",
					r.RepoKey, CredentialsEnvVar, ErrNoCredentialsKey)
			}
			enc, err := e.cipher.Encrypt(cfg.Password)
			if err != nil {
				return fmt.Errorf("remote credentials startup pass: %s: %w", r.RepoKey, err)
			}
			cfg.Password = enc
			if err := e.md.Remote().UpdateConfig(ctx, cfg); err != nil {
				return fmt.Errorf("remote credentials startup pass: %s: %w", r.RepoKey, err)
			}
			migrated++
			continue
		}
		if e.cipher == nil {
			return fmt.Errorf(
				"remote repository %q stores encrypted credentials but no master key is configured: set %s (base64 of exactly 32 bytes) and restart: %w",
				r.RepoKey, CredentialsEnvVar, ErrNoCredentialsKey)
		}
		if _, _, err := e.cipher.Decrypt(cfg.Password); err != nil {
			return fmt.Errorf("remote repository %q stores an undecryptable credential (wrong %s?): %w",
				r.RepoKey, CredentialsEnvVar, err)
		}
	}
	if migrated > 0 {
		e.log.Info("remote: legacy plaintext credentials encrypted at startup", "count", migrated)
	}
	if withCreds > 0 {
		e.log.Info("remote: credentials verified at startup", "repos", withCreds, "migrated", migrated)
	}
	return nil
}

func (e *Engine) now() time.Time { return e.nowFn().UTC() }

// counters returns (creating) the per-repository counter block.
func (e *Engine) counters(repoKey string) *repoCounters {
	e.mu.Lock()
	defer e.mu.Unlock()
	c := e.stats[repoKey]
	if c == nil {
		c = &repoCounters{}
		e.stats[repoKey] = c
	}
	return c
}

// ---- the six-step flow ----

// Fetch pulls one path through the remote proxy for a read. repoKey must be
// a remote repository and path a service-validated node path; authorization
// is the caller's — the service gates the read BEFORE any upstream contact
// (an unauthorized principal must not be able to aim BinFlow at URLs).
func (e *Engine) Fetch(ctx context.Context, repoKey, path string) (*FetchResult, error) {
	row, cfg, pol, err := e.loadRepo(ctx, repoKey)
	if err != nil {
		return nil, err
	}

	// Step 1: the manual mask (ADR-0012 decision 2's blocked_out column;
	// rest-api.md 1.2 step 6 wording, RepoRejectException's default 404).
	if cfg.BlockedOut {
		return nil, &FetchError{
			Status:  http.StatusNotFound,
			Message: fmt.Sprintf("The repository '%s' is blacked out and cannot serve artifact '%s/%s'.", repoKey, repoKey, path),
			Unfound: true,
		}
	}
	// Step 2: checksum sidecars are never proxied (RE-04 step 2, the M45
	// exact-equality message).
	if isChecksumPath(path) {
		return nil, &FetchError{
			Status:  http.StatusNotFound,
			Message: msgChecksumsNotDownloadable,
			Unfound: true,
		}
	}

	// Steps 3 through 5, retry loop (review B1/B2): EVERY pass re-runs the
	// negative-cache check and the local-copy lookup, so a waitor released
	// by a winner's miss record answers 404 without an upstream contact of
	// its own, and one released by a winner's landing serves the fresh copy
	// (the double-check of repo-semantics 7.3). The upstream is contacted
	// ONLY with the flight held — attempt returns (nil, nil) to mean "a
	// concurrent flight settled, re-run the lookup".
	for {
		// Step 3: the negative cache — a known miss inside its window
		// answers 404 without a single upstream packet.
		if entry, err := e.md.Remote().GetCache(ctx, repoKey, path); err == nil &&
			entry.Kind == cacheKindNegative && entryFresh(entry.ExpiresAt, e.now()) {
			e.counters(repoKey).negatives.Add(1)
			e.logResult(repoKey, path, cacheStateNegative, "", 0, time.Time{}, 0, "negative-cache hit")
			return nil, unfoundMissing(repoKey, path)
		}
		res, aerr := e.attempt(ctx, row, cfg, pol, repoKey, path)
		if res != nil || aerr != nil {
			return res, aerr
		}
	}
}

// attempt runs one pass of steps 4 and 5. A (nil, nil) return means "a
// concurrent flight settled — re-run the full lookup"; every terminal
// outcome — including the waiter's OWN upstream contact, which always
// happens under a held flight — returns a result or an error.
func (e *Engine) attempt(ctx context.Context, row *metadata.Repo, cfg *metadata.RemoteConfig, pol repoPolicy, repoKey, path string) (*FetchResult, error) {
	now := e.now()

	// Step 4: the local copy inside its TTL window.
	node, err := e.md.Nodes().Get(ctx, repoKey, path)
	if err != nil && !errors.Is(err, metadata.ErrNodeNotFound) {
		return nil, fmt.Errorf("remote %s: cache node %s: %w", repoKey, path, err)
	}
	if err == nil && node != nil && node.Sha256 != "" {
		if entry, cerr := e.md.Remote().GetCache(ctx, repoKey, path); cerr == nil && entryFresh(entry.ExpiresAt, now) {
			e.counters(repoKey).hits.Add(1)
			res, serr := e.serveCopy(ctx, node, CacheHit, "")
			e.logResult(repoKey, path, CacheHit, e.upstreamHost(cfg), 0, time.Time{}, 0, "")
			return res, serr
		}
	}

	// Step 5a: the assumed-offline window — zero upstream traffic inside it
	// (M44-4): stale copy, or 404/502 naming the offline state.
	if until, off := e.offlineWindow(repoKey, now); off {
		summary := fmt.Sprintf("assumed offline for another %.0fs", until.Sub(now).Seconds())
		res, derr := e.downgrade(ctx, node, repoKey, path, cfg, pol, summary)
		return res, derr
	}

	// Step 5b: singleflight — one in-flight fetch per (repo, path).
	flightKey := repoKey + "/" + path
	if done, won := e.acquireFlight(flightKey); !won {
		limit := time.Duration(1 << 62)
		if classifyPath(row.PackageType, path) == metadata.RemoteCacheKindMetadata {
			limit = e.metaWait
		}
		switch e.waitFlight(ctx, done, limit) {
		case waitDone:
			return nil, nil // re-run the lookup (repo-semantics 7.3)
		case waitTimeout:
			// metadataRetrievalTimeoutSecs (repo-semantics 7.1): the waiter
			// stops queueing behind a stuck winner. With an old copy
			// standing it is served as-is ("回发旧缓存副本", 7.3); without
			// one there is nothing to fall back to and the miss stands.
			if node != nil && node.Sha256 != "" {
				e.counters(repoKey).stales.Add(1)
				e.logResult(repoKey, path, CacheStale, e.upstreamHost(cfg), 0, time.Time{}, 0,
					"singleflight wait timeout — serving the expired copy")
				return e.serveCopy(ctx, node, CacheStale, "")
			}
			e.logResult(repoKey, path, "", e.upstreamHost(cfg), 0, time.Time{}, 0,
				"singleflight wait timeout without a cached copy")
			return nil, unfoundMissing(repoKey, path)
		default: // the context ended mid-wait
			return nil, fmt.Errorf(
				"remote %s: fetch of %s canceled while waiting for a concurrent fetch: %w",
				repoKey, path, ctx.Err())
		}
	}
	defer e.releaseFlight(flightKey)

	// Winner double-check: state may have moved while the flight was
	// contended (a previous winner's landing, a concurrent invalidate).
	if node2, err := e.md.Nodes().Get(ctx, repoKey, path); err == nil && node2.Sha256 != "" {
		if entry, cerr := e.md.Remote().GetCache(ctx, repoKey, path); cerr == nil && entryFresh(entry.ExpiresAt, now) {
			e.counters(repoKey).hits.Add(1)
			res, serr := e.serveCopy(ctx, node2, CacheHit, "")
			e.logResult(repoKey, path, CacheHit, e.upstreamHost(cfg), 0, time.Time{}, 0, "after flight")
			return res, serr
		}
		node = node2 // the freshest copy reference for the stale arms below
	}

	return e.contactUpstream(ctx, row, cfg, pol, repoKey, path, node)
}

// contactUpstream performs the upstream request and maps its outcome onto
// the RE-04 matrix. staleNode is the expired local copy, if any — the
// expired-but-serving and stale-while-error branches serve it.
func (e *Engine) contactUpstream(ctx context.Context, row *metadata.Repo, cfg *metadata.RemoteConfig, pol repoPolicy, repoKey, path string, staleNode *metadata.Node) (*FetchResult, error) {
	start := time.Now() // wall clock: the log duration, never the injectable TTL clock
	kind := classifyPath(row.PackageType, path)
	// The upstream request path: storage-form unless the protocol's
	// provider rewrites it (goproxy's escape facet, T-285). Cache keys,
	// landed nodes and the singleflight slot all keep the STORAGE path —
	// only the outbound hop sees the wire spelling.
	upPath := upstreamPathFor(row.PackageType, path)
	host := e.upstreamHost(cfg)
	client, err := e.clientFor(repoKey, cfg, pol)
	if err != nil {
		return nil, err
	}

	if kind == metadata.RemoteCacheKindMetadata {
		// Buffered class (packument, simple index, maven-metadata.xml): the
		// 64MB cap applies and an over-limit response is a 502 (NFR-S13
		// point 5) — never an offline mark, the upstream did answer.
		res, ferr := client.Fetch(ctx, Request{Path: upPath})
		if ferr != nil {
			out, merr := e.mapTransportFault(ctx, repoKey, path, cfg, pol, staleNode, ferr)
			return out, merr
		}
		if res.StatusCode != http.StatusOK {
			out, uerr := e.mapUpstreamStatus(ctx, repoKey, path, cfg, pol, staleNode, res.StatusCode, res.Status)
			e.logResult(repoKey, path, cacheStateOf(out), host, res.StatusCode, start, 0, "")
			return out, uerr
		}
		out, landErr := e.land(ctx, repoKey, path, kind, cfg, pol, bytes.NewReader(res.Body), res.Header)
		if landErr != nil {
			return nil, landErr
		}
		e.logResult(repoKey, path, CacheMiss, host, res.StatusCode, start, int64(len(res.Body)), "")
		return out, nil
	}

	// Streaming class (artifacts): unbounded and byte-counted (FR-20-AC11 —
	// a 1GB body must cross with a flat heap).
	res, ferr := client.Stream(ctx, Request{Path: upPath})
	if ferr != nil {
		out, merr := e.mapTransportFault(ctx, repoKey, path, cfg, pol, staleNode, ferr)
		return out, merr
	}
	if res.StatusCode != http.StatusOK {
		drainClose(res.Body)
		out, uerr := e.mapUpstreamStatus(ctx, repoKey, path, cfg, pol, staleNode, res.StatusCode, res.Status)
		e.logResult(repoKey, path, cacheStateOf(out), host, res.StatusCode, start, 0, "")
		return out, uerr
	}
	out, landErr := e.land(ctx, repoKey, path, kind, cfg, pol, res.Body, res.Header)
	if landErr != nil {
		_ = res.Body.Close()
		if errors.Is(landErr, errUpstreamBody) {
			// Mid-body transport failure: the session is aborted inside
			// land; it is an upstream fault like any other.
			out2, merr := e.mapTransportFault(ctx, repoKey, path, cfg, pol, staleNode, landErr)
			if out2 != nil {
				return out2, nil
			}
			return nil, merr
		}
		// A LOCAL write failure (commit, ledger/node/cache rows): never
		// blamed on the upstream — no offline mark, no stale downgrade, an
		// honest 500 (review side-fix).
		return nil, landErr
	}
	_ = res.Body.Close()
	e.logResult(repoKey, path, CacheMiss, host, res.StatusCode, start, out.Node.Size, "")
	return out, nil
}

// land streams the upstream body through a storage session and lands the
// metadata blob-first (architecture section 4.5: the same commit protocol as
// a local upload, so the landed bytes and the served response are
// bit-for-bit identical and the node's digests are the measured ones).
func (e *Engine) land(ctx context.Context, repoKey, path, kind string, cfg *metadata.RemoteConfig, pol repoPolicy, body io.Reader, hdr http.Header) (*FetchResult, error) {
	now := e.now()
	sess, err := e.st.BeginSession(ctx)
	if err != nil {
		return nil, fmt.Errorf("remote %s: begin cache session: %w", repoKey, err)
	}
	written, err := sess.Append(ctx, body)
	if err != nil {
		_ = sess.Abort(context.Background())
		// The marker routes this into the upstream-fault arm (offline mark,
		// stale service); without it a local failure would poison the
		// repository's upstream state (review side-fix).
		return nil, fmt.Errorf("remote %s: stream upstream body: %w: %w", repoKey, err, errUpstreamBody)
	}
	// M3 registers upstream X-Checksum-* without enforcing (PRD v1.2: the
	// four-value policy is M4; architecture section 4.5's strict
	// Commit(expect) posture is superseded by that ruling — noted in the
	// T-66 report for the architect).
	ref, err := sess.Commit(ctx, storage.BlobRef{})
	if err != nil {
		_ = sess.Abort(context.Background())
		return nil, fmt.Errorf("remote %s: commit upstream body: %w", repoKey, err)
	}

	// Blob-first (architecture section 3.2): the ledger row, then the node.
	if err := e.md.Blobs().Put(ctx, &metadata.Blob{
		Sha256: ref.Sha256, Sha1: ref.Sha1, Md5: ref.Md5, Size: ref.Size, CreatedAt: rfc3339(now),
	}); err != nil {
		return nil, fmt.Errorf("remote %s: cache blob row %s: %w", repoKey, ref.Sha256, err)
	}

	// The node keeps first-seen provenance on a same-digest refetch (the
	// idempotent-retransmit rule of repo-semantics section 3).
	existing, gerr := e.md.Nodes().Get(ctx, repoKey, path)
	node := &metadata.Node{
		RepoKey: repoKey, Path: path, Sha256: ref.Sha256, Size: ref.Size,
		Mime: mimeOf(hdr), CreatedBy: "remote-proxy",
		CreatedAt: rfc3339(now), UpdatedAt: rfc3339(now),
	}
	if gerr == nil && existing != nil {
		if existing.Sha256 == ref.Sha256 {
			node.CreatedBy = existing.CreatedBy
			node.CreatedAt = existing.CreatedAt
		}
		node.Mime = firstNonEmpty(mimeOf(hdr), existing.Mime)
	}
	if err := e.md.Nodes().Put(ctx, node); err != nil {
		return nil, fmt.Errorf("remote %s: cache node %s: %w", repoKey, path, err)
	}

	// Validators + TTL clock (dual TTL by class; the kind is recomputed per
	// fetch so a provider registration change re-classifies on refetch).
	ttl := ttlFor(kind, cfg.ContentTTLSeconds, cfg.MetadataTTLSeconds, pol.MissedRetrievalCachePeriodSecs)
	if err := e.md.Remote().PutCache(ctx, contentEntry(repoKey, path,
		hdr.Get("ETag"), hdr.Get("Last-Modified"), kind, now, ttl)); err != nil {
		return nil, fmt.Errorf("remote %s: cache state %s: %w", repoKey, path, err)
	}

	// Original-checksum registration (M3: log only, never reject).
	if declared := hdr.Get("X-Checksum-Sha256"); declared != "" && !strings.EqualFold(declared, ref.Sha256) {
		e.log.Warn("remote: upstream declared checksum differs from the measured one (registered, not enforced)",
			"repo", repoKey, "path", path, "declared", declared, "actual", ref.Sha256)
	}

	bodyRC, _, err := e.st.Open(ctx, ref.Sha256)
	if err != nil {
		return nil, fmt.Errorf("remote %s: open landed blob %s: %w", repoKey, ref.Sha256, err)
	}
	seekable, ok := bodyRC.(io.ReadSeekCloser)
	if !ok {
		_ = bodyRC.Close()
		return nil, fmt.Errorf("remote %s: open landed blob %s: storage backend does not support Seek", repoKey, ref.Sha256)
	}
	c := e.counters(repoKey)
	c.misses.Add(1)
	c.bytes.Add(written)
	return &FetchResult{Node: node, Body: hinted(seekable, CacheMiss, ""), CacheState: CacheMiss, HasCopy: true}, nil
}

// mapUpstreamStatus maps a definite non-200 upstream answer: 404 (negative
// cache + expired-but-serving), 401/403 (unfound with the upstream summary),
// other 4xx (unfound with the summary), 5xx (offline mark + downgrade).
func (e *Engine) mapUpstreamStatus(ctx context.Context, repoKey, path string, cfg *metadata.RemoteConfig, pol repoPolicy, staleNode *metadata.Node, status int, statusText string) (*FetchResult, error) {
	now := e.now()
	switch {
	case status == http.StatusNotFound:
		// Negative cache + expired-but-serving (repo-semantics 7.2 step 5).
		// The six-step order puts the negative check BEFORE the local copy,
		// so the next request inside the window answers 404 even though a
		// copy exists — the literal PRD/RE-04 reading, pinned by test.
		ttl := ttlFor(cacheKindNegative, 0, 0, pol.MissedRetrievalCachePeriodSecs)
		if err := e.md.Remote().PutCache(ctx, negativeEntry(repoKey, path, now, ttl)); err != nil {
			return nil, fmt.Errorf("remote %s: negative cache %s: %w", repoKey, path, err)
		}
		if staleNode != nil {
			e.counters(repoKey).stales.Add(1)
			return e.serveStale(ctx, staleNode, "upstream 404 (expired copy served)")
		}
		return nil, unfoundMissing(repoKey, path)

	case status == http.StatusUnauthorized || status == http.StatusForbidden:
		// Credentials refused upstream: the resource is unfound and the
		// message carries the upstream summary (repo-semantics 7.6). No
		// negative cache — credential state is correctable without a
		// restart — and no offline mark: the upstream is healthy.
		return nil, &FetchError{
			Status: http.StatusNotFound,
			Message: fmt.Sprintf("Failed to find the requested resource '%s/%s' (upstream answered %d %s; credentials refused or insufficient).",
				repoKey, path, status, statusText),
			Unfound: true,
		}

	case status >= 400 && status < 500:
		// Other 4xx: a definite answer, not a fault. PRD's offline matrix
		// lists only 5xx/timeout; BinFlow answers unfound with the summary
		// (repo-semantics 7.6's "other 4xx/5xx" row is medium confidence —
		// flagged as a decision point in the T-66 report).
		return nil, &FetchError{
			Status: http.StatusNotFound,
			Message: fmt.Sprintf("Failed to find the requested resource '%s/%s' (upstream answered %d %s).",
				repoKey, path, status, statusText),
			Unfound: true,
		}

	case status == http.StatusNotModified:
		// An unsolicited 304 (M3 sends no conditional headers yet — the
		// HEAD+validators negotiation is P1): with an expired copy standing
		// it simply refreshes the clock; without one it is upstream
		// nonsense and a 502, never an offline mark.
		if staleNode != nil {
			// Keep the standing entry's kind AND validators (the P1
			// conditional negotiation will need the ETag/Last-Modified —
			// a 304 carries no new ones) and slide the expiry forward one
			// full TTL (review side-fix: validators were being dropped).
			kind, etag, lastMod := metadata.RemoteCacheKindContent, "", ""
			if entry, gerr := e.md.Remote().GetCache(ctx, repoKey, path); gerr == nil && entry.Kind != "" {
				kind, etag, lastMod = entry.Kind, entry.ETag, entry.LastModified
			}
			ttl := ttlFor(kind, cfg.ContentTTLSeconds, cfg.MetadataTTLSeconds, pol.MissedRetrievalCachePeriodSecs)
			if err := e.md.Remote().PutCache(ctx, contentEntry(repoKey, path,
				etag, lastMod, kind, now, ttl)); err != nil {
				return nil, fmt.Errorf("remote %s: refresh cache state %s: %w", repoKey, path, err)
			}
			e.counters(repoKey).hits.Add(1)
			res, serr := e.serveCopy(ctx, staleNode, CacheRevalidated, "")
			return res, serr
		}
		return nil, &FetchError{
			Status:  http.StatusBadGateway,
			Message: fmt.Sprintf("Upstream answered 304 Not Modified for '%s/%s' without a cached copy.", repoKey, path),
		}

	default: // 5xx and anything else anomalous
		e.markOffline(repoKey, now.Add(time.Duration(offlineSecs(pol))*time.Second))
		summary := fmt.Sprintf("upstream %d %s", status, statusText)
		return e.downgrade(ctx, staleNode, repoKey, path, cfg, pol, summary)
	}
}

// mapTransportFault maps client failures (connection refused/reset/timeout,
// redirect excess, guarded dials, mid-body aborts): mark offline, then serve
// a stale copy or answer 404/502. SSRF denials and the buffered-body cap are
// NOT faults — they map to 400/502 directly with no offline mark (a
// screening refusal must not silence the repository's other paths).
func (e *Engine) mapTransportFault(ctx context.Context, repoKey, path string, cfg *metadata.RemoteConfig, pol repoPolicy, staleNode *metadata.Node, err error) (*FetchResult, error) {
	now := e.now()
	var rej *RejectionError
	switch {
	case errors.As(err, &rej):
		return nil, &FetchError{
			Status: http.StatusBadRequest,
			Message: fmt.Sprintf("Cannot fetch '%s/%s': upstream target refused — private or suppressed upstream (%v)",
				repoKey, path, err),
		}
	case errors.Is(err, ErrBodyTooLarge):
		return nil, &FetchError{
			Status:  http.StatusBadGateway,
			Message: fmt.Sprintf("Failed to proxy '%s/%s': %v", repoKey, path, err),
		}
	default:
		e.markOffline(repoKey, now.Add(time.Duration(offlineSecs(pol))*time.Second))
		return e.downgrade(ctx, staleNode, repoKey, path, cfg, pol, fmt.Sprintf("upstream unavailable: %v", err))
	}
}

// downgrade answers a fault with the stale-first policy: a copy — fresh
// copies cannot reach this branch, so always an expired one — is served with
// X-Binflow-Upstream-Error (STALE); without one, 404 naming the offline
// state, or 502 under hardFail (the T-79 errata: hardFail changes only the
// no-copy outcome).
func (e *Engine) downgrade(ctx context.Context, node *metadata.Node, repoKey, path string, cfg *metadata.RemoteConfig, pol repoPolicy, summary string) (*FetchResult, error) {
	host := e.upstreamHost(cfg)
	if node != nil && node.Sha256 != "" {
		e.counters(repoKey).stales.Add(1)
		e.logResult(repoKey, path, CacheStale, host, 0, time.Time{}, 0, summary)
		return e.serveStale(ctx, node, summary)
	}
	e.logResult(repoKey, path, "", host, 0, time.Time{}, 0, summary)
	if pol.HardFail {
		return nil, &FetchError{
			Status:  http.StatusBadGateway,
			Message: fmt.Sprintf("Upstream '%s' failed for '%s/%s' (hardFail enabled): %s.", host, repoKey, path, summary),
		}
	}
	return nil, &FetchError{
		Status: http.StatusNotFound,
		Message: fmt.Sprintf("Failed to find the requested resource '%s/%s': upstream %s is assumed offline (no cached copy; retry later).",
			repoKey, path, host),
		Unfound: true,
	}
}

// serveCopy opens the cached blob and wraps it with the response hints.
func (e *Engine) serveCopy(ctx context.Context, node *metadata.Node, state, upstreamError string) (*FetchResult, error) {
	body, _, err := e.st.Open(ctx, node.Sha256)
	if err != nil {
		return nil, fmt.Errorf("remote %s: open cached blob %s: %w", node.RepoKey, node.Sha256, err)
	}
	seekable, ok := body.(io.ReadSeekCloser)
	if !ok {
		_ = body.Close()
		return nil, fmt.Errorf("remote %s: open cached blob %s: storage backend does not support Seek", node.RepoKey, node.Sha256)
	}
	return &FetchResult{
		Node: node, Body: hinted(seekable, state, upstreamError),
		CacheState: state, UpstreamError: upstreamError, HasCopy: true,
	}, nil
}

// serveStale serves an expired copy with the upstream-error header.
func (e *Engine) serveStale(ctx context.Context, node *metadata.Node, summary string) (*FetchResult, error) {
	return e.serveCopy(ctx, node, CacheStale, summary)
}

// ---- cache invalidation (RE-06) ----

// Invalidate drops the local cache of one path — the node row(s) and the
// remote_cache entry — without any upstream contact. It reports whether
// anything was cached (the service's 204-vs-404 discriminator). A folder
// path drops the subtree's nodes and each dropped node's cache row;
// negative rows of paths that never had a node cannot be enumerated by
// prefix (the store exposes no such query) — deleting the FOLDER's own cache
// row and lingering child negatives expire on their own TTL, harmless: the
// next miss rewrites them.
func (e *Engine) Invalidate(ctx context.Context, repoKey, path string) (bool, error) {
	existed := false
	if strings.HasSuffix(path, "/") {
		dir := strings.TrimSuffix(path, "/")
		nodes, err := e.md.Nodes().ListByPrefix(ctx, repoKey, dir)
		if err != nil {
			return false, fmt.Errorf("remote %s: list cache under %s: %w", repoKey, path, err)
		}
		for _, n := range nodes {
			if n.Path != path && !strings.HasPrefix(n.Path, dir+"/") {
				continue // the exact-arm same-named file, mirroring service.Delete
			}
			if err := e.md.Nodes().Delete(ctx, repoKey, n.Path); err != nil && !errors.Is(err, metadata.ErrNodeNotFound) {
				return existed, fmt.Errorf("remote %s: delete cache node %s: %w", repoKey, n.Path, err)
			}
			if err := e.md.Remote().DeleteCache(ctx, repoKey, n.Path); err != nil && !errors.Is(err, metadata.ErrRemoteCacheNotFound) {
				return existed, fmt.Errorf("remote %s: delete cache state %s: %w", repoKey, n.Path, err)
			}
			existed = true
		}
		return existed, nil
	}
	if n, err := e.md.Nodes().Get(ctx, repoKey, path); err == nil && n != nil {
		if err := e.md.Nodes().Delete(ctx, repoKey, path); err != nil && !errors.Is(err, metadata.ErrNodeNotFound) {
			return false, fmt.Errorf("remote %s: delete cache node %s: %w", repoKey, path, err)
		}
		existed = true
	}
	if err := e.md.Remote().DeleteCache(ctx, repoKey, path); err == nil {
		existed = true
	} else if !errors.Is(err, metadata.ErrRemoteCacheNotFound) {
		return existed, fmt.Errorf("remote %s: delete cache state %s: %w", repoKey, path, err)
	}
	return existed, nil
}

// Forget drops every in-process trace of one repository (outbound client
// pool, assumed-offline window, counters) — the DeleteRepo teardown hook.
func (e *Engine) Forget(repoKey string) {
	e.mu.Lock()
	c := e.clients[repoKey]
	delete(e.clients, repoKey)
	delete(e.offline, repoKey)
	delete(e.stats, repoKey)
	e.mu.Unlock()
	if c != nil {
		c.client.CloseIdleConnections()
	}
}

// Stats snapshots one repository's RE-11 counters plus its current cache
// footprint (node count and bytes from the store).
func (e *Engine) Stats(ctx context.Context, repoKey string) (RepoStats, error) {
	e.mu.Lock()
	c := e.stats[repoKey]
	e.mu.Unlock()
	out := RepoStats{RepoKey: repoKey}
	if c != nil {
		out.Hits = c.hits.Load()
		out.Misses = c.misses.Load()
		out.Negatives = c.negatives.Load()
		out.Stales = c.stales.Load()
		out.UpstreamBytes = c.bytes.Load()
	}
	nodes, err := e.md.Nodes().ListByPrefix(ctx, repoKey, "")
	if err != nil {
		return out, fmt.Errorf("remote %s: stats listing: %w", repoKey, err)
	}
	for _, n := range nodes {
		out.CachedNodes++
		out.CachedBytes += n.Size
	}
	return out, nil
}

// ---- internals ----

// loadRepo resolves the repository row, its remote_configs row and the
// policy fields of the canonical config JSON.
func (e *Engine) loadRepo(ctx context.Context, repoKey string) (*metadata.Repo, *metadata.RemoteConfig, repoPolicy, error) {
	row, err := e.md.Repos().Get(ctx, repoKey)
	if err != nil {
		if errors.Is(err, metadata.ErrRepoNotFound) {
			return nil, nil, repoPolicy{}, &FetchError{
				Status:  http.StatusNotFound,
				Message: fmt.Sprintf("Failed to find the repository '%s' specified in the request.", repoKey),
				Unfound: true,
			}
		}
		return nil, nil, repoPolicy{}, fmt.Errorf("remote %s: load repository: %w", repoKey, err)
	}
	if row.Type != "remote" {
		return nil, nil, repoPolicy{}, fmt.Errorf("remote %s: repository is %q, not remote", repoKey, row.Type)
	}
	cfg, err := e.md.Remote().GetConfig(ctx, repoKey)
	if err != nil {
		if errors.Is(err, metadata.ErrRemoteConfigNotFound) {
			return nil, nil, repoPolicy{}, fmt.Errorf(
				"remote %s: no remote_configs row (the create crash window; update the repository config to heal): %w", repoKey, err)
		}
		return nil, nil, repoPolicy{}, fmt.Errorf("remote %s: load config: %w", repoKey, err)
	}
	pol := defaultPolicy
	if row.Config != "" {
		if err := json.Unmarshal([]byte(row.Config), &pol); err != nil {
			return nil, nil, repoPolicy{}, fmt.Errorf("remote %s: config policy: %w", repoKey, err)
		}
		// Zero fields keep the defaults (explicit-zero-is-default matches
		// the create-time rule T-64 pinned).
		if pol.MissedRetrievalCachePeriodSecs == 0 {
			pol.MissedRetrievalCachePeriodSecs = defaultPolicy.MissedRetrievalCachePeriodSecs
		}
		if pol.SocketTimeoutSecs == 0 {
			pol.SocketTimeoutSecs = defaultPolicy.SocketTimeoutSecs
		}
		if pol.AssumedOfflinePeriodSecs == 0 {
			pol.AssumedOfflinePeriodSecs = defaultPolicy.AssumedOfflinePeriodSecs
		}
	}
	return row, cfg, pol, nil
}

// clientFor returns the repository's outbound client, rebuilding it when the
// policy signature changed (a config update) and pooling otherwise. The
// decrypted password lives in the signature and the client only — never in a
// log line (NFR-S14).
func (e *Engine) clientFor(repoKey string, cfg *metadata.RemoteConfig, pol repoPolicy) (*Client, error) {
	password, _, err := e.cipher.Decrypt(cfg.Password)
	if err != nil {
		return nil, fmt.Errorf("remote %s: credentials: %w", repoKey, err)
	}
	sig := strings.Join([]string{
		cfg.URL, cfg.Username, password,
		fmt.Sprintf("%d", pol.SocketTimeoutSecs), fmt.Sprintf("%t", cfg.AllowPrivateUpstream),
	}, "\x00")
	e.mu.Lock()
	cached := e.clients[repoKey]
	e.mu.Unlock()
	if cached != nil && cached.sig == sig {
		return cached.client, nil
	}
	client, err := NewClient(Options{
		RepoKey:              repoKey,
		BaseURL:              cfg.URL,
		Username:             cfg.Username,
		Password:             password,
		AllowPrivateUpstream: cfg.AllowPrivateUpstream,
		SocketTimeout:        time.Duration(pol.SocketTimeoutSecs) * time.Second,
		Logger:               e.log,
		Resolve:              e.resolve,
	})
	if err != nil {
		return nil, fmt.Errorf("remote %s: outbound client: %w", repoKey, err)
	}
	e.mu.Lock()
	old := e.clients[repoKey]
	e.clients[repoKey] = &cachedClient{sig: sig, client: client}
	e.mu.Unlock()
	if old != nil {
		old.client.CloseIdleConnections()
	}
	return client, nil
}

// markOffline opens the assumed-offline window (the light circuit breaker of
// the T-79 errata: silence for assumedOfflinePeriodSecs, zero upstream
// traffic inside it, automatic recovery after).
func (e *Engine) markOffline(repoKey string, until time.Time) {
	e.mu.Lock()
	e.offline[repoKey] = until
	e.mu.Unlock()
	e.log.Warn("remote: upstream fault — repository assumed offline",
		"repo", repoKey, "until", rfc3339(until))
}

// offlineWindow reports the remaining assumed-offline window, if any.
func (e *Engine) offlineWindow(repoKey string, now time.Time) (time.Time, bool) {
	e.mu.Lock()
	defer e.mu.Unlock()
	until, ok := e.offline[repoKey]
	if !ok {
		return time.Time{}, false
	}
	if !now.Before(until) {
		delete(e.offline, repoKey)
		return time.Time{}, false
	}
	return until, true
}

// acquireFlight is the singleflight entry: the winner gets won=true and owns
// the fetch; waitors get the completion channel.
func (e *Engine) acquireFlight(key string) (done chan struct{}, won bool) {
	e.mu.Lock()
	defer e.mu.Unlock()
	if c, ok := e.flights[key]; ok {
		return c, false
	}
	c := make(chan struct{})
	e.flights[key] = c
	return c, true
}

// releaseFlight closes the completion channel and clears the slot.
func (e *Engine) releaseFlight(key string) {
	e.mu.Lock()
	c := e.flights[key]
	delete(e.flights, key)
	e.mu.Unlock()
	if c != nil {
		close(c)
	}
}

// waitFlight blocks until the winner finishes, the context ends or the
// metadata wait cap elapses (repo-semantics 7.1's 60s lock timeout). The
// return names WHICH event ended the wait: the caller serves the fallback
// copy on a timeout and re-runs the lookup when the winner settled.
func (e *Engine) waitFlight(ctx context.Context, done chan struct{}, limit time.Duration) waitReason {
	var timer <-chan time.Time
	if limit > 0 {
		t := time.NewTimer(limit)
		defer t.Stop()
		timer = t.C
	}
	select {
	case <-done:
		return waitDone
	case <-ctx.Done():
		return waitCanceled
	case <-timer:
		return waitTimeout
	}
}

// logResult emits the per-fetch outcome line with the section 6.3 fields
// (upstream_host / cache_result / upstream_status / upstream_duration_ms).
// Pure cache hits log at debug (hot path); everything upstream-touching at
// info. Credentials never appear (NFR-S14): host names and statuses only.
func (e *Engine) logResult(repoKey, path, cacheState, host string, status int, start time.Time, bytes int64, note string) {
	attrs := []any{
		slog.String("repo", repoKey),
		slog.String("path", path),
		slog.String("upstream_host", host),
		slog.String("cache_result", cacheState),
		slog.Int("upstream_status", status),
	}
	if !start.IsZero() {
		attrs = append(attrs, slog.Int64("upstream_duration_ms", time.Since(start).Milliseconds()))
	}
	if bytes > 0 {
		attrs = append(attrs, slog.Int64("bytes", bytes))
	}
	if note != "" {
		attrs = append(attrs, slog.String("note", note))
	}
	if cacheState == CacheHit {
		e.log.Debug("remote: fetch served from cache", attrs...)
		return
	}
	e.log.Info("remote: fetch", attrs...)
}

// upstreamHost extracts host[:port] from the configured URL (log field).
func (e *Engine) upstreamHost(cfg *metadata.RemoteConfig) string {
	if cfg == nil || cfg.URL == "" {
		return ""
	}
	if u, err := url.Parse(cfg.URL); err == nil {
		return u.Host
	}
	return ""
}

// unfoundMissing is the plain not-found wording (uniform with the local
// content plane's download-side 404 message).
func unfoundMissing(repoKey, path string) *FetchError {
	return &FetchError{
		Status:  http.StatusNotFound,
		Message: fmt.Sprintf("Failed to find the requested resource '%s/%s'.", repoKey, path),
		Unfound: true,
	}
}

// offlineSecs resolves the assumed-offline window with the default fallback.
func offlineSecs(pol repoPolicy) int64 {
	if pol.AssumedOfflinePeriodSecs > 0 {
		return pol.AssumedOfflinePeriodSecs
	}
	return defaultPolicy.AssumedOfflinePeriodSecs
}

// mimeOf picks the upstream Content-Type with the generic default.
func mimeOf(hdr http.Header) string {
	if ct := hdr.Get("Content-Type"); ct != "" {
		return ct
	}
	return "application/octet-stream"
}

func firstNonEmpty(a, b string) string {
	if a != "" {
		return a
	}
	return b
}

// drainClose discards and closes a non-200 streamed body so the pooled
// connection survives.
func drainClose(body io.ReadCloser) {
	_, _ = io.Copy(io.Discard, io.LimitReader(body, 8<<10))
	_ = body.Close()
}

// cacheStateOf extracts the state token of a result for logging.
func cacheStateOf(res *FetchResult) string {
	if res == nil {
		return ""
	}
	return res.CacheState
}
