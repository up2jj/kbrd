package extension

import (
	"bytes"
	"encoding/json"
	"os"
	"path/filepath"
	"testing"

	"kbrd/recents"
)

func TestNativeHostRoundTrip(t *testing.T) {
	boardDir := setupNativeBoard(t, "1. inbox")

	listed := runNativeRequest(t, nativeRequest{Action: "list_boards"})
	if !listed.OK {
		t.Fatalf("list_boards response: %+v", listed)
	}
	data, err := json.Marshal(listed.Data)
	if err != nil {
		t.Fatal(err)
	}
	var boards nativeBoardList
	if err := json.Unmarshal(data, &boards); err != nil {
		t.Fatal(err)
	}
	if len(boards.Boards) != 1 || boards.Boards[0].Path != boardDir {
		t.Fatalf("boards = %+v", boards.Boards)
	}

	created := runNativeRequest(t, nativeRequest{
		Action:  "add_file_to_board",
		Board:   boardDir,
		Folder:  "1. inbox",
		Name:    "  Docs / API: What's New?  ",
		Content: "from Chrome",
	})
	if !created.OK {
		t.Fatalf("add_file_to_board response: %+v", created)
	}
	content, err := os.ReadFile(filepath.Join(boardDir, "1. inbox", "docs-api-what-s-new.md"))
	if err != nil {
		t.Fatalf("read captured card: %v", err)
	}
	if string(content) != "from Chrome\n" {
		t.Fatalf("captured content = %q", content)
	}
}

func TestNativeHostRejectsCardNameWithoutFilenameCharacters(t *testing.T) {
	boardDir := setupNativeBoard(t, "inbox")

	response := runNativeRequest(t, nativeRequest{
		Action:  "add_file_to_board",
		Board:   boardDir,
		Folder:  "inbox",
		Name:    " / : ? ",
		Content: "content",
	})
	if response.OK || response.Error == "" {
		t.Fatalf("response = %+v, want empty sanitized-name error", response)
	}
}

func setupNativeBoard(t *testing.T, column string) string {
	t.Helper()
	home := t.TempDir()
	t.Setenv("HOME", home)
	t.Setenv("XDG_CONFIG_HOME", filepath.Join(home, ".config"))

	boardDir := filepath.Join(t.TempDir(), "work")
	if err := os.MkdirAll(filepath.Join(boardDir, column), 0o755); err != nil {
		t.Fatal(err)
	}
	store := recents.Store{Entries: []recents.Entry{{
		Path:   boardDir,
		Name:   "Work",
		Pinned: true,
	}}}
	if err := store.Save(); err != nil {
		t.Fatalf("save recents: %v", err)
	}
	return boardDir
}

func TestNativeHostReturnsOperationErrors(t *testing.T) {
	response := runNativeRequest(t, nativeRequest{Action: "nope"})
	if response.OK || response.Error == "" {
		t.Fatalf("response = %+v, want reported operation error", response)
	}
}

func TestIsNativeHostInvocation(t *testing.T) {
	if !IsNativeHostInvocation([]string{ExtensionOrigin}) {
		t.Fatal("registered extension origin was not recognized")
	}
	if IsNativeHostInvocation([]string{"chrome-extension://other/"}) {
		t.Fatal("unregistered extension origin was accepted")
	}
}

func runNativeRequest(t *testing.T, request nativeRequest) nativeResponse {
	t.Helper()
	body, err := json.Marshal(request)
	if err != nil {
		t.Fatal(err)
	}
	var input bytes.Buffer
	if err := writeNativeMessage(&input, body); err != nil {
		t.Fatal(err)
	}
	var output bytes.Buffer
	if err := RunNativeHost(&input, &output); err != nil {
		t.Fatalf("RunNativeHost: %v", err)
	}
	encoded, err := readNativeMessage(&output, maxNativeResponseBytes)
	if err != nil {
		t.Fatal(err)
	}
	var response nativeResponse
	if err := json.Unmarshal(encoded, &response); err != nil {
		t.Fatal(err)
	}
	return response
}
