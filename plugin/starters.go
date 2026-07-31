package plugin

import (
	"errors"
	"fmt"
	"io/fs"
	"math/rand/v2"
	"os"
	"path/filepath"
	"slices"
	"strconv"
	"strings"
)

// BoardStarter is one named starter directory from a verified plugin.
type BoardStarter struct {
	PluginID string
	Name     string
	root     string
}

type starterEntry struct {
	source   string
	relative string
	mode     fs.FileMode
	dir      bool
}

type starterCopyPlan struct {
	root    string
	target  string
	force   bool
	entries []starterEntry
}

// BoardStarters lists the immediate child directories of every enabled
// plugin's boardStarters asset directory.
func (m *Manager) BoardStarters(boardDir string) ([]BoardStarter, error) {
	packs, err := m.RuntimeAssetPacks(boardDir)
	if err != nil {
		return nil, err
	}
	var starters []BoardStarter
	for _, pack := range packs {
		loaded, err := startersInPack(pack)
		if err != nil {
			return nil, err
		}
		starters = append(starters, loaded...)
	}
	slices.SortFunc(starters, compareBoardStarters)
	return starters, nil
}

func startersInPack(pack RuntimeAssets) ([]BoardStarter, error) {
	if pack.BoardStarters == "" {
		return nil, nil
	}
	entries, err := os.ReadDir(pack.BoardStarters)
	if err != nil {
		return nil, fmt.Errorf("plugin %s board starters: %w", pack.ID, err)
	}
	var starters []BoardStarter
	for _, entry := range entries {
		if !entry.IsDir() || strings.HasPrefix(entry.Name(), ".") {
			continue
		}
		starters = append(starters, BoardStarter{
			PluginID: pack.ID,
			Name:     entry.Name(),
			root:     filepath.Join(pack.BoardStarters, entry.Name()),
		})
	}
	return starters, nil
}

func compareBoardStarters(a, b BoardStarter) int {
	if a.PluginID != b.PluginID {
		return cmpString(a.PluginID, b.PluginID)
	}
	return cmpString(a.Name, b.Name)
}

// ApplyBoardStarter copies a verified starter into target without touching the
// board lock or deleting existing content. Existing files are rejected unless
// force is true; .git and kbrd.plugins.lock are never copied.
func (m *Manager) ApplyBoardStarter(boardDir, pluginID, name, target string, force bool) error {
	starters, err := m.BoardStarters(boardDir)
	if err != nil {
		return err
	}
	starter, ok := findBoardStarter(starters, pluginID, name)
	if !ok {
		return fmt.Errorf("plugin %q has no board starter %q", pluginID, name)
	}
	if target == "" {
		target = boardDir
	}
	if err := starter.apply(target, force); err != nil {
		return fmt.Errorf("apply board starter %s/%s: %w", pluginID, name, err)
	}
	return nil
}

func findBoardStarter(starters []BoardStarter, pluginID, name string) (BoardStarter, bool) {
	for _, starter := range starters {
		if starter.PluginID == pluginID && starter.Name == name {
			return starter, true
		}
	}
	return BoardStarter{}, false
}

func (s BoardStarter) apply(target string, force bool) error {
	target, err := filepath.Abs(target)
	if err != nil {
		return fmt.Errorf("resolve starter target: %w", err)
	}
	plan := starterCopyPlan{root: s.root, target: target, force: force}
	if err := plan.build(); err != nil {
		return err
	}
	return plan.apply()
}

func (p *starterCopyPlan) build() error {
	if err := filepath.WalkDir(p.root, p.visit); err != nil {
		return err
	}
	return nil
}

func (p *starterCopyPlan) visit(path string, entry os.DirEntry, walkErr error) error {
	if walkErr != nil {
		return walkErr
	}
	if path == p.root {
		return nil
	}
	planned, err := p.planEntry(path, entry)
	if err != nil {
		return err
	}
	p.entries = append(p.entries, planned)
	return nil
}

func (p *starterCopyPlan) planEntry(path string, entry os.DirEntry) (starterEntry, error) {
	rel, err := filepath.Rel(p.root, path)
	if err != nil {
		return starterEntry{}, err
	}
	if isProtectedStarterPath(rel) {
		return starterEntry{}, fmt.Errorf("starter contains protected path %q", rel)
	}
	info, err := entry.Info()
	if err != nil {
		return starterEntry{}, err
	}
	if !info.IsDir() && !info.Mode().IsRegular() {
		return starterEntry{}, fmt.Errorf("starter contains non-regular file %q", rel)
	}
	return starterEntry{source: path, relative: rel, mode: info.Mode().Perm(), dir: info.IsDir()}, nil
}

