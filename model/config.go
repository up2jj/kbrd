package model

import (
	"errors"
	"fmt"
	stdfs "io/fs"
	"os"
	"path/filepath"
	"regexp"
	"strings"

	"kbrd/config"
	kbrdfs "kbrd/fs"
	"kbrd/mcp"

	"github.com/pelletier/go-toml/v2"
)

var (
	configSectionPattern        = regexp.MustCompile(`^\s*\[([A-Za-z0-9_.-]+)\]\s*(?:#.*)?$`)
	configArrayPattern          = regexp.MustCompile(`^\s*\[\[([A-Za-z0-9_.-]+)\]\]\s*(?:#.*)?$`)
	configCommentedArrayPattern = regexp.MustCompile(`^\s*#\s*\[\[([A-Za-z0-9_.-]+)\]\]`)
	configOptionPattern         = regexp.MustCompile(`^\s*#\s*([A-Za-z0-9_.-]+)\s*=`)
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
	if err := ensureFileFromTemplate(path, config.Template); err != nil {
		return err
	}
	return refreshConfigExamples(path, config.Template)
}

// refreshConfigExamples adds newly introduced, commented options from the
// embedded template to an existing config. User values, comments, and ordering
// are otherwise left alone. This keeps the config menu useful after upgrades,
// not only when kbrd.toml is first scaffolded.
func refreshConfigExamples(path string, template []byte) error {
	content, err := os.ReadFile(path)
	if err != nil {
		return err
	}
	updated, changed := mergeConfigExamples(content, template)
	if !changed {
		return nil
	}
	return kbrdfs.WriteExistingFileAtomicDurable(path, updated)
}

type configTemplateSection struct {
	name    string
	options []configTemplateOption
}

type configTemplateOption struct {
	name  string
	lines []string
}

func mergeConfigExamples(content, template []byte) ([]byte, bool) {
	// Do not rewrite a malformed file. Opening it is how the user gets a chance
	// to repair it, and adding examples would only obscure the original error.
	var settings map[string]any
	if err := toml.Unmarshal(content, &settings); err != nil {
		return content, false
	}

	sections := parseConfigTemplate(template)
	lines := strings.Split(strings.TrimSuffix(string(content), "\n"), "\n")
	present, headers := configEntries(lines, settings)
	changed := false

	for _, section := range sections {
		missing := make([]configTemplateOption, 0, len(section.options))
		for _, option := range section.options {
			if !present[section.name+"."+option.name] {
				missing = append(missing, option)
			}
		}
		if len(missing) == 0 {
			continue
		}

		addition := configOptionLines(missing, "")
		if header, ok := headers[section.name]; ok {
			insertAt := nextConfigSection(lines, header+1)
			for insertAt > header+1 && strings.TrimSpace(lines[insertAt-1]) == "" {
				insertAt--
			}
			addition = append([]string{""}, addition...)
			lines = insertLines(lines, insertAt, addition)
		} else if tableExists(settings, section.name) {
			// A dotted key such as git.diff_tool defines the table without a
			// [git] header. Keep the suggestions valid wherever they are placed.
			addition = configOptionLines(missing, section.name+".")
			lines = appendWithBlankLine(lines, addition)
		} else {
			addition = append([]string{"[" + section.name + "]"}, addition...)
			lines = appendWithBlankLine(lines, addition)
		}
		changed = true
		present, headers = configEntries(lines, settings)
	}

	if !changed {
		return content, false
	}
	return []byte(strings.Join(lines, "\n") + "\n"), true
}

func parseConfigTemplate(template []byte) []configTemplateSection {
	lines := strings.Split(string(template), "\n")
	sections := make([]configTemplateSection, 0)
	section := -1
	for i := 0; i < len(lines); i++ {
		match := configSectionPattern.FindStringSubmatch(lines[i])
		if match != nil {
			sections = append(sections, configTemplateSection{name: match[1]})
			section = len(sections) - 1
			continue
		}
		if section < 0 {
			continue
		}
		if configCommentedArrayPattern.MatchString(lines[i]) {
			section = -1
			continue
		}
		match = configOptionPattern.FindStringSubmatch(lines[i])
		if match == nil {
			continue
		}
		option := configTemplateOption{name: match[1], lines: []string{lines[i]}}
		for i+1 < len(lines) {
			next := lines[i+1]
			if configSectionPattern.MatchString(next) || configArrayPattern.MatchString(next) || configCommentedArrayPattern.MatchString(next) || configOptionPattern.MatchString(next) || strings.TrimSpace(next) == "" || !strings.HasPrefix(strings.TrimSpace(next), "#") {
				break
			}
			option.lines = append(option.lines, next)
			i++
		}
		sections[section].options = append(sections[section].options, option)
	}
	return sections
}

