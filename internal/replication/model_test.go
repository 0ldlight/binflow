package replication_test

// T-161 AC ③: config/task CRUD over the real 009_replication.sql schema. The
// database is built by metadata.Open (all migrations applied, PRAGMAs in
// force), seeded with one repositories row for the source_repo FK, then
// reopened on a dedicated connection with foreign_keys ON so every constraint
// (UNIQUE name, FK source_repo, FK replication_id, cascade delete) actually
// fires — the store is exercised against the migration output, not a fixture.

import (
	"context"
	"database/sql"
	"errors"
	"fmt"
	"net/url"
	"path/filepath"
	"testing"

	_ "modernc.org/sqlite" // driver registration for the test's own connection

	"github.com/lzwzzy/binflow/internal/metadata"
	"github.com/lzwzzy/binflow/internal/replication"
)

// openStore builds a fully migrated database, seeds one local repository and
// returns a Store over a second connection. Closing the metadata store first
// also proves the 009 schema fully committed.
func openStore(t *testing.T) (context.Context, replication.Store) {
	t.Helper()
	ctx := context.Background()

	path := filepath.Join(t.TempDir(), "binflow.db")
	mst, err := metadata.Open(ctx, metadata.Options{Path: path, AdminPassword: "pw-t161"})
	if err != nil {
		t.Fatalf("metadata.Open: %v", err)
	}
	now := metadata.Now()
	if err := mst.Repos().Create(ctx, &metadata.Repo{
		RepoKey: "libs-local", Type: "local", PackageType: "generic",
		Description: "replication source", Config: "{}", CreatedAt: now, UpdatedAt: now,
	}); err != nil {
		t.Fatalf("seed repo: %v", err)
	}
	if err := mst.Close(); err != nil {
		t.Fatalf("metadata.Close: %v", err)
	}

	dsn := "file:" + url.PathEscape(path) +
		"?_pragma=foreign_keys(1)&_pragma=busy_timeout(15000)"
	db, err := sql.Open("sqlite", dsn)
	if err != nil {
		t.Fatalf("sql.Open: %v", err)
	}
	t.Cleanup(func() { _ = db.Close() })
	return ctx, replication.NewSQLiteStore(db)
}

func fixtureConfig(name string) *replication.ReplicationConfig {
	return &replication.ReplicationConfig{
		Name:                    name,
		SourceRepo:              "libs-local",
		TargetURL:               "https://remote.example.com",
		TargetRepo:              "libs-dr",
		TargetUsername:          "repl-user",
		TargetPasswordEnc:       "enc:v1:AAECAwQ=",
		MaxBandwidthBytesPerSec: 1024,
		MaxItemsPerPush:         500,
		Enabled:                 true,
		CreatedAt:               "2026-08-22T10:00:00Z",
		UpdatedAt:               "2026-08-22T10:00:00Z",
	}
}

// TestConfigCRUD walks the full config lifecycle: create, get round-trip,
// list, update, delete — the happy path of every Store method.
func TestConfigCRUD(t *testing.T) {
	ctx, st := openStore(t)

	first := fixtureConfig("dr-libs")
	id1, err := st.CreateConfig(ctx, first)
	if err != nil {
		t.Fatalf("CreateConfig: %v", err)
	}
	second := fixtureConfig("dr-libs-eu")
	second.TargetURL = "https://eu.example.com"
	second.Enabled = false
	id2, err := st.CreateConfig(ctx, second)
	if err != nil {
		t.Fatalf("CreateConfig (second): %v", err)
	}
	if id1 == id2 || id1 <= 0 || id2 <= 0 {
		t.Fatalf("CreateConfig ids = %d, %d; want distinct positives", id1, id2)
	}

	got, err := st.GetConfig(ctx, id1)
	if err != nil {
		t.Fatalf("GetConfig: %v", err)
	}
	first.ID = id1
	if *got != *first {
		t.Errorf("GetConfig round-trip = %+v, want %+v", got, first)
	}

	list, err := st.ListConfigs(ctx)
	if err != nil {
		t.Fatalf("ListConfigs: %v", err)
	}
	if len(list) != 2 || list[0].ID != id1 || list[1].ID != id2 {
		t.Errorf("ListConfigs = %d rows (ids %v), want 2 ordered [%d %d]",
			len(list), []int64{list[0].ID, list[1].ID}, id1, id2)
	}

	first.Enabled = false
	first.MaxBandwidthBytesPerSec = 0 // 0 = unlimited
	first.UpdatedAt = "2026-08-22T11:30:00Z"
	if err := st.UpdateConfig(ctx, first); err != nil {
		t.Fatalf("UpdateConfig: %v", err)
	}
	got, err = st.GetConfig(ctx, id1)
	if err != nil {
		t.Fatalf("GetConfig after update: %v", err)
	}
	if got.Enabled || got.MaxBandwidthBytesPerSec != 0 || got.UpdatedAt != first.UpdatedAt {
		t.Errorf("UpdateConfig left %+v, want enabled=false bandwidth=0 updated=%s",
			got, first.UpdatedAt)
	}

	if err := st.DeleteConfig(ctx, id2); err != nil {
		t.Fatalf("DeleteConfig: %v", err)
	}
	if _, err := st.GetConfig(ctx, id2); !errors.Is(err, replication.ErrConfigNotFound) {
		t.Errorf("GetConfig after delete err = %v, want ErrConfigNotFound", err)
	}
	list, err = st.ListConfigs(ctx)
	if err != nil {
		t.Fatalf("ListConfigs after delete: %v", err)
	}
	if len(list) != 1 || list[0].ID != id1 {
		t.Errorf("ListConfigs after delete = %+v, want only id %d", list, id1)
	}
}

