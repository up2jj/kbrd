package browseredit

import (
	"os"
	"path/filepath"
	"testing"

	"kbrd/editdraft"
)

func TestWatchedCleanSessionAdoptsDocument(t *testing.T) {
	path := filepath.Join(t.TempDir(), "task.md")
	oldRaw, newRaw := "old\n", "new\n"
	if err := os.WriteFile(path, []byte(newRaw), 0o644); err != nil {
		t.Fatal(err)
	}
	m, s := managerWithSession(path, oldRaw)
	m.handleWatchedPath(path)
	s.mu.Lock()
	defer s.mu.Unlock()
	if s.document.Revision != Revision(newRaw) || s.document.Body != newRaw || s.conflict {
		t.Fatalf("session did not adopt external document: %+v", s.document)
	}
}

func TestWatchedDirtySessionRebasesNewestFrontmatter(t *testing.T) {
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
	m.handleWatchedPath(path)
	s.mu.Lock()
	conflict := s.conflict
	s.mu.Unlock()
	if !conflict {
		t.Fatal("dirty external change did not enter conflict")
	}
	draft, err := editdraft.Read(path)
	if err != nil {
		t.Fatal(err)
	}
	if want := "---\nowner: newest\n---\nbrowser\n"; string(draft) != want {
		t.Fatalf("rebased draft = %q, want %q", draft, want)
	}
}

func TestWatchedMalformedFrontmatterKeepsDurableDraft(t *testing.T) {
	path := filepath.Join(t.TempDir(), "task.md")
	oldRaw := "---\nowner: old\n---\ndisk\n"
	if err := os.WriteFile(path, []byte("---\nowner: ambiguous\n"), 0o644); err != nil {
		t.Fatal(err)
	}
	m, s := managerWithSession(path, oldRaw)
	s.dirty, s.draftBody = true, "browser\n"
	want := "---\nowner: old\n---\nbrowser\n"
	if err := editdraft.Write(path, []byte(want)); err != nil {
		t.Fatal(err)
	}
	m.handleWatchedPath(path)
	draft, err := editdraft.Read(path)
	if err != nil {
		t.Fatal(err)
	}
	if string(draft) != want {
		t.Fatalf("malformed external metadata replaced durable draft: %q", draft)
	}
}

func managerWithSession(path, raw string) (*Manager, *session) {
	m := New()
	s := &session{
		id: "id", token: "token", path: path, raw: raw,
		document: ParseDocument(raw), subscribers: make(map[chan streamEvent]struct{}),
	}
	m.sessions[s.id], m.sessions[s.token], m.byPath[path] = s, s, s
	return m, s
}
