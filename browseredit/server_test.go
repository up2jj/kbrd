package browseredit

import (
	"bytes"
	"encoding/json"
	"io"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"kbrd/editdraft"
)

func TestManagerIsLazyAndBindsIPv4Loopback(t *testing.T) {
	m := New()
	t.Cleanup(func() { _ = m.Close() })
	if m.Active() || m.listener != nil {
		t.Fatal("new manager started resources eagerly")
	}
	path := filepath.Join(t.TempDir(), "task.md")
	if err := os.WriteFile(path, []byte("body\n"), 0o644); err != nil {
		t.Fatal(err)
	}
	opened, err := m.Open(Card{Path: path, BoardName: "Board", ColumnName: "Todo", CardName: "task"})
	if err != nil {
		t.Fatal(err)
	}
	if !strings.HasPrefix(opened.URL, "http://127.0.0.1:") {
		t.Fatalf("URL = %q", opened.URL)
	}
	response, err := http.Get(opened.URL)
	if err != nil {
		t.Fatal(err)
	}
	defer response.Body.Close()
	if response.StatusCode != http.StatusOK {
		t.Fatalf("GET page = %d", response.StatusCode)
	}
	if got := response.Header.Get("Content-Security-Policy"); !strings.Contains(got, "default-src 'none'") || strings.Contains(got, "unsafe-eval") {
		t.Fatalf("CSP = %q", got)
	}
	if response.Header.Get("Access-Control-Allow-Origin") != "" {
		t.Fatal("server emitted CORS header")
	}
}

func TestDocumentReturnsCurrentLinkTargetLabels(t *testing.T) {
	m := New()
	t.Cleanup(func() { _ = m.Close() })
	dir := t.TempDir()
	path := filepath.Join(dir, "task.md")
	if err := os.WriteFile(path, []byte("body\n"), 0o644); err != nil {
		t.Fatal(err)
	}
	opened, err := m.Open(Card{Path: path, LinkTargets: []LinkTarget{{Name: "Old", Column: "Todo"}}})
	if err != nil {
		t.Fatal(err)
	}
	if _, err := m.Open(Card{Path: path, LinkTargets: []LinkTarget{{Name: "Current", Column: "Done"}}}); err != nil {
		t.Fatal(err)
	}
	response, err := http.Get(opened.URL + "document")
	if err != nil {
		t.Fatal(err)
	}
	body := readBody(t, response)
	if strings.Contains(body, dir) {
		t.Fatalf("document response exposed a filesystem path: %s", body)
	}
	var doc documentResponse
	if err := json.Unmarshal([]byte(body), &doc); err != nil {
		t.Fatal(err)
	}
	if len(doc.LinkTargets) != 1 || doc.LinkTargets[0] != (LinkTarget{Name: "Current", Column: "Done"}) {
		t.Fatalf("link targets = %+v", doc.LinkTargets)
	}
}

