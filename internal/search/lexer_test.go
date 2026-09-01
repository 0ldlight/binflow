package search

import "testing"

// Lexer unit tests: token shapes with positions, number and string literal
// decoding, and the literal validators (relative periods, partial dates).

func TestLexTokenStream(t *testing.T) {
	toks, qe := lexAll(`items.find({"repo":"a"}).limit(3)`)
	if qe != nil {
		t.Fatalf("lexAll error: %v", qe)
	}
	type wantTok struct {
		kind tokenKind
		text string
		pos  int
	}
	want := []wantTok{
		{tkIdent, "items", 0},
		{tkPunct, ".", 5},
		{tkIdent, "find", 6},
		{tkPunct, "(", 10},
		{tkPunct, "{", 11},
		{tkString, "repo", 12}, // keys are quoted strings on the wire
		{tkPunct, ":", 18},
		{tkString, "a", 19},
		{tkPunct, "}", 22},
		{tkPunct, ")", 23},
		{tkPunct, ".", 24},
		{tkIdent, "limit", 25},
		{tkPunct, "(", 30},
		{tkNumber, "3", 31},
		{tkPunct, ")", 32},
		{tkEOF, "", 33},
	}
	if len(toks) != len(want) {
		t.Fatalf("token count = %d, want %d (%+v)", len(toks), len(want), toks)
	}
	for i, w := range want {
		got := toks[i]
		if got.kind != w.kind || got.text != w.text || got.pos != w.pos {
			t.Errorf("token[%d] = {%d %q pos:%d}, want {%d %q pos:%d}", i, got.kind, got.text, got.pos, w.kind, w.text, w.pos)
		}
	}
}

func TestLexDollarWords(t *testing.T) {
	toks, qe := lexAll(`{"$and":[{}]}`)
	if qe != nil {
		t.Fatalf("lexAll error: %v", qe)
	}
	if !toks[1].isOpWord("$and") {
		t.Errorf("token[1] = %+v, want quoted $and operator word", toks[1])
	}
	// The bare $word spelling lexes as a dollar token and matches too.
	toks, qe = lexAll(`{$and:1}`)
	if qe != nil {
		t.Fatalf("lexAll error: %v", qe)
	}
	if !toks[1].isOpWord("$and") {
		t.Errorf("token[1] = %+v, want bare $and dollar word", toks[1])
	}
	// A lone '$' is a syntax error anchored at itself.
	if _, qe := lexAll(`$`); qe == nil || qe.Pos != 0 {
		t.Errorf("lone $: err = %+v, want ErrSyntax at 0", qe)
	}
}

func TestLexNumbers(t *testing.T) {
	tests := []struct {
		src   string
		int   int64
		frac  bool
		fails bool
	}{
		{"3", 3, false, false},
		{"007", 7, false, false},
		{"-2", -2, false, false},
		{"1.5", 0, true, false},
		{"1e3", 0, true, false},
		{"1E-2", 0, true, false},
		{"-", 0, false, true},   // bare minus
		{"1.", 1, false, false}, // dot belongs to the next token, not a fraction
	}
	for _, tt := range tests {
		toks, qe := lexAll(tt.src)
		if tt.fails {
			if qe == nil {
				t.Errorf("%q: expected error, got %+v", tt.src, toks)
			}
			continue
		}
		if qe != nil {
			t.Errorf("%q: unexpected error %v", tt.src, qe)
			continue
		}
		if toks[0].kind != tkNumber {
			t.Errorf("%q: kind = %d, want tkNumber", tt.src, toks[0].kind)
			continue
		}
		if toks[0].intVal != tt.int || toks[0].frac != tt.frac {
			t.Errorf("%q: int=%d frac=%v, want int=%d frac=%v", tt.src, toks[0].intVal, toks[0].frac, tt.int, tt.frac)
		}
	}
}

