//go:build darwin

package companion

import (
	"bytes"
	_ "embed"
	"encoding/xml"
	"fmt"
	"os"
	"os/exec"
	"path/filepath"
	"strings"

	kbrdfs "kbrd/fs"
)

const (
	appName          = "kbrd Companion.app"
	launchAgentLabel = "dev.kbrd.companion"
)

//go:generate ./assets/build.sh

//go:embed assets/kbrd-companion
var companionExecutable []byte

//go:embed assets/Info.plist
var companionInfoPlist []byte

var launchCompanion = func(appPath string) ([]byte, error) {
	return exec.Command("/usr/bin/open", appPath).CombinedOutput()
}

func Install(launch bool) (string, error) {
	home, err := os.UserHomeDir()
	if err != nil {
		return "", fmt.Errorf("locate home directory: %w", err)
	}
	executable, err := os.Executable()
	if err != nil {
		return "", fmt.Errorf("locate kbrd executable: %w", err)
	}
	executable, err = filepath.Abs(executable)
	if err != nil {
		return "", fmt.Errorf("resolve kbrd executable: %w", err)
	}
	kbrdExecutable, err := os.ReadFile(executable)
	if err != nil {
		return "", fmt.Errorf("read kbrd executable: %w", err)
	}
	appsDir := filepath.Join(home, "Applications")
	appPath := filepath.Join(appsDir, appName)
	tmp, err := os.MkdirTemp(appsDir, ".kbrd-companion-*")
	if err != nil {
		if os.IsNotExist(err) {
			if mkdirErr := os.MkdirAll(appsDir, 0o755); mkdirErr != nil {
				return "", fmt.Errorf("create Applications directory: %w", mkdirErr)
			}
			tmp, err = os.MkdirTemp(appsDir, ".kbrd-companion-*")
		}
		if err != nil {
			return "", fmt.Errorf("prepare companion bundle: %w", err)
		}
	}
	defer os.RemoveAll(tmp)
	bundle := filepath.Join(tmp, appName)
	macOSDir := filepath.Join(bundle, "Contents", "MacOS")
	resourcesDir := filepath.Join(bundle, "Contents", "Resources")
	if err := os.MkdirAll(macOSDir, 0o755); err != nil {
		return "", err
	}
	if err := os.MkdirAll(resourcesDir, 0o755); err != nil {
		return "", err
	}
	files := []struct {
		path string
		data []byte
		mode os.FileMode
	}{
		{filepath.Join(bundle, "Contents", "Info.plist"), companionInfoPlist, 0o644},
		{filepath.Join(macOSDir, "kbrd-companion"), companionExecutable, 0o755},
		{filepath.Join(resourcesDir, "kbrd"), kbrdExecutable, 0o755},
	}
	for _, file := range files {
		if err := os.WriteFile(file.path, file.data, file.mode); err != nil {
			return "", fmt.Errorf("write companion bundle: %w", err)
		}
	}
	if out, err := exec.Command("/usr/bin/codesign", "--force", "--deep", "--sign", "-", bundle).CombinedOutput(); err != nil {
		return "", fmt.Errorf("sign companion: %s", strings.TrimSpace(string(out)))
	}
	if err := os.RemoveAll(appPath); err != nil {
		return "", fmt.Errorf("replace companion: %w", err)
	}
	if err := os.Rename(bundle, appPath); err != nil {
		return "", fmt.Errorf("install companion: %w", err)
	}
	if err := installLaunchAgent(home, appPath); err != nil {
		return "", err
	}
	if launch {
		if out, err := launchCompanion(appPath); err != nil {
			return "", fmt.Errorf("launch companion: %s", strings.TrimSpace(string(out)))
		}
	}
	return appPath, nil
}

// Run launches the installed companion without changing its bundle or login item.
func Run() (string, error) {
	home, err := os.UserHomeDir()
	if err != nil {
		return "", fmt.Errorf("locate home directory: %w", err)
	}
	appPath := filepath.Join(home, "Applications", appName)
	info, err := os.Stat(appPath)
	if err != nil {
		if os.IsNotExist(err) {
			return "", fmt.Errorf("kbrd Companion is not installed; run `kbrd companion install`")
		}
		return "", fmt.Errorf("inspect companion installation: %w", err)
	}
	if !info.IsDir() {
		return "", fmt.Errorf("invalid companion installation at %s; run `kbrd companion install`", appPath)
	}
	if out, err := launchCompanion(appPath); err != nil {
		return "", fmt.Errorf("launch companion: %s", strings.TrimSpace(string(out)))
	}
	return appPath, nil
}

func installLaunchAgent(home, appPath string) error {
	dir := filepath.Join(home, "Library", "LaunchAgents")
	if err := os.MkdirAll(dir, 0o755); err != nil {
		return fmt.Errorf("create LaunchAgents directory: %w", err)
	}
	path := filepath.Join(dir, launchAgentLabel+".plist")
	if err := kbrdfs.WriteFileAtomicDurable(path, launchAgentPlist(appPath), 0o644); err != nil {
		return fmt.Errorf("install companion login item: %w", err)
	}
	return nil
}

func launchAgentPlist(appPath string) []byte {
	var escaped bytes.Buffer
	_ = xml.EscapeText(&escaped, []byte(appPath))
	return []byte(`<?xml version="1.0" encoding="UTF-8"?>
<!DOCTYPE plist PUBLIC "-//Apple//DTD PLIST 1.0//EN" "http://www.apple.com/DTDs/PropertyList-1.0.dtd">
<plist version="1.0">
<dict>
  <key>Label</key>
  <string>` + launchAgentLabel + `</string>
  <key>ProgramArguments</key>
  <array>
    <string>/usr/bin/open</string>
    <string>-a</string>
    <string>` + escaped.String() + `</string>
  </array>
  <key>RunAtLoad</key>
  <true/>
  <key>LimitLoadToSessionType</key>
  <string>Aqua</string>
</dict>
</plist>
`)
}
