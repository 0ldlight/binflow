package license_test

// T-279 Manager behavior: lifecycle (install/replace/uninstall), D7
// (failed install changes nothing), the addons.disabled CSV, the AddonEnabled
// matrix, D6 expiry (immediate via State + ticker-driven audit/log), the
// NFR-S53 startup fail-safe over a tampered row, and the -race no-tearing
// proof of concurrent Install/Uninstall x State.

import (
	"context"
	"encoding/json"
	"log/slog"
	"strings"
	"sync"
	"testing"
	"time"

	"github.com/lzwzzy/binflow/internal/audit"
	"github.com/lzwzzy/binflow/internal/license"
	"github.com/lzwzzy/binflow/internal/metadata"
)

// fakeStore is the in-memory LicenseStore: it can also hold a hand-tampered
// row (the NFR-S53 leg writes one directly, bypassing Install).
type fakeStore struct {
	mu  sync.Mutex
	rec *metadata.LicenseRecord
}

func (f *fakeStore) GetLicense(context.Context) (*metadata.LicenseRecord, error) {
	f.mu.Lock()
	defer f.mu.Unlock()
	return f.rec, nil
}

func (f *fakeStore) PutLicense(_ context.Context, rec *metadata.LicenseRecord) error {
	f.mu.Lock()
	defer f.mu.Unlock()
	f.rec = rec
	return nil
}

func (f *fakeStore) DeleteLicense(context.Context) error {
	f.mu.Lock()
	defer f.mu.Unlock()
	f.rec = nil
	return nil
}

// recordingAudit collects Record calls for assertions.
type recordingAudit struct {
	mu     sync.Mutex
	events []audit.Event
}

func (r *recordingAudit) Record(_ context.Context, e audit.Event) {
	r.mu.Lock()
	defer r.mu.Unlock()
	r.events = append(r.events, e)
}

func (r *recordingAudit) snapshot() []audit.Event {
	r.mu.Lock()
	defer r.mu.Unlock()
	return append([]audit.Event(nil), r.events...)
}

// logSink is a synchronized line-capturing slog writer.
type logSink struct {
	mu    sync.Mutex
	lines []string
}

func (l *logSink) Write(p []byte) (int, error) {
	l.mu.Lock()
	defer l.mu.Unlock()
	l.lines = append(l.lines, string(p))
	return len(p), nil
}

func (l *logSink) String() string {
	l.mu.Lock()
	defer l.mu.Unlock()
	return strings.Join(l.lines, "")
}

// mgr bundle: the manager under test plus the exact collaborators the
// assertions need (its signing keys, its audit recorder, its log sink).
type mgr struct {
	*license.Manager
	keys testKeys
	aud  *recordingAudit
	log  *logSink
}

// newTestManager builds the bundle with fresh keys.
func newTestManager(t *testing.T, mutate func(*license.Options), store license.Store, now func() time.Time) mgr {
	t.Helper()
	return newTestManagerKeys(t, mutate, store, now, newTestKeys(t))
}

// newTestManagerKeys builds the bundle over GIVEN keys (the restart legs:
// a second Manager over the same store must verify with the same issuer).
func newTestManagerKeys(t *testing.T, mutate func(*license.Options), store license.Store, now func() time.Time, k testKeys) mgr {
	t.Helper()
	sink := &logSink{}
	logger := slog.New(slog.NewTextHandler(sink, nil))
	rec := &recordingAudit{}
	opts := license.Options{
		Store:      store,
		VerifyKeys: k.keys,
		Audit:      rec,
		Log:        logger,
		Now:        now,
	}
	if mutate != nil {
		mutate(&opts)
	}
	m, err := license.New(opts)
	if err != nil {
		t.Fatalf("license.New: %v", err)
	}
	return mgr{Manager: m, keys: k, aud: rec, log: sink}
}

func realClock() func() time.Time { return func() time.Time { return time.Now().UTC() } }

// signTier renders a valid document for tier under m's verify keys.
func (m mgr) signTier(t *testing.T, now time.Time, tier license.Tier) string {
	t.Helper()
	s := m.keys.spec(now)
	s.tier = tier.String()
	if tier == license.TierCommunity {
		s.expiresAt = nil
	}
	return s.sign(t)
}

