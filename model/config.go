package model

import (
	"errors"
	"fmt"
	"io/fs"
	"os"
	"path/filepath"

	"kbrd/config"
)

func localConfigPath() (string, error) {
	cwd, err := os.Getwd()
	if err != nil {
		return "", fmt.Errorf("cwd: %w", err)
	}
	return filepath.Join(cwd, config.FolderConfigFile), nil
}

func globalConfigPath() (string, error) {
	dir, err := os.UserConfigDir()
	if err != nil {
		return "", fmt.Errorf("user config dir: %w", err)
	}
	return filepath.Join(dir, config.AppDirName, config.GlobalConfigFile), nil
}

func ensureConfigFile(path string) error {
	return ensureFileFromTemplate(path, config.Template)
}

func localCommandsPath() (string, error) {
	cwd, err := os.Getwd()
	if err != nil {
		return "", fmt.Errorf("cwd: %w", err)
	}
	return filepath.Join(cwd, config.FolderCommandsFile), nil
}

func ensureCommandsFile(path string) error {
	return ensureFileFromTemplate(path, config.CommandsTemplate)
}

func ensureFileFromTemplate(path string, content []byte) error {
	if _, err := os.Stat(path); errors.Is(err, fs.ErrNotExist) {
		if err := os.MkdirAll(filepath.Dir(path), 0o755); err != nil {
			return err
		}
		return os.WriteFile(path, content, 0o644)
	} else if err != nil {
		return err
	}
	return nil
}

func configFileExists(path string) bool {
	_, err := os.Stat(path)
	return err == nil
}

type ConfigCommandEntry struct {
	Key    string
	Label  string
	Path   string
	Exists bool
	Err    error
}

func configCommandEntries() []ConfigCommandEntry {
	entries := []ConfigCommandEntry{
		{Key: "c", Label: "open or create local config"},
		{Key: "C", Label: "open or create global config"},
		{Key: "x", Label: "open or create local commands"},
	}
	resolvers := []func() (string, error){localConfigPath, globalConfigPath, localCommandsPath}
	for i, resolve := range resolvers {
		path, err := resolve()
		if err != nil {
			entries[i].Err = err
			continue
		}
		entries[i].Path = path
		entries[i].Exists = configFileExists(path)
	}
	return entries
}
