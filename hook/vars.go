// Package hook contains declarative-hook behavior shared by the TUI and
// headless commands. It deliberately has no Bubble Tea dependency.
package hook

import (
	"path/filepath"
	"strconv"

	"kbrd/board"
	"kbrd/config"
	"kbrd/events"
)

// eventVars maps a hookable event to its canonical name and command-template
// variables. The variable names match board.VarContext, custom commands, and
// the Lua API. An empty name means ev has no declarative-hook representation.
func eventVars(cfg config.Config, ev events.Event) (string, map[string]string) {
	itemVars := func(column, name string) map[string]string {
		colPath := filepath.Join(cfg.Path, column)
		return board.VarContext{
			BoardPath:  cfg.Path,
			BoardName:  cfg.BoardName,
			ColumnPath: colPath,
			ColumnName: column,
			FilePath:   filepath.Join(colPath, name+".md"),
			FileName:   name,
		}.Vars()
	}
	boardVars := func() map[string]string {
		return board.VarContext{BoardPath: cfg.Path, BoardName: cfg.BoardName}.Vars()
	}

	switch e := ev.(type) {
	case events.ItemCreated:
		return events.NameItemCreated, itemVars(e.Item.Column, e.Item.Name)
	case events.ItemOpen:
		v := itemVars(e.Item.Column, e.Item.Name)
		v["kind"] = e.Kind
		return events.NameItemOpen, v
	case events.ItemSaved:
		v := itemVars(e.Item.Column, e.Item.Name)
		v["kind"] = e.Kind
		return events.NameItemSaved, v
	case events.ItemChanged:
		return events.NameItemChanged, itemVars(e.Item.Column, e.Item.Name)
	case events.ItemMoved:
		v := itemVars(e.To, e.Item.Name)
		v["fromColumn"] = e.From
		v["toColumn"] = e.To
		return events.NameItemMoved, v
	case events.ItemRenamed:
		v := itemVars(e.Item.Column, e.Item.Name)
		v["oldName"] = e.OldName
		return events.NameItemRenamed, v
	case events.ItemDeleted:
		return events.NameItemDeleted, itemVars(e.Column, e.Name)
	case events.ColumnCreated:
		v := boardVars()
		v["columnName"] = e.Name
		v["columnPath"] = filepath.Join(cfg.Path, e.Name)
		return events.NameColumnCreated, v
	case events.GitSyncDone:
		v := boardVars()
		v["ok"] = strconv.FormatBool(e.OK)
		v["stage"] = e.Stage
		v["error"] = e.Err
		return events.NameGitSyncDone, v
	case events.BoardLoad:
		return events.NameBoardLoad, boardVars()
	}
	return "", nil
}
