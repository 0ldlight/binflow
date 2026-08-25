package metadata

import (
	"errors"
	"fmt"
	"strings"
)

// Node property shape rules — the closed legality set of the 013
// node_props table's rows (M10 T-286, FR-89 / ADR-0033, architecture
// sections 15.3.1/15.3.2).
//
// One validator serves every writer of node properties: the deploy-time
// matrix parameters (adapter.ParseMatrixProps -> repo.PutOptions.
// Properties), the repo service's options guard and the REST ?properties
// write family (httpapi). It lives HERE — not repo — because the adapter
// root cannot import repo (repo -> remote -> adapter is a cycle) while
// both adapter and repo import this package freely; the rules are the
// table's row-shape law, which is the metadata layer's to own either way.
// Keeping them in one place means no writer can drift on charset, length
// or cardinality — the metadata-bomb guard PRD FR-89.3 pins (key <=64,
// single value <=1KiB, per-node cardinality caps).

// ErrInvalidProperties marks a property set rejected by the closed rules
// (bad key charset, oversize/empty value, cardinality over the caps). The
// HTTP plane maps it to 400 — the matrix arm through
// adapter.ErrBadRequestPath wrapping, the REST arm through this sentinel.
var ErrInvalidProperties = errors.New("invalid node properties")

// Property limits (ADR-0033 / architecture sections 15.3.1/15.3.2).
const (
	// MaxPropKeyLen bounds one property key (PropertyNameValidator shape).
	MaxPropKeyLen = 64
	// MaxPropValueLen bounds one property value in bytes.
	MaxPropValueLen = 1024
	// MaxNodePropKeys bounds the distinct keys one node may carry.
	MaxNodePropKeys = 64
	// MaxPropValues bounds the values one key may carry on one node.
	MaxPropValues = 32
)

// ValidatePropKey checks one property key against the closed charset
// [A-Za-z][A-Za-z0-9_.-]{0,63} (the PropertyNameValidator equivalent,
// architecture section 15.3.1). An empty or malformed key is an error even
// before the length rule — there is no spelling of "no key".
func ValidatePropKey(key string) error {
	if key == "" {
		return fmt.Errorf("%w: property key is empty", ErrInvalidProperties)
	}
	if len(key) > MaxPropKeyLen {
		return fmt.Errorf("%w: property key %q is longer than %d characters", ErrInvalidProperties, key, MaxPropKeyLen)
	}
	for i, r := range key {
		switch {
		case r >= 'a' && r <= 'z', r >= 'A' && r <= 'Z':
			// leading and body: always legal
		case i > 0 && (r >= '0' && r <= '9' || r == '_' || r == '.' || r == '-'):
			// body only: a key must open with a letter
		default:
			return fmt.Errorf("%w: property key %q must match [A-Za-z][A-Za-z0-9_.-]{0,%d}",
				ErrInvalidProperties, key, MaxPropKeyLen-1)
		}
	}
	return nil
}

// ValidatePropValue checks one property value: non-empty, at most
// MaxPropValueLen bytes, no control bytes (a value is rendered back on the
// REST plane and must never smuggle header/JSON-breaking bytes in). key
// names the value's owner for the error wording only.
func ValidatePropValue(key, value string) error {
	if value == "" {
		// The REST plane's recorded refusal wording (rest-api.md section 3,
		// high confidence) is reused verbatim so both write arms answer the
		// same shape. Calibration note: whether the reference matrix arm
		// also refuses empty values is unverified — BinFlow takes the
		// deterministic strict reading (T-286 log, spec 待验证).
		return fmt.Errorf("%w: Properties value cannot be empty. (key %q)", ErrInvalidProperties, key)
	}
	if len(value) > MaxPropValueLen {
		return fmt.Errorf("%w: property value for key %q is longer than %d bytes", ErrInvalidProperties, key, MaxPropValueLen)
	}
	if strings.ContainsFunc(value, isPropControlByte) {
		return fmt.Errorf("%w: property value for key %q contains control characters", ErrInvalidProperties, key)
	}
	return nil
}

// isPropControlByte reports C0 controls and DEL — the same byte set the
// artifact-path validators reject, restated locally so the property rules
// never depend on a foreign package's private helper drifting.
func isPropControlByte(r rune) bool { return r < 0x20 || r == 0x7f }

// ValidatePropSet checks a whole deploy/write set: every key and value
// legal, at most MaxNodePropKeys distinct keys, at most MaxPropValues
// values per key, no duplicate values within a key (the storage model is a
// set — (repo, path, name, value) is the primary key — so a duplicate
// would either fail the insert or silently collapse; refusing early keeps
// the client's picture and the stored set identical).
func ValidatePropSet(props map[string][]string) error {
	if len(props) == 0 {
		return nil
	}
	if len(props) > MaxNodePropKeys {
		return fmt.Errorf("%w: more than %d property keys on one node", ErrInvalidProperties, MaxNodePropKeys)
	}
	for key, values := range props {
		if err := ValidatePropKey(key); err != nil {
			return err
		}
		if len(values) > MaxPropValues {
			return fmt.Errorf("%w: property key %q carries more than %d values", ErrInvalidProperties, key, MaxPropValues)
		}
		seen := make(map[string]struct{}, len(values))
		for _, v := range values {
			if err := ValidatePropValue(key, v); err != nil {
				return err
			}
			if _, dup := seen[v]; dup {
				return fmt.Errorf("%w: property key %q repeats value %q", ErrInvalidProperties, key, v)
			}
			seen[v] = struct{}{}
		}
	}
	return nil
}
