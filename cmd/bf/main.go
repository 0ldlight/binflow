// Command bf is the BinFlow CLI client.
//
// It provides a command-line interface for common BinFlow operations:
// repository creation (repo create), artifact upload (artifact upload),
// user creation (user create) and API token issuance (token create). The
// T-148 skeleton established the binary; T-166 wires the four subcommands
// on top of internal/client (no cobra — plain flag package dispatch).
//
// Configuration lives in ~/.bf/config.yaml (override with BF_CONFIG):
// named profiles hold the server base URL and credential REFERENCES —
// secrets themselves never touch the disk, they are resolved from
// environment variables at runtime (see config.go).
//
// Output convention: success prints the key result to stdout
// (machine-readable first); failure exits non-zero with the error on
// stderr, surfacing the server's errors[].message when present.
//
// Version is injected at build time via ldflags (same source as
// binflow-server and bf-migrate: .goreleaser.yaml or the Makefile release
// targets). A bare build leaves the honest dev placeholder in place.
package main

import (
	"context"
	"crypto/sha256"
	"encoding/hex"
	"errors"
	"flag"
	"fmt"
	"io"
	"os"
	"strings"

	"github.com/lzwzzy/binflow/internal/client"
)

// version and revision are stamped at build time by the release faces
// (goreleaser ldflags -X main.version/-X main.revision, T-127/FR-34, T-148).
// A bare build leaves the dev placeholder.
var (
	version  = "dev"
	revision = "dev"
)

const usage = `bf is the BinFlow CLI client.

Usage:

	bf [global flags] <command> [flags] [arguments]

Commands:

	repo create        Create a repository
	artifact upload    Upload a file into a repository
	user create        Create a user
	token create       Create an API token (value shown once)
	license keygen     Generate an ed25519 license key pair (offline)
	license issue      Sign a license document (offline)
	license inspect    Parse and verify a license document (offline)
	help               Show this help text

Global flags:

	--profile <name>   Configuration profile to use (default "default")
	--server <url>     BinFlow server base URL (overrides profile and env)
	--version          Show the binary version
	--help, -h         Show this usage text

Configuration:

	bf reads ~/.bf/config.yaml (or $BF_CONFIG). Profiles carry the server
	base URL plus credential references; secrets stay in the environment:

		profiles:
		  default:
		    base_url: http://localhost:8080
		    username: admin
		    password_env: BINFLOW_PASSWORD

Environment:

	BF_CONFIG            Path to the configuration file
	BF_BASE_URL          Override the profile base URL
	BINFLOW_SERVER_URL   Alias for BF_BASE_URL (kept from the T-148 skeleton)
	BF_TOKEN             Bearer token (highest credential precedence)
	BF_USERNAME          Basic-auth username (overrides the profile username)
	BF_PASSWORD          Basic-auth password (overrides password_env)

Run "bf <command> --help" for command-specific flags.
`

const repoUsage = `bf repo — manage repositories.

Usage:

	bf repo create <key> [flags]

Run "bf repo create --help" for the create flags.
`

const repoCreateUsage = `bf repo create — create a repository.

Usage:

	bf repo create <key> [flags]

Arguments:

	<key>    Repository key (unique, 1-62 characters)

Flags:

	--type <class>          Repository class: local, remote or virtual
	                             (default "local")
	--package-type <type>   Package type: generic, docker, maven, npm or pypi
	                             (default "generic")
	--description <text>    Optional human-readable description
	--help, -h              Show this help text

Example:

	bf repo create test-repo --type local --package-type generic
`

const artifactUsage = `bf artifact — manage artifacts.

Usage:

	bf artifact upload <file> [flags]

Run "bf artifact upload --help" for the upload flags.
`

const artifactUploadUsage = `bf artifact upload — upload a file into a repository.

Usage:

	bf artifact upload <file> [flags]

Arguments:

	<file>    Local file to upload

Flags:

	--repo <key>          Target repository key (required)
	--path <path>         Target path inside the repository, e.g.
	                          doc/readme.md (required)
	--content-type <ct>   Override Content-Type (default: server-side
	                          inference from the extension)
	--help, -h            Show this help text

Example:

	bf artifact upload README.md --repo test-repo --path doc/readme.md
`

