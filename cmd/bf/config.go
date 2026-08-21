package main

import (
	"bytes"
	"crypto/tls"
	"encoding/base64"
	"errors"
	"fmt"
	"io"
	"io/fs"
	"net/http"
	"net/url"
	"os"
	"path/filepath"
	"sort"
	"strings"

	"github.com/lzwzzy/binflow/internal/client"
	yaml "go.yaml.in/yaml/v3"
)

// Profile is one named connection configuration inside ~/.bf/config.yaml.
//
// Secrets are never stored on disk: the profile carries only the NAME of an
// environment variable (password_env / token_env) that holds the secret at
// runtime (T-166 AC 2). Non-secret fields (base_url, username) may be
// written directly.
type Profile struct {
	// BaseURL is the BinFlow server base URL, e.g. "http://localhost:8080".
	BaseURL string `yaml:"base_url"`

	// Username is the basic-auth username (non-secret).
	Username string `yaml:"username,omitempty"`

	// PasswordEnv names the environment variable holding the basic-auth
	// password (the password itself never enters the config file).
	PasswordEnv string `yaml:"password_env,omitempty"`

	// TokenEnv names the environment variable holding a bearer API token.
	TokenEnv string `yaml:"token_env,omitempty"`

	// SkipTLSVerify disables TLS certificate verification for this profile
	// (developer convenience against self-signed instances).
	SkipTLSVerify bool `yaml:"skip_tls_verify,omitempty"`
}

// Config is the parsed ~/.bf/config.yaml.
type Config struct {
	// Profiles maps a profile name to its connection settings.
	Profiles map[string]Profile `yaml:"profiles"`

	// DefaultProfile selects the profile used when --profile is absent.
	// Empty means "default".
	DefaultProfile string `yaml:"default_profile,omitempty"`
}

// defaultProfileName is the profile used when neither --profile nor
// default_profile selects one.
const defaultProfileName = "default"

// configPath returns the configuration file path: $BF_CONFIG when set,
// otherwise ~/.bf/config.yaml.
func configPath() string {
	if p := os.Getenv("BF_CONFIG"); p != "" {
		return p
	}
	home, err := os.UserHomeDir()
	if err != nil {
		return ""
	}
	return filepath.Join(home, ".bf", "config.yaml")
}

// LoadConfig reads and parses the configuration file. A missing file is not
// an error — the CLI falls back to built-in defaults (zero-config usage).
// path may be empty, in which case configPath() decides.
func LoadConfig(path string) (*Config, error) {
	if path == "" {
		path = configPath()
	}
	if path == "" {
		return &Config{}, nil
	}
	data, err := os.ReadFile(path) //nolint:gosec // the path is the operator-provided BF_CONFIG / ~/.bf/config.yaml by design.
	if err != nil {
		if errors.Is(err, fs.ErrNotExist) {
			return &Config{}, nil
		}
		return nil, fmt.Errorf("read config %s: %w", path, err)
	}
	return decodeConfig(data)
}

// decodeConfig parses exactly one YAML document (trailing documents are
// rejected, mirroring the internal/config multi-document ruling from T-8).
// An empty document yields an empty Config.
func decodeConfig(data []byte) (*Config, error) {
	dec := yaml.NewDecoder(bytes.NewReader(data))
	var cfg Config
	if err := dec.Decode(&cfg); err != nil {
		if errors.Is(err, io.EOF) {
			return &Config{}, nil // empty file
		}
		return nil, fmt.Errorf("parse config YAML: %w", err)
	}
	var extra yaml.Node
	if err := dec.Decode(&extra); !errors.Is(err, io.EOF) {
		return nil, fmt.Errorf("parse config YAML: expected exactly one document")
	}
	return &cfg, nil
}

// resolveProfile returns the effective profile for the given --profile
// value. An empty config (no profiles at all) resolves to a zero Profile so
// the CLI works without any configuration file; a non-empty config with a
// missing profile name is an operator error.
func (c *Config) resolveProfile(name string) (Profile, error) {
	if name == "" {
		name = c.DefaultProfile
	}
	if name == "" {
		name = defaultProfileName
	}
	if p, ok := c.Profiles[name]; ok {
		return p, nil
	}
	if len(c.Profiles) == 0 {
		return Profile{}, nil
	}
	return Profile{}, fmt.Errorf("profile %q not found in config (available: %s)",
		name, strings.Join(sortedProfileNames(c.Profiles), ", "))
}

func sortedProfileNames(m map[string]Profile) []string {
	names := make([]string, 0, len(m))
	for n := range m {
		names = append(names, n)
	}
	sort.Strings(names)
	return names
}

