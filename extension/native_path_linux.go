//go:build linux

package extension

import (
	"fmt"
	"os"
	"path/filepath"
)

func nativeHostManifestPath() (string, error) {
	home, err := os.UserHomeDir()
	if err != nil {
		return "", fmt.Errorf("user home dir: %w", err)
	}
	return filepath.Join(home, ".config", "google-chrome", "NativeMessagingHosts", NativeHostName+".json"), nil
}

func registerNativeHost(string) error { return nil }
