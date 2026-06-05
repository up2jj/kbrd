package config

import (
	"bytes"
	_ "embed"
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"time"

	"github.com/spf13/viper"
)

//go:embed template.toml
var Template []byte

const (
	GlobalConfigName = "config"
	GlobalConfigFile = "config.toml"
	FolderConfigFile = "kbrd.toml"
	FolderMCPFile    = ".mcp.json"
	FolderAgentsFile = "AGENTS.md"
	AppDirName       = "kbrd"
)

type Config struct {
	Path string

	ColumnWidth         int
	PreviewLines        int
	TitleFromHeading    bool
	Theme               string
	NotifyBackend       string
	BoardName           string
	GitDiffTool         string
	GitAutoSyncInterval time.Duration
	GitGenerateReadme   bool

	Scripting ScriptingConfig
	Hooks     HooksConfig
	MCP       MCPConfig
	Template  TemplateConfig
}

// HooksConfig controls declarative YAML event hooks (hooks.yml /
// .kbrd_hooks.yml). These run independently of the Lua scripting subsystem, so
// they work even when scripting is disabled. TimeoutMs bounds each individual
// hook command; the runner executes hooks one at a time, in order.
type HooksConfig struct {
	Enabled   bool
	TimeoutMs int
}

// MCPConfig controls the built-in MCP server, which runs alongside the TUI and
// exposes board operations to MCP clients over Streamable HTTP. It is opt-in: a
// listener is started only when the --mcp flag is passed or Enabled is set true.
type MCPConfig struct {
	Enabled bool
	Addr    string
}

// TemplateConfig controls card templates. Exec gates the {{shell}} template
// function: when false (the default), a template's shell directives render as
// an inert "disabled" note instead of running. It is opt-in because templates
// are shared/pasted more casually than whole boards, and a {{shell}} command
// runs with kbrd's full environment. CommandTimeoutMs bounds each invocation.
type TemplateConfig struct {
	Exec             bool
	CommandTimeoutMs int
}

// ScriptingConfig controls the embedded Lua scripting subsystem.
// When Enabled is false, no Lua VM is created and no script files are read.
type ScriptingConfig struct {
	Enabled          bool
	CommandTimeoutMs int
	HookTimeoutMs    int
	InstructionLimit int
	// ErrorThreshold is the number of consecutive errors that disables a
	// failing timer or hook. 0 means "never auto-disable" — useful if you
	// want a periodically-flaky script to keep retrying forever. Default 3.
	ErrorThreshold int
}

func Load(path string) (Config, error) {
	globalDir, err := os.UserConfigDir()
	if err != nil {
		globalDir = ""
	}
	return loadFrom(filepath.Join(globalDir, AppDirName), path)
}

func loadFrom(globalDir, folderPath string) (Config, error) {
	v := viper.New()
	v.SetConfigType("toml")

	v.SetDefault("display.column_width", 32)
	v.SetDefault("display.preview_lines", 3)
	v.SetDefault("display.title_from_heading", false)
	v.SetDefault("display.theme", "dark")
	v.SetDefault("notify.backend", "auto")
	v.SetDefault("git.diff_tool", "auto")
	v.SetDefault("git.auto_sync_interval", "")
	v.SetDefault("git.generate_readme", false)
	v.SetDefault("scripting.enabled", true)
	v.SetDefault("scripting.command_timeout_ms", 2000)
	v.SetDefault("scripting.hook_timeout_ms", 500)
	v.SetDefault("scripting.instruction_limit", 10000000)
	v.SetDefault("scripting.error_threshold", 3)
	v.SetDefault("hooks.enabled", true)
	v.SetDefault("hooks.timeout_ms", 2000)
	v.SetDefault("mcp.enabled", false)
	v.SetDefault("mcp.addr", "127.0.0.1:7777")
	v.SetDefault("template.exec", false)
	v.SetDefault("template.command_timeout_ms", 20000)

	_ = v.BindEnv("notify.backend", "KBRD_NOTIFY")

	if globalDir != "" {
		v.SetConfigName(GlobalConfigName)
		v.AddConfigPath(globalDir)
		if err := v.ReadInConfig(); err != nil {
			var nf viper.ConfigFileNotFoundError
			if !errors.As(err, &nf) {
				return Config{}, fmt.Errorf("read global config: %w", err)
			}
		}
	}

	if folderPath != "" {
		folderFile := filepath.Join(folderPath, FolderConfigFile)
		if data, err := os.ReadFile(folderFile); err == nil {
			v2 := viper.New()
			v2.SetConfigType("toml")
			if err := v2.ReadConfig(bytes.NewReader(data)); err != nil {
				return Config{}, fmt.Errorf("read folder config %s: %w", folderFile, err)
			}
			if err := v.MergeConfigMap(v2.AllSettings()); err != nil {
				return Config{}, fmt.Errorf("merge folder config: %w", err)
			}
		} else if !os.IsNotExist(err) {
			return Config{}, fmt.Errorf("open folder config %s: %w", folderFile, err)
		}
	}

	autoSync, _ := time.ParseDuration(v.GetString("git.auto_sync_interval"))
	if autoSync < 0 {
		autoSync = 0
	}

	return Config{
		Path:                folderPath,
		ColumnWidth:         v.GetInt("display.column_width"),
		PreviewLines:        v.GetInt("display.preview_lines"),
		TitleFromHeading:    v.GetBool("display.title_from_heading"),
		Theme:               v.GetString("display.theme"),
		NotifyBackend:       v.GetString("notify.backend"),
		BoardName:           v.GetString("board.name"),
		GitDiffTool:         v.GetString("git.diff_tool"),
		GitAutoSyncInterval: autoSync,
		GitGenerateReadme:   v.GetBool("git.generate_readme"),
		Scripting: ScriptingConfig{
			Enabled:          v.GetBool("scripting.enabled"),
			CommandTimeoutMs: v.GetInt("scripting.command_timeout_ms"),
			HookTimeoutMs:    v.GetInt("scripting.hook_timeout_ms"),
			InstructionLimit: v.GetInt("scripting.instruction_limit"),
			ErrorThreshold:   v.GetInt("scripting.error_threshold"),
		},
		Hooks: HooksConfig{
			Enabled:   v.GetBool("hooks.enabled"),
			TimeoutMs: v.GetInt("hooks.timeout_ms"),
		},
		MCP: MCPConfig{
			Enabled: v.GetBool("mcp.enabled"),
			Addr:    v.GetString("mcp.addr"),
		},
		Template: TemplateConfig{
			Exec:             v.GetBool("template.exec"),
			CommandTimeoutMs: v.GetInt("template.command_timeout_ms"),
		},
	}, nil
}
