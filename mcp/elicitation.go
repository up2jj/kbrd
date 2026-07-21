package mcp

import (
	"context"
	"errors"
	"fmt"
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

// elicitationChoice separates the stable value returned by the client from
// the title shown to the user. This lets board paths and other opaque IDs stay
// unambiguous without making the form difficult to read.
type elicitationChoice struct {
	Value string
	Title string
}

// elicitChoice asks the user to choose one of the supplied values. The MCP SDK
// validates accepted content against this schema before returning it.
func elicitChoice(ctx context.Context, req *mcp.CallToolRequest, message string, choices []elicitationChoice) (string, error) {
	if !supportsFormElicitation(req) {
		return "", errElicitationUnsupported
	}
	schema, err := choiceSchema(choices)
	if err != nil {
		return "", err
	}

	elicitCtx, cancel := context.WithTimeout(ctx, elicitationTimeout)
	defer cancel()
	res, err := req.Session.Elicit(elicitCtx, &mcp.ElicitParams{
		Mode:            "form",
		Message:         message,
		RequestedSchema: schema,
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
		for _, candidate := range choices {
			if candidate.Value == choice {
				return choice, nil
			}
		}
		return "", fmt.Errorf("MCP client selected unknown choice %q", choice)
	case "decline":
		return "", errElicitationDeclined
	case "cancel":
		return "", errElicitationCanceled
	default:
		return "", fmt.Errorf("unknown elicitation action %q", res.Action)
	}
}

func choiceSchema(choices []elicitationChoice) (map[string]any, error) {
	if len(choices) == 0 {
		return nil, errors.New("elicitation requires at least one choice")
	}
	oneOf := make([]map[string]any, 0, len(choices))
	seen := make(map[string]bool, len(choices))
	for _, choice := range choices {
		if choice.Value == "" {
			return nil, errors.New("elicitation choice value cannot be empty")
		}
		if seen[choice.Value] {
			return nil, fmt.Errorf("duplicate elicitation choice %q", choice.Value)
		}
		seen[choice.Value] = true
		title := choice.Title
		if title == "" {
			title = choice.Value
		}
		oneOf = append(oneOf, map[string]any{"const": choice.Value, "title": title})
	}
	return map[string]any{
		"type": "object",
		"properties": map[string]any{
			"choice": map[string]any{
				"type":  "string",
				"title": "Choice",
				"oneOf": oneOf,
			},
		},
		"required": []string{"choice"},
	}, nil
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
	choices := make([]elicitationChoice, 0, len(candidates))
	for _, candidate := range candidates {
		choices = append(choices, elicitationChoice{
			Value: candidate.Path,
			Title: fmt.Sprintf("%s — %s", candidate.Label(), candidate.Path),
		})
	}
	choice, choiceErr := elicitChoice(ctx, req,
		fmt.Sprintf("Several boards match %q. Choose the intended board.", name), choices)
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

// resolveColumnForTool retains the existing default and explicit auto-create
// behavior. For an invalid named folder, it lets an elicitation-capable client
// explicitly create that folder or choose an existing one.
func resolveColumnForTool(ctx context.Context, req *mcp.CallToolRequest, ref board.Ref, folder string, autoCreate, offerCreate bool) (string, error) {
	path, err := board.ResolveColumn(ref.Path, folder, autoCreate)
	if err == nil || !errors.Is(err, board.ErrFolderNotFound) || autoCreate {
		return path, err
	}

	columns, columnsErr := board.Columns(ref.Path)
	if columnsErr != nil {
		return "", err
	}
	type folderAction struct {
		name   string
		create bool
	}
	actions := make(map[string]folderAction, len(columns)+1)
	choices := make([]elicitationChoice, 0, len(columns)+1)
	if clean, sanitizeErr := board.SanitizeFolder(folder); offerCreate && sanitizeErr == nil {
		const value = "create"
		actions[value] = folderAction{name: clean, create: true}
		choices = append(choices, elicitationChoice{Value: value, Title: fmt.Sprintf("Create %q", clean)})
	}
	for i, column := range columns {
		value := fmt.Sprintf("folder:%d", i)
		actions[value] = folderAction{name: column}
		choices = append(choices, elicitationChoice{Value: value, Title: fmt.Sprintf("Use %q", column)})
	}
	if len(choices) == 0 {
		return "", err
	}
	message := fmt.Sprintf("Folder %q does not exist in board %q. Choose an existing folder.", folder, ref.Label())
	if offerCreate {
		message = fmt.Sprintf("Folder %q does not exist in board %q. Create it or choose an existing folder.", folder, ref.Label())
	}
	choice, choiceErr := elicitChoice(ctx, req, message, choices)
	if errors.Is(choiceErr, errElicitationUnsupported) {
		return "", err
	}
	if choiceErr != nil {
		return "", fmt.Errorf("choose folder: %w", choiceErr)
	}
	action, ok := actions[choice]
	if !ok {
		return "", fmt.Errorf("MCP client selected unknown folder action %q", choice)
	}
	if action.create {
		return board.ResolveColumn(ref.Path, action.name, true)
	}
	return board.ResolveColumn(ref.Path, action.name, false)
}
