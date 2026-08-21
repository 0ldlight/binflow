// Package replication holds the push-replication domain model (M6, ADR-0021):
// one ReplicationConfig per source repository → target BinFlow instance
// mapping, and the ReplicationTask ledger the push engine (T-162) drains.
// Pull replication reuses the remote-repository mechanism and has no state
// here (ADR-0021 decision 2).
//
// The Store interface is defined on the consumer side (architecture section 3:
// interfaces live with their consumers); the SQLite implementation over the
// 009_replication.sql schema lives in store.go. Wiring the store into
// metadata.Open's sub-store surface is the metadata-side bridge ticket; this
// package only owns the model, the SQL CRUD and their contracts.
package replication

import (
	"context"
	"errors"
)

// Sentinel errors. Wrapping is allowed as long as errors.Is keeps working;
// implementations wrap driver-level detail underneath these.
var (
	// ErrConfigNotFound is returned by config Get/Update/Delete when no
	// replications row carries the requested id.
	ErrConfigNotFound = errors.New("replication: config not found")
	// ErrTaskNotFound is returned by task Get/Update/Delete when no
	// replication_tasks row carries the requested id.
	ErrTaskNotFound = errors.New("replication: task not found")
	// ErrDuplicateName is returned by CreateConfig/UpdateConfig when the
	// unique name is already taken by another config row.
	ErrDuplicateName = errors.New("replication: duplicate config name")
	// ErrInvalidStatus is returned by CreateTask/UpdateTask when the status
	// value is not one of the TaskStatus* constants (the 009 DDL column is
	// free-form TEXT; the store keeps the domain closed).
	ErrInvalidStatus = errors.New("replication: invalid task status")
)

// Task status values (009_replication.sql DDL comment block). These are the
// only values the store accepts; pending is the enqueue state, success and
// skipped are terminal, failed stays retryable (attempts < engine cap).
const (
	TaskStatusPending    = "pending"
	TaskStatusInProgress = "in_progress"
	TaskStatusSuccess    = "success"
	TaskStatusFailed     = "failed"
	TaskStatusSkipped    = "skipped"
)

// validTaskStatus reports whether s is one of the TaskStatus* constants.
func validTaskStatus(s string) bool {
	switch s {
	case TaskStatusPending, TaskStatusInProgress, TaskStatusSuccess,
		TaskStatusFailed, TaskStatusSkipped:
		return true
	default:
		return false
	}
}

// ReplicationConfig is one row of replications (009): a push replication
// target. Timestamps are RFC3339 UTC text (ADR-0007).
//
// TargetPasswordEnc is opaque to this package: like remote_configs.password
// it carries 'enc:v1:<b64(nonce+ciphertext)>' AES-256-GCM text (ADR-0012
// format, ADR-0021 reuse). The store neither inspects nor transforms it, and
// callers must never log it.
//
//nolint:revive // T-161 AC names the type ReplicationConfig; the domain qualifier outlives the ticket's naming contract.
type ReplicationConfig struct {
	ID                int64
	Name              string // unique, human-readable
	SourceRepo        string // -> repositories.repo_key (FK, cascade on repo delete)
	TargetURL         string // target BinFlow base URL, e.g. https://remote.example.com
	TargetRepo        string // repo key on the target instance
	TargetUsername    string
	TargetPasswordEnc string
	// MaxBandwidthBytesPerSec throttles one push trigger; 0 = unlimited.
	MaxBandwidthBytesPerSec int64
	// MaxItemsPerPush caps the items one trigger may enqueue; the 009 DDL
	// default is 1000.
	MaxItemsPerPush int64
	Enabled         bool
	CreatedAt       string
	UpdatedAt       string
}

