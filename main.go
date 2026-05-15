package main

import (
	"errors"
	"fmt"
	"io/fs"
	"os"
	"path/filepath"

	"kbrd/config"
	"kbrd/model"
	"kbrd/recents"

	tea "github.com/charmbracelet/bubbletea"
	"github.com/spf13/pflag"
)

func main() {
	flags := pflag.NewFlagSet("kbrd", pflag.ExitOnError)
	initGlobal := flags.Bool("init-config", false, "write a TOML config template to the user config dir and exit")
	initLocal := flags.Bool("init-local-config", false, "write a TOML config template to the current directory and exit")
	if err := flags.Parse(os.Args[1:]); err != nil {
		fmt.Fprintf(os.Stderr, "error: %v\n", err)
		os.Exit(2)
	}

	cwd, err := os.Getwd()
	if err != nil {
		fmt.Fprintf(os.Stderr, "error: cannot determine working directory: %v\n", err)
		os.Exit(1)
	}

	if *initGlobal {
		if err := writeGlobalTemplate(); err != nil {
			fmt.Fprintf(os.Stderr, "error: %v\n", err)
			os.Exit(1)
		}
		return
	}
	if *initLocal {
		if err := writeLocalTemplate(cwd); err != nil {
			fmt.Fprintf(os.Stderr, "error: %v\n", err)
			os.Exit(1)
		}
		return
	}

	info, err := os.Stat(cwd)
	if err != nil {
		fmt.Fprintf(os.Stderr, "error: cannot access path: %v\n", err)
		os.Exit(1)
	}
	if !info.IsDir() {
		fmt.Fprintf(os.Stderr, "error: path is not a directory\n")
		os.Exit(1)
	}

	cfg, err := config.Load(cwd)
	if err != nil {
		fmt.Fprintf(os.Stderr, "error: %v\n", err)
		os.Exit(1)
	}

	if abs, absErr := filepath.Abs(cwd); absErr == nil {
		store, _ := recents.Load()
		store.Touch(abs, cfg.BoardName)
		_ = store.Save()
	}

	m := model.NewBoard(cfg)

	p := tea.NewProgram(m, tea.WithAltScreen(), tea.WithMouseCellMotion())
	if _, err := p.Run(); err != nil {
		fmt.Fprintf(os.Stderr, "error: %v\n", err)
		os.Exit(1)
	}
}

func writeGlobalTemplate() error {
	dir, err := os.UserConfigDir()
	if err != nil {
		return fmt.Errorf("user config dir: %w", err)
	}
	appDir := filepath.Join(dir, config.AppDirName)
	if err := os.MkdirAll(appDir, 0o755); err != nil {
		return fmt.Errorf("create %s: %w", appDir, err)
	}
	target := filepath.Join(appDir, config.GlobalConfigFile)
	return writeTemplate(target)
}

func writeLocalTemplate(cwd string) error {
	return writeTemplate(filepath.Join(cwd, config.FolderConfigFile))
}

func writeTemplate(target string) error {
	if _, err := os.Stat(target); err == nil {
		return fmt.Errorf("refusing to overwrite existing file: %s", target)
	} else if !errors.Is(err, fs.ErrNotExist) {
		return fmt.Errorf("stat %s: %w", target, err)
	}
	if err := os.WriteFile(target, config.Template, 0o644); err != nil {
		return fmt.Errorf("write %s: %w", target, err)
	}
	fmt.Printf("wrote %s\n", target)
	return nil
}
