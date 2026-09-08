package webhook_test

// The outbox row-level face's domain legs (M17 T-496, FR-159.2): the
// keyset-paginated filtered query over a real migrated store (filters,
// cursor round-trip, the limit+1 has-more probe, the garbage-cursor
// refusal) and the bus's replay face (dead→pending reset, the two
// refusal arms, and the wake: a running engine delivers the replayed row
// without any further poke).

import (
	"context"
	"errors"
	"io"
	"log/slog"
	"net/http"
	"net/http/httptest"
	"sync/atomic"
	"testing"
	"time"

	"github.com/lzwzzy/binflow/internal/metadata"
	"github.com/lzwzzy/binflow/internal/webhook"
)

// newOutboxTestBus assembles the minimal bus over a migrated store (the
// closed-domain registry test reuses it for its legacy-row leg).
func newOutboxTestBus(t *testing.T) (*webhook.SQLiteStore, *webhook.Bus) {
	t.Helper()
	store, md, _ := openStore(t)
	bus, err := webhook.NewBus(webhook.BusOptions{
		Store:              store,
		Repos:              md.Repos(),
		Gate:               allowGate(),
		AllowPrivateTarget: true,
		Origin:             "https://binflow.example.com",
	})
	if err != nil {
		t.Fatalf("NewBus: %v", err)
	}
	return store, bus
}

// seedRows inserts outbox rows directly with distinct, ascending
// created_at stamps (a fan-out batch shares one stamp, so explicit
// clocks keep the (created_at, id) order deterministic) and returns the
// row ids in the same order.
func seedRows(t *testing.T, store *webhook.SQLiteStore, subID string, stamps []string) []string {
	t.Helper()
	ctx := context.Background()
	rows := make([]*webhook.Delivery, 0, len(stamps))
	for i, at := range stamps {
		rows = append(rows, &webhook.Delivery{
			ID:             "row-" + at[:8] + "-" + time.Duration(i).String(),
			SubscriptionID: subID,
			EventType:      "deployed",
			Payload:        `{"domain":"artifact"}`,
			NextAttemptAt:  at,
			CreatedAt:      at,
		})
	}
	if err := store.EnqueueDeliveries(ctx, rows); err != nil {
		t.Fatalf("seed rows: %v", err)
	}
	out := make([]string, 0, len(rows))
	for _, r := range rows {
		out = append(out, r.ID)
	}
	return out
}

// subscribeOutbox registers one subscription through the bus and answers
// its store id.
func subscribeOutbox(t *testing.T, bus *webhook.Bus, key string) string {
	t.Helper()
	body := `{
		"key": "` + key + `",
		"enabled": true,
		"event_filter": {"domain": "artifact", "event_types": ["deployed"], "criteria": {"anyLocal": true}},
		"handlers": [{"handler_type": "webhook", "url": "https://ci.example.com/hook"}]
	}`
	req := mustParse(t, body)
	sub, err := bus.Create(context.Background(), req, "admin")
	if err != nil {
		t.Fatalf("create subscription: %v", err)
	}
	return sub.ID
}