// TestConfigNotFoundPaths: every keyed config method answers the typed
// sentinel, not a bare driver error.
func TestConfigNotFoundPaths(t *testing.T) {
	ctx, st := openStore(t)
	const missing = int64(4242)

	cases := []struct {
		name string
		run  func() error
	}{
		{"get", func() error { _, err := st.GetConfig(ctx, missing); return err }},
		{"update", func() error {
			return st.UpdateConfig(ctx, &replication.ReplicationConfig{
				ID: missing, Name: "ghost", SourceRepo: "libs-local",
				CreatedAt: "2026-08-22T10:00:00Z", UpdatedAt: "2026-08-22T10:00:00Z",
			})
		}},
		{"delete", func() error { return st.DeleteConfig(ctx, missing) }},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			err := tc.run()
			if !errors.Is(err, replication.ErrConfigNotFound) {
				t.Errorf("err = %v, want ErrConfigNotFound", err)
			}
		})
	}
}

// TestConfigDuplicateName: the UNIQUE(name) constraint surfaces as the typed
// ErrDuplicateName on both create and rename.
func TestConfigDuplicateName(t *testing.T) {
	ctx, st := openStore(t)

	keeper := fixtureConfig("taken")
	keeperID, err := st.CreateConfig(ctx, keeper)
	if err != nil {
		t.Fatalf("CreateConfig keeper: %v", err)
	}

	cases := []struct {
		name string
		run  func() error
	}{
		{"create same name", func() error {
			_, err := st.CreateConfig(ctx, fixtureConfig("taken"))
			return err
		}},
		{"rename onto taken name", func() error {
			other := fixtureConfig("free-name")
			other.ID = keeperID + 100 // an id that does not exist yet is created first
			newID, err := st.CreateConfig(ctx, other)
			if err != nil {
				t.Fatalf("CreateConfig other: %v", err)
			}
			other.ID = newID
			other.Name = "taken"
			return st.UpdateConfig(ctx, other)
		}},
		{"self-rename keeps its name", func() error { // control: NOT a violation
			keeper.ID = keeperID
			return st.UpdateConfig(ctx, keeper)
		}},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			err := tc.run()
			if tc.name == "self-rename keeps its name" {
				if err != nil {
					t.Fatalf("updating a row with its own name must not conflict, got %v", err)
				}
				return
			}
			if !errors.Is(err, replication.ErrDuplicateName) {
				t.Errorf("err = %v, want ErrDuplicateName", err)
			}
		})
	}
}

// TestConfigFKSourceRepo: replications.source_repo references
// repositories.repo_key — an unknown repo is rejected by the FK (the service
// layer's pre-check is convenience, the schema is the guarantee).
func TestConfigFKSourceRepo(t *testing.T) {
	ctx, st := openStore(t)

	c := fixtureConfig("orphan")
	c.SourceRepo = "no-such-repo"
	_, err := st.CreateConfig(ctx, c)
	if err == nil {
		t.Fatal("CreateConfig with unknown source_repo: want FK error, got nil")
	}
	for _, sentinel := range []error{
		replication.ErrConfigNotFound, replication.ErrDuplicateName,
		replication.ErrTaskNotFound, replication.ErrInvalidStatus,
	} {
		if errors.Is(err, sentinel) {
			t.Errorf("FK violation misclassified as %v (err %v)", sentinel, err)
		}
	}
}

