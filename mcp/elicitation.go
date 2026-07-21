package mcp

import (
	"context"
	"errors"
	"fmt"
	"path/filepath"
	"time"

	"github.com/modelcontextprotocol/go-sdk/mcp"

	"kbrd/board"
)

const elicitationTimeout = 2 * time.Minute

var (
	errElicitationUnsupported = errors.New("MCP client does not support form elicitation")
	errElicitationDeclined    = errors.New("user declined elicitation")
	errElicitationCanceled    = errors.New("user canceled elicitation")
	errElicitationTimedOut    = errors.New("elicitation timed out")
)

// elicitChoice asks the user to choose one of the supplied values. The MCP SDK
// validates accepted content against this schema before returning it.
func elicitChoice(ctx context.Context, req *mcp.CallToolRequest, message string, choices []string) (string, error) {
	if !supportsFormElicitation(req) {
		return "", errElicitationUnsupported
	}
	if len(choices) == 0 {
		return "", errors.New("elicitation requires at least one choice")
	}

	elicitCtx, cancel := context.WithTimeout(ctx, elicitationTimeout)
	defer cancel()
	res, err := req.Session.Elicit(elicitCtx, &mcp.ElicitParams{
		Mode:    "form",
		Message: message,
		RequestedSchema: map[string]any{
			"type": "object",
			"properties": map[string]any{
				"choice": map[string]any{
					"type": "string",
					"enum": choices,
				},
			},
			"required": []string{"choice"},
		},
	})
	if err != nil {
		if errors.Is(elicitCtx.Err(), context.DeadlineExceeded) {
			return "", fmt.Errorf("%w after %s", errElicitationTimedOut, elicitationTimeout)
		}
		return "", fmt.Errorf("request user choice: %w", err)
	}
	if res == nil {
		return "", errors.New("MCP client returned an empty elicitation response")
	}

	switch res.Action {
	case "accept":
		choice, ok := res.Content["choice"].(string)
		if !ok || choice == "" {
			return "", errors.New("MCP client accepted elicitation without a choice")
		}
		return choice, nil
	case "decline":
		return "", errElicitationDeclined
	case "cancel":
		return "", errElicitationCanceled
	default:
		return "", fmt.Errorf("unknown elicitation action %q", res.Action)
	}
}

func supportsFormElicitation(req *mcp.CallToolRequest) bool {
	if req == nil || req.Session == nil {
		return false
	}
	init := req.Session.InitializeParams()
	if init == nil || init.Capabilities == nil || init.Capabilities.Elicitation == nil {
		return false
	}
	caps := init.Capabilities.Elicitation
	// An empty elicitation capability means form mode for compatibility. A
	// URL-only declaration explicitly excludes form mode.
	return caps.Form != nil || caps.URL == nil
}

// resolveBoardForTool resolves a board normally, eliciting only when the board
// package reports structured ambiguity. Clients without form support receive
// the original ambiguity error so existing agent-driven recovery still works.
func resolveBoardForTool(ctx context.Context, req *mcp.CallToolRequest, name string) (board.Ref, error) {
	ref, err := board.Resolve(name)
	if err == nil {
		return ref, nil
	}
	var ambiguous *board.AmbiguousBoardError
	if !errors.As(err, &ambiguous) {
		return board.Ref{}, err
	}

	candidates := uniqueBoardCandidates(ambiguous.Candidates)
	if len(candidates) == 1 {
		return candidates[0], nil
	}
	paths := make([]string, 0, len(candidates))
	for _, candidate := range candidates {
		paths = append(paths, candidate.Path)
	}
	choice, choiceErr := elicitChoice(ctx, req,
		fmt.Sprintf("Several boards match %q. Choose the intended board path.", name), paths)
	if errors.Is(choiceErr, errElicitationUnsupported) {
		return board.Ref{}, err
	}
	if choiceErr != nil {
		return board.Ref{}, fmt.Errorf("choose board: %w", choiceErr)
	}
	for _, candidate := range candidates {
		if candidate.Path == choice {
			return candidate, nil
		}
	}
	return board.Ref{}, fmt.Errorf("MCP client selected unknown board path %q", choice)
}

func uniqueBoardCandidates(candidates []board.Ref) []board.Ref {
	unique := make([]board.Ref, 0, len(candidates))
	seen := make(map[string]bool, len(candidates))
	for _, candidate := range candidates {
		if seen[candidate.Path] {
			continue
		}
		seen[candidate.Path] = true
		unique = append(unique, candidate)
	}
	return unique
}

// resolveColumnForTool retains the existing default and auto-create behavior.
// For an invalid named folder, it lets an elicitation-capable client choose an
// existing folder and otherwise returns the original error.
func resolveColumnForTool(ctx context.Context, req *mcp.CallToolRequest, ref board.Ref, folder string, autoCreate bool) (string, error) {
	path, err := board.ResolveColumn(ref.Path, folder, autoCreate)
	if err == nil || !errors.Is(err, board.ErrFolderNotFound) || autoCreate {
		return path, err
	}

	columns, columnsErr := board.Columns(ref.Path)
	if columnsErr != nil || len(columns) == 0 {
		return "", err
	}
	choice, choiceErr := elicitChoice(ctx, req,
		fmt.Sprintf("Folder %q does not exist in board %q. Choose an existing folder.", folder, ref.Label()), columns)
	if errors.Is(choiceErr, errElicitationUnsupported) {
		return "", err
	}
	if choiceErr != nil {
		return "", fmt.Errorf("choose folder: %w", choiceErr)
	}
	for _, column := range columns {
		if column == choice {
			return filepath.Join(ref.Path, column), nil
		}
	}
	return "", fmt.Errorf("MCP client selected unknown folder %q", choice)
}
