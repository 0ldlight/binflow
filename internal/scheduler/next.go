package scheduler

import (
	"fmt"
	"time"
)

// The next-run calculator (M16 T-446, FR-150.1 / ADR-0044 decision 4,
// anchor cron-scheduling.md section 4): a pure function of (expression,
// after), UTC, second resolution, deterministic — the same inputs always
// yield the same output, which is what makes the table-driven parity
// assertions of NFR-P74 expressible.
//
// Semantics (anchor section 4.1/4.2): the FIRST trigger STRICTLY AFTER the
// given instant — a trigger falling on the current second does not count,
// the calculator jumps to the one after it. There is no "last trigger"
// anchoring: callers hand in now, the answer is the future.

// ErrUnreachable marks an expression with no trigger after the given
// instant within the year domain's span (a past year domain, a February 30,
// a 2099-capped never-fire reached from 2100): the PUT-time validation
// refuses to store such an expression (ADR-0044 decision 9's
// never-reachable arm).
var ErrUnreachable = fmt.Errorf("scheduler: cron expression has no future trigger")

// ErrPastNextRun marks an explicitly configured next-run timestamp that
// lies in the past (ADR-0044 decision 9's past-time arm: the backup face's
// writable next-time field must refuse yesterday).
var ErrPastNextRun = fmt.Errorf("scheduler: next-run time is in the past")

// lastSearch is the calculator's horizon: one second past the year domain's
// legal maximum. The comparison is exclusive — a trigger AT 2099-12-31
// 23:59:59 is reachable, one beyond never is.
var lastSearch = time.Date(maxYear+1, 1, 1, 0, 0, 0, 0, time.UTC)

// Next parses expr and returns its first trigger strictly after `after`.
func Next(expr string, after time.Time) (time.Time, error) {
	p, err := Parse(expr)
	if err != nil {
		return time.Time{}, err
	}
	return p.Next(after)
}

// Next returns p's first trigger strictly after `after`, in UTC at second
// resolution. The cascade advances one calendar unit per mismatch (year →
// month → day → hour → minute → second), so even the sparsest legal
// expression — February 29, a single far-future year — resolves in bounded
// steps instead of a second-by-second walk.
func (p *Parsed) Next(after time.Time) (time.Time, error) {
	t := after.UTC().Truncate(time.Second).Add(time.Second)
	for t.Before(lastSearch) {
		if y, ok := p.year.nextFrom(t.Year()); !ok {
			break // no allowed year at or after this one: unreachable
		} else if y != t.Year() {
			t = time.Date(y, 1, 1, 0, 0, 0, 0, time.UTC)
			continue
		}
		if m, ok := p.month.nextFrom(int(t.Month())); !ok {
			// Roll the year: the loop's year arm re-runs.
			t = time.Date(t.Year()+1, 1, 1, 0, 0, 0, 0, time.UTC)
			continue
		} else if m != int(t.Month()) {
			t = time.Date(t.Year(), time.Month(m), 1, 0, 0, 0, 0, time.UTC)
			continue
		}
		if !p.dayMatches(t) {
			t = time.Date(t.Year(), t.Month(), t.Day(), 0, 0, 0, 0, time.UTC).AddDate(0, 0, 1)
			continue
		}
		if h, ok := p.hour.nextFrom(t.Hour()); !ok {
			t = nextDayStart(t)
			continue
		} else if h != t.Hour() {
			t = time.Date(t.Year(), t.Month(), t.Day(), h, 0, 0, 0, time.UTC)
			continue
		}
		if m, ok := p.min.nextFrom(t.Minute()); !ok {
			t = time.Date(t.Year(), t.Month(), t.Day(), t.Hour()+1, 0, 0, 0, time.UTC)
			continue
		} else if m != t.Minute() {
			t = time.Date(t.Year(), t.Month(), t.Day(), t.Hour(), m, 0, 0, time.UTC)
			continue
		}
		if s, ok := p.sec.nextFrom(t.Second()); !ok {
			t = time.Date(t.Year(), t.Month(), t.Day(), t.Hour(), t.Minute()+1, 0, 0, time.UTC)
			continue
		} else if s != t.Second() {
			t = time.Date(t.Year(), t.Month(), t.Day(), t.Hour(), t.Minute(), s, 0, time.UTC)
			continue
		}
		return t, nil
	}
	return time.Time{}, fmt.Errorf("%w before %s (year domain capped at %d)", ErrUnreachable, lastSearch.Format(time.RFC3339), maxYear)
}

