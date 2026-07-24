package script

import (
	"fmt"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"strings"
	"sync/atomic"
	"testing"
	"time"

	"kbrd/config"
)

// remoteCfg is defaultCfg with remote require turned on.
func remoteCfg() (cfg config.ScriptingConfig) {
	cfg = defaultCfg()
	cfg.RemoteRequire = true
	return cfg
}

// remoteServer serves a single Lua module at /mod.lua and counts requests.
func remoteServer(t *testing.T, body string) (url string, hits *atomic.Int32) {
	t.Helper()
	hits = &atomic.Int32{}
	mux := http.NewServeMux()
	mux.HandleFunc("/mod.lua", func(w http.ResponseWriter, r *http.Request) {
		hits.Add(1)
		fmt.Fprint(w, body)
	})
	ts := httptest.NewServer(mux)
	t.Cleanup(ts.Close)
	return ts.URL, hits
}

// loadRemote points the cache at a throwaway dir, writes a .kbrd.lua with the
// given body, and loads it through New.
func loadRemote(t *testing.T, cfg config.ScriptingConfig, kbrdLua string) (*Host, error) {
	t.Helper()
	t.Setenv("KBRD_CACHE_DIR", t.TempDir())
	dir := writeInit(t, kbrdLua)
	return New(cfg, &fakeAPI{}, nil, dir, "")
}

// TestRemoteRequireSideEffectAndReturn proves a remote module runs in the same
// VM (it calls kbrd) and that its return value reaches the caller.
func TestRemoteRequireSideEffectAndReturn(t *testing.T) {
	url, hits := remoteServer(t, `
notified = kbrd.notify ~= nil
return { add = function(a, b) return a + b end }
`)
	h, err := loadRemote(t, remoteCfg(), fmt.Sprintf(`
local m = require(%q)
sum = m.add(2, 3)
saw_kbrd = m ~= nil and notified
`, url+"/mod.lua"))
	if err != nil {
		t.Fatalf("load: %v", err)
	}
	defer h.Close()

	if got := luaNumber(t, h, "sum"); got != 5 {
		t.Errorf("sum = %v, want 5", got)
	}
	if !luaBool(t, h, "saw_kbrd") {
		t.Error("remote module did not see the kbrd global")
	}
	if got := hits.Load(); got != 1 {
		t.Errorf("server hit %d times, want 1", got)
	}
}

// TestRemoteRequireCachesAndDedupes verifies the same URL is fetched once
// (package.loaded memoization) and that a cache file is written.
func TestRemoteRequireCachesAndDedupes(t *testing.T) {
	url, hits := remoteServer(t, `return { v = 1 }`)
	cacheRoot := t.TempDir()
	t.Setenv("KBRD_CACHE_DIR", cacheRoot)
	dir := writeInit(t, fmt.Sprintf(`
local a = require(%[1]q)
local b = require(%[1]q)
same = a == b
`, url+"/mod.lua"))
	h, err := New(remoteCfg(), &fakeAPI{}, nil, dir, "")
	if err != nil {
		t.Fatalf("load: %v", err)
	}
	defer h.Close()

	if !luaBool(t, h, "same") {
		t.Error("two requires of the same URL returned different tables")
	}
	if got := hits.Load(); got != 1 {
		t.Errorf("server hit %d times, want 1 (memoization)", got)
	}
	if n := countCachedLua(t, cacheRoot); n != 1 {
		t.Errorf("cached .lua files = %d, want 1", n)
	}
	if n := countCachedExt(t, cacheRoot, ".url"); n != 1 {
		t.Errorf("cached .url sidecars = %d, want 1", n)
	}
}

// TestRemoteRequirePurgeRefetches confirms purge empties the cache and forces a
// fresh fetch on the next (fresh-VM) load.
func TestRemoteRequirePurgeRefetches(t *testing.T) {
	url, hits := remoteServer(t, `return { v = 1 }`)
	cacheRoot := t.TempDir()
	t.Setenv("KBRD_CACHE_DIR", cacheRoot)
	dir := writeInit(t, fmt.Sprintf("require(%q)\n", url+"/mod.lua"))

	h1, err := New(remoteCfg(), &fakeAPI{}, nil, dir, "")
	if err != nil {
		t.Fatalf("first load: %v", err)
	}
	h1.Close()
	if got := hits.Load(); got != 1 {
		t.Fatalf("after first load: hits = %d, want 1", got)
	}

	n, err := PurgeRemoteCache()
	if err != nil {
		t.Fatalf("purge: %v", err)
	}
	if n < 1 {
		t.Errorf("purge removed %d, want >= 1", n)
	}
	if got := countCachedLua(t, cacheRoot); got != 0 {
		t.Errorf("cache not empty after purge: %d files", got)
	}

	// A fresh VM (no package.loaded carryover) with an empty cache re-fetches.
	h2, err := New(remoteCfg(), &fakeAPI{}, nil, dir, "")
	if err != nil {
		t.Fatalf("second load: %v", err)
	}
	defer h2.Close()
	if got := hits.Load(); got != 2 {
		t.Errorf("after re-load: hits = %d, want 2 (purge should force a re-fetch)", got)
	}
}

