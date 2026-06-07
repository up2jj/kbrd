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

# 2. Markdown peek
Space
Sleep 1s
Screenshot docs/screenshots/peek.png
Escape
Sleep 500ms

# 3. Help overlay
Type "?"
Sleep 1s
Screenshot docs/screenshots/help.png
Escape
Sleep 500ms

# 4. Git panel (sample board has one modified file)
Type "g"
Sleep 1500ms
Screenshot docs/screenshots/git-panel.png
Type "q"
Sleep 500ms

# 5. Custom commands menu (folder-local .kbrd_commands.yml, run with `x`)
Type "x"
Sleep 1s
Screenshot docs/screenshots/custom-commands.png
Escape
Sleep 500ms

# 6. Board switcher (HOME is isolated, so only demo boards are listed)
Ctrl+P
Sleep 1s
Screenshot docs/screenshots/switcher.png
Escape
Sleep 500ms

# 7. Global search
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
