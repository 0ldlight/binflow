package license

import (
	"context"
	"crypto/ed25519"
	"encoding/json"
	"errors"
	"fmt"
	"log/slog"
	"strings"
	"sync"
	"sync/atomic"
	"time"

	"github.com/lzwzzy/binflow/internal/audit"
	"github.com/lzwzzy/binflow/internal/metadata"
)

// Audit action names of the license plane (ADR-0032 decision 4). They live
// here — the license package owns the vocabulary — and are stored by the
// audit trail verbatim; adding them to audit.Actions()' picker list is the
// audit package owner's one-liner.
const (
	// ActionInstall records a successful license installation. Detail
	// carries {tier, licensee, licenseId, expiresAt}.
	ActionInstall = "license.install"
	// ActionDelete records a license uninstall. Detail carries the removed
	// license's {tier, licensee, licenseId, expiresAt}.
	ActionDelete = "license.delete"
	// ActionInvalid records a rejected or failed verification: a refused
	// install, or a stored row that stopped verifying at startup/ticker
	// time. Detail carries {reason} (a category word, never document
	// content) and, when the row was readable, {licenseId, tier}.
	ActionInvalid = "license.invalid"
)

// actorSystem is the actor recorded for license.invalid events raised by
// the startup/ticker paths — no principal is involved in those transitions.
const actorSystem = "system"

// CoreAddonIDs are the five core package-type addon ids (generic, docker,
// maven, npm, pypi — ADR-0033 decision 4's retro-fit). Naming them here as
// plain strings keeps license independent of the addons package; the set
// exists only to shape the addons.disabled WARN (a core id in the disable
// list is a circuit breaker, not a licensing decision — allowed but loud).
var CoreAddonIDs = []string{"generic", "docker", "maven", "npm", "pypi"}

// Store is the persistence seam the Manager consumes (consumer-side
// interface, satisfied by metadata.Store's Licenses() sub-store; the single
// licenses row is the fact source the Manager re-verifies from).
type Store interface {
	// GetLicense returns the stored license row, or (nil, nil) when no
	// license is installed.
	GetLicense(ctx context.Context) (*metadata.LicenseRecord, error)
	// PutLicense atomically replaces the single row (the install
	// contract: the previous license survives until the new one verifies
	// and lands — no intermediate state).
	PutLicense(ctx context.Context, rec *metadata.LicenseRecord) error
	// DeleteLicense removes the row; idempotent (deleting an empty table
	// is the community floor already in force).
	DeleteLicense(ctx context.Context) error
}

// State is the effective license state snapshot. It is an immutable value:
// the Manager hands out copies, so a reader can never observe a half-write
// (Install/Uninstall swap one atomic pointer; FR-85.2 no-tearing).
type State struct {
	// Licensed reports whether a license document is installed (an
	// installed-but-expired document keeps Licensed=true with a degraded
	// Tier — the row is a fact, the expiry is a verdict).
	Licensed bool
	// Tier is the EFFECTIVE tier: the document's tier while its validity
	// window covers the clock, TierCommunity otherwise (D6 expiry and the
	// not-yet-valid symmetrical guard — NFR-S53: no unverifiable high
	// tier).
	Tier Tier
	// Expired marks an installed document whose expiresAt the clock has
	// already passed (the observable difference between "never licensed"
	// and "licensed but expired").
	Expired        bool
	LicenseID      string
	Licensee       string
	IssuedAt       time.Time
	NotBefore      time.Time
	ExpiresAt      time.Time // zero = perpetual
	Perpetual      bool
	AddonAllowlist []string // nil/empty = tier-wide unlock
	Limits         json.RawMessage
}