func TestWriterLeaseDraftAndExplicitSave(t *testing.T) {
	m := New()
	t.Cleanup(func() { _ = m.Close() })
	path := filepath.Join(t.TempDir(), "task.md")
	raw := "---\nowner: disk\n---\nold\n"
	if err := os.WriteFile(path, []byte(raw), 0o644); err != nil {
		t.Fatal(err)
	}
	opened, err := m.Open(Card{Path: path, CardName: "task"})
	if err != nil {
		t.Fatal(err)
	}
	root := strings.TrimSuffix(opened.URL, "/")
	clientID := strings.Repeat("a", 32)
	claim := doJSON(t, m, http.MethodPost, root+"/claim", "", map[string]any{"clientId": clientID})
	if claim.StatusCode != http.StatusOK {
		t.Fatalf("claim = %d: %s", claim.StatusCode, readBody(t, claim))
	}
	var claimed struct {
		Lease string `json:"lease"`
	}
	decodeResponse(t, claim, &claimed)
	if len(claimed.Lease) < 32 {
		t.Fatalf("lease too short: %q", claimed.Lease)
	}
	if m.Active() {
		t.Fatal("claim without application heartbeat marked editor active")
	}
	heartbeat := doJSON(t, m, http.MethodPost, root+"/heartbeat", claimed.Lease, map[string]any{})
	if heartbeat.StatusCode != http.StatusNoContent {
		t.Fatalf("heartbeat = %d: %s", heartbeat.StatusCode, readBody(t, heartbeat))
	}
	heartbeat.Body.Close()
	if !m.Active() {
		t.Fatal("valid application heartbeat did not mark editor active")
	}
	second := doJSON(t, m, http.MethodPost, root+"/claim", "", map[string]any{"clientId": strings.Repeat("b", 32)})
	if second.StatusCode != http.StatusConflict {
		t.Fatalf("second claim = %d", second.StatusCode)
	}
	second.Body.Close()

	draft := doJSON(t, m, http.MethodPut, root+"/draft", claimed.Lease, editRequest{BaseRevision: Revision(raw), Body: "mine\n"})
	if draft.StatusCode != http.StatusOK {
		t.Fatalf("draft = %d: %s", draft.StatusCode, readBody(t, draft))
	}
	draft.Body.Close()
	cardBytes, _ := os.ReadFile(path)
	if string(cardBytes) != raw {
		t.Fatalf("draft changed card: %q", cardBytes)
	}
	swap, err := editdraft.Read(path)
	if err != nil {
		t.Fatal(err)
	}
	if got, want := string(swap), "---\nowner: disk\n---\nmine\n"; got != want {
		t.Fatalf("draft = %q, want %q", got, want)
	}

	go func() {
		request := <-m.SaveRequests()
		merged, mergeErr := MergeBody(raw, request.Body)
		if mergeErr == nil {
			mergeErr = os.WriteFile(path, []byte(merged), 0o644)
		}
		request.Reply <- SaveResult{Document: ParseDocument(merged), Err: mergeErr}
	}()
	saved := doJSON(t, m, http.MethodPost, root+"/save", claimed.Lease, editRequest{BaseRevision: Revision(raw), Body: "mine\n"})
	if saved.StatusCode != http.StatusOK {
		t.Fatalf("save = %d: %s", saved.StatusCode, readBody(t, saved))
	}
	saved.Body.Close()
	if _, err := editdraft.Read(path); !os.IsNotExist(err) {
		t.Fatalf("successful save retained draft: %v", err)
	}
}

func TestCloseReleasesWriterLease(t *testing.T) {
	m := New()
	t.Cleanup(func() { _ = m.Close() })
	path := filepath.Join(t.TempDir(), "task.md")
	if err := os.WriteFile(path, []byte("body\n"), 0o644); err != nil {
		t.Fatal(err)
	}
	opened, err := m.Open(Card{Path: path, CardName: "task"})
	if err != nil {
		t.Fatal(err)
	}
	root := strings.TrimSuffix(opened.URL, "/")
	claim := doJSON(t, m, http.MethodPost, root+"/claim", "", map[string]any{"clientId": strings.Repeat("a", 32)})
	if claim.StatusCode != http.StatusOK {
		t.Fatalf("claim = %d: %s", claim.StatusCode, readBody(t, claim))
	}
	var claimed struct {
		Lease string `json:"lease"`
	}
	decodeResponse(t, claim, &claimed)
	heartbeat := doJSON(t, m, http.MethodPost, root+"/heartbeat", claimed.Lease, map[string]any{})
	if heartbeat.StatusCode != http.StatusNoContent {
		t.Fatalf("heartbeat = %d: %s", heartbeat.StatusCode, readBody(t, heartbeat))
	}
	heartbeat.Body.Close()
	if !m.Active() {
		t.Fatal("writer did not become active")
	}

	closed := doJSON(t, m, http.MethodPost, root+"/close", claimed.Lease, map[string]any{})
	if closed.StatusCode != http.StatusNoContent {
		t.Fatalf("close = %d: %s", closed.StatusCode, readBody(t, closed))
	}
	closed.Body.Close()
	if m.Active() {
		t.Fatal("close retained the writer lease")
	}

	next := doJSON(t, m, http.MethodPost, root+"/claim", "", map[string]any{"clientId": strings.Repeat("b", 32)})
	if next.StatusCode != http.StatusOK {
		t.Fatalf("claim after close = %d: %s", next.StatusCode, readBody(t, next))
	}
	next.Body.Close()
}

