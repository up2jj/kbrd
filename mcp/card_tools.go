package mcp

import (
	"context"
	"crypto/sha256"
	"encoding/hex"
	"fmt"
	"path/filepath"
	"strings"

	"github.com/modelcontextprotocol/go-sdk/mcp"

	"kbrd/board"
	"kbrd/boardops"
	"kbrd/frontmatter"
)

type GetCardInput struct {
	Board  string `json:"board" jsonschema:"friendly name of the board"`
	Column string `json:"column" jsonschema:"column containing the card"`
	Name   string `json:"name" jsonschema:"card name (the .md extension is optional)"`
}

type CardOutput struct {
	Board       string         `json:"board"`
	Column      string         `json:"column"`
	Name        string         `json:"name"`
	Raw         string         `json:"raw"`
	Body        string         `json:"body"`
	Frontmatter map[string]any `json:"frontmatter"`
	Revision    string         `json:"revision"`
}

func getCard(ctx context.Context, req *mcp.CallToolRequest, in GetCardInput, allowReads bool) (*mcp.CallToolResult, CardOutput, error) {
	if !allowReads {
		return nil, CardOutput{}, fmt.Errorf("card reads are disabled; set [mcp] allow_card_reads = true")
	}
	ref, col, err := resolveCardLocation(ctx, req, in.Board, in.Column)
	if err != nil {
		return nil, CardOutput{}, err
	}
	name, err := board.SanitizeName(in.Name)
	if err != nil {
		return nil, CardOutput{}, err
	}
	raw, err := board.ReadItem(col.Path, name)
	if err != nil {
		return nil, CardOutput{}, err
	}
	front, body, _ := frontmatter.Split(raw)
	parsed, err := frontmatter.Parse([]byte(front))
	if err != nil {
		return nil, CardOutput{}, fmt.Errorf("parse frontmatter: %w", err)
	}
	out := CardOutput{
		Board: ref.Label(), Column: col.Name, Name: name,
		Raw: raw, Body: body, Frontmatter: objectOrEmpty(parsed.Data), Revision: revision(raw),
	}
	return textf("read %s from [%s] %s at revision %s", out.Name, out.Board, out.Column, out.Revision), out, nil
}

type SearchCardsInput struct {
	Board   string   `json:"board" jsonschema:"friendly name of the board"`
	Query   string   `json:"query,omitempty" jsonschema:"case-insensitive text to find in card names, bodies, tags, or frontmatter; empty matches all cards"`
	Columns []string `json:"columns,omitempty" jsonschema:"optional column names to search; defaults to every column"`
}

type SearchCardEntry struct {
	Column      string         `json:"column"`
	Name        string         `json:"name"`
	Body        string         `json:"body"`
	Frontmatter map[string]any `json:"frontmatter"`
	Revision    string         `json:"revision"`
}

type SearchCardsOutput struct {
	Board string            `json:"board"`
	Cards []SearchCardEntry `json:"cards"`
}

func searchCards(ctx context.Context, req *mcp.CallToolRequest, in SearchCardsInput, allowReads bool) (*mcp.CallToolResult, SearchCardsOutput, error) {
	if !allowReads {
		return nil, SearchCardsOutput{}, fmt.Errorf("card reads are disabled; set [mcp] allow_card_reads = true")
	}
	ref, err := resolveBoardForTool(ctx, req, in.Board)
	if err != nil {
		return nil, SearchCardsOutput{}, err
	}
	columns, err := selectedColumns(ref.Path, in.Columns)
	if err != nil {
		return nil, SearchCardsOutput{}, err
	}
	query := strings.ToLower(in.Query)
	items, err := board.ScanItems(ref.Path, func(item board.ScannedItem) bool {
		if len(columns) > 0 && !columns[item.Column] {
			return false
		}
		if query == "" {
			return true
		}
		front, _, _ := frontmatter.Split(item.Raw)
		haystack := strings.ToLower(strings.Join([]string{
			item.Name, item.Body, front, strings.Join(item.Frontmatter.Tags, " "),
		}, "\n"))
		return strings.Contains(haystack, query)
	})
	if err != nil {
		return nil, SearchCardsOutput{}, err
	}
	out := SearchCardsOutput{Board: ref.Label(), Cards: make([]SearchCardEntry, 0, len(items))}
	for _, item := range items {
		out.Cards = append(out.Cards, SearchCardEntry{
			Column: item.Column, Name: item.Name, Body: item.Body,
			Frontmatter: objectOrEmpty(item.Frontmatter.Data), Revision: revision(item.Raw),
		})
	}
	return textf("found %d card(s) in [%s]", len(out.Cards), out.Board), out, nil
}

