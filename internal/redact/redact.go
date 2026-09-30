// Package redact holds render-side credential scrubbers shared across
// domains. It must stay a leaf package (stdlib-only imports): consumers
// include internal/remote, internal/httpapi and internal/auth, so it may
// not grow a dependency on any of them (ADR: no import cycles — auth in
// particular must never transit through remote/adapter).
package redact

import "regexp"

var reUserInfoInURL = regexp.MustCompile(`//[^/@?#\s]*@`)

// Userinfo strips embedded credentials before upstream references are
// rendered into anonymous-readable faces (R13 dual-review B1) or server log
// streams (T-617): a remote repo URL may legally carry userinfo (config
// validation accepts it), and the Go client's error text quotes it back —
// password masked as *** but the username intact. Works on bare URLs and
// free text alike; text without the userinfo form passes through unchanged.
func Userinfo(s string) string {
	return reUserInfoInURL.ReplaceAllString(s, "//")
}
