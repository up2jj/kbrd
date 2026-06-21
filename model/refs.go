package model

import "fmt"

// columnRef is a stable reference for delayed UI messages. Path wins when
// present because it survives column reordering and display-name collisions.
type columnRef struct {
	Name string
	Path string
	VID  string
}

// itemRefStable is a stable reference for delayed item actions. ItemPath wins
// when present; FileName is the fallback for older paths and newly-created
// items that do not exist yet.
type itemRefStable struct {
	Column   columnRef
	FileName string
	ItemPath string
}

func refForColumn(c *Column) columnRef {
	if c == nil {
		return columnRef{}
	}
	return columnRef{Name: c.Name, Path: c.Path, VID: c.VID}
}

func refForItem(c *Column, it *Item) itemRefStable {
	ref := itemRefStable{Column: refForColumn(c)}
	if it != nil {
		ref.FileName = it.Name
		ref.ItemPath = it.FullPath
	}
	return ref
}

func (r columnRef) hasStableIdentity() bool {
	return r.VID != "" || r.Path != "" || r.Name != ""
}

func (r itemRefStable) hasStableIdentity() bool {
	return r.Column.hasStableIdentity() && (r.ItemPath != "" || r.FileName != "")
}

func (b *Board) resolveColumnRef(ref columnRef, fallbackIdx int) (*Column, error) {
	if ref.VID != "" {
		for _, c := range b.columns {
			if c.Virtual && c.VID == ref.VID {
				return c, nil
			}
		}
	}
	if ref.Path != "" {
		for _, c := range b.columns {
			if samePath(c.Path, ref.Path) {
				return c, nil
			}
		}
	}
	if ref.Name != "" {
		for _, c := range b.columns {
			if c.Name == ref.Name {
				return c, nil
			}
		}
	}
	if fallbackIdx >= 0 && fallbackIdx < len(b.columns) {
		return b.columns[fallbackIdx], nil
	}
	return nil, fmt.Errorf("column no longer exists")
}

func (b *Board) resolveDelayedColumnRef(ref columnRef) (*Column, error) {
	if !ref.hasStableIdentity() {
		return nil, fmt.Errorf("column no longer exists")
	}
	return b.resolveColumnRef(ref, -1)
}

func (b *Board) indexOfColumn(col *Column) int {
	for i, c := range b.columns {
		if c == col {
			return i
		}
	}
	return -1
}

func (b *Board) resolveItemRef(ref itemRefStable, fallbackIdx int) (*Column, *Item, error) {
	col, err := b.resolveColumnRef(ref.Column, fallbackIdx)
	if err != nil {
		return nil, nil, err
	}
	if ref.ItemPath != "" {
		for i := range col.Items {
			if samePath(col.Items[i].FullPath, ref.ItemPath) {
				return col, &col.Items[i], nil
			}
		}
	}
	if ref.FileName != "" {
		for i := range col.Items {
			if col.Items[i].Name == ref.FileName {
				return col, &col.Items[i], nil
			}
		}
	}
	return nil, nil, fmt.Errorf("item no longer exists: %s", ref.FileName)
}

func (b *Board) resolveDelayedItemRef(ref itemRefStable) (*Column, *Item, error) {
	if !ref.hasStableIdentity() {
		return nil, nil, fmt.Errorf("item no longer exists: %s", ref.FileName)
	}
	return b.resolveItemRef(ref, -1)
}
