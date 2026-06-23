package model

import "kbrd/template"

// These constructors are for work that can cross an async/editor/dialog
// boundary. The stable prefix is intentional: callers should carry item/column
// refs and treat integer indexes as compatibility metadata only.
func newStableOpenLineCommandsMsg(target itemRefStable, colIdx int, fileName, line string, row int) openLineCommandsMsg {
	return openLineCommandsMsg{Target: target, ColIndex: colIdx, FileName: fileName, Line: line, Row: row}
}

func newStableEditorSaveMsg(target itemRefStable, colIdx int, fileName, content string) editorSaveMsg {
	return editorSaveMsg{Target: target, ColIndex: colIdx, FileName: fileName, Content: content}
}

func newStableManagedFileSaveMsg(path, label, content string) managedFileSaveMsg {
	return managedFileSaveMsg{Path: path, Label: label, Content: content}
}

func newStableEditorAppendMsg(target itemRefStable, colIdx int, fileName, text string) editorAppendMsg {
	return editorAppendMsg{Target: target, ColIndex: colIdx, FileName: fileName, Text: text}
}

func newStableEditorPrependMsg(target itemRefStable, colIdx int, fileName, text string) editorPrependMsg {
	return editorPrependMsg{Target: target, ColIndex: colIdx, FileName: fileName, Text: text}
}

func newStableEditorJournalMsg(target itemRefStable, colIdx int, fileName, text string) editorJournalMsg {
	return editorJournalMsg{Target: target, ColIndex: colIdx, FileName: fileName, Text: text}
}

func newStableEditorNewMsg(column columnRef, colIdx int, fileName string) editorNewMsg {
	return editorNewMsg{Column: column, ColIndex: colIdx, FileName: fileName}
}

func newStableDeleteConfirmMsg(target itemRefStable, colIdx int, fileName string) deleteConfirmMsg {
	return deleteConfirmMsg{Target: target, ColIndex: colIdx, FileName: fileName}
}

func newStableTemplateRemoveConfirmMsg(column columnRef, tmpl template.Template) templateRemoveConfirmMsg {
	return templateRemoveConfirmMsg{Column: column, Path: tmpl.Path, Name: tmpl.Name, Scope: tmpl.Scope}
}

func newStableRenameItemRequestMsg(target itemRefStable, colIdx int, oldName, newName string) renameItemRequestMsg {
	return renameItemRequestMsg{Target: target, ColIndex: colIdx, OldName: oldName, NewName: newName}
}

func newStableRenameColumnRequestMsg(column columnRef, colIdx int, oldName, newName string) renameColumnRequestMsg {
	return renameColumnRequestMsg{Column: column, ColIndex: colIdx, OldName: oldName, NewName: newName}
}

func newStableRenameItemConfirmMsg(target itemRefStable, colIdx int, oldName, newName string) renameItemConfirmMsg {
	return renameItemConfirmMsg{Target: target, ColIndex: colIdx, OldName: oldName, NewName: newName}
}

func newStableRenameColumnConfirmMsg(column columnRef, colIdx int, oldName, newName string) renameColumnConfirmMsg {
	return renameColumnConfirmMsg{Column: column, ColIndex: colIdx, OldName: oldName, NewName: newName}
}

func newStableTemplateSubmitMsg(column columnRef, colIdx int, tmpl template.Template, values map[string]any) templateSubmitMsg {
	return templateSubmitMsg{Column: column, ColIndex: colIdx, Template: tmpl, Values: values}
}

func newStableTemplateAuthorSubmitMsg(column columnRef, colIdx int, values templateAuthorValues, reopenMenu bool) templateAuthorSubmitMsg {
	return templateAuthorSubmitMsg{Column: column, ColIndex: colIdx, Values: values, ReopenMenu: reopenMenu}
}

func newStableFrontmatterSubmitMsg(target itemRefStable, colIdx int, fileName, key, value string, delete bool) frontmatterSubmitMsg {
	return frontmatterSubmitMsg{Target: target, ColIndex: colIdx, FileName: fileName, Key: key, Value: value, Delete: delete}
}
