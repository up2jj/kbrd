//go:build darwin

package companion

import (
	"bytes"
	_ "embed"
	"encoding/xml"
	"errors"
	"fmt"
	"os"
	"os/exec"
	"path/filepath"
	"strconv"
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

var runUninstallCommand = func(name string, args ...string) ([]byte, error) {
	return exec.Command(name, args...).CombinedOutput()
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

// Uninstall stops the companion and removes its app bundle and login item.
// The returned boolean reports whether either installed artifact existed.
func Uninstall() (bool, error) {
	home, err := os.UserHomeDir()
	if err != nil {
		return false, fmt.Errorf("locate home directory: %w", err)
	}
	appPath := filepath.Join(home, "Applications", appName)
	launchAgentPath := filepath.Join(home, "Library", "LaunchAgents", launchAgentLabel+".plist")

	appExists, err := pathExists(appPath)
	if err != nil {
		return false, fmt.Errorf("inspect companion installation: %w", err)
	}
	launchAgentExists, err := pathExists(launchAgentPath)
	if err != nil {
		return false, fmt.Errorf("inspect companion login item: %w", err)
	}
	if !appExists && !launchAgentExists {
		return false, nil
	}

	var uninstallErrors []error
	if launchAgentExists {
		domain := "gui/" + strconv.Itoa(os.Getuid())
		out, commandErr := runUninstallCommand("/bin/launchctl", "bootout", domain, launchAgentPath)
		if commandErr != nil && !launchAgentNotLoaded(out) {
			uninstallErrors = append(uninstallErrors, commandError("disable companion login item", out, commandErr))
		}
	}
	out, commandErr := runUninstallCommand("/usr/bin/pkill", "-u", strconv.Itoa(os.Getuid()), "-x", "kbrd-companion")
	if commandErr != nil && !processNotRunning(commandErr) {
		uninstallErrors = append(uninstallErrors, commandError("stop companion", out, commandErr))
	}
	if err := os.Remove(launchAgentPath); err != nil && !os.IsNotExist(err) {
		uninstallErrors = append(uninstallErrors, fmt.Errorf("remove companion login item: %w", err))
	}
	if err := os.RemoveAll(appPath); err != nil {
		uninstallErrors = append(uninstallErrors, fmt.Errorf("remove companion app: %w", err))
	}
	return true, errors.Join(uninstallErrors...)
}

func pathExists(path string) (bool, error) {
	_, err := os.Lstat(path)
	if err == nil {
		return true, nil
	}
	if os.IsNotExist(err) {
		return false, nil
	}
	return false, err
}

func launchAgentNotLoaded(output []byte) bool {
	message := strings.ToLower(string(output))
	return strings.Contains(message, "no such process") ||
		strings.Contains(message, "could not find specified service") ||
		strings.Contains(message, "service is not loaded") ||
		strings.Contains(message, "input/output error")
}

func processNotRunning(err error) bool {
	var exitErr interface{ ExitCode() int }
	return errors.As(err, &exitErr) && exitErr.ExitCode() == 1
}

func commandError(action string, output []byte, err error) error {
	message := strings.TrimSpace(string(output))
	if message == "" {
		return fmt.Errorf("%s: %w", action, err)
	}
	return fmt.Errorf("%s: %s: %w", action, message, err)
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
