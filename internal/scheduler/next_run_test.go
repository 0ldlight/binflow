package scheduler_test

import (
	"errors"
	"fmt"
	"sort"
	"testing"
	"time"

	"github.com/lzwzzy/binflow/internal/scheduler"
)

// The next-run calculator's cell-by-cell table (M16 T-446 AC3 / NFR-P74):
// every row is an expression, an `after` instant and the expected trigger,
// hand-expanded from the anchor's factory expressions (cron-scheduling.md
// section 2) and the semantics of section 4 — the first trigger STRICTLY
// AFTER `after`, computed in UTC at second resolution.

// thu is a fixed anchor instant: Thursday 2026-09-03 07:00:00 UTC (the
// anchor's own example date).
var thu = time.Date(2026, 9, 3, 7, 0, 0, 0, time.UTC)

func TestNextRunTable(t *testing.T) {
	cases := []struct {
		name  string
		expr  string
		after time.Time
		want  time.Time
	}{
		// Factory expressions, evaluated after Thursday 07:00 (both of
		// Thursday's own 02:00 slots already past).
		{"backup-daily skips to Friday", "0 0 2 ? * MON-FRI", thu,
			time.Date(2026, 9, 4, 2, 0, 0, 0, time.UTC)},
		{"backup-weekly lands Saturday", "0 0 2 ? * SAT", thu,
			time.Date(2026, 9, 5, 2, 0, 0, 0, time.UTC)},
		{"daily 05:23 rolls to Friday", "0 23 5 * * ?", thu,
			time.Date(2026, 9, 4, 5, 23, 0, 0, time.UTC)},
		{"daily 05:12 rolls to Friday", "0 12 5 * * ?", thu,
			time.Date(2026, 9, 4, 5, 12, 0, 0, time.UTC)},
		{"daily 00:12 rolls to Friday", "0 12 0 * * ?", thu,
			time.Date(2026, 9, 4, 0, 12, 0, 0, time.UTC)},
		// C-d, both edges of the bare /4 hours form: the trigger set is
		// 0,4,8,...,20 — no 24-wrap.
		{"bare /4 hours picks the next slot", "0 0 /4 * * ?",
			time.Date(2026, 9, 3, 7, 0, 0, 0, time.UTC),
			time.Date(2026, 9, 3, 8, 0, 0, 0, time.UTC)},
		{"bare /4 last slot of the day", "0 0 /4 * * ?",
			time.Date(2026, 9, 3, 20, 0, 0, 0, time.UTC),
			time.Date(2026, 9, 4, 0, 0, 0, 0, time.UTC)},
		{"bare /4 just before a slot", "0 0 /4 * * ?",
			time.Date(2026, 9, 3, 3, 59, 59, 0, time.UTC),
			time.Date(2026, 9, 3, 4, 0, 0, 0, time.UTC)},
		// CRON_NEVER: concrete year domain, next run lands on its January
		// 1 (section 6's parity arm: "next-run ≈ 2099").
		{"CRON_NEVER parks in 2099", "0 0 0 ? * * 2099", thu,
			time.Date(2099, 1, 1, 0, 0, 0, 0, time.UTC)},
		// Strictly-after: a trigger falling exactly on `after` does not
		// count (section 4.1), and a non-UTC offset input normalizes.
		{"same-second trigger is skipped", "0 0 12 1/1 * ? *",
			time.Date(2026, 9, 3, 12, 0, 0, 0, time.UTC),
			time.Date(2026, 9, 4, 12, 0, 0, 0, time.UTC)},
		{"sub-second after floors to the second", "0 0 12 1/1 * ? *",
			time.Date(2026, 9, 3, 12, 0, 0, 500_000_000, time.UTC),
			time.Date(2026, 9, 4, 12, 0, 0, 0, time.UTC)},
		{"offset zone input answers UTC", "0 0 12 1/1 * ? *",
			time.Date(2026, 9, 3, 14, 0, 0, 0, time.FixedZone("CEST", 2*3600)),
			time.Date(2026, 9, 4, 12, 0, 0, 0, time.UTC)},
		// Seconds and minute steps.
		{"seconds within the minute", "30 * * * * ?",
			time.Date(2026, 9, 3, 8, 0, 10, 0, time.UTC),
			time.Date(2026, 9, 3, 8, 0, 30, 0, time.UTC)},
		{"seconds roll to the next minute", "30 * * * * ?",
			time.Date(2026, 9, 3, 8, 0, 30, 0, time.UTC),
			time.Date(2026, 9, 3, 8, 1, 30, 0, time.UTC)},
		{"start/step seconds", "5/15 * * * * ?",
			time.Date(2026, 9, 3, 8, 0, 6, 0, time.UTC),
			time.Date(2026, 9, 3, 8, 0, 20, 0, time.UTC)},
		{"*/15 minutes", "0 */15 * * * ?",
			time.Date(2026, 9, 3, 8, 7, 0, 0, time.UTC),
			time.Date(2026, 9, 3, 8, 15, 0, 0, time.UTC)},
		// L: the last day of the month (September has 30 days), then the
		// hop into October (31 days).
		{"last day of month", "0 0 2 L * ?", thu,
			time.Date(2026, 9, 30, 2, 0, 0, 0, time.UTC)},
		{"last day of month rolls over", "0 0 2 L * ?",
			time.Date(2026, 9, 30, 2, 0, 0, 0, time.UTC),
			time.Date(2026, 10, 31, 2, 0, 0, 0, time.UTC)},
		// nL: 6 = Friday; the last Friday of September 2026 is the 25th
		// (Fridays: 4, 11, 18, 25), of October the 30th (2, 9, 16, 23, 30).
		{"last Friday of month", "0 0 2 ? * 6L", thu,
			time.Date(2026, 9, 25, 2, 0, 0, 0, time.UTC)},
		{"last Friday of next month", "0 0 2 ? * 6L",
			time.Date(2026, 9, 25, 2, 0, 1, 0, time.UTC),
			time.Date(2026, 10, 30, 2, 0, 0, 0, time.UTC)},
		{"last Sunday of month by number", "0 0 2 ? * 1L", thu,
			time.Date(2026, 9, 27, 2, 0, 0, 0, time.UTC)}, // Sundays: 6,13,20,27
		// Names and lists.
		{"monday-wednesday-friday list", "0 0 2 ? * MON,WED,FRI", thu,
			time.Date(2026, 9, 4, 2, 0, 0, 0, time.UTC)}, // Friday the 4th
		{"month name list", "0 0 2 ? JAN,DEC *", thu,
			time.Date(2026, 12, 1, 2, 0, 0, 0, time.UTC)},
		{"month wrap crosses the year", "0 0 2 ? AUG *", thu,
			time.Date(2027, 8, 1, 2, 0, 0, 0, time.UTC)},
		// The sparsest legal shape: February 29 — the bounded cascade
		// resolves a four-year gap without a second-by-second walk.
		{"february 29 leaps", "0 0 2 29 FEB ?", thu,
			time.Date(2028, 2, 29, 2, 0, 0, 0, time.UTC)},
		// Day-of-month with day-of-week "?" (the mutex's other side).
		{"day-of-month number", "0 0 2 15 * ?", thu,
			time.Date(2026, 9, 15, 2, 0, 0, 0, time.UTC)},
		{"day-of-month number next month", "0 0 2 1 * ?",
			time.Date(2026, 9, 1, 2, 0, 1, 0, time.UTC),
			time.Date(2026, 10, 1, 2, 0, 0, 0, time.UTC)},
		// Hours wrap the day; minutes wrap the hour.
		{"late hour rolls the day", "0 0 23 ? * *",
			time.Date(2026, 9, 3, 23, 30, 0, 0, time.UTC),
			time.Date(2026, 9, 4, 23, 0, 0, 0, time.UTC)},
		{"minute rolls the hour", "0 45 * ? * *",
			time.Date(2026, 9, 3, 8, 45, 0, 0, time.UTC),
			time.Date(2026, 9, 3, 9, 45, 0, 0, time.UTC)},
		// Year range: the first allowed year after the instant.
		{"year range starts at its floor", "0 0 2 ? * * 2050-2060", thu,
			time.Date(2050, 1, 1, 2, 0, 0, 0, time.UTC)},
		{"year list picks the next member", "0 0 2 ? * * 2050,2060",
			time.Date(2050, 12, 31, 3, 0, 0, 0, time.UTC),
			time.Date(2060, 1, 1, 2, 0, 0, 0, time.UTC)},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			got, err := scheduler.Next(tc.expr, tc.after)
			if err != nil {
				t.Fatalf("Next(%q, %s) = %v, want %s", tc.expr, tc.after, err, tc.want)
			}
			if !got.Equal(tc.want) {
				t.Errorf("Next(%q, %s) = %s, want %s", tc.expr, tc.after, got, tc.want)
			}
			if got.Location() != time.UTC {
				t.Errorf("Next(%q) location = %v, want UTC", tc.expr, got.Location())
			}
		})
	}
}