func TestLexStringDecoding(t *testing.T) {
	tests := []struct {
		src   string
		want  string
		fails bool
	}{
		{`"plain"`, "plain", false},
		{`"a\"b"`, `a"b`, false},
		{`"a\\b"`, `a\b`, false},
		{`"A"`, "A", false},
		{`"日本語"`, "日本語", false},
		{`"unterminated`, "", true},
		{`"bad\x"`, "", true},
		{`"raw`, "", true}, // EOF mid-string
	}
	for _, tt := range tests {
		toks, qe := lexAll(tt.src)
		if tt.fails {
			if qe == nil {
				t.Errorf("%q: expected error, got %q", tt.src, toks[0].text)
			}
			continue
		}
		if qe != nil {
			t.Errorf("%q: unexpected error %v", tt.src, qe)
			continue
		}
		if toks[0].kind != tkString || toks[0].text != tt.want {
			t.Errorf("%q: text = %q, want %q", tt.src, toks[0].text, tt.want)
		}
	}
}

func TestLexUnicodeSurrogatePair(t *testing.T) {
	toks, qe := lexAll(`"😀"`)
	if qe != nil {
		t.Fatalf("unexpected error: %v", qe)
	}
	if toks[0].text != "\U0001F600" {
		t.Errorf("text = %q, want U+1F600", toks[0].text)
	}
}

func TestLexIllegalCharacter(t *testing.T) {
	// ';' outside a string is illegal, anchored at its byte.
	_, qe := lexAll(`items.find({});`)
	if qe == nil || qe.Pos != 14 {
		t.Errorf("err = %+v, want ErrSyntax at 14", qe)
	}
	// Non-ASCII outside strings is equally illegal.
	_, qe = lexAll(`items.検索({})`)
	if qe == nil || qe.Pos != 6 {
		t.Errorf("non-ascii err = %+v, want ErrSyntax at 6", qe)
	}
}

func TestParsePeriodUnits(t *testing.T) {
	ok := []struct {
		raw   string
		count int64
		unit  TimeUnit
	}{
		{"3 days", 3, UnitDays},
		{"1 day", 1, UnitDays},
		{"2 d", 2, UnitDays},
		{"2 weeks", 2, UnitWeeks},
		{"2 w", 2, UnitWeeks},
		{"10 minutes", 10, UnitMinutes},
		{"45 seconds", 45, UnitSeconds},
		{"45 s", 45, UnitSeconds},
		{"6 months", 6, UnitMonths},
		{"6 mo", 6, UnitMonths},
		{"1 year", 1, UnitYears},
		{"1 y", 1, UnitYears},
		{"250 ms", 250, UnitMillis},
		{"250 millis", 250, UnitMillis},
	}
	for _, tt := range ok {
		per, valid := parsePeriod(tt.raw)
		if !valid || per.Count != tt.count || per.Unit != tt.unit {
			t.Errorf("parsePeriod(%q) = {%d %s} valid=%v, want {%d %s}", tt.raw, per.Count, per.Unit, valid, tt.count, tt.unit)
		}
	}
	bad := []string{
		"", "3", "days", "3days", "three days", "3 fortnights",
		"3 mi", "3 mills", "-1 days", "3 days ",
	}
	for _, raw := range bad {
		if per, valid := parsePeriod(raw); valid {
			t.Errorf("parsePeriod(%q) = {%d %s}, want invalid", raw, per.Count, per.Unit)
		}
	}
}

func TestValidPartialDate(t *testing.T) {
	ok := []string{
		"2026", "2026-08", "2026-08-23",
		"2026-08-23T08:19:04.618Z",
		"2026-08-23T08:19:04Z",
		"2026-08-23T08:19Z",
		"2026-08-23T08Z",
		"2026-08-23T08:19:04+02:00",
		"2026-08-23T08:19:04-0200",
	}
	for _, s := range ok {
		if !validPartialDate(s) {
			t.Errorf("validPartialDate(%q) = false, want true", s)
		}
	}
	bad := []string{
		"", "202", "20260", "2026-13", "2026-00", "2026-08-32", "2026-08-00",
		"2026-8-1", "2026-08-23T25:00Z", "2026-08-23T08:60Z", "2026-08-23T",
		"2026-08-23 ", "2026-08-23+99:00", "x2026",
	}
	for _, s := range bad {
		if validPartialDate(s) {
			t.Errorf("validPartialDate(%q) = true, want false", s)
		}
	}
}
