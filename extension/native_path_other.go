//go:build !darwin && !linux && !windows

package extension

import (
	"fmt"
	"runtime"
)

func nativeHostManifestPath() (string, error) {
	return "", fmt.Errorf("Chrome native messaging is not supported on %s", runtime.GOOS)
}

func registerNativeHost(string) error { return nil }
