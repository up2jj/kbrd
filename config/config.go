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

	ColumnWidth   int
	PreviewLines  int
	Theme         string
	NotifyBackend string
	BoardName     string
	GitDiffTool         string
	GitAutoSyncInterval time.Duration

	Scripting ScriptingConfig
	MCP       MCPConfig
}

// MCPConfig controls the built-in MCP server, which runs alongside the TUI and
// exposes board operations to MCP clients over Streamable HTTP. When Enabled is
// false (or the --no-mcp flag is set) no listener is started.
type MCPConfig struct {
	Enabled bool
	Addr    string
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
	v.SetDefault("display.theme", "dark")
	v.SetDefault("notify.backend", "auto")
	v.SetDefault("git.diff_tool", "auto")
	v.SetDefault("git.auto_sync_interval", "")
	v.SetDefault("scripting.enabled", true)
	v.SetDefault("scripting.command_timeout_ms", 2000)
	v.SetDefault("scripting.hook_timeout_ms", 500)
	v.SetDefault("scripting.instruction_limit", 10000000)
	v.SetDefault("scripting.error_threshold", 3)
	v.SetDefault("mcp.enabled", true)
	v.SetDefault("mcp.addr", "127.0.0.1:7777")

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
		Theme:               v.GetString("display.theme"),
		NotifyBackend:       v.GetString("notify.backend"),
		BoardName:           v.GetString("board.name"),
		GitDiffTool:         v.GetString("git.diff_tool"),
		GitAutoSyncInterval: autoSync,
		Scripting: ScriptingConfig{
			Enabled:          v.GetBool("scripting.enabled"),
			CommandTimeoutMs: v.GetInt("scripting.command_timeout_ms"),
			HookTimeoutMs:    v.GetInt("scripting.hook_timeout_ms"),
			InstructionLimit: v.GetInt("scripting.instruction_limit"),
			ErrorThreshold:   v.GetInt("scripting.error_threshold"),
		},
		MCP: MCPConfig{
			Enabled: v.GetBool("mcp.enabled"),
			Addr:    v.GetString("mcp.addr"),
		},
	}, nil
}
