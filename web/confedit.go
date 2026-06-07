package web

import (
	"net/http"
	"os"
	"strings"
)

// Config editor: a token-authed page for editing the board's kbrd.toml from
// the web UI. Saves are validated before they touch disk (ValidateConfig) so
// a typo cannot break the running server, guarded against concurrent changes
// with the same content-hash scheme the card editor uses, and committed like
// any other board mutation. The ConfigWatcher picks the write up and
// hot-applies it — no extra wiring here.

// readConfigFile returns the current kbrd.toml content; a missing file is an
// empty editor, not an error.
func (s *Server) readConfigFile() (string, error) {
	data, err := os.ReadFile(s.opts.ConfigFile)
	if err != nil {
		if os.IsNotExist(err) {
			return "", nil
		}
		return "", err
	}
	return string(data), nil
}

func (s *Server) handleConfigForm(w http.ResponseWriter, r *http.Request) {
	if s.opts.ConfigFile == "" {
		http.NotFound(w, r)
		return
	}
	content, err := s.readConfigFile()
	if err != nil {
		http.Error(w, "failed to read config", http.StatusInternalServerError)
		return
	}
	s.render(w, "config.html", s.page(map[string]any{
		"Content": content, "Hash": contentHash(content),
	}))
}

func (s *Server) handleConfigSave(w http.ResponseWriter, r *http.Request) {
	if s.opts.ConfigFile == "" {
		http.NotFound(w, r)
		return
	}
	// Normalize the textarea's CRLF so the committed TOML diffs cleanly.
	content := strings.ReplaceAll(r.FormValue("content"), "\r\n", "\n")
	if content != "" && !strings.HasSuffix(content, "\n") {
		content += "\n"
	}

	renderErr := func(msg, hash string) {
		s.render(w, "config.html", s.page(map[string]any{
			"Content": content, "Hash": hash, "Error": msg,
		}))
	}

	// Stale-edit guard: reject when the file changed since the form loaded
	// (e.g. a background pull brought a newer version).
	current, err := s.readConfigFile()
	if err != nil {
		http.Error(w, "failed to read config", http.StatusInternalServerError)
		return
	}
	if r.FormValue("hash") != contentHash(current) {
		renderErr("The config changed while you were editing. Your text is preserved above; review the current version before saving again.", contentHash(current))
		return
	}

	// Validate before writing — a rejected save never reaches disk, so it
	// can never break the running server.
	if s.opts.ValidateConfig != nil {
		if err := s.opts.ValidateConfig([]byte(content)); err != nil {
			renderErr(err.Error(), contentHash(current))
			return
		}
	}

	if err := os.WriteFile(s.opts.ConfigFile, []byte(content), 0o644); err != nil {
		renderErr("Save failed: "+err.Error(), contentHash(current))
		return
	}
	if err := s.sync.CommitPush("web: edit kbrd.toml"); err != nil {
		renderErr("Saved locally, but git sync failed: "+err.Error(), contentHash(content))
		return
	}
	http.Redirect(w, r, "/", http.StatusSeeOther)
}
