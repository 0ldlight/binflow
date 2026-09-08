// The build domain's webhook legs (M17 T-510, FR-152.3 / ADR-0045 decision
// 7): the registry flip's wire consequences only — the three types fire
// real subscriptions, the envelope keeps its seven documented fields with
// the four-field build payload (webhook.md 3.4 — schema zero-change,
// ADR-0041 decision 7), the build scope's criteria filter works, and a
// real local receiver verifies the HMAC signature off the exact received
// bytes with openssl itself (the documented one-liner's own binary).

package webhook_test

import (
	"context"
	"encoding/json"
	"os/exec"
	"regexp"
	"strings"
	"testing"
	"time"

	"github.com/lzwzzy/binflow/internal/remote"
	"github.com/lzwzzy/binflow/internal/webhook"
)

// buildSubscriptionBody renders one build-domain subscription body.
func buildSubscriptionBody(key, eventType, criteria string) string {
	return `{
		"key": "` + key + `",
		"enabled": true,
		"event_filter": {"domain": "build", "event_types": ["` + eventType + `"], "criteria": ` + criteria + `},
		"handlers": [{"handler_type": "webhook", "url": "https://ci.example.com/hook"}]
	}`
}

// buildEvent fires one build event through the seam.
func buildEvent(t string, name, number, started, repo string) webhook.Event {
	return webhook.Event{
		Domain: webhook.DomainBuild, Type: t,
		Name: name, Repo: repo, BuildNumber: number, BuildStarted: started,
		Actor: webhook.Actor{ID: "ci-bot", Realm: "internal"},
	}
}

// TestBuildEventsFireSubscriptions: the flipped types enqueue for their
// subscribers (the dormant drop arm is gone), each envelope carrying the
// build payload's EXACTLY four fields and the envelope's EXACTLY seven
// keys — the schema zero-change assertion (AC-1 / ADR-0041 decision 7:
// flipping a Source is adding an Emit call, never a schema change).
func TestBuildEventsFireSubscriptions(t *testing.T) {
	ctx := context.Background()
	store, _, _ := openStore(t)
	bus, err := webhook.NewBus(webhook.BusOptions{
		Store: store, Gate: allowGate(), Origin: "https://binflow.example.com",
	})
	if err != nil {
		t.Fatalf("NewBus: %v", err)
	}
	for _, c := range []struct{ key, eventType, criteria string }{
		{"build-up", "uploaded", `{"anyBuild": true}`},
		{"build-del", "deleted", `{"anyBuild": true}`},
		{"build-promo", "promoted", `{"anyBuild": true}`},
	} {
		if _, err := bus.Create(ctx, mustParse(t, buildSubscriptionBody(c.key, c.eventType, c.criteria)), "admin"); err != nil {
			t.Fatalf("create %s: %v", c.key, err)
		}
	}

	const started = "2026-09-07T10:00:00.000+0000" // webhook.md 3.4's format
	for _, ev := range []webhook.Event{
		buildEvent(webhook.TypeBuildUploaded, "pub-app", "51", started, "artifactory-build-info"),
		buildEvent(webhook.TypeBuildDeleted, "pub-app", "51", started, "artifactory-build-info"),
		buildEvent(webhook.TypeBuildPromoted, "pub-app", "51", started, "artifactory-build-info"),
	} {
		bus.Emit(ctx, ev)
	}
	if n, _ := bus.PendingCount(ctx); n != 3 {
		t.Fatalf("pending = %d, want 3 (one per flipped type)", n)
	}

	subs, err := bus.List(ctx)
	if err != nil {
		t.Fatalf("list: %v", err)
	}
	seen := map[string]bool{}
	for _, sub := range subs {
		rows, err := store.ListDeliveries(ctx, sub.ID, 10)
		if err != nil {
			t.Fatalf("deliveries: %v", err)
		}
		if len(rows) != 1 {
			t.Fatalf("subscription %s carries %d rows, want 1", sub.Key, len(rows))
		}
		var env map[string]any
		if err := json.Unmarshal([]byte(rows[0].Payload), &env); err != nil {
			t.Fatalf("payload: %v", err)
		}
		// AC-1's schema assertion: the seven documented envelope fields,
		// no more (webhook.md section 4).
		if len(env) != 7 {
			t.Fatalf("envelope carries %d keys, want exactly 7: %v", len(env), env)
		}
		if env["domain"] != "build" {
			t.Fatalf("domain = %v", env["domain"])
		}
		if env["subscription_key"] != sub.Key {
			t.Fatalf("subscription_key = %v", env["subscription_key"])
		}
		data, _ := env["data"].(map[string]any)
		if data == nil {
			t.Fatalf("data: %v", env["data"])
		}
		// The build payload is its own four-field set — none of the
		// artifact family's base fields ride (webhook.md 3.4).
		if len(data) != 4 {
			t.Fatalf("build data carries %d keys, want exactly 4: %v", len(data), data)
		}
		if data["build_name"] != "pub-app" || data["build_number"] != "51" ||
			data["build_started"] != started || data["build_repo"] != "artifactory-build-info" {
			t.Fatalf("build data: %v", data)
		}
		if uc, _ := env["userContext"].(map[string]any); uc == nil || uc["id"] != "ci-bot" {
			t.Fatalf("userContext: %v", env["userContext"])
		}
		seen[sub.Key] = true
	}
	if len(seen) != 3 {
		t.Fatalf("subscriptions with deliveries = %d, want 3", len(seen))
	}
}

