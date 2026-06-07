package web

import (
	"context"
	"errors"
	"fmt"
	"html/template"
	"io/fs"
	"log"
	"net"
	"net/http"
	"net/url"
	"sync/atomic"
	"time"
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

	// Init, when non-nil, runs in the background after the listener is up
	// (clone/pull on container boot). Until it returns the server answers
	// every request with the initializing splash; a returned error switches
	// the splash to a failure state. The callback reports phase changes via
	// setStatus ("cloning…", "pulling…") and may return a board name (e.g.
	// from config loaded post-clone) that overrides Options.BoardName.
	Init func(setStatus func(string)) (boardName string, err error)
}

// Server holds the running state. Mutations take the Syncer's mutex, reads go
// straight to disk.
type Server struct {
	opts Options
	tmpl *template.Template
	auth *auth
	sync *Syncer

	ready      atomic.Bool
	initFailed atomic.Bool
	initStatus atomic.Value // string
}

// Run starts the server and blocks until ctx is cancelled (graceful shutdown)
// or a listener fails.
func Run(ctx context.Context, opts Options) error {
	if opts.Token == "" {
		return errors.New("web: access token is required")
	}
	funcs := template.FuncMap{"pathesc": url.PathEscape}
	tmpl, err := template.New("").Funcs(funcs).ParseFS(assets, "templates/*.html")
	if err != nil {
		return fmt.Errorf("web: parse templates: %w", err)
	}

	s := &Server{
		opts: opts,
		tmpl: tmpl,
		auth: newAuth(opts.Token, opts.Domain != ""),
	}
	s.initStatus.Store("starting…")

	if opts.Init == nil {
		s.finishInit()
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
				s.opts.BoardName = name
			}
			s.finishInit()
		}()
	}

	handler := s.middleware(s.routes())
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

// finishInit wires the Syncer (now that the repo exists on disk) and opens the
// board to requests.
func (s *Server) finishInit() {
	s.sync = NewSyncer(s.opts.BoardPath, s.opts.AuthorName, s.opts.AuthorEmail)
	if s.sync == nil {
		log.Printf("web: %s is not a git repository — running without sync", s.opts.BoardPath)
	} else if s.opts.PullEvery > 0 {
		go s.sync.PullLoop(context.Background(), s.opts.PullEvery)
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

	static, _ := fs.Sub(assets, "static")
	mux.Handle("GET /static/", http.StripPrefix("/static/", http.FileServerFS(static)))

	mux.HandleFunc("GET /healthz", s.handleHealthz)
	mux.HandleFunc("GET /login", s.handleLoginForm)
	mux.HandleFunc("POST /login", s.handleLogin)
	mux.HandleFunc("POST /logout", s.handleLogout)

	mux.HandleFunc("GET /{$}", s.handleBoard)
	mux.HandleFunc("GET /history", s.handleHistory)
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

		if r.URL.Path == "/login" {
			next.ServeHTTP(w, r)
			return
		}
		if !s.auth.validCookie(r) {
			http.Redirect(w, r, "/login", http.StatusSeeOther)
			return
		}
		next.ServeHTTP(w, r)
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
	if err := s.tmpl.ExecuteTemplate(w, name, data); err != nil {
		log.Printf("web: render %s: %v", name, err)
	}
}
