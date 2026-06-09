package web

import (
	"context"
	"errors"
	"fmt"
	"html/template"
	"log"
	"net"
	"net/http"
	"os"
	"sync"
	"sync/atomic"
	"time"

	"kbrd/config"
)

// Options configures the web server.
type Options struct {
	Addr         string // listen address for plain HTTP; ignored when Domain is set
	Domain       string // non-empty enables Let's Encrypt TLS on :443 (+ :80 redirect)
	CertCacheDir string // autocert cache; must survive restarts
	BoardPath    string // board directory (columns live here)
	BoardName    string // header label
	Token        string // shared access token (required)
	AuthorName   string // git commit author
	AuthorEmail  string
	PullEvery    time.Duration // background pull interval; 0 disables

	// InstanceName is this server's machine-local name, used to route
	// instance-scoped Lua timers (kbrd.timer.every(.., { instance = "..." })).
	InstanceName string

	// Scripting enables the headless task scheduler: a Lua host that loads the
	// board's init.lua / .kbrd.lua and fires their timers. Off by default —
	// `serve` is safe-by-default, and this runs board-supplied code. Only the
	// Enabled field plus the timeouts/limits are consulted.
	Scripting config.ScriptingConfig

	// Init, when non-nil, runs in the background after the listener is up
	// (clone/pull on container boot). Until it returns the server answers
	// every request with the initializing splash; a returned error switches
	// the splash to a failure state. The callback reports phase changes via
	// setStatus ("cloning…", "pulling…") and may return a board name (e.g.
	// from config loaded post-clone) that overrides Options.BoardName.
	Init func(setStatus func(string)) (boardName string, err error)

	// LoadConfig, when non-nil, re-loads config and re-resolves the
	// hot-reloadable subset with the same flag > env > toml precedence the
	// serve command applied at startup. Called once after init and again by
	// the config watcher whenever a watched config file is saved.
	LoadConfig func() (ReloadableConfig, error)

	// ConfigWatch lists config file paths whose saves trigger LoadConfig
	// (their parent directories are watched, so files may not exist yet).
	ConfigWatch []string

	// ConfigFile is the board config the web editor reads and writes
	// (the board's kbrd.toml); empty disables the /config editor.
	ConfigFile string

	// ValidateConfig vets candidate ConfigFile bytes before the editor
	// writes them, so a bad save cannot break the running server.
	ValidateConfig func([]byte) error
}

// ReloadableConfig is the subset of serve configuration that can change while
// the server runs. Addr and Domain are NOT hot-applied — kbrd.toml can arrive
// via git pull, and rebinding the listener or swapping the ACME domain from a
// pulled file would hand listener control to anyone with push access. They
// are carried here only so a change can be logged as "restart required".
type ReloadableConfig struct {
	BoardName string
	PullEvery time.Duration
	Addr      string
	Domain    string
}

// Server holds the running state. Mutations take the Syncer's mutex, reads go
// straight to disk.
type Server struct {
	opts Options
	tmpl atomic.Pointer[template.Template]
	auth *auth
	sync *Syncer

	ready      atomic.Bool
	initFailed atomic.Bool
	initStatus atomic.Value // string

	boardName atomic.Value // string; hot-reloadable header label

	// sched is the Lua task scheduler, set by finishInit once the board exists
	// and scripting is enabled (nil otherwise). The request-middleware hooks
	// call into it; it owns the single-threaded Lua VM.
	sched atomic.Pointer[taskScheduler]

	pullMu     sync.Mutex // guards the pull-loop fields below
	pullCancel context.CancelFunc
	pullEvery  time.Duration

	restartNote atomic.Value // string; last logged "restart required" diff
}

// currentBoardName returns the header label, preferring a hot-reloaded value.
// Handlers must use this instead of opts.BoardName: the init goroutine and
// the config watcher both update the name after requests start flowing.
func (s *Server) currentBoardName() string {
	if name, _ := s.boardName.Load().(string); name != "" {
		return name
	}
	return s.opts.BoardName
}

// restartPullLoop swaps the background pull loop to a new interval: no-op when
// unchanged, cancels the running loop, and starts a fresh one when every > 0.
func (s *Server) restartPullLoop(ctx context.Context, every time.Duration) {
	s.pullMu.Lock()
	defer s.pullMu.Unlock()
	if every == s.pullEvery && s.pullCancel != nil {
		return
	}
	if s.pullCancel != nil {
		s.pullCancel()
		s.pullCancel = nil
	}
	s.pullEvery = every
	if every <= 0 || s.sync == nil {
		return
	}
	loopCtx, cancel := context.WithCancel(ctx)
	s.pullCancel = cancel
	go s.sync.PullLoop(loopCtx, every)
}

