// Package config loads the BinFlow server configuration from a single YAML
// file plus environment overrides, validates it fail-fast, and exposes an
// immutable value tree (architecture section 8; implemented by T-8).
//
// Resolution order: YAML file, then BINFLOW_-prefixed environment variables,
// then defaults. Environment variables use the BINFLOW_ prefix with __ as the
// nesting separator, e.g. BINFLOW_METADATA__DRIVER=postgres maps to the YAML
// key metadata.driver. Only scalar keys can be overridden; list values
// (server.cors_origins) are YAML-only.
//
// Four names are deliberate exceptions to the __ scheme:
//
//   - BINFLOW_ADMIN_PASSWORD carries the admin bootstrap password. Secrets
//     never live in the YAML file (ADR-0009); the key is rejected in YAML.
//   - BINFLOW_SECURITY_ANONYMOUS_ACCESS is the user-visible spelling of
//     security.anonymous_access (PRD C27); the regular __ spelling works too.
//   - BINFLOW_DATA_DIR is the flat convenience spelling of storage.data_dir
//     (docker -e / compose friendly); BINFLOW_STORAGE__DATA_DIR also works.
//   - BINFLOW_HOME is reserved for cmd (T-16) and is ignored here. It is a
//     different knob from BINFLOW_DATA_DIR: HOME is the directory-resolution
//     root for config/data defaults, DATA_DIR points storage at an explicit
//     path and wins over the HOME-derived default.
//
// The alias auth.anonymous_read (env BINFLOW_AUTH__ANONYMOUS_READ) is
// equivalent to security.anonymous_access; providing both with different
// values is a load error.
//
// Load fails fast on: unparseable listen port, unknown keys (strict YAML),
// more than one YAML document, secret-looking keys in YAML, bad enum values,
// non-positive durations, and a storage.data_dir that cannot be created or
// written.
package config
