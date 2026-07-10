package commands

import (
	"errors"
	"fmt"
	"io/fs"
	"os"
	"os/signal"
	"path/filepath"
	"syscall"
	"time"

	"github.com/spf13/cobra"

	"kbrd/config"
	fsutil "kbrd/fs"
	"kbrd/model"
	"kbrd/web"
)

// serveFlags holds the `kbrd serve` options. Every flag has an env fallback
// (flag > env > default) so the dockerized form needs no arguments.
type serveFlags struct {
	addr         string // KBRD_ADDR
	domain       string // KBRD_DOMAIN
	token        string // KBRD_TOKEN
	dir          string
	gitURL       string // GIT_URL
	pullInterval string // KBRD_PULL_INTERVAL
	instance     string // KBRD_INSTANCE — machine-local name for timer routing
	scripting    bool   // KBRD_SCRIPTING — run the board's Lua timers (off by default)
}

const minTokenLen = 12

// newServeCmd builds `kbrd serve`: the headless web frontend. The TUI never
// runs; board-supplied code is off by default (no hooks, no template exec) —
// serve is safe-by-default. The one opt-in is --scripting, which runs the
// board's Lua timers (init.lua/.kbrd.lua) for repeating tasks; it executes
// board-supplied code and is intended for single-user boards. The MCP server
// never runs: the inherited --mcp/--mcp-addr flags are rejected rather than
// silently ignored.
func newServeCmd() *cobra.Command {
	var f serveFlags
	cmd := &cobra.Command{
		Use:   "serve",
		Short: "Serve the board as a mobile-first web app (headless, git-backed)",
		Args:  cobra.NoArgs,
		PreRunE: func(cmd *cobra.Command, args []string) error {
			if cmd.Flags().Changed("mcp") || cmd.Flags().Changed("mcp-addr") {
				return errors.New("--mcp is not supported with serve: the web frontend runs no MCP server")
			}
			return nil
		},
		RunE: func(cmd *cobra.Command, args []string) error { return runServe(cmd, f) },
	}
	cmd.AddCommand(newServeEjectCmd())
	cmd.Flags().StringVar(&f.addr, "addr", "", "listen address for plain HTTP (env KBRD_ADDR, default :8080); ignored when --domain is set")
	cmd.Flags().StringVar(&f.domain, "domain", "", "public domain: enables Let's Encrypt TLS on :443 + :80 (env KBRD_DOMAIN)")
	cmd.Flags().StringVar(&f.token, "token", "", "shared access token, min 12 chars (env KBRD_TOKEN)")
	cmd.Flags().StringVar(&f.dir, "dir", "", "board directory (default current directory)")
	cmd.Flags().StringVar(&f.gitURL, "git-url", "", "clone this repo into --dir when it is missing or empty (env GIT_URL)")
	cmd.Flags().StringVar(&f.pullInterval, "pull-interval", "", "background git pull interval, 0 to disable (env KBRD_PULL_INTERVAL, default 60s)")
	cmd.Flags().StringVar(&f.instance, "name", "", "instance name for routing instance-scoped Lua timers (env KBRD_INSTANCE, default hostname)")
	cmd.Flags().BoolVar(&f.scripting, "scripting", false, "run the board's Lua timers (init.lua/.kbrd.lua) — executes board-supplied code, single-user boards only (env KBRD_SCRIPTING)")
	return cmd
}

// loadServeConfig loads the [serve] config for dir; before a clone the
// board's kbrd.toml does not exist yet, so only the global config applies.
func loadServeConfig(dir string, needsClone bool) (config.Config, error) {
	if needsClone {
		return config.Load("")
	}
	return config.Load(dir)
}

