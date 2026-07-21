package mcp

import (
	"context"
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"strings"
	"time"

	"github.com/modelcontextprotocol/go-sdk/mcp"

	"kbrd/board"
	"kbrd/config"
	"kbrd/shellcmd"
)

// commandTimeout caps how long a custom command may run. Commands run with
// stdin from /dev/null, so interactive prompts return immediately rather than
// blocking the server.
const commandTimeout = 60 * time.Second
const maxCommandOutputBytes = 64 * 1024

type ListCommandsInput struct {
	Board string `json:"board" jsonschema:"friendly name of the board (folder-local commands depend on it)"`
}

type CommandEntry struct {
	ID          string `json:"id"`
	Name        string `json:"name"`
	Description string `json:"description"`
}

type ListCommandsOutput struct {
	Board    string         `json:"board"`
	Commands []CommandEntry `json:"commands"`
	Warnings []string       `json:"warnings,omitempty"`
}

func listCustomCommands(ctx context.Context, _ *mcp.CallToolRequest, in ListCommandsInput) (*mcp.CallToolResult, ListCommandsOutput, error) {
	return listCustomCommandsWithPolicy(ctx, nil, in, Policy{AllowFolderCommands: true})
}

func listCustomCommandsWithPolicy(ctx context.Context, _ *mcp.CallToolRequest, in ListCommandsInput, policy Policy) (*mcp.CallToolResult, ListCommandsOutput, error) {
	ref, err := board.Resolve(in.Board)
	if err != nil {
		return nil, ListCommandsOutput{}, err
	}
	// LoadCommands returns only shell commands (from YAML). Lua-registered
	// commands live in the running TUI's script VM and are not available to
	// the headless MCP server, so they are not listed.
	cmds, warnings, err := config.LoadCommandsWithOptions(ref.Path, config.CommandLoadOptions{
		IncludeFolder: policy.AllowFolderCommands,
	})
	if err != nil {
		return nil, ListCommandsOutput{}, err
	}
	out := ListCommandsOutput{Board: ref.Label(), Commands: make([]CommandEntry, 0, len(cmds))}
	for _, c := range cmds {
		out.Commands = append(out.Commands, CommandEntry{ID: c.ID, Name: c.Name, Description: c.Description})
	}
	for _, w := range warnings {
		out.Warnings = append(out.Warnings, w.Source+": "+w.Message)
	}
	return textf("%d command(s) in [%s]", len(out.Commands), out.Board), out, nil
}

type RunCommandInput struct {
	Board   string `json:"board" jsonschema:"friendly name of the board"`
	Command string `json:"command" jsonschema:"the command id to run (see list_custom_commands)"`
	Folder  string `json:"folder,omitempty" jsonschema:"folder (column) context; defaults to the first folder when an item is given"`
	Item    string `json:"item,omitempty" jsonschema:"item name for commands that use file variables (filePath, fileName, fileDir)"`
}

type RunCommandOutput struct {
	Command  string `json:"command"`
	ExitCode int    `json:"exit_code"`
	Output   string `json:"output"`
}

func runCustomCommand(ctx context.Context, _ *mcp.CallToolRequest, in RunCommandInput) (*mcp.CallToolResult, RunCommandOutput, error) {
	return runCustomCommandWithPolicy(ctx, nil, in, Policy{AllowCommands: true, AllowFolderCommands: true})
}

func runCustomCommandWithPolicy(ctx context.Context, _ *mcp.CallToolRequest, in RunCommandInput, policy Policy) (*mcp.CallToolResult, RunCommandOutput, error) {
	if !policy.AllowCommands {
		return nil, RunCommandOutput{}, errors.New("run_custom_command is disabled; set [mcp] allow_commands = true and do not use --safe to enable shell command execution")
	}
	ref, err := board.Resolve(in.Board)
	if err != nil {
		return nil, RunCommandOutput{}, err
	}

	cmds, _, err := config.LoadCommandsWithOptions(ref.Path, config.CommandLoadOptions{
		IncludeFolder: policy.AllowFolderCommands,
	})
	if err != nil {
		return nil, RunCommandOutput{}, err
	}
	var cmd *config.Command
	ids := make([]string, 0, len(cmds))
	for i := range cmds {
		ids = append(ids, cmds[i].ID)
		if cmds[i].ID == in.Command {
			cmd = &cmds[i]
		}
	}
	if cmd == nil {
		return nil, RunCommandOutput{}, fmt.Errorf("unknown command %q; available: %v", in.Command, ids)
	}
	if cmd.Source != config.SourceShell {
		return nil, RunCommandOutput{}, fmt.Errorf("command %q is a Lua command and can only be run from the kbrd TUI", in.Command)
	}

	vars, err := commandVars(ref, in.Folder, in.Item)
	if err != nil {
		return nil, RunCommandOutput{}, err
	}
	rendered, err := cmd.Render(vars)
	if err != nil {
		// missingkey=error surfaces here when the template needs a variable
		// (e.g. filePath) that wasn't supplied (no item/folder given).
		return nil, RunCommandOutput{}, fmt.Errorf("cannot run %q: %w", in.Command, err)
	}

	runCtx, cancel := context.WithTimeout(ctx, commandTimeout)
	defer cancel()
	res, runErr := shellcmd.Run(runCtx, ref.Path, rendered)
	if runErr != nil {
		if errors.Is(runErr, shellcmd.ErrTimeout) {
			return nil, RunCommandOutput{}, fmt.Errorf("command %q timed out after %s", in.Command, commandTimeout)
		}
		return nil, RunCommandOutput{}, fmt.Errorf("run %q: %w", in.Command, runErr)
	}

	out := RunCommandOutput{Command: cmd.ID, Output: limitCommandOutput(res.Output), ExitCode: res.ExitCode}
	if res.ExitCode != 0 {
		// Non-zero exit is a command result, not a tool failure: report it.
		return textf("%s exited %d\n%s", cmd.Name, out.ExitCode, out.Output), out, nil
	}
	return textf("%s finished\n%s", cmd.Name, out.Output), out, nil
}

func limitCommandOutput(out string) string {
	if len(out) <= maxCommandOutputBytes {
		return out
	}
	return out[:maxCommandOutputBytes] + fmt.Sprintf("\n[output truncated to %d bytes]", maxCommandOutputBytes)
}

// commandVars builds the template variables for a custom command, mirroring
// model.Board.buildCommandVars but headlessly. Column variables are added when
// a folder (or item) is in play; file variables when an item is given.
func commandVars(ref board.Ref, folder, item string) (map[string]string, error) {
	vc := board.VarContext{BoardPath: ref.Path, BoardName: ref.Name}
	if folder == "" && item == "" {
		return vc.Vars(), nil
	}

	colPath, err := board.ResolveColumn(ref.Path, folder, false)
	if err != nil {
		return nil, err
	}
	vc.ColumnPath = colPath
	vc.ColumnName = filepath.Base(colPath)

	if item != "" {
		fp, err := board.ItemPath(colPath, item)
		if err != nil {
			return nil, err
		}
		if _, err := os.Stat(fp); err != nil {
			return nil, fmt.Errorf("item %q not found in folder %q", item, filepath.Base(colPath))
		}
		vc.FilePath = fp
		vc.FileName = strings.TrimSuffix(filepath.Base(fp), ".md")
	}
	return vc.Vars(), nil
}
