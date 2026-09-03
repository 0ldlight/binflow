package scheduler

import (
	"fmt"
	"sort"
	"strconv"
	"strings"
)

// The Quartz six-domain cron subset (M16 T-446, FR-150.1 / ADR-0044
// decision 3, calibrated against the T-435 anchor docs/reverse/
// cron-scheduling.md sections 1 and 2). Pure functions, zero IO, zero
// database: parsing is a closed-set string-to-structure translation, so the
// injection surface NFR-S79 worries about is structurally closed (no eval,
// no concatenation, nothing but the closed alphabet ever accepted).
//
// Grammar (whitespace-separated, strings.Fields tokenization — 6 or 7
// fields; a 5-field Unix cron is rejected at the field-count check the way
// Quartz itself refuses it):
//
//	seconds      0-59            | , - * /
//	minutes      0-59            | , - * /
//	hours        0-23            | , - * /   (bare "/N" steps accepted — the
//	                                        factory GC template 0 0 /4 * * ?
//	                                        shape, anchor C-d)
//	day-of-month 1-31            | , - * / ? L    (L = last day of month)
//	month        1-12 JAN-DEC    | , - * /   (case-insensitive)
//	day-of-week  1-7 SUN-SAT     | , - * / ? nL   (1=SUN; nL = last weekday-n)
//	year (opt.)  1970-2099       | , - * /
//
// Honest-subset rejections (every error names the rejected form, the anchor
// section 3 posture): W (nearest weekday), # (nth weekday), LW, L-<offset>,
// mixed name/number ranges, day-of-week digit 0, and any character outside
// the closed alphabet. The year domain carries the anchor's full 1970-2099
// legality (cron-scheduling.md section 1 table): CRON_NEVER
// "0 0 0 ? * * 2099" is a factory default there, so concrete years must
// parse — the T-435 soft-seam override of ADR-0044's "year only *"
// sketch, registered in the T-446 report for the errata lap.
//
// The day-of-month / day-of-week mutex (anchor section 1: one of the two
// must be "?"): exactly one of them carries "?"; both-specified and
// both-"?" are rejected.

const (
	// MaxExprLen is the expression length gate (ADR-0044 decision 11: an
	// engine-internal constant, deliberately not a config key).
	MaxExprLen = 256
	// minYear/maxYear are the year domain's legal span, the anchor's
	// Quartz table verbatim; Next never searches past maxYear.
	minYear = 1970
	maxYear = 2099
)

// The consuming domains of the schedules ledger (the 021 CHECK's closed
// set, ADR-0044 decision 1: a new domain is an incremental registration —
// a Register call plus one CHECK-list migration — never a schema change).
const (
	DomainMaintenance = "maintenance"
	DomainBackup      = "backup"
	DomainReplication = "replication"
)

// Domains returns the closed domain set (Register's validator and the
// closed-set pin the tests assert).
func Domains() []string {
	return []string{DomainMaintenance, DomainBackup, DomainReplication}
}

// ErrInvalidExpr marks every parse rejection: the REST face maps it onto
// the anchor's 400 family (cron-scheduling.md section 3) via errors.Is.
var ErrInvalidExpr = fmt.Errorf("scheduler: invalid cron expression")

// fieldSpec is one parsed field: the allowed values (sorted ascending), or
// one of the three single-element specials.
type fieldSpec struct {
	values []int
	// unset is "?" (day-of-month / day-of-week only): the field constrains
	// nothing — the other day field owns day selection.
	unset bool
	// lastDOM: the field is exactly "L" (day-of-month only).
	lastDOM bool
	// lastDOW: the field is exactly "nL" (day-of-week only); values[0]=n.
	lastDOW bool
}

// Parsed is one compiled expression. Build with Parse, query with Next. A
// Parsed is immutable and safe for concurrent use.
type Parsed struct {
	sec, min, hour, month, year fieldSpec
	dom, dow                    fieldSpec
}

var monthNames = map[string]int{
	"JAN": 1, "FEB": 2, "MAR": 3, "APR": 4, "MAY": 5, "JUN": 6,
	"JUL": 7, "AUG": 8, "SEP": 9, "OCT": 10, "NOV": 11, "DEC": 12,
}

var dowNames = map[string]int{
	"SUN": 1, "MON": 2, "TUE": 3, "WED": 4, "THU": 5, "FRI": 6, "SAT": 7,
}

// fieldDef is one field's parsing rules.
type fieldDef struct {
	name   string // error text: "seconds", "day-of-month", ...
	lo, hi int
	// names enables the month/day-of-week abbreviations.
	names map[string]int
	// question/L enable the day fields' specials.
	question bool
	l        bool
}

