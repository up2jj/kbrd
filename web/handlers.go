package web

import (
	"errors"
	"fmt"
	"maps"
	"net/http"
	"net/url"
	"os"
	"path/filepath"
	"strings"

	"kbrd/board"
)

// syncView feeds the header chip in base.html.
type syncView struct {
	Enabled bool
	OK      bool
	Detail  string
}

func (s *Server) syncView() syncView {
	if s.sync == nil {
		return syncView{Enabled: false, OK: true}
	}
	ok, detail := s.sync.Status()
	return syncView{Enabled: true, OK: ok, Detail: detail}
}

// page bundles the fields every full page shares.
func (s *Server) page(extra map[string]any) map[string]any {
	data := map[string]any{
		"BoardName": s.opts.BoardName,
		"Sync":      s.syncView(),
	}
	maps.Copy(data, extra)
	return data
}

// resolveCol maps the {col} path segment to its directory, or writes a 404.
func (s *Server) resolveCol(w http.ResponseWriter, r *http.Request) (name, path string, ok bool) {
	name = r.PathValue("col")
	path, err := board.ResolveColumn(s.opts.BoardPath, name, false)
	if err != nil {
		http.NotFound(w, r)
		return "", "", false
	}
	return filepath.Base(path), path, true
}

// itemName sanitizes the {name} path segment, or writes a 400.
func itemName(w http.ResponseWriter, r *http.Request) (string, bool) {
	clean, err := board.SanitizeName(r.PathValue("name"))
	if err != nil {
		http.Error(w, "bad item name", http.StatusBadRequest)
		return "", false
	}
	return clean, true
}

// redirectBoard sends the client back to the board, scrolled to the column.
// htmx requests get HX-Redirect (full page swap), plain forms a 303.
func redirectBoard(w http.ResponseWriter, r *http.Request, col string) {
	target := "/#col-" + url.PathEscape(col)
	if r.Header.Get("HX-Request") == "true" {
		w.Header().Set("HX-Redirect", target)
		w.WriteHeader(http.StatusOK)
		return
	}
	http.Redirect(w, r, target, http.StatusSeeOther)
}

// ---- auth pages ----

func (s *Server) handleLoginForm(w http.ResponseWriter, r *http.Request) {
	if s.auth.validCookie(r) {
		http.Redirect(w, r, "/", http.StatusSeeOther)
		return
	}
	s.render(w, "login.html", s.page(nil))
}

func (s *Server) handleLogin(w http.ResponseWriter, r *http.Request) {
	if s.auth.login(w, r, r.FormValue("token")) {
		http.Redirect(w, r, "/", http.StatusSeeOther)
		return
	}
	w.WriteHeader(http.StatusUnauthorized)
	s.render(w, "login.html", s.page(map[string]any{
		"Error": "Wrong token, or too many attempts — wait a moment and retry.",
	}))
}

func (s *Server) handleLogout(w http.ResponseWriter, r *http.Request) {
	s.auth.logout(w)
	http.Redirect(w, r, "/login", http.StatusSeeOther)
}

// ---- board views ----

func (s *Server) handleBoard(w http.ResponseWriter, r *http.Request) {
	cols, err := loadBoard(s.opts.BoardPath)
	if err != nil {
		http.Error(w, "failed to read board", http.StatusInternalServerError)
		return
	}
	cols = markChanged(cols, s.headChangedSet())
	q := strings.TrimSpace(r.URL.Query().Get("q"))
	data := s.page(map[string]any{
		"Columns":    filterColumns(cols, q),
		"Query":      q,
		"ShowSearch": true,
	})
	// The filter input's hx-get wants just the columns fragment; boosted
	// navigations (hx-boost on <body>) still get the full page.
	if r.Header.Get("HX-Request") == "true" && r.Header.Get("HX-Boosted") != "true" {
		s.render(w, "columns", data)
		return
	}
	s.render(w, "board.html", data)
}

