package pypi

import (
	"fmt"
	"regexp"
	"strings"
)

// nameSeparators matches the PEP 503 normalization class: runs of "-", "_"
// and "." collapse to a single "-" (the whole regex is one character class
// with a + quantifier, so "demo__pkg..1" and "demo-pkg-1" normalize alike).
var nameSeparators = regexp.MustCompile(`[-_.]+`)

// normalizePackageName applies PEP 503 name normalization: lowercase, then
// every run of "-", "_" or "." becomes a single "-". It is the index lookup
// key ONLY — the storage path keeps the metadata's original spelling
// (maven-npm-pypi.md section 3.5: "{name}/{version}/{filename}" with the
// ORIGINAL name; normalization never rewrites what is stored).
func normalizePackageName(name string) string {
	return nameSeparators.ReplaceAllString(strings.ToLower(name), "-")
}

// errUnsafeSegment marks an upload field (name, version or filename) that
// cannot become one storage path segment: the upload path is raw client
// input becoming a stored path, so the same defense the layout applies to
// URLs applies here to form fields (NFR-S18).
var errUnsafeSegment = fmt.Errorf("unsafe upload path segment")

// validateUploadSegment checks one future path segment from the multipart
// form: non-empty, no separators, no dot segments, no control bytes, no
// leading-tilde home escape, bounded length (a full path is bounded again
// by the service's node-path validator — this is the front-door check).
func validateUploadSegment(field, value string) error {
	if value == "" {
		return fmt.Errorf("%w: the '%s' field is empty", errUnsafeSegment, field)
	}
	if len(value) > 128 {
		return fmt.Errorf("%w: the '%s' field is longer than 128 characters", errUnsafeSegment, field)
	}
	if strings.ContainsAny(value, "/\\") {
		return fmt.Errorf("%w: the '%s' field must not contain a path separator", errUnsafeSegment, field)
	}
	if value == "." || value == ".." || strings.HasPrefix(value, "../") || strings.HasSuffix(value, "/..") {
		return fmt.Errorf("%w: the '%s' field must not be a dot segment", errUnsafeSegment, field)
	}
	if strings.ContainsFunc(value, isControlByte) {
		return fmt.Errorf("%w: the '%s' field must not contain control characters", errUnsafeSegment, field)
	}
	return nil
}
