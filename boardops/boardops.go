// Package boardops holds board workflows shared by headless adapters and the
// TUI's scripting bridge. It sits above package board's filesystem primitives
// and below UI-specific concerns: boardops never publishes events, changes UI
// selection, sends notifications, or commits git changes.
package boardops

import (
	"fmt"
	"os"
	"path/filepath"

	"kbrd/board"
	"kbrd/colstore"
	"kbrd/events"
	"kbrd/frontmatter"
	kbrdfs "kbrd/fs"
	"kbrd/template"
)

// ColumnRef identifies a filesystem column. Path is preferred when present;
// Name is the stable display/folder name fallback used by scripts.
type ColumnRef struct {
	Name string
	Path string
}

// ItemRef identifies a filesystem item. Path is preferred when present; Name
// and Column are the fallback identity used by script-facing APIs.
type ItemRef struct {
	Column ColumnRef
	Name   string
	Path   string
}

// BoardContext carries the board-level data needed to render template variable
// contexts consistently across adapters.
type BoardContext struct {
	Root string
	Name string
}

// MutationResult describes a successful item mutation. Callers attach their
// own side effects (events, selection, notifications, commits) using this data.
type MutationResult struct {
	Column  ColumnRef
	Item    ItemRef
	Path    string
	Changed bool
}

// ShellPolicy rewrites template shell markers for an adapter.
type ShellPolicy func(body string) string

// ResolveColumn resolves a column name under boardRoot. Empty names use the
// board package's default first-column behavior.
func ResolveColumn(boardRoot, name string, autoCreate bool) (ColumnRef, error) {
	path, err := board.ResolveColumn(boardRoot, name, autoCreate)
	if err != nil {
		return ColumnRef{}, err
	}
	return ColumnRef{Name: filepath.Base(path), Path: path}, nil
}

// CreateItem creates an item in col.
func CreateItem(col ColumnRef, name, content string) (MutationResult, error) {
	path, err := board.CreateItem(col.Path, name, content)
	if err != nil {
		return MutationResult{}, err
	}
	clean := itemNameFromPath(path)
	return MutationResult{
		Column: col,
		Item:   ItemRef{Column: col, Name: clean, Path: path},
		Path:   path,
	}, nil
}

// MoveItem moves item from src to dst.
func MoveItem(src, dst ColumnRef, itemName string) (MutationResult, error) {
	if err := board.MoveItem(src.Path, dst.Path, itemName); err != nil {
		return MutationResult{}, err
	}
	path, err := board.ItemPath(dst.Path, itemName)
	if err != nil {
		return MutationResult{}, err
	}
	return MutationResult{
		Column: dst,
		Item:   ItemRef{Column: dst, Name: itemName, Path: path},
		Path:   path,
	}, nil
}

// RenameItem renames itemName within col.
func RenameItem(col ColumnRef, itemName, newName string) (MutationResult, error) {
	if err := board.RenameItem(col.Path, itemName, newName); err != nil {
		return MutationResult{}, err
	}
	path, err := board.ItemPath(col.Path, newName)
	if err != nil {
		return MutationResult{}, err
	}
	return MutationResult{
		Column: col,
		Item:   ItemRef{Column: col, Name: newName, Path: path},
		Path:   path,
	}, nil
}

// DeleteItem removes itemName from col.
func DeleteItem(col ColumnRef, itemName string) (MutationResult, error) {
	path, _ := board.ItemPath(col.Path, itemName)
	if err := board.DeleteItem(col.Path, itemName); err != nil {
		return MutationResult{}, err
	}
	return MutationResult{
		Column: col,
		Item:   ItemRef{Column: col, Name: itemName, Path: path},
		Path:   path,
	}, nil
}

// SetFrontmatter rewrites a single top-level frontmatter key on itemName.
func SetFrontmatter(col ColumnRef, itemName, key, value string) (MutationResult, error) {
	return rewriteFrontmatter(col, itemName, func(raw string) string {
		return frontmatter.Set(raw, key, value)
	})
}

// DeleteFrontmatter removes a single top-level frontmatter key from itemName.
func DeleteFrontmatter(col ColumnRef, itemName, key string) (MutationResult, error) {
	return rewriteFrontmatter(col, itemName, func(raw string) string {
		return frontmatter.Delete(raw, key)
	})
}

// ApplyFrontmatterPatch applies a formatting-preserving top-level patch to an
// item. Values must already be encoded as inline YAML scalars or flow
// collections; callers own any runtime variable expansion.
func ApplyFrontmatterPatch(col ColumnRef, itemName string, patch frontmatter.Patch) (MutationResult, error) {
	path, err := board.ItemPath(col.Path, itemName)
	if err != nil {
		return MutationResult{}, err
	}
	raw, err := os.ReadFile(path)
	if err != nil {
		return MutationResult{}, err
	}
	updated, err := frontmatter.Apply(string(raw), patch)
	if err != nil {
		return MutationResult{}, err
	}
	if updated != string(raw) {
		if err := board.ReplaceFileContent(path, updated); err != nil {
			return MutationResult{}, err
		}
	}
	return MutationResult{
		Column:  col,
		Item:    ItemRef{Column: col, Name: itemName, Path: path},
		Path:    path,
		Changed: updated != string(raw),
	}, nil
}

