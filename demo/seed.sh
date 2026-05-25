#!/usr/bin/env bash
# Build a deterministic sample board for screenshots/demos.
# Idempotent: wipes and recreates demo/sample-board on every run.
# Does NOT touch your real boards. The board is a git repo with one
# uncommitted change so the git panel has something to show.
set -euo pipefail

here="$(cd "$(dirname "${BASH_SOURCE[0]}")" && pwd)"
board="$here/sample-board"

rm -rf "$board"
mkdir -p "$board/Backlog" "$board/In Progress" "$board/Done"

cat > "$board/Backlog/Polish the theme palette.md" <<'EOF'
# Polish the theme palette

Tune contrast for the **dark** theme and verify the light theme is
readable on a white terminal.

- [ ] Audit border colors
- [ ] Check selected-row contrast
- [ ] Screenshot both themes
EOF

cat > "$board/Backlog/Investigate file watcher races.md" <<'EOF'
# Investigate file watcher races

Rapid external edits occasionally land before the previous reload
finishes. Add a short debounce around `fsnotify` events.
EOF

cat > "$board/Backlog/Document the Lua API.md" <<'EOF'
# Document the Lua API

Expand `SCRIPTING.md` with end-to-end examples for `kbrd.on`,
`kbrd.timer`, and `kbrd.async`.
EOF

cat > "$board/In Progress/Add a built-in MCP server.md" <<'EOF'
# Add a built-in MCP server

Expose board operations over Streamable HTTP so agents can add and
list cards headlessly.

## Tools
- `list_boards`, `list_folders`, `list_files`
- `add_file_to_board`
- `run_custom_command`
EOF

cat > "$board/In Progress/Global search across boards.md" <<'EOF'
# Global search across boards

Fuzzy, ripgrep-backed search over every recent board. Debounced input,
results capped for responsiveness.
EOF

cat > "$board/Done/Git panel.md" <<'EOF'
# Git panel

Diff, commit, log, and sync (pull + push) without leaving the board.
EOF

cat > "$board/Done/Board switcher.md" <<'EOF'
# Board switcher

Fuzzy switch between boards, pin favorites, and remove stale boards.
EOF

cat > "$board/Done/Markdown peek.md" <<'EOF'
# Markdown peek

Press space to preview a card's rendered Markdown in a scrollable
viewport.
EOF

cd "$board"
if [ ! -d .git ]; then
  git init -q
  git config user.email "demo@kbrd.local"
  git config user.name "kbrd demo"
fi
git add -A
git commit -q -m "Seed sample board" || true

# Leave one uncommitted change so the git panel shows a modified file.
printf '\n- [ ] Add a screenshot to the README\n' >> "$board/Backlog/Polish the theme palette.md"

echo "$board"
