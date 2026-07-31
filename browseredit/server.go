package browseredit

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"html/template"
	"io"
	"io/fs"
	"net/http"
	"os"
	"strings"
	"time"

	"kbrd/editdraft"
)

const (
	maxJSONBody        = 1 << 20
	handlerTimeout     = 12 * time.Second
	streamWriteTimeout = 5 * time.Second
	streamHeartbeat    = 15 * time.Second
)

var pageTemplate = template.Must(template.ParseFS(assets, "index.html"))

type documentResponse struct {
	Document
	BoardName    string       `json:"boardName"`
	ColumnName   string       `json:"columnName"`
	CardName     string       `json:"cardName"`
	LinkTargets  []LinkTarget `json:"linkTargets"`
	DraftPresent bool         `json:"draftPresent"`
	DraftBody    string       `json:"draftBody,omitzero"`
	Conflict     bool         `json:"conflict,omitzero"`
}

type editRequest struct {
	BaseRevision string `json:"baseRevision"`
	Body         string `json:"body"`
}

type claimRequest struct {
	ClientID string `json:"clientId"`
}

func (m *Manager) routes() http.Handler {
	mux := http.NewServeMux()
	staticFS, err := fs.Sub(assets, "static")
	if err != nil {
		panic(err)
	}
	mux.Handle("GET /static/", http.StripPrefix("/static/", http.FileServer(http.FS(staticFS))))
	mux.HandleFunc("GET /s/{token}/", m.handlePage)
	mux.HandleFunc("GET /s/{token}/document", m.handleDocument)
	mux.HandleFunc("POST /s/{token}/claim", m.handleClaim)
	mux.HandleFunc("PUT /s/{token}/draft", m.handleDraft)
	mux.HandleFunc("DELETE /s/{token}/draft", m.handleDeleteDraft)
	mux.HandleFunc("POST /s/{token}/save", m.handleSave)
	mux.HandleFunc("GET /s/{token}/events", m.handleEvents)
	mux.HandleFunc("POST /s/{token}/heartbeat", m.handleHeartbeat)
	mux.HandleFunc("POST /s/{token}/handoff-ready", m.handleHandoffReady)
	mux.HandleFunc("POST /s/{token}/close", m.handleCloseLease)
	return m.securityMiddleware(mux)
}

func (m *Manager) securityMiddleware(next http.Handler) http.Handler {
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Security-Policy", "default-src 'none'; script-src 'self'; style-src 'self' 'unsafe-inline'; connect-src 'self'; img-src 'self' data: blob:; font-src 'self'; base-uri 'none'; frame-ancestors 'none'; form-action 'self'")
		w.Header().Set("Referrer-Policy", "no-referrer")
		w.Header().Set("X-Content-Type-Options", "nosniff")
		w.Header().Set("Cross-Origin-Resource-Policy", "same-origin")
		if r.Host != m.address {
			http.Error(w, "invalid host", http.StatusBadRequest)
			return
		}
		if r.Method == http.MethodPost || r.Method == http.MethodPut || r.Method == http.MethodDelete {
			if r.Header.Get("Origin") != m.baseURL {
				http.Error(w, "invalid origin", http.StatusForbidden)
				return
			}
		}
		if strings.HasSuffix(r.URL.Path, "/events") {
			next.ServeHTTP(w, r)
			return
		}
		ctx, cancel := context.WithTimeout(r.Context(), handlerTimeout)
		defer cancel()
		next.ServeHTTP(w, r.WithContext(ctx))
	})
}

func (m *Manager) lookupHTTP(w http.ResponseWriter, r *http.Request) (*session, bool) {
	s := m.sessionByToken(r.PathValue("token"))
	if s == nil {
		http.Error(w, "unknown editor session", http.StatusNotFound)
		return nil, false
	}
	s.mu.Lock()
	invalid, reason := s.status == sessionInvalid, s.reason
	s.mu.Unlock()
	if invalid {
		if reason == "" {
			reason = "editor session is no longer available"
		}
		http.Error(w, reason, http.StatusGone)
		return nil, false
	}
	return s, true
}

func (m *Manager) handlePage(w http.ResponseWriter, r *http.Request) {
	s, ok := m.lookupHTTP(w, r)
	if !ok {
		return
	}
	s.mu.Lock()
	data := struct {
		Token, BoardName, ColumnName, CardName string
	}{s.token, s.card.BoardName, s.card.ColumnName, s.card.CardName}
	s.mu.Unlock()
	w.Header().Set("Content-Type", "text/html; charset=utf-8")
	if err := pageTemplate.ExecuteTemplate(w, "index.html", data); err != nil {
		return
	}
}

