package mcp

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"net/url"
	"os"
	"path/filepath"
	"strings"

	"github.com/modelcontextprotocol/go-sdk/mcp"

	"kbrd/board"
)

const (
	boardsResourceURI    = "kbrd://boards"
	boardResourceTmpl    = "kbrd://board/{board}"
	cardResourceTmpl     = "kbrd://card/{board}/{column}/{card}"
	resourceSchemaV1     = 1
	jsonResourceMIME     = "application/json"
	markdownResourceMIME = "text/markdown; charset=utf-8"
)

type boardsResource struct {
	SchemaVersion int                  `json:"schema_version"`
	Boards        []resourceBoardEntry `json:"boards"`
}

type resourceBoardEntry struct {
	Name      string `json:"name"`
	URI       string `json:"uri"`
	Pinned    bool   `json:"pinned"`
	Available bool   `json:"available"`
	Ambiguous bool   `json:"ambiguous,omitempty"`
}

type boardResource struct {
	SchemaVersion int                   `json:"schema_version"`
	Name          string                `json:"name"`
	URI           string                `json:"uri"`
	Columns       []resourceColumnEntry `json:"columns"`
}

type resourceColumnEntry struct {
	Name  string              `json:"name"`
	Cards []resourceCardEntry `json:"cards"`
}

type resourceCardEntry struct {
	Name string `json:"name"`
	URI  string `json:"uri,omitempty"`
}

func registerResources(s *mcp.Server, policy Policy) {
	s.AddResource(&mcp.Resource{
		URI:         boardsResourceURI,
		Name:        "boards",
		Title:       "kbrd boards",
		Description: "Boards known to kbrd, including availability and board resource URIs.",
		MIMEType:    jsonResourceMIME,
	}, readBoardsResource)

	s.AddResourceTemplate(&mcp.ResourceTemplate{
		URITemplate: boardResourceTmpl,
		Name:        "board",
		Title:       "kbrd board",
		Description: "A current board snapshot containing its columns, cards, and card resource URIs when card reads are enabled.",
		MIMEType:    jsonResourceMIME,
	}, func(ctx context.Context, req *mcp.ReadResourceRequest) (*mcp.ReadResourceResult, error) {
		return readBoardResource(ctx, req, policy.AllowCardReads)
	})

	if policy.AllowCardReads {
		s.AddResourceTemplate(&mcp.ResourceTemplate{
			URITemplate: cardResourceTmpl,
			Name:        "card",
			Title:       "kbrd card",
			Description: "The complete Markdown content of a kbrd card, including YAML frontmatter.",
			MIMEType:    markdownResourceMIME,
		}, readCardResource)
	}
}

func readBoardsResource(_ context.Context, req *mcp.ReadResourceRequest) (*mcp.ReadResourceResult, error) {
	if req == nil || req.Params == nil || req.Params.URI != boardsResourceURI {
		return nil, resourceNotFound(req)
	}
	refs, err := board.ListBoards()
	if err != nil {
		return nil, fmt.Errorf("list boards for resource: %w", err)
	}
	out := boardsResource{SchemaVersion: resourceSchemaV1, Boards: make([]resourceBoardEntry, 0, len(refs))}
	labelCount := make(map[string]int, len(refs))
	for _, ref := range refs {
		labelCount[ref.Label()]++
	}
	for _, ref := range refs {
		info, statErr := os.Stat(ref.Path)
		ambiguous := labelCount[ref.Label()] > 1
		out.Boards = append(out.Boards, resourceBoardEntry{
			Name:      ref.Label(),
			URI:       boardResourceURI(ref.Label()),
			Pinned:    ref.Pinned,
			Available: statErr == nil && info.IsDir() && !ambiguous,
			Ambiguous: ambiguous,
		})
	}
	return jsonResourceResult(boardsResourceURI, out)
}

func readBoardResource(_ context.Context, req *mcp.ReadResourceRequest, includeCardURIs bool) (*mcp.ReadResourceResult, error) {
	uri := requestResourceURI(req)
	parts, err := parseResourceURI(uri, "board", 1)
	if err != nil {
		return nil, mcp.ResourceNotFoundError(uri)
	}
	if uri != boardResourceURI(parts[0]) {
		return nil, mcp.ResourceNotFoundError(uri)
	}
	ref, err := resolveBoardExact(parts[0])
	if err != nil {
		return nil, mcp.ResourceNotFoundError(uri)
	}
	if info, err := os.Stat(ref.Path); err != nil || !info.IsDir() {
		return nil, mcp.ResourceNotFoundError(uri)
	}

	columns, err := board.Columns(ref.Path)
	if err != nil {
		return nil, fmt.Errorf("list columns for board resource %q: %w", ref.Label(), err)
	}
	out := boardResource{
		SchemaVersion: resourceSchemaV1,
		Name:          ref.Label(),
		URI:           boardResourceURI(ref.Label()),
		Columns:       make([]resourceColumnEntry, 0, len(columns)),
	}
	for _, column := range columns {
		items, err := board.Items(filepath.Join(ref.Path, column))
		if err != nil {
			return nil, fmt.Errorf("list cards in column %q: %w", column, err)
		}
		entry := resourceColumnEntry{Name: column, Cards: make([]resourceCardEntry, 0, len(items))}
		for _, item := range items {
			card := resourceCardEntry{Name: item}
			if includeCardURIs {
				card.URI = cardResourceURI(ref.Label(), column, item)
			}
			entry.Cards = append(entry.Cards, card)
		}
		out.Columns = append(out.Columns, entry)
	}
	return jsonResourceResult(uri, out)
}

