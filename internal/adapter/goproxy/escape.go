package goproxy

import (
	"fmt"
	"strings"
	"unicode"
	"unicode/utf8"

	"github.com/lzwzzy/binflow/internal/adapter"
)

// The !lower case encoding (go.dev/ref/mod "GOPROXY protocol", encoded
// paths; goproxy.md section 3.1): to keep case-insensitive filesystems
// unambiguous, every UPPERCASE letter of a module or version element is
// transmitted as '!' plus the lowercase letter — example.com/MyMod travels
// as example.com/!my!mod. The decoded (case-restored) form is what storage
// uses; the wire form is what clients send and what an upstream GOPROXY
// expects. Escape and unescape are inverse on every path this package can
// produce (storage paths contain no '!').

// errMalformedEscape marks every unescape rejection: a doubled '!!' (the
// reverse-engineered IllegalArgumentException shape, goproxy.md S12) or a
// trailing '!' with no payload rune. Callers map it to 400 via
// adapter.ErrBadRequestPath.
var errMalformedEscape = fmt.Errorf("%w: malformed !-escape in module path", adapter.ErrBadRequestPath)

// unescapeElement maps one wire element to its decoded form: '!x' becomes
// unicode.ToUpper(x) for ANY payload rune (the official rule is stated for
// letters; Upper of a non-letter is the identity, so the mapping is total).
// '!!' and a trailing '!' are rejected — they cannot be produced by a legal
// encoder and cannot be decoded unambiguously.
func unescapeElement(s string) (string, error) {
	if !strings.ContainsRune(s, '!') {
		return s, nil
	}
	var b strings.Builder
	b.Grow(len(s))
	for i := 0; i < len(s); {
		c := s[i]
		if c < utf8.RuneSelf {
			if c != '!' {
				b.WriteByte(c)
				i++
				continue
			}
			r, size := utf8.DecodeRuneInString(s[i+1:])
			if size == 0 || r == '!' {
				return "", fmt.Errorf("%w: %q contains the illegal escape %q",
					errMalformedEscape, s, s[i:min(i+2, len(s))])
			}
			b.WriteRune(unicode.ToUpper(r))
			i += 1 + size
			continue
		}
		r, size := utf8.DecodeRuneInString(s[i:])
		b.WriteRune(r)
		i += size
	}
	return b.String(), nil
}

// escapeElement maps one decoded element to its wire form: every uppercase
// rune becomes '!' plus its lowercase (ASCII letters are the realistic
// domain; the rune-wise rule keeps the mapping total and round-trip exact).
// A decoded element never contains '!' (unescape cannot produce one and
// Layout rejects the wire spellings that would), so no '!' escaping exists.
func escapeElement(s string) string {
	needs := false
	for _, r := range s {
		if unicode.IsUpper(r) {
			needs = true
			break
		}
	}
	if !needs {
		return s
	}
	var b strings.Builder
	b.Grow(len(s) + 4)
	for _, r := range s {
		if unicode.IsUpper(r) {
			b.WriteByte('!')
			b.WriteRune(unicode.ToLower(r))
			continue
		}
		b.WriteRune(r)
	}
	return b.String()
}

// unescapePath decodes a whole '/'-joined path element by element.
func unescapePath(path string) (string, error) {
	if !strings.ContainsRune(path, '!') {
		return path, nil
	}
	segs := strings.Split(path, "/")
	for i, seg := range segs {
		dec, err := unescapeElement(seg)
		if err != nil {
			return "", err
		}
		segs[i] = dec
	}
	return strings.Join(segs, "/"), nil
}

// escapePath encodes a whole '/'-joined path element by element. The literal
// protocol marker segments ("@v", "list", "@latest" never reaches here as an
// element that matters) contain no uppercase, so applying the rule
// unconditionally is safe.
func escapePath(path string) string {
	segs := strings.Split(path, "/")
	for i, seg := range segs {
		segs[i] = escapeElement(seg)
	}
	return strings.Join(segs, "/")
}