func (m *Manager) handleDocument(w http.ResponseWriter, r *http.Request) {
	s, ok := m.lookupHTTP(w, r)
	if !ok {
		return
	}
	raw, err := os.ReadFile(s.path)
	if err != nil {
		if errors.Is(err, os.ErrNotExist) {
			m.Invalidate(s.id, "card no longer exists")
			http.Error(w, "card no longer exists", http.StatusGone)
			return
		}
		http.Error(w, "failed to read card", http.StatusInternalServerError)
		return
	}
	doc := ParseDocument(string(raw))
	m.reconcileDiskDocument(s, string(raw), doc)
	s.mu.Lock()
	response := documentResponse{
		Document: doc, BoardName: s.card.BoardName, ColumnName: s.card.ColumnName,
		CardName: s.card.CardName, LinkTargets: append([]LinkTarget(nil), s.card.LinkTargets...), Conflict: s.conflict,
	}
	s.mu.Unlock()
	if draft, err := editdraft.Read(s.path); err == nil && string(draft) != string(raw) {
		draftDoc := ParseDocument(string(draft))
		if draftDoc.WYSIWYGSafe {
			response.DraftPresent = true
			response.DraftBody = draftDoc.Body
		}
	}
	w.Header().Set("ETag", `"`+doc.Revision+`"`)
	writeJSON(w, http.StatusOK, response)
}

func (m *Manager) handleClaim(w http.ResponseWriter, r *http.Request) {
	s, ok := m.lookupHTTP(w, r)
	if !ok {
		return
	}
	var req claimRequest
	if err := decodeJSON(w, r, &req); err != nil {
		return
	}
	if len(req.ClientID) < 16 || len(req.ClientID) > 256 {
		http.Error(w, "invalid client id", http.StatusBadRequest)
		return
	}
	presented := r.Header.Get("X-Kbrd-Editor-Lease")
	now := time.Now()
	s.mu.Lock()
	defer s.mu.Unlock()
	if s.status != sessionOpen {
		http.Error(w, "session is handing off", http.StatusConflict)
		return
	}
	if s.leaseOwned(now) {
		if presented == s.leaseToken && req.ClientID == s.leaseClient {
			writeJSON(w, http.StatusOK, map[string]any{"lease": s.leaseToken, "writer": true})
			return
		}
		writeJSON(w, http.StatusConflict, map[string]any{"writer": false, "message": "already open in another browser tab"})
		return
	}
	lease, err := randomToken(32)
	if err != nil {
		http.Error(w, "failed to create lease", http.StatusInternalServerError)
		return
	}
	s.leaseToken, s.leaseClient = lease, req.ClientID
	s.leaseClaimedAt = now
	s.lastHeartbeat = time.Time{}
	s.leaseGeneration++
	writeJSON(w, http.StatusOK, map[string]any{"lease": lease, "writer": true})
}

func (m *Manager) handleHeartbeat(w http.ResponseWriter, r *http.Request) {
	s, ok := m.lookupHTTP(w, r)
	if !ok {
		return
	}
	lease := r.Header.Get("X-Kbrd-Editor-Lease")
	s.mu.Lock()
	if !s.validateLease(lease, time.Now(), false) {
		s.mu.Unlock()
		http.Error(w, "writer lease is invalid", http.StatusConflict)
		return
	}
	s.lastHeartbeat = time.Now()
	s.mu.Unlock()
	w.WriteHeader(http.StatusNoContent)
}

func (m *Manager) handleDraft(w http.ResponseWriter, r *http.Request) {
	s, ok := m.lookupHTTP(w, r)
	if !ok {
		return
	}
	if !m.requireLease(w, r, s, true) {
		return
	}
	var req editRequest
	if err := decodeJSON(w, r, &req); err != nil {
		return
	}
	raw, err := os.ReadFile(s.path)
	if err != nil {
		if errors.Is(err, os.ErrNotExist) {
			m.Invalidate(s.id, "card no longer exists")
			http.Error(w, "card no longer exists", http.StatusGone)
			return
		}
		http.Error(w, "failed to read card", http.StatusInternalServerError)
		return
	}
	merged, err := MergeBody(string(raw), req.Body)
	if err != nil {
		http.Error(w, err.Error(), http.StatusConflict)
		return
	}
	if err := editdraft.Write(s.path, []byte(merged)); err != nil {
		http.Error(w, "failed to persist recovery draft", http.StatusInternalServerError)
		return
	}
	current := ParseDocument(string(raw))
	stale := current.Revision != req.BaseRevision
	s.mu.Lock()
	s.draftBody, s.dirty, s.conflict = req.Body, true, stale
	s.mu.Unlock()
	writeJSON(w, http.StatusOK, map[string]any{"draftSaved": true, "stale": stale, "document": current})
}

