package model

import (
	"fmt"
	"os"
	"testing"
)

// TestMain keeps model tests from reading or writing the user's kbrd state.
// Several tests exercise complete board switches, which intentionally update
// the recent-board registry in normal application use.
func TestMain(m *testing.M) {
	home, err := os.MkdirTemp("", "kbrd-model-test-home-")
	if err != nil {
		fmt.Fprintf(os.Stderr, "create model test home: %v\n", err)
		os.Exit(1)
	}
	if err := os.Setenv("HOME", home); err != nil {
		fmt.Fprintf(os.Stderr, "set model test home: %v\n", err)
		os.Exit(1)
	}
	if err := os.Setenv("XDG_CONFIG_HOME", home); err != nil {
		fmt.Fprintf(os.Stderr, "set model test config home: %v\n", err)
		os.Exit(1)
	}

	code := m.Run()
	if err := os.RemoveAll(home); err != nil && code == 0 {
		fmt.Fprintf(os.Stderr, "remove model test home: %v\n", err)
		code = 1
	}
	os.Exit(code)
}