// TestNextRunDeterministic: the calculator is a pure function — the same
// inputs must always yield the same output (NFR-P74's parity assertion is
// expressible exactly because of this).
func TestNextRunDeterministic(t *testing.T) {
	for _, expr := range []string{"0 0 2 ? * MON-FRI", "0 0 /4 * * ?", "0 0 2 L * ?"} {
		first, err := scheduler.Next(expr, thu)
		if err != nil {
			t.Fatalf("Next(%q): %v", expr, err)
		}
		for i := 0; i < 100; i++ {
			again, err := scheduler.Next(expr, thu)
			if err != nil || !again.Equal(first) {
				t.Fatalf("Next(%q) not deterministic: %s vs %s (%v)", expr, again, first, err)
			}
		}
	}
}

// TestNextRunChain walks one expression forward through its own next runs:
// the sequence of triggers must satisfy the expression every step and
// strictly increase — the coarser cross-check behind the table above.
func TestNextRunChain(t *testing.T) {
	exprs := map[string]string{
		"daily":      "0 0 2 ? * *",
		"weekdays":   "0 0 2 ? * MON-FRI",
		"every4h":    "0 0 /4 * * ?",
		"month-last": "0 0 2 L * ?",
	}
	for name, expr := range exprs {
		p, err := scheduler.Parse(expr)
		if err != nil {
			t.Fatalf("%s: parse: %v", name, err)
		}
		t.Run(name, func(t *testing.T) {
			cur := thu
			var prev time.Time
			for i := 0; i < 40; i++ {
				next, err := p.Next(cur)
				if err != nil {
					t.Fatalf("step %d: %v", i, err)
				}
				if i > 0 && !next.After(prev) {
					t.Fatalf("step %d: %s not after %s", i, next, prev)
				}
				// The trigger must be strictly after its seed and satisfy
				// every field — spot-verified through one re-Next call
				// landing strictly later.
				recheck, err := p.Next(next.Add(-time.Second))
				if err != nil || !recheck.Equal(next) {
					t.Fatalf("step %d: %s does not re-derive (got %s, %v)", i, next, recheck, err)
				}
				prev, cur = next, next
			}
		})
	}
}