// TestTaskCRUD walks the task lifecycle: enqueue defaults to pending, status
// transitions carry attempts/last_error/completed_at, delete removes.
func TestTaskCRUD(t *testing.T) {
	ctx, st := openStore(t)

	cfgID, err := st.CreateConfig(ctx, fixtureConfig("dr-libs"))
	if err != nil {
		t.Fatalf("CreateConfig: %v", err)
	}

	// Enqueue with an empty status: defaults to pending (the engine's call
	// site never spells it).
	taskID, err := st.CreateTask(ctx, &replication.ReplicationTask{
		ReplicationID: cfgID,
		BlobSHA256:    "aa1256a02c81b9bec7f0e5c3f5d2b7e44d4b5e6f7a8b9c0d1e2f3a4b5c6d7e8",
		NodePath:      "org/acme/lib/1.0/lib-1.0.jar",
		CreatedAt:     "2026-08-22T10:05:00Z",
	})
	if err != nil {
		t.Fatalf("CreateTask: %v", err)
	}

	got, err := st.GetTask(ctx, taskID)
	if err != nil {
		t.Fatalf("GetTask: %v", err)
	}
	want := &replication.ReplicationTask{
		ID: taskID, ReplicationID: cfgID,
		BlobSHA256: "aa1256a02c81b9bec7f0e5c3f5d2b7e44d4b5e6f7a8b9c0d1e2f3a4b5c6d7e8",
		NodePath:   "org/acme/lib/1.0/lib-1.0.jar",
		Status:     replication.TaskStatusPending,
		CreatedAt:  "2026-08-22T10:05:00Z",
	}
	if *got != *want {
		t.Errorf("GetTask round-trip = %+v, want %+v", got, want)
	}

	// The engine's transition sequence: pending -> in_progress (attempt 1)
	// -> success (completed_at set).
	steps := []struct {
		status      string
		attempts    int64
		lastError   string
		completedAt string
	}{
		{replication.TaskStatusInProgress, 1, "", ""},
		{replication.TaskStatusSuccess, 1, "", "2026-08-22T10:05:09Z"},
	}
	for _, step := range steps {
		got.Status = step.status
		got.Attempts = step.attempts
		got.LastError = step.lastError
		got.CompletedAt = step.completedAt
		if err := st.UpdateTask(ctx, got); err != nil {
			t.Fatalf("UpdateTask -> %s: %v", step.status, err)
		}
	}
	got, err = st.GetTask(ctx, taskID)
	if err != nil {
		t.Fatalf("GetTask after transitions: %v", err)
	}
	if got.Status != replication.TaskStatusSuccess || got.Attempts != 1 ||
		got.CompletedAt != "2026-08-22T10:05:09Z" || got.LastError != "" {
		t.Errorf("task after transitions = %+v, want success/1/completed", got)
	}

	// A failed retry records the reason and keeps completed_at empty (failed
	// is retryable, not terminal).
	got.Status = replication.TaskStatusFailed
	got.Attempts = 2
	got.LastError = "push: 507 insufficient storage"
	got.CompletedAt = ""
	if err := st.UpdateTask(ctx, got); err != nil {
		t.Fatalf("UpdateTask -> failed: %v", err)
	}

	if err := st.DeleteTask(ctx, taskID); err != nil {
		t.Fatalf("DeleteTask: %v", err)
	}
	if _, err := st.GetTask(ctx, taskID); !errors.Is(err, replication.ErrTaskNotFound) {
		t.Errorf("GetTask after delete err = %v, want ErrTaskNotFound", err)
	}
}

