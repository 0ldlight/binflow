package auth

// ResetRecurrentLedgerForTest clears the service's recurrent-failure
// ledger. The breaker is deliberately process-local with no admin reset
// face; external test packages (auth_test) call this when their scenario's
// own expected failures would otherwise arm it and drown the behavior
// under test (e.g. the hot LDAP directory switch).
func ResetRecurrentLedgerForTest(svc *Service) {
	svc.recurrent.mu.Lock()
	svc.recurrent.entries = map[string]recurrentEntry{}
	svc.recurrent.mu.Unlock()
}
