package extension

import (
	"bytes"
	"encoding/json"
	"os"
	"path/filepath"
	"testing"

	"kbrd/config"
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
		Content: "## Captured\n\nA [formatted link](https://example.com).",
	})
	if !created.OK {
		t.Fatalf("add_file_to_board response: %+v", created)
	}
	content, err := os.ReadFile(filepath.Join(boardDir, "1. inbox", "docs-api-what-s-new.md"))
	if err != nil {
		t.Fatalf("read captured card: %v", err)
	}
	if string(content) != "## Captured\n\nA [formatted link](https://example.com).\n" {
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

func TestNativeHostRunsItemCreatedHooks(t *testing.T) {
	boardDir := setupNativeBoard(t, "inbox")
	hookOutput := filepath.Join(boardDir, "hook-output")
	hooks := "hooks:\n" +
		"  - name: Record browser capture\n" +
		"    id: record-browser-capture\n" +
		"    event: item_created\n" +
		"    command: printf '%s|%s|%s' '{{.filePath}}' '{{.columnName}}' '{{.fileName}}' > '" + hookOutput + "'\n"
	if err := os.WriteFile(filepath.Join(boardDir, config.FolderHooksFile), []byte(hooks), 0o644); err != nil {
		t.Fatal(err)
	}

	response := runNativeRequest(t, nativeRequest{
		Action:  "add_file_to_board",
		Board:   boardDir,
		Folder:  "inbox",
		Name:    "Browser Capture",
		Content: "content",
	})
	if !response.OK {
		t.Fatalf("add_file_to_board response: %+v", response)
	}
	got, err := os.ReadFile(hookOutput)
	if err != nil {
		t.Fatalf("read hook output: %v", err)
	}
	want := filepath.Join(boardDir, "inbox", "browser-capture.md") + "|inbox|browser-capture"
	if string(got) != want {
		t.Fatalf("hook output = %q, want %q", got, want)
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
