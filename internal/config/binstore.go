package config

// binstore.yaml — the standalone storage-chain configuration file
// (T-306, FR-93 / ADR-0036). The blob provider chain is decoupled from the
// main configuration the way Artifactory's binarystore.xml is: one file next
// to the effective binflow.yaml declares the ordered provider chain
// ([filestore], [s3], or the [filestore, s3] migration chain) and, for the
// migration chain, the chain-level mode spelling of M6's three migration
// modes (bypass | dual-write | completed).
//
// Behavior anchors: docs/reverse/config-formats.md §1 (file location,
// chain-of-providers expression, provider configurable points — the YAML
// carrier itself is BinFlow's own, PRD 93.1's clean-room boundary). Two
// in-ticket decisions from that review are recorded here because the
// Artifactory side could not be located (its chain assembler lives in a
// JFrog shared library outside the decompilation scope):
//
//   - Decision (config-formats §1 待验证 binarystore-1): the ERROR SHAPE for
//     bad files, unknown types and illegal combinations is BinFlow's own
//     spelling — absolute file path + YAML line + offending key, per
//     ADR-0036 decision 7. Nothing above is copied from a form we never saw.
//   - Decision (config-formats §1 待验证 binarystore-2): BinFlow has no
//     template layer, so the self-describing chain label (ADR-0036 decision
//     7's INFO line, StorageChain) is the DECLARED provider list joined in
//     order ("filestore,s3"), never a synthesized template name. Where the
//     chain comes from the embedded section instead, the label is the
//     assembled chain (what the boot actually runs).
//
// Coexistence (ADR-0036 decision 5, the Q5 three branches), judged on the
// "chain keys" only — the embedded storage.backend/storage.s3/storage.migration
// groups, i.e. everything binstore.yaml can express. data_dir, session_ttl
// and the gc_* keys always stay with binflow.yaml and never participate:
//
//	① no binstore.yaml + chain keys set (YAML or env)  → boot as-is + WARN
//	② binstore.yaml + embedded chain keys absent      → file wins, silent
//	③ binstore.yaml + embedded keys, equivalent after → file wins + WARN
//	   normalization → semantic DIVERGENCE (the two sources would assemble
//	   different chains) → refuse to start, naming both files and keys.
//
// Credential discipline (decision 4): no plaintext secret key is accepted
// anywhere in the file (recursive scan, line-pointed refusal naming the env
// escape hatch); BINFLOW_STORAGE_S3_SECRET_ACCESS_KEY always reaches the
// live S3 provider even when every other chain-scoped env override is
// ignored (decision 6).

import (
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"runtime"
	"sort"
	"strconv"
	"strings"

	yaml "go.yaml.in/yaml/v3"
)

// BinstoreFileName is the standalone storage-chain file, looked up next to
// the effective main configuration file (ADR-0036 decision 1: no new flag,
// no env knob — "binstore.yaml sits next to binflow.yaml").
const BinstoreFileName = "binstore.yaml"

// The provider type closed set and the migration mode closed set
// (ADR-0036 decision 2).
const (
	ProviderFilestore = "filestore"
	ProviderS3        = "s3"

	MigrationModeBypass    = "bypass"
	MigrationModeDualWrite = "dual-write"
	MigrationModeCompleted = "completed"
)

// ChainSourceEmbedded marks a chain resolved from binflow.yaml's embedded
// storage section (the compatibility-window spelling, branch ①).
const ChainSourceEmbedded = "embedded"

// reservedBinstoreProviders are provider names held as extension slots
// (ADR-0036 decision 2): appearing in a chain refuses the boot with an
// honest "reserved, not implemented" message — never a silent ignore and
// never a pretense of support.
var reservedBinstoreProviders = map[string]bool{
	"cache-fs": true,
	"azure":    true,
	"gs":       true,
}

// binstoreChain is the parsed, fully validated content of one binstore.yaml.
type binstoreChain struct {
	providers    []string // declared order
	s3           *rawS3Params
	mode         string // "" outside the dual chain
	concurrency  int    // declared migration concurrency; 0 = default
	hasMigration bool
}

func (b *binstoreChain) hasFilestore() bool {
	return slicesContains(b.providers, ProviderFilestore)
}

func (b *binstoreChain) hasS3() bool { return slicesContains(b.providers, ProviderS3) }

