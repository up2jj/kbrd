//go:build darwin

package model

import (
	"crypto/sha256"
	_ "embed"
	"encoding/hex"
	"fmt"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
)

const shareHelperBundleName = "kbrd Share Card.app"

//go:generate ./assets/build-kbrd-share-helper.sh

//go:embed assets/kbrd-share-helper
var shareHelperExecutable []byte

//go:embed assets/kbrd-share-Info.plist
var shareHelperInfoPlist []byte

var shareUserCacheDir = os.UserCacheDir

var signShareHelper = func(appPath string) ([]byte, error) {
	return exec.Command("/usr/bin/codesign", "--force", "--sign", "-", appPath).CombinedOutput()
}

var launchShareHelper = func(appPath, cardPath string) ([]byte, error) {
	return exec.Command(
		"/usr/bin/open", "-n", "-a", appPath, "--args", cardPath,
	).CombinedOutput()
}

func defaultShareFile(path string) error {
	info, err := os.Stat(path)
	if err != nil {
		return fmt.Errorf("open card: %w", err)
	}
	if !info.Mode().IsRegular() {
		return fmt.Errorf("card is not a regular file")
	}
	absPath, err := filepath.Abs(path)
	if err != nil {
		return fmt.Errorf("resolve card path: %w", err)
	}
	appPath, err := ensureShareHelper()
	if err != nil {
		return err
	}
	out, err := launchShareHelper(appPath, absPath)
	if err == nil {
		return nil
	}
	detail := strings.TrimSpace(string(out))
	if detail == "" {
		detail = err.Error()
	}
	return fmt.Errorf("open macOS share sheet: %s", detail)
}

func ensureShareHelper() (string, error) {
	cacheDir, err := shareUserCacheDir()
	if err != nil {
		return "", fmt.Errorf("locate cache directory: %w", err)
	}

	hash := sha256.New()
	_, _ = hash.Write(shareHelperExecutable)
	_, _ = hash.Write(shareHelperInfoPlist)
	version := hex.EncodeToString(hash.Sum(nil))[:12]
	appPath := filepath.Join(cacheDir, "kbrd", "share-helper-"+version, shareHelperBundleName)
	markerPath := filepath.Join(filepath.Dir(appPath), ".ready")
	if marker, readErr := os.ReadFile(markerPath); readErr == nil && string(marker) == version {
		return appPath, nil
	}

	macOSDir := filepath.Join(appPath, "Contents", "MacOS")
	if err := os.MkdirAll(macOSDir, 0o755); err != nil {
		return "", fmt.Errorf("create share helper bundle: %w", err)
	}
	if err := os.WriteFile(filepath.Join(appPath, "Contents", "Info.plist"), shareHelperInfoPlist, 0o644); err != nil {
		return "", fmt.Errorf("write share helper metadata: %w", err)
	}
	if err := os.WriteFile(filepath.Join(macOSDir, "kbrd-share"), shareHelperExecutable, 0o755); err != nil {
		return "", fmt.Errorf("write share helper executable: %w", err)
	}
	if out, err := signShareHelper(appPath); err != nil {
		detail := strings.TrimSpace(string(out))
		if detail == "" {
			detail = err.Error()
		}
		return "", fmt.Errorf("sign share helper: %s", detail)
	}
	if err := os.WriteFile(markerPath, []byte(version), 0o644); err != nil {
		return "", fmt.Errorf("finish share helper installation: %w", err)
	}
	return appPath, nil
}
