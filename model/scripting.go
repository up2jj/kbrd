package model

import (
	"fmt"

	"kbrd/config"
	"kbrd/events"
	"kbrd/script"
)

// initScripting creates the Lua host (if enabled and init files exist) and
// subscribes it to the event bus. Idempotent: a second call closes the
// previous host first, which is what board-switching needs.
func (b *Board) initScripting() {
	if b.scripts != nil {
		b.scripts.Close()
		b.scripts = nil
	}
	b.bus = events.Bus{}

	if !b.cfg.Scripting.Enabled {
		return
	}
	logger := script.NewFileLogger()
	host, err := script.New(b.cfg.Scripting, boardScriptAPI{b: b}, logger, b.cfg.Path)
	if err != nil && host == nil {
		// Hard failure during init — surface but keep running.
		b.commandWarnings = append(b.commandWarnings, config.CommandLoadWarning{
			Source:  "init.lua",
			Message: err.Error(),
		})
		return
	}
	if host == nil {
		return
	}
	if err != nil {
		// Partial failure — some files loaded, others errored.
		b.commandWarnings = append(b.commandWarnings, config.CommandLoadWarning{
			Source:  "init.lua",
			Message: err.Error(),
		})
	}
	b.scripts = host
	b.bus.Subscribe(host)
}

// boardScriptAPI is the events.BoardAPI implementation handed to the Lua
// host. It must remain safe to call while h.mu is held inside the host —
// so it never calls back into the host itself.
type boardScriptAPI struct {
	b *Board
}

func (a boardScriptAPI) Notify(msg, level string) {
	sev := notifySuccess
	if level == "error" {
		sev = notifyError
	}
	a.b.notifier.fire(msg, sev)
}

func (a boardScriptAPI) MoveItem(item events.ItemRef, toColumn string) error {
	var src, dst *Column
	for _, c := range a.b.columns {
		if c.Name == item.Column {
			src = c
		}
		if c.Name == toColumn {
			dst = c
		}
	}
	if src == nil {
		return fmt.Errorf("source column %q not found", item.Column)
	}
	if dst == nil {
		return fmt.Errorf("destination column %q not found", toColumn)
	}
	if err := src.MoveItemTo(dst, item.Name); err != nil {
		return err
	}
	a.b.bus.Publish(events.ItemMoved{
		Item: item,
		From: item.Column,
		To:   toColumn,
	})
	return nil
}
