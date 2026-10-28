package auth

// Test-only seams the package exports for EXTERNAL test packages (the
// httpapi harness family): the breaker ledger is deliberately
// process-local with no admin reset face (T-632), so a scenario whose
// own expected failures would arm it needs this one off-switch to keep
// the behavior under test from drowning under leftover state. Never part
// of any request path.

// ResetRecurrentLedgerForTest clears the service's recurrent-failure
// ledger (every username's failure count and armed block). The breaker
// is deliberately process-local with no admin reset face; external test
// packages call this when their scenario's own expected failures would
// otherwise arm it and drown the behavior under test (e.g. the httpapi
// session-login choreography, T-634).
func ResetRecurrentLedgerForTest(svc *Service) {
	svc.recurrent.mu.Lock()
	svc.recurrent.entries = map[string]recurrentEntry{}
	svc.recurrent.mu.Unlock()
}