const userUsage = `bf user — manage users.

Usage:

	bf user create <username> [flags]

Run "bf user create --help" for the create flags.
`

//nolint:gosec // G101 false positive: the help text names the --password flags; no credential is hardcoded
const userCreateUsage = `bf user create — create a user.

Usage:

	bf user create <username> [flags]

Arguments:

	<username>    User name (lowercase; the server rejects mixed case)

Flags:

	--password <pw>         Initial password (visible in shell history;
	                            prefer --password-env)
	--password-env <var>    Environment variable holding the password
	--email <addr>          User email (required by the server)
	--admin                 Grant administrator privileges
	--help, -h              Show this help text

Example:

	bf user create ci-user --password-env CI_PW --email ci@example.com
`

//nolint:gosec // G101 false positive: the help text names the token flags; no credential is hardcoded
const tokenUsage = `bf token — manage API tokens.

Usage:

	bf token create [flags]

Run "bf token create --help" for the create flags.
`

//nolint:gosec // G101 false positive: the help text names the token flags; no credential is hardcoded
const tokenCreateUsage = `bf token create — create an API token.

The token value is printed as the first stdout line (script-friendly) and
is shown exactly once; the server cannot retrieve it later.

Usage:

	bf token create [flags]

Flags:

	--username <name>     Token subject (defaults to the profile username)
	--expires-in <secs>   Token lifetime in seconds (0 = server default)
	--scope <scope>       Token scope, e.g. "api:*"
	--description <text>  Human-readable label
	--help, -h            Show this help text

Example:

	bf token create --username deployer --expires-in 3600
`

// validRclasses and validPackageTypes mirror the server's repository model
// (docs/design/architecture.md §5.1): fail fast client-side with a clear
// message instead of a server round-trip.
var (
	validRclasses     = map[string]bool{"local": true, "remote": true, "virtual": true}
	validPackageTypes = map[string]bool{"generic": true, "docker": true, "maven": true, "npm": true, "pypi": true}
)

// globalOpts carries the flags that apply to every subcommand. They must
// appear BEFORE the command word (stdlib flag parsing stops at the first
// non-flag argument).
type globalOpts struct {
	profile string
	server  string
}

func main() {
	if err := run(os.Args[1:], os.Stdout, os.Stderr); err != nil {
		if !errors.Is(err, flag.ErrHelp) {
			_, _ = fmt.Fprintf(os.Stderr, "bf: %v\n", err)
		}
		os.Exit(1)
	}
}

func run(args []string, stdout, stderr io.Writer) error {
	if len(args) == 0 {
		_, _ = fmt.Fprint(stdout, usage)
		return nil
	}

	fs := flag.NewFlagSet("bf", flag.ContinueOnError)
	fs.SetOutput(stderr)
	fs.Usage = func() { _, _ = fmt.Fprint(stderr, usage) }
	var opts globalOpts
	var showHelp, showVersion bool
	fs.StringVar(&opts.profile, "profile", "", "configuration profile name")
	fs.StringVar(&opts.server, "server", "", "BinFlow server base URL")
	fs.BoolVar(&showHelp, "help", false, "show this help text")
	fs.BoolVar(&showHelp, "h", false, "show this help text")
	fs.BoolVar(&showVersion, "version", false, "show the binary version")
	if err := fs.Parse(args); err != nil {
		return err
	}
	if showHelp {
		_, _ = fmt.Fprint(stdout, usage)
		return nil
	}
	if showVersion {
		_, _ = fmt.Fprintf(stdout, "bf %s (%s)\n", version, revision)
		return nil
	}

	rest := fs.Args()
	if len(rest) == 0 {
		_, _ = fmt.Fprint(stdout, usage)
		return nil
	}
	switch rest[0] {
	case "help", "--help", "-h":
		_, _ = fmt.Fprint(stdout, usage)
		return nil
	case "--version":
		_, _ = fmt.Fprintf(stdout, "bf %s (%s)\n", version, revision)
		return nil
	case "repo":
		return ignoreHelp(runRepo(rest[1:], opts, stdout))
	case "artifact":
		return ignoreHelp(runArtifact(rest[1:], opts, stdout))
	case "user":
		return ignoreHelp(runUser(rest[1:], opts, stdout))
	case "token":
		return ignoreHelp(runToken(rest[1:], opts, stdout))
	case "license":
		// Offline family (T-281): no client runtime, no profile — the
		// global flags parse but carry nothing for these verbs.
		return ignoreHelp(runLicense(rest[1:], stdout, stderr))
	default:
		return fmt.Errorf("unknown command %q, see --help for usage", rest[0])
	}
}