// TestTaskInvalidStatus: the DDL column is free-form TEXT; the store keeps
// the domain closed to the five TaskStatus* constants.
func TestTaskInvalidStatus(t *testing.T) {
	ctx, st := openStore(t)

	cfgID, err := st.CreateConfig(ctx, fixtureConfig("dr-libs"))
	if err != nil {
		t.Fatalf("CreateConfig: %v", err)
	}
	taskID, err := st.CreateTask(ctx, &replication.ReplicationTask{
		ReplicationID: cfgID, NodePath: "a.jar", CreatedAt: "2026-08-22T10:05:00Z",
	})
	if err != nil {
		t.Fatalf("CreateTask: %v", err)
	}

	cases := []struct {
		name   string
		status string
		run    func(status string) error
	}{
		{"create bogus", "bogus", func(s string) error {
			_, err := st.CreateTask(ctx, &replication.ReplicationTask{
				ReplicationID: cfgID, NodePath: "x.jar", Status: s,
				CreatedAt: "2026-08-22T10:06:00Z",
			})
			return err
		}},
		{"create uppercase", "PENDING", func(s string) error {
			_, err := st.CreateTask(ctx, &replication.ReplicationTask{
				ReplicationID: cfgID, NodePath: "y.jar", Status: s,
				CreatedAt: "2026-08-22T10:06:01Z",
			})
			return err
		}},
		{"update empty means invalid (no default on update)", "", func(s string) error {
			return st.UpdateTask(ctx, &replication.ReplicationTask{
				ID: taskID, Status: s, CreatedAt: "2026-08-22T10:05:00Z",
			})
		}},
		{"update done", "done", func(s string) error {
			return st.UpdateTask(ctx, &replication.ReplicationTask{
				ID: taskID, Status: s, CreatedAt: "2026-08-22T10:05:00Z",
			})
		}},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			err := tc.run(tc.status)
			if !errors.Is(err, replication.ErrInvalidStatus) {
				t.Errorf("status %q: err = %v, want ErrInvalidStatus", tc.status, err)
			}
		})
	}
}

// TestTaskNotFoundPaths: keyed task methods answer ErrTaskNotFound.
func TestTaskNotFoundPaths(t *testing.T) {
	ctx, st := openStore(t)
	const missing = int64(9242)

	cases := []struct {
		name string
		run  func() error
	}{
		{"get", func() error { _, err := st.GetTask(ctx, missing); return err }},
		{"update", func() error {
			return st.UpdateTask(ctx, &replication.ReplicationTask{
				ID: missing, Status: replication.TaskStatusPending,
			})
		}},
		{"delete", func() error { return st.DeleteTask(ctx, missing) }},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			err := tc.run()
			if !errors.Is(err, replication.ErrTaskNotFound) {
				t.Errorf("err = %v, want ErrTaskNotFound", err)
			}
		})
	}
}

// TestTaskFKReplication: replication_tasks.replication_id references
// replications.id — an orphan task is rejected by the FK.
func TestTaskFKReplication(t *testing.T) {
	ctx, st := openStore(t)

	_, err := st.CreateTask(ctx, &replication.ReplicationTask{
		ReplicationID: 4242, NodePath: "orphan.jar", CreatedAt: "2026-08-22T10:05:00Z",
	})
	if err == nil {
		t.Fatal("CreateTask with unknown replication_id: want FK error, got nil")
	}
	if errors.Is(err, replication.ErrTaskNotFound) || errors.Is(err, replication.ErrInvalidStatus) {
		t.Errorf("FK violation misclassified (err %v)", err)
	}
}

// TestListTasksNewestFirst: the panel event list is newest-first and honors
// the limit.
func TestListTasksNewestFirst(t *testing.T) {
	ctx, st := openStore(t)

	cfgID, err := st.CreateConfig(ctx, fixtureConfig("dr-libs"))
	if err != nil {
		t.Fatalf("CreateConfig: %v", err)
	}
	paths := []string{"c.jar", "a.jar", "b.jar"}
	times := []string{
		"2026-08-22T10:07:00Z",
		"2026-08-22T10:08:00Z",
		"2026-08-22T10:09:00Z",
	}
	for i, p := range paths {
		if _, err := st.CreateTask(ctx, &replication.ReplicationTask{
			ReplicationID: cfgID, NodePath: p,
			CreatedAt: times[i],
		}); err != nil {
			t.Fatalf("CreateTask %s: %v", p, err)
		}
	}

	got, err := st.ListTasks(ctx, cfgID, 0)
	if err != nil {
		t.Fatalf("ListTasks: %v", err)
	}
	if len(got) != 3 {
		t.Fatalf("ListTasks = %d rows, want 3", len(got))
	}
	wantOrder := []string{"b.jar", "a.jar", "c.jar"} // created_at DESC
	for i, want := range wantOrder {
		if got[i].NodePath != want {
			t.Errorf("ListTasks[%d].NodePath = %q, want %q (newest first)", i, got[i].NodePath, want)
		}
	}

	capped, err := st.ListTasks(ctx, cfgID, 2)
	if err != nil {
		t.Fatalf("ListTasks (limit 2): %v", err)
	}
	if len(capped) != 2 || capped[0].NodePath != "b.jar" || capped[1].NodePath != "a.jar" {
		t.Errorf("ListTasks limit 2 = %v, want the two newest [b.jar a.jar]", capped)
	}

	// A config with no tasks answers an empty list, not an error.
	other, err := st.CreateConfig(ctx, fixtureConfig("dr-idle"))
	if err != nil {
		t.Fatalf("CreateConfig (idle): %v", err)
	}
	empty, err := st.ListTasks(ctx, other, 0)
	if err != nil || len(empty) != 0 {
		t.Errorf("ListTasks of task-less config = %v (err %v), want empty", empty, err)
	}
}