// Options configures the Manager. VerifyKeys and Store are required; every
// other field has a production default with a test-injection seam.
type Options struct {
	// Store persists the license row (required).
	Store Store
	// VerifyKeys is the kid -> public key table (required, non-empty;
	// production assembly passes EmbeddedVerifyKeys(), tests inject their
	// own pairs — the ADR-mandated seam, never a build tag).
	VerifyKeys map[string]ed25519.PublicKey
	// DisabledCSV is the config addons.disabled value (CSV of addon ids;
	// empty = nothing disabled). T-283 wires the config key; the parse
	// and the core-id WARN live here so the semantics have one owner.
	DisabledCSV string
	// Audit records license.invalid events raised by the Manager (nil =
	// silent; install/delete events are the HTTP layer's — it owns the
	// actor).
	Audit audit.Recorder
	// Log is the structured logger (nil = slog.Default()). Log lines
	// never carry the licensee, the document or signature bytes
	// (NFR-S52).
	Log *slog.Logger
	// Now is the clock (nil = time.Now). Injected by tests; production
	// trusts the server's local UTC clock (ADR-0032: no callhome, no NTP
	// cross-check).
	Now func() time.Time
	// TickEvery is the re-evaluation interval of Run (0 = 24h, the
	// ADR's daily ticker; tests shrink it).
	TickEvery time.Duration
}

// Manager owns the installed-license state: the single point every gate
// consults (Install/Uninstall/State/AddonEnabled/PackageTypeAvailable,
// ADR-0032). It is safe for concurrent use.
type Manager struct {
	store    Store
	keys     map[string]ed25519.PublicKey
	disabled map[string]struct{}
	audit    audit.Recorder
	log      *slog.Logger
	now      func() time.Time
	tick     time.Duration

	mu    sync.Mutex // serializes Install/Uninstall (single-row writers)
	state atomic.Pointer[State]
}

// New builds a Manager on the community floor (no license installed). Call
// Load before serving so a stored license re-enters the state — New itself
// does no I/O.
func New(opts Options) (*Manager, error) {
	if opts.Store == nil {
		return nil, fmt.Errorf("license: Options.Store is required")
	}
	if len(opts.VerifyKeys) == 0 {
		return nil, fmt.Errorf("license: Options.VerifyKeys is required (assembly passes EmbeddedVerifyKeys)")
	}
	log := opts.Log
	if log == nil {
		log = slog.Default()
	}
	now := opts.Now
	if now == nil {
		now = func() time.Time { return time.Now().UTC() }
	}
	tick := opts.TickEvery
	if tick <= 0 {
		tick = 24 * time.Hour
	}
	m := &Manager{
		store: opts.Store,
		keys:  opts.VerifyKeys,
		audit: opts.Audit,
		log:   log,
		now:   now,
		tick:  tick,
	}
	m.state.Store(floorState())
	m.disabled = parseDisabledCSV(opts.DisabledCSV)
	for _, id := range CoreAddonIDs {
		if _, hit := m.disabled[id]; hit {
			// Circuit breaker on a core package type: legal (the operator's
			// degradation knob) but loud — five times out of five this is
			// either an outage tool or a typo'd id.
			log.Warn("license: addons.disabled includes a core package type (effective immediately)",
				"addon", id)
		}
	}
	return m, nil
}

// Load reads the stored row and re-runs the verification chain (the startup
// path, section 15.1.2). A row that no longer verifies — key rotation, an
// externally tampered doc — degrades to the community floor WITH a WARN and
// a license.invalid audit event, never an error: an unverifiable license
// must not block startup nor keep a high tier (NFR-S53 / FR-84-AC6). Only a
// store failure is returned as an error.
func (m *Manager) Load(ctx context.Context) error {
	return m.refresh(ctx, true)
}

// Run blocks re-evaluating the stored document on the daily ticker until
// ctx is done (the expiry crossing is a runtime event — D6: the downgrade
// needs no restart). Callers run it in one goroutine next to the server.
func (m *Manager) Run(ctx context.Context) {
	t := time.NewTicker(m.tick)
	defer t.Stop()
	for {
		select {
		case <-ctx.Done():
			return
		case <-t.C:
			_ = m.refresh(ctx, false)
		}
	}
}

