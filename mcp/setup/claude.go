package setup

import (
	"encoding/json"
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"strings"

	"kbrd/mcp"
)

type claudeAdapter struct{}

func (claudeAdapter) Name() string       { return ClientClaude }
func (claudeAdapter) Executable() string { return "claude" }

func (claudeAdapter) Configure(ctx runContext, binary string) (Result, error) {
	return configureClaude(ctx.addr, binary, ctx.opts.Force, ctx.opts.DryRun, ctx.opts.Run)
}

func configureClaude(addr, binary string, force, dryRun bool, run func(string, ...string) error) (Result, error) {
	path, err := claudeConfigPath()
	if err != nil {
		return Result{}, err
	}
	data, exists, err := readOptional(path)
	if err != nil {
		return Result{}, err
	}
	if exists {
		if err := validateJSON(path, data); err != nil {
			return Result{}, err
		}
	}
	url := "http://" + addr
	entry, ok, err := claudeEntry(data)
	if err != nil {
		return Result{}, fmt.Errorf("%s: %w", path, err)
	}
	if ok {
		if entry.URL == url && (entry.Type == "http" || entry.Type == "streamable-http") {
			return Result{Target: ClientClaude, Status: StatusConfigured, Path: path, Detail: "already configured (" + binary + ")"}, nil
		}
		if !force {
			return Result{Target: ClientClaude, Status: StatusSkipped, Path: path, Detail: "existing kbrd entry preserved; pass --force to replace"}, nil
		}
	}
	if dryRun {
		return Result{Target: ClientClaude, Status: StatusFound, Path: path, Detail: "would configure Claude Code MCP (" + binary + ")"}, nil
	}
	if ok && force {
		next, err := writeClaudeEntry(data, url)
		if err != nil {
			return Result{}, fmt.Errorf("%s: %w", path, err)
		}
		if err := writeFile(path, next, exists); err != nil {
			return Result{}, fmt.Errorf("write %s: %w", path, err)
		}
		return Result{Target: ClientClaude, Status: StatusConfigured, Path: path, Detail: "Claude Code MCP configured (" + binary + ")"}, nil
	}
	if run != nil {
		if err := run(binary, "mcp", "add", "--transport", "http", "--scope", "user", mcp.ServerName, url); err == nil {
			after, afterExists, readErr := readOptional(path)
			if readErr != nil {
				return Result{}, readErr
			}
			if afterExists {
				afterEntry, afterOK, entryErr := claudeEntry(after)
				if entryErr != nil {
					return Result{}, fmt.Errorf("%s: %w", path, entryErr)
				}
				if afterOK && afterEntry.URL == url && (afterEntry.Type == "http" || afterEntry.Type == "streamable-http") {
					return Result{Target: ClientClaude, Status: StatusConfigured, Path: path, Detail: "Claude Code MCP configured (" + binary + ")"}, nil
				}
				data = after
				exists = true
			}
		}
	}
	next, err := writeClaudeEntry(data, url)
	if err != nil {
		return Result{}, fmt.Errorf("%s: %w", path, err)
	}
	if err := validateJSONBytes(path, next); err != nil {
		return Result{}, err
	}
	if err := writeFile(path, next, exists); err != nil {
		return Result{}, fmt.Errorf("write %s: %w", path, err)
	}
	return Result{Target: ClientClaude, Status: StatusConfigured, Path: path, Detail: "Claude Code MCP configured (" + binary + ")"}, nil
}

func claudeConfigPath() (string, error) {
	home, err := os.UserHomeDir()
	if err != nil {
		return "", fmt.Errorf("home dir: %w", err)
	}
	return filepath.Join(home, ".claude.json"), nil
}

func validateJSON(path string, data []byte) error {
	return validateJSONBytes(path, data)
}

func validateJSONBytes(path string, data []byte) error {
	var out any
	if len(strings.TrimSpace(string(data))) == 0 {
		return nil
	}
	if err := json.Unmarshal(data, &out); err != nil {
		return fmt.Errorf("%s: invalid JSON: %w", path, err)
	}
	return nil
}

type claudeServerEntry struct {
	Type string `json:"type,omitempty"`
	URL  string `json:"url,omitempty"`
}

func claudeEntry(data []byte) (claudeServerEntry, bool, error) {
	cfg, err := decodeClaude(data)
	if err != nil {
		return claudeServerEntry{}, false, err
	}
	servers, err := claudeServers(cfg)
	if err != nil {
		return claudeServerEntry{}, false, err
	}
	raw, ok := servers[mcp.ServerName]
	if !ok {
		return claudeServerEntry{}, false, nil
	}
	encoded, err := json.Marshal(raw)
	if err != nil {
		return claudeServerEntry{}, false, err
	}
	var entry claudeServerEntry
	if err := json.Unmarshal(encoded, &entry); err != nil {
		return claudeServerEntry{}, false, err
	}
	return entry, true, nil
}

func writeClaudeEntry(data []byte, url string) ([]byte, error) {
	cfg, err := decodeClaude(data)
	if err != nil {
		return nil, err
	}
	servers, err := claudeServers(cfg)
	if err != nil {
		return nil, err
	}
	if servers == nil {
		servers = map[string]any{}
		cfg["mcpServers"] = servers
	}
	servers[mcp.ServerName] = map[string]any{
		"type": "http",
		"url":  url,
	}
	next, err := json.MarshalIndent(cfg, "", "  ")
	if err != nil {
		return nil, err
	}
	return append(next, '\n'), nil
}

func decodeClaude(data []byte) (map[string]any, error) {
	if strings.TrimSpace(string(data)) == "" {
		return map[string]any{}, nil
	}
	var cfg map[string]any
	if err := json.Unmarshal(data, &cfg); err != nil {
		return nil, err
	}
	if cfg == nil {
		cfg = map[string]any{}
	}
	return cfg, nil
}

func claudeServers(cfg map[string]any) (map[string]any, error) {
	raw, ok := cfg["mcpServers"]
	if !ok || raw == nil {
		return nil, nil
	}
	servers, ok := raw.(map[string]any)
	if !ok {
		return nil, errors.New("mcpServers must be an object")
	}
	return servers, nil
}
