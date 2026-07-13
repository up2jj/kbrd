package model

import (
	"errors"
	"fmt"
	"os"
	"path/filepath"

	tea "charm.land/bubbletea/v2"
)

func (b *Board) handleTimelineMessage(msg tea.Msg) (tea.Model, tea.Cmd, bool) {
	switch msg := msg.(type) {
	case timelineLoadedMsg:
		if !b.timeline.Active() || msg.seq != b.timeline.seq {
			return b, nil, true
		}
		if msg.err != nil {
			b.timeline.Close()
			return b, b.notifier.ErrorCause("load card history", msg.err), true
		}
		b.timeline.loaded(msg)
		return b, nil, true
	case timelineDocumentMsg:
		if !b.timeline.Active() || msg.seq != b.timeline.seq {
			return b, nil, true
		}
		if msg.err != nil {
			return b, b.notifier.ErrorCause("open history revision", msg.err), true
		}
		b.timeline.showDocument(msg)
		return b, nil, true
	case timelineRestoreMsg:
		if !b.timeline.Active() || msg.seq != b.timeline.seq {
			return b, nil, true
		}
		if msg.err != nil {
			return b, b.notifier.ErrorCause("restore history revision", msg.err), true
		}
		path, err := writeRestoredCopy(restoredCopyPath(b.timeline.cardPath, msg.event), msg.contents)
		if err != nil {
			return b, b.notifier.ErrorCause("restore history revision", err), true
		}
		b.timeline.Close()
		if err := b.loadColumns(); err != nil {
			return b, b.notifier.ErrorCause("reload board", err), true
		}
		b.applyColumnTransforms()
		return b, b.notifier.Success("restored as copy: " + filepath.Base(path)), true
	}
	return b, nil, false
}

func writeRestoredCopy(preferred string, contents []byte) (string, error) {
	ext := filepath.Ext(preferred)
	base := preferred[:len(preferred)-len(ext)]
	for i := 1; ; i++ {
		path := preferred
		if i > 1 {
			path = fmt.Sprintf("%s %d%s", base, i, ext)
		}
		f, err := os.OpenFile(path, os.O_WRONLY|os.O_CREATE|os.O_EXCL, 0o644)
		if errors.Is(err, os.ErrExist) {
			continue
		}
		if err != nil {
			return "", err
		}
		if _, err := f.Write(contents); err != nil {
			_ = f.Close()
			_ = os.Remove(path)
			return "", err
		}
		if err := f.Close(); err != nil {
			return "", err
		}
		return path, nil
	}
}