// Install verifies doc through the full chain and, only on success,
// atomically replaces the stored row and swaps the state snapshot (D7: a
// failed install leaves the current license fully in force). The error
// wraps one of the VerifyDocument sentinels or a store failure; it never
// carries document content.
func (m *Manager) Install(ctx context.Context, doc string) (State, error) {
	m.mu.Lock()
	defer m.mu.Unlock()

	d, err := VerifyDocument(doc, m.keys, m.now())
	if err != nil {
		return State{}, err
	}
	rec := &metadata.LicenseRecord{
		LicenseID:   d.LicenseID,
		Tier:        d.Tier.String(),
		Licensee:    d.Licensee,
		Doc:         strings.TrimSpace(doc),
		IssuedAt:    d.IssuedAt.Format(time.RFC3339),
		NotBefore:   d.NotBefore.Format(time.RFC3339),
		ExpiresAt:   expiryText(d),
		InstalledAt: m.now().Format(time.RFC3339),
	}
	if err := m.store.PutLicense(ctx, rec); err != nil {
		return State{}, fmt.Errorf("license: persisting document: %w", err)
	}
	m.state.Store(stateFromDocument(d))
	return m.State(), nil
}

// Uninstall removes the stored row and drops to the community floor
// (immediate, idempotent — uninstalling an empty table is a no-op that
// keeps the floor). Existing artifacts stay readable: the floor releases
// nothing into the data plane (D1).
func (m *Manager) Uninstall(ctx context.Context) error {
	m.mu.Lock()
	defer m.mu.Unlock()
	if err := m.store.DeleteLicense(ctx); err != nil {
		return fmt.Errorf("license: removing document: %w", err)
	}
	m.state.Store(floorState())
	return nil
}

// State returns the effective snapshot. The read is lock-free; expiry is
// evaluated against the injected clock at read time so a tier whose window
// the clock has already passed is never observed as active, even between
// ticks (the ticker still owns the logged/audited downgrade transition).
func (m *Manager) State() State {
	st := *m.state.Load()
	if !st.Licensed {
		return st
	}
	now := m.now()
	if now.Before(st.NotBefore.Add(-clockLeeway)) {
		return degradedState(st, false)
	}
	if !st.Perpetual && now.After(st.ExpiresAt.Add(clockLeeway)) {
		return degradedState(st, true)
	}
	return st
}

// AddonEnabled is the single evaluation point of the tier x addon unlock
// matrix (section 15.2.3): disabled-config hit -> false; effective tier
// below minTier -> false; explicit allowlist present and not naming id ->
// false; otherwise true. The community floor ignores the allowlist — a
// floor-tier addon is unlocked even when a license narrows its own
// allowlist.
//
// id and minTier are primitives on purpose: license must not import the
// addons package (ADR-0033 dependency direction).
func (m *Manager) AddonEnabled(_ context.Context, id string, minTier Tier) bool {
	if _, off := m.disabled[id]; off {
		return false
	}
	st := m.State()
	if st.Tier < minTier {
		return false
	}
	if minTier <= TierCommunity {
		return true
	}
	return len(st.AddonAllowlist) == 0 || containsID(st.AddonAllowlist, id)
}

// PackageTypeAvailable is the repo-create gate's spelling of the same
// evaluation (weave point 2, section 15.1.5): for a package-type addon the
// addon id IS the package type, so the question reduces to AddonEnabled.
// The caller resolves packageType -> (id, minTier) through the addons
// registry — the mapping is registry data, not license data.
func (m *Manager) PackageTypeAvailable(ctx context.Context, packageType string, minTier Tier) bool {
	return m.AddonEnabled(ctx, packageType, minTier)
}