func configEntries(lines []string, settings map[string]any) (map[string]bool, map[string]int) {
	present := make(map[string]bool)
	collectConfigKeys(settings, "", present)
	headers := make(map[string]int)
	section := ""
	for i, line := range lines {
		if match := configSectionPattern.FindStringSubmatch(line); match != nil {
			section = match[1]
			headers[section] = i
			continue
		}
		if configArrayPattern.MatchString(line) {
			section = ""
			continue
		}
		if match := configOptionPattern.FindStringSubmatch(line); match != nil {
			name := match[1]
			if strings.Contains(name, ".") {
				present[name] = true
			}
			if section != "" {
				name = section + "." + name
			}
			present[name] = true
		}
	}
	return present, headers
}

func collectConfigKeys(values map[string]any, prefix string, present map[string]bool) {
	for key, value := range values {
		name := key
		if prefix != "" {
			name = prefix + "." + key
		}
		present[name] = true
		if nested, ok := value.(map[string]any); ok {
			collectConfigKeys(nested, name, present)
		}
	}
}

func tableExists(settings map[string]any, name string) bool {
	current := any(settings)
	for _, part := range strings.Split(name, ".") {
		table, ok := current.(map[string]any)
		if !ok {
			return false
		}
		current, ok = table[part]
		if !ok {
			return false
		}
	}
	_, ok := current.(map[string]any)
	return ok
}

func nextConfigSection(lines []string, from int) int {
	for i := from; i < len(lines); i++ {
		if configSectionPattern.MatchString(lines[i]) || configArrayPattern.MatchString(lines[i]) {
			return i
		}
	}
	return len(lines)
}

func configOptionLines(options []configTemplateOption, prefix string) []string {
	var lines []string
	for _, option := range options {
		optionLines := append([]string(nil), option.lines...)
		if prefix != "" {
			optionLines[0] = strings.Replace(optionLines[0], option.name, prefix+option.name, 1)
		}
		lines = append(lines, optionLines...)
	}
	return lines
}

func insertLines(lines []string, at int, addition []string) []string {
	lines = append(lines, make([]string, len(addition))...)
	copy(lines[at+len(addition):], lines[at:len(lines)-len(addition)])
	copy(lines[at:], addition)
	return lines
}

func appendWithBlankLine(lines, addition []string) []string {
	if len(lines) > 0 && strings.TrimSpace(lines[len(lines)-1]) != "" {
		lines = append(lines, "")
	}
	return append(lines, addition...)
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

func localMCPPath() (string, error) {
	cwd, err := os.Getwd()
	if err != nil {
		return "", fmt.Errorf("cwd: %w", err)
	}
	return filepath.Join(cwd, config.FolderMCPFile), nil
}

func localAgentsPath() (string, error) {
	cwd, err := os.Getwd()
	if err != nil {
		return "", fmt.Errorf("cwd: %w", err)
	}
	return filepath.Join(cwd, config.FolderAgentsFile), nil
}

// ensureAgentsFile writes an AGENTS.md at path if it does not already exist.
// Content comes from the mcp package, which owns the documented tool surface.
func ensureAgentsFile(path string) error {
	return ensureFileFromTemplate(path, mcp.AgentsMarkdown())
}

// ensureMCPFile writes a .mcp.json for addr at path if it does not already
// exist, mirroring ensureFileFromTemplate but with content generated by the
// mcp package (the owner of kbrd's client-connection details).
func ensureMCPFile(path, addr string) error {
	if _, err := os.Stat(path); errors.Is(err, stdfs.ErrNotExist) {
		content, err := mcp.ClientConfigJSON(addr)
		if err != nil {
			return err
		}
		if err := os.MkdirAll(filepath.Dir(path), 0o755); err != nil {
			return err
		}
		return kbrdfs.WriteNewFileNoClobberDurable(path, content, 0o644)
	} else if err != nil {
		return err
	}
	return nil
}

func ensureFileFromTemplate(path string, content []byte) error {
	if _, err := os.Stat(path); errors.Is(err, stdfs.ErrNotExist) {
		if err := os.MkdirAll(filepath.Dir(path), 0o755); err != nil {
			return err
		}
		return kbrdfs.WriteNewFileNoClobberDurable(path, content, 0o644)
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
		{Key: "m", Label: "create local .mcp.json"},
		{Key: "a", Label: "create local AGENTS.md"},
	}
	resolvers := []func() (string, error){localConfigPath, globalConfigPath, localCommandsPath, localMCPPath, localAgentsPath}
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
