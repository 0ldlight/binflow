//go:build tools

// Package tools pins M6 dependency versions so they survive go mod tidy.
// ADR-0005 (T-149): these dependencies are architect-approved and will be
// imported by M6 feature code (S3 storage, OIDC auth, LDAP auth). The
// build constraint ensures they stay in go.mod without being imported by
// any production code yet.
package tools

import (
	_ "github.com/coreos/go-oidc/v3/oidc" // ADR-0020: OIDC auth (M6)
	_ "github.com/go-ldap/ldap/v3"        // ADR-0020: LDAP auth (M6)
	_ "github.com/minio/minio-go/v7"      // ADR-0018: S3 storage backend (M6)
)