func readCardResource(_ context.Context, req *mcp.ReadResourceRequest) (*mcp.ReadResourceResult, error) {
	uri := requestResourceURI(req)
	parts, err := parseResourceURI(uri, "card", 3)
	if err != nil {
		return nil, mcp.ResourceNotFoundError(uri)
	}
	if uri != cardResourceURI(parts[0], parts[1], parts[2]) {
		return nil, mcp.ResourceNotFoundError(uri)
	}
	ref, err := resolveBoardExact(parts[0])
	if err != nil {
		return nil, mcp.ResourceNotFoundError(uri)
	}
	columnPath, err := resolveColumnExact(ref.Path, parts[1])
	if err != nil {
		return nil, mcp.ResourceNotFoundError(uri)
	}
	found, err := containsExactCard(columnPath, parts[2])
	if err != nil {
		return nil, fmt.Errorf("list cards for resource: %w", err)
	}
	if !found {
		return nil, mcp.ResourceNotFoundError(uri)
	}
	content, err := board.ReadItem(columnPath, parts[2])
	if err != nil {
		if errors.Is(err, os.ErrNotExist) {
			return nil, mcp.ResourceNotFoundError(uri)
		}
		return nil, fmt.Errorf("read card resource: %w", err)
	}
	return &mcp.ReadResourceResult{Contents: []*mcp.ResourceContents{{
		URI: uri, MIMEType: markdownResourceMIME, Text: content,
	}}}, nil
}

func resolveBoardExact(name string) (board.Ref, error) {
	refs, err := board.ListBoards()
	if err != nil {
		return board.Ref{}, err
	}
	var match board.Ref
	found := false
	for _, ref := range refs {
		if ref.Label() != name {
			continue
		}
		if found {
			return board.Ref{}, fmt.Errorf("board label %q is ambiguous", name)
		}
		match, found = ref, true
	}
	if !found {
		return board.Ref{}, board.ErrBoardNotFound
	}
	return match, nil
}

func resolveColumnExact(boardPath, name string) (string, error) {
	columns, err := board.Columns(boardPath)
	if err != nil {
		return "", err
	}
	for _, column := range columns {
		if column == name {
			return filepath.Join(boardPath, column), nil
		}
	}
	return "", board.ErrFolderNotFound
}

func containsExactCard(columnPath, name string) (bool, error) {
	items, err := board.Items(columnPath)
	if err != nil {
		return false, err
	}
	for _, item := range items {
		if item == name {
			return true, nil
		}
	}
	return false, nil
}

func jsonResourceResult(uri string, value any) (*mcp.ReadResourceResult, error) {
	data, err := json.Marshal(value)
	if err != nil {
		return nil, fmt.Errorf("encode resource %s: %w", uri, err)
	}
	return &mcp.ReadResourceResult{Contents: []*mcp.ResourceContents{{
		URI: uri, MIMEType: jsonResourceMIME, Text: string(data),
	}}}, nil
}

func parseResourceURI(raw, host string, segmentCount int) ([]string, error) {
	u, err := url.Parse(raw)
	if err != nil || u.Scheme != "kbrd" || u.Host != host || u.User != nil || u.RawQuery != "" || u.Fragment != "" || u.Opaque != "" {
		return nil, errors.New("invalid kbrd resource URI")
	}
	escaped := strings.TrimPrefix(u.EscapedPath(), "/")
	parts := strings.Split(escaped, "/")
	if len(parts) != segmentCount {
		return nil, errors.New("invalid kbrd resource path")
	}
	for i, part := range parts {
		if part == "" {
			return nil, errors.New("empty kbrd resource path segment")
		}
		parts[i], err = url.PathUnescape(part)
		if err != nil || parts[i] == "" {
			return nil, errors.New("invalid kbrd resource path escape")
		}
	}
	return parts, nil
}

func boardResourceURI(name string) string {
	return "kbrd://board/" + url.PathEscape(name)
}

func cardResourceURI(boardName, column, card string) string {
	return "kbrd://card/" + url.PathEscape(boardName) + "/" + url.PathEscape(column) + "/" + url.PathEscape(card)
}

func requestResourceURI(req *mcp.ReadResourceRequest) string {
	if req == nil || req.Params == nil {
		return ""
	}
	return req.Params.URI
}

func resourceNotFound(req *mcp.ReadResourceRequest) error {
	return mcp.ResourceNotFoundError(requestResourceURI(req))
}
