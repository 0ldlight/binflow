package keypair

import (
	"encoding/json"
	"fmt"
	"strings"
)

// RepoConfigReference extracts the keypair association from a repository
// config blob (the single home of the field spelling, RepoConfigField —
// spec section 3.3):
//
//	name, present, err := RepoConfigReference(config)
//
// present reports whether the field appears at all (an explicit "" clears
// the association — the v2 DELETE face writes exactly that); name is ""
// when unset or cleared. A present non-string value refuses (the caller
// maps it to its config refusal). The blob's other fields are ignored:
// this is a single-field projection, not the config's validator.
func RepoConfigReference(config string) (name string, present bool, err error) {
	if strings.TrimSpace(config) == "" {
		return "", false, nil
	}
	var probe map[string]json.RawMessage
	if err := json.Unmarshal([]byte(config), &probe); err != nil {
		return "", false, fmt.Errorf("keypair reference: config is not valid JSON: %w", err)
	}
	raw, ok := probe[RepoConfigField]
	if !ok {
		return "", false, nil
	}
	if err := json.Unmarshal(raw, &name); err != nil {
		return "", true, fmt.Errorf("keypair reference: %s must be a string", RepoConfigField)
	}
	return name, true, nil
}
