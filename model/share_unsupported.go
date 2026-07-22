//go:build !darwin

package model

import (
	"fmt"
	"runtime"
)

func defaultShareFile(_ string) error {
	return fmt.Errorf("system sharing is not available on %s", runtime.GOOS)
}