// rawS3Params mirrors the s3 provider's parameter block: every scalar is a
// pointer so "absent" falls back to the default the embedded schema uses.
type rawS3Params struct {
	bucket            *string
	region            *string
	endpoint          *string
	accessKeyID       *string
	bucketPrefix      *string
	usePathStyle      *bool
	uploadPartSize    *int64
	uploadConcurrency *int
}

// s3Config resolves the declared parameters onto the defaults (the same
// defaulting the embedded storage.s3 block gets). SecretAccessKey is
// deliberately not touched here: it is env-only.
func (p *rawS3Params) s3Config() S3Config {
	cfg := S3Config{
		UploadPartSize:    DefaultS3UploadPartSize,
		UploadConcurrency: DefaultS3UploadConcurrency,
	}
	if p == nil {
		return cfg
	}
	if p.bucket != nil {
		cfg.Bucket = *p.bucket
	}
	if p.region != nil {
		cfg.Region = *p.region
	}
	if p.endpoint != nil {
		cfg.Endpoint = *p.endpoint
	}
	if p.accessKeyID != nil {
		cfg.AccessKeyID = *p.accessKeyID
	}
	if p.bucketPrefix != nil {
		cfg.BucketPrefix = *p.bucketPrefix
	}
	if p.usePathStyle != nil {
		cfg.UsePathStyle = *p.usePathStyle
	}
	if p.uploadPartSize != nil {
		cfg.UploadPartSize = *p.uploadPartSize
	}
	if p.uploadConcurrency != nil {
		cfg.UploadConcurrency = *p.uploadConcurrency
	}
	return cfg
}

