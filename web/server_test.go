package web

import (
	"fmt"
	"net/http"
	"net/http/httptest"
	"net/url"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"
)

const testToken = "test-token-1234"

// newTestServer builds a Server over a temp board (no git → Syncer nil) and
// returns it with its full middleware-wrapped handler.
func newTestServer(t *testing.T) (*Server, http.Handler, string) {
	t.Helper()
	boardDir := t.TempDir()
	for col, items := range map[string][]string{"1. todo": {"task-a"}, "2. done": nil} {
		dir := filepath.Join(boardDir, col)
		os.MkdirAll(dir, 0o755)
		for _, it := range items {
			os.WriteFile(filepath.Join(dir, it+".md"), []byte("---\ntags: [x]\n---\n# Task A\nbody line\n"), 0o644)
		}
	}
	tmpl, err := buildTemplates(boardDir)
	if err != nil {
		t.Fatal(err)
	}
	s := &Server{
		opts: Options{BoardPath: boardDir, BoardName: "test", Token: testToken},
		auth: newAuth(testToken, false),
	}
	s.tmpl.Store(tmpl)
	s.ready.Store(true)
	return s, s.middleware(s.routes()), boardDir
}

// loginCookie performs a login and returns the session cookie.
func loginCookie(t *testing.T, h http.Handler) *http.Cookie {
	t.Helper()
	form := url.Values{"token": {testToken}}
	req := httptest.NewRequest("POST", "/login", strings.NewReader(form.Encode()))
	req.Header.Set("Content-Type", "application/x-www-form-urlencoded")
	rec := httptest.NewRecorder()
	h.ServeHTTP(rec, req)
	for _, c := range rec.Result().Cookies() {
		if c.Name == sessionCookie && c.Value != "" {
			return c
		}
	}
	t.Fatal("login did not set a session cookie")
	return nil
}

func get(h http.Handler, path string, c *http.Cookie) *httptest.ResponseRecorder {
	req := httptest.NewRequest("GET", path, nil)
	if c != nil {
		req.AddCookie(c)
	}
	rec := httptest.NewRecorder()
	h.ServeHTTP(rec, req)
	return rec
}

func post(h http.Handler, path string, form url.Values, c *http.Cookie) *httptest.ResponseRecorder {
	req := httptest.NewRequest("POST", path, strings.NewReader(form.Encode()))
	req.Header.Set("Content-Type", "application/x-www-form-urlencoded")
	if c != nil {
		req.AddCookie(c)
	}
	rec := httptest.NewRecorder()
	h.ServeHTTP(rec, req)
	return rec
}

func TestAuthGate(t *testing.T) {
	_, h, _ := newTestServer(t)

	if rec := get(h, "/", nil); rec.Code != http.StatusSeeOther || rec.Header().Get("Location") != "/login" {
		t.Fatalf("unauthenticated / : %d -> %q", rec.Code, rec.Header().Get("Location"))
	}
	// Static and healthz bypass auth.
	if rec := get(h, "/static/style.css", nil); rec.Code != http.StatusOK {
		t.Fatalf("static: %d", rec.Code)
	}
	if rec := get(h, "/healthz", nil); rec.Code != http.StatusOK {
		t.Fatalf("healthz: %d", rec.Code)
	}

	// Valid login first — a failed attempt would trip the per-IP backoff for
	// the shared httptest client address (covered by TestLoginRateLimit).
	c := loginCookie(t, h)

	// Wrong token rejected.
	rec := post(h, "/login", url.Values{"token": {"wrong"}}, nil)
	if rec.Code != http.StatusUnauthorized {
		t.Fatalf("bad login: %d", rec.Code)
	}
	if rec := get(h, "/", c); rec.Code != http.StatusOK || !strings.Contains(rec.Body.String(), "1. todo") {
		t.Fatalf("authed board: %d", rec.Code)
	}

	// Forged cookie rejected.
	forged := &http.Cookie{Name: sessionCookie, Value: strings.Repeat("ab", 32)}
	if rec := get(h, "/", forged); rec.Code != http.StatusSeeOther {
		t.Fatalf("forged cookie accepted: %d", rec.Code)
	}
}

func TestSecurityHeaders(t *testing.T) {
	_, h, _ := newTestServer(t)
	rec := get(h, "/healthz", nil)
	for header, want := range map[string]string{
		"Content-Security-Policy": "default-src 'self'; frame-ancestors 'none'",
		"X-Content-Type-Options":  "nosniff",
		"Referrer-Policy":         "no-referrer",
	} {
		if got := rec.Header().Get(header); got != want {
			t.Errorf("%s = %q, want %q", header, got, want)
		}
	}
	if rec.Header().Get("Strict-Transport-Security") != "" {
		t.Error("HSTS set without TLS")
	}
}

