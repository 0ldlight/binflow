// Package license implements the BinFlow entitlement core (M10 T-279,
// ADR-0032 / architecture section 15.1): the self-owned ed25519-signed
// license document format, the offline verification chain, the closed tier
// set and the Manager that owns the installed-license state every gate
// consults.
//
// Design anchors (ADR-0032, Accepted):
//
//   - Document format v1 is a JWS-compact-minimal two-segment text,
//     <base64url(payloadJSON)>.<base64url(ed25519Sig)>, with fully
//     self-owned payload fields (clean-room: no JFrog format, algorithm or
//     key material is read, reproduced or recognized).
//   - The verification chain is ONE function (VerifyDocument) shared by the
//     install, startup and daily-ticker paths: parse -> typ/alg/kid static
//     check -> ed25519.Verify over the payload bytes -> time window with a
//     1h leeway. Everything is offline; there is no callhome.
//   - The tier closed set is community < pro < enterprise (a code
//     constant, not data). The five core package types sit on the community
//     floor and stay unlocked with no license at all, so an unlicensed
//     instance behaves exactly like m9-done.
//   - State is an atomic snapshot: Install/Uninstall swap it atomically and
//     readers never lock. The expiry crossing (D6, no grace period) is
//     re-evaluated by the daily ticker; State additionally refuses to honor
//     a snapshot whose window the local clock has already passed, so an
//     expired tier is never observed as valid between ticks (NFR-S53: an
//     unverifiable license is never kept at a high tier).
//
// Dependency direction: license imports metadata (the persistence layer,
// same posture as repo/audit) and audit (the event recorder). It must NEVER
// import internal/addons — gate evaluation takes primitive parameters
// (AddonEnabled(ctx, id, minTier)) precisely so the addon registry can sit
// on top without a cycle (ADR-0033 decision 2).
package license
