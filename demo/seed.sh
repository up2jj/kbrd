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

# Board-wide template so the template flow (`t`) opens a rich form directly.
# A single template skips the picker and goes straight to the form. Mirrors
# examples/templates/bug.md.
mkdir -p "$board/.kbrd_templates"
cp "$here/../examples/templates/bug.md" "$board/.kbrd_templates/bug.md"

# Folder-local Lua script registering a "Tasks" virtual column (ripgreps the
# board's open `- [ ]` checkboxes). The Backlog cards already have unchecked
# boxes, so the column lists several tasks. Kept in sync with the example.
cp "$here/../examples/tasks/tasks.lua" "$board/.kbrd.lua"

# Folder-local custom commands so the `x` menu has entries to show.
cat > "$board/.kbrd_commands.yml" <<'EOF'
# Custom commands for the sample board. Press `x` on a card to run one.
commands:
  - name: Edit in vim
    id: edit-vim
    description: Open the card in vim
    command: vim "{{.filePath}}"

  - name: Word count
    id: word-count
    description: Show wc stats for the card
    command: wc "{{.filePath}}"

  - name: Reveal column in Finder
    id: reveal-finder
    description: Open the column folder
    command: open "{{.columnPath}}"

  - name: Copy path to clipboard
    id: copy-path
    description: Copy the card's absolute path
    command: printf '%s' "{{.filePath}}" | pbcopy
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

# --- Isolated HOME with a clean recents list (for the switcher screenshot) ---
# kbrd reads recents from os.UserConfigDir(), i.e.
# $HOME/Library/Application Support/kbrd/recent.json on macOS. We run the
# capture under demo/home so the switcher shows only these demo boards and
# never your real recents.
home="$here/home"
cache="$home/Library/Application Support/kbrd"
rm -rf "$home"
mkdir -p "$cache"

# A few sibling demo boards so the switcher has something to list.
for b in Personal Work "Open Source"; do
  mkdir -p "$here/boards/$b/Todo" "$here/boards/$b/Done"
done

cat > "$cache/recent.json" <<EOF
{
  "entries": [
    { "path": "$board", "name": "Sample Board" },
    { "path": "$here/boards/Work", "name": "Work", "pinned": true },
    { "path": "$here/boards/Personal", "name": "Personal" },
    { "path": "$here/boards/Open Source", "name": "Open Source" }
  ]
}
EOF

echo "$board"
