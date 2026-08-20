//go:build !race

package httpapi_test

// raceEnabled is false on non-race builds (see race_enabled_test.go).
const raceEnabled = false
