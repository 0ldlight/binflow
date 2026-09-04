// The migration-023 dry-run report (M16 T-444, FR-146.1 / ADR-0044 K68
// point 4 / architecture section 25.6): the write→deploy-cache/annotate
// split's mapping evidence, machine-readable. Two pure reads over the raw
// database handle (the CurrentVersion seam — the public Store interface
// deliberately exposes no DDL):
//
//   - AnnotateMappingReport is the DRY-RUN arm: the full-row mapping table
//     of every permission_principals row, computed from can_write alone so
//     it runs identically against a pre-023 database (the actual dry run)
//     and a post-023 one. Every row maps deterministically — the mapping
//     rate must be 100% and the annotate column of the table is exactly the
//     backfill set (≡ the write set, K68's bidirectional assertion).
//   - AnnotateBackfillDiff is the POST-APPLY arm: it reads both stored
//     columns and reports the two disagreement counts whose zero is the set
//     equality "annotate_after ⇔ write_before".
//
// Rollback posture (K68 point 4): can_write is untouched by the whole
// cycle, so the reverse migration is the column's deprecation (DROP or
// retain-with-zero-consumers) plus the wire word flip — no data loss in
// either direction.

package metadata

import (
	"context"
	"database/sql"
	"fmt"
)

// AnnotateMappingRow is one principal row's entry of the dry-run mapping
// table: what the row maps to under the split — deploy-cache (can_write,
// value unchanged) plus annotate (the backfill, = the write bit).
type AnnotateMappingRow struct {
	TargetName    string `json:"target"`
	Principal     string `json:"principal"`
	PrincipalType string `json:"type"`
	// DeployCache is the row's can_write bit — the deploy-cache grant the
	// split leaves exactly where it was.
	DeployCache bool `json:"deploy-cache"`
	// AnnotateBackfill is the can_annotate value migration 023 writes for
	// the row: the write bit again (the pre-split property-write gate was
	// `w`, so existing grants follow both halves).
	AnnotateBackfill bool `json:"annotate"`
}

// AnnotateMappingReport is the dry-run document: every principal row, the
// counts, and the mapping rate. A rate below 100 means a row with no
// deterministic mapping — impossible while the mapping is the pure function
// of can_write defined here, which is exactly why the invariant is worth
// asserting rather than assuming.
type AnnotateMappingReport struct {
	Rows []AnnotateMappingRow `json:"rows"`
	// TotalRows is the permission_principals row count.
	TotalRows int `json:"total_rows"`
	// MappedRows counts rows with a deterministic mapping.
	MappedRows int `json:"mapped_rows"`
	// WriteRows counts can_write=1 rows — the backfill set's size.
	WriteRows int `json:"write_rows"`
	// MappingRatePercent is MappedRows/TotalRows*100 (100.0 for an empty
	// table — the vacuous truth of "every row maps").
	MappingRatePercent float64 `json:"mapping_rate_percent"`
}

// AnnotateMappingDryRun computes the dry-run mapping table (see the file
// comment). It reads can_write only, so the same call serves the pre-run
// preview and the post-run recomputation.
func AnnotateMappingDryRun(ctx context.Context, db *sql.DB) (*AnnotateMappingReport, error) {
	rows, err := db.QueryContext(ctx,
		`SELECT target_name, principal, principal_type, can_write
		FROM permission_principals ORDER BY target_name, principal_type, principal`)
	if err != nil {
		return nil, fmt.Errorf("metadata: annotate mapping report: %w", err)
	}
	defer func() { _ = rows.Close() }()
	rep := &AnnotateMappingReport{Rows: []AnnotateMappingRow{}}
	for rows.Next() {
		var r AnnotateMappingRow
		var canWrite int
		if err := rows.Scan(&r.TargetName, &r.Principal, &r.PrincipalType, &canWrite); err != nil {
			return nil, fmt.Errorf("metadata: annotate mapping report scan: %w", err)
		}
		r.DeployCache = canWrite != 0
		r.AnnotateBackfill = r.DeployCache // the backfill law: annotate := write
		rep.Rows = append(rep.Rows, r)
		rep.TotalRows++
		rep.MappedRows++ // the mapping is total by construction; the report proves it
		if r.DeployCache {
			rep.WriteRows++
		}
	}
	if err := rows.Err(); err != nil {
		return nil, fmt.Errorf("metadata: annotate mapping report rows: %w", err)
	}
	rep.MappingRatePercent = 100
	if rep.TotalRows > 0 {
		rep.MappingRatePercent = float64(rep.MappedRows) / float64(rep.TotalRows) * 100
	}
	return rep, nil
}

// AnnotateBackfillDiff is the post-apply verification: the two directions
// of the set equality "backfilled annotate set ≡ write set". Both
// disagreement counts must be zero (K68 point 4's bidirectional assertion);
// a non-empty disagreement names the offending rows.
type AnnotateBackfillDiff struct {
	WriteRows            int      `json:"write_rows"`
	AnnotateRows         int      `json:"annotate_rows"`
	WriteWithoutAnnotate int      `json:"write_without_annotate"`
	AnnotateWithoutWrite int      `json:"annotate_without_write"`
	Disagreements        []string `json:"disagreements,omitempty"`
}

// AnnotateBackfillVerify reads both stored action columns and reports the
// set-equality evidence (see the file comment). It requires the 023 schema
// (can_annotate) — against a pre-023 database it fails with the driver's
// no-such-column error, which is the honest answer for a post-apply check.
func AnnotateBackfillVerify(ctx context.Context, db *sql.DB) (*AnnotateBackfillDiff, error) {
	rows, err := db.QueryContext(ctx,
		`SELECT target_name, principal, principal_type, can_write, can_annotate
		FROM permission_principals ORDER BY target_name, principal_type, principal`)
	if err != nil {
		return nil, fmt.Errorf("metadata: annotate backfill diff: %w", err)
	}
	defer func() { _ = rows.Close() }()
	d := &AnnotateBackfillDiff{}
	key := func(target, principal, ptype string) string {
		return ptype + ":" + principal + "@" + target
	}
	for rows.Next() {
		var target, principal, ptype string
		var canWrite, canAnnotate int
		if err := rows.Scan(&target, &principal, &ptype, &canWrite, &canAnnotate); err != nil {
			return nil, fmt.Errorf("metadata: annotate backfill diff scan: %w", err)
		}
		w, a := canWrite != 0, canAnnotate != 0
		if w {
			d.WriteRows++
		}
		if a {
			d.AnnotateRows++
		}
		if w && !a {
			d.WriteWithoutAnnotate++
			d.Disagreements = append(d.Disagreements, key(target, principal, ptype)+" (write, no annotate)")
		}
		if a && !w {
			d.AnnotateWithoutWrite++
			d.Disagreements = append(d.Disagreements, key(target, principal, ptype)+" (annotate, no write)")
		}
	}
	if err := rows.Err(); err != nil {
		return nil, fmt.Errorf("metadata: annotate backfill diff rows: %w", err)
	}
	return d, nil
}