// ignoreHelp maps flag.ErrHelp to success: the subcommand's FlagSet has
// already printed its help text (via fs.Usage), so -h exits zero.
func ignoreHelp(err error) error {
	if errors.Is(err, flag.ErrHelp) {
		return nil
	}
	return err
}

// parseArgs parses flags from args while allowing positional arguments to
// be interleaved: the stdlib flag package stops at the first non-flag
// argument, so both "create <key> --type local" and "create --type local
// <key>" are accepted by consuming one positional and re-parsing the rest.
// A bare "--" terminates flag processing; everything after it is positional.
func parseArgs(fs *flag.FlagSet, args []string) ([]string, error) {
	var pos []string
	for {
		if err := fs.Parse(args); err != nil {
			return nil, err
		}
		args = fs.Args()
		if len(args) == 0 {
			return pos, nil
		}
		pos = append(pos, args[0])
		args = args[1:]
	}
}

// ---------------------------------------------------------------------------
// repo
// ---------------------------------------------------------------------------

func runRepo(args []string, opts globalOpts, stdout io.Writer) error {
	if len(args) == 0 || args[0] == "help" || args[0] == "--help" || args[0] == "-h" {
		_, _ = fmt.Fprint(stdout, repoUsage)
		return nil
	}
	if args[0] != "create" {
		return fmt.Errorf("unknown repo verb %q, try 'bf repo create'", args[0])
	}
	return repoCreate(args[1:], opts, stdout)
}

func repoCreate(args []string, opts globalOpts, stdout io.Writer) error {
	fs := flag.NewFlagSet("bf repo create", flag.ContinueOnError)
	fs.SetOutput(io.Discard)
	fs.Usage = func() { _, _ = fmt.Fprint(stdout, repoCreateUsage) }
	rclass := fs.String("type", "local", "repository class")
	packageType := fs.String("package-type", "generic", "package type")
	description := fs.String("description", "", "human-readable description")

	pos, err := parseArgs(fs, args)
	if err != nil {
		return fmt.Errorf("bf repo create: %w", err)
	}
	if len(pos) != 1 || pos[0] == "" {
		return fmt.Errorf("repo create requires exactly one <key> argument, got %d", len(pos))
	}
	key := pos[0]
	if !validRclasses[*rclass] {
		return fmt.Errorf("invalid --type %q: want local, remote or virtual", *rclass)
	}
	if !validPackageTypes[*packageType] {
		return fmt.Errorf("invalid --package-type %q: want generic, docker, maven, npm or pypi", *packageType)
	}

	rt, err := loadRuntime(opts)
	if err != nil {
		return err
	}
	info, err := rt.Client.CreateRepo(context.Background(), client.RepoCreateRequest{
		Key:         key,
		Rclass:      *rclass,
		PackageType: *packageType,
		Description: *description,
	})
	if err != nil {
		return err
	}
	created := key
	if info != nil && info.Key != "" {
		created = info.Key
	}
	_, _ = fmt.Fprintf(stdout, "Repository '%s' created.\n", created)
	return nil
}

// ---------------------------------------------------------------------------
// artifact
// ---------------------------------------------------------------------------

