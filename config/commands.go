package config

import (
	"bytes"
	_ "embed"
	"fmt"
	"os"
	"path/filepath"
	"text/template"

	"gopkg.in/yaml.v3"
)

//go:embed commands_template.yml
var CommandsTemplate []byte

const (
	GlobalCommandsFile = "commands.yml"
	FolderCommandsFile = ".kbrd_commands.yml"
)

type Command struct {
	Name        string `yaml:"name"`
	Shortcut    string `yaml:"shortcut"`
	Description string `yaml:"description"`
	Template    string `yaml:"command"`
}

type commandsFile struct {
	Commands []Command `yaml:"commands"`
}

type CommandLoadWarning struct {
	Source  string
	Message string
}

// LoadCommands reads global (~/.config/kbrd/commands.yml) then folder-local
// (<folderPath>/.kbrd_commands.yml) command definitions and merges them.
// Folder entries override global entries that share the same Shortcut.
// Missing files are not errors. Invalid entries are skipped and reported via
// the returned warnings slice (non-fatal).
func LoadCommands(folderPath string) ([]Command, []CommandLoadWarning, error) {
	globalDir, err := os.UserConfigDir()
	if err != nil {
		globalDir = ""
	}
	return loadCommandsFrom(filepath.Join(globalDir, AppDirName), folderPath)
}

func loadCommandsFrom(globalDir, folderPath string) ([]Command, []CommandLoadWarning, error) {
	var warnings []CommandLoadWarning

	var global []Command
	if globalDir != "" {
		path := filepath.Join(globalDir, GlobalCommandsFile)
		cmds, ws, err := readCommandsFile(path)
		if err != nil {
			return nil, warnings, err
		}
		warnings = append(warnings, ws...)
		global = cmds
	}

	var local []Command
	if folderPath != "" {
		path := filepath.Join(folderPath, FolderCommandsFile)
		cmds, ws, err := readCommandsFile(path)
		if err != nil {
			return nil, warnings, err
		}
		warnings = append(warnings, ws...)
		local = cmds
	}

	merged := mergeCommands(global, local)
	warnings = append(warnings, detectDuplicates(merged)...)
	return merged, warnings, nil
}

func detectDuplicates(cmds []Command) []CommandLoadWarning {
	var warnings []CommandLoadWarning
	seen := make(map[string]string, len(cmds))
	for _, c := range cmds {
		if winner, ok := seen[c.Shortcut]; ok {
			warnings = append(warnings, CommandLoadWarning{
				Source: FolderCommandsFile,
				Message: fmt.Sprintf("duplicate shortcut %q: %q shadowed by %q",
					c.Shortcut, c.Name, winner),
			})
			continue
		}
		seen[c.Shortcut] = c.Name
	}
	return warnings
}

func readCommandsFile(path string) ([]Command, []CommandLoadWarning, error) {
	data, err := os.ReadFile(path)
	if err != nil {
		if os.IsNotExist(err) {
			return nil, nil, nil
		}
		return nil, nil, fmt.Errorf("read %s: %w", path, err)
	}
	var f commandsFile
	if err := yaml.Unmarshal(data, &f); err != nil {
		return nil, []CommandLoadWarning{{Source: path, Message: "parse error: " + err.Error()}}, nil
	}

	var out []Command
	var warnings []CommandLoadWarning
	for i, c := range f.Commands {
		if err := validateCommand(c); err != nil {
			warnings = append(warnings, CommandLoadWarning{
				Source:  path,
				Message: fmt.Sprintf("entry %d (%q): %s", i, c.Name, err.Error()),
			})
			continue
		}
		out = append(out, c)
	}
	return out, warnings, nil
}

func validateCommand(c Command) error {
	if c.Name == "" {
		return fmt.Errorf("name is required")
	}
	if c.Template == "" {
		return fmt.Errorf("command is required")
	}
	if c.Shortcut == "" {
		return fmt.Errorf("shortcut is required")
	}
	r := []rune(c.Shortcut)
	if len(r) != 1 {
		return fmt.Errorf("shortcut must be a single character")
	}
	return nil
}

func mergeCommands(global, local []Command) []Command {
	merged := make([]Command, 0, len(global)+len(local))
	overridden := make(map[string]bool, len(local))
	for _, c := range local {
		overridden[c.Shortcut] = true
	}
	for _, c := range global {
		if overridden[c.Shortcut] {
			continue
		}
		merged = append(merged, c)
	}
	merged = append(merged, local...)
	return merged
}

// Render expands the command's template against the provided variables.
func (c Command) Render(vars map[string]string) (string, error) {
	tmpl, err := template.New("cmd").Option("missingkey=error").Parse(c.Template)
	if err != nil {
		return "", err
	}
	var buf bytes.Buffer
	if err := tmpl.Execute(&buf, vars); err != nil {
		return "", err
	}
	return buf.String(), nil
}