// TestNextRunUnreachable: expressions whose triggers are all in the past
// of the instant answer ErrUnreachable — the PUT-time refusal arm.
func TestNextRunUnreachable(t *testing.T) {
	for _, expr := range []string{
		"0 0 2 ? * * 2020", // a past year domain
		"0 0 2 30 2 ?",     // February 30, any year
	} {
		if _, err := scheduler.Next(expr, thu); !errors.Is(err, scheduler.ErrUnreachable) {
			t.Errorf("Next(%q) = %v, want ErrUnreachable", expr, err)
		}
	}
	// The horizon itself: past 2099 nothing is reachable.
	if _, err := scheduler.Next("0 0 2 ? * * *", time.Date(2101, 1, 1, 0, 0, 0, 0, time.UTC)); !errors.Is(err, scheduler.ErrUnreachable) {
		t.Errorf("Next past the year horizon = %v, want ErrUnreachable", err)
	}
}

// TestValidateExprAndPastNextRun: the two PUT-time guards (ADR-0044
// decision 9's parse-time arms): never-reachable refusal and past-time
// refusal of an explicit next-run field.
func TestValidateExprAndPastNextRun(t *testing.T) {
	next, err := scheduler.ValidateExpr("0 0 2 ? * MON-FRI", thu)
	if err != nil {
		t.Fatalf("ValidateExpr: %v", err)
	}
	if want := time.Date(2026, 9, 4, 2, 0, 0, 0, time.UTC); !next.Equal(want) {
		t.Errorf("ValidateExpr next = %s, want %s", next, want)
	}
	if _, err := scheduler.ValidateExpr("0 0 2 ? * * 2020", thu); !errors.Is(err, scheduler.ErrUnreachable) {
		t.Errorf("ValidateExpr(past year) = %v, want ErrUnreachable", err)
	}
	if _, err := scheduler.ValidateExpr("nonsense", thu); !errors.Is(err, scheduler.ErrInvalidExpr) {
		t.Errorf("ValidateExpr(nonsense) = %v, want ErrInvalidExpr", err)
	}

	past := thu.Add(-time.Hour)
	if err := scheduler.CheckNextRunAt(past, thu); !errors.Is(err, scheduler.ErrPastNextRun) {
		t.Errorf("CheckNextRunAt(past) = %v, want ErrPastNextRun", err)
	}
	if err := scheduler.CheckNextRunAt(thu, thu); !errors.Is(err, scheduler.ErrPastNextRun) {
		t.Errorf("CheckNextRunAt(equal) = %v, want ErrPastNextRun (now is not the future)", err)
	}
	if err := scheduler.CheckNextRunAt(thu.Add(time.Minute), thu); err != nil {
		t.Errorf("CheckNextRunAt(future) = %v, want nil", err)
	}
}

