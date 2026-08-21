package httpapi

import (
	"encoding/json"
	"fmt"
	"net/http"
	"os"
	"path/filepath"

	"github.com/lzwzzy/binflow/internal/metadata"
)

// ProductName is the product identifier reported by /api/system/version
// (PRD E-03/Q4: honest BinFlow identity, never an emulated Artifactory
// version).
const ProductName = "BinFlow"

// DefaultVersion is the fallback version string when the caller did not
// inject one (T-16 stamps the real build version; the scaffold binary
// reports "dev" — same convention as cmd's own --version).
const DefaultVersion = "dev"

// healthResponse is the /binflow/api/v1/health body (E-22/FR-6-AC2):
// a top-level status plus one nested object per subsystem. Every
// subsystem reports {status, detail} so a degraded component is
// self-describing. The registry field is the M2 addition and version the
// M5 one (PRD section 6.3): add-only, never a rename — older dashboards
// keep parsing.
type healthResponse struct {
	Status   string          `json:"status"`
	Version  string          `json:"version"`
	Storage  subsystemStatus `json:"storage"`
	Metadata subsystemStatus `json:"metadata"`
	Registry subsystemStatus `json:"registry"`
}

// subsystemStatus is one subsystem's health verdict: "ok" or "error",
// with a human-readable detail. degradation ("warn") is deliberately not
// in the M1 vocabulary — every check is binary (writable or not, pings or
// not).
type subsystemStatus struct {
	Status string `json:"status"`
	Detail string `json:"detail,omitempty"`
}

// handlePing answers 200 text/plain "OK" (rest-api.md section 5, E-02).
// No auth: the whole point is the cheapest possible liveness probe.
func handlePing(w http.ResponseWriter, _ *http.Request) {
	w.Header().Set("Content-Type", "text/plain")
	w.WriteHeader(http.StatusOK)
	_, _ = w.Write([]byte("OK"))
}

// versionResponse is the /binflow/api/system/version body (E-03):
// version + revision + product:"BinFlow". Honest values only (Q4).
type versionResponse struct {
	Version  string `json:"version"`
	Revision string `json:"revision"`
	Product  string `json:"product"`
}

// handleVersion answers the honest version block.
func (s *Server) handleVersion(w http.ResponseWriter, _ *http.Request) {
	version, revision := s.deps.Version, s.deps.Revision
	if version == "" {
		version = DefaultVersion
	}
	body, err := json.MarshalIndent(versionResponse{Version: version, Revision: revision, Product: ProductName}, "", "  ")
	if err != nil {
		writeError(w, http.StatusInternalServerError, "render version response: "+err.Error())
		return
	}
	w.Header().Set("Content-Type", "application/json")
	w.WriteHeader(http.StatusOK)
	_, _ = w.Write(body)
}

// handleV1Health answers /binflow/api/v1/health (E-22): storage writable
// probe + metadata ping. status is "ok" only when every subsystem is ok;
// the response stays 200 even when degraded — readiness signaling is
// /readyz's job, this endpoint is the operator's dashboard.
func (s *Server) handleV1Health(w http.ResponseWriter, r *http.Request) {
	// The top-level version echoes the injected build identity (PB-02,
	// FR-34-AC2): same value --version prints, via the same Deps seam.
	resp := healthResponse{
		Status:  "ok",
		Version: s.versionOrDefault(),
		Storage: s.probeStorage(),
	}
	if err := s.deps.Metadata.Ping(r.Context()); err != nil {
		resp.Status = "error"
		resp.Metadata = subsystemStatus{Status: "error", Detail: fmt.Sprintf("metadata ping: %v", err)}
	} else {
		resp.Metadata = subsystemStatus{Status: "ok"}
	}
	resp.Registry = s.probeRegistry()
	if resp.Registry.Status != "ok" {
		resp.Status = "error"
	}
	if resp.Storage.Status != "ok" {
		resp.Status = "error"
	}
	body, err := json.MarshalIndent(resp, "", "  ")
	if err != nil {
		writeError(w, http.StatusInternalServerError, "render health response: "+err.Error())
		return
	}
	w.Header().Set("Content-Type", "application/json")
	w.WriteHeader(http.StatusOK)
	_, _ = w.Write(body)
}

