package mcpsetup

import (
	"fmt"
	"os"
	"path/filepath"

	"kbrd/config"
)

func configureKBRD(addr string, dryRun bool) (Result, error) {
	path, err := kbrdConfigPath()
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
	changed, next, err := ensureTOMLTableKeys(data, "mcp", map[string]string{
		"enabled": "true",
		"addr":    quoteTOMLString(addr),
	})
	if err != nil {
		return Result{}, fmt.Errorf("%s: %w", path, err)
	}
	if !changed {
		return Result{Target: "kbrd", Status: StatusEnabled, Path: path, Detail: "global MCP startup already enabled"}, nil
	}
	if err := validateTOMLBytes(path, next); err != nil {
		return Result{}, err
	}
	if dryRun {
		return Result{Target: "kbrd", Status: StatusFound, Path: path, Detail: "would enable global MCP startup"}, nil
	}
	if err := writeFile(path, next, exists); err != nil {
		return Result{}, fmt.Errorf("write %s: %w", path, err)
	}
	return Result{Target: "kbrd", Status: StatusEnabled, Path: path, Detail: "global MCP startup enabled"}, nil
}

func kbrdConfigPath() (string, error) {
	dir, err := os.UserConfigDir()
	if err != nil {
		return "", fmt.Errorf("user config dir: %w", err)
	}
	return filepath.Join(dir, config.AppDirName, config.GlobalConfigFile), nil
}
