package webhook_test

// The M17-Q5 裁① legs (FR-159.2 AC-2, ADR-0041 decision 7 errata): the
// registration face is closed-domain — only domains with BinFlow trigger
// sources validate. Three assertions per the ruling: the deregistered
// nine refuse new subscriptions at the parse gate (触发源注销), the one
// dormant type inside a sourced domain keeps its honest silent label
// (标注断言), and a LEGACY stored subscription on a deregistered pair
// stays readable and never fires (the honest migration posture — no
// deletion, no fabrication).

import (
	"context"
	"strings"
	"testing"
	"time"

	"github.com/lzwzzy/binflow/internal/webhook"
)

// TestClosedDomainRefusesSourcelessSubscriptions: every one of the nine
// deregistered domains answers the validation 400 naming the legal set —
// the write face is closed, spelled in the parse error the REST plane
// surfaces verbatim.
func TestClosedDomainRefusesSourcelessSubscriptions(t *testing.T) {
	for _, d := range webhook.DeregisteredDomains() {
		body := `{
			"key": "legacy-` + strings.ReplaceAll(d, "_", "-") + `",
			"event_filter": {"domain": "` + d + `", "event_types": ["created"], "criteria": {}},
			"handlers": [{"handler_type": "webhook", "url": "https://ci.example.com/hook"}]
		}`
		_, err := webhook.ParseSubscriptionRequest([]byte(body), nil)
		if err == nil || !strings.Contains(err.Error(), "unknown event domain") {
			t.Errorf("domain %q: ParseSubscriptionRequest = %v, want the unknown-domain 400", d, err)
		}
		if !strings.Contains(err.Error(), "artifact") || !strings.Contains(err.Error(), "build") {
			t.Errorf("domain %q: error %q must name the legal set", d, err.Error())
		}
	}
}

// TestDormantLabelHonestInSourcedDomain: docker/promoted — the one type a
// sourced domain carries without a trigger — resolves, is NOT wired, and
// a subscription on it parses green (subscribable, never fired: the
// Emit seam's drop is pinned in TestT362EventClosedSet's arm and the
// dispatcher family; this is the label's honesty, zero fabrication).
func TestDormantLabelHonestInSourcedDomain(t *testing.T) {
	et, ok := webhook.Lookup("docker", "promoted")
	if !ok {
		t.Fatal("docker/promoted must stay registered")
	}
	if et.Source != webhook.SourceDormant {
		t.Fatalf("docker/promoted source = %q, want dormant", et.Source)
	}
	if webhook.Wired("docker", "promoted") {
		t.Fatal("docker/promoted must not report wired")
	}
	body := `{
		"key": "await-docker-promotion",
		"enabled": true,
		"event_filter": {"domain": "docker", "event_types": ["promoted"], "criteria": {}},
		"handlers": [{"handler_type": "webhook", "url": "https://ci.example.com/hook"}]
	}`
	if _, err := webhook.ParseSubscriptionRequest([]byte(body), nil); err != nil {
		t.Fatalf("dormant-in-sourced-domain subscription must parse green: %v", err)
	}
}

// TestLegacyDormantSubscriptionStaysReadableAndSilent: a row stored
// before the ruling (inserted straight through the store — validation
// would refuse it now) keeps its honest postures: the read face echoes
// it verbatim, a PUT re-validates against the closed set and refuses,
// and Emit of its (deregistered) pair enqueues nothing — the row can
// never fire, and nothing deletes it (the 安全底线: legacy data is the
// operator's to remove).
func TestLegacyDormantSubscriptionStaysReadableAndSilent(t *testing.T) {
	store, bus := newOutboxTestBus(t)
	ctx := context.Background()
	legacy := &webhook.Subscription{
		ID: "legacy-sub-1", Key: "legacy-release-bundle", Domain: "release_bundle",
		EventTypes: []string{"created"}, Criteria: []byte(`{}`),
		Handler:   webhook.Handler{Type: "webhook", URL: "https://ci.example.com/hook"},
		CreatedAt: time.Now().UTC().Format(time.RFC3339Nano),
	}
	if err := store.CreateSubscription(ctx, legacy); err != nil {
		t.Fatalf("seed legacy subscription: %v", err)
	}
	// Readable: the read face never re-validates against the closed set.
	got, err := bus.Get(ctx, "legacy-release-bundle")
	if err != nil {
		t.Fatalf("legacy Get: %v", err)
	}
	if got.Domain != "release_bundle" || len(got.EventTypes) != 1 || got.EventTypes[0] != "created" {
		t.Fatalf("legacy echo drifted: %+v", got)
	}
	// Silent: Emit of the deregistered pair drops before matching — no
	// enqueue, no delivery row, the loss counter untouched (an unregistered
	// drop is a programming-error guard, not a visible loss).
	before := bus.EnqueueFailures()
	bus.Emit(ctx, webhook.Event{Domain: "release_bundle", Type: "created"})
	if bus.EnqueueFailures() != before {
		t.Fatalf("unregistered emit counted as enqueue failure (%d -> %d)", before, bus.EnqueueFailures())
	}
	if n, err := store.CountPending(ctx); err != nil || n != 0 {
		t.Fatalf("pending after deregistered emit = %d (err %v), want 0", n, err)
	}
	// Refused on rewrite: the update re-validates the event filter and
	// answers the unknown-domain 400 (closed-set face).
	updateBody := `{
		"key": "legacy-release-bundle",
		"event_filter": {"domain": "release_bundle", "event_types": ["created"], "criteria": {}},
		"handlers": [{"handler_type": "webhook", "url": "https://ci.example.com/hook"}]
	}`
	req, err := webhook.ParseSubscriptionRequest([]byte(updateBody), nil)
	if err == nil {
		_, uerr := bus.Update(ctx, "legacy-release-bundle", req, "admin")
		if uerr == nil || !strings.Contains(uerr.Error(), "unknown event domain") {
			t.Fatalf("legacy Update = %v, want the unknown-domain 400", uerr)
		}
	}
}
