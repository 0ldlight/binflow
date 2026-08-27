// Package keypair is the instance-level GPG signing keypair domain (M11
// T-319, ADR-0038 / docs/design/gpg-keypair.md): generation, import,
// query, update (rotation) and delete with the in-use guard, the enc:v1
// dual-column sealed storage, and the signing seam the debian (T-321) and
// rpm (T-322) metadata-signing legs consume.
//
// The wire face (paths, field names, KeyPairSummary shape) is the
// Artifactory-compatible /api/security/keypair family anchored to the
// official REST documentation (spec section 1, level L-A); the mechanisms
// (sealing, association, guard, gates) are ADR-0038 (level L-B). The
// private key and the passphrase NEVER leave the store: no read path
// returns them and no export endpoint exists (ADR-0038 decision 3).
package keypair