func runArtifact(args []string, opts globalOpts, stdout io.Writer) error {
	if len(args) == 0 || args[0] == "help" || args[0] == "--help" || args[0] == "-h" {
		_, _ = fmt.Fprint(stdout, artifactUsage)
		return nil
	}
	if args[0] != "upload" {
		return fmt.Errorf("unknown artifact verb %q, try 'bf artifact upload'", args[0])
	}
	return artifactUpload(args[1:], opts, stdout)
}

func artifactUpload(args []string, opts globalOpts, stdout io.Writer) error {
	fs := flag.NewFlagSet("bf artifact upload", flag.ContinueOnError)
	fs.SetOutput(io.Discard)
	fs.Usage = func() { _, _ = fmt.Fprint(stdout, artifactUploadUsage) }
	repo := fs.String("repo", "", "target repository key")
	nodePath := fs.String("path", "", "target path inside the repository")
	contentType := fs.String("content-type", "", "override Content-Type")

	pos, err := parseArgs(fs, args)
	if err != nil {
		return fmt.Errorf("bf artifact upload: %w", err)
	}
	if len(pos) != 1 || pos[0] == "" {
		return fmt.Errorf("artifact upload requires exactly one <file> argument, got %d", len(pos))
	}
	if *repo == "" {
		return fmt.Errorf("artifact upload requires --repo")
	}
	if *nodePath == "" {
		return fmt.Errorf("artifact upload requires --path")
	}

	file := pos[0]
	f, err := os.Open(file) //nolint:gosec // the path is the operator-provided CLI argument; opening it is the command's purpose.
	if err != nil {
		return fmt.Errorf("open %s: %w", file, err)
	}
	defer func() { _ = f.Close() }()
	st, err := f.Stat()
	if err != nil {
		return fmt.Errorf("stat %s: %w", file, err)
	}
	if st.IsDir() {
		return fmt.Errorf("%s is a directory", file)
	}
	sum, err := fileSha256(f)
	if err != nil {
		return err
	}

	rt, err := loadRuntime(opts)
	if err != nil {
		return err
	}
	if err := rt.Client.UploadArtifact(context.Background(), *repo, *nodePath, f, st.Size(), *contentType); err != nil {
		return err
	}
	// The printed URI carries the percent-escaped spelling (the same
	// EscapePathSegments the upload itself used): a raw '%'/'#'/'?' in the
	// node path would make the printed URL unparseable for the operator's
	// next curl/browser hop. The repo key holds no '/', so folding it into
	// the same call is exactly the per-segment escape.
	uri := strings.TrimRight(rt.BaseURL, "/") + "/binflow/" +
		client.EscapePathSegments(*repo+"/"+strings.TrimLeft(*nodePath, "/"))
	_, _ = fmt.Fprintf(stdout, "sha256: %s\n", sum)
	_, _ = fmt.Fprintf(stdout, "uri: %s\n", uri)
	return nil
}

// fileSha256 hashes the file from its current position and rewinds it so
// the subsequent upload streams from the start.
func fileSha256(f *os.File) (string, error) {
	h := sha256.New()
	if _, err := io.Copy(h, f); err != nil {
		return "", fmt.Errorf("hash %s: %w", f.Name(), err)
	}
	if _, err := f.Seek(0, io.SeekStart); err != nil {
		return "", fmt.Errorf("seek %s: %w", f.Name(), err)
	}
	return hex.EncodeToString(h.Sum(nil)), nil
}

// ---------------------------------------------------------------------------
// user
// ---------------------------------------------------------------------------

func runUser(args []string, opts globalOpts, stdout io.Writer) error {
	if len(args) == 0 || args[0] == "help" || args[0] == "--help" || args[0] == "-h" {
		_, _ = fmt.Fprint(stdout, userUsage)
		return nil
	}
	if args[0] != "create" {
		return fmt.Errorf("unknown user verb %q, try 'bf user create'", args[0])
	}
	return userCreate(args[1:], opts, stdout)
}