func (s *Server) handleColumn(w http.ResponseWriter, r *http.Request) {
	colName, _, ok := s.resolveCol(w, r)
	if !ok {
		return
	}
	col, err := loadColumn(s.opts.BoardPath, colName)
	if err != nil {
		http.Error(w, "failed to read column", http.StatusInternalServerError)
		return
	}
	col = markChanged([]Column{col}, s.headChangedSet())[0]
	if q := strings.TrimSpace(r.URL.Query().Get("q")); q != "" {
		col.Cards = filterCards(col.Cards, strings.ToLower(q))
	}
	s.render(w, "column", col)
}

// ---- create ----

func (s *Server) handleNewForm(w http.ResponseWriter, r *http.Request) {
	colName, _, ok := s.resolveCol(w, r)
	if !ok {
		return
	}
	s.render(w, "new.html", s.page(map[string]any{"Col": colName}))
}

func (s *Server) handleCreate(w http.ResponseWriter, r *http.Request) {
	colName, colPath, ok := s.resolveCol(w, r)
	if !ok {
		return
	}
	name, content := r.FormValue("name"), r.FormValue("content")

	renderErr := func(msg string) {
		s.render(w, "new.html", s.page(map[string]any{
			"Col": colName, "Name": name, "Content": content, "Error": msg,
		}))
	}

	created, err := board.CreateItem(colPath, name, content)
	switch {
	case errors.Is(err, os.ErrExist):
		renderErr("A card with that name already exists.")
		return
	case err != nil:
		renderErr("Invalid name: " + err.Error())
		return
	}

	createdName := strings.TrimSuffix(filepath.Base(created), ".md")
	if err := s.sync.CommitPush(fmt.Sprintf("web: create %s/%s", colName, createdName)); err != nil {
		renderErr("Card created, but git sync failed: " + err.Error())
		return
	}
	redirectBoard(w, r, colName)
}

// ---- edit ----

func (s *Server) handleEditForm(w http.ResponseWriter, r *http.Request) {
	colName, colPath, ok := s.resolveCol(w, r)
	if !ok {
		return
	}
	name, ok := itemName(w, r)
	if !ok {
		return
	}
	content, err := board.ReadItem(colPath, name)
	if err != nil {
		http.NotFound(w, r)
		return
	}
	s.render(w, "edit.html", s.page(map[string]any{
		"Col": colName, "Name": name, "Content": content, "Hash": contentHash(content),
	}))
}

func (s *Server) handleSave(w http.ResponseWriter, r *http.Request) {
	colName, colPath, ok := s.resolveCol(w, r)
	if !ok {
		return
	}
	name, ok := itemName(w, r)
	if !ok {
		return
	}
	content := r.FormValue("content")

	renderErr := func(msg, hash string) {
		s.render(w, "edit.html", s.page(map[string]any{
			"Col": colName, "Name": name, "Content": content, "Hash": hash, "Error": msg,
		}))
	}

	// Stale-edit guard: reject when the file changed since the form loaded
	// (e.g. a background pull brought a newer version) — a clean git rebase
	// would otherwise silently supersede that change.
	current, err := board.ReadItem(colPath, name)
	if err != nil {
		http.NotFound(w, r)
		return
	}
	if r.FormValue("hash") != contentHash(current) {
		renderErr("This card changed while you were editing. Your text is preserved above; review the current version before saving again.", contentHash(current))
		return
	}

	if err := board.WriteItem(colPath, name, content); err != nil {
		renderErr("Save failed: "+err.Error(), contentHash(current))
		return
	}
	if err := s.sync.CommitPush(fmt.Sprintf("web: edit %s/%s", colName, name)); err != nil {
		renderErr("Saved locally, but git sync failed: "+err.Error(), contentHash(content))
		return
	}
	redirectBoard(w, r, colName)
}

// ---- delete ----

func (s *Server) handleDelete(w http.ResponseWriter, r *http.Request) {
	colName, colPath, ok := s.resolveCol(w, r)
	if !ok {
		return
	}
	name, ok := itemName(w, r)
	if !ok {
		return
	}
	if err := board.DeleteItem(colPath, name); err != nil {
		http.NotFound(w, r)
		return
	}
	if err := s.sync.CommitPush(fmt.Sprintf("web: delete %s/%s", colName, name)); err != nil {
		http.Error(w, "deleted locally, but git sync failed: "+err.Error(), http.StatusInternalServerError)
		return
	}
	redirectBoard(w, r, colName)
}
