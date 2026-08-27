// Package rpm serves the RPM/YUM repository protocol (M11 T-311, FR-98).
//
// The content plane is a pure storage-path protocol (rpm.md section 1): a
// yum/dnf client GETs repository-relative file paths verbatim — the .rpm
// artifacts, repodata/repomd.xml, the digest-prefixed indexes — with no
// API mount of its own. The management plane is the /binflow/api/yum
// reindex family (internal/httpapi/rpm.go — ADR-0034's dispatchAPI
// posture).
//
// LOCAL repositories only in this release: the remote pull-through and the
// virtual aggregation land with their own M11 tickets.
//
// The two self-built engines:
//
//   - header.go: the RPM binary-format reader (lead + signature header +
//     main header; NEVRA, the six dependency groups, the file list, the
//     changelog) — pure Go, zero dependencies;
//   - repomd.go + reindex.go: the repodata generator (primary/filelists/
//     other trio, checksum-prefixed .xml.gz naming, the _tmp_ staged
//     promote, generation retention, the comps group chain).
//
// Checksums are SHA-256 throughout (TL-5 — the deliberate exception to
// Artifactory's SHA-1 default). calculateYumMetadata defaults FALSE
// (RP-2's final ruling: uploads store, repodata recomputes on demand).
package rpm
