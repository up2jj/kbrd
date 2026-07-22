//go:build windows

package extension

import (
	"fmt"
	"os"
	"path/filepath"

	"golang.org/x/sys/windows/registry"
)

func nativeHostManifestPath() (string, error) {
	dir, err := os.UserConfigDir()
	if err != nil {
		return "", fmt.Errorf("user config dir: %w", err)
	}
	return filepath.Join(dir, "kbrd", "native-messaging", NativeHostName+".json"), nil
}

func registerNativeHost(manifestPath string) error {
	key, _, err := registry.CreateKey(registry.CURRENT_USER, `Software\Google\Chrome\NativeMessagingHosts\`+NativeHostName, registry.SET_VALUE)
	if err != nil {
		return fmt.Errorf("create Chrome native host registry key: %w", err)
	}
	defer key.Close()
	if err := key.SetStringValue("", manifestPath); err != nil {
		return fmt.Errorf("register Chrome native host manifest: %w", err)
	}
	return nil
}