func TestReadinessGate(t *testing.T) {
	s, h, _ := newTestServer(t)
	s.ready.Store(false)
	s.initStatus.Store("cloning…")

	rec := get(h, "/", nil)
	if rec.Code != http.StatusServiceUnavailable {
		t.Fatalf("not-ready / : %d", rec.Code)
	}
	if !strings.Contains(rec.Body.String(), "initializing") {
		t.Fatal("splash page missing")
	}
	if rec := get(h, "/healthz", nil); rec.Code != http.StatusServiceUnavailable {
		t.Fatalf("not-ready healthz: %d", rec.Code)
	}
}

func TestBoardAndColumn(t *testing.T) {
	_, h, _ := newTestServer(t)
	c := loginCookie(t, h)

	body := get(h, "/", c).Body.String()
	for _, want := range []string{"Task A", "#x", "body line", "2. done"} {
		if !strings.Contains(body, want) {
			t.Errorf("board missing %q", want)
		}
	}

	rec := get(h, "/c/"+url.PathEscape("1. todo"), c)
	if rec.Code != http.StatusOK || !strings.Contains(rec.Body.String(), "Task A") {
		t.Fatalf("column fragment: %d", rec.Code)
	}

	if rec := get(h, "/c/nope", c); rec.Code != http.StatusNotFound {
		t.Fatalf("unknown column: %d", rec.Code)
	}
}

func TestCreateEditDelete(t *testing.T) {
	_, h, boardDir := newTestServer(t)
	c := loginCookie(t, h)
	col := url.PathEscape("1. todo")

	// Create.
	rec := post(h, "/c/"+col+"/cards", url.Values{"name": {"new-card"}, "content": {"hello"}}, c)
	if rec.Code != http.StatusSeeOther {
		t.Fatalf("create: %d %s", rec.Code, rec.Body.String())
	}
	path := filepath.Join(boardDir, "1. todo", "new-card.md")
	if data, err := os.ReadFile(path); err != nil || string(data) != "hello\n" {
		t.Fatalf("created file: %q, %v", data, err)
	}

	// Duplicate create re-renders the form with an error.
	rec = post(h, "/c/"+col+"/cards", url.Values{"name": {"new-card"}, "content": {""}}, c)
	if !strings.Contains(rec.Body.String(), "already exists") {
		t.Fatal("duplicate create not rejected")
	}

	// Path traversal rejected.
	rec = post(h, "/c/"+col+"/cards", url.Values{"name": {"../escape"}}, c)
	if _, err := os.Stat(filepath.Join(boardDir, "escape.md")); err == nil {
		t.Fatal("traversal escaped the column")
	}
	if !strings.Contains(rec.Body.String(), "Invalid name") {
		t.Fatalf("traversal create: %d", rec.Code)
	}

	// Edit round-trip with the stale-edit guard.
	editRec := get(h, "/c/"+col+"/i/new-card", c)
	hash := extractHash(t, editRec.Body.String())
	rec = post(h, "/c/"+col+"/i/new-card", url.Values{"content": {"updated"}, "hash": {hash}}, c)
	if rec.Code != http.StatusSeeOther {
		t.Fatalf("save: %d %s", rec.Code, rec.Body.String())
	}
	if data, _ := os.ReadFile(path); string(data) != "updated\n" {
		t.Fatalf("saved content: %q", data)
	}

	// Stale hash rejected, user text preserved.
	rec = post(h, "/c/"+col+"/i/new-card", url.Values{"content": {"mine"}, "hash": {hash}}, c)
	if rec.Code == http.StatusSeeOther {
		t.Fatal("stale edit accepted")
	}
	if body := rec.Body.String(); !strings.Contains(body, "changed while you were editing") || !strings.Contains(body, "mine") {
		t.Fatal("stale-edit error page wrong")
	}
	if data, _ := os.ReadFile(path); string(data) != "updated\n" {
		t.Fatal("stale edit overwrote the file")
	}

	// htmx save gets HX-Redirect instead of 303.
	editRec = get(h, "/c/"+col+"/i/new-card", c)
	hash = extractHash(t, editRec.Body.String())
	req := httptest.NewRequest("POST", "/c/"+col+"/i/new-card", strings.NewReader(url.Values{"content": {"v3"}, "hash": {hash}}.Encode()))
	req.Header.Set("Content-Type", "application/x-www-form-urlencoded")
	req.Header.Set("HX-Request", "true")
	req.AddCookie(c)
	hxRec := httptest.NewRecorder()
	h.ServeHTTP(hxRec, req)
	if hxRec.Header().Get("HX-Redirect") == "" {
		t.Fatal("htmx save missing HX-Redirect")
	}

	// Delete.
	rec = post(h, "/c/"+col+"/i/new-card/delete", nil, c)
	if rec.Code != http.StatusSeeOther {
		t.Fatalf("delete: %d", rec.Code)
	}
	if _, err := os.Stat(path); !os.IsNotExist(err) {
		t.Fatal("file survived delete")
	}
	if rec := post(h, "/c/"+col+"/i/new-card/delete", nil, c); rec.Code != http.StatusNotFound {
		t.Fatalf("double delete: %d", rec.Code)
	}
}

