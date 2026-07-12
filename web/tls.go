package web

import (
	"context"
	"log"
	"net/http"

	"golang.org/x/crypto/acme/autocert"
	"golang.org/x/sync/errgroup"
)

// runTLS serves handler on :443 with Let's Encrypt certificates for
// opts.Domain, plus a :80 listener that answers ACME HTTP-01 challenges and
// redirects everything else to https. Both shut down when ctx is cancelled.
func runTLS(ctx context.Context, opts Options, handler http.Handler, logger *log.Logger) error {
	manager := &autocert.Manager{
		Prompt:     autocert.AcceptTOS,
		HostPolicy: autocert.HostWhitelist(opts.Domain),
		Cache:      autocert.DirCache(opts.CertCacheDir),
	}

	tlsSrv := newHTTPServer(":443", handler)
	tlsSrv.TLSConfig = manager.TLSConfig()

	redirect := http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		// /healthz stays answerable over plain HTTP so an in-container
		// healthcheck works without TLS or DNS for the public domain.
		if r.URL.Path == "/healthz" {
			handler.ServeHTTP(w, r)
			return
		}
		http.Redirect(w, r, "https://"+opts.Domain+r.URL.RequestURI(), http.StatusMovedPermanently)
	})
	httpSrv := newHTTPServer(":80", manager.HTTPHandler(redirect))

	logf(logger, "web: board available at https://%s (TLS on :443, ACME + redirect on :80)", opts.Domain)

	g, gctx := errgroup.WithContext(ctx)
	g.Go(func() error {
		return serveUntilDone(gctx, tlsSrv, func() error { return tlsSrv.ListenAndServeTLS("", "") })
	})
	g.Go(func() error {
		return serveUntilDone(gctx, httpSrv, func() error { return httpSrv.ListenAndServe() })
	})
	return g.Wait()
}