func TestQueryOutboxFiltersPagesAndCursors(t *testing.T) {
	store, bus := newOutboxTestBus(t)
	ctx := context.Background()
	subID := subscribeOutbox(t, bus, "pager-hook")

	// Six rows at one-second intervals; row 1 goes dead, row 4 delivered,
	// so the status filter has honest variety.
	stamps := []string{
		"2026-01-01T00:00:01Z", "2026-01-01T00:00:02Z", "2026-01-01T00:00:03Z",
		"2026-01-01T00:00:04Z", "2026-01-01T00:00:05Z", "2026-01-01T00:00:06Z",
	}
	ids := seedRows(t, store, subID, stamps)
	code := int64(500)
	if err := store.MarkDead(ctx, ids[0], 5, "receiver answered 500", &code); err != nil {
		t.Fatalf("mark dead: %v", err)
	}
	if err := store.MarkDelivered(ctx, ids[3], 1, "2026-01-01T00:00:04.500Z", 200); err != nil {
		t.Fatalf("mark delivered: %v", err)
	}

	// Unfiltered: newest first, keys joined.
	page, err := store.QueryOutbox(ctx, webhook.OutboxFilter{})
	if err != nil {
		t.Fatalf("QueryOutbox: %v", err)
	}
	if len(page.Rows) != 6 {
		t.Fatalf("unfiltered page = %d rows, want 6", len(page.Rows))
	}
	if page.NextCursor != "" {
		t.Fatalf("full result must be the last page, got cursor %q", page.NextCursor)
	}
	if page.Rows[0].ID != ids[5] || page.Rows[5].ID != ids[0] {
		t.Fatalf("order = %v..%v, want newest first", page.Rows[0].ID, page.Rows[5].ID)
	}
	for _, r := range page.Rows {
		if r.SubscriptionKey != "pager-hook" {
			t.Fatalf("row %s joined key = %q, want pager-hook", r.ID, r.SubscriptionKey)
		}
	}

	// Status filter: exactly the dead row, with its observability columns.
	page, err = store.QueryOutbox(ctx, webhook.OutboxFilter{Status: webhook.StatusDead})
	if err != nil || len(page.Rows) != 1 {
		t.Fatalf("dead filter = %d rows (err %v), want 1", len(page.Rows), err)
	}
	dead := page.Rows[0]
	if dead.ID != ids[0] || dead.Attempts != 5 || dead.LastError != "receiver answered 500" ||
		dead.LastStatusCode == nil || *dead.LastStatusCode != 500 || dead.DeliveredAt != nil {
		t.Fatalf("dead row shape: %+v", dead)
	}
	// The delivered row carries its stamp and cleared error.
	page, _ = store.QueryOutbox(ctx, webhook.OutboxFilter{Status: webhook.StatusDelivered})
	if len(page.Rows) != 1 || page.Rows[0].ID != ids[3] ||
		page.Rows[0].DeliveredAt == nil || *page.Rows[0].DeliveredAt != "2026-01-01T00:00:04.500Z" {
		t.Fatalf("delivered filter: %+v", page.Rows)
	}

	// Subscription filter: unknown key is an empty page, never a 404.
	page, err = store.QueryOutbox(ctx, webhook.OutboxFilter{SubscriptionKey: "no-such-hook"})
	if err != nil || len(page.Rows) != 0 || page.NextCursor != "" {
		t.Fatalf("unknown subscription filter = %+v (err %v), want empty page", page, err)
	}

	// Pagination: three pages of two, cursor handed page to page, the
	// exact-fill boundary detectable (the limit+1 probe).
	var walked []string
	f := webhook.OutboxFilter{Limit: 2}
	for i := 0; i < 4; i++ {
		page, err = store.QueryOutbox(ctx, f)
		if err != nil {
			t.Fatalf("page %d: %v", i, err)
		}
		for _, r := range page.Rows {
			walked = append(walked, r.ID)
		}
		if page.NextCursor == "" {
			break
		}
		f.Cursor = page.NextCursor
	}
	if len(walked) != 6 {
		t.Fatalf("walked %d rows over pages, want 6: %v", len(walked), walked)
	}
	want := []string{ids[5], ids[4], ids[3], ids[2], ids[1], ids[0]}
	for i := range want {
		if walked[i] != want[i] {
			t.Fatalf("walk order = %v, want %v", walked, want)
		}
	}

	// The cursor this store never issued is client input: ErrInvalidCursor.
	if _, err = store.QueryOutbox(ctx, webhook.OutboxFilter{Cursor: "garbage"}); !errors.Is(err, metadata.ErrInvalidCursor) {
		t.Fatalf("garbage cursor = %v, want ErrInvalidCursor", err)
	}
	if _, err = store.QueryOutbox(ctx, webhook.OutboxFilter{Cursor: "2026-01-01T00:00:01Z|"}); !errors.Is(err, metadata.ErrInvalidCursor) {
		t.Fatalf("empty-id cursor = %v, want ErrInvalidCursor", err)
	}
}

// TestBusReplayResetsDeadRow: the bus face behind the REST endpoint —
// dead row resets to pending with attempts zeroed and the joined key on
// the echo; a live row refuses ErrNotDead; a missing row refuses
// ErrNotFound.
func TestBusReplayResetsDeadRow(t *testing.T) {
	store, bus := newOutboxTestBus(t)
	ctx := context.Background()
	subID := subscribeOutbox(t, bus, "replay-hook")
	ids := seedRows(t, store, subID, []string{"2026-01-02T00:00:01Z"})
	code := int64(404)
	if err := store.MarkDead(ctx, ids[0], 1, "receiver answered 404", &code); err != nil {
		t.Fatalf("mark dead: %v", err)
	}
	row, err := bus.Replay(ctx, ids[0])
	if err != nil {
		t.Fatalf("Replay: %v", err)
	}
	if row.Status != webhook.StatusPending || row.Attempts != 0 || row.LastError != "" ||
		row.SubscriptionKey != "replay-hook" {
		t.Fatalf("post-replay row: %+v", row)
	}
	if row.NextAttemptAt == "" {
		t.Fatal("post-replay row must be due (next_attempt_at stamped)")
	}
	// A live row refuses.
	if _, err = bus.Replay(ctx, ids[0]); !errors.Is(err, webhook.ErrNotDead) {
		t.Fatalf("replay of pending row = %v, want ErrNotDead", err)
	}
	// A missing row refuses.
	if _, err = bus.Replay(ctx, "no-such-row"); !errors.Is(err, webhook.ErrNotFound) {
		t.Fatalf("replay of missing row = %v, want ErrNotFound", err)
	}
}

