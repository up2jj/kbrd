package mcp

import (
	"context"
	"errors"
	"fmt"

	"github.com/modelcontextprotocol/go-sdk/mcp"

	"kbrd/config"
)

// resolveCommandForTool returns an exact command match, or asks an
// elicitation-capable client to replace an unknown ID with one of the commands
// allowed by the already-applied loading policy.
func resolveCommandForTool(ctx context.Context, req *mcp.CallToolRequest, requested string, commands []config.Command) (*config.Command, error) {
	ids := make([]string, 0, len(commands))
	for i := range commands {
		ids = append(ids, commands[i].ID)
		if commands[i].ID == requested {
			return &commands[i], nil
		}
	}
	unknownErr := fmt.Errorf("unknown command %q; available: %v", requested, ids)
	if len(commands) == 0 {
		return nil, unknownErr
	}

	choices := make([]elicitationChoice, 0, len(commands))
	for _, command := range commands {
		title := command.Name
		if command.Description != "" {
			title += " — " + command.Description
		}
		choices = append(choices, elicitationChoice{Value: command.ID, Title: title})
	}
	choice, err := elicitChoice(ctx, req,
		fmt.Sprintf("Command %q is not available. Choose a command to run.", requested), choices)
	if errors.Is(err, errElicitationUnsupported) {
		return nil, unknownErr
	}
	if err != nil {
		return nil, fmt.Errorf("choose command: %w", err)
	}
	for i := range commands {
		if commands[i].ID == choice {
			return &commands[i], nil
		}
	}
	return nil, fmt.Errorf("MCP client selected unknown command %q", choice)
}