type UpdateCardInput struct {
	Board            string `json:"board" jsonschema:"friendly name of the board"`
	Column           string `json:"column" jsonschema:"column containing the card"`
	Name             string `json:"name" jsonschema:"card name (the .md extension is optional)"`
	Content          string `json:"content" jsonschema:"replacement raw Markdown, including any frontmatter"`
	ExpectedRevision string `json:"expected_revision" jsonschema:"revision returned by get_card or search_cards; the update fails if the card changed"`
}

type MutationOutput struct {
	Board    string `json:"board"`
	Column   string `json:"column"`
	Name     string `json:"name"`
	Revision string `json:"revision,omitempty"`
}

func updateCard(ctx context.Context, req *mcp.CallToolRequest, in UpdateCardInput) (*mcp.CallToolResult, MutationOutput, error) {
	ref, col, err := resolveCardLocation(ctx, req, in.Board, in.Column)
	if err != nil {
		return nil, MutationOutput{}, err
	}
	name, err := board.SanitizeName(in.Name)
	if err != nil {
		return nil, MutationOutput{}, err
	}
	if err := checkRevision(col.Path, name, in.ExpectedRevision); err != nil {
		return nil, MutationOutput{}, err
	}
	if err := board.WriteItem(col.Path, name, in.Content); err != nil {
		return nil, MutationOutput{}, err
	}
	raw, err := board.ReadItem(col.Path, name)
	if err != nil {
		return nil, MutationOutput{}, err
	}
	out := MutationOutput{Board: ref.Label(), Column: col.Name, Name: name, Revision: revision(raw)}
	return textf("updated %s in [%s] %s at revision %s", out.Name, out.Board, out.Column, out.Revision), out, nil
}

type MoveCardInput struct {
	Board       string `json:"board" jsonschema:"friendly name of the board"`
	Column      string `json:"column" jsonschema:"current column containing the card"`
	Name        string `json:"name" jsonschema:"card name (the .md extension is optional)"`
	Destination string `json:"destination" jsonschema:"existing destination column"`
}

func moveCard(ctx context.Context, req *mcp.CallToolRequest, in MoveCardInput) (*mcp.CallToolResult, MutationOutput, error) {
	ref, src, err := resolveCardLocation(ctx, req, in.Board, in.Column)
	if err != nil {
		return nil, MutationOutput{}, err
	}
	dstPath, err := resolveColumnForTool(ctx, req, ref, in.Destination, false, false)
	if err != nil {
		return nil, MutationOutput{}, err
	}
	dst := boardops.ColumnRef{Name: columnName(dstPath), Path: dstPath}
	name, err := board.SanitizeName(in.Name)
	if err != nil {
		return nil, MutationOutput{}, err
	}
	if _, err := boardops.MoveItem(src, dst, name); err != nil {
		return nil, MutationOutput{}, err
	}
	out := MutationOutput{Board: ref.Label(), Column: dst.Name, Name: name}
	return textf("moved %s in [%s] from %s to %s", name, out.Board, src.Name, dst.Name), out, nil
}

type RenameCardInput struct {
	Board   string `json:"board" jsonschema:"friendly name of the board"`
	Column  string `json:"column" jsonschema:"column containing the card"`
	Name    string `json:"name" jsonschema:"current card name (the .md extension is optional)"`
	NewName string `json:"new_name" jsonschema:"new card name (the .md extension is optional); must not already exist"`
}

func renameCard(ctx context.Context, req *mcp.CallToolRequest, in RenameCardInput) (*mcp.CallToolResult, MutationOutput, error) {
	ref, col, err := resolveCardLocation(ctx, req, in.Board, in.Column)
	if err != nil {
		return nil, MutationOutput{}, err
	}
	name, err := board.SanitizeName(in.Name)
	if err != nil {
		return nil, MutationOutput{}, err
	}
	newName, err := board.SanitizeName(in.NewName)
	if err != nil {
		return nil, MutationOutput{}, err
	}
	if _, err := boardops.RenameItem(col, name, newName); err != nil {
		return nil, MutationOutput{}, err
	}
	out := MutationOutput{Board: ref.Label(), Column: col.Name, Name: newName}
	return textf("renamed %s to %s in [%s] %s", name, newName, out.Board, col.Name), out, nil
}

