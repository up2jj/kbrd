package mcp

import (
	"context"
	"fmt"
	"path/filepath"

	"github.com/modelcontextprotocol/go-sdk/mcp"

	"kbrd/board"
)

// Errors returned by these handlers are packed by the SDK into a tool result
// with IsError set (and the error text as content), so the client/LLM can see
// the failure and self-correct. We never return a Go error for normal "not
// found"/"ambiguous" conditions vs. success — both flow through this path.

type AddFileInput struct {
	Board        string `json:"board" jsonschema:"friendly name of the target board"`
	Name         string `json:"name" jsonschema:"item/card name (the .md extension is optional)"`
	Folder       string `json:"folder,omitempty" jsonschema:"folder (column) to place the item in; defaults to the board's first folder"`
	Content      string `json:"content,omitempty" jsonschema:"markdown body of the item"`
	CreateFolder bool   `json:"create_folder,omitempty" jsonschema:"create the folder if it does not exist (default false)"`
}

type AddFileOutput struct {
	Path   string `json:"path"`
	Board  string `json:"board"`
	Folder string `json:"folder"`
}

func addFileToBoard(ctx context.Context, _ *mcp.CallToolRequest, in AddFileInput) (*mcp.CallToolResult, AddFileOutput, error) {
	ref, err := board.Resolve(in.Board)
	if err != nil {
		return nil, AddFileOutput{}, err
	}
	colPath, err := board.ResolveColumn(ref.Path, in.Folder, in.CreateFolder)
	if err != nil {
		return nil, AddFileOutput{}, err
	}
	path, err := board.CreateItem(colPath, in.Name, in.Content)
	if err != nil {
		return nil, AddFileOutput{}, err
	}
	out := AddFileOutput{Path: path, Board: ref.Label(), Folder: filepath.Base(colPath)}
	return textf("added %s to [%s] %s", filepath.Base(path), out.Board, out.Folder), out, nil
}

type ListBoardsOutput struct {
	Boards []BoardEntry `json:"boards"`
}

type BoardEntry struct {
	Name   string `json:"name"`
	Path   string `json:"path"`
	Pinned bool   `json:"pinned"`
}

func listBoards(ctx context.Context, _ *mcp.CallToolRequest, _ struct{}) (*mcp.CallToolResult, ListBoardsOutput, error) {
	refs, err := board.ListBoards()
	if err != nil {
		return nil, ListBoardsOutput{}, err
	}
	out := ListBoardsOutput{Boards: make([]BoardEntry, 0, len(refs))}
	for _, r := range refs {
		out.Boards = append(out.Boards, BoardEntry{Name: r.Name, Path: r.Path, Pinned: r.Pinned})
	}
	return textf("%d board(s)", len(out.Boards)), out, nil
}

type ListFoldersInput struct {
	Board string `json:"board" jsonschema:"friendly name of the board"`
}

type ListFoldersOutput struct {
	Board   string   `json:"board"`
	Folders []string `json:"folders"`
}

func listFolders(ctx context.Context, _ *mcp.CallToolRequest, in ListFoldersInput) (*mcp.CallToolResult, ListFoldersOutput, error) {
	ref, err := board.Resolve(in.Board)
	if err != nil {
		return nil, ListFoldersOutput{}, err
	}
	cols, err := board.Columns(ref.Path)
	if err != nil {
		return nil, ListFoldersOutput{}, err
	}
	out := ListFoldersOutput{Board: ref.Label(), Folders: cols}
	return textf("%d folder(s) in [%s]", len(cols), out.Board), out, nil
}

type ListFilesInput struct {
	Board  string `json:"board" jsonschema:"friendly name of the board"`
	Folder string `json:"folder,omitempty" jsonschema:"folder (column); defaults to the board's first folder"`
}

type ListFilesOutput struct {
	Board  string   `json:"board"`
	Folder string   `json:"folder"`
	Files  []string `json:"files"`
}

func listFiles(ctx context.Context, _ *mcp.CallToolRequest, in ListFilesInput) (*mcp.CallToolResult, ListFilesOutput, error) {
	ref, err := board.Resolve(in.Board)
	if err != nil {
		return nil, ListFilesOutput{}, err
	}
	colPath, err := board.ResolveColumn(ref.Path, in.Folder, false)
	if err != nil {
		return nil, ListFilesOutput{}, err
	}
	items, err := board.Items(colPath)
	if err != nil {
		return nil, ListFilesOutput{}, err
	}
	out := ListFilesOutput{Board: ref.Label(), Folder: filepath.Base(colPath), Files: items}
	return textf("%d file(s) in [%s] %s", len(items), out.Board, out.Folder), out, nil
}

// textf builds a CallToolResult carrying a single human-readable text line.
// Structured output is populated separately by the SDK from the typed Out.
func textf(format string, args ...any) *mcp.CallToolResult {
	return &mcp.CallToolResult{
		Content: []mcp.Content{&mcp.TextContent{Text: fmt.Sprintf(format, args...)}},
	}
}
