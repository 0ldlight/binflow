// Parse-error anchor parity (L027-1 c27): Artifactory's AQL parser streams
// its tokens, so a grammar failure at an earlier position preempts a lexer
// failure further right in the text. The E1 residual segment must anchor at
// the grammar failure, not at the byte the lexer chokes on.
package search

import (
	"errors"
	"testing"
)

func TestParseErrorAnchorPreemptsLexerError(t *testing.T) {
	cases := []struct {
		name    string
		query   string
		segment string // the E1 residual sub-query the reference echoes
	}{
		{
			// The live-probe pair: A anchors at `bogus` (the parser wanted
			// `{` there); the lexer alone would anchor at the `-` it cannot
			// start a number from (L027-1 wire c27, l026a p33 family).
			name:    "grammar failure before lexer failure",
			query:   "items.find(bogus-syntax(",
			segment: "bogus-syntax(",
		},
		{
			// The prefix parses completely: the streaming parser asks for
			// the next token exactly where the lexer dies, so the lexer's
			// anchor IS the report.
			name:    "lexer failure after a complete prefix",
			query:   `items.find({"repo":{"$eq":"a"}}) ~`,
			segment: "~",
		},
		{
			// An unclosed construct over a dead tail: the parser waits for
			// the closing brace, asks the lexer, and dies at the bad byte.
			name:    "unclosed construct meets bad tail",
			query:   `items.find({"repo":{"$eq":"a"} ~`,
			segment: "~",
		},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			_, err := Parse(tc.query)
			if err == nil {
				t.Fatalf("Parse(%q) succeeded, want the E1 syntax error", tc.query)
			}
			var qe *QueryError
			if !errors.As(err, &qe) || qe.Kind != ErrSyntax {
				t.Fatalf("Parse(%q) error = %v, want *QueryError{Kind:ErrSyntax}", tc.query, err)
			}
			if qe.Segment != tc.segment {
				t.Errorf("E1 segment = %q, want %q", qe.Segment, tc.segment)
			}
		})
	}
}
