package web

import (
	"context"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"testing"

	"kbrd/config"
)

func scriptingCfg() config.ScriptingConfig {
	return config.ScriptingConfig{
		Enabled:          true,
		CommandTimeoutMs: 2000,
		HookTimeoutMs:    500,
	}
}

// withLuaServer builds a test server whose board carries the given .kbrd.lua and
// attaches a running task scheduler so the request/response hooks fire. The
// scheduler is torn down when the test ends.
func withLuaServer(t *testing.T, lua string) (http.Handler, *http.Cookie) {
	t.Helper()
	s, h, boardDir := newTestServer(t)
	if err := os.WriteFile(filepath.Join(boardDir, ".kbrd.lua"), []byte(lua), 0o644); err != nil {
		t.Fatal(err)
	}
	ctx, cancel := context.WithCancel(context.Background())
	t.Cleanup(cancel)
	ts, err := startTaskScheduler(ctx, boardDir, "test", "", scriptingCfg(), nil, nil)
	if err != nil {
		t.Fatalf("scheduler: %v", err)
	}
	if ts == nil {
		t.Fatal("expected a scheduler")
	}
	s.sched.Store(ts)
	return h, loginCookie(t, h)
}

func TestRequestHookRespondBeforeAuth(t *testing.T) {
	h, _ := withLuaServer(t, `
		kbrd.on("http_request", function(req)
			if req.path == "/blocked" then
				return { action="respond", status=403, body="blocked" }
			end
			return nil
		end)`)

	// No auth cookie: the hook still runs (it is before auth) and short-circuits.
	req := httptest.NewRequest("GET", "/blocked", nil)
	rec := httptest.NewRecorder()
	h.ServeHTTP(rec, req)
	if rec.Code != http.StatusForbidden || rec.Body.String() != "blocked" {
		t.Fatalf("expected 403 blocked, got %d %q", rec.Code, rec.Body.String())
	}
}

func TestRequestHookRedirect(t *testing.T) {
	h, cookie := withLuaServer(t, `
		kbrd.on("http_request", function(req)
			if req.path == "/old" then
				return { action="redirect", location="/", status=302 }
			end
			return nil
		end)`)

	req := httptest.NewRequest("GET", "/old", nil)
	req.AddCookie(cookie)
	rec := httptest.NewRecorder()
	h.ServeHTTP(rec, req)
	if rec.Code != http.StatusFound || rec.Header().Get("Location") != "/" {
		t.Fatalf("expected 302 → /, got %d %q", rec.Code, rec.Header().Get("Location"))
	}
}

func TestRequestHookContinuePassesThrough(t *testing.T) {
	// A declining hook must not disturb a normal authenticated request.
	h, cookie := withLuaServer(t, `kbrd.on("http_request", function(req) return nil end)`)

	req := httptest.NewRequest("GET", "/", nil)
	req.AddCookie(cookie)
	rec := httptest.NewRecorder()
	h.ServeHTTP(rec, req)
	if rec.Code != http.StatusOK {
		t.Fatalf("expected 200 board, got %d", rec.Code)
	}
}

func TestResponseHookRewritesHeaders(t *testing.T) {
	h, cookie := withLuaServer(t, `
		kbrd.on("http_response", function(resp)
			if resp.path == "/" then
				return { set_headers={["X-Kbrd-Hook"]="hello"} }
			end
			return nil
		end)`)

	req := httptest.NewRequest("GET", "/", nil)
	req.AddCookie(cookie)
	rec := httptest.NewRecorder()
	h.ServeHTTP(rec, req)
	if rec.Code != http.StatusOK {
		t.Fatalf("expected 200, got %d", rec.Code)
	}
	if got := rec.Header().Get("X-Kbrd-Hook"); got != "hello" {
		t.Fatalf("expected response hook header, got %q", got)
	}
}

func TestResponseHookRewritesBody(t *testing.T) {
	h, cookie := withLuaServer(t, `
		kbrd.on("http_response", function(resp)
			return { body="OVERRIDDEN" }
		end)`)

	req := httptest.NewRequest("GET", "/", nil)
	req.AddCookie(cookie)
	rec := httptest.NewRecorder()
	h.ServeHTTP(rec, req)
	if rec.Body.String() != "OVERRIDDEN" {
		t.Fatalf("expected overridden body, got %q", rec.Body.String())
	}
	if rec.Header().Get("Content-Length") != "10" {
		t.Fatalf("expected corrected Content-Length 10, got %q", rec.Header().Get("Content-Length"))
	}
}

// With no scheduler attached the chain behaves exactly as before (zero overhead).
func TestNoSchedulerZeroOverhead(t *testing.T) {
	_, h, _ := newTestServer(t)
	cookie := loginCookie(t, h)
	req := httptest.NewRequest("GET", "/", nil)
	req.AddCookie(cookie)
	rec := httptest.NewRecorder()
	h.ServeHTTP(rec, req)
	if rec.Code != http.StatusOK {
		t.Fatalf("expected 200, got %d", rec.Code)
	}
}
