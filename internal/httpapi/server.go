package httpapi

import (
	"context"
	"errors"
	"fmt"
	"log/slog"
	"net"
	"net/http"
	"time"

	"github.com/lzwzzy/binflow/internal/adapter"
	"github.com/lzwzzy/binflow/internal/auth"
	"github.com/lzwzzy/binflow/internal/config"
	"github.com/lzwzzy/binflow/internal/metadata"
	"github.com/lzwzzy/binflow/internal/repo"
)

// RepoLookup is the consumer-side repository-metadata seam the router
// needs to resolve a repo key to its package type for dispatch. The real
// repo.Service's GetRepo demands an authenticated principal, but the
// ROUTER must resolve anonymous content reads too (anonymous GET passes
// before any adapter runs, ADR-0009): the seam therefore reads the row
// directly. The package type is routing data, not protected content — an
// anonymous reader who learns that a repo key exists has learned nothing
// the download itself wouldn't reveal, and the row's config fields never
// cross this seam (only PackageType is read).
type RepoLookup interface {
	Get(ctx context.Context, repoKey string) (*metadata.Repo, error)
}

// Deps carries every collaborator the HTTP core needs. All fields are
// required except Version/Revision (empty Version reports DefaultVersion).
type Deps struct {
	Config   *config.Config
	Auth     auth.Authenticator
	Authz    auth.Authorizer
	Metadata metadata.Store
	Repos    RepoLookup
	// ReposSvc is the repository use-case service behind the compatible
	// /api/repositories and /api/storage planes (T-15). The router's own
	// dispatch uses the lighter RepoLookup above; these handlers need the
	// full service (permission-checked operations).
	ReposSvc repo.Service
	// Passwords rotates account passwords (PUT /api/security/password and
	// its real-route alias).
	Passwords auth.PasswordChanger
	// Tokens issues and revokes API tokens (/api/security/token[/revoke]).
	Tokens auth.TokenRegistry
	// DataDir is storage.data_dir — the health probe writes there and the
	// stats endpoint sizes blobs/ under it.
	DataDir string
	// Console serves "/" and "/binflow/" (M1 placeholder JSON, M4 SPA).
	Console http.Handler
	// Adapters are the mounted protocol handlers (architecture section
	// 5.1: cmd passes adapter.All(); tests inject per-stack instances so
	// the process-wide registry never couples test cases together).
	Adapters []adapter.Handler
	Version  string
	Revision string
}

// Server is the assembled HTTP surface (architecture section 7).
type Server struct {
	deps     Deps
	log      *slog.Logger
	adapters map[string]adapter.Handler // package type -> handler
	srv      *http.Server
}

// New assembles the server. deps.Console may be nil (a bare console
// placeholder is installed) and log may be nil (slog.Default()).
func New(deps Deps, log *slog.Logger) *Server {
	if log == nil {
		log = slog.Default()
	}
	if deps.Console == nil {
		deps.Console = http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
			writeError(w, http.StatusNotFound, "console is not configured")
		})
	}
	// Dispatch keys on the repository's package type (architecture section
	// 5.1): the registry maps a handler under both its protocol name and
	// each repo class it serves; the injected slice mirrors that contract
	// without the process-global singleton.
	adapters := make(map[string]adapter.Handler, len(deps.Adapters))
	for _, h := range deps.Adapters {
		adapters[h.Protocol()] = h
		for _, t := range h.RepoTypes() {
			adapters[t] = h
		}
	}
	s := &Server{deps: deps, log: log, adapters: adapters}

	s.srv = &http.Server{
		Handler:           s.route(),
		ReadHeaderTimeout: 10 * time.Second,
	}
	return s
}

// route is the top-level request switch. net/http's ServeMux is used ONLY
// for the two probe patterns (exact matches that can never contain a dot
// segment): every other path must bypass mux cleaning entirely, because
// ServeMux redirects any un-normalized path — even through a lone
// catch-all pattern — and that would rewrite /binflow/r/a/../b into
// /binflow/r/b ahead of the adapter's 400 defense (FR-4-AC10; probe
// evidence in router.go's package comment).
func (s *Server) route() http.Handler {
	probes := http.NewServeMux()
	probes.HandleFunc("/healthz", func(w http.ResponseWriter, r *http.Request) {
		s.probeHandler(false).ServeHTTP(w, r)
	})
	probes.HandleFunc("/readyz", func(w http.ResponseWriter, r *http.Request) {
		s.probeHandler(true).ServeHTTP(w, r)
	})
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		switch r.URL.EscapedPath() {
		case "/healthz", "/readyz":
			probes.ServeHTTP(w, r)
		default:
			s.rootHandler().ServeHTTP(w, r)
		}
	})
}

// Handler exposes the assembled handler (tests drive it with httptest;
// Run drives it with a listener).
func (s *Server) Handler() http.Handler { return s.srv.Handler }

// Run listens, serves and blocks until the listener fails or the context
// is canceled. SIGINT/SIGTERM handling belongs to cmd (T-16): it calls
// Shutdown with the config's graceful timeout, then closes storage and
// metadata in that order (architecture section 7.4).
func (s *Server) Run(ctx context.Context) error {
	ln, err := net.Listen("tcp", s.deps.Config.Server.Listen)
	if err != nil {
		return fmt.Errorf("httpapi: listen %s: %w", s.deps.Config.Server.Listen, err)
	}
	s.log.Info("binflow listening", "addr", ln.Addr().String(), "version", s.versionOrDefault())

	errCh := make(chan error, 1)
	go func() { errCh <- s.srv.Serve(ln) }()

	select {
	case err := <-errCh:
		if errors.Is(err, http.ErrServerClosed) {
			return nil
		}
		return fmt.Errorf("httpapi: serve: %w", err)
	case <-ctx.Done():
		return s.Shutdown(s.deps.Config.Server.GracefulTimeout)
	}
}

// Shutdown drains in-flight requests for at most timeout, then returns.
// Callers follow with storage.Close() then metadata.Close() (section 7.4);
// this method touches neither, so the ordering stays in one place (cmd).
func (s *Server) Shutdown(timeout time.Duration) error {
	if timeout <= 0 {
		timeout = config.DefaultGracefulTimeout
	}
	ctx, cancel := context.WithTimeout(context.Background(), timeout)
	defer cancel()
	if err := s.srv.Shutdown(ctx); err != nil {
		return fmt.Errorf("httpapi: shutdown within %s: %w", timeout, err)
	}
	return nil
}

func (s *Server) versionOrDefault() string {
	if s.deps.Version != "" {
		return s.deps.Version
	}
	return DefaultVersion
}