// refresh re-reads the row and re-verifies. startup marks the initial load
// (its WARN wording says "startup"); ticker transitions from a previously
// licensed state additionally audit license.invalid once per fall.
func (m *Manager) refresh(ctx context.Context, startup bool) error {
	rec, err := m.store.GetLicense(ctx)
	if err != nil {
		// Transient store failure: keep the current snapshot. Flapping to
		// the floor on one failed read would tear concurrent gates for no
		// security gain — the stored row did not change.
		m.log.ErrorContext(ctx, "license: reading stored license failed; keeping current state",
			"error", err.Error())
		return fmt.Errorf("license: reading stored license: %w", err)
	}
	if rec == nil {
		m.state.Store(floorState())
		return nil
	}
	d, err := VerifyDocument(rec.Doc, m.keys, m.now())
	if err == nil {
		m.state.Store(stateFromDocument(d))
		return nil
	}

	// D-fail-safe: an installed row that stopped verifying degrades to the
	// floor with a WARN and (on a transition, or at startup) an audit
	// event. The stored row itself is left in place: the operator's next
	// step (reinstall or clean delete) must find the evidence.
	prev := m.state.Load()
	phase := "ticker"
	if startup {
		phase = "startup"
	}
	m.log.WarnContext(ctx, "license: stored license failed verification; degrading to community tier",
		"phase", phase, "reason", err.Error())
	if m.audit != nil && (startup || prev.Licensed) {
		detail, _ := json.Marshal(map[string]string{
			"reason":    reasonClass(err),
			"licenseId": rec.LicenseID,
			"tier":      rec.Tier,
		})
		m.audit.Record(ctx, audit.Event{
			Actor:  actorSystem,
			Action: ActionInvalid,
			Detail: string(detail),
		})
	}
	m.state.Store(floorState())
	return nil
}

// floorState is the unlicensed community floor: every M1~M9 capability
// unlocked, perpetual by definition (the floor has no expiry to cross).
func floorState() *State {
	return &State{Tier: TierCommunity, Perpetual: true}
}

// stateFromDocument projects a verified document into the snapshot form.
// Window evaluation stays with State()/refresh — the snapshot records the
// grant, the clock decides whether it is in force.
func stateFromDocument(d *Document) *State {
	return &State{
		Licensed:       true,
		Tier:           d.Tier,
		LicenseID:      d.LicenseID,
		Licensee:       d.Licensee,
		IssuedAt:       d.IssuedAt,
		NotBefore:      d.NotBefore,
		ExpiresAt:      d.ExpiresAt,
		Perpetual:      d.Perpetual,
		AddonAllowlist: d.AddonAllowlist,
		Limits:         d.Limits,
	}
}

// degradedState returns a copy of st on the community floor. expired
// distinguishes "window already passed" (true) from "window not open yet".
func degradedState(st State, expired bool) State {
	st.Tier = TierCommunity
	st.Expired = expired
	return st
}

// expiryText renders the record's expires_at column value ("" = perpetual,
// the metadata store's convention).
func expiryText(d *Document) string {
	if d.Perpetual {
		return ""
	}
	return d.ExpiresAt.Format(time.RFC3339)
}

// containsID is the allowlist membership test (linear over a tiny list;
// the allowlist is a handful of ids by construction).
func containsID(ids []string, id string) bool {
	for _, v := range ids {
		if v == id {
			return true
		}
	}
	return false
}

// reasonClass reduces a verification error to its wire-safe category for
// audit detail: expired / not-yet-valid / everything else ("invalid"). The
// category is the whole story — internal check details do not travel.
func reasonClass(err error) string {
	switch {
	case errors.Is(err, ErrExpired):
		return "expired"
	case errors.Is(err, ErrNotYetValid):
		return "not_yet_valid"
	default:
		return "invalid"
	}
}

// parseDisabledCSV splits the addons.disabled CSV into the effective set
// (trimmed, empties dropped, case preserved — addon ids are case-sensitive
// registry keys).
func parseDisabledCSV(csv string) map[string]struct{} {
	set := map[string]struct{}{}
	for _, part := range strings.Split(csv, ",") {
		if part = strings.TrimSpace(part); part != "" {
			set[part] = struct{}{}
		}
	}
	return set
}