// applyConfig hot-applies a reloaded config: board name and pull interval
// take effect immediately; a changed addr/domain is only logged (startup-only
// by design — see ReloadableConfig).
func (s *Server) applyConfig(ctx context.Context, rc ReloadableConfig) {
	s.boardName.Store(rc.BoardName) // "" falls back to opts.BoardName
	s.restartPullLoop(ctx, rc.PullEvery)

	if rc.Addr != s.opts.Addr || rc.Domain != s.opts.Domain {
		note := rc.Addr + "|" + rc.Domain
		if prev, _ := s.restartNote.Load().(string); prev != note {
			s.restartNote.Store(note)
			log.Printf("web: serve.addr/domain changed in config — restart kbrd serve to apply")
		}
	}
}

// Run starts the server and blocks until ctx is cancelled (graceful shutdown)
// or a listener fails.
func Run(ctx context.Context, opts Options) error {
	if opts.Token == "" {
		return errors.New("web: access token is required")
	}
	// The standard logger defaults to stderr; send all web logs to stdout.
	log.SetOutput(os.Stdout)
	// Parse the embedded set for the initializing splash; finishInit rebuilds
	// it with any .kbrd_web_templates overrides once the board exists on disk.
	tmpl, err := buildTemplates("")
	if err != nil {
		return err
	}

	s := &Server{
		opts: opts,
		auth: newAuth(opts.Token, opts.Domain != ""),
	}
	s.tmpl.Store(tmpl)
	s.initStatus.Store("starting…")

	if opts.Init == nil {
		s.finishInit(ctx)
	} else {
		go func() {
			name, err := opts.Init(func(st string) { s.initStatus.Store(st) })
			if err != nil {
				log.Printf("web: init failed: %v", err)
				s.initStatus.Store(err.Error())
				s.initFailed.Store(true)
				return
			}
			if name != "" {
				s.boardName.Store(name)
			}
			s.finishInit(ctx)
		}()
	}

	handler := s.accessLog(s.middleware(s.routes()))
	if opts.Domain != "" {
		return runTLS(ctx, opts, handler)
	}
	srv := newHTTPServer(opts.Addr, handler)
	log.Printf("web: board available at %s", displayURL(opts.Addr))
	return serveUntilDone(ctx, srv, func() error { return srv.ListenAndServe() })
}

// displayURL turns a listen address into the URL a user can open: a missing
// or wildcard host becomes localhost, the default http port is dropped.
func displayURL(addr string) string {
	host, port, err := net.SplitHostPort(addr)
	if err != nil {
		return "http://" + addr
	}
	switch host {
	case "", "0.0.0.0", "::":
		host = "localhost"
	}
	if port == "80" {
		return "http://" + host
	}
	return "http://" + net.JoinHostPort(host, port)
}

// finishInit wires the Syncer (now that the repo exists on disk), applies the
// on-disk config (it may have just been cloned), starts the config watcher,
// and opens the board to requests.
func (s *Server) finishInit(ctx context.Context) {
	s.sync = NewSyncer(s.opts.BoardPath, s.opts.AuthorName, s.opts.AuthorEmail, s.opts.InstanceName)
	if s.sync == nil {
		log.Printf("web: %s is not a git repository — running without sync", s.opts.BoardPath)
	}
	s.restartPullLoop(ctx, s.opts.PullEvery)

	// The board (and any .kbrd_web_templates overrides) now exist on disk —
	// rebuild the template set with overrides applied. A bad override keeps the
	// embedded set already stored at startup. Then hot-reload on save.
	if tmpl, err := buildTemplates(s.opts.BoardPath); err != nil {
		log.Printf("web: template overrides not applied: %v", err)
	} else {
		s.tmpl.Store(tmpl)
	}
	go s.watchTemplates(ctx)

	if s.opts.LoadConfig != nil {
		// Immediate load: in the --git-url flow the board's kbrd.toml only
		// exists after the clone that just finished.
		if rc, err := s.opts.LoadConfig(); err != nil {
			log.Printf("web: load config: %v", err)
		} else {
			s.applyConfig(ctx, rc)
		}
		cw, err := NewConfigWatcher(s.opts.ConfigWatch, s.opts.LoadConfig, func(rc ReloadableConfig) {
			log.Printf("web: config reloaded")
			s.applyConfig(ctx, rc)
		})
		if err != nil {
			log.Printf("web: config watcher disabled: %v", err)
		} else {
			go cw.Run(ctx)
		}
	}

	// Start the repeating-task scheduler once the board exists on disk and the
	// Syncer is wired (so task mutations commit/push). Off unless --scripting.
	if s.opts.Scripting.Enabled {
		switch ts, err := startTaskScheduler(ctx, s.opts.BoardPath, s.currentBoardName(), s.opts.InstanceName, s.opts.Scripting, s.sync); {
		case err != nil:
			log.Printf("web: task scheduler disabled: %v", err)
		case ts != nil:
			s.sched.Store(ts)
			log.Printf("web: task scheduler running (instance %q)", s.opts.InstanceName)
		default:
			log.Printf("web: scripting enabled but no init.lua/.kbrd.lua found")
		}
	}

	s.ready.Store(true)
	log.Printf("web: board ready at %s", s.opts.BoardPath)
}

