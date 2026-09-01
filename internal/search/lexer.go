package search

import (
	"fmt"
	"strconv"
	"strings"
	"unicode"
	"unicode/utf16"
	"unicode/utf8"
)

// Lexer for the AQL query text (T-409). The language is JSON-shaped but not
// JSON: object keys are bare identifiers ({"repo":"x"} — t407-evidence v1c),
// operators are $-prefixed words, numbers may be quoted or bare (aql.md §2.5
// syntax rules). The lexer is position-tracking so every failure can anchor
// the E1 residual segment.

type tokenKind int

const (
	tkEOF    tokenKind = iota
	tkIdent            // bare word: items, find, repo, sort, property, null...
	tkDollar           // $-prefixed word: $and, $eq, $asc...
	tkString           // double-quoted, escapes decoded
	tkNumber           // integer or decimal literal
	tkPunct            // one of { } [ ] ( ) , . : @ *
)

type token struct {
	kind tokenKind
	text string // ident word / decoded string / raw number / punct char
	pos  int    // byte offset of the first character
	// Number fields: intVal is set when the literal is integral (no fraction
	// or exponent), frac reports a non-integral literal.
	intVal int64
	frac   bool
}

func (t token) isPunct(c string) bool { return t.kind == tkPunct && t.text == c }

func (t token) isWord(w string) bool { return t.kind == tkIdent && t.text == w }

func (t token) isDollar(w string) bool { return t.kind == tkDollar && t.text == w }

// isOpWord matches an operator/logical word in either spelling: the quoted
// form "$eq" (what all recorded wire traffic uses — keys are JSON-style
// quoted strings, t407-evidence v1c) or the bare $eq (liberal arm).
func (t token) isOpWord(w string) bool {
	return t.isDollar(w) || (t.kind == tkString && t.text == w)
}

type lexer struct {
	query string
	pos   int
}

// lexAll tokenizes the whole query up front. AQL queries are capped at
// MaxQueryLen chars, so the token slice is bounded by construction.
func lexAll(query string) ([]token, *QueryError) {
	lx := &lexer{query: query}
	var toks []token
	for {
		t, err := lx.next()
		if err != nil {
			return nil, err
		}
		toks = append(toks, t)
		if t.kind == tkEOF {
			return toks, nil
		}
	}
}

func (l *lexer) errAt(pos int) *QueryError {
	return &QueryError{
		Kind:    ErrSyntax,
		Pos:     pos,
		Segment: l.query[pos:],
		Msg:     fmt.Sprintf(e1Format, l.query, l.query[pos:]),
	}
}

func (l *lexer) next() (token, *QueryError) {
	l.skipSpace()
	pos := l.pos
	if pos >= len(l.query) {
		return token{kind: tkEOF, pos: pos}, nil
	}
	c := l.query[pos]
	switch {
	case isIdentStart(rune(c)):
		return l.lexWord(pos, tkIdent), nil
	case c == '$':
		if pos+1 < len(l.query) && isIdentStart(rune(l.query[pos+1])) {
			l.pos++
			return l.lexWord(pos, tkDollar), nil
		}
		return token{}, l.errAt(pos)
	case c >= '0' && c <= '9', c == '-':
		return l.lexNumber(pos)
	case c == '"':
		return l.lexString(pos)
	case strings.ContainsRune("{}[](),.:@*", rune(c)):
		l.pos++
		return token{kind: tkPunct, text: string(c), pos: pos}, nil
	default:
		// Non-ASCII is only legal inside quoted strings; identifiers in the
		// AQL grammar are ASCII. Anything else here is a syntax error
		// anchored at the offending byte.
		return token{}, l.errAt(pos)
	}
}

// lexWord consumes an identifier. The language itself is ASCII (domains,
// fields, methods); values needing other scripts ride in quoted strings.
// pos is the token start — the '$' for dollar words, so text keeps its prefix.
func (l *lexer) lexWord(pos int, kind tokenKind) token {
	for l.pos < len(l.query) && isIdentPart(rune(l.query[l.pos])) {
		l.pos++
	}
	return token{kind: kind, text: l.query[pos:l.pos], pos: pos}
}

