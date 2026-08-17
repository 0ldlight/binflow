package console

import (
	"encoding/json"
	"net/http"
)

// placeholderBody is the M1 response served at "/" and "/binflow/"
// (architecture section 7.1: console is a placeholder JSON in M1, an
// embedded SPA in M4).
type placeholderBody struct {
	Service string `json:"service"`
	Console string `json:"console"`
}

// Handler returns the M1 console placeholder: a JSON notice that the web
// console ships in M4. In M4 this file becomes a go:embed mount of the
// built SPA (web/dist -> internal/console/dist, already excluded from
// lint); the exported signature is the stable seam the httpapi router
// binds, so M4 swaps the implementation without touching routing.
//
// (The embed mention above is deliberately not a compiler directive; M4
// will introduce the real one.)
func Handler() http.Handler {
	body, err := json.Marshal(placeholderBody{Service: "binflow", Console: "M4"})
	if err != nil {
		// A two-field struct with constant strings cannot fail; panic on
		// assembly bug rather than serve a broken handler forever.
		panic("console: marshal placeholder: " + err.Error())
	}
	return http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		w.Header().Set("Content-Type", "application/json")
		w.WriteHeader(http.StatusOK)
		_, _ = w.Write(body)
	})
}