// TestBuildDomainRegistryAudit: the registry's wired/dormant marking AFTER
// the flip and the M17-Q5 closed-domain ruling, asserted per domain (the
// zero-fake audit: exactly the four domains with BinFlow trigger sources
// are registered — artifact 5 wired, artifact_property 2, docker 2 wired
// + promoted dormant, build 3 — every sourceless domain is DEREGISTERED,
// never a fabricated trigger and never a subscribable ghost).
func TestBuildDomainRegistryAudit(t *testing.T) {
	wantWired := map[string]int{
		"artifact": 5, "artifact_property": 2, "docker": 2, "build": 3,
	}
	gotWired := map[string]int{}
	total := 0
	for _, d := range webhook.Domains() {
		types := webhook.EventTypesOfDomain(d)
		total += len(types)
		for _, et := range types {
			if webhook.Wired(d, et) {
				gotWired[d]++
			}
		}
	}
	if total != 13 {
		t.Fatalf("registry total = %d, want 13 (4 sourced domains)", total)
	}
	if len(webhook.Domains()) != len(wantWired) {
		t.Fatalf("domain count = %d, want %d", len(webhook.Domains()), len(wantWired))
	}
	for d, want := range wantWired {
		if gotWired[d] != want {
			t.Errorf("domain %s carries %d wired types, want %d (zero-fake audit)", d, gotWired[d], want)
		}
	}
	// The M17-Q5 裁① arm: the nine sourceless domains are gone from the
	// registration face entirely — no dormant ghost rows left to audit.
	for _, d := range webhook.DeregisteredDomains() {
		if webhook.ValidDomain(d) {
			t.Errorf("deregistered domain %q must not be a registered domain", d)
		}
		if got := len(webhook.EventTypesOfDomain(d)); got != 0 {
			t.Errorf("deregistered domain %q carries %d types, want 0", d, got)
		}
	}
}

// TestBuildCriteriaScope: the build domain's criteria legs (webhook.md 2.2:
// anyBuild / selectedBuilds / include-excludePatterns) — driven through
// Emit so the whole match path is under assertion.
func TestBuildCriteriaScope(t *testing.T) {
	cases := []struct {
		name     string
		criteria string
		build    string
		want     bool
	}{
		{"anyBuild admits", `{"anyBuild": true}`, "any-name", true},
		{"nothing selected admits nothing", `{}`, "any-name", false},
		{"selectedBuilds hit", `{"selectedBuilds": ["pub-app"]}`, "pub-app", true},
		{"selectedBuilds miss", `{"selectedBuilds": ["other-app"]}`, "pub-app", false},
		{"anyBuild + include pattern hit", `{"anyBuild": true, "includePatterns": ["pub-*"]}`, "pub-app", true},
		{"anyBuild + include pattern miss", `{"anyBuild": true, "includePatterns": ["rel-*"]}`, "pub-app", false},
		{"selectedBuilds + exclude wins", `{"selectedBuilds": ["pub-app"], "excludePatterns": ["pub-app"]}`, "pub-app", false},
		{"anyBuild + exclude wins", `{"anyBuild": true, "excludePatterns": ["internal-*"]}`, "internal-app", false},
		// repoKeys is NOT a build-domain dimension: naming the build repo
		// alone must not admit (the honest reading of the criteria table).
		{"repoKeys is not consumed", `{"repoKeys": ["artifactory-build-info"]}`, "pub-app", false},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			ctx := context.Background()
			store, _, _ := openStore(t)
			bus, err := webhook.NewBus(webhook.BusOptions{Store: store, Gate: allowGate()})
			if err != nil {
				t.Fatalf("NewBus: %v", err)
			}
			if _, err := bus.Create(ctx, mustParse(t, buildSubscriptionBody("probe", "uploaded", tc.criteria)), "admin"); err != nil {
				t.Fatalf("create: %v", err)
			}
			bus.Emit(ctx, buildEvent(webhook.TypeBuildUploaded, tc.build, "1",
				"2026-09-07T10:00:00.000+0000", "artifactory-build-info"))
			n, _ := bus.PendingCount(ctx)
			if (n == 1) != tc.want {
				t.Fatalf("pending = %d, want admitted = %v", n, tc.want)
			}
		})
	}
}