// TestConfigDeleteCascadesTasks: dropping a config takes its task history
// with it (ON DELETE CASCADE, the no-orphan-tasks invariant).
func TestConfigDeleteCascadesTasks(t *testing.T) {
	ctx, st := openStore(t)

	cfgID, err := st.CreateConfig(ctx, fixtureConfig("dr-libs"))
	if err != nil {
		t.Fatalf("CreateConfig: %v", err)
	}
	for i := 0; i < 2; i++ {
		if _, err := st.CreateTask(ctx, &replication.ReplicationTask{
			ReplicationID: cfgID,
			NodePath:      fmt.Sprintf("org/acme/%d.jar", i),
			CreatedAt:     "2026-08-22T10:10:00Z",
		}); err != nil {
			t.Fatalf("CreateTask %d: %v", i, err)
		}
	}

	if err := st.DeleteConfig(ctx, cfgID); err != nil {
		t.Fatalf("DeleteConfig: %v", err)
	}
	tasks, err := st.ListTasks(ctx, cfgID, 0)
	if err != nil {
		t.Fatalf("ListTasks after cascade: %v", err)
	}
	if len(tasks) != 0 {
		t.Errorf("ListTasks after config delete = %d rows, want 0 (cascade)", len(tasks))
	}
}

// TestStatus: the derived per-config state (T-159 panel source) counts every
// status bucket and reports the newest successful push; an unknown or idle
// config answers zeros, not an error.
func TestStatus(t *testing.T) {
	ctx, st := openStore(t)

	cfgID, err := st.CreateConfig(ctx, fixtureConfig("dr-libs"))
	if err != nil {
		t.Fatalf("CreateConfig: %v", err)
	}
	seed := []struct {
		status      string
		completedAt string
	}{
		{replication.TaskStatusPending, ""},
		{replication.TaskStatusPending, ""},
		{replication.TaskStatusInProgress, ""},
		{replication.TaskStatusSuccess, "2026-08-22T10:15:00Z"},
		{replication.TaskStatusSuccess, "2026-08-22T10:16:00Z"},
		{replication.TaskStatusSuccess, "2026-08-22T10:17:00Z"},
		{replication.TaskStatusFailed, ""},
		{replication.TaskStatusSkipped, "2026-08-22T10:14:00Z"},
	}
	for i, s := range seed {
		if _, err := st.CreateTask(ctx, &replication.ReplicationTask{
			ReplicationID: cfgID,
			NodePath:      fmt.Sprintf("org/acme/task-%d.jar", i),
			Status:        s.status,
			CompletedAt:   s.completedAt,
			CreatedAt:     fmt.Sprintf("2026-08-22T10:1%d:00Z", i%10),
		}); err != nil {
			t.Fatalf("CreateTask %d (%s): %v", i, s.status, err)
		}
	}

	got, err := st.Status(ctx, cfgID)
	if err != nil {
		t.Fatalf("Status: %v", err)
	}
	want := replication.ConfigStatus{
		ReplicationID: cfgID,
		Pending:       2, InProgress: 1, Succeeded: 3, Failed: 1, Skipped: 1,
		LastSuccessAt: "2026-08-22T10:17:00Z",
	}
	if *got != want {
		t.Errorf("Status = %+v, want %+v", got, want)
	}

	idle, err := st.Status(ctx, cfgID+100)
	if err != nil {
		t.Fatalf("Status (unknown id): %v", err)
	}
	if idle.ReplicationID != cfgID+100 || idle.Pending != 0 || idle.Succeeded != 0 ||
		idle.Failed != 0 || idle.InProgress != 0 || idle.Skipped != 0 || idle.LastSuccessAt != "" {
		t.Errorf("Status of unknown id = %+v, want all zeros", idle)
	}
}
