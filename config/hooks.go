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
	var warnings []CommandLoadWarning

	var global []Hook
	if globalDir != "" {
		hooks, ws, err := readHooksFile(filepath.Join(globalDir, GlobalHooksFile))
		if err != nil {
			return nil, warnings, err
		}
		warnings = append(warnings, ws...)
		global = hooks
	}

	var local []Hook
	if folderPath != "" {
		hooks, ws, err := readHooksFile(filepath.Join(folderPath, FolderHooksFile))
		if err != nil {
			return nil, warnings, err
		}
		warnings = append(warnings, ws...)
		local = hooks
	}

	merged := mergeHooks(global, local)
	warnings = append(warnings, detectHookDuplicates(merged)...)
	return merged, warnings, nil
}

func detectHookDuplicates(hooks []Hook) []CommandLoadWarning {
	var warnings []CommandLoadWarning
	seen := make(map[string]string, len(hooks))
	for _, h := range hooks {
		if winner, ok := seen[h.ID]; ok {
			warnings = append(warnings, CommandLoadWarning{
				Source: FolderHooksFile,
				Message: fmt.Sprintf("duplicate id %q: %q shadowed by %q",
					h.ID, h.Name, winner),
			})
			continue
		}
		seen[h.ID] = h.Name
	}
	return warnings
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

func mergeHooks(global, local []Hook) []Hook {
	merged := make([]Hook, 0, len(global)+len(local))
	overridden := make(map[string]bool, len(local))
	for _, h := range local {
		overridden[h.ID] = true
	}
	for _, h := range global {
		if overridden[h.ID] {
			continue
		}
		merged = append(merged, h)
	}
	merged = append(merged, local...)
	return merged
}

// Render expands the hook's command template against the provided variables,
// using the same engine (and {{env}} func / missingkey=error semantics) as
// custom commands.
func (h Hook) Render(vars map[string]string) (string, error) {
	return renderTemplate(h.Template, vars)
}