func (l *lexer) lexNumber(pos int) (token, *QueryError) {
	start := l.pos
	if l.query[l.pos] == '-' {
		l.pos++
	}
	digits := 0
	for l.pos < len(l.query) && isDigit(l.query[l.pos]) {
		l.pos++
		digits++
	}
	frac := false
	if l.pos < len(l.query) && l.query[l.pos] == '.' &&
		l.pos+1 < len(l.query) && isDigit(l.query[l.pos+1]) {
		frac = true
		l.pos++
		for l.pos < len(l.query) && isDigit(l.query[l.pos]) {
			l.pos++
		}
	}
	if digits == 0 {
		return token{}, l.errAt(pos)
	}
	// An exponent keeps the literal numeric but non-integral.
	if l.pos < len(l.query) && (l.query[l.pos] == 'e' || l.query[l.pos] == 'E') {
		p := l.pos + 1
		if p < len(l.query) && (l.query[p] == '+' || l.query[p] == '-') {
			p++
		}
		if p < len(l.query) && isDigit(l.query[p]) {
			frac = true
			l.pos = p
			for l.pos < len(l.query) && isDigit(l.query[l.pos]) {
				l.pos++
			}
		}
	}
	t := token{kind: tkNumber, text: l.query[start:l.pos], pos: pos, frac: frac}
	if !frac {
		var iv int64
		if n, err := fmt.Sscanf(t.text, "%d", &iv); n == 1 && err == nil {
			t.intVal = iv
		} else {
			t.frac = true // out-of-range integer: keep the raw text, reject later
		}
	}
	return t, nil
}

func (l *lexer) lexString(pos int) (token, *QueryError) {
	l.pos++ // opening quote
	var b strings.Builder
	for {
		if l.pos >= len(l.query) {
			return token{}, l.errAt(pos)
		}
		c := l.query[l.pos]
		switch {
		case c == '"':
			l.pos++
			return token{kind: tkString, text: b.String(), pos: pos}, nil
		case c == '\\':
			l.pos++
			if l.pos >= len(l.query) {
				return token{}, l.errAt(pos)
			}
			e := l.query[l.pos]
			l.pos++
			switch e {
			case '"', '\\', '/':
				b.WriteByte(e)
			case 'b':
				b.WriteByte('\b')
			case 'f':
				b.WriteByte('\f')
			case 'n':
				b.WriteByte('\n')
			case 'r':
				b.WriteByte('\r')
			case 't':
				b.WriteByte('\t')
			case 'u':
				r, err := l.lexUnicodeEscape()
				if err != nil {
					return token{}, err
				}
				b.WriteRune(r)
			default:
				return token{}, l.errAt(l.pos - 1)
			}
		case c < 0x20:
			// Raw control characters are not legal in AQL strings.
			return token{}, l.errAt(l.pos)
		default:
			b.WriteByte(c)
			l.pos++
		}
	}
}

// lexUnicodeEscape reads a 4-hex-digit \u escape, combining a surrogate pair
// when followed by a second \u escape (JSON semantics).
func (l *lexer) lexUnicodeEscape() (rune, *QueryError) {
	read4 := func() (rune, bool) {
		if l.pos+4 > len(l.query) {
			return 0, false
		}
		var v rune
		for i := 0; i < 4; i++ {
			d := hexVal(l.query[l.pos+i])
			if d < 0 {
				return 0, false
			}
			v = v*16 + rune(d) //nolint:gosec // G115: d is a bounded hex digit (0..15) by hexVal
		}
		l.pos += 4
		return v, true
	}
	v, ok := read4()
	if !ok {
		return 0, l.errAt(l.pos)
	}
	if utf16.IsSurrogate(rune(v)) {
		if l.pos+6 <= len(l.query) && l.query[l.pos] == '\\' && l.query[l.pos+1] == 'u' {
			save := l.pos
			l.pos += 2
			if lo, ok2 := read4(); ok2 && utf16.IsSurrogate(lo) {
				if r := utf16.DecodeRune(rune(v), lo); r != utf8.RuneError {
					return r, nil
				}
				return utf8.RuneError, nil //nolint:nilerr // lone surrogate: decode keeps it honest
			}
			l.pos = save
		}
		return utf8.RuneError, nil
	}
	return v, nil
}

func hexVal(c byte) int {
	switch {
	case c >= '0' && c <= '9':
		return int(c - '0')
	case c >= 'a' && c <= 'f':
		return int(c-'a') + 10
	case c >= 'A' && c <= 'F':
		return int(c-'A') + 10
	default:
		return -1
	}
}

func (l *lexer) skipSpace() {
	for l.pos < len(l.query) {
		switch l.query[l.pos] {
		case ' ', '\t', '\r', '\n':
			l.pos++
		default:
			return
		}
	}
}

