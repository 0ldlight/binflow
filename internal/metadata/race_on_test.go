//go:build race

package metadata

// raceDetectorOn reports that the race detector instrumentation is active
// (its bookkeeping skews wall-clock measurements, so the T-411 perf legs
// skip under -race: they assert production latency budgets, not detector
// overhead).
const raceDetectorOn = true
