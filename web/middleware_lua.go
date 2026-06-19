package web

import (
	"bytes"
	"maps"
	"net/http"
	"strconv"

	"kbrd/events"
	"kbrd/script"
)

// runRequestHook fires the Lua http_request hook (if any) before the built-in
// auth. It returns true when the hook short-circuited the request and has
// already written the full response (a "respond" or "redirect" verdict); the
// caller must then stop. A "continue" verdict mutates r in place and returns
// false so the normal chain proceeds.
//
// Fail-open everywhere: when scripting is off, no hook is registered, the VM is
// busy, or the evaluation could not run, the request passes through unchanged.
func (s *Server) runRequestHook(w http.ResponseWriter, r *http.Request) bool {
	sched := s.sched.Load()
	if !sched.HasHook(events.NameHTTPRequest) {
		return false
	}
	verdict, ok := sched.EvalRequest(r.Context(), script.HTTPRequestData{
		Method:     r.Method,
		Path:       r.URL.Path,
		RawQuery:   r.URL.RawQuery,
		Headers:    r.Header.Clone(),
		RemoteAddr: r.RemoteAddr,
	})
	if !ok || verdict.Skipped {
		return false
	}
	switch verdict.Action {
	case "respond":
		for k, v := range verdict.Headers {
			w.Header().Set(k, v)
		}
		status := verdict.Status
		if status == 0 {
			status = http.StatusOK
		}
		w.WriteHeader(status)
		_, _ = w.Write([]byte(verdict.Body))
		return true
	case "redirect":
		status := verdict.Status
		if status == 0 {
			status = http.StatusSeeOther
		}
		http.Redirect(w, r, verdict.Location, status)
		return true
	default: // "continue"
		applyRewrite(r, verdict.Rewrite)
		return false
	}
}

// applyRewrite mutates the live request per a continuing http_request verdict.
// Path and RawQuery are replaced only when non-empty; headers are set/deleted.
// kbrd's handlers call r.FormValue/PathValue after this point, so the rewritten
// query and path take effect. Form-body fields are not rewritable here (would
// require buffering and re-encoding the body) — documented in SCRIPTING.md.
func applyRewrite(r *http.Request, rw *script.HTTPRewrite) {
	if rw == nil {
		return
	}
	if rw.Path != "" {
		r.URL.Path = rw.Path
	}
	if rw.Query != "" {
		r.URL.RawQuery = rw.Query
	}
	for k, v := range rw.SetHeaders {
		r.Header.Set(k, v)
	}
	for _, k := range rw.DelHeaders {
		r.Header.Del(k)
	}
}

// serveWithResponseHook runs next and, when an http_response hook is registered,
// captures its response so the hook can rewrite status/headers/body before it
// reaches the client. With no such hook it calls next directly — the streaming,
// zero-overhead path (response buffering is only paid for when actually used).
func (s *Server) serveWithResponseHook(next http.Handler, w http.ResponseWriter, r *http.Request) {
	sched := s.sched.Load()
	if !sched.HasHook(events.NameHTTPResponse) {
		next.ServeHTTP(w, r)
		return
	}

	cap := &captureWriter{header: http.Header{}, status: http.StatusOK}
	next.ServeHTTP(cap, r)

	verdict, ok := sched.EvalResponse(r.Context(), script.HTTPResponseData{
		Method:  r.Method,
		Path:    r.URL.Path,
		Status:  cap.status,
		Headers: cap.header,
		Body:    cap.buf.String(),
	})
	cap.flush(w, verdict, ok)
}

// captureWriter buffers a handler's response so an http_response hook can rewrite
// it before anything is flushed to the real ResponseWriter. It intentionally
// defeats streaming/http.Flusher — acceptable because kbrd serves small HTML
// pages and capture only happens when a response hook is registered.
type captureWriter struct {
	header http.Header
	status int
	buf    bytes.Buffer
}

func (c *captureWriter) Header() http.Header         { return c.header }
func (c *captureWriter) WriteHeader(status int)      { c.status = status }
func (c *captureWriter) Write(b []byte) (int, error) { return c.buf.Write(b) }

// flush applies the hook verdict (when ok and Changed) over the captured
// response and writes the result to the real writer, correcting Content-Length
// to the final body so a rewritten body is never truncated or padded.
func (c *captureWriter) flush(w http.ResponseWriter, verdict script.HTTPResponseVerdict, ok bool) {
	status := c.status
	body := c.buf.Bytes()

	dst := w.Header()
	maps.Copy(dst, c.header)
	if ok && verdict.Changed && !verdict.Skipped {
		for k, v := range verdict.SetHeaders {
			dst.Set(k, v)
		}
		if verdict.Status != 0 {
			status = verdict.Status
		}
		if verdict.Body != nil {
			body = []byte(*verdict.Body)
		}
	}
	dst.Set("Content-Length", strconv.Itoa(len(body)))
	w.WriteHeader(status)
	_, _ = w.Write(body)
}