func isProtectedStarterPath(rel string) bool {
	slashPath := filepath.ToSlash(rel)
	first, _, _ := strings.Cut(slashPath, "/")
	return first == ".git" || slashPath == LockFile
}

func (p *starterCopyPlan) apply() error {
	if err := os.MkdirAll(p.target, 0o755); err != nil {
		return fmt.Errorf("create starter target: %w", err)
	}
	root, err := os.OpenRoot(p.target)
	if err != nil {
		return fmt.Errorf("open starter target: %w", err)
	}
	defer root.Close()

	for _, entry := range p.entries {
		if err := validateStarterDestination(root, entry, p.force); err != nil {
			return err
		}
	}
	for _, entry := range p.entries {
		if err := entry.copy(root); err != nil {
			return err
		}
	}
	return nil
}

func validateStarterDestination(root *os.Root, entry starterEntry, force bool) error {
	if err := rejectStarterSymlinkParents(root, entry.relative); err != nil {
		return err
	}
	existing, err := root.Lstat(entry.relative)
	if errors.Is(err, os.ErrNotExist) {
		return nil
	}
	if err != nil {
		return err
	}
	if existing.Mode()&os.ModeSymlink != 0 {
		return fmt.Errorf("starter path %q conflicts with symlink", entry.relative)
	}
	if entry.dir {
		if !existing.IsDir() {
			return fmt.Errorf("starter directory %q conflicts with file", entry.relative)
		}
		return nil
	}
	if existing.IsDir() {
		return fmt.Errorf("starter file %q conflicts with directory", entry.relative)
	}
	if !force {
		return fmt.Errorf("starter file %q already exists; use --force to replace files", entry.relative)
	}
	return nil
}

func rejectStarterSymlinkParents(root *os.Root, rel string) error {
	parent := filepath.Dir(rel)
	if parent == "." {
		return nil
	}
	current := ""
	for _, part := range strings.Split(parent, string(filepath.Separator)) {
		current = filepath.Join(current, part)
		info, err := root.Lstat(current)
		if errors.Is(err, os.ErrNotExist) {
			return nil
		}
		if err != nil {
			return err
		}
		if info.Mode()&os.ModeSymlink != 0 {
			return fmt.Errorf("starter path %q has symlinked parent %q", rel, current)
		}
		if !info.IsDir() {
			return fmt.Errorf("starter path %q has non-directory parent %q", rel, current)
		}
	}
	return nil
}

func (e starterEntry) copy(root *os.Root) error {
	if e.dir {
		if err := root.MkdirAll(e.relative, e.mode); err != nil {
			return fmt.Errorf("create starter directory %q: %w", e.relative, err)
		}
		return nil
	}
	data, err := os.ReadFile(e.source)
	if err != nil {
		return err
	}
	if err := root.MkdirAll(filepath.Dir(e.relative), 0o755); err != nil {
		return err
	}
	if err := writeStarterFileAtomic(root, e.relative, data, e.mode); err != nil {
		return fmt.Errorf("write starter file %q: %w", e.relative, err)
	}
	return nil
}

func writeStarterFileAtomic(root *os.Root, name string, data []byte, mode fs.FileMode) error {
	dir, base := filepath.Split(name)
	dir = filepath.Clean(dir)
	for range 100 {
		tempName := filepath.Join(dir, "."+base+".tmp-"+strconv.FormatUint(rand.Uint64(), 36))
		file, err := root.OpenFile(tempName, os.O_CREATE|os.O_EXCL|os.O_WRONLY, mode)
		if errors.Is(err, os.ErrExist) {
			continue
		}
		if err != nil {
			return err
		}
		cleanup := true
		defer func() {
			if cleanup {
				_ = root.Remove(tempName)
			}
		}()
		if err := file.Chmod(mode); err != nil {
			_ = file.Close()
			return err
		}
		if _, err := file.Write(data); err != nil {
			_ = file.Close()
			return err
		}
		if err := file.Sync(); err != nil {
			_ = file.Close()
			return err
		}
		if err := file.Close(); err != nil {
			return err
		}
		if err := root.Rename(tempName, name); err != nil {
			return err
		}
		cleanup = false
		if parent, err := root.Open(dir); err == nil {
			_ = parent.Sync()
			_ = parent.Close()
		}
		return nil
	}
	return fmt.Errorf("could not allocate temporary file")
}
