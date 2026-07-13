package model

import (
	"charm.land/bubbles/v2/key"
	tea "charm.land/bubbletea/v2"
)

type itemActionID string

const (
	actionPeek            itemActionID = "peek"
	actionEdit            itemActionID = "edit"
	actionAppend          itemActionID = "append"
	actionPrepend         itemActionID = "prepend"
	actionJournal         itemActionID = "journal"
	actionCopy            itemActionID = "copy"
	actionPaste           itemActionID = "paste"
	actionOpenExternal    itemActionID = "open_external"
	actionPin             itemActionID = "pin"
	actionMoveMenu        itemActionID = "move_menu"
	actionMoveNext        itemActionID = "move_next"
	actionRenameItem      itemActionID = "rename_item"
	actionDelete          itemActionID = "delete"
	actionCustomCommands  itemActionID = "custom_commands"
	actionEditFrontmatter itemActionID = "edit_frontmatter"
	actionApplyPreset     itemActionID = "apply_frontmatter_preset"
	actionTimeline        itemActionID = "timeline"
)

type itemActionSource string

const (
	actionSourceKey   itemActionSource = "key"
	actionSourceHelp  itemActionSource = "help"
	actionSourceMouse itemActionSource = "mouse"
	actionSourcePeek  itemActionSource = "peek"
)

type itemActionCardinality int

const (
	actionSingle itemActionCardinality = iota
	actionMultiEach
	actionMultiBatch
)

type itemActionTarget struct {
	Ref  itemRefStable
	Item Item
}

type itemActionContext struct {
	Board   *Board
	ColIdx  int
	Column  *Column
	Item    *Item
	Targets []itemActionTarget
	Source  itemActionSource
}

type itemActionSpec struct {
	ID          itemActionID
	Binding     key.Binding
	Label       string
	Description string
	Cardinality itemActionCardinality
	NeedsItem   bool
	Run         func(itemActionContext) tea.Cmd
}

type boardItemActions struct {
	board *Board
}

func (b *Board) itemActions() boardItemActions {
	return boardItemActions{board: b}
}

func (a boardItemActions) Invoke(id itemActionID, source itemActionSource) (tea.Cmd, bool) {
	spec, ok := itemActionSpecByID(id)
	if !ok {
		return nil, false
	}
	ctx, ok := a.contextFor(spec, source)
	if !ok {
		return nil, false
	}
	return spec.Run(ctx), true
}

func (a boardItemActions) InvokeKey(msg tea.KeyPressMsg, source itemActionSource) (tea.Cmd, bool) {
	for _, spec := range itemActionSpecs() {
		if key.Matches(msg, spec.Binding) {
			return a.Invoke(spec.ID, source)
		}
	}
	return nil, false
}

func (a boardItemActions) InvokeCustomCommand(id string, source itemActionSource) (tea.Cmd, bool) {
	spec, _ := itemActionSpecByID(actionCustomCommands)
	ctx, ok := a.contextFor(spec, source)
	if !ok {
		return nil, false
	}
	b := a.board
	cmd, ok := commandByID(b.commands, id)
	if !ok {
		return nil, false
	}
	if cmd.NeedsItem() && len(ctx.Targets) > 0 && ctx.Column.MarkedCount() > 0 {
		runs := customCommandRunsForTargets(b, ctx, ctx.Targets)
		if len(runs) > 0 {
			return func() tea.Msg { return runCustomCommandBatchMsg{Cmd: cmd, Runs: runs} }, true
		}
	}
	vars, vctx, ok := customCommandContextForItem(b, ctx.Column, ctx.Item)
	if !ok {
		return nil, false
	}
	return func() tea.Msg { return runCustomCommandMsg{Cmd: cmd, Vars: vars, VCtx: vctx} }, true
}

func (a boardItemActions) contextFor(spec itemActionSpec, source itemActionSource) (itemActionContext, bool) {
	b := a.board
	if len(b.columns) == 0 || b.selectedCol < 0 || b.selectedCol >= len(b.columns) {
		return itemActionContext{}, false
	}
	col := b.columns[b.selectedCol]
	item := col.SelectedItem()
	if item != nil && item.Separator {
		item = nil
	}
	targets := a.targetsFor(col, spec.Cardinality)
	if spec.NeedsItem && item == nil && len(targets) == 0 {
		return itemActionContext{}, false
	}
	return itemActionContext{
		Board:   b,
		ColIdx:  b.selectedCol,
		Column:  col,
		Item:    item,
		Targets: targets,
		Source:  source,
	}, true
}

