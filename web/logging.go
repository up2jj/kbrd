package web

import (
	"net/http"
	"time"
)

// statusRecorder is a pass-through http.ResponseWriter that remembers the status
// code and counts bytes written, so accessLog can report them. Unlike
// captureWriter (web/middleware_lua.go), it does NOT buffer the body — Write goes
// straight to the underlying writer, preserving streaming.
type statusRecorder struct {
	http.ResponseWriter
	status int
	bytes  int
}

func (r *statusRecorder) WriteHeader(code int) {
	r.status = code
	r.ResponseWriter.WriteHeader(code)
}

func (r *statusRecorder) Write(b []byte) (int, error) {
	if r.status == 0 {
		r.status = http.StatusOK // implicit 200 when the handler writes without WriteHeader
	}
	n, err := r.ResponseWriter.Write(b)
	r.bytes += n
	return n, err
}

// accessLog is the outermost middleware: it emits one stdout line per request
// (method, path, status, bytes, duration, client IP), covering every request —
// including static assets, health checks, and Lua-short-circuited responses.
func (s *Server) accessLog(next http.Handler) http.Handler {
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		start := time.Now()
		rec := &statusRecorder{ResponseWriter: w}
		next.ServeHTTP(rec, r)
		status := rec.status
		if status == 0 {
			status = http.StatusOK // handler returned without writing anything
		}
		s.logf("web: %s %s %d %dB %s %s",
			r.Method, r.URL.Path, status, rec.bytes,
			time.Since(start).Round(time.Microsecond), clientIP(r))
	})
}