// TestNextRunPerformanceLeavesEvidence: the ticket's P95 magnitude
// evidence — parse + next over the factory expression set (plus the two
// sparse worst cases) ten thousand times. Asserted at a generous ceiling
// (the cascade is bounded, so even -race has orders of magnitude of
// headroom) and logged for the report.
func TestNextRunPerformanceLeavesEvidence(t *testing.T) {
	if testing.Short() {
		t.Skip("perf evidence is a full-suite leg")
	}
	exprs := []string{
		"0 0 2 ? * MON-FRI", "0 0 2 ? * SAT", "0 23 5 * * ?",
		"0 0 /4 * * ?", "0 12 5 * * ?", "0 0 0 ? * * 2099",
		"0 0 2 L * ?", "0 0 2 ? * 6L", "0 0 2 29 FEB ?",
	}
	const rounds = 10000
	durations := make([]time.Duration, 0, rounds)
	after := thu
	for i := 0; i < rounds; i++ {
		expr := exprs[i%len(exprs)]
		start := time.Now()
		if _, err := scheduler.Next(expr, after); err != nil {
			t.Fatalf("Next(%q): %v", expr, err)
		}
		durations = append(durations, time.Since(start))
	}
	sort.Slice(durations, func(i, j int) bool { return durations[i] < durations[j] })
	p50, p95, p100 := durations[rounds/2], durations[rounds*95/100], durations[rounds-1]
	t.Logf("parse+next over %d rounds (9-expression mix incl. Feb-29 sparse case): p50=%s p95=%s max=%s",
		rounds, p50, p95, p100)
	if limit := 500 * time.Microsecond; p95 > limit {
		t.Errorf("p95 parse+next = %s, want <= %s", p95, limit)
	}
	// Human-readable magnitude evidence for the report.
	fmt.Printf("scheduler P95 evidence: %d parse+next rounds: p50=%s p95=%s max=%s\n",
		rounds, p50, p95, p100)
}
