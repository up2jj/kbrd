package mcpsetup

import (
	"fmt"
	"os"
	"path/filepath"
	"strings"

	toml "github.com/pelletier/go-toml/v2"

	"kbrd/mcp"
)

type codexAdapter struct{}

func (codexAdapter) Name() string       { return ClientCodex }
func (codexAdapter) Executable() string { return "codex" }

func (codexAdapter) Configure(ctx runContext, binary string) (Result, error) {
	return configureCodex(ctx.addr, binary, ctx.opts.Force, ctx.opts.DryRun, ctx.opts.Run)
}

func configureCodex(addr, binary string, force, dryRun bool, run func(string, ...string) error) (Result, error) {
	path, err := codexConfigPath()
	if err != nil {
		return Result{}, err
	}
	data, exists, err := readOptional(path)
	if err != nil {
		return Result{}, err
	}
	if exists {
		if err := validateTOML(path, data); err != nil {
			return Result{}, err
		}
	}
	url := "http://" + addr
	entry, ok, err := codexEntry(data)
	if err != nil {
		return Result{}, fmt.Errorf("%s: %w", path, err)
	}
	if ok {
		if entry.URL == url {
			return Result{Target: ClientCodex, Status: StatusConfigured, Path: path, Detail: "already configured (" + binary + ")"}, nil
		}
		if !force {
			return Result{Target: ClientCodex, Status: StatusSkipped, Path: path, Detail: "existing kbrd entry preserved; pass --force to replace"}, nil
		}
	}

	if dryRun {
		return Result{Target: ClientCodex, Status: StatusFound, Path: path, Detail: "would configure Codex MCP (" + binary + ")"}, nil
	}
	if ok && force {
		if err := run(binary, "mcp", "remove", mcp.ServerName); err != nil {
			return Result{}, fmt.Errorf("codex mcp remove %s: %w", mcp.ServerName, err)
		}
	}
	if err := run(binary, "mcp", "add", mcp.ServerName, "--url", url); err != nil {
		return Result{}, fmt.Errorf("codex mcp add %s: %w", mcp.ServerName, err)
	}
	return Result{Target: ClientCodex, Status: StatusConfigured, Path: path, Detail: "Codex MCP configured (" + binary + ")"}, nil
}

func codexConfigPath() (string, error) {
	if home := os.Getenv("CODEX_HOME"); home != "" {
		return filepath.Join(home, "config.toml"), nil
	}
	home, err := os.UserHomeDir()
	if err != nil {
		return "", fmt.Errorf("home dir: %w", err)
	}
	return filepath.Join(home, ".codex", "config.toml"), nil
}

type codexServerEntry struct {
	URL string
}

func codexEntry(data []byte) (codexServerEntry, bool, error) {
	if strings.TrimSpace(string(data)) == "" {
		return codexServerEntry{}, false, nil
	}
	var cfg struct {
		MCPServers map[string]codexServerEntry `toml:"mcp_servers"`
	}
	if err := toml.Unmarshal(data, &cfg); err != nil {
		return codexServerEntry{}, false, err
	}
	entry, ok := cfg.MCPServers[mcp.ServerName]
	return entry, ok, nil
}