// parseBinstore decodes and validates one binstore.yaml. Every failure is a
// boot-refusing error carrying the file's absolute path and, where the YAML
// tree knows it, the offending line (ADR-0036 decision 7 — BinFlow's own
// error spelling; Artifactory's was never located, config-formats §1 C7).
func parseBinstore(src []byte, path string) (*binstoreChain, error) {
	fail := func(line int, format string, args ...any) error {
		return fmt.Errorf("config: binstore.yaml %s: line %d: %s", path, line, fmt.Sprintf(format, args...))
	}

	var root yaml.Node
	if err := yaml.Unmarshal(src, &root); err != nil {
		// yaml syntax errors already carry their own "line N:" prefix.
		return nil, fmt.Errorf("config: binstore.yaml %s: %w", path, err)
	}
	if len(root.Content) == 0 {
		return nil, fail(1, "file is empty — a binstore.yaml must declare a provider chain (e.g. \"chain:\\n  - type: filestore\")")
	}
	doc := root.Content[0]
	if doc.Tag == "!!null" {
		return nil, fail(doc.Line, "file is empty — a binstore.yaml must declare a provider chain")
	}
	if doc.Kind != yaml.MappingNode {
		return nil, fail(doc.Line, "document must be a YAML mapping")
	}
	if err := rejectBinstoreSecrets(doc, path); err != nil {
		return nil, err
	}

	b := &binstoreChain{}
	seen := map[string]bool{}
	var chainNode, migrationNode *yaml.Node
	for i := 0; i+1 < len(doc.Content); i += 2 {
		k, v := doc.Content[i], doc.Content[i+1]
		if k.Kind != yaml.ScalarNode {
			return nil, fail(k.Line, "mapping key must be a scalar")
		}
		if seen[k.Value] {
			return nil, fail(k.Line, "duplicate key %q", k.Value)
		}
		seen[k.Value] = true
		switch k.Value {
		case "version":
			if v.Tag != "!!int" || v.Value != "1" {
				return nil, fail(v.Line, "version must be 1, got %q", v.Value)
			}
		case "chain":
			chainNode = v
		case "migration":
			migrationNode = v
		default:
			return nil, fail(k.Line, "unknown key %q (binstore.yaml accepts only version, chain and migration)", k.Value)
		}
	}
	if chainNode == nil {
		return nil, fail(doc.Line, "chain is required — the ordered provider chain is the file's whole purpose")
	}
	if chainNode.Kind != yaml.SequenceNode {
		return nil, fail(chainNode.Line, "chain must be a YAML list of provider mappings")
	}
	if len(chainNode.Content) == 0 {
		return nil, fail(chainNode.Line, "chain must declare at least one provider")
	}

	providerLines := map[string]int{}
	for _, item := range chainNode.Content {
		if item.Kind != yaml.MappingNode {
			return nil, fail(item.Line, "each chain entry must be a YAML mapping with a type key")
		}
		var ptype string
		var typeLine int
		itemSeen := map[string]bool{}
		var paramPairs []*yaml.Node // flat k,v,k,v,… of the non-type keys
		for i := 0; i+1 < len(item.Content); i += 2 {
			k, v := item.Content[i], item.Content[i+1]
			if k.Kind != yaml.ScalarNode {
				return nil, fail(k.Line, "provider key must be a scalar")
			}
			if itemSeen[k.Value] {
				return nil, fail(k.Line, "duplicate key %q in the %s provider", k.Value, providerLabel(ptype))
			}
			itemSeen[k.Value] = true
			if k.Value == "type" {
				if v.Kind != yaml.ScalarNode || v.Tag == "!!null" {
					return nil, fail(v.Line, "provider type must be a scalar string")
				}
				ptype, typeLine = v.Value, v.Line
				continue
			}
			paramPairs = append(paramPairs, k, v)
		}
		if ptype == "" {
			return nil, fail(item.Line, "chain entry is missing its type key")
		}
		if reservedBinstoreProviders[ptype] {
			return nil, fail(typeLine,
				"provider type %q is a reserved slot this BinFlow build does not implement — refusing to start rather than pretending to support it; remove it from the chain (reserved slots: cache-fs, azure, gs)",
				ptype)
		}
		if ptype != ProviderFilestore && ptype != ProviderS3 {
			return nil, fail(typeLine,
				"unknown provider type %q (closed set: filestore, s3; reserved for a future build: cache-fs, azure, gs)", ptype)
		}
		if _, dup := providerLines[ptype]; dup {
			return nil, fail(typeLine, "provider %q appears twice in the chain — each provider type is allowed at most once", ptype)
		}
		providerLines[ptype] = typeLine
		b.providers = append(b.providers, ptype)

		if ptype == ProviderS3 {
			params, err := parseBinstoreS3Params(paramPairs, fail)
			if err != nil {
				return nil, err
			}
			b.s3 = params
			continue
		}
		// filestore takes no parameters in this build; dir is a declared
		// extension slot (ADR-0036 decision 2) and gets its own message.
		for i := 0; i+1 < len(paramPairs); i += 2 {
			k := paramPairs[i]
			if k.Value == "dir" {
				return nil, fail(k.Line, "filestore takes no parameters in this build; dir is a reserved extension slot, not implemented")
			}
			return nil, fail(k.Line, "unknown filestore key %q (the filestore provider takes no parameters in this build)", k.Value)
		}
	}

	if migrationNode != nil {
		if migrationNode.Kind != yaml.MappingNode {
			return nil, fail(migrationNode.Line, "migration must be a YAML mapping (mode, concurrency)")
		}
		mSeen := map[string]bool{}
		for i := 0; i+1 < len(migrationNode.Content); i += 2 {
			k, v := migrationNode.Content[i], migrationNode.Content[i+1]
			if k.Kind != yaml.ScalarNode {
				return nil, fail(k.Line, "migration key must be a scalar")
			}
			if mSeen[k.Value] {
				return nil, fail(k.Line, "duplicate key %q in the migration block", k.Value)
			}
			mSeen[k.Value] = true
			switch k.Value {
			case "mode":
				if v.Kind != yaml.ScalarNode || v.Tag == "!!null" {
					return nil, fail(v.Line, "migration.mode must be a string (bypass | dual-write | completed)")
				}
				switch v.Value {
				case MigrationModeBypass, MigrationModeDualWrite, MigrationModeCompleted:
					b.mode = v.Value
				default:
					return nil, fail(v.Line,
						"migration.mode %q is not one of bypass, dual-write, completed (the M6 three-mode spelling, ADR-0036 decision 2)", v.Value)
				}
			case "concurrency":
				if v.Tag != "!!int" {
					return nil, fail(v.Line, "migration.concurrency must be a positive integer, got %q", v.Value)
				}
				n, err := strconv.Atoi(v.Value)
				if err != nil || n <= 0 {
					return nil, fail(v.Line, "migration.concurrency must be a positive integer, got %q", v.Value)
				}
				b.concurrency = n
			default:
				return nil, fail(k.Line, "unknown migration key %q (accepted: mode, concurrency)", k.Value)
			}
		}
		b.hasMigration = true
		if b.mode == "" {
			return nil, fail(migrationNode.Line, "migration.mode is required (bypass | dual-write | completed)")
		}
	}

	// Chain-shape gate (ADR-0036 decision 2): M11's legal chains are
	// [filestore], [s3] and [filestore, s3]; the migration block is legal
	// on — and required by — the dual chain only. A filestore+s3 pair
	// without migration intent would be the cache-fs shape, which this
	// build does not implement, so that form does not exist either.
	dual := b.hasFilestore() && b.hasS3()
	if !dual && b.hasMigration {
		return nil, fail(migrationNode.Line,
			"the migration block is only legal on the [filestore, s3] chain (this chain is [%s]) — filestore+s3 without migration intent is the cache-fs shape, which this build does not implement",
			strings.Join(b.providers, ", "))
	}
	if dual && !b.hasMigration {
		return nil, fail(chainNode.Line,
			"the [filestore, s3] chain requires a migration block (mode: bypass | dual-write | completed)")
	}
	if dual && b.providers[0] != ProviderFilestore {
		return nil, fail(chainNode.Line,
			"illegal chain order [%s]: legal chains are [filestore], [s3] and [filestore, s3] (the dual-write migration chain)",
			strings.Join(b.providers, ", "))
	}
	return b, nil
}

