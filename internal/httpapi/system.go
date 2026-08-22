package httpapi

import (
	"context"
	"encoding/json"
	"fmt"
	"net/http"
	"os"
	"path/filepath"
	"strings"
	"time"

	"github.com/lzwzzy/binflow/internal/storage"
	minio "github.com/minio/minio-go/v7"
	"github.com/minio/minio-go/v7/pkg/credentials"
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

// probeStorage verifies the data directory accepts writes (disk backend) or
// does a HeadBucket -> PutObject -> DeleteObject round-trip (S3 backend).
// When backend is unset, defaults to disk.
func (s *Server) probeStorage() subsystemStatus {
	backend := s.deps.Config.Storage.Backend
	if backend == "" {
		backend = "disk"
	}
	if backend == "s3" {
		return s.probeS3Storage()
	}
	return s.probeDiskStorage()
}

// probeDiskStorage verifies the data directory accepts writes: create +
// remove a probe file next to the blob store (E-22: "storage 可写探测").
func (s *Server) probeDiskStorage() subsystemStatus {
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

// SecureFromEndpoint reports whether the S3 endpoint should be dialed over
// TLS. minio-go's Secure option and the endpoint URL scheme must AGREE:
// v7 rejects an "http://host:port" endpoint built with Secure:true outright
// ("Endpoint url scheme ... conflicts with the secure option"), so a
// hardcoded Secure:true made every plain-HTTP MinIO endpoint fail the
// probe at client construction — /readyz permanently 503 (T-170 smoke
// finding, fixed in T-178). An endpoint without an explicit scheme keeps
// minio-go's TLS default.
//
// Exported as the single spelling (T-180): the /readyz probe here and cmd's
// S3 engine assembly used to carry two identical private copies — the
// drift risk the T-178 report flagged. cmd imports this one.
func SecureFromEndpoint(endpoint string) bool {
	if i := strings.Index(endpoint, "://"); i > 0 {
		return strings.EqualFold(endpoint[:i], "https")
	}
	return true
}

// probeS3Storage performs a HeadBucket -> PutObject(sentinel) -> DeleteObject
// round-trip to verify S3 connectivity and write permissions. If any step
// fails, the subsystem is reported as unhealthy.
func (s *Server) probeS3Storage() subsystemStatus {
	cfg := s.deps.Config.Storage.S3

	client, err := minio.New(cfg.Endpoint, &minio.Options{
		Creds:  credentials.NewStaticV4(cfg.AccessKeyID, cfg.SecretAccessKey, ""),
		Secure: SecureFromEndpoint(cfg.Endpoint),
		Region: cfg.Region,
	})
	if err != nil {
		return subsystemStatus{Status: "error", Detail: fmt.Sprintf("s3 client init: %v", err)}
	}

	ctx, cancel := context.WithTimeout(context.Background(), 10*time.Second)
	defer cancel()

	// Step 1: BucketExists — verify the bucket exists and is reachable.
	exists, err := client.BucketExists(ctx, cfg.Bucket)
	if err != nil {
		return subsystemStatus{Status: "error", Detail: fmt.Sprintf("s3 BucketExists %s: %v", cfg.Bucket, err)}
	}
	if !exists {
		return subsystemStatus{Status: "error", Detail: fmt.Sprintf("s3 %s: bucket does not exist", cfg.Bucket)}
	}

	// Step 2: PutObject — verify write access with a sentinel object.
	sentinelKey := fmt.Sprintf(".healthz/healthz-%d", os.Getpid())
	if cfg.BucketPrefix != "" {
		sentinelKey = cfg.BucketPrefix + "/" + sentinelKey
	}
	_, err = client.PutObject(ctx, cfg.Bucket, sentinelKey, nil, 0, minio.PutObjectOptions{
		ContentType: "text/plain",
	})
	if err != nil {
		return subsystemStatus{Status: "error", Detail: fmt.Sprintf("s3 PutObject %s/%s: %v", cfg.Bucket, sentinelKey, err)}
	}

	// Step 3: RemoveObject — clean up the sentinel.
	err = client.RemoveObject(ctx, cfg.Bucket, sentinelKey, minio.RemoveObjectOptions{})
	if err != nil {
		// Don't fail the health check if cleanup fails — the bucket is writable.
		// But log the detail so an operator can clean up the sentinel.
		return subsystemStatus{Status: "ok", Detail: fmt.Sprintf("s3 probe ok (cleanup failed: %v)", err)}
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
// disk under blobs/ — or, on an S3-backed instance, summed object bytes in
// the bucket, T-201/T-173 D-1). blob_count < referenced-node count is the
// deduplication observable (C12).
type storageStats struct {
	BlobCount     int64 `json:"blobs"`
	LogicalBytes  int64 `json:"logical_bytes"`
	PhysicalBytes int64 `json:"physical_bytes"`
}

// BlobInventory is the read-only sizing seam engine-backed instances plug
// into the stats/metrics/GC-sizing faces (T-201, T-173 D-1): one bucket
// listing answering every blob's stored size. The S3 engine implements it;
// cmd wires it only when the assembly's engine is the S3 one — a dual-write
// (migration in progress) stack keeps the disk walk because its data
// directory still carries every blob. Nil (the default) preserves the M1
// disk behavior everywhere else.
type BlobInventory interface {
	BlobStats(ctx context.Context) (map[string]storage.BlobStat, error)
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
	logical, physical, err := s.countBytes(r)
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

// countBytes computes the logical total (sum of node sizes across every
// repository) and the physical total. The two numbers diverge exactly by
// deduplication and unreferenced-blob residue.
//
// The physical half is engine-aware (T-201, T-173 D-1): a wired
// Deps.BlobInventory (S3-backed instance) sizes the bucket through the
// engine's listing — the previous unconditional dirSize(data_dir/blobs)
// made the endpoint a permanent 500 under backend=s3, where no such
// directory exists. Everything else keeps the disk walk.
func (s *Server) countBytes(r *http.Request) (logical, physical int64, err error) {
	md := s.deps.Metadata
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
	if s.deps.BlobInventory != nil {
		stats, err := s.deps.BlobInventory.BlobStats(r.Context())
		if err != nil {
			return 0, 0, fmt.Errorf("size blobs through the storage engine: %w", err)
		}
		for _, st := range stats {
			physical += st.Size
		}
		return logical, physical, nil
	}
	physical, err = dirSize(filepath.Join(s.deps.DataDir, "blobs"))
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