func (m *Manager) handleDeleteDraft(w http.ResponseWriter, r *http.Request) {
	s, ok := m.lookupHTTP(w, r)
	if !ok || !m.requireLease(w, r, s, false) {
		return
	}
	if err := editdraft.Clear(s.path); err != nil {
		http.Error(w, "failed to clear draft", http.StatusInternalServerError)
		return
	}
	raw, err := os.ReadFile(s.path)
	if err != nil {
		http.Error(w, "failed to read card", http.StatusInternalServerError)
		return
	}
	doc := ParseDocument(string(raw))
	s.mu.Lock()
	s.raw, s.document, s.draftBody, s.dirty, s.conflict = string(raw), doc, "", false, false
	s.broadcastLocked(streamEvent{Name: "document", Data: doc})
	s.mu.Unlock()
	writeJSON(w, http.StatusOK, doc)
}

func (m *Manager) handleSave(w http.ResponseWriter, r *http.Request) {
	s, ok := m.lookupHTTP(w, r)
	if !ok || !m.requireLease(w, r, s, false) {
		return
	}
	var req editRequest
	if err := decodeJSON(w, r, &req); err != nil {
		return
	}
	// Explicit save also persists the latest browser buffer first. A stale or
	// failed Board mutation therefore retains a complete recovery draft.
	raw, err := os.ReadFile(s.path)
	if err != nil {
		if errors.Is(err, os.ErrNotExist) {
			m.Invalidate(s.id, "card no longer exists")
			http.Error(w, "card no longer exists", http.StatusGone)
			return
		}
		http.Error(w, "failed to read card", http.StatusInternalServerError)
		return
	}
	merged, err := MergeBody(string(raw), req.Body)
	if err != nil {
		http.Error(w, err.Error(), http.StatusConflict)
		return
	}
	if err := editdraft.Write(s.path, []byte(merged)); err != nil {
		http.Error(w, "failed to persist recovery draft", http.StatusInternalServerError)
		return
	}
	s.mu.Lock()
	s.draftBody, s.dirty = req.Body, true
	s.mu.Unlock()
	reply := make(chan SaveResult, 1)
	save := SaveRequest{SessionID: s.id, BaseRevision: req.BaseRevision, Body: req.Body, Reply: reply}
	select {
	case m.saves <- save:
	case <-r.Context().Done():
		return
	case <-m.ctx.Done():
		http.Error(w, "browser editor is shutting down", http.StatusServiceUnavailable)
		return
	}
	select {
	case result := <-reply:
		m.finishSaveHTTP(w, s, result)
	case <-r.Context().Done():
		return
	case <-m.ctx.Done():
		http.Error(w, "browser editor is shutting down", http.StatusServiceUnavailable)
	}
}

func (m *Manager) finishSaveHTTP(w http.ResponseWriter, s *session, result SaveResult) {
	switch {
	case result.Gone:
		m.Invalidate(s.id, "card no longer exists")
		writeJSON(w, http.StatusGone, result)
	case result.Conflict:
		s.mu.Lock()
		s.conflict = true
		s.broadcastLocked(streamEvent{Name: "conflict", Data: result.Document})
		s.mu.Unlock()
		writeJSON(w, http.StatusPreconditionFailed, result)
	case result.Err != nil:
		http.Error(w, result.Err.Error(), http.StatusInternalServerError)
	default:
		_ = editdraft.Clear(s.path)
		s.mu.Lock()
		s.document, s.raw = result.Document, ""
		s.draftBody, s.dirty, s.conflict = "", false, false
		s.broadcastLocked(streamEvent{Name: "document", Data: result.Document})
		s.mu.Unlock()
		w.Header().Set("ETag", `"`+result.Document.Revision+`"`)
		writeJSON(w, http.StatusOK, result)
	}
}

func (m *Manager) requireLease(w http.ResponseWriter, r *http.Request, s *session, allowHandoff bool) bool {
	s.mu.Lock()
	ok := s.validateLease(r.Header.Get("X-Kbrd-Editor-Lease"), time.Now(), allowHandoff)
	s.mu.Unlock()
	if !ok {
		http.Error(w, "writer lease is invalid", http.StatusConflict)
	}
	return ok
}