func TestSecurityRejectsOriginHostLeaseAndInvalidation(t *testing.T) {
	m := New()
	t.Cleanup(func() { _ = m.Close() })
	path := filepath.Join(t.TempDir(), "task.md")
	if err := os.WriteFile(path, []byte("body"), 0o644); err != nil {
		t.Fatal(err)
	}
	opened, err := m.Open(Card{Path: path})
	if err != nil {
		t.Fatal(err)
	}
	root := strings.TrimSuffix(opened.URL, "/")
	req, _ := http.NewRequest(http.MethodPost, root+"/claim", bytes.NewBufferString(`{"clientId":"aaaaaaaaaaaaaaaa"}`))
	req.Header.Set("Content-Type", "application/json")
	req.Header.Set("Origin", "http://evil.invalid")
	response, err := http.DefaultClient.Do(req)
	if err != nil {
		t.Fatal(err)
	}
	if response.StatusCode != http.StatusForbidden {
		t.Fatalf("bad origin = %d", response.StatusCode)
	}
	response.Body.Close()

	wrongLease := doJSON(t, m, http.MethodPut, root+"/draft", "wrong", editRequest{Body: "x"})
	if wrongLease.StatusCode != http.StatusConflict {
		t.Fatalf("wrong lease = %d", wrongLease.StatusCode)
	}
	wrongLease.Body.Close()
	m.Invalidate(opened.ID, "test invalidation")
	response, err = http.Get(opened.URL + "document")
	if err != nil {
		t.Fatal(err)
	}
	if response.StatusCode != http.StatusGone {
		t.Fatalf("invalidated = %d", response.StatusCode)
	}
	response.Body.Close()
}

func TestDocumentGetReconcilesDirtyExternalChange(t *testing.T) {
	path := filepath.Join(t.TempDir(), "task.md")
	oldRaw := "---\nowner: old\n---\ndisk\n"
	newRaw := "---\nowner: newest\n---\nexternal\n"
	if err := os.WriteFile(path, []byte(newRaw), 0o644); err != nil {
		t.Fatal(err)
	}
	m, s := managerWithSession(path, oldRaw)
	s.dirty, s.draftBody = true, "browser\n"
	if err := editdraft.Write(path, []byte("---\nowner: old\n---\nbrowser\n")); err != nil {
		t.Fatal(err)
	}

	req := httptest.NewRequest(http.MethodGet, "/s/token/document", nil)
	req.SetPathValue("token", s.token)
	recorder := httptest.NewRecorder()
	m.handleDocument(recorder, req)
	if recorder.Code != http.StatusOK {
		t.Fatalf("GET document = %d: %s", recorder.Code, recorder.Body.String())
	}
	var response documentResponse
	if err := json.NewDecoder(recorder.Body).Decode(&response); err != nil {
		t.Fatal(err)
	}
	if !response.Conflict || response.Revision != Revision(newRaw) || !response.DraftPresent || response.DraftBody != "browser\n" {
		t.Fatalf("response = %+v", response)
	}

	s.mu.Lock()
	baseline, conflict := s.document.Revision, s.conflict
	s.mu.Unlock()
	if baseline != Revision(oldRaw) || !conflict {
		t.Fatalf("dirty session baseline = %q, conflict = %v", baseline, conflict)
	}
	draft, err := editdraft.Read(path)
	if err != nil {
		t.Fatal(err)
	}
	if want := "---\nowner: newest\n---\nbrowser\n"; string(draft) != want {
		t.Fatalf("rebased draft = %q, want %q", draft, want)
	}

	// The eventual fsnotify pass must not undo or suppress the reconciliation.
	m.handleWatchedPath(path)
	s.mu.Lock()
	defer s.mu.Unlock()
	if !s.conflict || s.document.Revision != Revision(oldRaw) {
		t.Fatalf("watcher changed dirty baseline: %+v", s.document)
	}
}

func doJSON(t *testing.T, m *Manager, method, url, lease string, value any) *http.Response {
	t.Helper()
	data, err := json.Marshal(value)
	if err != nil {
		t.Fatal(err)
	}
	req, err := http.NewRequest(method, url, bytes.NewReader(data))
	if err != nil {
		t.Fatal(err)
	}
	req.Header.Set("Content-Type", "application/json")
	req.Header.Set("Origin", m.baseURL)
	if lease != "" {
		req.Header.Set("X-Kbrd-Editor-Lease", lease)
	}
	response, err := http.DefaultClient.Do(req)
	if err != nil {
		t.Fatal(err)
	}
	return response
}

func decodeResponse(t *testing.T, response *http.Response, dst any) {
	t.Helper()
	defer response.Body.Close()
	if err := json.NewDecoder(response.Body).Decode(dst); err != nil {
		t.Fatal(err)
	}
}

func readBody(t *testing.T, response *http.Response) string {
	t.Helper()
	defer response.Body.Close()
	b, err := io.ReadAll(response.Body)
	if err != nil {
		t.Fatal(err)
	}
	return string(b)
}