func userCreate(args []string, opts globalOpts, stdout io.Writer) error {
	fs := flag.NewFlagSet("bf user create", flag.ContinueOnError)
	fs.SetOutput(io.Discard)
	fs.Usage = func() { _, _ = fmt.Fprint(stdout, userCreateUsage) }
	password := fs.String("password", "", "initial password")
	passwordEnv := fs.String("password-env", "", "environment variable holding the password")
	email := fs.String("email", "", "user email")
	admin := fs.Bool("admin", false, "grant administrator privileges")

	pos, err := parseArgs(fs, args)
	if err != nil {
		return fmt.Errorf("bf user create: %w", err)
	}
	if len(pos) != 1 || pos[0] == "" {
		return fmt.Errorf("user create requires exactly one <username> argument, got %d", len(pos))
	}
	username := pos[0]
	if *email == "" {
		return fmt.Errorf("user create requires --email (the server rejects a blank email)")
	}
	pw := *password
	if pw == "" && *passwordEnv != "" {
		pw = os.Getenv(*passwordEnv)
	}
	if pw == "" {
		return fmt.Errorf("user create requires --password or --password-env")
	}

	rt, err := loadRuntime(opts)
	if err != nil {
		return err
	}
	info, err := rt.Client.CreateUser(context.Background(), client.UserCreateRequest{
		Name:     username,
		Password: pw,
		Email:    *email,
		Admin:    *admin,
	})
	if err != nil {
		return err
	}
	created := username
	if info != nil && info.Name != "" {
		created = info.Name
	}
	_, _ = fmt.Fprintf(stdout, "User '%s' created.\n", created)
	return nil
}

// ---------------------------------------------------------------------------
// token
// ---------------------------------------------------------------------------

func runToken(args []string, opts globalOpts, stdout io.Writer) error {
	if len(args) == 0 || args[0] == "help" || args[0] == "--help" || args[0] == "-h" {
		_, _ = fmt.Fprint(stdout, tokenUsage)
		return nil
	}
	if args[0] != "create" {
		return fmt.Errorf("unknown token verb %q, try 'bf token create'", args[0])
	}
	return tokenCreate(args[1:], opts, stdout)
}

func tokenCreate(args []string, opts globalOpts, stdout io.Writer) error {
	fs := flag.NewFlagSet("bf token create", flag.ContinueOnError)
	fs.SetOutput(io.Discard)
	fs.Usage = func() { _, _ = fmt.Fprint(stdout, tokenCreateUsage) }
	username := fs.String("username", "", "token subject")
	expiresIn := fs.Int64("expires-in", 0, "token lifetime in seconds")
	scope := fs.String("scope", "", "token scope")
	description := fs.String("description", "", "human-readable label")

	pos, err := parseArgs(fs, args)
	if err != nil {
		return fmt.Errorf("bf token create: %w", err)
	}
	if len(pos) != 0 {
		return fmt.Errorf("token create takes no positional arguments, got %d", len(pos))
	}

	rt, err := loadRuntime(opts)
	if err != nil {
		return err
	}
	subject := *username
	if subject == "" {
		subject = rt.Username
	}
	resp, err := rt.Client.CreateToken(context.Background(), client.TokenCreateRequest{
		Username:    subject,
		Scope:       *scope,
		ExpiresIn:   *expiresIn,
		Description: *description,
	})
	if err != nil {
		return err
	}
	if resp == nil || resp.Token == "" {
		return fmt.Errorf("server returned an empty token")
	}
	_, _ = fmt.Fprintln(stdout, resp.Token)
	if resp.TokenID != "" {
		_, _ = fmt.Fprintf(stdout, "token_id: %s\n", resp.TokenID)
	}
	if resp.Username != "" {
		_, _ = fmt.Fprintf(stdout, "username: %s\n", resp.Username)
	}
	if resp.Scope != "" {
		_, _ = fmt.Fprintf(stdout, "scope: %s\n", resp.Scope)
	}
	if resp.ExpiresIn > 0 {
		_, _ = fmt.Fprintf(stdout, "expires_in: %d\n", resp.ExpiresIn)
	}
	return nil
}