func (m *Manager) handleHandoffReady(w http.ResponseWriter, r *http.Request) {
	s, ok := m.lookupHTTP(w, r)
	if !ok || !m.requireLease(w, r, s, true) {
		return
	}
	s.mu.Lock()
	if s.status != sessionHandingOff {
		s.mu.Unlock()
		http.Error(w, "handoff was not requested", http.StatusConflict)
		return
	}
	s.handoffOnce.Do(func() { close(s.handoffReady) })
	s.mu.Unlock()
	w.WriteHeader(http.StatusNoContent)
}

func (m *Manager) handleCloseLease(w http.ResponseWriter, r *http.Request) {
	s, ok := m.lookupHTTP(w, r)
	if !ok || !m.requireLease(w, r, s, true) {
		return
	}
	s.mu.Lock()
	s.leaseToken, s.leaseClient = "", ""
	s.leaseClaimedAt = time.Time{}
	s.lastHeartbeat = time.Time{}
	s.mu.Unlock()
	w.WriteHeader(http.StatusNoContent)
}

func (m *Manager) handleEvents(w http.ResponseWriter, r *http.Request) {
	s, ok := m.lookupHTTP(w, r)
	if !ok {
		return
	}
	flusher, ok := w.(http.Flusher)
	if !ok {
		http.Error(w, "streaming unsupported", http.StatusInternalServerError)
		return
	}
	w.Header().Set("Content-Type", "text/event-stream")
	w.Header().Set("Cache-Control", "no-cache")
	w.Header().Set("Connection", "keep-alive")
	ch := make(chan streamEvent, 8)
	s.mu.Lock()
	s.subscribers[ch] = struct{}{}
	initial := streamEvent{Name: "document", Data: s.document}
	if s.conflict {
		initial.Name = "conflict"
	}
	s.mu.Unlock()
	defer func() {
		s.mu.Lock()
		delete(s.subscribers, ch)
		s.mu.Unlock()
	}()
	controller := http.NewResponseController(w)
	if !writeStreamEvent(controller, w, flusher, initial) {
		return
	}
	ticker := time.NewTicker(streamHeartbeat)
	defer ticker.Stop()
	for {
		select {
		case event := <-ch:
			if !writeStreamEvent(controller, w, flusher, event) {
				return
			}
			if event.Name == "invalidated" {
				return
			}
		case <-ticker.C:
			if err := controller.SetWriteDeadline(time.Now().Add(streamWriteTimeout)); err != nil {
				return
			}
			if _, err := io.WriteString(w, ": heartbeat\n\n"); err != nil {
				return
			}
			flusher.Flush()
			_ = controller.SetWriteDeadline(time.Time{})
		case <-r.Context().Done():
			return
		case <-m.ctx.Done():
			return
		}
	}
}

func writeStreamEvent(controller *http.ResponseController, w io.Writer, flusher http.Flusher, event streamEvent) bool {
	data, err := json.Marshal(event.Data)
	if err != nil {
		return false
	}
	if err := controller.SetWriteDeadline(time.Now().Add(streamWriteTimeout)); err != nil {
		return false
	}
	if _, err := fmt.Fprintf(w, "event: %s\ndata: %s\n\n", event.Name, data); err != nil {
		return false
	}
	flusher.Flush()
	_ = controller.SetWriteDeadline(time.Time{})
	return true
}

func decodeJSON(w http.ResponseWriter, r *http.Request, dst any) error {
	mediaType := strings.ToLower(strings.TrimSpace(strings.Split(r.Header.Get("Content-Type"), ";")[0]))
	if mediaType != "application/json" {
		http.Error(w, "content type must be application/json", http.StatusUnsupportedMediaType)
		return errors.New("invalid content type")
	}
	r.Body = http.MaxBytesReader(w, r.Body, maxJSONBody)
	decoder := json.NewDecoder(r.Body)
	decoder.DisallowUnknownFields()
	if err := decoder.Decode(dst); err != nil {
		var maxErr *http.MaxBytesError
		if errors.As(err, &maxErr) {
			http.Error(w, "request body is too large", http.StatusRequestEntityTooLarge)
		} else {
			http.Error(w, "invalid JSON request", http.StatusBadRequest)
		}
		return err
	}
	if err := decoder.Decode(&struct{}{}); !errors.Is(err, io.EOF) {
		http.Error(w, "request must contain one JSON value", http.StatusBadRequest)
		return errors.New("trailing JSON data")
	}
	return nil
}

func writeJSON(w http.ResponseWriter, status int, value any) {
	w.Header().Set("Content-Type", "application/json")
	w.WriteHeader(status)
	_ = json.NewEncoder(w).Encode(value)
}