func providerLabel(ptype string) string {
	if ptype == "" {
		return "chain entry"
	}
	return ptype
}

// parseBinstoreS3Params validates the s3 provider's parameter block. The key
// family is the embedded storage.s3 schema verbatim (bucket, region,
// endpoint, access_key_id, use_path_style, upload_part_size,
// upload_concurrency, bucket_prefix — ADR-0036 decision 2); secret_access_key
// never reaches here (the recursive secret scan refuses it first).
func parseBinstoreS3Params(pairs []*yaml.Node, fail func(int, string, ...any) error) (*rawS3Params, error) {
	p := &rawS3Params{}
	for i := 0; i+1 < len(pairs); i += 2 {
		k, v := pairs[i], pairs[i+1]
		switch k.Value {
		case "bucket", "region", "endpoint", "access_key_id", "bucket_prefix":
			if v.Tag != "!!str" {
				return nil, fail(v.Line, "s3 provider key %q must be a string, got %s", k.Value, v.Tag)
			}
			s := v.Value
			switch k.Value {
			case "bucket":
				p.bucket = &s
			case "region":
				p.region = &s
			case "endpoint":
				p.endpoint = &s
			case "access_key_id":
				p.accessKeyID = &s
			case "bucket_prefix":
				p.bucketPrefix = &s
			}
		case "use_path_style":
			if v.Tag != "!!bool" {
				return nil, fail(v.Line, "s3 provider key %q must be a boolean, got %s", k.Value, v.Tag)
			}
			bv, err := strconv.ParseBool(v.Value)
			if err != nil {
				return nil, fail(v.Line, "s3 provider key %q must be a boolean, got %q", k.Value, v.Value)
			}
			p.usePathStyle = &bv
		case "upload_part_size", "upload_concurrency":
			if v.Tag != "!!int" {
				return nil, fail(v.Line, "s3 provider key %q must be a positive integer, got %s", k.Value, v.Tag)
			}
			n, err := strconv.ParseInt(v.Value, 10, 64)
			if err != nil || n <= 0 {
				return nil, fail(v.Line, "s3 provider key %q must be a positive integer, got %q", k.Value, v.Value)
			}
			if k.Value == "upload_part_size" {
				sz := n
				p.uploadPartSize = &sz
			} else {
				c := int(n)
				p.uploadConcurrency = &c
			}
		default:
			return nil, fail(k.Line,
				"unknown s3 provider key %q (accepted: bucket, region, endpoint, access_key_id, use_path_style, upload_part_size, upload_concurrency, bucket_prefix)",
				k.Value)
		}
	}
	return p, nil
}

// rejectBinstoreSecrets scans the whole document recursively for
// secret-looking keys (ADR-0036 decision 4: the file accepts NO plaintext
// secret, at any depth — including the s3 provider subtree). The refusal
// names the file, the line and the env escape hatch.
func rejectBinstoreSecrets(n *yaml.Node, path string) error {
	return walkBinstoreSecrets(n, "", path)
}