// SetPinned sets or removes the "pinned" frontmatter key for itemName.
func SetPinned(col ColumnRef, itemName string, pinned bool) (MutationResult, error) {
	return rewriteFrontmatter(col, itemName, func(raw string) string {
		if pinned {
			return frontmatter.Set(raw, "pinned", "true")
		}
		return frontmatter.Delete(raw, "pinned")
	})
}

func rewriteFrontmatter(col ColumnRef, itemName string, rewrite func(string) string) (MutationResult, error) {
	path, err := board.ItemPath(col.Path, itemName)
	if err != nil {
		return MutationResult{}, err
	}
	raw, err := os.ReadFile(path)
	if err != nil {
		return MutationResult{}, err
	}
	updated := rewrite(string(raw))
	if updated != string(raw) {
		if err := board.ReplaceFileContent(path, updated); err != nil {
			return MutationResult{}, err
		}
	}
	return MutationResult{
		Column: col,
		Item:   ItemRef{Column: col, Name: itemName, Path: path},
		Path:   path,
	}, nil
}

// ListTemplates lists templates available for col.
func ListTemplates(ctx BoardContext, col ColumnRef) ([]events.TemplateInfo, error) {
	tmpls, _, err := template.List(ctx.Root, col.Path)
	if err != nil {
		return nil, err
	}
	infos := make([]events.TemplateInfo, 0, len(tmpls))
	for _, t := range tmpls {
		infos = append(infos, events.TemplateInfo{Name: t.Name, Scope: t.Scope})
	}
	return infos, nil
}

// CreateItemFromTemplate renders tmplName for col and creates the resulting
// card. ShellPolicy may rewrite generated body before creation.
func CreateItemFromTemplate(ctx BoardContext, col ColumnRef, tmplName string, values map[string]any, shellPolicy ShellPolicy) (MutationResult, error) {
	tmpls, _, err := template.List(ctx.Root, col.Path)
	if err != nil {
		return MutationResult{}, err
	}
	var found *template.Template
	names := make([]string, 0, len(tmpls))
	for i := range tmpls {
		names = append(names, tmpls[i].Name)
		if tmpls[i].Name == tmplName {
			found = &tmpls[i]
		}
	}
	if found == nil {
		return MutationResult{}, fmt.Errorf("template %q not found; available: %v", tmplName, names)
	}
	name, body, err := template.Instantiate(*found, board.VarContext{
		BoardPath:  ctx.Root,
		BoardName:  ctx.Name,
		ColumnPath: col.Path,
		ColumnName: col.Name,
	}, values)
	if err != nil {
		return MutationResult{}, err
	}
	if shellPolicy != nil {
		body = shellPolicy(body)
	}
	return CreateItem(col, name, body)
}

// CreateColumn creates a column directory and a .gitkeep so empty columns can
// be committed by headless adapters.
func CreateColumn(boardRoot, name string) (ColumnRef, error) {
	clean, err := board.SanitizeFolder(name)
	if err != nil {
		return ColumnRef{}, err
	}
	dir := filepath.Join(boardRoot, clean)
	if _, err := os.Stat(dir); err == nil {
		return ColumnRef{}, fmt.Errorf("column %q already exists", name)
	}
	if err := os.Mkdir(dir, 0o755); err != nil {
		return ColumnRef{}, err
	}
	_ = kbrdfs.WriteNewFileNoClobberDurable(filepath.Join(dir, ".gitkeep"), nil, 0o644)
	return ColumnRef{Name: clean, Path: dir}, nil
}

func ColumnConfigGet(col ColumnRef, key string) (any, bool, error) {
	s, err := colstore.Read(col.Path)
	if err != nil {
		return nil, false, err
	}
	v, ok := s.Get(key)
	return v, ok, nil
}

func ColumnConfigSet(col ColumnRef, key string, value any) error {
	return colstore.Update(col.Path, func(s *colstore.Store) error {
		s.Set(key, value)
		return nil
	})
}

func ColumnConfigAll(col ColumnRef) (map[string]any, error) {
	s, err := colstore.Read(col.Path)
	if err != nil {
		return nil, err
	}
	return s.All(), nil
}

func ColumnConfigDelete(col ColumnRef, key string) error {
	return colstore.Update(col.Path, func(s *colstore.Store) error {
		s.Delete(key)
		return nil
	})
}

func itemNameFromPath(path string) string {
	return filepath.Base(path[:len(path)-len(filepath.Ext(path))])
}