// TestRemoteRequireDisabled checks the opt-in gate: with RemoteRequire off the
// require fails (surfaced via New's firstErr) and nothing hits the network.
func TestRemoteRequireDisabled(t *testing.T) {
	url, hits := remoteServer(t, `return {}`)
	cfg := defaultCfg() // RemoteRequire stays false
	h, err := loadRemote(t, cfg, fmt.Sprintf("require(%q)\n", url+"/mod.lua"))
	if h != nil {
		defer h.Close()
	}
	if err == nil {
		t.Fatal("expected an error when remote require is disabled")
	}
	if !strings.Contains(err.Error(), "disabled") {
		t.Errorf("error = %v, want it to mention 'disabled'", err)
	}
	if got := hits.Load(); got != 0 {
		t.Errorf("server hit %d times while disabled, want 0", got)
	}
}

// TestRemoteRequire404 confirms a non-200 fetch surfaces a Lua error and caches
// nothing.
func TestRemoteRequire404(t *testing.T) {
	ts := httptest.NewServer(http.NotFoundHandler())
	t.Cleanup(ts.Close)
	cacheRoot := t.TempDir()
	t.Setenv("KBRD_CACHE_DIR", cacheRoot)
	dir := writeInit(t, fmt.Sprintf("require(%q)\n", ts.URL+"/missing.lua"))

	h, err := New(remoteCfg(), &fakeAPI{}, nil, dir, "")
	if h != nil {
		defer h.Close()
	}
	if err == nil {
		t.Fatal("expected an error for a 404 remote module")
	}
	if got := countCachedLua(t, cacheRoot); got != 0 {
		t.Errorf("a failed fetch cached %d files, want 0", got)
	}
}

func TestRemoteRequireHonorsInitTimeout(t *testing.T) {
	started := make(chan struct{})
	ts := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		close(started)
		<-r.Context().Done()
	}))
	t.Cleanup(ts.Close)
	cfg := remoteCfg()
	cfg.InitTimeoutMs = 50
	dir := writeInit(t, fmt.Sprintf("require(%q)\n", ts.URL+"/mod.lua"))

	begin := time.Now()
	h, err := New(cfg, &fakeAPI{}, nil, dir, "")
	if h != nil {
		defer h.Close()
	}
	if err == nil {
		t.Fatal("expected initialization timeout")
	}
	if elapsed := time.Since(begin); elapsed > time.Second {
		t.Fatalf("remote initialization timeout took %v", elapsed)
	}
	select {
	case <-started:
	default:
		t.Fatal("remote request never started")
	}
}

func TestRemoteRequireRejectsNonLoopbackHTTP(t *testing.T) {
	h, err := loadRemote(t, remoteCfg(), `require("http://example.com/mod.lua")`)
	if h != nil {
		defer h.Close()
	}
	if err == nil {
		t.Fatal("expected an error for non-loopback http URL")
	}
	if !strings.Contains(err.Error(), "loopback http") {
		t.Errorf("error = %v, want it to mention loopback http", err)
	}
}

// TestResolveRemoteURL covers the github: shorthand and pass-through, with no
// network involved.
func TestResolveRemoteURL(t *testing.T) {
	cases := []struct{ in, want string }{
		{"github:o/r/p.lua@v1", "https://raw.githubusercontent.com/o/r/v1/p.lua"},
		{"github:o/r/dir/p.lua@abc123", "https://raw.githubusercontent.com/o/r/abc123/dir/p.lua"},
		{"github:o/r/p.lua", "https://raw.githubusercontent.com/o/r/HEAD/p.lua"},
		{"https://example.com/x.lua", "https://example.com/x.lua"},
	}
	for _, c := range cases {
		got, err := resolveRemoteURL(c.in)
		if err != nil {
			t.Errorf("resolveRemoteURL(%q) error: %v", c.in, err)
			continue
		}
		if got != c.want {
			t.Errorf("resolveRemoteURL(%q) = %q, want %q", c.in, got, c.want)
		}
	}

	for _, bad := range []string{"ftp://x", "github:o/r", "github:o/r/p.lua@", "plain"} {
		if _, err := resolveRemoteURL(bad); err == nil {
			t.Errorf("resolveRemoteURL(%q): expected error", bad)
		}
	}
}

// countCachedLua counts cached module files under a KBRD_CACHE_DIR root.
func countCachedLua(t *testing.T, cacheRoot string) int {
	return countCachedExt(t, cacheRoot, ".lua")
}

func countCachedExt(t *testing.T, cacheRoot, ext string) int {
	t.Helper()
	entries, err := os.ReadDir(filepath.Join(cacheRoot, remoteCacheSubdir))
	if err != nil {
		if os.IsNotExist(err) {
			return 0
		}
		t.Fatalf("read cache dir: %v", err)
	}
	n := 0
	for _, e := range entries {
		if strings.HasSuffix(e.Name(), ext) {
			n++
		}
	}
	return n
}
