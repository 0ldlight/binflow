package config

import (
	"bytes"
	"errors"
	"fmt"
	"io"
	"net"
	"os"
	"path/filepath"
	"sort"
	"strconv"
	"strings"
	"time"

	yaml "go.yaml.in/yaml/v3"
)

// raw mirrors the YAML file schema (architecture section 8). Every scalar is a
// pointer so "absent" is distinguishable from "explicitly zero": absent fields
// fall back to defaults, explicit fields win, and the anonymous-access alias
// needs to know whether it was set at all. Unknown keys are rejected by the
// strict decoder, not silently dropped.
type raw struct {
	Server *struct {
		Listen                 *string  `yaml:"listen"`
		BaseURL                *string  `yaml:"base_url"`
		CORSOrigins            []string `yaml:"cors_origins"`
		GracefulTimeoutSeconds *int     `yaml:"graceful_timeout_seconds"`
	} `yaml:"server"`
	Storage *struct {
		DataDir          *string `yaml:"data_dir"`
		Backend          *string `yaml:"backend"`
		SessionTTLHours  *int    `yaml:"session_ttl_hours"`
		GCGraceHours     *int    `yaml:"gc_grace_hours"`
		GCHoldTTLSeconds *int    `yaml:"gc_hold_ttl_seconds"`
		S3               *struct {
			Bucket            *string `yaml:"bucket"`
			Region            *string `yaml:"region"`
			Endpoint          *string `yaml:"endpoint"`
			AccessKeyID       *string `yaml:"access_key_id"`
			UsePathStyle      *bool   `yaml:"use_path_style"`
			UploadPartSize    *int64  `yaml:"upload_part_size"`
			UploadConcurrency *int    `yaml:"upload_concurrency"`
			BucketPrefix      *string `yaml:"bucket_prefix"`
		} `yaml:"s3"`
		Migration *struct {
			Enabled     *bool `yaml:"enabled"`
			Completed   *bool `yaml:"completed"`
			Concurrency *int  `yaml:"concurrency"`
		} `yaml:"migration"`
	} `yaml:"storage"`
	Metadata *struct {
		Driver *string `yaml:"driver"`
		DSN    *string `yaml:"dsn"`
	} `yaml:"metadata"`
	Auth *struct {
		Argon2MemoryMB         *int  `yaml:"argon2_memory_mb"`
		TokenDefaultTTLHours   *int  `yaml:"token_default_ttl_hours"`
		TokenNonAdminMaxTTLSec *int  `yaml:"token_nonadmin_max_ttl"`
		TokenStepUp            *bool `yaml:"token_step_up"`
		TokenStepUpGrantTTLSec *int  `yaml:"token_step_up_grant_ttl_seconds"`
		HashConcurrency        *int  `yaml:"hash_concurrency"`
		AnonymousRead          *bool `yaml:"anonymous_read"`
		OIDC                   *struct {
			Enabled       *bool    `yaml:"enabled"`
			IssuerURL     *string  `yaml:"issuer_url"`
			ClientID      *string  `yaml:"client_id"`
			RedirectURL   *string  `yaml:"redirect_url"`
			Scopes        []string `yaml:"scopes"`
			UserClaim     *string  `yaml:"user_claim"`
			GroupClaim    *string  `yaml:"group_claim"`
			AdminGroup    *string  `yaml:"admin_group"`
			ReadOnlyGroup *string  `yaml:"readonly_group"`
		} `yaml:"oidc"`
		LDAP *struct {
			Enabled       *bool   `yaml:"enabled"`
			URL           *string `yaml:"url"`
			BaseDN        *string `yaml:"base_dn"`
			BindDN        *string `yaml:"bind_dn"`
			UserFilter    *string `yaml:"user_filter"`
			UserIDAttr    *string `yaml:"user_id_attr"`
			GroupFilter   *string `yaml:"group_filter"`
			GroupBaseDN   *string `yaml:"group_base_dn"`
			GroupNameAttr *string `yaml:"group_name_attr"`
			AdminGroup    *string `yaml:"admin_group"`
			ReadOnlyGroup *string `yaml:"readonly_group"`
			PoolSize      *int    `yaml:"pool_size"`
			StartTLS      *bool   `yaml:"start_tls"`
			SkipTLSVerify *bool   `yaml:"skip_tls_verify"`
		} `yaml:"ldap"`
	} `yaml:"auth"`
	Security *struct {
		AnonymousAccess *bool `yaml:"anonymous_access"`
	} `yaml:"security"`
	Audit *struct {
		Enabled *bool `yaml:"enabled"`
	} `yaml:"audit"`
	Logging *struct {
		Level  *string `yaml:"level"`
		Format *string `yaml:"format"`
	} `yaml:"logging"`
	Console *struct {
		SessionTTLHours   *int `yaml:"session_ttl_hours"`
		SessionTTLSeconds *int `yaml:"session_ttl_seconds"`
	} `yaml:"console"`
	Metrics *struct {
		RequireAuth *bool `yaml:"require_auth"`
	} `yaml:"metrics"`
	Replication *struct {
		AllowPrivateTarget *bool `yaml:"allow_private_target"`
	} `yaml:"replication"`
	// Addons is the M10 circuit-breaker section (T-283, ADR-0032 / section
	// 15.5): disabled is a CSV of addon ids, restart-effective.
	Addons *struct {
		Disabled *string `yaml:"disabled"`
	} `yaml:"addons"`
}

