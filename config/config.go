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
	// GitManualSyncMode controls the TUI's manual sync (the "s" key). "attended"
	// (default) keeps the loud pull --ff-only that stops on divergence; "auto"
	// runs the same self-healing merge-with-sidecar reconciliation the automatic
	// sync flows use. Automatic flows always self-heal regardless of this value.
	GitManualSyncMode string
	// GitSyncOnStartup reconciles with the remote once when the board opens, so a
	// stale checkout catches up without the user remembering to pull. Default
	// true; a no-op when the repo has no remote.
	GitSyncOnStartup bool
	// GitAutoCommit makes the TUI's auto-sync commit pending edits before it
	// reconciles, so it keeps itself synced while you work instead of waiting for
	// a clean tree. Default false (manual commits stay user-curated). TUI-only;
	// the web daemon always commits per mutation regardless.
	GitAutoCommit bool

	Scripting ScriptingConfig
	Hooks     HooksConfig
	MCP       MCPConfig
	Template  TemplateConfig
	Serve     ServeConfig
	Journal   JournalConfig

	// InstanceName is this process's machine-local name, used to route
	// instance-scoped Lua timers (and exposed as kbrd.instance.name). It is set
	// by the command layer from --name / KBRD_INSTANCE / the hostname and is
	// deliberately never read from a TOML file: the board's kbrd.toml is carried
	// by git, so a name in it would be identical on every clone and routing
	// would collapse. See ResolveInstanceName.
	InstanceName string
}

// ServeConfig holds the [serve] table consumed by `kbrd serve`. Values are
// one layer of the flag > env > toml > default chain, so unset keys stay ""
// for the resolver to fall through. Addr and Domain are read at startup only:
// kbrd.toml can arrive via git pull from a remote, and hot-applying listener
// or ACME settings would let anyone with push access rebind the server.
// PullInterval stays a raw duration string because the serve command
// re-resolves it against env/flag on every hot reload.
type ServeConfig struct {
	Addr         string // serve.addr — startup only
	Domain       string // serve.domain — startup only
	PullInterval string // serve.pull_interval — hot-reloadable
	TokenInTOML  bool   // serve.token was present: warned about and never read
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

// JournalConfig controls journal entries. When DetectDate is true (the default),
// a leading natural-language date in an entry ("yesterday fixed the bug",
// "next monday call client") is parsed and used as the entry's timestamp, and the
// date phrase is dropped from the recorded text. When false, journal entries always
// use the current time and the text is kept verbatim.
type JournalConfig struct {
	DetectDate bool
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
	// RemoteRequire enables require() of scripts from remote URLs
	// (https:// or the github: shorthand). Off by default: a remote module
	// runs with the same trust level as the user's own init file, so it must
	// be opted into explicitly. See SCRIPTING.md "Remote scripts".
	RemoteRequire bool
}

// ResolveInstanceName picks this process's machine-local instance name from
// the precedence flag > KBRD_INSTANCE env > hostname. It never consults TOML:
// the name must differ per machine, but kbrd.toml travels with the board over
// git, so a name there would be the same on every clone. An empty flagVal means
// "not set"; the hostname keeps zero-config setups working (each box differs).
func ResolveInstanceName(flagVal string) string {
	if flagVal != "" {
		return flagVal
	}
	if e := os.Getenv("KBRD_INSTANCE"); e != "" {
		return e
	}
	if host, err := os.Hostname(); err == nil {
		return host
	}
	return ""
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
	v.SetDefault("git.manual_sync_mode", "attended")
	v.SetDefault("git.sync_on_startup", true)
	v.SetDefault("git.auto_commit", false)
	v.SetDefault("scripting.enabled", true)
	v.SetDefault("scripting.command_timeout_ms", 2000)
	v.SetDefault("scripting.hook_timeout_ms", 500)
	v.SetDefault("scripting.instruction_limit", 10000000)
	v.SetDefault("scripting.error_threshold", 3)
	v.SetDefault("scripting.remote_require", false)
	v.SetDefault("hooks.enabled", true)
	v.SetDefault("hooks.timeout_ms", 2000)
	v.SetDefault("mcp.enabled", false)
	v.SetDefault("mcp.addr", "127.0.0.1:7777")
	v.SetDefault("journal.detect_date", true)
	v.SetDefault("template.exec", false)
	v.SetDefault("template.command_timeout_ms", 20000)
	v.SetDefault("serve.addr", "")
	v.SetDefault("serve.domain", "")
	v.SetDefault("serve.pull_interval", "")

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

	// Anything but the explicit opt-in falls back to the safe attended policy.
	manualSync := v.GetString("git.manual_sync_mode")
	if manualSync != "auto" {
		manualSync = "attended"
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
		GitManualSyncMode:   manualSync,
		GitSyncOnStartup:    v.GetBool("git.sync_on_startup"),
		GitAutoCommit:       v.GetBool("git.auto_commit"),
		Scripting: ScriptingConfig{
			Enabled:          v.GetBool("scripting.enabled"),
			CommandTimeoutMs: v.GetInt("scripting.command_timeout_ms"),
			HookTimeoutMs:    v.GetInt("scripting.hook_timeout_ms"),
			InstructionLimit: v.GetInt("scripting.instruction_limit"),
			ErrorThreshold:   v.GetInt("scripting.error_threshold"),
			RemoteRequire:    v.GetBool("scripting.remote_require"),
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
		Journal: JournalConfig{
			DetectDate: v.GetBool("journal.detect_date"),
		},
		Serve: ServeConfig{
			Addr:         v.GetString("serve.addr"),
			Domain:       v.GetString("serve.domain"),
			PullInterval: v.GetString("serve.pull_interval"),
			// Deliberately never read the value: a token in a file that gets
			// committed and pulled with the board repo is a leaked token.
			TokenInTOML: v.IsSet("serve.token"),
		},
	}, nil
}

// ValidateServe checks candidate kbrd.toml bytes before they are written —
// the web config editor calls this so a save cannot break the running server.
// It enforces TOML syntax plus the [serve] rules; other sections are
// syntax-checked only.
func ValidateServe(data []byte) error {
	v := viper.New()
	v.SetConfigType("toml")
	if err := v.ReadConfig(bytes.NewReader(data)); err != nil {
		return fmt.Errorf("invalid TOML: %w", err)
	}
	if v.IsSet("serve.token") {
		return errors.New("serve.token cannot be set in kbrd.toml — use --token or KBRD_TOKEN (this file may be committed with the board)")
	}
	if pi := v.GetString("serve.pull_interval"); pi != "" {
		d, err := time.ParseDuration(pi)
		if err != nil {
			return fmt.Errorf("serve.pull_interval %q is not a duration (e.g. \"60s\", \"5m\", \"0\")", pi)
		}
		if d < 0 {
			return fmt.Errorf("serve.pull_interval %q must not be negative", pi)
		}
	}
	return nil
}
