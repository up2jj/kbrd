package main

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
}

const minTokenLen = 12

// newServeCmd builds `kbrd serve`: the headless web frontend. The TUI never
// runs; board-supplied code (scripting, hooks, template exec) never executes —
// serve is implicitly --safe. The MCP server never runs either: the inherited
// --mcp/--mcp-addr flags are rejected rather than silently ignored.
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
	cmd.Flags().StringVar(&f.addr, "addr", "", "listen address for plain HTTP (env KBRD_ADDR, default :8080); ignored when --domain is set")
	cmd.Flags().StringVar(&f.domain, "domain", "", "public domain: enables Let's Encrypt TLS on :443 + :80 (env KBRD_DOMAIN)")
	cmd.Flags().StringVar(&f.token, "token", "", "shared access token, min 12 chars (env KBRD_TOKEN)")
	cmd.Flags().StringVar(&f.dir, "dir", "", "board directory (default current directory)")
	cmd.Flags().StringVar(&f.gitURL, "git-url", "", "clone this repo into --dir when it is missing or empty (env GIT_URL)")
	cmd.Flags().StringVar(&f.pullInterval, "pull-interval", "", "background git pull interval, 0 to disable (env KBRD_PULL_INTERVAL, default 60s)")
	return cmd
}

// envDefault fills v from env when the flag was not set.
func envDefault(v, envKey, def string) string {
	if v != "" {
		return v
	}
	if e := os.Getenv(envKey); e != "" {
		return e
	}
	return def
}

func runServe(cmd *cobra.Command, f serveFlags) error {
	f.addr = envDefault(f.addr, "KBRD_ADDR", ":8080")
	f.domain = envDefault(f.domain, "KBRD_DOMAIN", "")
	f.token = envDefault(f.token, "KBRD_TOKEN", "")
	f.gitURL = envDefault(f.gitURL, "GIT_URL", "")
	f.pullInterval = envDefault(f.pullInterval, "KBRD_PULL_INTERVAL", "60s")

	if len(f.token) < minTokenLen {
		return fmt.Errorf("an access token of at least %d characters is required (--token or KBRD_TOKEN); generate one with: openssl rand -base64 24", minTokenLen)
	}
	pullEvery, err := time.ParseDuration(f.pullInterval)
	if err != nil || pullEvery < 0 {
		return fmt.Errorf("invalid pull interval %q", f.pullInterval)
	}

	dir := f.dir
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

	ctx, stop := signal.NotifyContext(cmd.Context(), os.Interrupt, syscall.SIGTERM)
	defer stop()

	fmt.Printf("kbrd %s serve: board %s\n", model.Version, dir)

	opts := web.Options{
		Addr:         f.addr,
		Domain:       f.domain,
		CertCacheDir: filepath.Join(filepath.Dir(dir), "certs"),
		BoardPath:    dir,
		BoardName:    filepath.Base(dir),
		Token:        f.token,
		AuthorName:   envDefault("", "GIT_AUTHOR_NAME", "kbrd-web"),
		AuthorEmail:  envDefault("", "GIT_AUTHOR_EMAIL", "kbrd@localhost"),
		PullEvery:    pullEvery,
		Init: func(setStatus func(string)) (string, error) {
			return initBoard(dir, f.gitURL, needsClone, setStatus)
		},
	}
	return web.Run(ctx, opts)
}

// initBoard prepares the board directory while the splash page is up: clone
// on first boot (empty volume), pull on subsequent ones, then load config for
// the display name. Board-supplied code never runs here.
func initBoard(dir, gitURL string, needsClone bool, setStatus func(string)) (string, error) {
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
		if err := fsutil.GitPullRebase(fsutil.GitRepoRoot(dir)); err != nil {
			// A failed boot pull is not fatal: serve the local state.
			fmt.Fprintf(os.Stderr, "warning: boot pull failed: %v\n", err)
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