// TestBuildDeliverySignedToReceiver: the T-362/T-364 form's build leg — a
// real local HTTP receiver gets the envelope over the wire, and the
// X-JFrog-Event-Auth HMAC is verified against the EXACT received bytes
// twice: through the package's own Signature and through the openssl
// binary itself (webhook.md section 6's documented verification command —
// `openssl dgst -sha256 -hmac "<secret>"` over the payload bytes).
func TestBuildDeliverySignedToReceiver(t *testing.T) {
	const secret = "t510-build-secret"
	receiver, hits := scriptedReceiver(t, 200)
	cipher, err := remote.NewCipher(make([]byte, 32))
	if err != nil {
		t.Fatalf("NewCipher: %v", err)
	}

	ctx := context.Background()
	store, _, _ := openStore(t)
	bus, err := webhook.NewBus(webhook.BusOptions{
		Store: store, Gate: allowGate(),
		Cipher:             cipher,
		AllowPrivateTarget: true, // the loopback receiver; the default-refusal arm is T-362's
		Origin:             "https://binflow.example.com",
	})
	if err != nil {
		t.Fatalf("NewBus: %v", err)
	}
	body := `{
		"key": "build-ci",
		"enabled": true,
		"event_filter": {"domain": "build", "event_types": ["uploaded", "promoted"], "criteria": {"anyBuild": true}},
		"handlers": [{"handler_type": "webhook", "url": "` + receiver.URL + `", "secret": "` + secret + `", "use_secret_for_signing": true}]
	}`
	if _, err := bus.Create(ctx, mustParseCipher(t, body, cipher), "admin"); err != nil {
		t.Fatalf("create: %v", err)
	}
	_, stop := startDispatcher(t, bus, nil)
	defer stop()

	bus.Emit(ctx, buildEvent(webhook.TypeBuildUploaded, "pub-app", "51",
		"2026-09-07T10:00:00.000+0000", "artifactory-build-info"))
	waitFor(t, 5*time.Second, "receiver hit", func() bool { return len(hits()) > 0 })

	got := hits()
	if len(got) != 1 {
		t.Fatalf("receiver saw %d requests, want 1", len(got))
	}
	wire := got[0].body

	// Arm 1: the package's own digest over the exact received bytes.
	if want := webhook.Signature(secret, []byte(wire)); got[0].auth != want {
		t.Fatalf("X-JFrog-Event-Auth = %q, want HMAC %q", got[0].auth, want)
	}
	// Arm 2: openssl itself, byte for byte — the documented verification
	// command's own binary (skipped only where no openssl exists).
	if openssl, err := exec.LookPath("openssl"); err == nil {
		cmd := exec.Command(openssl, "dgst", "-sha256", "-hmac", secret)
		cmd.Stdin = strings.NewReader(wire)
		out, err := cmd.Output()
		if err != nil {
			t.Fatalf("openssl dgst: %v: %s", err, out)
		}
		fields := strings.Fields(string(out))
		digest := fields[len(fields)-1]
		if !regexp.MustCompile(`^[0-9a-f]{64}$`).MatchString(digest) {
			t.Fatalf("openssl output carries no hex digest: %q", string(out))
		}
		if digest != got[0].auth {
			t.Fatalf("openssl digest %s != received header %q", digest, got[0].auth)
		}
	} else {
		t.Log("openssl binary not on PATH — the binary arm skipped (the Signature arm above already pins the digest)")
	}

	// The envelope the receiver parsed: domain/event_type/the four fields.
	var env struct {
		Domain    string `json:"domain"`
		EventType string `json:"event_type"`
		Data      struct {
			BuildName    string `json:"build_name"`
			BuildNumber  string `json:"build_number"`
			BuildStarted string `json:"build_started"`
			BuildRepo    string `json:"build_repo"`
		} `json:"data"`
	}
	if err := json.Unmarshal([]byte(wire), &env); err != nil {
		t.Fatalf("envelope: %v", err)
	}
	if env.Domain != "build" || env.EventType != "uploaded" {
		t.Fatalf("envelope identity: %+v", env)
	}
	if env.Data.BuildName != "pub-app" || env.Data.BuildNumber != "51" ||
		env.Data.BuildStarted != "2026-09-07T10:00:00.000+0000" ||
		env.Data.BuildRepo != "artifactory-build-info" {
		t.Fatalf("build data: %+v", env.Data)
	}

	// The promoted twin rides the SAME subscription (multi-type filter).
	bus.Emit(ctx, buildEvent(webhook.TypeBuildPromoted, "pub-app", "51",
		"2026-09-07T10:00:00.000+0000", "artifactory-build-info"))
	waitFor(t, 5*time.Second, "second receiver hit", func() bool { return len(hits()) == 2 })
	if second := hits()[1]; !strings.Contains(second.body, `"event_type":"promoted"`) {
		t.Fatalf("second delivery is not promoted: %s", second.body)
	}
}
