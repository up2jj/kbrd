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
	v.SetDefault("display.theme", "light")
	v.SetDefault("notify.backend", "auto")
	v.SetDefault("git.diff_tool", "auto")
	v.SetDefault("git.auto_sync_interval", "")

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
	}, nil
}