func walkBinstoreSecrets(n *yaml.Node, prefix, path string) error {
	switch n.Kind {
	case yaml.MappingNode:
		for i := 0; i+1 < len(n.Content); i += 2 {
			k, v := n.Content[i], n.Content[i+1]
			childPrefix := prefix
			if k.Kind == yaml.ScalarNode {
				lower := strings.ToLower(k.Value)
				dotted := k.Value
				if prefix != "" {
					dotted = prefix + "." + k.Value
				}
				if isSecretYAMLKey(sectionOf(prefix), lower) || lower == "secret_access_key" {
					return fmt.Errorf(
						"config: binstore.yaml %s: line %d: key %q looks like a secret; secrets must not be written into binstore.yaml — use the environment variable %s instead",
						path, k.Line, dotted, secretEnvHint(dotted))
				}
				childPrefix = dotted
			}
			if err := walkBinstoreSecrets(v, childPrefix, path); err != nil {
				return err
			}
		}
	case yaml.SequenceNode:
		for _, item := range n.Content {
			if err := walkBinstoreSecrets(item, prefix, path); err != nil {
				return err
			}
		}
	}
	return nil
}

// sectionOf lowers a dotted prefix to its first section the way
// isSecretYAMLKey expects ("" for the document root).
func sectionOf(prefix string) string {
	if prefix == "" {
		return ""
	}
	section, _, _ := strings.Cut(prefix, ".")
	return strings.ToLower(section)
}

// ---------------------------------------------------------------------------
// discovery + coexistence resolution
// ---------------------------------------------------------------------------

// binstoreOverlay couples a parsed chain with its provenance.
type binstoreOverlay struct {
	chain *binstoreChain
	path  string // absolute
}

// loadBinstoreAt parses the binstore.yaml at path. A missing file is NOT an
// error: it returns (nil, nil) — absence is the zero-behavior-change branch
// (ADR-0036 decision 1). Anything else about the file (unreadable, bad
// YAML, reserved type, illegal shape, plaintext secret) is a boot refusal.
func loadBinstoreAt(path string) (*binstoreOverlay, error) {
	//nolint:gosec // G304: reading the operator-provided binstore.yaml path is the feature — same ruling as config/load.go's ReadFile (the .golangci.yml path exclusion predates this file).
	src, err := os.ReadFile(path)
	if err != nil {
		if errors.Is(err, os.ErrNotExist) {
			return nil, nil
		}
		return nil, fmt.Errorf("config: reading binstore.yaml %s: %w", path, err)
	}
	abs := path
	if resolved, err := filepath.Abs(path); err == nil {
		abs = resolved
	}
	chain, err := parseBinstore(src, abs)
	if err != nil {
		return nil, err
	}
	return &binstoreOverlay{chain: chain, path: abs}, nil
}

// loadBinstoreNextTo resolves dir(mainPath)/binstore.yaml — the one lookup
// position (ADR-0036 decision 1: the file sits next to the EFFECTIVE main
// config, following serve's -c resolution; no search path, no fallbacks —
// mirroring binarystore.xml's fixed single location, config-formats L1).
func loadBinstoreNextTo(mainPath string) (*binstoreOverlay, error) {
	return loadBinstoreAt(filepath.Join(filepath.Dir(mainPath), BinstoreFileName))
}

// apply merges the chain onto a resolved Config, replacing the embedded
// chain keys wholesale (the file owns the chain — ADR-0036 decision 5's
// "file wins"), and records the chain's self-describing label. The
// env-resolved secret is carried over untouched: the secret env var always
// reaches the live S3 provider (decision 4/6) even though every other
// chain-scoped env key was skipped.
func (o *binstoreOverlay) apply(c *Config) {
	secret := c.Storage.S3.SecretAccessKey
	mig := MigrationConfig{Concurrency: DefaultMigrationConcurrency}
	if o.chain.concurrency > 0 {
		mig.Concurrency = o.chain.concurrency
	}

	s3 := o.chain.s3.s3Config()
	switch {
	case o.chain.hasFilestore() && o.chain.hasS3():
		switch o.chain.mode {
		case MigrationModeBypass:
			// Only the filestore leg assembles; the S3 parameters ride
			// along as a pre-declared slot (ADR-0036 decision 3).
			c.Storage.Backend = StorageBackendDisk
		case MigrationModeDualWrite:
			c.Storage.Backend = StorageBackendS3
			mig.Enabled = true
		case MigrationModeCompleted:
			c.Storage.Backend = StorageBackendS3
			mig.Enabled = true
			mig.Completed = true
		}
	case o.chain.hasS3():
		c.Storage.Backend = StorageBackendS3
	default:
		c.Storage.Backend = StorageBackendDisk
		s3 = S3Config{
			UploadPartSize:    DefaultS3UploadPartSize,
			UploadConcurrency: DefaultS3UploadConcurrency,
		}
	}
	s3.SecretAccessKey = secret
	c.Storage.S3 = s3
	c.Storage.Migration = mig
	c.Storage.Chain = StorageChain{
		Providers: append([]string(nil), o.chain.providers...),
		Mode:      o.chain.mode,
		Source:    o.path,
	}
	if w := binstorePermWarn(o.path); w != "" {
		c.StartupWarnings = append(c.StartupWarnings, w)
	}
}

