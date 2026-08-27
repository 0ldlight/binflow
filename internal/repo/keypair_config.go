package repo

import (
	"context"
	"fmt"

	"github.com/lzwzzy/binflow/internal/keypair"
)

// The keypair association rules of the local repository config (M11 T-319,
// ADR-0038 decision 5 / docs/design/gpg-keypair.md section 3.3). This file
// is deliberately the ONLY keypair touchpoint of the repo package's config
// validation: T-317's smart-remote work owns config.go, and the two
// validations never share a field.

// The package-type spellings of the two signing package families (the
// addons slots' spellings; the repo package has no constants of its own
// for the gated slots — the dynamic overlay carries them).
const (
	keypairPackageDebian = "debian"
	keypairPackageRpm    = "rpm"
)

// validateLocalKeypairRef enforces the local-repository branch of the
// association matrix:
//
//   - local debian/rpm: the reference must EXIST (a dangling name refuses
//     with the field named — the create/update-time half of the delete
//     guard's invariant);
//   - every other local package type (helm included, HL-4: `.prov` files
//     are plain storage): the field is refused by name — signing is a
//     debian/rpm local-write behavior, and silently dropping the field
//     would let an admin believe a generic or helm repository signs its
//     metadata (the inert-field trap).
//
// An absent field or an explicit "" (clearing the association) passes.
func (s *service) validateLocalKeypairRef(ctx context.Context, packageType, config string) error {
	name, present, err := keypair.RepoConfigReference(config)
	if err != nil || !present || name == "" {
		return nil // the shape error and the other package rules are config.go's own decode's
	}
	if packageType != keypairPackageDebian && packageType != keypairPackageRpm {
		return fmt.Errorf(
			"%w: local %s repository config: %q is not accepted (GPG metadata signing is a debian/rpm local-repository behavior; helm provenance files are plain storage)",
			ErrInvalidRepoConfig, packageType, keypair.RepoConfigField)
	}
	rec, err := s.md.GpgKeypairs().GetKeypair(ctx, name)
	if err != nil {
		return fmt.Errorf("key pair %q: %w", name, err)
	}
	if rec == nil {
		return fmt.Errorf(
			"%w: local %s repository config: %s %q does not exist (create the key pair first: POST /binflow/api/security/keypair)",
			ErrInvalidRepoConfig, packageType, keypair.RepoConfigField, name)
	}
	return nil
}

// rejectKeypairRefOnNonLocal refuses the field on remote and virtual
// configs by name (ADR-0038 decision 5: signing is a local write-path
// behavior — a remote mirrors upstream metadata and a virtual aggregates
// members, neither signs). Presence-based, like rejectM11RemoteFields: a
// null value is still a configured field.
func rejectKeypairRefOnNonLocal(rclass, config string) error {
	name, present, err := keypair.RepoConfigReference(config)
	if err != nil || !present {
		return nil
	}
	if name == "" {
		return nil // an explicit "" is the cleared state, not a signing intent
	}
	return fmt.Errorf(
		"%w: %s repository config: %q is not accepted (GPG metadata signing is a local-repository behavior)",
		ErrInvalidRepoConfig, rclass, keypair.RepoConfigField)
}
