//go:build !race

package metadata

// raceDetectorOn is false in ordinary builds (see race_on_test.go).
const raceDetectorOn = false
