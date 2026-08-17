package httpapi

import (
	"log/slog"
	"net/http"
)

// ChainHeadForTest exposes the production middleware head — requestID ->
// accessLog -> recover — wrapped around a caller-supplied terminal
// handler. It exists so external tests can assert the chain's observable
// behavior (late WriteHeader logging, mid-stream panic handling) against
// the exact composition the server serves, instead of rebuilding a
// look-alike that could drift.
//
// The CORS and auth layers are intentionally excluded: they are route
// configuration, not part of what these assertions exercise.
func ChainHeadForTest(logger *slog.Logger, terminal http.Handler) http.Handler {
	if logger == nil {
		logger = slog.Default()
	}
	return chain(
		requestID,
		accessLog(logger),
		recoverPanic(logger),
	)(terminal)
}
