package web

import (
	"context"
	"fmt"
	"html/template"
	"io"
	"io/fs"
	"log"
	"net/url"
	"os"
	"path/filepath"
	"time"

	"github.com/fsnotify/fsnotify"

	fsutil "kbrd/fs"
)

// WebDir is the board subfolder that overrides the embedded web assets. It
// mirrors the embedded layout: templates/*.html shadow embedded templates of
// the same base name, and static/* overrides (or adds) static files. The
// ".kbrd_" prefix keeps it out of board/column/item discovery, the same
// convention as card templates' .kbrd_templates.
const WebDir = ".kbrd_web_templates"

// templateFuncs is the function map shared by every parse of the web
// templates (startup and every hot-reload).
func templateFuncs() template.FuncMap {
	return template.FuncMap{"pathesc": url.PathEscape}
}

// buildTemplates parses the embedded templates, then overlays any
// <board>/.kbrd_web_templates/templates/*.html on top: re-parsing a file by
// its base name redefines that associated template (and any {{define}} blocks
// it contains), so overrides shadow the embedded markup file-by-file. A
// missing override folder is not an error; a malformed override is.
func buildTemplates(boardPath string) (*template.Template, error) {
	tmpl, err := template.New("").Funcs(templateFuncs()).ParseFS(assets, "templates/*.html")
	if err != nil {
		return nil, fmt.Errorf("web: parse templates: %w", err)
	}
	if boardPath == "" { // embedded-only (e.g. the pre-clone splash)
		return tmpl, nil
	}
	overrides, err := filepath.Glob(filepath.Join(boardPath, WebDir, "templates", "*.html"))
	if err != nil {
		return nil, fmt.Errorf("web: glob template overrides: %w", err)
	}
	if len(overrides) > 0 {
		if tmpl, err = tmpl.ParseFiles(overrides...); err != nil {
			return nil, fmt.Errorf("web: parse template overrides: %w", err)
		}
	}
	return tmpl, nil
}

// overlayFS serves a file from primary when present, falling back to fallback.
// Used so on-disk static overrides win over embedded defaults while missing
// files transparently fall through.
type overlayFS struct {
	primary  fs.FS
	fallback fs.FS
}

func (o overlayFS) Open(name string) (fs.File, error) {
	if f, err := o.primary.Open(name); err == nil {
		return f, nil
	}
	return o.fallback.Open(name)
}

// staticFS returns the filesystem backing /static/: on-disk overrides under
// <board>/.kbrd_web_templates/static layered over the embedded assets. Because
// os.DirFS reads live, static overrides take effect without a restart, and a
// missing folder (e.g. before a --git-url clone) simply falls back to embedded.
// http.FileServerFS cleans request paths and os.DirFS rejects "..", so this is
// traversal-safe.
func staticFS(boardPath string) fs.FS {
	embedded, _ := fs.Sub(assets, "static")
	return overlayFS{
		primary:  os.DirFS(filepath.Join(boardPath, WebDir, "static")),
		fallback: embedded,
	}
}

// EjectAssets writes the embedded web assets into
// <boardPath>/.kbrd_web_templates so users have the defaults to customize from.
// It never overwrites an existing file (a re-run won't clobber edits): such
// files are returned in skipped instead. Paths in written/skipped are relative
// to the board directory.
func EjectAssets(boardPath string) (written, skipped []string, err error) {
	walkErr := fs.WalkDir(assets, ".", func(p string, d fs.DirEntry, err error) error {
		if err != nil {
			return err
		}
		if d.IsDir() {
			return nil
		}
		// p is like "templates/board.html" or "static/style.css".
		dst := filepath.Join(boardPath, WebDir, filepath.FromSlash(p))
		rel := filepath.Join(WebDir, filepath.FromSlash(p))
		if _, statErr := os.Stat(dst); statErr == nil {
			skipped = append(skipped, rel)
			return nil
		}
		if err := os.MkdirAll(filepath.Dir(dst), 0o755); err != nil {
			return err
		}
		src, err := assets.Open(p)
		if err != nil {
			return err
		}
		defer src.Close()
		out, err := os.Create(dst)
		if err != nil {
			return err
		}
		if _, err := io.Copy(out, src); err != nil {
			out.Close()
			return err
		}
		if err := out.Close(); err != nil {
			return err
		}
		written = append(written, rel)
		return nil
	})
	return written, skipped, walkErr
}

// templateReloadDebounce coalesces editor rename-saves and git-pull checkout
// churn into a single rebuild, mirroring configDebounce.
const templateReloadDebounce = 300 * time.Millisecond

// watchTemplates re-parses the template set whenever a *.html under the
// override folder changes, swapping it into store on success and keeping the
// last-good set on a parse error. It blocks until ctx is cancelled. A missing
// or unwatchable folder disables hot-reload silently (overrides still applied
// at startup); editing then requires a restart.
func (s *Server) watchTemplates(ctx context.Context) {
	dir := filepath.Join(s.opts.BoardPath, WebDir, "templates")
	if info, err := os.Stat(dir); err != nil || !info.IsDir() {
		return
	}
	w, err := fsutil.NewWatcher(nil)
	if err != nil {
		log.Printf("web: template hot-reload disabled: %v", err)
		return
	}
	if err := w.Add(dir); err != nil {
		log.Printf("web: not watching %s: %v", dir, err)
		w.Close()
		return
	}
	defer w.Close()

	var timer *time.Timer
	var fire <-chan time.Time
	for {
		select {
		case <-ctx.Done():
			if timer != nil {
				timer.Stop()
			}
			return
		case ev, ok := <-w.Events():
			if !ok {
				return
			}
			if filepath.Ext(ev.Name) != ".html" {
				continue
			}
			if ev.Op&(fsnotify.Create|fsnotify.Write|fsnotify.Rename|fsnotify.Remove) == 0 {
				continue
			}
			if timer != nil {
				timer.Stop()
			}
			timer = time.NewTimer(templateReloadDebounce)
			fire = timer.C
		case <-fire:
			timer, fire = nil, nil
			tmpl, err := buildTemplates(s.opts.BoardPath)
			if err != nil {
				log.Printf("web: template reload skipped: %v", err)
				continue
			}
			s.tmpl.Store(tmpl)
			log.Printf("web: templates reloaded")
		case err, ok := <-w.Errors():
			if !ok {
				return
			}
			log.Printf("web: template watcher: %v", err)
		}
	}
}
