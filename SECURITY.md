# Security

## Trust model: opening a board runs its code

A kbrd board is "just a folder." That folder can carry **executable configuration that
kbrd loads and runs automatically the moment you open the board** — with no prompt and
no confirmation. If you `git clone`, sync, or otherwise obtain a board folder authored
by someone else, opening it executes their code with your user privileges.

This is the same trust model as a shell's `.bashrc`/`.zshrc` or an editor's `.vimrc`:
the convenience of folder-local configuration is inseparable from the risk of running
configuration you didn't write. **Treat a board folder the way you'd treat a dotfiles
repo — only open boards you trust.**

There is currently **no per-directory trust gate** — the default is trust-on-open.

## What runs automatically

When a board is opened (at startup, and again on every board switch), kbrd loads:

| File | Scope | What it does |
| --- | --- | --- |
| `~/.config/kbrd/init.lua` | Global (yours) | Lua script, executed at boot |
| `~/.config/kbrd/commands.yml` | Global (yours) | Shell commands registered in the `x` menu |
| `<board>/.kbrd.lua` | **Folder-local (travels with the folder)** | Lua script, executed on open |
| `<board>/.kbrd_commands.yml` | **Folder-local (travels with the folder)** | Shell commands registered in the `x` menu |
| `<board>/.kbrd_hooks.yml` | **Folder-local (travels with the folder)** | Shell commands run automatically on board events |
| `<board>/.kbrd_templates/*.md` | **Folder-local (travels with the folder)** | Card templates; with `[template] exec = true`, a `{{shell}}` directive runs a command on card creation |
| `<board>/.mcp.json` | **Folder-local (travels with the folder)** | Configures MCP servers, which spawn processes |

The global files in `~/.config/kbrd/` are your own. The **folder-local** files are the
supply-chain surface — they come with the folder and execute on someone else's behalf.

### Blast radius

Folder-local `.kbrd.lua` runs with the full Lua standard library plus the `kbrd.*` API,
which includes:

- `kbrd.async.run(cmd, ...)` — run arbitrary shell commands.
- `kbrd.fs.read / write / mkdir / glob` — read and write files. **By design these are
  not sandboxed to the board root**: a script may pass an absolute path
  (`/Users/you/.ssh/id_rsa`) or escape via `..`. Once you trust the folder, full
  filesystem access is the intended behavior (the `.vimrc` model), so this is documented
  rather than restricted.
- `kbrd.timer.every / after` — schedule code to run repeatedly while kbrd is open.

Folder-local `.kbrd_commands.yml` registers shell commands that run when you invoke them
from the `x` menu, and `.mcp.json` can point the MCP server at external processes.

Card-template **shell exec** (`{{shell}}`) is off unless a board or your config sets
`[template] exec = true`. When enabled, a command declared in a template runs on the Bubble
Tea host when you create a card from it (the `T` key — never at render time, never on the
Lua API). Like every other shell path it **inherits kbrd's full environment**, so a command
can read secrets such as `$ANTHROPIC_API_KEY` and exfiltrate them. This is no more powerful
than `.kbrd.lua` (which can already shell out), which is why it is gated the same way and
defaults off.

## Mitigations

- **`kbrd --safe`** opens a board with **all** board-supplied code disabled — Lua scripting,
  event hooks, and template `{{shell}}` exec — regardless of config (including a folder-local
  `kbrd.toml` that tried to enable them). This is the one switch a board cannot ship around;
  use it for any board you didn't author and haven't reviewed.
- **Disable scripting** for untrusted boards: set `[scripting] enabled = false` in
  `~/.config/kbrd/config.toml` (or a board's `kbrd.toml`). With scripting disabled no Lua
  VM is created and `.kbrd.lua` is never read. See [SCRIPTING.md](./SCRIPTING.md).
- **Keep template exec off** (`[template] exec = false`, the default) unless you trust the
  board and want its `{{shell}}` templates to run.
- **Review before opening**: inspect `.kbrd.lua`, `.kbrd_commands.yml`, `.kbrd_hooks.yml`,
  `.kbrd_templates/`, and `.mcp.json` in any board you didn't author before opening it.
- **Delete** folder-local config you don't need or trust.

## Reporting a vulnerability

Please report security issues privately rather than opening a public issue — use GitHub
private security advisories at <https://github.com/up2jj/kbrd/security/advisories/new>.
We'll acknowledge the report and work with you on a fix before any public disclosure.