func (a boardItemActions) targetsFor(col *Column, cardinality itemActionCardinality) []itemActionTarget {
	if cardinality != actionSingle {
		if marked := col.MarkedItems(); len(marked) > 0 {
			return targetsForItems(col, marked)
		}
	}
	if item := col.SelectedItem(); item != nil && !item.Separator {
		return targetsForItems(col, []Item{*item})
	}
	return nil
}

func targetsForItems(col *Column, items []Item) []itemActionTarget {
	out := make([]itemActionTarget, 0, len(items))
	for _, item := range items {
		if item.Separator {
			continue
		}
		it := item
		out = append(out, itemActionTarget{Ref: refForItem(col, &it), Item: item})
	}
	return out
}

func itemActionSpecByID(id itemActionID) (itemActionSpec, bool) {
	for _, spec := range itemActionSpecs() {
		if spec.ID == id {
			return spec, true
		}
	}
	return itemActionSpec{}, false
}

func itemActionSpecs() []itemActionSpec {
	return []itemActionSpec{
		{
			ID:          actionPeek,
			Binding:     Keys.Peek,
			Label:       "peek",
			Description: "Preview the selected card's rendered markdown in a reader.",
			Cardinality: actionSingle,
			NeedsItem:   true,
			Run: func(ctx itemActionContext) tea.Cmd {
				return ctx.Board.itemActions().peek(ctx.Column, ctx.Item)
			},
		},
		{
			ID:          actionEdit,
			Binding:     Keys.Edit,
			Label:       "edit",
			Description: "Open the selected card in the editor to change its body.",
			Cardinality: actionSingle,
			NeedsItem:   true,
			Run: func(ctx itemActionContext) tea.Cmd {
				return ctx.Board.itemActions().edit(ctx.ColIdx, ctx.Column, ctx.Item)
			},
		},
		{
			ID:          actionAppend,
			Binding:     Keys.Append,
			Label:       "append",
			Description: "Append text to the end of the selected card.",
			Cardinality: actionSingle,
			NeedsItem:   true,
			Run: func(ctx itemActionContext) tea.Cmd {
				return ctx.Board.itemActions().append(ctx.ColIdx, ctx.Column, ctx.Item)
			},
		},
		{
			ID:          actionPrepend,
			Binding:     Keys.Prepend,
			Label:       "prepend",
			Description: "Prepend text to the start of the selected card.",
			Cardinality: actionSingle,
			NeedsItem:   true,
			Run: func(ctx itemActionContext) tea.Cmd {
				return ctx.Board.itemActions().prepend(ctx.ColIdx, ctx.Column, ctx.Item)
			},
		},
		{
			ID:          actionJournal,
			Binding:     Keys.Journal,
			Label:       "journal entry",
			Description: "Add a timestamped journal entry to the selected card.",
			Cardinality: actionSingle,
			NeedsItem:   true,
			Run: func(ctx itemActionContext) tea.Cmd {
				return ctx.Board.itemActions().journal(ctx.ColIdx, ctx.Column, ctx.Item)
			},
		},
		{
			ID:          actionCopy,
			Binding:     Keys.Copy,
			Label:       "copy",
			Description: "Copy the selected card to the clipboard.",
			Cardinality: actionMultiBatch,
			NeedsItem:   true,
			Run: func(ctx itemActionContext) tea.Cmd {
				if len(ctx.Targets) > 1 || ctx.Column.MarkedCount() > 0 {
					return ctx.Board.itemActions().copyTargets(ctx.Column, ctx.Targets)
				}
				return ctx.Board.itemActions().copy(ctx.Column, ctx.Item)
			},
		},
		{
			ID:          actionPaste,
			Binding:     Keys.Paste,
			Label:       "paste",
			Description: "Paste clipboard content into a card or as a new file.",
			Cardinality: actionSingle,
			NeedsItem:   false,
			Run: func(ctx itemActionContext) tea.Cmd {
				return ctx.Board.itemActions().paste(ctx.ColIdx, ctx.Column, ctx.Item)
			},
		},
		{
			ID:          actionOpenExternal,
			Binding:     Keys.OpenExternal,
			Label:       "open in $EDITOR",
			Description: "Open the selected card in your $EDITOR.",
			Cardinality: actionSingle,
			NeedsItem:   true,
			Run: func(ctx itemActionContext) tea.Cmd {
				return ctx.Board.itemActions().openExternal(ctx.Column, ctx.Item)
			},
		},
		{
			ID:          actionPin,
			Binding:     Keys.Pin,
			Label:       "pin / unpin",
			Description: "Pin or unpin the selected card to the top of its column.",
			Cardinality: actionSingle,
			NeedsItem:   true,
			Run: func(ctx itemActionContext) tea.Cmd {
				return ctx.Board.itemActions().togglePin(ctx.Column, ctx.Item)
			},
		},
		{
			ID:          actionMoveMenu,
			Binding:     Keys.MoveMenu,
			Label:       "move to…",
			Description: "Choose a destination column for the selected or marked cards.",
			Cardinality: actionMultiBatch,
			NeedsItem:   true,
			Run: func(ctx itemActionContext) tea.Cmd {
				return ctx.Board.moveMenuActions().open(ctx)
			},
		},
		{
			ID:          actionMoveNext,
			Binding:     Keys.MoveNext,
			Label:       "move to next column",
			Description: "Move the selected card to the next column.",
			Cardinality: actionMultiBatch,
			NeedsItem:   true,
			Run: func(ctx itemActionContext) tea.Cmd {
				if len(ctx.Targets) > 1 || ctx.Column.MarkedCount() > 0 {
					return ctx.Board.itemActions().moveTargets(ctx.ColIdx, ctx.Column, ctx.Targets, (ctx.ColIdx+1)%len(ctx.Board.columns), true)
				}
				return ctx.Board.itemActions().moveNext(ctx.ColIdx, ctx.Column, ctx.Item, true)
			},
		},
		{
			ID:          actionRenameItem,
			Binding:     Keys.RenameItem,
			Label:       "rename item",
			Description: "Rename the selected card's file.",
			Cardinality: actionSingle,
			NeedsItem:   true,
			Run: func(ctx itemActionContext) tea.Cmd {
				return ctx.Board.editor.OpenRenameItem(ctx.ColIdx, ctx.Column.Path, ctx.Item.FullPath, ctx.Item.Name)
			},
		},
		{
			ID:          actionDelete,
			Binding:     Keys.Delete,
			Label:       "delete",
			Description: "Delete the selected card after confirmation.",
			Cardinality: actionMultiBatch,
			NeedsItem:   true,
			Run: func(ctx itemActionContext) tea.Cmd {
				if len(ctx.Targets) > 1 || ctx.Column.MarkedCount() > 0 {
					return ctx.Board.itemActions().confirmDeleteTargets(ctx.ColIdx, ctx.Column, ctx.Targets)
				}
				return ctx.Board.itemActions().confirmDelete(ctx.ColIdx, ctx.Column, ctx.Item)
			},
		},
		{
			ID:          actionCustomCommands,
			Binding:     Keys.CustomCommands,
			Label:       "custom commands",
			Description: "Run a custom command against the selected card or focused column.",
			Cardinality: actionMultiEach,
			NeedsItem:   false,
			Run: func(ctx itemActionContext) tea.Cmd {
				_, cmd, _ := ctx.Board.openCustomCommands(ctx)
				return cmd
			},
		},
		{
			ID:          actionTimeline,
			Binding:     Keys.Timeline,
			Label:       "timeline",
			Description: "Browse this card's semantic history, diffs, and snapshots.",
			Cardinality: actionSingle,
			NeedsItem:   true,
			Run: func(ctx itemActionContext) tea.Cmd {
				if ctx.Item.Virtual {
					return ctx.Board.notifier.Error("virtual cards have no Git history")
				}
				root := ctx.Board.git.RepoRoot()
				if root == "" {
					return ctx.Board.notifier.Error("no Git repository")
				}
				return ctx.Board.timeline.Open(root, ctx.Item.FullPath, ctx.Item.Title)
			},
		},
		{
			ID:          actionEditFrontmatter,
			Binding:     Keys.EditFrontmatter,
			Label:       "edit frontmatter",
			Description: "Edit the selected card's YAML frontmatter.",
			Cardinality: actionSingle,
			NeedsItem:   true,
			Run: func(ctx itemActionContext) tea.Cmd {
				return ctx.Board.frontmatterActions().openEditor(ctx.ColIdx, ctx.Column, ctx.Item)
			},
		},
		{
			ID:          actionApplyPreset,
			Binding:     Keys.ApplyPreset,
			Label:       "apply frontmatter preset",
			Description: "Apply a board-local frontmatter preset to the selected or marked cards.",
			Cardinality: actionMultiBatch,
			NeedsItem:   true,
			Run: func(ctx itemActionContext) tea.Cmd {
				return ctx.Board.frontmatterPresetActions().open(ctx)
			},
		},
	}
}

func itemActionHelpEntries() []HelpEntry {
	specs := itemActionSpecs()
	out := make([]HelpEntry, 0, len(specs))
	for _, spec := range specs {
		e := helpEntry(spec.Binding, spec.Description)
		e.Label = spec.Label
		e.NeedsItem = spec.NeedsItem
		out = append(out, e)
	}
	return out
}
