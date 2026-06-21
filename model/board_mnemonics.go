package model

type boardMnemonics struct {
	b *Board
}

func (m boardMnemonics) rebuild() {
	b := m.b
	type cell struct {
		ref itemRefStable
	}
	var cells []cell
	for _, col := range b.columns {
		for _, item := range col.VisibleItems() {
			if item.Separator {
				continue // inert grouping rows get no quick-jump tag
			}
			item := item
			cells = append(cells, cell{ref: refForItem(col, &item)})
		}
	}
	tags := GenerateMnemonics(len(cells))
	b.mnemonicByRef = make(map[itemRefStable]string, len(cells))
	b.refByMnemonic = make(map[string]itemRefStable, len(cells))
	max := 0
	for i, c := range cells {
		tag := tags[i]
		b.mnemonicByRef[c.ref] = tag
		b.refByMnemonic[tag] = c.ref
		if len(tag) > max {
			max = len(tag)
		}
	}
	b.mnemonicMaxLen = max
}

func (m boardMnemonics) lookup(colIdx int) func(name string) string {
	b := m.b
	return func(name string) string {
		if colIdx < 0 || colIdx >= len(b.columns) {
			return ""
		}
		col := b.columns[colIdx]
		if item := col.ItemByName(name); item != nil {
			return b.mnemonicByRef[refForItem(col, item)]
		}
		return ""
	}
}

func (b *Board) mnemonics() boardMnemonics {
	return boardMnemonics{b: b}
}

func (b *Board) rebuildMnemonics() {
	b.mnemonics().rebuild()
}

func (b *Board) mnemonicLookup(colIdx int) func(name string) string {
	return b.mnemonics().lookup(colIdx)
}
