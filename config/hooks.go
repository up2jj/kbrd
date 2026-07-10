package config

import (
	_ "embed"
	"fmt"
	"os"
	"path/filepath"

	"gopkg.in/yaml.v3"

	"kbrd/events"
)

//go:embed hooks_template.yml
var HooksTemplate []byte

const (
	GlobalHooksFile = "hooks.yml"
	FolderHooksFile = ".kbrd_hooks.yml"
)

// Hook binds an event name to a shell-command template. Hooks run reactively
// (after the operation, fire-and-observe) and are declared in YAML, mirroring
// custom commands — see commands.go. Unlike commands, a hook has no menu entry;
// it is dispatched by the model's hook runner when the matching event fires.
type Hook struct {
	Name        string `yaml:"name"`
	ID          string `yaml:"id"`
	Description string `yaml:"description"`
	Event       string `yaml:"event"`
	Template    string `yaml:"command"`
}

type hooksFile struct {
	Hooks []Hook `yaml:"hooks"`
}

// LoadHooks reads global (~/.config/kbrd/hooks.yml) then folder-local
// (<folderPath>/.kbrd_hooks.yml) hook definitions and merges them. Folder
// entries override global entries that share the same ID. Missing files are not
// errors. Invalid entries are skipped and reported via the returned warnings
// slice (non-fatal). Warnings reuse CommandLoadWarning so the model can funnel
// hook and command problems through one channel.
func LoadHooks(folderPath string) ([]Hook, []CommandLoadWarning, error) {
	globalDir, err := os.UserConfigDir()
	if err != nil {
		globalDir = ""
	}
	return loadHooksFrom(filepath.Join(globalDir, AppDirName), folderPath)
}

func loadHooksFrom(globalDir, folderPath string) ([]Hook, []CommandLoadWarning, error) {
	return loadScopedEntries(globalDir, folderPath, GlobalHooksFile, FolderHooksFile,
		readHooksFile,
		func(h Hook) string { return h.ID },
		func(h Hook) string { return h.Name },
	)
}

func readHooksFile(path string) ([]Hook, []CommandLoadWarning, error) {
	data, err := os.ReadFile(path)
	if err != nil {
		if os.IsNotExist(err) {
			return nil, nil, nil
		}
		return nil, nil, fmt.Errorf("read %s: %w", path, err)
	}
	var f hooksFile
	if err := yaml.Unmarshal(data, &f); err != nil {
		return nil, []CommandLoadWarning{{Source: path, Message: "parse error: " + err.Error()}}, nil
	}

	var out []Hook
	var warnings []CommandLoadWarning
	for i, h := range f.Hooks {
		if err := validateHook(h); err != nil {
			warnings = append(warnings, CommandLoadWarning{
				Source:  path,
				Message: fmt.Sprintf("entry %d (%q): %s", i, h.Name, err.Error()),
			})
			continue
		}
		out = append(out, h)
	}
	return out, warnings, nil
}

func validateHook(h Hook) error {
	if h.Name == "" {
		return fmt.Errorf("name is required")
	}
	if h.ID == "" {
		return fmt.Errorf("id is required")
	}
	if h.Template == "" {
		return fmt.Errorf("command is required")
	}
	if h.Event == "" {
		return fmt.Errorf("event is required")
	}
	// Semantic check against the canonical hookable set owned by events/.
	// High-frequency events are Lua-only to keep the serial queue backlog-free.
	if !events.IsHookable(h.Event) {
		return fmt.Errorf("event %q is not hookable from YAML; use a Lua kbrd.on(%q, ...) hook instead", h.Event, h.Event)
	}
	return nil
}

// Render expands the hook's command template against the provided variables,
// using the same engine (and {{env}} func / missingkey=error semantics) as
// custom commands.
func (h Hook) Render(vars map[string]string) (string, error) {
	return renderTemplate(h.Template, vars)
}