type DeleteCardInput struct {
	Board            string `json:"board" jsonschema:"friendly name of the board"`
	Column           string `json:"column" jsonschema:"column containing the card"`
	Name             string `json:"name" jsonschema:"card name (the .md extension is optional)"`
	ExpectedRevision string `json:"expected_revision" jsonschema:"revision returned by get_card or search_cards; deletion fails if the card changed"`
}

func deleteCard(ctx context.Context, req *mcp.CallToolRequest, in DeleteCardInput) (*mcp.CallToolResult, MutationOutput, error) {
	ref, col, err := resolveCardLocation(ctx, req, in.Board, in.Column)
	if err != nil {
		return nil, MutationOutput{}, err
	}
	name, err := board.SanitizeName(in.Name)
	if err != nil {
		return nil, MutationOutput{}, err
	}
	if err := checkRevision(col.Path, name, in.ExpectedRevision); err != nil {
		return nil, MutationOutput{}, err
	}
	if _, err := boardops.DeleteItem(col, name); err != nil {
		return nil, MutationOutput{}, err
	}
	out := MutationOutput{Board: ref.Label(), Column: col.Name, Name: name}
	return textf("deleted %s from [%s] %s", name, out.Board, col.Name), out, nil
}

type CreateColumnInput struct {
	Board string `json:"board" jsonschema:"friendly name of the board"`
	Name  string `json:"name" jsonschema:"name of the new column"`
}

type CreateColumnOutput struct {
	Board  string `json:"board"`
	Column string `json:"column"`
}

func createColumn(ctx context.Context, req *mcp.CallToolRequest, in CreateColumnInput) (*mcp.CallToolResult, CreateColumnOutput, error) {
	ref, err := resolveBoardForTool(ctx, req, in.Board)
	if err != nil {
		return nil, CreateColumnOutput{}, err
	}
	col, err := boardops.CreateColumn(ref.Path, in.Name)
	if err != nil {
		return nil, CreateColumnOutput{}, err
	}
	out := CreateColumnOutput{Board: ref.Label(), Column: col.Name}
	return textf("created column %s in [%s]", out.Column, out.Board), out, nil
}

func resolveCardLocation(ctx context.Context, req *mcp.CallToolRequest, boardName, column string) (board.Ref, boardops.ColumnRef, error) {
	ref, err := resolveBoardForTool(ctx, req, boardName)
	if err != nil {
		return board.Ref{}, boardops.ColumnRef{}, err
	}
	path, err := resolveColumnForTool(ctx, req, ref, column, false, false)
	if err != nil {
		return board.Ref{}, boardops.ColumnRef{}, err
	}
	return ref, boardops.ColumnRef{Name: columnName(path), Path: path}, nil
}

func selectedColumns(boardPath string, requested []string) (map[string]bool, error) {
	if len(requested) == 0 {
		return nil, nil
	}
	available, err := board.Columns(boardPath)
	if err != nil {
		return nil, err
	}
	selected := make(map[string]bool, len(requested))
	for _, requestedColumn := range requested {
		match := ""
		for _, availableColumn := range available {
			if strings.EqualFold(requestedColumn, availableColumn) {
				match = availableColumn
				break
			}
		}
		if match == "" {
			return nil, fmt.Errorf("column %q not found; available: %v", requestedColumn, available)
		}
		selected[match] = true
	}
	return selected, nil
}

func checkRevision(columnPath, name, expected string) error {
	if expected == "" {
		return fmt.Errorf("expected_revision is required")
	}
	raw, err := board.ReadItem(columnPath, name)
	if err != nil {
		return err
	}
	current := revision(raw)
	if current != expected {
		return fmt.Errorf("revision conflict: card is at %s, expected %s; read it again before retrying", current, expected)
	}
	return nil
}

func revision(raw string) string {
	sum := sha256.Sum256([]byte(raw))
	return hex.EncodeToString(sum[:])
}

// objectOrEmpty keeps structured MCP output aligned with its generated JSON
// schema: absent frontmatter is an empty object, never JSON null.
func objectOrEmpty(data map[string]any) map[string]any {
	if data == nil {
		return map[string]any{}
	}
	return data
}

func columnName(path string) string {
	return filepath.Base(path)
}