// binstorePermWarn flags permissions wider than 0600 (ADR-0036 decision 4:
// WARN, not a refusal — the file carries no secrets by policy; 0600 is the
// deployment-document recommendation). Skipped on Windows, where the POSIX
// mode bits are an approximation.
func binstorePermWarn(path string) string {
	if runtime.GOOS == "windows" {
		return ""
	}
	info, err := os.Stat(path)
	if err != nil {
		return ""
	}
	if perm := info.Mode().Perm(); perm&^0o600 != 0 {
		return fmt.Sprintf(
			"config: binstore.yaml %s: file permissions are wider than 0600 (%#o); tighten to 0600 (hygiene — the file must carry no secrets by policy)",
			path, perm)
	}
	return ""
}

// chainView is the normalized, comparable form of one chain declaration:
// what the boot would assemble, per ADR-0036 decision 3's three-chain
// mapping. Equivalence is judged on this view (semantic comparison, never
// text comparison).
type chainView struct {
	dualWrite   bool
	s3Live      bool // the assembled chain runs the S3 engine
	s3          S3Config
	concurrency int
}

// embeddedChainView normalizes the embedded section. migration.enabled is
// the dual-write intent itself (decision 3 maps it unconditionally), so
// enabled && !completed normalizes to the live dual-write chain regardless
// of the backend spelling — including the backend=disk spelling that the
// pre-binstore engine refuses; against an equivalent binstore.yaml that
// pair counts as equivalent (the file fixes the boot), against anything
// else it diverges and refuses.
func embeddedChainView(r *raw) chainView {
	v := chainView{
		s3:          S3Config{UploadPartSize: DefaultS3UploadPartSize, UploadConcurrency: DefaultS3UploadConcurrency},
		concurrency: DefaultMigrationConcurrency,
	}
	if r.Storage == nil {
		return v
	}
	backend := DefaultStorageBackend
	if r.Storage.Backend != nil {
		backend = *r.Storage.Backend
	}
	if r.Storage.S3 != nil {
		if r.Storage.S3.Bucket != nil {
			v.s3.Bucket = *r.Storage.S3.Bucket
		}
		if r.Storage.S3.Region != nil {
			v.s3.Region = *r.Storage.S3.Region
		}
		if r.Storage.S3.Endpoint != nil {
			v.s3.Endpoint = *r.Storage.S3.Endpoint
		}
		if r.Storage.S3.AccessKeyID != nil {
			v.s3.AccessKeyID = *r.Storage.S3.AccessKeyID
		}
		if r.Storage.S3.BucketPrefix != nil {
			v.s3.BucketPrefix = *r.Storage.S3.BucketPrefix
		}
		if r.Storage.S3.UsePathStyle != nil {
			v.s3.UsePathStyle = *r.Storage.S3.UsePathStyle
		}
		if r.Storage.S3.UploadPartSize != nil {
			v.s3.UploadPartSize = *r.Storage.S3.UploadPartSize
		}
		if r.Storage.S3.UploadConcurrency != nil {
			v.s3.UploadConcurrency = *r.Storage.S3.UploadConcurrency
		}
	}
	var enabled, completed bool
	if r.Storage.Migration != nil {
		if r.Storage.Migration.Enabled != nil {
			enabled = *r.Storage.Migration.Enabled
		}
		if r.Storage.Migration.Completed != nil {
			completed = *r.Storage.Migration.Completed
		}
		if r.Storage.Migration.Concurrency != nil {
			v.concurrency = *r.Storage.Migration.Concurrency
		}
	}
	if enabled && !completed {
		v.dualWrite = true
		v.s3Live = true
	} else {
		v.s3Live = backend == StorageBackendS3
	}
	return v
}

