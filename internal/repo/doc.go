// Package repo orchestrates repository use cases: artifact Get/Put/Delete/List,
// repository CRUD, — since M2 (T-35) — the docker registry index use cases
// (manifest/tag/ref publish, resolve, list, delete), and — since M3 (T-64) —
// the three-class repository model (local/remote/virtual configuration,
// FR-15) plus the landed-blob registration (PutLandedBlob, architecture
// section 11.13), delegating to the storage, metadata and auth contracts
// (architecture sections 3.3 and 5.1: adapters never touch the metadata
// layer directly; this Service is the only orchestration point).
package repo