// TestManagerLifecycle: floor -> install pro -> replace with enterprise ->
// uninstall -> floor; the store row follows (single-row model).
func TestManagerLifecycle(t *testing.T) {
	now := realClock()
	store := &fakeStore{}
	m := newTestManager(t, nil, store, now)
	ctx := context.Background()

	if st := m.State(); st.Licensed || st.Tier != license.TierCommunity || !st.Perpetual {
		t.Fatalf("fresh manager not on the floor: %+v", st)
	}

	pro := m.signTier(t, now(), license.TierPro)
	st, err := m.Install(ctx, pro)
	if err != nil {
		t.Fatalf("install pro: %v", err)
	}
	if !st.Licensed || st.Tier != license.TierPro || st.Licensee != "Acme Corp" {
		t.Fatalf("post-install state wrong: %+v", st)
	}
	rec, _ := store.GetLicense(ctx)
	if rec == nil || rec.Tier != "pro" || rec.Doc != strings.TrimSpace(pro) {
		t.Fatalf("stored row wrong: %+v", rec)
	}

	if _, err := m.Install(ctx, m.signTier(t, now(), license.TierEnterprise)); err != nil {
		t.Fatalf("install enterprise: %v", err)
	}
	rec, _ = store.GetLicense(ctx)
	if rec.Tier != "enterprise" {
		t.Fatalf("replace did not land: %+v", rec)
	}

	if err := m.Uninstall(ctx); err != nil {
		t.Fatalf("uninstall: %v", err)
	}
	if st := m.State(); st.Licensed || st.Tier != license.TierCommunity {
		t.Fatalf("post-uninstall state wrong: %+v", st)
	}
	if rec, _ := store.GetLicense(ctx); rec != nil {
		t.Fatalf("row survived uninstall: %+v", rec)
	}
	// Idempotent uninstall: the floor is already in force.
	if err := m.Uninstall(ctx); err != nil {
		t.Fatalf("idempotent uninstall: %v", err)
	}
}

// TestManagerInstallFailureKeepsState (D7): a rejected document leaves the
// current license fully in force — state snapshot AND stored row.
func TestManagerInstallFailureKeepsState(t *testing.T) {
	now := realClock()
	store := &fakeStore{}
	m := newTestManager(t, nil, store, now)
	ctx := context.Background()

	if _, err := m.Install(ctx, m.signTier(t, now(), license.TierPro)); err != nil {
		t.Fatalf("install pro: %v", err)
	}
	expired := m.keys.spec(now())
	e := now().Add(-2 * time.Hour).Format(time.RFC3339)
	expired.expiresAt = &e
	bad := []string{
		"garbage",
		tamperPayloadContent(t, m.signTier(t, now(), license.TierEnterprise)),
		expired.sign(t),
	}
	for _, doc := range bad {
		if _, err := m.Install(ctx, doc); err == nil {
			t.Fatalf("install accepted an invalid document: %.40s", doc)
		}
		st := m.State()
		if !st.Licensed || st.Tier != license.TierPro {
			t.Fatalf("rejected install changed the state: %+v", st)
		}
		rec, _ := store.GetLicense(ctx)
		if rec == nil || rec.Tier != "pro" {
			t.Fatalf("rejected install changed the stored row: %+v", rec)
		}
	}
}

// TestManagerDisabledCSV: the CSV parses into the disabled set; a core id
// in the list WARNs but still takes effect (FR-85.3's circuit-breaker
// posture).
func TestManagerDisabledCSV(t *testing.T) {
	now := realClock()
	m := newTestManager(t, func(o *license.Options) {
		o.DisabledCSV = " npm , ha ,,docker,"
	}, &fakeStore{}, now)
	ctx := context.Background()

	for _, id := range []string{"npm", "ha", "docker"} {
		if m.AddonEnabled(ctx, id, license.TierCommunity) {
			t.Fatalf("addon %q should be disabled", id)
		}
	}
	if !m.AddonEnabled(ctx, "maven", license.TierCommunity) {
		t.Fatal("maven should be unaffected")
	}
	// Exactly the two core ids warn; the non-core "ha" stays silent.
	logs := m.log.String()
	if got := strings.Count(logs, "core package type"); got != 2 {
		t.Fatalf("core-id WARN count = %d, want 2 (npm+docker): %s", got, logs)
	}
}