// Load reads the YAML file at path, applies BINFLOW_-prefixed environment
// overrides, fills defaults, and validates the result. Any problem — missing
// file, malformed YAML, unknown key, bad enum, unwritable data_dir — is a
// returned error; the caller must refuse to start on it (fail-fast).
func Load(path string) (*Config, error) {
	src, err := os.ReadFile(path) // operator-provided config path by design (G304 excluded globally)
	if err != nil {
		return nil, fmt.Errorf("config: read %s: %w", path, err)
	}
	r, err := decodeRaw(src)
	if err != nil {
		return nil, fmt.Errorf("config: parse %s: %w", path, err)
	}
	return build(r, environ())
}

// decodeRaw unmarshals YAML strictly: unknown keys and duplicate keys are
// errors, the document must be a mapping (a null document counts as empty),
// and the stream must hold exactly one document — a second document would
// otherwise bypass both the secret scan and strict decoding (review B1).
func decodeRaw(src []byte) (*raw, error) {
	var root yaml.Node
	if err := yaml.Unmarshal(src, &root); err != nil {
		return nil, err
	}
	r := &raw{}
	if len(root.Content) == 0 { // empty file: everything falls back to defaults
		return r, nil
	}
	doc := root.Content[0]
	isNull := doc.Tag == "!!null"
	if !isNull && doc.Kind != yaml.MappingNode {
		return nil, errors.New("config file must be a YAML mapping")
	}
	if !isNull {
		if err := rejectSecrets(doc); err != nil {
			return nil, err
		}
	}
	dec := yaml.NewDecoder(bytes.NewReader(src))
	dec.KnownFields(true)
	if err := dec.Decode(r); err != nil {
		return nil, err
	}
	// The strict decoder only consumed the first document; require the stream
	// to end there. Without this, anything after a "---" separator (secrets,
	// unknown keys, overriding values) would be silently dropped while still
	// looking like a successfully loaded config.
	var extra yaml.Node
	if err := dec.Decode(&extra); !errors.Is(err, io.EOF) {
		return nil, errors.New("config file must contain exactly one YAML document")
	}
	return r, nil
}

// rejectSecrets scans the YAML mapping for secret-looking keys: at the top
// level and inside the security/auth/metadata sections. Secrets never enter
// the config file (ADR-0009): the admin password is BINFLOW_ADMIN_PASSWORD,
// env-only. Secret-ish spellings inside known sections would also be caught
// by strict decoding; this pass gives them a pointed error message instead
// of a bare "field not found".
func rejectSecrets(m *yaml.Node) error {
	for i := 0; i+1 < len(m.Content); i += 2 {
		keyNode, valNode := m.Content[i], m.Content[i+1]
		if keyNode.Kind != yaml.ScalarNode {
			continue
		}
		section := strings.ToLower(keyNode.Value)
		if isSecretYAMLKey("", section) {
			return secretYAMLErr(keyNode.Value)
		}
		if valNode.Kind != yaml.MappingNode {
			continue
		}
		// Extend the secret scan to storage.s3 as well as the existing sections:
		// secret_access_key must never appear in YAML.
		if section != "security" && section != "auth" && section != "metadata" && section != "storage" {
			continue
		}
		for j := 0; j+1 < len(valNode.Content); j += 2 {
			nested := valNode.Content[j]
			if nested.Kind != yaml.ScalarNode {
				continue
			}
			key := strings.ToLower(nested.Value)
			if isSecretYAMLKey(section, key) {
				return secretYAMLErr(section + "." + nested.Value)
			}
			// Recurse into storage.s3 for secret_access_key, and into
			// auth.oidc / auth.ldap for client_secret / bind_password (the
			// env-only secrets of the M6 identity-provider sections,
			// ADR-0009's blanket rule applies at every nesting depth).
			if section == "storage" && key == "s3" && valNode.Content[j+1].Kind == yaml.MappingNode {
				s3map := valNode.Content[j+1]
				for k := 0; k+1 < len(s3map.Content); k += 2 {
					s3key := s3map.Content[k]
					if s3key.Kind != yaml.ScalarNode {
						continue
					}
					if strings.ToLower(s3key.Value) == "secret_access_key" {
						return secretYAMLErr("storage.s3.secret_access_key")
					}
				}
			}
			if section == "auth" && (key == "oidc" || key == "ldap") &&
				valNode.Content[j+1].Kind == yaml.MappingNode {
				provMap := valNode.Content[j+1]
				for k := 0; k+1 < len(provMap.Content); k += 2 {
					provKey := provMap.Content[k]
					if provKey.Kind != yaml.ScalarNode {
						continue
					}
					if isSecretYAMLKey("auth."+key, strings.ToLower(provKey.Value)) {
						return secretYAMLErr("auth." + key + "." + provKey.Value)
					}
				}
			}
		}
	}
	return nil
}

