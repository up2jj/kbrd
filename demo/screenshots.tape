# VHS tape: capture static screenshots of kbrd's key screens.
# Run from the repo root:  vhs demo/screenshots.tape
# Requires: vhs, a built kbrd binary (KBRD env or ./kbrd), and demo/seed.sh already run.

Set Shell "bash"
Set FontSize 16
Set Width 1280
Set Height 800
Set Padding 20
Set Theme "Catppuccin Mocha"

# A screenshot is needed for the GIF output VHS always produces; discard it.
Output demo/.discard.gif

# Use an isolated HOME (seeded by seed.sh) so the switcher shows only demo
# boards, then enter the sample board (cwd is the board root) and launch kbrd.
Hide
Type "export HOME=$(pwd)/demo/home" Enter
Type "cd demo/sample-board && ${KBRD:-../../kbrd}" Enter
Sleep 2s
Show

# 1. Main board
Sleep 1s
Screenshot docs/screenshots/board.png

# 2. Zoom a single column (+/= toggles, - exits)
Type "+"
Sleep 1s
Screenshot docs/screenshots/zoom.png
Type "-"
Sleep 500ms

# 3. Markdown peek
Space
Sleep 1s
Screenshot docs/screenshots/peek.png
Escape
Sleep 500ms

# 4. Help overlay
Type "?"
Sleep 1s
Screenshot docs/screenshots/help.png
Escape
Sleep 500ms

# 5. Template form (board-wide .kbrd_templates/bug.md, run with `t`)
Type "t"
Sleep 1s
Type "Theme contrast regression"
Sleep 800ms
Screenshot docs/screenshots/template.png
Escape
Escape
Sleep 500ms

# 6. Git panel (sample board has one modified file)
Type "g"
Sleep 1500ms
Screenshot docs/screenshots/git-panel.png
Type "q"
Sleep 500ms

# 7. Custom commands menu (folder-local .kbrd_commands.yml, run with `x`)
Type "x"
Sleep 1s
Screenshot docs/screenshots/custom-commands.png
Escape
Sleep 500ms

# 8. Virtual column: tab past the fs columns (Backlog → In Progress → Done →
# Tasks) to the Lua-driven "Tasks" column, then let the async rg scan render.
Tab
Tab
Tab
Sleep 1500ms
Screenshot docs/screenshots/virtual-column.png

# 9. Board switcher (HOME is isolated, so only demo boards are listed)
Ctrl+P
Sleep 1s
Screenshot docs/screenshots/switcher.png
Escape
Sleep 500ms

# 10. Global search
Type "f"
Sleep 800ms
Type "search"
Sleep 1500ms
Screenshot docs/screenshots/search.png
Escape
Sleep 500ms

# Quit cleanly
Ctrl+C
Sleep 500ms
