//go:build !race

package repo_test

// raceEnabled is false on non-race builds (see race_enabled_test.go). Every
// budget arm keyed on it asserts the production number on this path.
const raceEnabled = false
