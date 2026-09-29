//go:build !dev

package license

import "log/slog"

// resolveDevTier is the production arm of the T-573 (BIN-55) seam: a
// constant no-op. The dev-build scratch bypass does not exist in a default
// build — BINFLOW_DEV_TIER is not read, not parsed and not logged here, so
// default-build entitlement behavior is byte-identical whether or not the
// variable is set (pinned by devtier_prod_test.go's //go:build !dev arm).
func resolveDevTier(_ *slog.Logger) devTierOverride {
	return devTierOverride{}
}
