package scheduler_test

import (
	"errors"
	"strings"
	"testing"

	"github.com/lzwzzy/binflow/internal/scheduler"
)

// The expression subset's accept/reject tables (M16 T-446 AC3): every
// accepted form is an evidenced shape (the factory templates and official
// examples of docs/reverse/cron-scheduling.md sections 1 and 2, the
// seven-field replication form of replication.md section 9.2-C-9), every
// rejection carries a pointed message naming the refused form (the anchor
// section 3 posture — the C-a four arms among them).

func TestCronExprAcceptedForms(t *testing.T) {
	accepted := []string{
		// The factory templates, verbatim (cron-scheduling.md section 2).
		"0 0 2 ? * MON-FRI", // backup-daily
		"0 0 2 ? * SAT",     // backup-weekly
		"0 23 5 * * ?",      // GC-adjacent daily 23:05 shape
		"0 0 /4 * * ?",      // GC default: bare /N step in hours (C-d)
		"0 12 5 * * ?",      // cleanup daily 05:12
		"0 12 0 * * ?",      // virtual cache cleanup 00:12
		"0 0 0 ? * * 2099",  // CRON_NEVER — concrete year (section 2's table)
		// The seven-field replication shape (replication.md section 9.2-C-9).
		"0 0 12 1/1 * ? *",
		"0 0 12 1/1 * ? 2050",
		// Case-insensitive month and weekday abbreviations (section 1).
		"0 0 2 ? jan-dec *",
		"0 0 2 ? * mon,wed,fri",
		"0 0 2 ? * sat",
		// Steps: */N, start/N, range/N, single values, ranges, lists.
		"*/15 * * * * ?",
		"5/15 * * ? * *",
		"10-20/2 * * ? * *",
		"0,30 * * ? * *",
		"5-5 * * ? * *",
		"0 0 2 15 JAN ?",
		"0 0 2 ? JAN,DEC *",
		// L (last day of month) and nL (last weekday-n) — the month-end
		// backup forms ADR-0044 decision 3 admits.
		"0 0 2 L * ?",
		"0 0 2 ? * 6L",
		"0 0 2 ? * 7L",
		// The day mutex's both evidencable shapes: day-of-month carries "?",
		// and day-of-week carries "?".
		"0 0 2 ? * *",
		"0 0 2 * * ?",
		"0 0 2 * * ? 2099", // the "?" side with the year domain present
		// Year domain range and list forms (section 1's table: , - * /).
		"0 0 2 ? * * 2050-2060",
		"0 0 2 ? * * 2050,2060",
		"0 0 2 ? * * */10",
		// Whitespace tolerance (Fields tokenization).
		"0   0  2  ?  *  MON-FRI",
	}
	for _, expr := range accepted {
		if _, err := scheduler.Parse(expr); err != nil {
			t.Errorf("Parse(%q) = %v, want accepted", expr, err)
		}
	}
}

func TestCronExprRejectedForms(t *testing.T) {
	rejected := []struct {
		expr string
		want string // a substring of the pointed rejection message
	}{
		// Field count: the 5-field Unix form is not a Quartz expression
		// (section 1's headline), and 8 fields is past the year domain.
		{"0 2 * * *", "5 fields"},
		{"0 0 2 ? * * 2050 2099", "8 fields"},
		{"", "empty"},
		{strings.Repeat("0 ", 128) + "0", "256 characters"},
		// W / # / LW / L-<offset>: the honest-subset rejections, each
		// named (section 6's C-layer difference registration).
		{"0 0 2 15W * ?", "nearest weekday"},
		{"0 0 2 ? * 6#3", "nth weekday"},
		{"0 0 2 LW * ?", "last weekday"},
		{"0 0 2 L-3 * ?", "offset from the last day"},
		// The day mutex: both specified, both "?".
		{"0 0 2 5 * MON", "both specified"},
		{"0 0 2 ? * ?", "both \"?\""},
		// Ranges and values.
		{"0 60 * * * ?", "out of range 0-59"},
		{"60 * * * * ?", "out of range 0-59"},
		{"* * 24 * * ?", "out of range 0-23"},
		{"0 0 2 0 * ?", "out of range 1-31"},
		{"0 0 2 32 * ?", "out of range 1-31"},
		{"0 0 2 ? 13 *", "out of range 1-12"},
		{"0 0 2 ? * 0", "out of range 1-7"},
		{"0 0 2 ? * 8", "out of range 1-7"},
		{"0 0 2 ? * 9L", "out of range 1-7"},
		{"0 0 2 ? * * 1969", "out of range 1970-2099"},
		{"0 0 2 ? * * 2100", "out of range 1970-2099"},
		{"*/0 * * * * ?", "step must be"},
		{"0 0 2 ? * MON-M0N", "not a known day-of-week name"},
		{"0 0 2 ? * MONN", "not a known day-of-week name"},
		{"0 0 2 ? MONN *", "not a known month name"},
		{"0 0 2 ? JAN-3 *", "mixed name and number"},
		{"0 0 2 ? 3-JAN *", "mixed name and number"},
		{"0 0 2 ? * MON-THU-MRI", "malformed range"},
		{"0 0 2 20-10 * ?", "inverted"},
		// "?" and L outside their fields, or combined with lists.
		{"0 ? * * * ?", "only valid in day-of-month or day-of-week"},
		{"0 0 L * * ?", "only valid in day-of-month"},
		{"0 0 2 ? * L", "bare L is only valid in day-of-month"},
		{"0 0 2 1,L * ?", "cannot combine with lists"},
		{"0 0 2 ? * 6L,MON", "cannot combine with lists"},
		{"0 0 2 L,5 * ?", "cannot combine with lists"},
		// Names in numeric fields.
		{"0 0 JAN * * ?", "does not accept names"},
		{"MON * * * * ?", "does not accept names"},
		{"0 0 2 JAN * ? 2050", "does not accept names"},
		// The closed alphabet.
		{"0 0 2 ? * M0N-FRI", "not a known day-of-week name"},
		{"0 0 $ * * ?", "not a valid value"},
		{"0 0 2 ? * MON;FRI", "not a known day-of-week name"},
		{"0 0 2 15,,16 * ?", "empty list element"},
	}
	for _, tc := range rejected {
		_, err := scheduler.Parse(tc.expr)
		if err == nil {
			t.Errorf("Parse(%q) = accepted, want rejected", tc.expr)
			continue
		}
		if !errors.Is(err, scheduler.ErrInvalidExpr) {
			t.Errorf("Parse(%q) error does not wrap ErrInvalidExpr: %v", tc.expr, err)
		}
		if !strings.Contains(err.Error(), tc.want) {
			t.Errorf("Parse(%q) error %q does not name the form %q", tc.expr, err.Error(), tc.want)
		}
	}
}

func TestCronDomainsClosedSet(t *testing.T) {
	got := scheduler.Domains()
	want := []string{scheduler.DomainMaintenance, scheduler.DomainBackup, scheduler.DomainReplication}
	if len(got) != len(want) {
		t.Fatalf("Domains() = %v, want %v", got, want)
	}
	for i := range want {
		if got[i] != want[i] {
			t.Fatalf("Domains() = %v, want %v", got, want)
		}
	}
}
