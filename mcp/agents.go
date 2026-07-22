package mcp

import "strings"

// agentsTemplate is the AGENTS.md dropped into a board to orient LLM tools: how
// the board maps to the filesystem and which MCP tools to use. The § sentinel
// stands in for a Markdown code-span backtick and is substituted by
// AgentsMarkdown.
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
- §get_card§ — read raw Markdown, parsed frontmatter, and a revision hash;
  requires §[mcp] allow_card_reads = true§.
- §search_cards§ — search names, bodies, tags, and frontmatter across columns;
  requires §[mcp] allow_card_reads = true§.
- §update_card§ — replace complete Markdown using an §expected_revision§.
- §move_card§ and §rename_card§ — relocate or rename without overwriting.
- §delete_card§ — delete only when §expected_revision§ still matches.
- §create_column§ — create a durable empty column.
- §list_custom_commands§ — list the board's shell custom commands, optionally
  filtered with the same §folder§ and §item§ context used for execution.
- §run_custom_command§ — run one of those commands by id.

Prefer these tools over editing files blindly: resolve the board with
§list_boards§, inspect folders with §list_folders§, then use the focused card or
column tool for the requested operation.
When the MCP client supports form elicitation, kbrd may ask the user to choose
between ambiguous boards, create or replace an unknown folder while adding a
card, or replace an unknown custom-command id with an available command.

Read-only MCP resources offer the same hierarchy as stable context:

- §kbrd://boards§ — discover known boards and their board resource URIs.
- §kbrd://board/{board}§ — read a board snapshot with columns and cards.
- §kbrd://card/{board}/{column}/{card}§ — read a card's complete Markdown
  when §[mcp] allow_card_reads = true§.

The server also offers built-in MCP prompts for summarizing, triaging, and
planning board work, optionally scoped to a column. Board-local prompts from
§.kbrd_mcp_prompts.yml§ are advertised with a board-qualified name such as
§work__weekly_review§.

## Conventions

- Card files are plain Markdown. Keep the first line a short title or summary.
- The default folder for a new card is the first column alphabetically (often a
  "TODO"-style column). Pass an explicit §folder§ to place it elsewhere.
- Don't create files in §.§/§_§-prefixed directories; kbrd hides them.
`

// instructionsTemplate is the operating guide sent to every MCP client during
// initialization. Keep it independent of the client's working directory: one
// server can expose several boards discovered through kbrd's recents store.
// The § sentinel stands in for a Markdown code-span backtick (which a Go raw
// string cannot contain) and is substituted by ServerInstructions.
const instructionsTemplate = `kbrd manages file-based kanban boards. A board is
a directory, each immediate visible subdirectory is a folder (column), and each
§*.md§ file in a folder is an item (card). Folder names sort alphabetically from
left to right. Names beginning with §.§ or §_§ are hidden.

## Workflow

- Do not infer a board from the client's working directory. Start with
  §list_boards§ and use the returned friendly name for later calls. The server
  may expose multiple boards from kbrd's recents store.
- If a board name is missing or ambiguous, list the boards and ask the user to
  choose; do not guess. kbrd may elicit this choice directly when the client
  supports it. Inspect a selected board with §list_folders§ and §list_files§
  before changing it when the target is not already unambiguous.
- Use §add_file_to_board§ to create a card. The §name§ is the filename/title
  (the §.md§ suffix is optional), §content§ is its Markdown body, and §folder§
  selects its column. If §folder§ is omitted, the first alphabetical column is
  used. An unknown named folder may trigger an elicitation offering existing
  folders. Set §create_folder§ only when the user asked to create a new column.
- Card creation never overwrites an existing file. If a name conflicts, report
  it and ask for a different name instead of silently changing or replacing it.
- Do not move a card to a Done-like column merely because work appears complete.
  Use §move_card§, §update_card§, §rename_card§, and §delete_card§ only when the
  user explicitly requests the corresponding change. Never emulate them with a
  shell command.
- Before updating or deleting, call §get_card§ or §search_cards§ and pass its
  revision as §expected_revision§. On a revision conflict, read the card again
  and reconsider the change; do not blindly retry stale content.
- Use §kbrd://boards§ and §kbrd://board/{board}§ when MCP resources are
  available. Card resources contain complete Markdown, including frontmatter,
  and are advertised only when §[mcp] allow_card_reads = true§.
- Built-in MCP prompts cover board summaries, triage, and work planning.
  Boards may add prompts in §.kbrd_mcp_prompts.yml§; their names are qualified
  with the board name so prompts from different boards do not replace one another.

## Card content

Cards are plain Markdown. Use a short, descriptive filename and put detail in
the body. Optional YAML frontmatter must be the first block in the file, bounded
by §---§ lines. Common keys include §accent§, §icon§, §meta§, §tags§, §assignee§,
and §pinned§; custom keys are allowed. Preserve valid frontmatter when composing
content and do not invent metadata the user did not request.

## Custom commands and safety

- §list_custom_commands§ lists configured shell commands for a board. Pass
  §folder§ and optionally §item§ to return commands applicable to that context;
  its arguments can then be forwarded to §run_custom_command§.
- §run_custom_command§ executes a listed command by id only when server policy
  permits it. Commands may modify files or run arbitrary programs. Run one only
  when the user's request requires that specific command; never use it to bypass
  a disabled or absent MCP operation. Treat deletion and other destructive
  effects as requiring explicit user intent. An unknown command id may trigger
  an elicitation offering only commands permitted by the current server policy.
- Lua commands are available only in the kbrd TUI and are not exposed by MCP.

Available tools: §list_boards§, §list_folders§, §list_files§,
§add_file_to_board§, §get_card§, §search_cards§, §update_card§, §move_card§,
§rename_card§, §delete_card§, §create_column§, §list_custom_commands§, and
§run_custom_command§.
`

// ServerInstructions returns the working-directory-independent operating guide
// advertised to MCP clients in the initialize response.
func ServerInstructions() string {
	return strings.ReplaceAll(instructionsTemplate, "§", "`")
}

// AgentsMarkdown returns the contents of an AGENTS.md describing kbrd for LLM
// tools working against a board.
func AgentsMarkdown() []byte {
	return []byte(strings.ReplaceAll(agentsTemplate, "§", "`"))
}