// view normalizes the binstore chain. bypass keeps the S3 parameters as a
// pre-declared slot without assembling the S3 leg, so it compares equal to
// a plain [filestore] embedded section (same assembled chain); completed
// compares equal to a plain backend=s3 section — the terminal equivalence
// ADR-0036 decision 3 spells out ("[s3] ≡ backend=s3, including the
// post-migration terminal shape").
func (b *binstoreChain) view() chainView {
	v := chainView{
		s3:          b.s3.s3Config(),
		concurrency: DefaultMigrationConcurrency,
	}
	if b.concurrency > 0 {
		v.concurrency = b.concurrency
	}
	switch {
	case b.hasFilestore() && b.hasS3():
		switch b.mode {
		case MigrationModeDualWrite:
			v.s3Live = true
			v.dualWrite = true
		case MigrationModeCompleted:
			v.s3Live = true
		default: // bypass: filestore-only assembly
		}
	case b.hasS3():
		v.s3Live = true
	}
	return v
}

// diffChains lists the semantic divergences between two views — empty means
// equivalent. Only keys that change the assembled chain are compared: S3
// parameters matter when S3 is live on both sides, migration concurrency
// when both sides dual-write. The secret is never compared (env-only, one
// spelling).
func diffChains(a, b chainView) []string {
	var diffs []string
	if a.dualWrite != b.dualWrite {
		diffs = append(diffs, "the dual-write migration mode is armed on one side only")
	}
	if a.s3Live != b.s3Live {
		diffs = append(diffs, "the S3 provider is live on one side only")
	}
	if a.s3Live && b.s3Live {
		for _, d := range []struct {
			key     string
			a, bval any
		}{
			{"storage.s3.bucket", a.s3.Bucket, b.s3.Bucket},
			{"storage.s3.region", a.s3.Region, b.s3.Region},
			{"storage.s3.endpoint", a.s3.Endpoint, b.s3.Endpoint},
			{"storage.s3.access_key_id", a.s3.AccessKeyID, b.s3.AccessKeyID},
			{"storage.s3.bucket_prefix", a.s3.BucketPrefix, b.s3.BucketPrefix},
			{"storage.s3.use_path_style", a.s3.UsePathStyle, b.s3.UsePathStyle},
			{"storage.s3.upload_part_size", a.s3.UploadPartSize, b.s3.UploadPartSize},
			{"storage.s3.upload_concurrency", a.s3.UploadConcurrency, b.s3.UploadConcurrency},
		} {
			if d.a != d.bval {
				diffs = append(diffs, fmt.Sprintf("%s (%v vs %v)", d.key, d.a, d.bval))
			}
		}
	}
	if a.dualWrite && b.dualWrite && a.concurrency != b.concurrency {
		diffs = append(diffs, fmt.Sprintf("storage.migration.concurrency (%d vs %d)", a.concurrency, b.concurrency))
	}
	return diffs
}

// embeddedChainDeclared reports whether binflow.yaml declares any chain key
// at all (storage.backend / storage.s3 / storage.migration). Absent means
// branch ②'s clean form.
func embeddedChainDeclared(r *raw) bool {
	if r.Storage == nil {
		return false
	}
	return r.Storage.Backend != nil || r.Storage.S3 != nil || r.Storage.Migration != nil
}

// chainEnvNames lists the chain-scoped BINFLOW_ env variables that are set
// (decision 6's ignore set, and branch ①'s "chain keys set" detector for
// env-only spellings). The secret variables are deliberately absent: the
// secret env var is always effective and never ignored.
func chainEnvNames(env map[string]string) []string {
	var names []string
	for name := range env {
		upper := strings.ToUpper(name)
		if !strings.HasPrefix(upper, "BINFLOW_") || upper == "BINFLOW_" {
			continue
		}
		if upper == "BINFLOW_HOME" || upper == "BINFLOW_REMOTE_CREDENTIALS_KEY" {
			continue
		}
		path, kind, ok := splitEnvKey(strings.TrimPrefix(upper, "BINFLOW_"))
		if ok && kind != envSecret && isChainEnvPath(strings.Join(path, ".")) {
			names = append(names, name)
		}
	}
	sort.Strings(names)
	return names
}

// isChainEnvPath reports whether an env-mapped config path belongs to the
// chain-key group (everything binstore.yaml can express). data_dir,
// session_ttl and the gc_* keys are binflow.yaml's forever and never count.
func isChainEnvPath(where string) bool {
	switch where {
	case "storage.backend",
		"storage.s3.bucket", "storage.s3.region", "storage.s3.endpoint",
		"storage.s3.access_key_id", "storage.s3.bucket_prefix",
		"storage.s3.use_path_style", "storage.s3.upload_part_size",
		"storage.s3.upload_concurrency",
		"storage.migration.enabled", "storage.migration.completed",
		"storage.migration.concurrency":
		return true
	}
	return false
}

