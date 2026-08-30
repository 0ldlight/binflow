//go:build !race

package deb

// raceEnabled is false on non-race builds (see race_enabled_test.go). Every
// polling window keyed on it keeps its base width on this path.
const raceEnabled = false