// nextFrom returns the smallest allowed value >= v (ok=false when every
// allowed value is below v).
func (f fieldSpec) nextFrom(v int) (int, bool) {
	for _, allowed := range f.values {
		if allowed >= v {
			return allowed, true
		}
	}
	return 0, false
}

// nextDayStart moves to the first second of the following day.
func nextDayStart(t time.Time) time.Time {
	t = time.Date(t.Year(), t.Month(), t.Day(), 0, 0, 0, 0, time.UTC)
	return t.AddDate(0, 0, 1)
}

// dayMatches reports whether t's calendar day satisfies the day fields.
// Exactly one of dom/dow is "?" (Parse enforces the mutex), so exactly one
// side constrains; both are consulted anyway so a future relaxation of the
// mutex cannot silently drop a constraint.
func (p *Parsed) dayMatches(t time.Time) bool {
	if !p.dom.unset && !p.dom.matchDay(t) {
		return false
	}
	if !p.dow.unset && !p.dow.matchDOW(t) {
		return false
	}
	return true
}

// matchDay: the day-of-month side (a value set, or L = last day of month).
func (f fieldSpec) matchDay(t time.Time) bool {
	if f.lastDOM {
		return t.Day() == lastDayOfMonth(t.Year(), t.Month())
	}
	return contains(f.values, t.Day())
}

// matchDOW: the day-of-week side (a value set, or nL = the month's last
// weekday-n). Quartz numbers 1=SUN..7=SAT; Go's Weekday is 0=SUN..6=SAT.
func (f fieldSpec) matchDOW(t time.Time) bool {
	if f.lastDOW {
		target := time.Weekday(f.values[0] - 1)
		return t.Weekday() == target && t.Day() > lastDayOfMonth(t.Year(), t.Month())-7
	}
	return contains(f.values, int(t.Weekday())+1)
}

// lastDayOfMonth: day 0 of the following month (Go normalizes the spill).
func lastDayOfMonth(year int, month time.Month) int {
	return time.Date(year, month+1, 0, 0, 0, 0, 0, time.UTC).Day()
}

func contains(values []int, v int) bool {
	for _, allowed := range values {
		if allowed == v {
			return true
		}
	}
	return false
}

// ValidateExpr is the PUT-time contract (ADR-0044 decision 4): an
// expression is storable only when it parses AND has a trigger after now.
// It returns that next run so the config surface can persist it in the
// same write.
func ValidateExpr(expr string, now time.Time) (time.Time, error) {
	p, err := Parse(expr)
	if err != nil {
		return time.Time{}, err
	}
	next, err := p.Next(now)
	if err != nil {
		return time.Time{}, fmt.Errorf("%w: %q: %w", ErrUnreachable, expr, err)
	}
	return next, nil
}

// CheckNextRunAt refuses an explicit next-run timestamp in the past or in
// the present second (ADR-0044 decision 9: past-time configuration refusal
// — the writable next-time field of the backup face). Equal timestamps are
// refused too: a schedule "starting now" is expressed through the cron
// expression, never through a next-run pinned to the current second.
func CheckNextRunAt(at, now time.Time) error {
	if !at.After(now) {
		return fmt.Errorf("%w: %s is not after %s",
			ErrPastNextRun, at.Format(time.RFC3339), now.Format(time.RFC3339))
	}
	return nil
}
