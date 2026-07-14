// Package boardenv applies directory-scoped environments to the kbrd process.
package boardenv

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"os"
	"os/exec"
	"path/filepath"
	"slices"
	"strings"
	"sync"
	"time"
)

const exportTimeout = 30 * time.Second

var processEnvMu sync.Mutex

// Manager applies direnv transitions. New resolves the executable once so a
// board cannot make switching stop working by replacing PATH.
type Manager struct {
	executable string
}

// Change records the previous values touched by an environment transition.
// Restore is safe to call more than once.
type Change struct {
	before   map[string]previousValue
	restored bool
	mu       sync.Mutex
}

type previousValue struct {
	value string
	set   bool
}

// New returns a manager. A missing direnv executable disables integration,
// keeping direnv an optional runtime dependency.
func New() *Manager {
	executable, _ := exec.LookPath("direnv")
	return &Manager{executable: executable}
}

// Active reports whether the process currently carries an environment loaded
// by direnv. DIRENV_DIFF is direnv's session marker and is removed when the
// environment is unloaded.
func (m *Manager) Active() bool {
	return m != nil && os.Getenv("DIRENV_DIFF") != ""
}

// Apply asks direnv for the environment diff at dir and applies it to the
// current process. The returned Change can restore the exact previous values
// if the board fails to load. direnv's JSON output uses null to unset a key.
func (m *Manager) Apply(dir string) (*Change, error) {
	if m == nil || m.executable == "" {
		return nil, nil
	}

	absDir, err := filepath.Abs(dir)
	if err != nil {
		return nil, fmt.Errorf("resolve board path for direnv: %w", err)
	}

	ctx, cancel := context.WithTimeout(context.Background(), exportTimeout)
	defer cancel()

	cmd := exec.CommandContext(ctx, m.executable, "export", "json")
	cmd.Dir = absDir
	cmd.Env = os.Environ()
	out, err := cmd.Output()
	if err != nil {
		if ctx.Err() != nil {
			return nil, fmt.Errorf("direnv export for %s: %w", absDir, ctx.Err())
		}
		// Do not include stderr: .envrc is arbitrary code and may print secrets.
		return nil, fmt.Errorf("direnv export for %s failed: %w (approve trusted files with `direnv allow %s`)", absDir, err, absDir)
	}
	if len(strings.TrimSpace(string(out))) == 0 {
		return nil, nil
	}

	var diff map[string]*string
	if err := json.Unmarshal(out, &diff); err != nil {
		return nil, fmt.Errorf("decode direnv environment for %s: %w", absDir, err)
	}
	return apply(diff)
}

func apply(diff map[string]*string) (*Change, error) {
	if len(diff) == 0 {
		return nil, nil
	}

	processEnvMu.Lock()
	defer processEnvMu.Unlock()

	change := &Change{before: make(map[string]previousValue, len(diff))}
	keys := mapsKeys(diff)
	slices.Sort(keys)
	for _, key := range keys {
		value, set := os.LookupEnv(key)
		change.before[key] = previousValue{value: value, set: set}
	}
	for _, key := range keys {
		var err error
		if diff[key] == nil {
			err = os.Unsetenv(key)
		} else {
			err = os.Setenv(key, *diff[key])
		}
		if err != nil {
			restoreErr := restore(change.before)
			return nil, errors.Join(fmt.Errorf("apply direnv variable %q: %w", key, err), restoreErr)
		}
	}
	return change, nil
}

// Restore puts every variable touched by the transition back to its previous
// value. It does not affect unrelated environment changes.
func (c *Change) Restore() error {
	if c == nil {
		return nil
	}
	c.mu.Lock()
	defer c.mu.Unlock()
	if c.restored {
		return nil
	}

	processEnvMu.Lock()
	err := restore(c.before)
	processEnvMu.Unlock()
	if err == nil {
		c.restored = true
	}
	return err
}

func restore(before map[string]previousValue) error {
	var errs []error
	keys := mapsKeys(before)
	slices.Sort(keys)
	for _, key := range keys {
		old := before[key]
		if old.set {
			if err := os.Setenv(key, old.value); err != nil {
				errs = append(errs, fmt.Errorf("restore environment variable %q: %w", key, err))
			}
			continue
		}
		if err := os.Unsetenv(key); err != nil {
			errs = append(errs, fmt.Errorf("unset environment variable %q: %w", key, err))
		}
	}
	return errors.Join(errs...)
}

func mapsKeys[V any](m map[string]V) []string {
	keys := make([]string, 0, len(m))
	for key := range m {
		keys = append(keys, key)
	}
	return keys
}