// probeStorage verifies the data directory accepts writes: create +
// remove a probe file next to the blob store (E-22: "storage 可写探测").
func (s *Server) probeStorage() subsystemStatus {
	if s.deps.DataDir == "" {
		return subsystemStatus{Status: "error", Detail: "no data directory configured"}
	}
	probe := filepath.Join(s.deps.DataDir, fmt.Sprintf(".healthz-%d", os.Getpid()))
	if err := os.WriteFile(probe, []byte("ok"), 0o600); err != nil {
		return subsystemStatus{Status: "error", Detail: fmt.Sprintf("data dir not writable: %v", err)}
	}
	if err := os.Remove(probe); err != nil {
		return subsystemStatus{Status: "error", Detail: fmt.Sprintf("probe cleanup: %v", err)}
	}
	return subsystemStatus{Status: "ok"}
}

// probeRegistry reports the docker registry plane's availability (M2,
// PRD section 6.3): "ok" when the /v2 route has a mounted docker adapter,
// "error" with the honest reason when assembly ran without one. The probe
// answers FROM routing data only — a live /v2 round-trip would belong to
// readiness semantics, and this endpoint is the operator's dashboard, not
// a probe target.
func (s *Server) probeRegistry() subsystemStatus {
	if _, ok := s.adapters["docker"]; !ok {
		return subsystemStatus{Status: "error", Detail: "no docker adapter mounted: /v2 answers 404"}
	}
	return subsystemStatus{Status: "ok"}
}

// storageStats is the /binflow/api/v1/storage/stats body (E-23): blob
// count, logical bytes (sum of node sizes) and physical bytes (bytes on
// disk under blobs/). blob_count < referenced-node count is the
// deduplication observable (C12).
type storageStats struct {
	BlobCount     int64 `json:"blobs"`
	LogicalBytes  int64 `json:"logical_bytes"`
	PhysicalBytes int64 `json:"physical_bytes"`
}

// handleV1StorageStats answers the QA/ops stats endpoint (E-23). Requires
// authentication (mounted under /binflow/api/** — the management plane,
// ADR-0009).
func (s *Server) handleV1StorageStats(w http.ResponseWriter, r *http.Request) {
	blobCount, err := s.deps.Metadata.Blobs().Count(r.Context())
	if err != nil {
		writeError(w, http.StatusInternalServerError, fmt.Sprintf("count blobs: %v", err))
		return
	}
	logical, physical, err := countBytes(r, s.deps.Metadata, s.deps.DataDir)
	if err != nil {
		writeError(w, http.StatusInternalServerError, err.Error())
		return
	}
	body, err := json.MarshalIndent(storageStats{
		BlobCount:     blobCount,
		LogicalBytes:  logical,
		PhysicalBytes: physical,
	}, "", "  ")
	if err != nil {
		writeError(w, http.StatusInternalServerError, "render stats response: "+err.Error())
		return
	}
	w.Header().Set("Content-Type", "application/json")
	w.WriteHeader(http.StatusOK)
	_, _ = w.Write(body)
}

// metadataStore is the consumer-side slice of metadata.Store the system
// endpoints need (full Store also satisfies it — cmd wires the real one).
type metadataStore interface {
	Repos() metadata.RepoStore
	Nodes() metadata.NodeStore
	Blobs() metadata.BlobStore
}

// countBytes computes the logical total (sum of node sizes across every
// repository) and the physical total (file sizes under blobs/). The two
// numbers diverge exactly by deduplication and unreferenced-blob residue.
func countBytes(r *http.Request, md metadataStore, dataDir string) (logical, physical int64, err error) {
	repos, err := md.Repos().List(r.Context())
	if err != nil {
		return 0, 0, fmt.Errorf("list repositories for stats: %w", err)
	}
	for _, repo := range repos {
		nodes, err := md.Nodes().ListByPrefix(r.Context(), repo.RepoKey, "")
		if err != nil {
			return 0, 0, fmt.Errorf("list nodes of %s for stats: %w", repo.RepoKey, err)
		}
		for _, n := range nodes {
			logical += n.Size
		}
	}
	physical, err = dirSize(filepath.Join(dataDir, "blobs"))
	if err != nil {
		return 0, 0, fmt.Errorf("size of blobs dir: %w", err)
	}
	return logical, physical, nil
}

// dirSize sums the regular file sizes under root (shards walk; symlinked
// or irregular entries are skipped, matching the GC sweep's posture).
func dirSize(root string) (int64, error) {
	var total int64
	err := filepath.WalkDir(root, func(_ string, d os.DirEntry, err error) error {
		if err != nil {
			return err
		}
		if d.IsDir() {
			return nil
		}
		info, err := d.Info()
		if err != nil {
			return err
		}
		if info.Mode().IsRegular() {
			total += info.Size()
		}
		return nil
	})
	if err != nil {
		return 0, err
	}
	return total, nil
}