func TestQuickFilter(t *testing.T) {
	_, h, boardDir := newTestServer(t)
	// Second card: tag zeta, unique word "needle" on the 4th body line —
	// beyond previewLines, so a hit proves full-body matching.
	content := "---\ntags: [zeta]\n---\n# Task B\nline one\nline two\nline three\nthe needle word\n"
	os.WriteFile(filepath.Join(boardDir, "1. todo", "task-b.md"), []byte(content), 0o644)
	c := loginCookie(t, h)

	// Full-body match, case-insensitive; non-matching card filtered out.
	for _, q := range []string{"needle", "NEEDLE"} {
		body := get(h, "/?q="+q, c).Body.String()
		if !strings.Contains(body, "Task B") || strings.Contains(body, "Task A") {
			t.Errorf("q=%s: wrong cards on board", q)
		}
		if !strings.Contains(body, `<span class="count">1</span>`) {
			t.Errorf("q=%s: count not updated", q)
		}
	}

	// Tag match.
	if body := get(h, "/?q=zeta", c).Body.String(); !strings.Contains(body, "Task B") || strings.Contains(body, "Task A") {
		t.Error("tag match failed")
	}

	// Empty query: everything visible.
	if body := get(h, "/?q=", c).Body.String(); !strings.Contains(body, "Task A") || !strings.Contains(body, "Task B") {
		t.Error("empty query filtered cards")
	}

	// htmx filter request gets just the columns fragment.
	req := httptest.NewRequest("GET", "/?q=needle", nil)
	req.Header.Set("HX-Request", "true")
	req.AddCookie(c)
	rec := httptest.NewRecorder()
	h.ServeHTTP(rec, req)
	if body := rec.Body.String(); !strings.Contains(body, `id="columns"`) || strings.Contains(body, "topbar") {
		t.Error("htmx request did not get a bare columns fragment")
	}

	// Boosted navigation still gets the full page.
	req = httptest.NewRequest("GET", "/", nil)
	req.Header.Set("HX-Request", "true")
	req.Header.Set("HX-Boosted", "true")
	req.AddCookie(c)
	rec = httptest.NewRecorder()
	h.ServeHTTP(rec, req)
	if !strings.Contains(rec.Body.String(), "topbar") {
		t.Error("boosted request lost the full page")
	}

	// Query echo is escaped.
	if body := get(h, "/?q=%3Cscript%3E", c).Body.String(); strings.Contains(body, "<script>alert") || !strings.Contains(body, "&lt;script&gt;") {
		t.Error("query echo not escaped")
	}

	// Column fragment endpoint honors q.
	if body := get(h, "/c/"+url.PathEscape("1. todo")+"?q=needle", c).Body.String(); !strings.Contains(body, "Task B") || strings.Contains(body, "Task A") {
		t.Error("column fragment filter failed")
	}
}

// extractHash pulls the stale-edit guard value out of the edit form.
func extractHash(t *testing.T, body string) string {
	t.Helper()
	_, rest, found := strings.Cut(body, `name="hash" value="`)
	if !found {
		t.Fatal("edit form has no hash field")
	}
	hash, _, _ := strings.Cut(rest, `"`)
	return hash
}

func TestLoginRateLimit(t *testing.T) {
	_, h, _ := newTestServer(t)

	// First failure triggers backoff; the immediate retry with the RIGHT
	// token must still be rejected (limiter, not comparison).
	post(h, "/login", url.Values{"token": {"wrong"}}, nil)
	rec := post(h, "/login", url.Values{"token": {testToken}}, nil)
	if rec.Code != http.StatusUnauthorized {
		t.Fatalf("rate limiter not applied: %d", rec.Code)
	}
}

