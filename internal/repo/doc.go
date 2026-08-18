// Package repo orchestrates repository use cases: artifact Get/Put/Delete/List,
// repository CRUD and — since M2 (T-35) — the docker registry index use cases
// (manifest/tag/ref publish, resolve, list, delete), delegating to the
// storage, metadata and auth contracts (architecture sections 3.3 and 5.1:
// adapters never touch the metadata layer directly; this Service is the only
// orchestration point).
package repo
