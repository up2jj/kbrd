package script

import (
	"context"
	"crypto/sha256"
	"encoding/hex"
	"errors"
	"fmt"
	"io"
	"net"
	"net/http"
	"net/url"
	"os"
	"path/filepath"
	"sort"
	"strings"
	"time"

	lua "github.com/yuin/gopher-lua"

	"kbrd/config"
)

const (
	// remoteCacheSubdir is the directory under the cache root where fetched
	// remote modules live. Filenames are sha256(resolvedURL) so a URL maps to a
	// stable file; an adjacent <key>.url sidecar records the original require
	// name so `kbrd cache script list` can show it.
	remoteCacheSubdir = "remote-scripts"

	// remoteFetchTimeout bounds a single fetch. The first require of a URL is
	// synchronous during init-script load, so this also bounds worst-case
	// startup delay; every later start hits the cache and is instant.
	remoteFetchTimeout = 15 * time.Second

	// remoteMaxBytes caps a fetched module. A script file is small; this keeps a
	// misconfigured or hostile URL from streaming an unbounded body into memory.
	remoteMaxBytes = 4 << 20 // 4 MiB

	rawGitHubBase = "https://raw.githubusercontent.com"
)

// CachedScript describes one entry in the remote-script cache, for the
// `kbrd cache script list` command.
type CachedScript struct {
	URL   string // original require name (from the .url sidecar)
	Path  string // absolute path of the cached source file
	Bytes int64  // size of the cached source
}

// remoteCacheDir returns the directory holding cached remote modules. It honors
// the KBRD_CACHE_DIR override (used by tests and as a user escape hatch) and
// otherwise nests under the OS user cache dir. The directory is not created
// here — fetchRemote creates it lazily on the first successful write.
func remoteCacheDir() (string, error) {
	if override := os.Getenv("KBRD_CACHE_DIR"); override != "" {
		return filepath.Join(override, remoteCacheSubdir), nil
	}
	base, err := os.UserCacheDir()
	if err != nil {
		return "", err
	}
	return filepath.Join(base, config.AppDirName, remoteCacheSubdir), nil
}

// resolveRemoteURL turns a require name into the HTTPS URL to fetch. It expands
// the github:owner/repo/path@ref shorthand to a raw.githubusercontent.com URL.
// Plain https:// URLs pass through. http:// is accepted only for loopback hosts
// so tests and local development servers can work without weakening normal use.
func resolveRemoteURL(name string) (string, error) {
	if spec, ok := strings.CutPrefix(name, "github:"); ok {
		ref := "HEAD"
		if at := strings.LastIndex(spec, "@"); at != -1 {
			ref = spec[at+1:]
			spec = spec[:at]
			if ref == "" {
				return "", fmt.Errorf("github require %q: empty ref after @", name)
			}
		}
		// spec is now owner/repo/path/to/file.lua
		parts := strings.SplitN(spec, "/", 3)
		if len(parts) < 3 || parts[0] == "" || parts[1] == "" || parts[2] == "" {
			return "", fmt.Errorf("github require %q: want github:owner/repo/path@ref", name)
		}
		return fmt.Sprintf("%s/%s/%s/%s/%s", rawGitHubBase, parts[0], parts[1], ref, parts[2]), nil
	}
	if strings.HasPrefix(name, "https://") {
		return name, nil
	}
	if strings.HasPrefix(name, "http://") && isLoopbackHTTPURL(name) {
		return name, nil
	}
	return "", fmt.Errorf("unsupported remote require %q: expected https://, loopback http://, or github:owner/repo/path@ref", name)
}

func isLoopbackHTTPURL(raw string) bool {
	u, err := url.Parse(raw)
	if err != nil || u.Scheme != "http" {
		return false
	}
	host := u.Hostname()
	if host == "localhost" {
		return true
	}
	ip := net.ParseIP(host)
	return ip != nil && ip.IsLoopback()
}

func checkRemoteRedirect(req *http.Request, via []*http.Request) error {
	if len(via) >= 10 {
		return errors.New("stopped after 10 redirects")
	}
	if req.URL.Scheme == "https" {
		return nil
	}
	if req.URL.Scheme != "http" || !isLoopbackHTTPURL(req.URL.String()) {
		return fmt.Errorf("redirect target %q must use HTTPS or loopback HTTP", req.URL.Redacted())
	}
	if len(via) > 0 && via[len(via)-1].URL.Scheme == "https" {
		return fmt.Errorf("refusing HTTPS downgrade redirect to %q", req.URL.Redacted())
	}
	return nil
}

// cacheKey is the cache filename (without extension) for a resolved URL.
func cacheKey(resolvedURL string) string {
	sum := sha256.Sum256([]byte(resolvedURL))
	return hex.EncodeToString(sum[:])
}