// TestAddonEnabledMatrix: the tier x addon unlock table at the single
// evaluation point (section 15.2.3).
func TestAddonEnabledMatrix(t *testing.T) {
	now := realClock()
	store := &fakeStore{}
	m := newTestManager(t, nil, store, now)
	ctx := context.Background()

	// No license: floor addons pass, gated tiers do not.
	if !m.AddonEnabled(ctx, "generic", license.TierCommunity) {
		t.Fatal("floor addon locked without a license")
	}
	if m.AddonEnabled(ctx, "go", license.TierPro) {
		t.Fatal("pro addon unlocked without a license")
	}

	// Pro license: pro gated addons pass, enterprise ones do not.
	if _, err := m.Install(ctx, m.signTier(t, now(), license.TierPro)); err != nil {
		t.Fatalf("install pro: %v", err)
	}
	if !m.AddonEnabled(ctx, "go", license.TierPro) {
		t.Fatal("pro addon locked under a pro license")
	}
	if m.AddonEnabled(ctx, "ha", license.TierEnterprise) {
		t.Fatal("enterprise addon unlocked under a pro license")
	}

	// Explicit allowlist narrows, but never the floor tier.
	narrow := m.keys.spec(now())
	narrow.addons = []string{"go"}
	if _, err := m.Install(ctx, narrow.sign(t)); err != nil {
		t.Fatalf("install allowlisted pro: %v", err)
	}
	if !m.AddonEnabled(ctx, "go", license.TierPro) {
		t.Fatal("allowlisted addon locked")
	}
	if m.AddonEnabled(ctx, "cargo", license.TierPro) {
		t.Fatal("non-allowlisted pro addon unlocked")
	}
	if !m.AddonEnabled(ctx, "generic", license.TierCommunity) {
		t.Fatal("floor addon constrained by an allowlist")
	}

	// Disabled config outranks everything, the floor included.
	mDis := newTestManager(t, func(o *license.Options) { o.DisabledCSV = "generic" }, &fakeStore{}, now)
	if mDis.AddonEnabled(ctx, "generic", license.TierCommunity) {
		t.Fatal("disabled config did not lock a floor addon")
	}
	// PackageTypeAvailable is the same evaluation under the repo-gate name.
	if !m.PackageTypeAvailable(ctx, "maven", license.TierCommunity) {
		t.Fatal("PackageTypeAvailable disagrees with AddonEnabled on a floor type")
	}
	if m.PackageTypeAvailable(ctx, "go", license.TierEnterprise) {
		t.Fatal("PackageTypeAvailable unlocked an enterprise-gated type")
	}
}

