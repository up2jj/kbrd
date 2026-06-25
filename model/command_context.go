package model

import (
	"kbrd/board"
	"kbrd/config"
)

type boardCommandContext struct {
	board *Board
}

func (b *Board) commandContext() boardCommandContext {
	return boardCommandContext{board: b}
}

// vars renders the flat template vars for a command. item may be nil (a
// requiresItem: false command on an empty column), in which case the file fields
// are omitted and VarContext.Vars drops them so a template that references
// filePath fails cleanly.
func (c boardCommandContext) vars(colIdx int, item *Item) map[string]string {
	b := c.board
	col := b.columns[colIdx]
	vc := board.VarContext{
		BoardPath:  b.cfg.Path,
		BoardName:  b.cfg.BoardName,
		ColumnPath: col.Path,
		ColumnName: col.Name,
	}
	if item != nil {
		vc.FilePath = item.FullPath
		vc.FileName = item.Name
	}
	return vc.Vars()
}

// virtualVars builds the structured ctx for a virtual-column command. item may
// be nil (a requiresItem: false command on an empty column); the item-specific
// fields (title/fileName/path/data) are then omitted, leaving the board/column
// context.
func (c boardCommandContext) virtualVars(col *Column, item *Item) map[string]any {
	b := c.board
	m := map[string]any{
		"boardPath":  b.cfg.Path,
		"boardName":  b.cfg.BoardName,
		"columnName": col.Name,
		"vid":        col.VID,
	}
	if item != nil {
		m["title"] = item.Title
		m["fileName"] = item.Name
		if item.FullPath != "" {
			m["path"] = item.FullPath
			m["filePath"] = item.FullPath
		}
		if item.Data != nil {
			m["data"] = item.Data
		}
	}
	return m
}

// filesystemCtx builds the structured Lua ctx for a command dispatched on a
// filesystem item that carries frontmatter data: a strict superset of the flat
// string vars (so scripts reading ctx.fileDir etc. keep working) plus `title`,
// the shared `path`, and the nested `data` table that a string map can't hold.
// Items without frontmatter keep the plain string-vars flow.
func (c boardCommandContext) filesystemCtx(colIdx int, item *Item) map[string]any {
	ctx := map[string]any{}
	for k, v := range c.vars(colIdx, item) {
		ctx[k] = v
	}
	if item != nil {
		ctx["path"] = item.FullPath
		ctx["title"] = item.Title
		ctx["data"] = item.Data
	}
	return ctx
}

// commandsForColumn returns the menu command list for the focused column,
// applying scope: filesystem columns show files/all globals; virtual columns
// show their own column-scoped commands first, then virtual/all globals. When
// the column has no selected item, commands that require one are dropped so only
// requiresItem: false commands remain (possibly none).
func (c boardCommandContext) commandsForColumn(col *Column) []config.Command {
	b := c.board
	hasItem := col.HasSelectedItem()
	keep := func(cmd config.Command) bool { return hasItem || !cmd.NeedsItem() }
	if col.Virtual {
		out := make([]config.Command, 0, len(col.colCmds)+len(b.commands))
		for _, vc := range col.colCmds {
			requiresItem := vc.RequiresItem
			cmd := config.Command{
				Name:         vc.Name,
				ID:           vc.ID,
				Scope:        "virtual",
				RequiresItem: &requiresItem,
				Source:       config.SourceLua,
				LuaRef:       vc.Ref,
			}
			if keep(cmd) {
				out = append(out, cmd)
			}
		}
		for _, cmd := range b.commands {
			if cmd.ShowsOnVirtual() && keep(cmd) {
				out = append(out, cmd)
			}
		}
		return out
	}
	out := make([]config.Command, 0, len(b.commands))
	for _, cmd := range b.commands {
		if cmd.ShowsOnFiles() && keep(cmd) {
			out = append(out, cmd)
		}
	}
	return out
}