// secretYAMLErr formats the "secret in YAML" error with the env escape hatch.
// The hint names the env variable that matches the rejected secret — by the
// section it was found in (storage.s3 / auth.oidc / auth.ldap) or by its
// spelling when mis-nested (client_secret is only ever OIDC's,
// bind_password only ever LDAP's, secret_access_key only ever S3's).
// Everything else falls back to the admin-password spelling.
func secretYAMLErr(path string) error {
	hint := SecretEnvVar
	switch {
	case strings.Contains(path, "oidc"), strings.Contains(path, "client_secret"), strings.Contains(path, "clientsecret"):
		hint = OIDCClientSecretEnvVar
	case strings.Contains(path, "ldap"), strings.Contains(path, "bind_password"), strings.Contains(path, "bindpassword"):
		hint = LDAPBindPasswordEnvVar
	case strings.Contains(path, "s3"), strings.Contains(path, "secret_access_key"):
		hint = S3SecretEnvVar
	}
	return fmt.Errorf(
		"config: key %q looks like a secret; secrets must not be written into the YAML file — use the environment variable %s instead",
		path, hint)
}

// build resolves raw YAML plus environment into a Config with defaults
// applied. Env wins over YAML; YAML wins over defaults.
func build(r *raw, env map[string]string) (*Config, error) {
	c := defaults()

	if r.Server != nil {
		if r.Server.Listen != nil {
			c.Server.Listen = *r.Server.Listen
		}
		if r.Server.BaseURL != nil {
			c.Server.BaseURL = *r.Server.BaseURL
		}
		if r.Server.CORSOrigins != nil {
			c.Server.CORSOrigins = r.Server.CORSOrigins
		}
		if r.Server.GracefulTimeoutSeconds != nil {
			c.Server.GracefulTimeout = time.Duration(*r.Server.GracefulTimeoutSeconds) * time.Second
		}
	}
	if r.Storage != nil {
		if r.Storage.DataDir != nil {
			c.Storage.DataDir = *r.Storage.DataDir
		}
		if r.Storage.Backend != nil {
			c.Storage.Backend = *r.Storage.Backend
		}
		if r.Storage.SessionTTLHours != nil {
			c.Storage.SessionTTL = time.Duration(*r.Storage.SessionTTLHours) * time.Hour
		}
		if r.Storage.GCGraceHours != nil {
			c.Storage.GCGrace = time.Duration(*r.Storage.GCGraceHours) * time.Hour
		}
		// [M9] ADR-0031: the hold-TTL backstop. An explicit 0 stays 0 — the
		// sentinel the engine maps onto its default (the same zero-value
		// reading grace's key documents); the [60s, ∞) domain is Validate's.
		if r.Storage.GCHoldTTLSeconds != nil {
			c.Storage.GCHoldTTL = time.Duration(*r.Storage.GCHoldTTLSeconds) * time.Second
		}
		if r.Storage.S3 != nil {
			if r.Storage.S3.Bucket != nil {
				c.Storage.S3.Bucket = *r.Storage.S3.Bucket
			}
			if r.Storage.S3.Region != nil {
				c.Storage.S3.Region = *r.Storage.S3.Region
			}
			if r.Storage.S3.Endpoint != nil {
				c.Storage.S3.Endpoint = *r.Storage.S3.Endpoint
			}
			if r.Storage.S3.AccessKeyID != nil {
				c.Storage.S3.AccessKeyID = *r.Storage.S3.AccessKeyID
			}
			if r.Storage.S3.UsePathStyle != nil {
				c.Storage.S3.UsePathStyle = *r.Storage.S3.UsePathStyle
			}
			if r.Storage.S3.UploadPartSize != nil {
				c.Storage.S3.UploadPartSize = *r.Storage.S3.UploadPartSize
			}
			if r.Storage.S3.UploadConcurrency != nil {
				c.Storage.S3.UploadConcurrency = *r.Storage.S3.UploadConcurrency
			}
			if r.Storage.S3.BucketPrefix != nil {
				c.Storage.S3.BucketPrefix = *r.Storage.S3.BucketPrefix
			}
		}
		if r.Storage.Migration != nil {
			if r.Storage.Migration.Enabled != nil {
				c.Storage.Migration.Enabled = *r.Storage.Migration.Enabled
			}
			if r.Storage.Migration.Completed != nil {
				c.Storage.Migration.Completed = *r.Storage.Migration.Completed
			}
			if r.Storage.Migration.Concurrency != nil {
				c.Storage.Migration.Concurrency = *r.Storage.Migration.Concurrency
			}
		}
	}
	if r.Metadata != nil {
		if r.Metadata.Driver != nil {
			c.Metadata.Driver = *r.Metadata.Driver
		}
		if r.Metadata.DSN != nil {
			c.Metadata.DSN = *r.Metadata.DSN
		}
	}
	if r.Auth != nil {
		if r.Auth.Argon2MemoryMB != nil {
			c.Auth.Argon2MemoryMB = *r.Auth.Argon2MemoryMB
		}
		if r.Auth.TokenDefaultTTLHours != nil {
			c.Auth.TokenDefaultTTL = time.Duration(*r.Auth.TokenDefaultTTLHours) * time.Hour
		}
		if r.Auth.TokenNonAdminMaxTTLSec != nil {
			c.Auth.TokenNonAdminMaxTTL = time.Duration(*r.Auth.TokenNonAdminMaxTTLSec) * time.Second
		}
		if r.Auth.TokenStepUp != nil {
			c.Auth.TokenStepUp = *r.Auth.TokenStepUp
		}
		if r.Auth.TokenStepUpGrantTTLSec != nil {
			c.Auth.TokenStepUpGrantTTL = time.Duration(*r.Auth.TokenStepUpGrantTTLSec) * time.Second
		}
		// Explicit 0 counts as unset: the sentinel keeps the auth service's
		// derived default (GOMAXPROCS clamped), so operators can spell
		// "default" without deleting the line. Validate rejects negatives.
		if r.Auth.HashConcurrency != nil {
			c.Auth.HashConcurrency = *r.Auth.HashConcurrency
		}
		if r.Auth.OIDC != nil {
			o := r.Auth.OIDC
			if o.Enabled != nil {
				c.Auth.OIDC.Enabled = *o.Enabled
			}
			if o.IssuerURL != nil {
				c.Auth.OIDC.IssuerURL = *o.IssuerURL
			}
			if o.ClientID != nil {
				c.Auth.OIDC.ClientID = *o.ClientID
			}
			if o.RedirectURL != nil {
				c.Auth.OIDC.RedirectURL = *o.RedirectURL
			}
			if o.Scopes != nil {
				c.Auth.OIDC.Scopes = o.Scopes
			}
			if o.UserClaim != nil {
				c.Auth.OIDC.UserClaim = *o.UserClaim
			}
			if o.GroupClaim != nil {
				c.Auth.OIDC.GroupClaim = *o.GroupClaim
			}
			if o.AdminGroup != nil {
				c.Auth.OIDC.AdminGroup = *o.AdminGroup
			}
			if o.ReadOnlyGroup != nil {
				c.Auth.OIDC.ReadOnlyGroup = *o.ReadOnlyGroup
			}
		}
		if r.Auth.LDAP != nil {
			l := r.Auth.LDAP
			if l.Enabled != nil {
				c.Auth.LDAP.Enabled = *l.Enabled
			}
			if l.URL != nil {
				c.Auth.LDAP.URL = *l.URL
			}
			if l.BaseDN != nil {
				c.Auth.LDAP.BaseDN = *l.BaseDN
			}
			if l.BindDN != nil {
				c.Auth.LDAP.BindDN = *l.BindDN
			}
			if l.UserFilter != nil {
				c.Auth.LDAP.UserFilter = *l.UserFilter
			}
			if l.UserIDAttr != nil {
				c.Auth.LDAP.UserIDAttr = *l.UserIDAttr
			}
			if l.GroupFilter != nil {
				c.Auth.LDAP.GroupFilter = *l.GroupFilter
			}
			if l.GroupBaseDN != nil {
				c.Auth.LDAP.GroupBaseDN = *l.GroupBaseDN
			}
			if l.GroupNameAttr != nil {
				c.Auth.LDAP.GroupNameAttr = *l.GroupNameAttr
			}
			if l.AdminGroup != nil {
				c.Auth.LDAP.AdminGroup = *l.AdminGroup
			}
			if l.ReadOnlyGroup != nil {
				c.Auth.LDAP.ReadOnlyGroup = *l.ReadOnlyGroup
			}
			if l.PoolSize != nil {
				c.Auth.LDAP.PoolSize = *l.PoolSize
			}
			if l.StartTLS != nil {
				c.Auth.LDAP.StartTLS = *l.StartTLS
			}
			if l.SkipTLSVerify != nil {
				c.Auth.LDAP.SkipTLSVerify = *l.SkipTLSVerify
			}
		}
	}
	if r.Audit != nil && r.Audit.Enabled != nil {
		c.Audit.Enabled = *r.Audit.Enabled
	}
	if r.Logging != nil {
		if r.Logging.Level != nil {
			c.Logging.Level = *r.Logging.Level
		}
		if r.Logging.Format != nil {
			c.Logging.Format = *r.Logging.Format
		}
	}
	if r.Console != nil {
		// Dual-key resolution (PRD v1.1 R4): hours is the primary operator
		// spelling, seconds the override for test/short-session granularity.
		// Unlike the anonymous-access alias pair, a both-set conflict is NOT
		// an error — seconds simply wins, because the keys are two precisions
		// of one value, not two spellings of two features.
		if r.Console.SessionTTLHours != nil {
			c.Console.SessionTTL = time.Duration(*r.Console.SessionTTLHours) * time.Hour
		}
		if r.Console.SessionTTLSeconds != nil {
			c.Console.SessionTTL = time.Duration(*r.Console.SessionTTLSeconds) * time.Second
		}
	}

	if r.Metrics != nil && r.Metrics.RequireAuth != nil {
		c.Metrics.RequireAuth = *r.Metrics.RequireAuth
	}

	if r.Replication != nil && r.Replication.AllowPrivateTarget != nil {
		c.Replication.AllowPrivateTarget = *r.Replication.AllowPrivateTarget
	}

	// M10 T-283 (ADR-0032 / section 15.5): the addons.disabled CSV passes
	// through verbatim — the license Manager owns the parsing, the trimming
	// and the core-id WARN, so the key's semantics have one owner.
	if r.Addons != nil && r.Addons.Disabled != nil {
		c.Addons.Disabled = *r.Addons.Disabled
	}

	// The anonymous toggle has two equivalent keys; resolve them with a
	// conflict check before env overrides apply on top of the merged value.
	anon, err := resolveAnonymous(r, DefaultAnonymousAccess)
	if err != nil {
		return nil, err
	}
	c.Security.AnonymousAccess = anon

	if err := applyEnv(c, env); err != nil {
		return nil, err
	}
	// AdminPassword is assigned inside applyEnv/setEnvValue together with
	// every other env value, so case-insensitive spellings behave uniformly
	// (review B3): reading env[SecretEnvVar] here with exact casing would let
	// binflow_admin_password silently vanish and the deployment fall back to
	// the documented default password.

	if err := c.Validate(); err != nil {
		return nil, err
	}
	return c, nil
}

