package model

import (
	"fmt"

	"kbrd/board"
	"kbrd/events"
)

// allFilesystemColumns returns the authoritative filesystem-backed columns.
// The fallback keeps small unit-test Boards that only populate columns useful.
func (b *Board) allFilesystemColumns() []*Column {
	if b.filesystemCols != nil {
		return b.filesystemCols
	}
	cols := make([]*Column, 0, len(b.columns))
	for _, col := range b.columns {
		if !col.Virtual {
			cols = append(cols, col)
		}
	}
	return cols
}

func (b *Board) setFilesystemColumns(columns []*Column) {
	b.filesystemCols = columns
	b.rebuildVisibleColumns()
}

func (b *Board) materializeFilesystemColumns() {
	if b.filesystemCols == nil && len(b.columns) > 0 {
		b.filesystemCols = b.allFilesystemColumns()
	}
}

// rebuildVisibleColumns projects the authoritative filesystem columns through
// the script-owned hidden set, then appends virtual columns. Selection follows
// its column identity when possible and otherwise falls to the nearest column.
func (b *Board) rebuildVisibleColumns() {
	selectedName := ""
	oldIndex := b.selectedCol
	if oldIndex >= 0 && oldIndex < len(b.columns) {
		selectedName = b.columns[oldIndex].Name
	}
	b.materializeFilesystemColumns()

	visible := make([]*Column, 0, len(b.filesystemCols)+len(b.virtualCols))
	for _, col := range b.filesystemCols {
		if _, hidden := b.hiddenColumns[col.Name]; hidden {
			continue
		}
		if b.visibleHeight > 0 {
			col.SetHeight(b.visibleHeight)
		}
		col.palette = b.palette
		visible = append(visible, col)
	}
	if !b.virtualHidden {
		for _, col := range b.virtualCols {
			if b.visibleHeight > 0 {
				col.SetHeight(b.visibleHeight)
			}
			col.palette = b.palette
			visible = append(visible, col)
		}
	}
	b.columns = visible

	if selectedName != "" {
		for i, col := range b.columns {
			if col.Name == selectedName {
				b.selectedCol = i
				b.clampFirstVisibleCol()
				return
			}
		}
	}
	b.selectedCol = min(oldIndex, len(b.columns)-1)
	b.clampSelectedCol()
	b.clampFirstVisibleCol()
}

func (b *Board) clampFirstVisibleCol() {
	maxFirst := max(len(b.columns)-1, 0)
	if b.firstVisibleCol > maxFirst {
		b.firstVisibleCol = maxFirst
	}
	if b.firstVisibleCol < 0 {
		b.firstVisibleCol = 0
	}
}

func (b *Board) filesystemColumnKnown(name string) bool {
	if b.filesystemColumn(name) != nil {
		return true
	}
	// Lua init files run before the initial column load. Resolve against disk so
	// top-level visibility calls can still be validated and recorded.
	names, err := board.Columns(b.cfg.Path)
	if err != nil {
		return false
	}
	for _, candidate := range names {
		if candidate == name {
			return true
		}
	}
	return false
}

func (b *Board) filesystemColumn(name string) *Column {
	for _, col := range b.allFilesystemColumns() {
		if col.Name == name {
			return col
		}
	}
	return nil
}

func (b *Board) visibleFilesystemCount() int {
	count := 0
	for _, col := range b.allFilesystemColumns() {
		if _, hidden := b.hiddenColumns[col.Name]; !hidden {
			count++
		}
	}
	if b.filesystemCols != nil {
		return count
	}
	names, err := board.Columns(b.cfg.Path)
	if err != nil {
		return count
	}
	count = 0
	for _, name := range names {
		if _, hidden := b.hiddenColumns[name]; !hidden {
			count++
		}
	}
	return count
}

func (b *Board) hideColumn(name string) error {
	if !b.filesystemColumnKnown(name) {
		return fmt.Errorf("filesystem column %q not found", name)
	}
	if _, hidden := b.hiddenColumns[name]; hidden {
		return nil
	}
	if b.visibleFilesystemCount() <= 1 && b.visibleVirtualCount() == 0 {
		return fmt.Errorf("cannot hide the final visible column")
	}
	if b.hiddenColumns == nil {
		b.hiddenColumns = make(map[string]struct{})
	}
	b.hiddenColumns[name] = struct{}{}
	b.rebuildVisibleColumns()
	return nil
}

func (b *Board) showColumn(name string) error {
	if !b.filesystemColumnKnown(name) {
		return fmt.Errorf("filesystem column %q not found", name)
	}
	delete(b.hiddenColumns, name)
	b.rebuildVisibleColumns()
	return nil
}

func (b *Board) showAllColumns() {
	_ = b.showAllColumnsByKind(events.ColumnKindReal)
}

func (b *Board) visibleVirtualCount() int {
	if b.virtualHidden {
		return 0
	}
	return len(b.virtualCols)
}

func (b *Board) filesystemColumnNames() []string {
	if b.filesystemCols != nil {
		names := make([]string, len(b.filesystemCols))
		for i, col := range b.filesystemCols {
			names[i] = col.Name
		}
		return names
	}
	names, _ := board.Columns(b.cfg.Path)
	return names
}

func (b *Board) hideAllColumns(kind events.ColumnKind) error {
	switch kind {
	case events.ColumnKindReal:
		if b.visibleFilesystemCount() == 0 {
			return nil
		}
		if b.visibleVirtualCount() == 0 {
			return fmt.Errorf("cannot hide all real columns: no visible virtual column remains")
		}
		if b.hiddenColumns == nil {
			b.hiddenColumns = make(map[string]struct{})
		}
		for _, name := range b.filesystemColumnNames() {
			b.hiddenColumns[name] = struct{}{}
		}
	case events.ColumnKindVirtual:
		if b.virtualHidden {
			return nil
		}
		if b.visibleFilesystemCount() == 0 && len(b.virtualCols) > 0 {
			return fmt.Errorf("cannot hide all virtual columns: no visible real column remains")
		}
		b.virtualHidden = true
	default:
		return fmt.Errorf("unknown column kind %q", kind)
	}
	b.rebuildVisibleColumns()
	return nil
}

func (b *Board) showAllColumnsByKind(kind events.ColumnKind) error {
	switch kind {
	case events.ColumnKindReal:
		b.hiddenColumns = nil
	case events.ColumnKindVirtual:
		b.virtualHidden = false
	default:
		return fmt.Errorf("unknown column kind %q", kind)
	}
	b.rebuildVisibleColumns()
	return nil
}

func (b *Board) columnHidden(name string) bool {
	_, hidden := b.hiddenColumns[name]
	return hidden
}
