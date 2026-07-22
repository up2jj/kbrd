//go:build !darwin

package companion

import "fmt"

func Install(bool) (string, error) {
	return "", fmt.Errorf("the menu-bar companion is only available on macOS")
}

func Run() (string, error) {
	return "", fmt.Errorf("the menu-bar companion is only available on macOS")
}