// TestManagerExpiryD6: the moment the clock passes expiresAt+leeway the
// tier degrades with no grace — State reflects it immediately, and the
// ticker's re-evaluation turns it into the logged+audited transition.
func TestManagerExpiryD6(t *testing.T) {
	base := time.Date(2026, 8, 25, 12, 0, 0, 0, time.UTC)
	// A guarded fake clock: the ticker goroutine reads it while the test
	// advances it (production's time.Now is inherently goroutine-safe).
	var clockMu sync.Mutex
	clock := base
	now := func() time.Time {
		clockMu.Lock()
		defer clockMu.Unlock()
		return clock
	}
	advance := func(d time.Duration) {
		clockMu.Lock()
		clock = clock.Add(d)
		clockMu.Unlock()
	}
	store := &fakeStore{}
	m := newTestManager(t, func(o *license.Options) { o.TickEvery = 20 * time.Millisecond }, store, now)
	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()
	go m.Run(ctx) // the lifecycle runServe owns in production

	// Document valid for 2h from base.
	s := m.keys.spec(base)
	exp := base.Add(2 * time.Hour).Format(time.RFC3339)
	s.expiresAt = &exp
	if _, err := m.Install(ctx, s.sign(t)); err != nil {
		t.Fatalf("install: %v", err)
	}
	if st := m.State(); st.Tier != license.TierPro {
		t.Fatalf("pre-expiry tier wrong: %+v", st)
	}

	// Advance past expiry+leeway (D6: no grace beyond the 1h skew bound).
	advance(3*time.Hour + 2*time.Minute)
	st := m.State()
	if st.Tier != license.TierCommunity || !st.Licensed || !st.Expired {
		t.Fatalf("post-expiry state wrong (want degraded+expired flags): %+v", st)
	}

	// The ticker lands the transition: WARN + one license.invalid event
	// (audited once per fall, not per tick).
	deadline := time.Now().Add(3 * time.Second)
	for {
		var invalid int
		for _, e := range m.aud.snapshot() {
			if e.Action == license.ActionInvalid {
				invalid++
			}
		}
		if invalid >= 1 && strings.Contains(m.log.String(), "failed verification") {
			break
		}
		if time.Now().After(deadline) {
			t.Fatalf("ticker never landed the expiry transition; audit=%d logs=%s", invalid, m.log.String())
		}
		time.Sleep(5 * time.Millisecond)
	}
	// The audit transition fires once, not once per tick.
	time.Sleep(60 * time.Millisecond)
	var invalid int
	for _, e := range m.aud.snapshot() {
		if e.Action == license.ActionInvalid {
			invalid++
		}
	}
	if invalid != 1 {
		t.Fatalf("license.invalid fired %d times, want exactly 1 (once per fall)", invalid)
	}
	// The stored row survives: the operator's evidence for the reinstall.
	if rec, _ := store.GetLicense(ctx); rec == nil {
		t.Fatal("expiry degradation deleted the stored row")
	}
}

// TestManagerLoadFailsafe (NFR-S53 / FR-84-AC6): a stored row that no
// longer verifies — the DB-tamper shape — degrades to the floor at Load
// with a WARN and a license.invalid event; Load itself returns no error.
func TestManagerLoadFailsafe(t *testing.T) {
	now := realClock()
	store := &fakeStore{}
	// A row whose doc was tampered after a legitimate install.
	good := newTestKeys(t).spec(now()).sign(t)
	if err := store.PutLicense(context.Background(), &metadata.LicenseRecord{
		LicenseID:   "0f1e2d3c-test",
		Tier:        "enterprise",
		Licensee:    "Acme Corp",
		Doc:         tamperPayloadContent(t, good),
		IssuedAt:    now().Add(-24 * time.Hour).Format(time.RFC3339),
		NotBefore:   now().Add(-1 * time.Hour).Format(time.RFC3339),
		ExpiresAt:   now().Add(24 * time.Hour).Format(time.RFC3339),
		InstalledAt: now().Format(time.RFC3339),
	}); err != nil {
		t.Fatalf("planting tampered row: %v", err)
	}

	m := newTestManager(t, nil, store, now)
	if err := m.Load(context.Background()); err != nil {
		t.Fatalf("Load must not fail startup: %v", err)
	}
	if st := m.State(); st.Licensed || st.Tier != license.TierCommunity {
		t.Fatalf("tampered row kept a high tier: %+v", st)
	}
	logs := m.log.String()
	if !strings.Contains(logs, "failed verification") || !strings.Contains(logs, "degrading") {
		t.Fatalf("fail-safe WARN missing: %s", logs)
	}
	if !strings.Contains(logs, "startup") {
		t.Fatalf("startup phase not logged: %s", logs)
	}
	found := false
	for _, e := range m.aud.snapshot() {
		if e.Action == license.ActionInvalid && e.Actor == "system" {
			var d map[string]string
			if err := json.Unmarshal([]byte(e.Detail), &d); err != nil {
				t.Fatalf("invalid detail not JSON: %v", err)
			}
			if d["reason"] == "" || d["licenseId"] == "" {
				t.Fatalf("invalid detail missing fields: %s", e.Detail)
			}
			found = true
		}
	}
	if !found {
		t.Fatal("license.invalid audit event missing at startup")
	}
}

// TestManagerLoadValidRow: the happy startup — a verifying row re-enters
// the state.
func TestManagerLoadValidRow(t *testing.T) {
	now := realClock()
	store := &fakeStore{}
	m := newTestManager(t, nil, store, now)
	if _, err := m.Install(context.Background(), m.signTier(t, now(), license.TierPro)); err != nil {
		t.Fatalf("install: %v", err)
	}
	m2 := newTestManagerKeys(t, nil, store, now, m.keys)
	if err := m2.Load(context.Background()); err != nil {
		t.Fatalf("load: %v", err)
	}
	if st := m2.State(); !st.Licensed || st.Tier != license.TierPro {
		t.Fatalf("valid row did not re-enter the state: %+v", st)
	}
}

