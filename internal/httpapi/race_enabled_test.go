//go:build race

package httpapi_test

// raceEnabled reports whether the race detector instruments this test build
// (the file pair is selected by the race build tag). The NFR-P17 spot check
// skips under -race: the detector multiplies the measured path's cost by an
// order of magnitude, so the 1s production budget would be asserted against
// an instrumented strawman (observed p95 1.98s under -race vs 0.12s without,
// same binary shape). The formal gate belongs to T-105's no-race run.
const raceEnabled = true
