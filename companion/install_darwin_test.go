//go:build darwin

package companion

import (
	"bytes"
	"errors"
	"os"
	"path/filepath"
	"strings"
	"testing"
)

type exitCodeError int

func (e exitCodeError) Error() string { return "command failed" }
func (e exitCodeError) ExitCode() int { return int(e) }

func TestLaunchAgentPlist(t *testing.T) {
	plist := launchAgentPlist(`/Users/A & B/Applications/kbrd Companion.app`)
	for _, want := range [][]byte{
		[]byte(`<string>dev.kbrd.companion</string>`),
		[]byte(`<string>/usr/bin/open</string>`),
		[]byte(`<string>/Users/A &amp; B/Applications/kbrd Companion.app</string>`),
		[]byte(`<key>RunAtLoad</key>`),
	} {
		if !bytes.Contains(plist, want) {
			t.Fatalf("launch agent plist missing %q:\n%s", want, plist)
		}
	}
}

func TestRunRequiresInstalledCompanion(t *testing.T) {
	t.Setenv("HOME", t.TempDir())
	_, err := Run()
	if err == nil || !strings.Contains(err.Error(), "kbrd companion install") {
		t.Fatalf("Run() error = %v, want install guidance", err)
	}
}

func TestCompanionPlistAdvertisesCaptureService(t *testing.T) {
	for _, want := range [][]byte{
		[]byte(`<key>NSServices</key>`),
		[]byte(`<string>Capture in kbrd</string>`),
		[]byte(`<key>NSMessage</key><string>captureInKbrd</string>`),
		[]byte(`<string>public.html</string>`),
		[]byte(`<string>public.rtf</string>`),
		[]byte(`<string>public.utf8-plain-text</string>`),
		[]byte(`<string>public.url</string>`),
	} {
		if !bytes.Contains(companionInfoPlist, want) {
			t.Fatalf("companion Info.plist missing %q:\n%s", want, companionInfoPlist)
		}
	}
}

func TestUninstallRemovesCompanionArtifacts(t *testing.T) {
	home := t.TempDir()
	t.Setenv("HOME", home)
	appPath := filepath.Join(home, "Applications", appName)
	launchAgentPath := filepath.Join(home, "Library", "LaunchAgents", launchAgentLabel+".plist")
	if err := os.MkdirAll(appPath, 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.MkdirAll(filepath.Dir(launchAgentPath), 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(launchAgentPath, []byte("plist"), 0o644); err != nil {
		t.Fatal(err)
	}

	previousRunUninstallCommand := runUninstallCommand
	t.Cleanup(func() { runUninstallCommand = previousRunUninstallCommand })
	var commands [][]string
	runUninstallCommand = func(name string, args ...string) ([]byte, error) {
		commands = append(commands, append([]string{name}, args...))
		return nil, nil
	}

	removed, err := Uninstall()
	if err != nil {
		t.Fatal(err)
	}
	if !removed {
		t.Fatal("Uninstall() removed = false, want true")
	}
	for _, path := range []string{appPath, launchAgentPath} {
		if _, err := os.Lstat(path); !os.IsNotExist(err) {
			t.Fatalf("artifact %s still exists or could not be inspected: %v", path, err)
		}
	}
	if len(commands) != 2 {
		t.Fatalf("commands = %#v, want launchctl and pkill", commands)
	}
	if got := strings.Join(commands[0], " "); !strings.Contains(got, "launchctl bootout") || !strings.Contains(got, launchAgentPath) {
		t.Fatalf("launchctl command = %q", got)
	}
	if got := strings.Join(commands[1], " "); !strings.Contains(got, "pkill") || !strings.Contains(got, "kbrd-companion") {
		t.Fatalf("pkill command = %q", got)
	}

	commands = nil
	removed, err = Uninstall()
	if err != nil {
		t.Fatal(err)
	}
	if removed {
		t.Fatal("second Uninstall() removed = true, want false")
	}
	if len(commands) != 0 {
		t.Fatalf("second Uninstall() commands = %#v, want none", commands)
	}
}

func TestUninstallIgnoresAlreadyStoppedCompanion(t *testing.T) {
	home := t.TempDir()
	t.Setenv("HOME", home)
	appPath := filepath.Join(home, "Applications", appName)
	if err := os.MkdirAll(appPath, 0o755); err != nil {
		t.Fatal(err)
	}

	previousRunUninstallCommand := runUninstallCommand
	t.Cleanup(func() { runUninstallCommand = previousRunUninstallCommand })
	runUninstallCommand = func(string, ...string) ([]byte, error) {
		return nil, exitCodeError(1)
	}

	removed, err := Uninstall()
	if err != nil {
		t.Fatal(err)
	}
	if !removed {
		t.Fatal("Uninstall() removed = false, want true")
	}
}

func TestLaunchAgentNotLoaded(t *testing.T) {
	for _, message := range []string{
		"Boot-out failed: 3: No such process",
		"Could not find specified service",
		"Service is not loaded",
		"Boot-out failed: 5: Input/output error",
	} {
		if !launchAgentNotLoaded([]byte(message)) {
			t.Errorf("launchAgentNotLoaded(%q) = false, want true", message)
		}
	}
	if launchAgentNotLoaded([]byte("Boot-out failed: 1: Operation not permitted")) {
		t.Fatal("launchAgentNotLoaded treated a permission failure as benign")
	}
}

func TestUninstallContinuesAfterCommandFailures(t *testing.T) {
	home := t.TempDir()
	t.Setenv("HOME", home)
	appPath := filepath.Join(home, "Applications", appName)
	launchAgentPath := filepath.Join(home, "Library", "LaunchAgents", launchAgentLabel+".plist")
	if err := os.MkdirAll(appPath, 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.MkdirAll(filepath.Dir(launchAgentPath), 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(launchAgentPath, []byte("plist"), 0o644); err != nil {
		t.Fatal(err)
	}

	previousRunUninstallCommand := runUninstallCommand
	t.Cleanup(func() { runUninstallCommand = previousRunUninstallCommand })
	runUninstallCommand = func(string, ...string) ([]byte, error) {
		return []byte("permission denied"), errors.New("exit status 1")
	}

	removed, err := Uninstall()
	if !removed {
		t.Fatal("Uninstall() removed = false, want true")
	}
	if err == nil || !strings.Contains(err.Error(), "disable companion login item") || !strings.Contains(err.Error(), "stop companion") {
		t.Fatalf("Uninstall() error = %v, want both command failures", err)
	}
	for _, path := range []string{appPath, launchAgentPath} {
		if _, statErr := os.Lstat(path); !os.IsNotExist(statErr) {
			t.Fatalf("artifact %s still exists or could not be inspected: %v", path, statErr)
		}
	}
}
