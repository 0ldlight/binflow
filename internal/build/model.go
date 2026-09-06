package build

import (
	"errors"
	"fmt"
	"strings"
	"unicode/utf8"

	"github.com/lzwzzy/binflow/internal/metadata"
)

// Coordinate identifies one build run: the uniqueness four-tuple
// (build_name, build_number, started, build_repo) of ADR-0045 Errata ④㋑
// — same name and number with a different started is a DIFFERENT run, and
// started is the disambiguation key. An empty Started addresses "the
// latest run of (Name, Number, Repo)" on reads (the single-build GET
// face's default); an empty Repo normalizes to
// metadata.DefaultBuildRepo everywhere.
type Coordinate struct {
	Name    string
	Number  string
	Started string
	Repo    string
}

// Resolve returns the coordinate with Repo normalized to the default
// logical key when empty (custom build repos pass through untouched).
func (c Coordinate) Resolve() Coordinate {
	if c.Repo == "" {
		c.Repo = metadata.DefaultBuildRepo
	}
	return c
}

// Coordinate limits: the name doubles as an authorization PATH element
// (the allow() path plane matches patterns against it) and as the one
// segment of the reference layout, so it may not contain '/'; numbers are
// deliberately permissive — the reference API supports build numbers with
// special characters, which is why its delete face exists as a POST with a
// body. Control characters are refused everywhere (wire hygiene).
const (
	maxBuildNameLength   = 256
	maxBuildNumberLength = 128
)

// ErrInvalidCoordinate marks a malformed coordinate: the 400 family's
// service face (the httpapi layer maps it; a denied but well-formed
// coordinate is ErrForbidden, never this).
var ErrInvalidCoordinate = errors.New("build: invalid build coordinate")

// Validate enforces the hard input floor: non-empty name and number, no
// '/', no control characters, sane lengths. Started may be empty (the
// latest-run address); when present it must be control-free — full
// yyyy-MM-dd'T'HH:mm:ss.SSSZ format parsing is the T-508 wire gate's job,
// the store never interprets the literal.
func (c Coordinate) Validate() error {
	if err := ValidateBuildName(c.Name); err != nil {
		return err
	}
	if c.Number == "" {
		return fmt.Errorf("build number is empty: %w", ErrInvalidCoordinate)
	}
	if len(c.Number) > maxBuildNumberLength {
		return fmt.Errorf("build number exceeds %d bytes: %w", maxBuildNumberLength, ErrInvalidCoordinate)
	}
	if hasControl(c.Number) {
		return fmt.Errorf("build number %q contains control characters: %w", c.Number, ErrInvalidCoordinate)
	}
	if hasControl(c.Started) {
		return fmt.Errorf("build started %q contains control characters: %w", c.Started, ErrInvalidCoordinate)
	}
	return nil
}

// ValidateBuildName is the name floor alone — the list faces take a bare
// name without a number, and the upload face (T-508) reuses the same law.
func ValidateBuildName(name string) error {
	if name == "" {
		return fmt.Errorf("build name is empty: %w", ErrInvalidCoordinate)
	}
	if len(name) > maxBuildNameLength {
		return fmt.Errorf("build name exceeds %d bytes: %w", maxBuildNameLength, ErrInvalidCoordinate)
	}
	if strings.ContainsRune(name, '/') {
		// The name is the ACL path element and the layout segment; a '/'
		// would blur both (pattern hygiene).
		return fmt.Errorf("build name %q contains '/': %w", name, ErrInvalidCoordinate)
	}
	if hasControl(name) {
		return fmt.Errorf("build name %q contains control characters: %w", name, ErrInvalidCoordinate)
	}
	return nil
}

// hasControl reports whether s contains any Unicode control character
// (C0 including NUL/newline/carriage return, and DEL). Invalid UTF-8
// counts as control too: a malformed byte never passes the wire floor.
func hasControl(s string) bool {
	if !utf8.ValidString(s) {
		return true
	}
	for _, r := range s {
		if r < 0x20 || r == 0x7f {
			return true
		}
	}
	return false
}