// fillChainFromConfig derives the self-describing chain label for a chain
// that came from the embedded section (or from pure defaults): the ASSEMBLED
// chain — what the boot actually runs — plus the migration mode when one is
// armed. (For binstore-declared chains the DECLARED provider order is kept
// instead; see the binstore-2 decision at the top of this file.)
func fillChainFromConfig(c *Config) {
	ch := StorageChain{Source: ChainSourceEmbedded}
	switch {
	case c.Storage.Migration.Enabled && !c.Storage.Migration.Completed:
		ch.Providers = []string{ProviderFilestore, ProviderS3}
		ch.Mode = MigrationModeDualWrite
	case c.Storage.Backend == StorageBackendS3:
		ch.Providers = []string{ProviderS3}
		if c.Storage.Migration.Completed {
			ch.Mode = MigrationModeCompleted
		}
	default:
		ch.Providers = []string{ProviderFilestore}
	}
	c.Storage.Chain = ch
}

// legacyChainWarning is branch ①'s every-boot migration hint (and the
// shared wording for the env-only spelling of the same state).
func legacyChainWarning(where string) string {
	return "config: the storage chain is configured through " + where +
		" with no binstore.yaml next to the main configuration file; the existing spelling stays in effect for this compatibility window (ADR-0036) — consider moving it to a binstore.yaml"
}

// BuildDefaultConfig assembles the no-binflow.yaml boot — defaults plus
// environment, the FR-6-AC5 bare-binary form — with binstore.yaml resolved
// through the main config's own default lookup order (./binstore.yaml, then
// $BINFLOW_HOME/binstore.yaml, so the default form keeps "binstore.yaml
// next to where binflow.yaml would be", ADR-0036 decision 1).
//
// T-349 (FR-113.5, T-325's registered ordering question) settled the boot
// order: the discovery runs BEFORE the environment overrides are validated,
// mirroring buildWithBinstore's verdict order on the file-based path so the
// two boot paths share one precedence:
//
//   - a binstore.yaml that is present OWNS the chain and its own errors
//     fire first (an unreadable/bad file refuses the boot before any
//     env-spelled complaint);
//   - with the file owning the chain, the chain-scoped env keys are SKIPPED
//     (decision 6) whether their set is complete or not — a stale partial
//     BINFLOW_STORAGE__BACKEND=s3 from a deployment pipeline is the same
//     "ignored + WARN" as a complete one, not a boot-killing missing-secret
//     refusal about keys the file replaces anyway;
//   - with NO binstore.yaml anywhere, the env-only chain set is validated
//     at load time and an incomplete S3 group refuses the boot naming the
//     missing key (fail fast, the pre-boot posture Validate always had).
//
// The returned Config is fully validated.
func BuildDefaultConfig(home string) (*Config, error) {
	env := environ()
	candidates := []string{BinstoreFileName}
	if home != "" {
		candidates = append(candidates, filepath.Join(home, BinstoreFileName))
	}
	for _, path := range candidates {
		overlay, err := loadBinstoreAt(path)
		if err != nil {
			return nil, err
		}
		if overlay == nil {
			continue
		}
		c, err := buildWithOptions(&raw{}, env, buildOpts{skipChainEnv: true})
		if err != nil {
			return nil, err
		}
		overlay.apply(c)
		for _, name := range chainEnvNames(env) {
			c.StartupWarnings = append(c.StartupWarnings, fmt.Sprintf(
				"config: %s is set but ignored: binstore.yaml %s owns the storage chain (secret environment variables still apply)",
				name, overlay.path))
		}
		if err := c.Validate(); err != nil {
			return nil, err
		}
		return c, nil
	}
	c, err := build(&raw{}, env)
	if err != nil {
		return nil, err
	}
	if names := chainEnvNames(env); len(names) > 0 {
		c.StartupWarnings = append(c.StartupWarnings, legacyChainWarning(
			"the environment variable(s) "+strings.Join(names, ", ")))
	}
	fillChainFromConfig(c)
	return c, nil
}

// slicesContains is the pre-generics-free spelling kept local to avoid
// dragging a dependency for two calls.
func slicesContains(list []string, want string) bool {
	for _, s := range list {
		if s == want {
			return true
		}
	}
	return false
}