func runServe(cmd *cobra.Command, f serveFlags) error {
	fl := cmd.Flags()

	// Token never comes from TOML: kbrd.toml is committed and pulled with the
	// board repo, so a token in it is a leaked token.
	f.token = envDefault(f.token, "KBRD_TOKEN", "")
	f.gitURL = envDefault(f.gitURL, "GIT_URL", "")

	if len(f.token) < minTokenLen {
		return fmt.Errorf("an access token of at least %d characters is required (--token or KBRD_TOKEN); generate one with: openssl rand -base64 24", minTokenLen)
	}

	dir := f.dir
	var err error
	if dir == "" {
		if dir, err = os.Getwd(); err != nil {
			return fmt.Errorf("cannot determine working directory: %w", err)
		}
	}
	if dir, err = filepath.Abs(dir); err != nil {
		return err
	}

	needsClone, err := dirMissingOrEmpty(dir)
	if err != nil {
		return err
	}
	if needsClone && f.gitURL == "" {
		return fmt.Errorf("board directory %s is missing or empty and no --git-url/GIT_URL is set", dir)
	}

	cfg, err := loadServeConfig(dir, needsClone)
	if err != nil {
		return err
	}
	if cfg.Serve.TokenInTOML {
		fmt.Fprintln(os.Stderr, "warning: serve.token in config is ignored — use --token or KBRD_TOKEN (a token in kbrd.toml would be committed with the board)")
	}

	addr := resolveOpt(fl, "addr", f.addr, "KBRD_ADDR", cfg.Serve.Addr, ":8080")
	domain := resolveOpt(fl, "domain", f.domain, "KBRD_DOMAIN", cfg.Serve.Domain, "")
	pullInterval := resolveOpt(fl, "pull-interval", f.pullInterval, "KBRD_PULL_INTERVAL", cfg.Serve.PullInterval, "60s")
	pullEvery, err := time.ParseDuration(pullInterval)
	if err != nil || pullEvery < 0 {
		return fmt.Errorf("invalid pull interval %q", pullInterval)
	}

	// Scripting opt-in: flag, or KBRD_SCRIPTING truthy. Carries the board's
	// scripting timeouts/limits but with Enabled gated on this explicit opt-in
	// (serve stays safe-by-default). Instance name is machine-local — flag/env/
	// hostname, never the git-carried kbrd.toml.
	scripting := cfg.Scripting
	scripting.Enabled = f.scripting || isTruthyEnv("KBRD_SCRIPTING")
	instanceName := config.ResolveInstanceName(f.instance)

	ctx, stop := signal.NotifyContext(cmd.Context(), os.Interrupt, syscall.SIGTERM)
	defer stop()

	fmt.Printf("kbrd %s serve: board %s\n", model.Version, dir)

	globalDir := ""
	if d, err := os.UserConfigDir(); err == nil {
		globalDir = filepath.Join(d, config.AppDirName)
	}
	configWatch := []string{filepath.Join(dir, config.FolderConfigFile)}
	if globalDir != "" {
		configWatch = append(configWatch, filepath.Join(globalDir, config.GlobalConfigFile))
	}

	opts := web.Options{
		Addr:         addr,
		Domain:       domain,
		CertCacheDir: filepath.Join(filepath.Dir(dir), "certs"),
		BoardPath:    dir,
		BoardName:    filepath.Base(dir),
		Token:        f.token,
		InstanceName: instanceName,
		Scripting:    scripting,
		AuthorName:   envDefault("", "GIT_AUTHOR_NAME", "kbrd-web"),
		AuthorEmail:  envDefault("", "GIT_AUTHOR_EMAIL", "kbrd@localhost"),
		PullEvery:    pullEvery,
		Init: func(setStatus func(string)) (string, error) {
			return initBoard(dir, f.gitURL, instanceName, needsClone, setStatus)
		},
		// LoadConfig re-runs the full precedence chain against the file on
		// disk, so env/flag still beat a freshly saved (or pulled) TOML.
		LoadConfig: func() (web.ReloadableConfig, error) {
			cfg, err := config.Load(dir)
			if err != nil {
				return web.ReloadableConfig{}, err
			}
			pi := resolveOpt(fl, "pull-interval", f.pullInterval, "KBRD_PULL_INTERVAL", cfg.Serve.PullInterval, "60s")
			every, err := time.ParseDuration(pi)
			if err != nil || every < 0 {
				return web.ReloadableConfig{}, fmt.Errorf("invalid pull interval %q", pi)
			}
			return web.ReloadableConfig{
				BoardName: cfg.BoardName,
				PullEvery: every,
				Addr:      resolveOpt(fl, "addr", f.addr, "KBRD_ADDR", cfg.Serve.Addr, ":8080"),
				Domain:    resolveOpt(fl, "domain", f.domain, "KBRD_DOMAIN", cfg.Serve.Domain, ""),
			}, nil
		},
		ConfigWatch:    configWatch,
		ConfigFile:     filepath.Join(dir, config.FolderConfigFile),
		ValidateConfig: config.ValidateServe,
	}
	return web.Run(ctx, opts)
}

// initBoard prepares the board directory while the splash page is up: clone
// on first boot (empty volume), pull on subsequent ones, then load config for
// the display name. Board-supplied code never runs here.
func initBoard(dir, gitURL, instanceName string, needsClone bool, setStatus func(string)) (string, error) {
	switch {
	case needsClone:
		setStatus("cloning board…")
		fmt.Printf("cloning %s into %s\n", fsutil.RedactCredentials(gitURL), dir)
		// Clone into the (possibly existing, empty) dir.
		if err := fsutil.GitCloneStreaming(gitURL, dir, os.Stderr); err != nil {
			return "", err
		}
		fmt.Println("clone done")
	case fsutil.GitRepoRoot(dir) != "" && fsutil.GitHasRemote(fsutil.GitRepoRoot(dir)):
		setStatus("pulling latest…")
		fmt.Println("repo present, pulling")
		// Boot is unattended like the rest of serve, so reconcile with the
		// self-healing merge (a clean boot tree makes this a fast-forward in
		// practice). A failed boot pull is not fatal: serve the local state.
		if sidecars, err := fsutil.GitMergeResolveSidecar(fsutil.GitRepoRoot(dir), instanceName, "kbrd-web", "kbrd@localhost"); err != nil {
			fmt.Fprintf(os.Stderr, "warning: boot pull failed: %v\n", err)
		} else {
			for _, p := range sidecars {
				fmt.Fprintf(os.Stderr, "boot sync created conflict copy %s\n", p)
			}
			// Resolving a conflict creates a local merge commit. Publish it before
			// serving the board: otherwise the later pull loop sees an up-to-date
			// merge with no newly-created sidecars and never pushes this result.
			if len(sidecars) > 0 {
				if err := fsutil.GitPush(fsutil.GitRepoRoot(dir)); err != nil {
					fmt.Fprintf(os.Stderr, "warning: boot sync push failed: %v\n", err)
				}
			}
		}
	}

	cfg, err := config.Load(dir)
	if err != nil {
		return "", err
	}
	return cfg.BoardName, nil
}

// dirMissingOrEmpty reports whether dir does not exist or contains no entries.
func dirMissingOrEmpty(dir string) (bool, error) {
	entries, err := os.ReadDir(dir)
	if errors.Is(err, fs.ErrNotExist) {
		return true, nil
	}
	if err != nil {
		return false, fmt.Errorf("cannot access board directory: %w", err)
	}
	return len(entries) == 0, nil
}