// ReplicationTask is one row of replication_tasks (009): a single blob push
// attempt. The table is a job ledger, not a dedup set — re-enqueuing the same
// (replication, path) appends a new row, and history accumulates for the
// console event list (T-159).
//
//nolint:revive // T-161 AC names the type ReplicationTask; the domain qualifier outlives the ticket's naming contract.
type ReplicationTask struct {
	ID            int64
	ReplicationID int64  // -> replications.id (FK, cascade on config delete)
	BlobSHA256    string // hex sha256 of the blob being replicated
	NodePath      string // repo-relative path of the node
	Status        string // TaskStatus*; '' means pending at enqueue time
	Attempts      int64
	LastError     string // '' until a failure records the reason
	CreatedAt     string
	CompletedAt   string // '' while pending/in_progress; set on terminal states
}

// ConfigStatus is the derived per-config state the console panel needs
// (T-159: URL/repo/enabled come from the config row; pending/error counts and
// the last successful push time do not exist as columns — 009 keeps them
// derived from replication_tasks). Counts are cheap: the
// idx_replication_tasks_status index serves the grouped scan.
type ConfigStatus struct {
	ReplicationID int64
	Pending       int64
	InProgress    int64
	Succeeded     int64
	Failed        int64
	Skipped       int64
	// LastSuccessAt is the MAX(completed_at) over successful tasks — '' when
	// nothing ever succeeded. RFC3339 UTC text compares chronologically, so
	// the lexicographic MAX is the newest success.
	LastSuccessAt string
}

// Store is the persistence seam of the replication domain. Implementations
// must be safe for concurrent use and take an explicit context on every
// method; timestamps are RFC3339 UTC text supplied by the caller (ADR-0007)
// so the store stays deterministic.
type Store interface {
	// CreateConfig inserts one config and returns the new row id. A name
	// already taken surfaces ErrDuplicateName; SourceRepo must reference an
	// existing repositories row (the FK rejects unknown repos).
	CreateConfig(ctx context.Context, c *ReplicationConfig) (int64, error)
	// GetConfig returns the config row; ErrConfigNotFound when absent.
	GetConfig(ctx context.Context, id int64) (*ReplicationConfig, error)
	// UpdateConfig refreshes every non-key column of the id (rename allowed,
	// subject to ErrDuplicateName); ErrConfigNotFound when the id is absent.
	// ID and CreatedAt are immutable.
	UpdateConfig(ctx context.Context, c *ReplicationConfig) error
	// DeleteConfig removes the config; its task rows cascade via the FK;
	// ErrConfigNotFound when absent.
	DeleteConfig(ctx context.Context, id int64) error
	// ListConfigs returns every config ordered by id.
	ListConfigs(ctx context.Context) ([]*ReplicationConfig, error)

	// CreateTask inserts one task and returns the new row id. An empty Status
	// defaults to pending (the enqueue call site); an unknown status value is
	// rejected with ErrInvalidStatus. ReplicationID must reference an
	// existing config row (the FK rejects orphans).
	CreateTask(ctx context.Context, t *ReplicationTask) (int64, error)
	// GetTask returns the task row; ErrTaskNotFound when absent.
	GetTask(ctx context.Context, id int64) (*ReplicationTask, error)
	// UpdateTask persists the mutable columns (status, attempts, last_error,
	// completed_at); ErrTaskNotFound when the id is absent, ErrInvalidStatus
	// on an unknown status value.
	UpdateTask(ctx context.Context, t *ReplicationTask) error
	// DeleteTask removes one row; ErrTaskNotFound when absent.
	DeleteTask(ctx context.Context, id int64) error
	// ListTasks returns the tasks of one replication newest-first
	// (created_at DESC, id DESC) — the panel event list. limit<=0 means 100.
	ListTasks(ctx context.Context, replicationID int64, limit int) ([]*ReplicationTask, error)

	// Status returns the derived per-config state. An id with no task rows
	// (or an unknown id) answers a zero ConfigStatus, not an error — the
	// counts are facts about tasks, and "never run yet" is a valid state.
	Status(ctx context.Context, replicationID int64) (*ConfigStatus, error)
}