// TestBusReplayWakesTheEngine: the replay's notify is the dispatcher's
// wake hook — a running engine delivers the replayed row without any
// further poke (the FR-159.2 chain's domain half; the REST half lives in
// httpapi's webhook_outbox_test.go).
func TestBusReplayWakesTheEngine(t *testing.T) {
	store, md, _ := openStore(t)
	ctx := context.Background()
	if err := md.Repos().Create(ctx, &metadata.Repo{
		RepoKey: "maven-local", Type: "local", PackageType: "maven",
		CreatedAt: "2026-01-01T00:00:00Z", UpdatedAt: "2026-01-01T00:00:00Z",
	}); err != nil {
		t.Fatalf("seed repo: %v", err)
	}
	var hits atomic.Int64
	rcv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		hits.Add(1)
		w.WriteHeader(http.StatusOK)
	}))
	defer rcv.Close()

	bus, err := webhook.NewBus(webhook.BusOptions{
		Store: store, Gate: allowGate(), Repos: md.Repos(),
		AllowPrivateTarget: true, Origin: "https://binflow.example.com",
		Logger: slog.New(slog.NewTextHandler(io.Discard, nil)),
	})
	if err != nil {
		t.Fatalf("NewBus: %v", err)
	}
	d, err := webhook.NewDispatcher(webhook.DispatcherOptions{
		Bus: bus, Logger: slog.New(slog.NewTextHandler(io.Discard, nil)),
		RetryWait: 40 * time.Millisecond, PollInterval: 10 * time.Millisecond,
		FrequencyPerSec: 100000, BurstSize: 100000,
		TroubleshootingCleanupInterval: time.Hour,
	})
	if err != nil {
		t.Fatalf("NewDispatcher: %v", err)
	}
	runCtx, cancel := context.WithCancel(ctx)
	done := make(chan struct{})
	go func() {
		defer close(done)
		_ = d.Run(runCtx)
	}()
	defer func() { cancel(); <-done }()

	body := `{
		"key": "wake-hook",
		"enabled": true,
		"event_filter": {"domain": "artifact", "event_types": ["deployed"], "criteria": {"anyLocal": true}},
		"handlers": [{"handler_type": "webhook", "url": "` + rcv.URL + `"}]
	}`
	sub, err := bus.Create(ctx, mustParse(t, body), "admin")
	if err != nil {
		t.Fatalf("create subscription: %v", err)
	}

	// Dead letter the row by hand (the engine's 500x4 exhaustion is the
	// httpapi leg's job; here the transition is the fixture).
	rows := []*webhook.Delivery{{
		ID: "wake-row-1", SubscriptionID: sub.ID, EventType: "deployed",
		Payload:       `{"domain":"artifact","event_type":"deployed","subscription_key":"wake-hook"}`,
		NextAttemptAt: "2026-01-03T00:00:00Z", CreatedAt: "2026-01-03T00:00:00Z",
	}}
	if err := store.EnqueueDeliveries(ctx, rows); err != nil {
		t.Fatalf("seed row: %v", err)
	}
	if err := store.MarkDead(ctx, "wake-row-1", 5, "receiver answered 500", nil); err != nil {
		t.Fatalf("mark dead: %v", err)
	}
	if _, err := bus.Replay(ctx, "wake-row-1"); err != nil {
		t.Fatalf("Replay: %v", err)
	}
	waitForOutbox(t, 5*time.Second, "replayed delivery", func() bool {
		row, err := store.GetDelivery(ctx, "wake-row-1")
		return err == nil && row.Status == webhook.StatusDelivered
	})
	if n := hits.Load(); n == 0 {
		t.Fatal("the engine never delivered the replayed row")
	}
}

// waitForOutbox polls cond until it holds or the timeout expires (the
// dispatcher-test helper's shape, local to keep this file standalone).
func waitForOutbox(t *testing.T, timeout time.Duration, what string, cond func() bool) {
	t.Helper()
	deadline := time.Now().Add(timeout)
	for time.Now().Before(deadline) {
		if cond() {
			return
		}
		time.Sleep(5 * time.Millisecond)
	}
	t.Fatalf("timed out waiting for %s", what)
}