var fieldDefs = []fieldDef{
	{name: "seconds", lo: 0, hi: 59},
	{name: "minutes", lo: 0, hi: 59},
	{name: "hours", lo: 0, hi: 23},
	{name: "day-of-month", lo: 1, hi: 31, question: true, l: true},
	{name: "month", lo: 1, hi: 12, names: monthNames},
	{name: "day-of-week", lo: 1, hi: 7, names: dowNames, question: true, l: true},
	{name: "year", lo: minYear, hi: maxYear},
}

// Parse compiles one expression. The returned error wraps ErrInvalidExpr
// and names the offending field and form — a rejection the operator can
// act on, not a bare "invalid".
func Parse(expr string) (*Parsed, error) {
	if expr == "" {
		return nil, fmt.Errorf("%w: expression is empty", ErrInvalidExpr)
	}
	if len(expr) > MaxExprLen {
		return nil, fmt.Errorf("%w: expression exceeds %d characters", ErrInvalidExpr, MaxExprLen)
	}
	parts := strings.Fields(expr)
	if len(parts) < 6 || len(parts) > 7 {
		return nil, fmt.Errorf("%w: got %d fields, want 6 or 7 (a 5-field Unix cron is not a Quartz expression)",
			ErrInvalidExpr, len(parts))
	}
	var out [7]fieldSpec
	for i, def := range fieldDefs[:len(parts)] {
		spec, err := parseField(parts[i], def)
		if err != nil {
			return nil, err
		}
		out[i] = spec
	}
	if len(parts) == 6 {
		out[6] = fullRange(fieldDefs[6]) // no year domain: every year
	}
	// The day mutex (anchor section 1): exactly one of day-of-month /
	// day-of-week is "?" — both specified is the residual-support form
	// Quartz refuses; both "?" leaves nobody owning day selection.
	switch {
	case !out[3].unset && !out[5].unset:
		return nil, fmt.Errorf("%w: day-of-month and day-of-week are both specified — exactly one must be \"?\"",
			ErrInvalidExpr)
	case out[3].unset && out[5].unset:
		return nil, fmt.Errorf("%w: day-of-month and day-of-week are both \"?\" — exactly one must carry a value",
			ErrInvalidExpr)
	}
	return &Parsed{
		sec: out[0], min: out[1], hour: out[2],
		dom: out[3], month: out[4], dow: out[5], year: out[6],
	}, nil
}

// fullRange builds the every-value field of def.
func fullRange(def fieldDef) fieldSpec {
	values := make([]int, 0, def.hi-def.lo+1)
	for v := def.lo; v <= def.hi; v++ {
		values = append(values, v)
	}
	return fieldSpec{values: values}
}

// parseField parses one field against its rules. Specials first (? and the
// L family, each a named accept-or-reject), then the value/list grammar.
func parseField(text string, def fieldDef) (fieldSpec, error) {
	bad := func(reason string, args ...any) error {
		return fmt.Errorf("%w: %s field %q: %s", ErrInvalidExpr, def.name, text, fmt.Sprintf(reason, args...))
	}
	if text == "?" {
		if !def.question {
			return fieldSpec{}, bad("\"?\" is only valid in day-of-month or day-of-week")
		}
		return fieldSpec{unset: true}, nil
	}
	// The L family (day fields only). "L" in day-of-month and "nL" in
	// day-of-week are the accepted pair; every other L-bearing token gets
	// a pointed rejection (LW, L-3, lists, bare L in day-of-week).
	if def.l && strings.HasSuffix(text, "L") {
		if text == "L" && def.name == "day-of-month" {
			return fieldSpec{lastDOM: true, values: []int{def.hi}}, nil
		}
		if def.name == "day-of-week" && text != "L" {
			if n, err := strconv.Atoi(strings.TrimSuffix(text, "L")); err == nil {
				if n < def.lo || n > def.hi {
					return fieldSpec{}, bad("weekday %d out of range %d-%d", n, def.lo, def.hi)
				}
				return fieldSpec{lastDOW: true, values: []int{n}}, nil
			}
		}
		switch {
		case text == "LW":
			return fieldSpec{}, bad("LW (last weekday) is not supported")
		case strings.HasPrefix(text, "L-"):
			return fieldSpec{}, bad("L-<offset> (offset from the last day) is not supported")
		case strings.Contains(text, ","):
			return fieldSpec{}, bad("L forms must stand alone — they cannot combine with lists")
		case text == "L" && def.name == "day-of-week":
			return fieldSpec{}, bad("bare L is only valid in day-of-month (use nL for the last weekday-n)")
		default:
			return fieldSpec{}, bad("unsupported L form (supported: L in day-of-month, nL in day-of-week)")
		}
	}

	spec := fieldSpec{}
	for _, el := range strings.Split(text, ",") {
		if el == "" {
			return fieldSpec{}, bad("empty list element")
		}
		lo, hi, step, err := parseElement(el, def, text)
		if err != nil {
			return fieldSpec{}, err
		}
		for v := lo; v <= hi; v += step {
			spec.values = append(spec.values, v)
		}
	}
	if len(spec.values) == 0 {
		return fieldSpec{}, bad("no values")
	}
	spec.values = sortedUnique(spec.values)
	return spec, nil
}

