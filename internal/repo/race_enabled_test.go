//go:build race

package repo_test

// raceEnabled reports whether the race detector instruments this test build
// (the file pair is selected by the race build tag). FR-121 escape #1 (T-361):
// the TestBigTreeCopyNo5xx dry-run budget is RECALIBRATED under -race, never
// skipped — see dryRunBudget in operations_test.go for the two-tier ceiling
// and the calibration evidence behind it.
const raceEnabled = true
