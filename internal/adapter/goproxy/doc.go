// Package goproxy is the GOPROXY protocol adapter (M10 T-285, FR-87):
// the Go modules registry plane behind /binflow/<repoKey>. The package name
// avoids the Go keyword "go"; the package TYPE this handler serves — the
// repositories.package_type value httpapi dispatches on — is "go"
// (architecture section 15.2.4).
//
// Behavior basis: docs/reverse/goproxy.md (T-278), which anchors every
// wire-level decision on the official go.dev/ref/mod "GOPROXY protocol"
// chapter and marks the reverse-engineered supplements. The load-bearing
// protocol facts implemented here:
//
//   - wire paths are $module/@v/$version.{info,mod,zip} plus @v/list and
//     @latest; $module and $version elements carry the !lower case encoding
//     (every uppercase letter becomes '!' plus the lowercase letter) while
//     STORAGE paths keep the decoded (case-restored) form — three states:
//     wire, storage, upstream (re-escaped when proxying);
//   - error responses are text/plain (the go command only continues to the
//     next GOPROXY source on 404/410; any other status is terminal), so this
//     adapter deliberately does NOT use the errors[] JSON envelope;
//   - uploads are the vendor extension PUT trio with a validation chain
//     (version grammar, .info JSON, .mod module path, client checksums);
//   - remote repositories pull through an upstream GOPROXY with the
//     internal/remote engine (escaping applied at the upstream hop) and
//     virtual repositories aggregate by member order (first-found downloads,
//     union lists, global-best latest).
package goproxy