// parseElement parses one list element — "*", a bare "/n" step, "a", "a/n",
// "a-b", "a-b/n" — returning the [lo, hi] span and the step. "a/n" spans
// a..field-max (Quartz's start/step form); bare "/n" spans field-min..max
// (the factory 0 0 /4 * * ? hours shape).
func parseElement(el string, def fieldDef, fieldText string) (lo, hi, step int, err error) {
	bad := func(reason string, args ...any) (int, int, int, error) {
		return 0, 0, 0, fmt.Errorf("%w: %s field %q: element %q: %s",
			ErrInvalidExpr, def.name, fieldText, el, fmt.Sprintf(reason, args...))
	}
	// The named special-character rejections come before the generic
	// alphabet check so the message points at the form, not the character.
	// The W arm matches a numeric (or L) prefix only — "WED" is a weekday
	// name, not a nearest-weekday token.
	if strings.Contains(el, "#") {
		return bad("n#m (nth weekday) is not supported")
	}
	if strings.HasSuffix(el, "W") {
		if prefix := strings.TrimSuffix(el, "W"); prefix == "L" {
			return bad("LW (last weekday) is not supported")
		} else if _, err := strconv.Atoi(prefix); err == nil {
			return bad("nW (nearest weekday) is not supported")
		}
	}
	if strings.HasPrefix(el, "L-") {
		return bad("L-<offset> (offset from the last day) is not supported")
	}
	if !def.l && def.names == nil && strings.HasSuffix(el, "L") {
		// A bare-L element outside the day fields (month/dow names like
		// JUL never land here — their fields carry the names map).
		return bad("L is only valid in day-of-month or day-of-week")
	}
	if def.l && strings.HasSuffix(el, "L") {
		return bad("L forms must stand alone — they cannot combine with lists")
	}
	stepParts := strings.Split(el, "/")
	if len(stepParts) > 2 {
		return bad("multiple steps")
	}
	step = 1
	if len(stepParts) == 2 {
		n, err := strconv.Atoi(stepParts[1])
		if err != nil || n < 1 {
			return bad("step must be an integer >= 1")
		}
		step = n
	}
	rangePart := stepParts[0]
	switch {
	case rangePart == "*" || rangePart == "":
		// "*/n" and the bare "/n" both span the whole field.
		return def.lo, def.hi, step, nil
	case strings.Contains(rangePart, "-"):
		bounds := strings.Split(rangePart, "-")
		if len(bounds) != 2 {
			return bad("malformed range")
		}
		alo, aname, err := parseValue(bounds[0], def)
		if err != nil {
			return bad("%s", err.Error())
		}
		bhi, bname, err := parseValue(bounds[1], def)
		if err != nil {
			return bad("%s", err.Error())
		}
		if aname != bname {
			return bad("mixed name and number in range")
		}
		if alo > bhi {
			return bad("range %d-%d is inverted", alo, bhi)
		}
		return alo, bhi, step, nil
	default:
		v, _, err := parseValue(rangePart, def)
		if err != nil {
			return bad("%s", err.Error())
		}
		if len(stepParts) == 2 {
			return v, def.hi, step, nil // "a/n" spans a..field-max
		}
		return v, v, step, nil
	}
}

// parseValue parses one value token: a decimal number (leading zeros fine)
// or, in the name-carrying fields, a case-insensitive abbreviation.
func parseValue(tok string, def fieldDef) (v int, isName bool, err error) {
	if n, err := strconv.Atoi(tok); err == nil {
		if n < def.lo || n > def.hi {
			return 0, false, fmt.Errorf("value %d out of range %d-%d", n, def.lo, def.hi)
		}
		return n, false, nil
	}
	if def.names != nil {
		if n, ok := def.names[strings.ToUpper(tok)]; ok {
			return n, true, nil
		}
		return 0, false, fmt.Errorf("%q is not a known %s name", tok, def.name)
	}
	if isAlpha(tok) {
		return 0, false, fmt.Errorf("the %s field does not accept names", def.name)
	}
	return 0, false, fmt.Errorf("%q is not a valid value (closed alphabet: digits, names where allowed, and , - * / ?)", tok)
}

// isAlpha reports whether s is all ASCII letters.
func isAlpha(s string) bool {
	for _, r := range s {
		if (r < 'a' || r > 'z') && (r < 'A' || r > 'Z') {
			return false
		}
	}
	return s != ""
}

// sortedUnique returns values ascending without duplicates (a list like
// "5,5-10" is legal Quartz; the set form collapses it).
func sortedUnique(values []int) []int {
	sort.Ints(values)
	out := values[:1]
	for _, v := range values[1:] {
		if v != out[len(out)-1] {
			out = append(out, v)
		}
	}
	return out
}