// defaults returns the fully populated default configuration (architecture
// section 8): listen :8080, data_dir ./data, driver sqlite, anonymous_access
// true, 24h session TTL / GC grace, 30d token TTL, argon2 64MB, audit on,
// info/json logging.
func defaults() *Config {
	return &Config{
		Server: ServerConfig{
			Listen:          DefaultListen,
			BaseURL:         "",
			CORSOrigins:     []string{},
			GracefulTimeout: DefaultGracefulTimeout,
		},
		Storage: StorageConfig{
			DataDir:    DefaultDataDir,
			Backend:    DefaultStorageBackend,
			SessionTTL: DefaultTTL,
			GCGrace:    DefaultTTL,
			// GCHoldTTL stays the 0 sentinel — DefaultGCHoldTTL (600s) is
			// applied by the storage engine, the zero-value semantics
			// storage.gc_hold_ttl_seconds documents ([M9] ADR-0031).
			GCHoldTTL: 0,
			S3: S3Config{
				UploadPartSize:    DefaultS3UploadPartSize,
				UploadConcurrency: DefaultS3UploadConcurrency,
			},
			Migration: MigrationConfig{
				Concurrency: DefaultMigrationConcurrency,
			},
		},
		Metadata: MetadataConfig{Driver: DefaultDriver, DSN: ""},
		Auth: AuthConfig{
			Argon2MemoryMB:      DefaultArgon2MemoryMB,
			TokenDefaultTTL:     DefaultTokenTTL,
			TokenNonAdminMaxTTL: DefaultTokenNonAdminMaxTTL,
			// M7 step-up (ADR-0027): switch defaults OFF — an unconfigured
			// boot keeps the Q11 self-mint posture byte-for-byte; the grant
			// TTL carries its documented default either way (the domain is
			// validated even with the switch off).
			TokenStepUp:         false,
			TokenStepUpGrantTTL: DefaultTokenStepUpGrantTTL,
			// HashConcurrency stays at its sentinel 0: unset means the
			// auth service derives its own limit (GOMAXPROCS clamped to
			// [1,16] — see AuthConfig.HashConcurrency), so an unconfigured
			// boot is byte-for-byte the pre-T-204 behavior.
			HashConcurrency: 0,
			// M6 identity providers default to disabled: an unconfigured boot
			// keeps the pre-M6 local-only posture (zero behavior change).
			OIDC: OIDCConfig{},
			LDAP: LDAPConfig{PoolSize: DefaultLDAPPoolSize},
		},
		Security: SecurityConfig{AnonymousAccess: DefaultAnonymousAccess},
		Audit:    AuditConfig{Enabled: DefaultAuditEnabled},
		Logging:  LoggingConfig{Level: DefaultLogLevel, Format: DefaultLogFormat},
		Console:  ConsoleConfig{SessionTTL: DefaultConsoleSessionTTL},
		// metrics.require_auth defaults to false (ADR-0022: /metrics rides
		// the /healthz-family anonymous posture).
		Metrics: MetricsConfig{},
		// replication.allow_private_target defaults to true (T-210 /
		// ADR-0025 decision 4): private targets stay allowed so existing
		// deployments that replicate over private networks are unchanged.
		Replication: ReplicationConfig{AllowPrivateTarget: DefaultAllowPrivateTarget},
		// addons.disabled defaults to empty (M10 T-283, ADR-0032): nothing
		// is switched off unless the operator spells it.
		Addons: AddonsConfig{},
	}
}