// newHTTPServer applies the hardening defaults shared by all listeners.
func newHTTPServer(addr string, handler http.Handler) *http.Server {
	return &http.Server{
		Addr:              addr,
		Handler:           handler,
		ReadHeaderTimeout: 5 * time.Second,
		ReadTimeout:       30 * time.Second,
		WriteTimeout:      30 * time.Second,
		IdleTimeout:       120 * time.Second,
	}
}

// serveUntilDone runs listen (blocking) and shuts the server down gracefully
// when ctx is cancelled.
func serveUntilDone(ctx context.Context, srv *http.Server, listen func() error) error {
	errCh := make(chan error, 1)
	go func() { errCh <- listen() }()
	select {
	case <-ctx.Done():
		shutCtx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
		defer cancel()
		return srv.Shutdown(shutCtx)
	case err := <-errCh:
		return err
	}
}

// routes builds the mux. Auth and readiness gating live in middleware.
func (s *Server) routes() *http.ServeMux {
	mux := http.NewServeMux()

	mux.Handle("GET /static/", http.StripPrefix("/static/", http.FileServerFS(staticFS(s.opts.BoardPath))))

	mux.HandleFunc("GET /healthz", s.handleHealthz)
	mux.HandleFunc("GET /login", s.handleLoginForm)
	mux.HandleFunc("POST /login", s.handleLogin)
	mux.HandleFunc("POST /logout", s.handleLogout)

	mux.HandleFunc("GET /{$}", s.handleBoard)
	mux.HandleFunc("GET /history", s.handleHistory)
	mux.HandleFunc("GET /config", s.handleConfigForm)
	mux.HandleFunc("POST /config", s.handleConfigSave)
	mux.HandleFunc("GET /c/{col}", s.handleColumn)
	mux.HandleFunc("GET /c/{col}/new", s.handleNewForm)
	mux.HandleFunc("POST /c/{col}/cards", s.handleCreate)
	mux.HandleFunc("GET /c/{col}/i/{name}", s.handleEditForm)
	mux.HandleFunc("POST /c/{col}/i/{name}", s.handleSave)
	mux.HandleFunc("POST /c/{col}/i/{name}/delete", s.handleDelete)

	return mux
}

// middleware: security headers → readiness gate → body cap → auth.
func (s *Server) middleware(next http.Handler) http.Handler {
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		h := w.Header()
		h.Set("Content-Security-Policy", "default-src 'self'; frame-ancestors 'none'")
		h.Set("X-Content-Type-Options", "nosniff")
		h.Set("Referrer-Policy", "no-referrer")
		if s.opts.Domain != "" {
			h.Set("Strict-Transport-Security", "max-age=31536000")
		}

		if r.URL.Path == "/static/" || hasPrefixSegment(r.URL.Path, "/static") {
			next.ServeHTTP(w, r)
			return
		}
		if r.URL.Path == "/healthz" {
			next.ServeHTTP(w, r)
			return
		}

		if !s.ready.Load() {
			s.renderInitializing(w)
			return
		}

		if r.Method == http.MethodPost {
			r.Body = http.MaxBytesReader(w, r.Body, 1<<20)
		}

		// Lua request middleware (serve --scripting only). Runs before the
		// built-in cookie auth so a hook can gate even /login. Returns true when
		// it has already written the response (short-circuit), in which case we
		// stop here.
		if s.runRequestHook(w, r) {
			return
		}

		if r.URL.Path == "/login" {
			s.serveWithResponseHook(next, w, r)
			return
		}
		if !s.auth.validCookie(r) {
			http.Redirect(w, r, "/login", http.StatusSeeOther)
			return
		}
		s.serveWithResponseHook(next, w, r)
	})
}

func hasPrefixSegment(path, prefix string) bool {
	return len(path) > len(prefix) && path[:len(prefix)] == prefix && path[len(prefix)] == '/'
}

func (s *Server) handleHealthz(w http.ResponseWriter, r *http.Request) {
	if !s.ready.Load() {
		http.Error(w, "initializing", http.StatusServiceUnavailable)
		return
	}
	fmt.Fprintln(w, "ok")
}

func (s *Server) renderInitializing(w http.ResponseWriter) {
	status, _ := s.initStatus.Load().(string)
	failed := s.initFailed.Load()
	if !failed {
		w.Header().Set("Retry-After", "2")
	}
	w.WriteHeader(http.StatusServiceUnavailable)
	s.render(w, "initializing.html", map[string]any{"Status": status, "Failed": failed})
}

// render executes a template, logging (not exposing) render errors.
func (s *Server) render(w http.ResponseWriter, name string, data any) {
	if err := s.tmpl.Load().ExecuteTemplate(w, name, data); err != nil {
		log.Printf("web: render %s: %v", name, err)
	}
}
