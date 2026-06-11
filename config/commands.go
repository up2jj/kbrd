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

// CommandSource identifies how a command's body is executed.
// "sh" means a shell template rendered through text/template and run via `sh -c`.
// "lua" means a Lua callback registered at runtime via the script subsystem.
type CommandSource string

const (
	SourceShell CommandSource = "sh"
	SourceLua   CommandSource = "lua"
)

type Command struct {
	Name        string `yaml:"name"`
	ID          string `yaml:"id"`
	Description string `yaml:"description"`
	Template    string `yaml:"command"`
	// Scope controls which columns a command appears on: "files" (default —
	// filesystem columns only), "virtual" (virtual columns only), or "all".
	// The default keeps file-assuming commands off fileless virtual columns.
	Scope string `yaml:"scope"`
	// RequiresItem controls whether the command needs a selected item. nil or
	// true (the default) keeps item-assuming commands off empty columns; set
	// requiresItem: false to opt a command into running with no item in context
	// (e.g. on an empty column). A pointer so omitted != false.
	RequiresItem *bool `yaml:"requiresItem"`
	// Source is populated by the loader/registrar; never read from YAML.
	Source CommandSource `yaml:"-"`
	// LuaRef is opaque to non-script code; the script subsystem uses it to
	// dispatch back to the registered callback. Empty for shell commands.
	LuaRef string `yaml:"-"`
}

// EffectiveScope returns the command's scope with the empty/unknown value
// normalized to "files" (the backward-compatible default).
func (c Command) EffectiveScope() string {
	switch c.Scope {
	case "virtual", "all":
		return c.Scope
	default:
		return "files"
	}
}

// ShowsOnVirtual reports whether the command should appear on a virtual column.
func (c Command) ShowsOnVirtual() bool {
	s := c.EffectiveScope()
	return s == "virtual" || s == "all"
}

// ShowsOnFiles reports whether the command should appear on a filesystem column.
func (c Command) ShowsOnFiles() bool {
	s := c.EffectiveScope()
	return s == "files" || s == "all"
}

// NeedsItem reports whether the command requires a selected item. Default true
// (backward compatible); only requiresItem: false opts out so the command can
// run on an empty column.
func (c Command) NeedsItem() bool {
	return c.RequiresItem == nil || *c.RequiresItem
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
// Folder entries override global entries that share the same ID.
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
		if winner, ok := seen[c.ID]; ok {
			warnings = append(warnings, CommandLoadWarning{
				Source: FolderCommandsFile,
				Message: fmt.Sprintf("duplicate id %q: %q shadowed by %q",
					c.ID, c.Name, winner),
			})
			continue
		}
		seen[c.ID] = c.Name
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
		c.Source = SourceShell
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
	if c.ID == "" {
		return fmt.Errorf("id is required")
	}
	return nil
}

func mergeCommands(global, local []Command) []Command {
	merged := make([]Command, 0, len(global)+len(local))
	overridden := make(map[string]bool, len(local))
	for _, c := range local {
		overridden[c.ID] = true
	}
	for _, c := range global {
		if overridden[c.ID] {
			continue
		}
		merged = append(merged, c)
	}
	merged = append(merged, local...)
	return merged
}

// Render expands the command's template against the provided variables.
func (c Command) Render(vars map[string]string) (string, error) {
	return renderTemplate(c.Template, vars)
}

// renderTemplate is the shared text/template expansion used by both custom
// commands and declarative hooks: it exposes the {{env "VAR"}} func and treats
// a reference to a missing variable as an error (so a template that needs an
// item fails cleanly when none is in context).
func renderTemplate(tmplStr string, vars map[string]string) (string, error) {
	tmpl, err := template.New("tmpl").
		Funcs(template.FuncMap{"env": os.Getenv}).
		Option("missingkey=error").
		Parse(tmplStr)
	if err != nil {
		return "", err
	}
	var buf bytes.Buffer
	if err := tmpl.Execute(&buf, vars); err != nil {
		return "", err
	}
	return buf.String(), nil
}