// resolveAnonymous merges security.anonymous_access (primary, PRD spelling)
// with the architecture §8 alias auth.anonymous_read: unset falls back to def,
// a single set key wins, both set to the same value is fine, both set with
// different values is an error naming both keys.
func resolveAnonymous(r *raw, def bool) (bool, error) {
	var primary, alias *bool
	if r.Security != nil {
		primary = r.Security.AnonymousAccess
	}
	if r.Auth != nil {
		alias = r.Auth.AnonymousRead
	}
	switch {
	case primary == nil && alias == nil:
		return def, nil
	case primary != nil && alias == nil:
		return *primary, nil
	case primary == nil && alias != nil:
		return *alias, nil
	case *primary == *alias:
		return *primary, nil
	default:
		return false, errors.New(
			"config: security.anonymous_access and auth.anonymous_read are both set with different values; " +
				"they are equivalent — keep only security.anonymous_access")
	}
}

// environ snapshots the process environment into a map. Later entries win,
// matching os.Getenv semantics for duplicated keys.
func environ() map[string]string {
	out := map[string]string{}
	for _, kv := range os.Environ() {
		name, value, _ := strings.Cut(kv, "=")
		out[name] = value
	}
	return out
}

// applyEnv walks BINFLOW_-prefixed variables in deterministic order, applies
// the known ones, and collects the unknown ones into a single error.
func applyEnv(c *Config, env map[string]string) error {
	names := make([]string, 0, len(env))
	for name := range env {
		names = append(names, name)
	}
	sort.Strings(names)

	var unknown []string
	for _, name := range names {
		upper := strings.ToUpper(name)
		if !strings.HasPrefix(upper, "BINFLOW_") || upper == "BINFLOW_" {
			continue // not ours
		}
		if upper == "BINFLOW_HOME" {
			continue // reserved for cmd (T-16): config/data dir resolution
		}
		if upper == "BINFLOW_REMOTE_CREDENTIALS_KEY" {
			// Reserved for internal/remote (ADR-0012 decision 4 / T-66):
			// the env-only master key of the credential AES-256-GCM chain.
			// Like the admin password it is never a config field — secrets
			// do not live in the YAML tree — so the loader only tolerates
			// the name; internal/remote reads the value itself.
			continue
		}
		path, kind, ok := splitEnvKey(strings.TrimPrefix(upper, "BINFLOW_"))
		if !ok {
			unknown = append(unknown, name)
			continue
		}
		if err := setEnvValue(c, path, kind, env[name], name); err != nil {
			return err
		}
	}
	if len(unknown) > 0 {
		return fmt.Errorf(
			"config: unknown BINFLOW_ environment variable(s) %s; keys map as BINFLOW_<SECTION>__<KEY>, e.g. BINFLOW_METADATA__DRIVER",
			strings.Join(unknown, ", "))
	}
	return nil
}

