package model

import (
	"os"
	"os/exec"
	"runtime"
)

var openFile = defaultOpenFile

func defaultOpenFile(path string) error {
	switch runtime.GOOS {
	case "darwin":
		cmd := exec.Command("open", path)
		return cmd.Run()
	case "linux":
		cmd := exec.Command("xdg-open", path)
		return cmd.Run()
	case "windows":
		cmd := exec.Command("cmd", "/c", "start", "", path)
		return cmd.Run()
	default:
		return os.ErrInvalid
	}
}