// runtimeConfig is the fully resolved execution context for one command.
type runtimeConfig struct {
	// Client is the internal/client consumer used by all subcommands.
	Client *client.Client

	// BaseURL is the resolved server base URL (no trailing slash).
	BaseURL string

	// Username is the resolved basic-auth username (flag/env/profile),
	// used as the default token subject.
	Username string
}

// loadRuntime loads the config file, resolves the profile and builds the
// client. It is called after argument validation so flag errors never
// touch the filesystem or network.
func loadRuntime(opts globalOpts) (runtimeConfig, error) {
	cfg, err := LoadConfig("")
	if err != nil {
		return runtimeConfig{}, err
	}
	prof, err := cfg.resolveProfile(opts.profile)
	if err != nil {
		return runtimeConfig{}, err
	}
	return buildClient(prof, opts.server)
}

// buildClient assembles the internal/client Client from a profile plus
// environment overrides.
//
// Precedence, highest first:
//
//	base URL  --server flag > BF_BASE_URL / BINFLOW_SERVER_URL > profile.base_url > built-in default
//	token     BF_TOKEN > profile.token_env lookup
//	basic     BF_USERNAME + BF_PASSWORD > profile.username + profile.password_env lookup
//
// A resolved token wins over basic credentials (the client injects the
// Bearer header, and the server's auth precedence puts Bearer first).
func buildClient(prof Profile, serverOverride string) (runtimeConfig, error) {
	baseURL := firstNonEmpty(serverOverride,
		os.Getenv("BF_BASE_URL"), os.Getenv("BINFLOW_SERVER_URL"), prof.BaseURL)
	if baseURL == "" {
		baseURL = client.DefaultBaseURL
	}
	if err := validateBaseURL(baseURL); err != nil {
		return runtimeConfig{}, err
	}

	username := firstNonEmpty(os.Getenv("BF_USERNAME"), prof.Username)
	password := firstNonEmpty(os.Getenv("BF_PASSWORD"), envValue(prof.PasswordEnv))

	transport := http.RoundTripper(http.DefaultTransport)
	if prof.SkipTLSVerify {
		base, _ := http.DefaultTransport.(*http.Transport)
		t := base.Clone()
		t.TLSClientConfig = &tls.Config{
			InsecureSkipVerify: true, //nolint:gosec // opt-in via the profile's skip_tls_verify field; developer convenience against self-signed instances.
		}
		transport = t
	}
	if password != "" {
		if username == "" {
			return runtimeConfig{}, fmt.Errorf("password configured but no username (set the profile username or BF_USERNAME)")
		}
		transport = &basicAuthTransport{rt: transport, user: username, pass: password}
	}

	c := client.NewWithClient(&http.Client{
		Transport: transport,
		Timeout:   client.DefaultTimeout,
	})
	c.BaseURL = baseURL
	if token := firstNonEmpty(os.Getenv("BF_TOKEN"), envValue(prof.TokenEnv)); token != "" {
		c.Token = token
	}
	return runtimeConfig{Client: c, BaseURL: baseURL, Username: username}, nil
}

// validateBaseURL rejects base URLs the client could never talk to.
func validateBaseURL(raw string) error {
	u, err := url.Parse(raw)
	if err != nil || u.Host == "" || (u.Scheme != "http" && u.Scheme != "https") {
		return fmt.Errorf("invalid server URL %q: want http(s)://host[:port]", raw)
	}
	return nil
}

// basicAuthTransport injects an Authorization: Basic header on requests
// that carry no Authorization header (the client package only injects
// Bearer tokens; basic auth is composed here at the CLI layer).
type basicAuthTransport struct {
	rt   http.RoundTripper
	user string
	pass string
}

func (t *basicAuthTransport) RoundTrip(req *http.Request) (*http.Response, error) {
	if req.Header.Get("Authorization") == "" {
		req = req.Clone(req.Context())
		req.Header.Set("Authorization", "Basic "+basicCredentials(t.user, t.pass))
	}
	return t.rt.RoundTrip(req)
}

func basicCredentials(user, pass string) string {
	return base64.StdEncoding.EncodeToString([]byte(user + ":" + pass))
}

// firstNonEmpty returns the first non-empty argument, or "".
func firstNonEmpty(values ...string) string {
	for _, v := range values {
		if v != "" {
			return v
		}
	}
	return ""
}

// envValue looks up an environment variable by name; an empty name yields "".
func envValue(name string) string {
	if name == "" {
		return ""
	}
	return os.Getenv(name)
}