// setEnvValue writes one parsed env value into the Config tree.
func setEnvValue(c *Config, path []string, kind envKind, value, name string) error {
	where := strings.Join(path, ".")
	switch kind {
	case envSecret:
		// Env-only secret (ADR-0009); every spelling of the variable name
		// funnels through this same case-insensitive path (review B3). An
		// empty value counts as unset. If several case variants of the name
		// coexist in the environment, applyEnv's sorted (C-locale) walk makes
		// the lexicographically-last variant win deterministically.
		if value != "" {
			switch {
			case strings.EqualFold(name, SecretEnvVar):
				c.AdminPassword = value
			case strings.EqualFold(name, S3SecretEnvVar):
				c.Storage.S3.SecretAccessKey = value
			case strings.EqualFold(name, OIDCClientSecretEnvVar):
				c.Auth.OIDC.ClientSecret = value
			case strings.EqualFold(name, LDAPBindPasswordEnvVar):
				c.Auth.LDAP.BindPassword = value
			default:
				return fmt.Errorf("config: internal: unhandled secret env %s", name)
			}
		}
		return nil
	case envBool:
		b, err := parseBool(value)
		if err != nil {
			return fmt.Errorf("config: %s: %w", name, err)
		}
		switch where {
		case "security.anonymous_access", "auth.anonymous_read":
			c.Security.AnonymousAccess = b
		case "audit.enabled":
			c.Audit.Enabled = b
		case "storage.s3.use_path_style":
			c.Storage.S3.UsePathStyle = b
		case "storage.migration.enabled":
			c.Storage.Migration.Enabled = b
		case "storage.migration.completed":
			c.Storage.Migration.Completed = b
		case "metrics.require_auth":
			c.Metrics.RequireAuth = b
		case "auth.token_step_up":
			// M7 (ADR-0027 decision 6): the step-up switch; default false.
			c.Auth.TokenStepUp = b
		case "replication.allow_private_target":
			c.Replication.AllowPrivateTarget = b
		default:
			return fmt.Errorf("config: internal: bool path %q not wired", where)
		}
		return nil
	case envString:
		switch where {
		case "server.listen":
			c.Server.Listen = value
		case "server.base_url":
			c.Server.BaseURL = value
		case "storage.data_dir":
			c.Storage.DataDir = value
		case "storage.backend":
			c.Storage.Backend = value
		case "storage.s3.bucket":
			c.Storage.S3.Bucket = value
		case "storage.s3.region":
			c.Storage.S3.Region = value
		case "storage.s3.endpoint":
			c.Storage.S3.Endpoint = value
		case "storage.s3.access_key_id":
			c.Storage.S3.AccessKeyID = value
		case "storage.s3.bucket_prefix":
			c.Storage.S3.BucketPrefix = value
		case "metadata.driver":
			d := strings.ToLower(value)
			if !allowedDrivers()[d] {
				return fmt.Errorf("config: %s: unknown driver %q (want %q or %q)", name, value, DriverSQLite, DriverPostgres)
			}
			c.Metadata.Driver = d
		case "metadata.dsn":
			c.Metadata.DSN = value
		case "auth.oidc.readonly_group":
			c.Auth.OIDC.ReadOnlyGroup = value
		case "auth.ldap.readonly_group":
			c.Auth.LDAP.ReadOnlyGroup = value
		case "addons.disabled":
			// M10 T-283: the CSV rides through verbatim (the license
			// Manager owns its parsing).
			c.Addons.Disabled = value
		case "logging.level":
			l := strings.ToLower(value)
			if !allowedLogLevels()[l] {
				return fmt.Errorf("config: %s: unknown level %q (want debug, info, warn or error)", name, value)
			}
			c.Logging.Level = l
		case "logging.format":
			f := strings.ToLower(value)
			if !allowedLogFormats()[f] {
				return fmt.Errorf("config: %s: unknown format %q (want json or console)", name, value)
			}
			c.Logging.Format = f
		default:
			return fmt.Errorf("config: internal: string path %q not wired", where)
		}
		return nil
	case envIntPos:
		n, err := strconv.Atoi(value)
		if err != nil {
			return fmt.Errorf("config: %s: %w", name, err)
		}
		if n <= 0 {
			return fmt.Errorf("config: %s: must be a positive integer, got %q", name, value)
		}
		switch where {
		case "server.graceful_timeout_seconds":
			c.Server.GracefulTimeout = time.Duration(n) * time.Second
		case "storage.session_ttl_hours":
			c.Storage.SessionTTL = time.Duration(n) * time.Hour
		case "storage.gc_grace_hours":
			c.Storage.GCGrace = time.Duration(n) * time.Hour
		case "storage.gc_hold_ttl_seconds":
			// [M9] ADR-0031: positive int seconds; the domain check is
			// Validate's (floor 60s refuses the boot).
			c.Storage.GCHoldTTL = time.Duration(n) * time.Second
		case "storage.s3.upload_part_size":
			c.Storage.S3.UploadPartSize = int64(n)
		case "storage.s3.upload_concurrency":
			c.Storage.S3.UploadConcurrency = n
		case "storage.migration.concurrency":
			c.Storage.Migration.Concurrency = n
		case "auth.argon2_memory_mb":
			c.Auth.Argon2MemoryMB = n
		case "auth.hash_concurrency":
			c.Auth.HashConcurrency = n
		case "auth.token_default_ttl_hours":
			c.Auth.TokenDefaultTTL = time.Duration(n) * time.Hour
		case "auth.token_nonadmin_max_ttl":
			c.Auth.TokenNonAdminMaxTTL = time.Duration(n) * time.Second
		case "auth.token_step_up_grant_ttl_seconds":
			// The [60, 3600] domain is Validate's (refusing the boot on an
			// out-of-domain override, whatever wrote the value here).
			c.Auth.TokenStepUpGrantTTL = time.Duration(n) * time.Second
		case "console.session_ttl_hours":
			c.Console.SessionTTL = time.Duration(n) * time.Hour
		case "console.session_ttl_seconds":
			c.Console.SessionTTL = time.Duration(n) * time.Second
		default:
			return fmt.Errorf("config: internal: int path %q not wired", where)
		}
		return nil
	default:
		return fmt.Errorf("config: internal: unhandled env kind for %s", name)
	}
}

