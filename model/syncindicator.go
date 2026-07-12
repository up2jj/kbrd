package model

import (
	"strconv"

	"kbrd/git"
)

// syncCell maps the git sync status (plus board-level shutdown/editor/dirty
// context) to the header sync cell. The second result is false when the cell
// should be hidden (no remote configured). Keeping this mapping here — not in
// the git package — keeps display formatting at the call site; git owns only
// the state.
//
// autoCommit suppresses the "commit to sync" hint: when the next auto-sync will
// commit pending edits itself, a dirty tree isn't waiting on the user.
func syncCell(ss git.SyncStatus, dirty int, shuttingDown, editorActive, autoCommit bool, p Palette) (Cell, bool) {
	cell := func(text, fg string) (Cell, bool) {
		return Cell{Text: text, FG: fg}, true
	}
	switch {
	case shuttingDown:
		return cell("⟳ finishing sync…", string(p.AccentSoft))
	case ss.Syncing:
		return cell("⟳ syncing", string(p.AccentSoft))
	case !ss.HasRemote:
		return Cell{}, false
	case ss.Failed:
		return cell("✕ sync", string(p.Danger))
	case ss.Conflicts > 0:
		text := "⚠ " + strconv.Itoa(ss.Conflicts) + " conflict"
		if ss.Conflicts > 1 {
			text += "s"
		}
		return cell(text, string(p.Warning))
	case editorActive:
		return cell("⇅ sync paused", string(p.FgMuted))
	case dirty > 0 && !autoCommit:
		// Auto-sync needs a clean tree (it can't merge over uncommitted edits),
		// so say why it's paused rather than implying it just synced. With
		// auto_commit on, the next tick commits for you, so skip this hint.
		return cell("⇅ commit to sync", string(p.FgMuted))
	case !ss.LastSync.IsZero():
		return cell("⇅ synced "+timeAgo(ss.LastSync), string(p.FgMuted))
	default:
		return cell("⇅ sync", string(p.FgMuted))
	}
}