// TestManagerNewGuards: the constructor refuses a keyless/ storeless
// assembly loudly (a license core that silently verifies nothing is the
// worst failure shape).
func TestManagerNewGuards(t *testing.T) {
	k := newTestKeys(t)
	if _, err := license.New(license.Options{VerifyKeys: k.keys}); err == nil {
		t.Fatal("New accepted a nil store")
	}
	if _, err := license.New(license.Options{Store: &fakeStore{}}); err == nil {
		t.Fatal("New accepted empty verify keys")
	}
}

// TestManagerRedaction (NFR-S52): through the full lifecycle — installs,
// rejections, the fail-safe — neither the licensee nor any document/signature
// bytes reach the log stream.
func TestManagerRedaction(t *testing.T) {
	now := realClock()
	store := &fakeStore{}
	m := newTestManager(t, nil, store, now)
	ctx := context.Background()

	doc := m.signTier(t, now(), license.TierEnterprise)
	if _, err := m.Install(ctx, doc); err != nil {
		t.Fatalf("install: %v", err)
	}
	if _, err := m.Install(ctx, "garbage"); err == nil {
		t.Fatal("garbage accepted")
	}
	_ = m.Load(ctx)
	_ = m.Uninstall(ctx)

	logs := m.log.String()
	if strings.Contains(logs, "Acme Corp") {
		t.Fatalf("licensee leaked into logs: %s", logs)
	}
	for _, seg := range strings.Split(doc, ".") {
		if seg != "" && strings.Contains(logs, seg) {
			t.Fatalf("document segment leaked into logs: %s", logs)
		}
	}
}

// TestManagerConcurrentNoTearing (FR-85.2 / AC "State 为 atomic 快照,
// -race 并发 Install/Uninstall × State 读零撕裂"): writers flip between
// pro/enterprise installs and uninstalls while readers assert the
// self-consistency invariant — a Licensed state always carries the license
// id and a paid-tier spelling; an unlicensed one never does. go test -race
// is the detector.
func TestManagerConcurrentNoTearing(t *testing.T) {
	now := realClock()
	m := newTestManager(t, nil, &fakeStore{}, now)
	ctx := context.Background()
	pro := m.signTier(t, now(), license.TierPro)
	enter := m.signTier(t, now(), license.TierEnterprise)

	var writers, readers sync.WaitGroup
	stop := make(chan struct{})

	for i := 0; i < 4; i++ {
		writers.Add(1)
		go func(i int) {
			defer writers.Done()
			doc := pro
			if i%2 == 1 {
				doc = enter
			}
			for j := 0; j < 200; j++ {
				if _, err := m.Install(ctx, doc); err != nil {
					t.Errorf("concurrent install: %v", err)
					return
				}
				if err := m.Uninstall(ctx); err != nil {
					t.Errorf("concurrent uninstall: %v", err)
					return
				}
			}
		}(i)
	}
	for i := 0; i < 4; i++ {
		readers.Add(1)
		go func() {
			defer readers.Done()
			for {
				select {
				case <-stop:
					return
				default:
				}
				st := m.State()
				if st.Licensed {
					if st.LicenseID == "" {
						t.Errorf("licensed state without license id (torn read): %+v", st)
						return
					}
					if st.Tier != license.TierPro && st.Tier != license.TierEnterprise {
						t.Errorf("licensed state outside the paid closed set (torn read): %+v", st)
						return
					}
				} else if st.LicenseID != "" {
					t.Errorf("unlicensed state carries a license id (torn read): %+v", st)
					return
				}
			}
		}()
	}
	writers.Wait()
	// The last writer action is always an uninstall: the floor is the
	// deterministic endpoint. Release the readers and join them.
	if st := m.State(); st.Licensed {
		t.Fatalf("final state should be the floor after the uninstall loop: %+v", st)
	}
	close(stop)
	readers.Wait()
}