// parseBool accepts the usual Go booleans plus the YAML/shell spellings
// yes/no/on/off; an empty value counts as false (BINFLOW_X= is BINFLOW_X=0).
func parseBool(value string) (bool, error) {
	switch strings.ToLower(strings.TrimSpace(value)) {
	case "1", "t", "true", "y", "yes", "on":
		return true, nil
	case "0", "f", "false", "n", "no", "off", "":
		return false, nil
	}
	return false, fmt.Errorf("invalid boolean %q", value)
}

// ensureDataDir fails fast when the data directory cannot be created or is
// not writable: refusing boot is better than 500-ing on every upload later.
// The SQLite path lands in the same directory (T-10), so one probe covers it.
//
// The probe is best-effort, not a guarantee: permissions or mount state can
// change between this check and actual use (TOCTOU), and the fixed probe
// filename is predictable — treat this as boot-time fail-fast hygiene, never
// as a security boundary. Concurrent Load calls are harmless: the probe is
// create-then-remove, so racing writers at worst observe a stale-file error
// for a file that is about to disappear.
func ensureDataDir(dir string) error {
	if err := os.MkdirAll(dir, 0o750); err != nil {
		return fmt.Errorf("create data_dir %s: %w", dir, err)
	}
	probe := filepath.Join(dir, ".config-write-probe")
	if err := os.WriteFile(probe, nil, 0o600); err != nil {
		return fmt.Errorf("data_dir %s not writable: %w", dir, err)
	}
	if err := os.Remove(probe); err != nil {
		return fmt.Errorf("data_dir %s: remove write probe: %w", dir, err)
	}
	return nil
}

// validatePort accepts ":8080", "0.0.0.0:8080", "localhost:8080" (and the
// ephemeral ":0" used by tests); it rejects scheme prefixes, missing ports,
// and non-numeric or out-of-range ports.
func validatePort(listen string) error {
	if strings.Contains(listen, "://") {
		return fmt.Errorf("listen %q must not contain a scheme", listen)
	}
	_, portStr, err := net.SplitHostPort(listen)
	if err != nil {
		return fmt.Errorf("listen %q: %w", listen, err)
	}
	port, err := strconv.Atoi(portStr)
	if err != nil {
		return fmt.Errorf("listen %q: non-numeric port %q", listen, portStr)
	}
	if port < 0 || port > 65535 {
		return fmt.Errorf("listen %q: port %d out of range", listen, port)
	}
	return nil
}
