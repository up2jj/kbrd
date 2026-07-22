//go:build darwin

package model

import (
	"errors"
	"os"
	"path/filepath"
	"strings"
	"testing"
)

func TestDefaultShareFileInstallsAndLaunchesNativeHelper(t *testing.T) {
	card := filepath.Join(t.TempDir(), "card.md")
	if err := os.WriteFile(card, []byte("hello\n"), 0o644); err != nil {
		t.Fatal(err)
	}

	cacheDir := t.TempDir()
	oldCache := shareUserCacheDir
	oldSign := signShareHelper
	oldLaunch := launchShareHelper
	t.Cleanup(func() {
		shareUserCacheDir = oldCache
		signShareHelper = oldSign
		launchShareHelper = oldLaunch
	})
	shareUserCacheDir = func() (string, error) { return cacheDir, nil }

	signCalls := 0
	signShareHelper = func(appPath string) ([]byte, error) {
		signCalls++
		if filepath.Base(appPath) != shareHelperBundleName {
			t.Fatalf("signed app = %q", appPath)
		}
		return nil, nil
	}
	var gotApp, gotCard string
	launchShareHelper = func(appPath, cardPath string) ([]byte, error) {
		gotApp, gotCard = appPath, cardPath
		return nil, nil
	}

	if err := defaultShareFile(card); err != nil {
		t.Fatalf("defaultShareFile: %v", err)
	}
	if gotCard != card {
		t.Fatalf("helper card path = %q, want %q", gotCard, card)
	}
	if gotApp == "" {
		t.Fatal("helper app path is empty")
	}
	executable := filepath.Join(gotApp, "Contents", "MacOS", "kbrd-share")
	info, err := os.Stat(executable)
	if err != nil {
		t.Fatalf("stat helper executable: %v", err)
	}
	if info.Mode().Perm() != 0o755 {
		t.Fatalf("helper mode = %o, want 755", info.Mode().Perm())
	}
	if _, err := os.Stat(filepath.Join(gotApp, "Contents", "Info.plist")); err != nil {
		t.Fatalf("stat helper Info.plist: %v", err)
	}

	if err := defaultShareFile(card); err != nil {
		t.Fatalf("second defaultShareFile: %v", err)
	}
	if signCalls != 1 {
		t.Fatalf("helper signed %d times, want 1", signCalls)
	}
}

func TestDefaultShareFileReportsHelperLaunchFailure(t *testing.T) {
	card := filepath.Join(t.TempDir(), "card.md")
	if err := os.WriteFile(card, nil, 0o644); err != nil {
		t.Fatal(err)
	}

	oldCache := shareUserCacheDir
	oldSign := signShareHelper
	oldLaunch := launchShareHelper
	t.Cleanup(func() {
		shareUserCacheDir = oldCache
		signShareHelper = oldSign
		launchShareHelper = oldLaunch
	})
	shareUserCacheDir = func() (string, error) { return t.TempDir(), nil }
	signShareHelper = func(string) ([]byte, error) { return nil, nil }
	launchShareHelper = func(string, string) ([]byte, error) {
		return []byte("Launch Services rejected the app"), errors.New("exit status 1")
	}

	err := defaultShareFile(card)
	if err == nil || !strings.Contains(err.Error(), "Launch Services rejected the app") {
		t.Fatalf("launch error = %v", err)
	}
}

func TestEnsureShareHelperReportsSigningFailure(t *testing.T) {
	oldCache := shareUserCacheDir
	oldSign := signShareHelper
	t.Cleanup(func() {
		shareUserCacheDir = oldCache
		signShareHelper = oldSign
	})
	shareUserCacheDir = func() (string, error) { return t.TempDir(), nil }
	signShareHelper = func(string) ([]byte, error) {
		return []byte("bad signature"), errors.New("exit status 1")
	}

	_, err := ensureShareHelper()
	if err == nil || !strings.Contains(err.Error(), "bad signature") {
		t.Fatalf("signing error = %v", err)
	}
}

func TestEmbeddedShareHelperIsUniversalMachO(t *testing.T) {
	if len(shareHelperExecutable) < 4 {
		t.Fatalf("embedded helper is only %d bytes", len(shareHelperExecutable))
	}
	if got := shareHelperExecutable[:4]; string(got) != "\xca\xfe\xba\xbe" {
		t.Fatalf("helper magic = %x, want universal Mach-O", got)
	}
	if !strings.Contains(string(shareHelperInfoPlist), "dev.kbrd.share-helper") {
		t.Fatal("helper Info.plist is missing its bundle identifier")
	}
}