func TestApplyConfig_BoardName(t *testing.T) {
	s, _, _ := newTestServer(t)

	if got := s.currentBoardName(); got != "test" {
		t.Fatalf("initial board name: got %q want test", got)
	}
	s.applyConfig(t.Context(), ReloadableConfig{BoardName: "renamed"})
	if got := s.currentBoardName(); got != "renamed" {
		t.Fatalf("board name after apply: got %q want renamed", got)
	}
	// Empty reloaded name falls back to the startup label.
	s.applyConfig(t.Context(), ReloadableConfig{})
	if got := s.currentBoardName(); got != "test" {
		t.Fatalf("board name after empty apply: got %q want test", got)
	}
}

func TestRestartPullLoop_Bookkeeping(t *testing.T) {
	s, _, _ := newTestServer(t) // Syncer nil: loops never start, bookkeeping still runs
	ctx := t.Context()

	s.restartPullLoop(ctx, 0)
	if s.pullEvery != 0 || s.pullCancel != nil {
		t.Fatalf("after 0: every=%v cancel=%v", s.pullEvery, s.pullCancel != nil)
	}
	s.restartPullLoop(ctx, 5*time.Second)
	if s.pullEvery != 5*time.Second {
		t.Fatalf("after 5s: every=%v", s.pullEvery)
	}
	if s.pullCancel != nil {
		t.Fatal("nil Syncer must not leave a cancel func behind")
	}
	s.restartPullLoop(ctx, 0)
	if s.pullEvery != 0 {
		t.Fatalf("back to 0: every=%v", s.pullEvery)
	}
}

func TestConfigEditor(t *testing.T) {
	s, h, boardDir := newTestServer(t)
	cfgPath := filepath.Join(boardDir, "kbrd.toml")
	s.opts.ConfigFile = cfgPath
	validateErr := error(nil)
	s.opts.ValidateConfig = func([]byte) error { return validateErr }
	c := loginCookie(t, h)

	post := func(content, hash string) *httptest.ResponseRecorder {
		form := url.Values{"content": {content}, "hash": {hash}}
		req := httptest.NewRequest("POST", "/config", strings.NewReader(form.Encode()))
		req.Header.Set("Content-Type", "application/x-www-form-urlencoded")
		req.AddCookie(c)
		rec := httptest.NewRecorder()
		h.ServeHTTP(rec, req)
		return rec
	}

	// Empty editor for a missing kbrd.toml.
	rec := get(h, "/config", c)
	if rec.Code != http.StatusOK {
		t.Fatalf("GET /config: %d", rec.Code)
	}

	// Valid save creates the file (hash of empty current content).
	emptyHash := contentHash("")
	if rec = post("[board]\nname = \"x\"\n", emptyHash); rec.Code != http.StatusSeeOther {
		t.Fatalf("valid save: got %d want 303 (%s)", rec.Code, rec.Body.String())
	}
	saved, err := os.ReadFile(cfgPath)
	if err != nil || string(saved) != "[board]\nname = \"x\"\n" {
		t.Fatalf("saved content: %q err=%v", saved, err)
	}

	// Stale hash is rejected and the file stays untouched.
	if rec = post("[board]\nname = \"y\"\n", emptyHash); rec.Code != http.StatusOK || !strings.Contains(rec.Body.String(), "changed while you were editing") {
		t.Fatalf("stale save: got %d body %q", rec.Code, rec.Body.String())
	}

	// Validation failure is rejected, file stays untouched, text preserved.
	validateErr = fmt.Errorf("serve.token cannot be set in kbrd.toml")
	curHash := contentHash(string(saved))
	rec = post("[serve]\ntoken = \"x\"\n", curHash)
	if rec.Code != http.StatusOK || !strings.Contains(rec.Body.String(), "serve.token cannot be set") {
		t.Fatalf("invalid save: got %d body %q", rec.Code, rec.Body.String())
	}
	if !strings.Contains(rec.Body.String(), "token = ") {
		t.Fatal("invalid save must preserve the user's text in the form")
	}
	after, _ := os.ReadFile(cfgPath)
	if string(after) != string(saved) {
		t.Fatalf("file changed by rejected save: %q", after)
	}

	// CRLF from the textarea is normalized before validation and write.
	validateErr = nil
	if rec = post("[board]\r\nname = \"z\"", curHash); rec.Code != http.StatusSeeOther {
		t.Fatalf("crlf save: got %d (%s)", rec.Code, rec.Body.String())
	}
	after, _ = os.ReadFile(cfgPath)
	if string(after) != "[board]\nname = \"z\"\n" {
		t.Fatalf("crlf normalization: got %q", after)
	}
}

func TestConfigEditor_DisabledWithoutConfigFile(t *testing.T) {
	_, h, _ := newTestServer(t)
	c := loginCookie(t, h)
	if rec := get(h, "/config", c); rec.Code != http.StatusNotFound {
		t.Fatalf("GET /config without ConfigFile: got %d want 404", rec.Code)
	}
}
