package model

import (
	tea "charm.land/bubbletea/v2"

	kbrdgit "kbrd/git"
)

type peekLineMarkersLoadedMsg struct {
	Seq      int
	ItemPath string
	Markers  []PeekLineMarker
}

func (b *Board) openPeekForItem(item *Item, content string) tea.Cmd {
	b.peekSeq++
	seq := b.peekSeq
	title := ""
	itemPath := ""
	if item != nil {
		title = item.Title
		if !item.Virtual {
			itemPath = item.FullPath
		}
	}
	b.peekItemPath = itemPath
	cmd := b.peek.OpenWithLineMarkerGutter(title, content, b.termWidth, b.shouldReservePeekMarkerGutter(itemPath))
	return batchCmd(cmd, b.loadPeekLineMarkersCmd(seq, itemPath))
}

func (b *Board) shouldReservePeekMarkerGutter(itemPath string) bool {
	if itemPath == "" || b.git.RepoRoot() == "" {
		return false
	}
	_, ok := b.git.StatFor(itemPath)
	return ok
}

func (b *Board) loadPeekLineMarkersCmd(seq int, itemPath string) tea.Cmd {
	if itemPath == "" || b.git.RepoRoot() == "" {
		return nil
	}
	gitController := b.git
	return func() tea.Msg {
		return peekLineMarkersLoadedMsg{
			Seq:      seq,
			ItemPath: itemPath,
			Markers:  lineChangesToPeekMarkers(gitController.LineChanges(itemPath)),
		}
	}
}

func (b *Board) handlePeekLineMarkersLoaded(msg peekLineMarkersLoadedMsg) (tea.Model, tea.Cmd) {
	if !b.peek.Active() || msg.Seq != b.peekSeq || msg.ItemPath != b.peekItemPath {
		return b, nil
	}
	b.peek.SetLineMarkers(msg.Markers, b.termWidth)
	return b, nil
}

func lineChangesToPeekMarkers(changes []kbrdgit.LineChange) []PeekLineMarker {
	if len(changes) == 0 {
		return nil
	}
	markers := make([]PeekLineMarker, 0, len(changes))
	for _, change := range changes {
		if change.Line <= 0 {
			continue
		}
		markers = append(markers, PeekLineMarker{
			Line: change.Line,
			Kind: lineChangeKindToPeekMarker(change.Kind),
		})
	}
	return markers
}

func lineChangeKindToPeekMarker(kind kbrdgit.LineChangeKind) PeekLineMarkerKind {
	switch kind {
	case kbrdgit.LineAdded:
		return PeekLineAdded
	case kbrdgit.LineModified:
		return PeekLineModified
	case kbrdgit.LineDeleted:
		return PeekLineDeleted
	default:
		return PeekLineModified
	}
}
