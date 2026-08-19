// Package remote implements the outbound half of BinFlow's remote
// (pull-through proxy) repositories.
//
// T-65 delivers two sub-domains (the fetcher, cache-state and credentials
// sub-domains land with T-66):
//
//   - ssrfguard.go — the NFR-S13 outbound validation chain: request-time
//     scheme assertion, all-IP screening of the resolved host (loopback,
//     RFC1918, IPv6 ULA, link-local including the cloud metadata address
//     169.254.169.254, unspecified, multicast, broadcast, reserved — with
//     the per-repository allowPrivateUpstream exemption), and DNS-rebinding
//     defense (dial the validated IP instead of the hostname; the dialer's
//     Control callback re-screens the address actually being connected).
//   - client.go — the stdlib-only transport (ADR-0005 baseline, ADR-0012
//     decision 5): manual redirect following with every hop re-checked
//     (five hops max, T-79 errata), per-repository socket timeouts
//     (socketTimeoutSecs, 15s default), a 64MB cap on buffered
//     metadata-class responses, idempotent GET/HEAD retry with exponential
//     backoff (twice), byte-counting streaming bodies, and Basic-credential
//     pass-through (decryption from at-rest storage stays with the
//     fetcher).
//
// Every chain rejection is a *RejectionError plus exactly one structured
// WARN log line (repo key, refused target, reason category, chain phase)
// with no stack trace — the M42 forensic surface. The fetcher (T-66) maps
// rejections to 400 E-01 responses (PRD FR-20-AC3) and oversized buffered
// bodies to 502 (NFR-S13 point 5).
package remote
