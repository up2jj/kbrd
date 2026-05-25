package mcp

import "strings"

// agentsTemplate is the AGENTS.md dropped into a board to orient LLM tools: how
// the board maps to the filesystem and which MCP tools to use. It is kept in
// the mcp package, the owner of the tool surface it documents. The § sentinel
// stands in for a Markdown code-span backtick (which a Go raw string can't
// contain) and is substituted by AgentsMarkdown.
const agentsTemplate = `# kbrd — agent guide

kbrd is a file-based kanban board that lives entirely on disk. This file
describes how the board is structured so AI tools can read and modify it
safely.

## Layout

- **Board** — this directory. Its friendly name (if set) comes from the
  §[board] name§ field in §kbrd.toml§.
- **Folder (column)** — each immediate subdirectory is a column on the board,
  shown left-to-right in alphabetical order. Directories whose names start with
  §.§ or §_§ are ignored (e.g. §.git§, §_archive§).
- **Item (card)** — each §*.md§ file inside a folder is one card. The file name
  (without §.md§) is the card title; the file body is the card content.

To add a card, create a §<title>.md§ file in the desired folder. To move a card
between columns, move the file. There is no database — the filesystem *is* the
state.

## MCP server (server name: ` + ServerName + `)

kbrd runs a built-in MCP server (Streamable HTTP) while the TUI is open. The
§.mcp.json§ in this directory points MCP clients at it. Available tools:

- §list_boards§ — discover boards and their friendly names.
- §list_folders§ — list a board's folders (columns).
- §list_files§ — list the cards in a folder.
- §add_file_to_board§ — create a card in a board folder by friendly name;
  optional §folder§ (defaults to the first column) and §content§; set
  §create_folder§ to make a missing folder.
- §list_custom_commands§ — list the board's shell custom commands.
- §run_custom_command§ — run one of those commands by id.

Prefer these tools over editing files blindly: resolve the board with
§list_boards§, inspect folders with §list_folders§, then §add_file_to_board§.

## Conventions

- Card files are plain Markdown. Keep the first line a short title or summary.
- The default folder for a new card is the first column alphabetically (often a
  "TODO"-style column). Pass an explicit §folder§ to place it elsewhere.
- Don't create files in §.§/§_§-prefixed directories; kbrd hides them.
`

// AgentsMarkdown returns the contents of an AGENTS.md describing kbrd for LLM
// tools working against a board.
func AgentsMarkdown() []byte {
	return []byte(strings.ReplaceAll(agentsTemplate, "§", "`"))
}