// isIdentStart/isIdentPart are ASCII-only: a byte >= 0x80 never starts or
// continues an identifier (multi-script text rides in quoted strings), and
// the rune cast of such a byte must not be read as a Latin-1 letter.
func isIdentStart(r rune) bool {
	return r < utf8.RuneSelf && (r == '_' || unicode.IsLetter(r))
}

func isIdentPart(r rune) bool {
	return r < utf8.RuneSelf && (r == '_' || unicode.IsLetter(r) || unicode.IsDigit(r))
}

func isDigit(c byte) bool { return c >= '0' && c <= '9' }

// periodUnits maps the relative-date unit spellings to canonical units
// (aql.md §2.4): the official table's plurals and short suffixes, plus the
// singular long forms. The millisecond unit registers the implemented
// spelling "millis" (the official table's "mills" is a documented typo the
// spec registers per the implementation); the decompiled-only "mi" minutes
// short form stays out (V-f posture: undocumented variants are not adopted).
var periodUnits = map[string]TimeUnit{
	"ms": UnitMillis, "millis": UnitMillis,
	"millisecond": UnitMillis, "milliseconds": UnitMillis,
	"s": UnitSeconds, "second": UnitSeconds, "seconds": UnitSeconds,
	"minutes": UnitMinutes, "minute": UnitMinutes,
	"d": UnitDays, "day": UnitDays, "days": UnitDays,
	"w": UnitWeeks, "week": UnitWeeks, "weeks": UnitWeeks,
	"mo": UnitMonths, "month": UnitMonths, "months": UnitMonths,
	"y": UnitYears, "year": UnitYears, "years": UnitYears,
}

// parsePeriod parses a $last/$before operand: "<count> <unit>" with at least
// one space (the shape of every registered example, aql.md §2.4 / v11).
func parsePeriod(s string) (Period, bool) {
	i := 0
	for i < len(s) && isDigit(s[i]) {
		i++
	}
	if i == 0 || i >= len(s) {
		return Period{}, false
	}
	j := i
	for j < len(s) && (s[j] == ' ' || s[j] == '\t') {
		j++
	}
	if j == i || j >= len(s) {
		return Period{}, false
	}
	unit, ok := periodUnits[s[j:]]
	if !ok {
		return Period{}, false
	}
	n, err := strconv.ParseInt(s[:i], 10, 64)
	if err != nil {
		return Period{}, false
	}
	return Period{Count: n, Unit: unit}, true
}

// validPartialDate accepts ISO8601 dates at the documented partial
// precisions (aql.md §2.5: YYYY, YYYY-MM, YYYY-MM-DD, and the full datetime
// with an optional zone). Range checks are calendar-shape level (month/day
// bounds); 0-padded fields only.
func validPartialDate(s string) bool {
	p := 0
	take := func(n int) (string, bool) {
		if p+n > len(s) {
			return "", false
		}
		d := s[p : p+n]
		for i := 0; i < n; i++ {
			if !isDigit(d[i]) {
				return "", false
			}
		}
		p += n
		return d, true
	}
	lit := func(c byte) bool {
		if p < len(s) && s[p] == c {
			p++
			return true
		}
		return false
	}
	inRange := func(v, lo, hi string) bool { return v >= lo && v <= hi }
	if _, ok := take(4); !ok {
		return false
	}
	if lit('-') {
		mm, ok := take(2)
		if !ok || !inRange(mm, "01", "12") {
			return false
		}
		if lit('-') {
			dd, ok := take(2)
			if !ok || !inRange(dd, "01", "31") {
				return false
			}
		}
	}
	if p < len(s) && (s[p] == 'T' || s[p] == 't') {
		p++
		hh, ok := take(2)
		if !ok || !inRange(hh, "00", "23") {
			return false
		}
		if lit(':') {
			mi, ok := take(2)
			if !ok || !inRange(mi, "00", "59") {
				return false
			}
			if lit(':') {
				ss, ok := take(2)
				if !ok || !inRange(ss, "00", "59") {
					return false
				}
				if lit('.') {
					if _, ok := take(1); !ok {
						return false
					}
					for p < len(s) && isDigit(s[p]) {
						p++
					}
				}
			}
		}
	}
	if p < len(s) {
		switch s[p] {
		case 'Z', 'z':
			p++
		case '+', '-':
			p++
			zh, ok := take(2)
			if !ok || !inRange(zh, "00", "23") {
				return false
			}
			if lit(':') {
				zm, ok := take(2)
				if !ok || !inRange(zm, "00", "59") {
					return false
				}
			} else if _, ok := take(2); !ok {
				return false
			}
		}
	}
	return p == len(s)
}
