# Clipboard ring

kbrd keeps a small clipboard history for assembling cards from several pieces
of work. Copying a card with `c`, or yanking text in the Vim editor, still writes
to the operating-system clipboard and also records the copied content in the
local ring.

Press `C` to open the browser. It supports keyboard selection and a preview of
the highlighted entry. Press `/` to enter fuzzy filtering. `Enter` sends the
entry through the normal paste menu, where it can be inserted as a new card,
prepended, appended, or added as a journal entry. The browser also supports:

When a ring entry opens the paste-choice menu, `esc` returns to the ring. A
confirmed paste returns to the board as usual.

| Key | Action |
| --- | --- |
| `i` | Import the current OS clipboard into the ring |
| `p` | Pin or unpin the selected entry |
| `d` | Delete the selected entry |
| `c` | Clear the entire ring |
| `/` | Enter fuzzy search |
| `esc` / `q` | Close the browser |

Entries are typed as plain text, Markdown, checklist, frontmatter, code, or a
link using a lightweight content detector. Each entry records when it was
copied and, for card copies, the board, column, and card source. Marked-card
copies become one composite Markdown entry with a card count in its metadata.

The ring is machine-local and never lives in a board directory or Git history.
It is stored as `clipboard.json` below the platform's user config directory,
with a 100-entry limit and atomic writes. Pinned entries are retained when old
unpinned entries are pruned; if every entry is pinned, the oldest pinned entry
is removed to enforce the limit.

The system clipboard and the ring are separate: `v` continues to read the
external clipboard, while `C` selects from kbrd's saved history.
