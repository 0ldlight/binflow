//go:build race

package deb

// raceEnabled reports whether the race detector instruments this test build
// (the file pair is selected by the race build tag). FR-121 escape #2
// (T-361): the deb full-load family's async-recompute polling windows are
// RECALIBRATED under -race via pollWindow (handler_test.go) — never skipped;
// the assertions behind every window stay live in both postures.
const raceEnabled = true