// fetchRemote resolves a require name, returns the cached source if present, and
// otherwise fetches it over HTTP and caches it. Caching is success-only: a
// network error or non-200 response writes nothing, so a transient failure
// cannot poison the purge-only cache. Requires cfg.RemoteRequire — when off it
// returns an error rather than reaching the network.
func (h *Host) fetchRemote(ctx context.Context, name string) (string, error) {
	if !h.cfg.RemoteRequire {
		return "", errors.New("remote require is disabled (set scripting.remote_require = true to enable)")
	}

	url, err := resolveRemoteURL(name)
	if err != nil {
		return "", err
	}

	dir, err := remoteCacheDir()
	if err != nil {
		return "", fmt.Errorf("locate cache dir: %w", err)
	}
	srcPath := filepath.Join(dir, cacheKey(url)+".lua")

	if data, err := os.ReadFile(srcPath); err == nil {
		return string(data), nil
	}

	body, err := httpGet(ctx, url)
	if err != nil {
		return "", err
	}

	// Cache best-effort: a write failure shouldn't fail the require, since we
	// already have the source in hand. Next load just re-fetches.
	if mkErr := os.MkdirAll(dir, 0o755); mkErr == nil {
		_ = writeFileAtomic(srcPath, []byte(body), 0o644)
		_ = writeFileAtomic(filepath.Join(dir, cacheKey(url)+".url"), []byte(name), 0o644)
	}
	return body, nil
}

func writeFileAtomic(path string, data []byte, perm os.FileMode) error {
	tmp, err := os.CreateTemp(filepath.Dir(path), "."+filepath.Base(path)+".tmp-*")
	if err != nil {
		return err
	}
	tmpName := tmp.Name()
	defer os.Remove(tmpName)
	if _, err := tmp.Write(data); err != nil {
		_ = tmp.Close()
		return err
	}
	if err := tmp.Chmod(perm); err != nil {
		_ = tmp.Close()
		return err
	}
	if err := tmp.Close(); err != nil {
		return err
	}
	return os.Rename(tmpName, path)
}

// httpGet fetches url with a bounded timeout and a size cap, returning the body
// only on a 2xx response.
func httpGet(ctx context.Context, url string) (string, error) {
	client := &http.Client{
		Timeout:       remoteFetchTimeout,
		CheckRedirect: checkRemoteRedirect,
	}
	req, err := http.NewRequestWithContext(ctx, http.MethodGet, url, nil)
	if err != nil {
		return "", fmt.Errorf("build request for %s: %w", url, err)
	}
	resp, err := client.Do(req)
	if err != nil {
		return "", fmt.Errorf("fetch %s: %w", url, err)
	}
	defer resp.Body.Close()
	if resp.StatusCode < 200 || resp.StatusCode >= 300 {
		return "", fmt.Errorf("fetch %s: status %s", url, resp.Status)
	}
	data, err := io.ReadAll(io.LimitReader(resp.Body, remoteMaxBytes+1))
	if err != nil {
		return "", fmt.Errorf("read %s: %w", url, err)
	}
	if int64(len(data)) > remoteMaxBytes {
		return "", fmt.Errorf("fetch %s: exceeds %d-byte limit", url, remoteMaxBytes)
	}
	return string(data), nil
}

// luaRemoteFetch backs kbrd._remoteFetch(name); the Lua searcher in installAPI
// calls it and compiles the returned source. Returns (source) on success or
// (nil, errmsg) following the package's error-tuple convention.
func (h *Host) luaRemoteFetch(L *lua.LState) int {
	name := L.CheckString(1)
	ctx := L.Context()
	if ctx == nil {
		ctx = context.Background()
	}
	src, err := h.fetchRemote(ctx, name)
	if err != nil {
		return errResult(L, err)
	}
	L.Push(lua.LString(src))
	return 1
}

// PurgeRemoteCache removes the entire remote-script cache and returns the number
// of cached modules (.lua files) that were removed. A missing cache dir is not
// an error — it just means nothing was cached.
func PurgeRemoteCache() (int, error) {
	dir, err := remoteCacheDir()
	if err != nil {
		return 0, err
	}
	entries, err := os.ReadDir(dir)
	if err != nil {
		if os.IsNotExist(err) {
			return 0, nil
		}
		return 0, err
	}
	count := 0
	for _, e := range entries {
		if strings.HasSuffix(e.Name(), ".lua") {
			count++
		}
	}
	if err := os.RemoveAll(dir); err != nil {
		return 0, err
	}
	return count, nil
}

// ListRemoteCache returns the cached remote modules, sorted by original URL. A
// missing cache dir yields an empty list, not an error.
func ListRemoteCache() ([]CachedScript, error) {
	dir, err := remoteCacheDir()
	if err != nil {
		return nil, err
	}
	entries, err := os.ReadDir(dir)
	if err != nil {
		if os.IsNotExist(err) {
			return nil, nil
		}
		return nil, err
	}
	var out []CachedScript
	for _, e := range entries {
		if !strings.HasSuffix(e.Name(), ".lua") {
			continue
		}
		key := strings.TrimSuffix(e.Name(), ".lua")
		srcPath := filepath.Join(dir, e.Name())
		cs := CachedScript{URL: "(unknown)", Path: srcPath}
		if info, err := e.Info(); err == nil {
			cs.Bytes = info.Size()
		}
		if url, err := os.ReadFile(filepath.Join(dir, key+".url")); err == nil {
			cs.URL = strings.TrimSpace(string(url))
		}
		out = append(out, cs)
	}
	sort.Slice(out, func(i, j int) bool { return out[i].URL < out[j].URL })
	return out, nil
}
