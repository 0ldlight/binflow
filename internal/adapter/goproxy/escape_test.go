package goproxy

import (
	"errors"
	"testing"

	"github.com/lzwzzy/binflow/internal/adapter"
)

// TestUnescapeElement: the wire->storage leg of the !lower rule — '!x'
// becomes Upper(x) for any payload rune; '!!' and a trailing '!' are the
// 400 family (goproxy.md section 3.1 / S12).
func TestUnescapeElement(t *testing.T) {
	cases := []struct {
		wire, want string
		wantErr    bool
	}{
		{wire: "example.com", want: "example.com"},
		{wire: "example.com/!my!mod", want: "example.com/MyMod"},
		{wire: "example.com/!mymod", want: "example.com/Mymod"},
		{wire: "!upper", want: "Upper"},
		{wire: "v1.0.0-!b!e!t!a", want: "v1.0.0-BETA"},
		{wire: "MiXeD", want: "MiXeD"},
		{wire: "mo!d", want: "moD"}, // a mid-element escape is legal
		{wire: "example.com/!!mod", wantErr: true},
		{wire: "bang!", wantErr: true},
		{wire: "a!!b", wantErr: true},
		{wire: "plain!1", want: "plain1"}, // Upper of a non-letter is the identity
	}
	for _, c := range cases {
		got, err := unescapeElement(c.wire)
		if c.wantErr {
			if err == nil {
				t.Errorf("unescapeElement(%q) = %q, want error", c.wire, got)
			} else if !errors.Is(err, adapter.ErrBadRequestPath) {
				t.Errorf("unescapeElement(%q) err = %v, want ErrBadRequestPath", c.wire, err)
			}
			continue
		}
		if err != nil {
			t.Errorf("unescapeElement(%q): %v", c.wire, err)
			continue
		}
		if got != c.want {
			t.Errorf("unescapeElement(%q) = %q, want %q", c.wire, got, c.want)
		}
	}
}

// TestEscapeElement: the storage->wire leg — uppercase runes become '!'+
// lowercase; everything else passes through.
func TestEscapeElement(t *testing.T) {
	cases := []struct {
		decoded, want string
	}{
		{decoded: "example.com/MyMod", want: "example.com/!my!mod"},
		{decoded: "Upper", want: "!upper"},
		{decoded: "lower", want: "lower"},
		{decoded: "v1.0.0-BETA", want: "v1.0.0-!b!e!t!a"},
	}
	for _, c := range cases {
		if got := escapeElement(c.decoded); got != c.want {
			t.Errorf("escapeElement(%q) = %q, want %q", c.decoded, got, c.want)
		}
	}
}

// TestEscapeRoundTrip: wire -> storage -> wire is the identity on every
// path the protocol can produce (the three-state rule's coherence check).
func TestEscapeRoundTrip(t *testing.T) {
	for _, wire := range []string{
		"example.com/mymod",
		"example.com/!my!mod",
		"example.com/!my!mod/sub!pkg",
		"gopkg.in/!yaml.!v2",
	} {
		storage, err := unescapePath(wire)
		if err != nil {
			t.Fatalf("unescapePath(%q): %v", wire, err)
		}
		if back := escapePath(storage); back != wire {
			t.Errorf("round trip of %q: storage %q re-escapes to %q", wire, storage, back)
		}
	}
}
