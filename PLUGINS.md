# Lua Plugins and Marketplaces

kbrd can install Lua plugins from Git-backed marketplaces. Marketplace
registration is machine-local discovery state; plugin activation belongs only
to the board's committed `kbrd.plugins.lock`. The downloaded cache is disposable
and never decides which plugins load.

Plugins execute with the same privileges as `init.lua` and `.kbrd.lua`, including
the Lua standard library and the `kbrd.*` APIs. Only add marketplaces and plugins
whose code you trust.

## Using plugins

Register and browse one or more marketplaces:

```bash
kbrd plugin marketplace add https://github.com/acme/kbrd-plugins.git
kbrd plugin marketplace add https://git.example.com/team/plugins.git --ref stable
kbrd plugin marketplace list
kbrd plugin marketplace update
kbrd plugin search date
```

### Using a local marketplace

For local plugin development, register a marketplace from a filesystem path:

```bash
kbrd plugin marketplace add ../kbrd-plugins
kbrd plugin add acme/date-tools
```

The path must point to a Git repository with the [marketplace layout](#marketplace-layout),
including `marketplace.json` and a `plugin.json` for each listed plugin. Relative
paths are accepted and stored as absolute paths. A standalone plugin directory
cannot be registered directly; place it in a marketplace repository first.

kbrd clones and verifies the repository rather than loading files directly from
the supplied path. Changes therefore do not become active immediately. Commit
them in the local marketplace, then update the board's locked copy:

```bash
git -C ../kbrd-plugins add .
git -C ../kbrd-plugins commit -m "feat: update date tools"
kbrd plugin update acme/date-tools
```

The update refreshes the marketplace checkout, copies the verified plugin into
kbrd's cache, and rewrites `kbrd.plugins.lock`. Review and commit the lock-file
change with the board.

Add plugins to the current board:

```bash
kbrd plugin add acme/date-tools
kbrd plugin list
kbrd plugin update acme/date-tools
kbrd plugin remove acme/date-tools
```

`add` refreshes the selected catalog, copies and verifies the plugin, then
atomically writes `kbrd.plugins.lock`. Commit that lock with the board. After
cloning the board on another machine, run:

```bash
kbrd plugin sync
```

If locked content is missing when the board opens, kbrd stops before executing
`.kbrd.lua`. Press `i` on the recovery screen to run the synchronization, or `s`
to open the board with Lua disabled for that session. `kbrd --safe` never loads
or synchronizes plugins.

### Updating a marketplace

Refresh one marketplace catalog from its configured Git ref:

```bash
kbrd plugin marketplace update acme
```

Omit the name to refresh every registered marketplace:

```bash
kbrd plugin marketplace update
```

This updates only the machine-local marketplace checkout and catalog metadata.
It does not change `kbrd.plugins.lock` or the plugin versions used by any board.
Use it when you want newly published plugins to appear in `plugin search`, or
before inspecting what a marketplace currently offers.

### Updating a board plugin

Update one plugin used by the current board:

```bash
kbrd plugin update acme/date-tools
git add kbrd.plugins.lock
git commit -m "chore: update date-tools plugin"
```

Omit the plugin ID to update every plugin in the board lock:

```bash
kbrd plugin update
```

Plugin update refreshes the relevant marketplace, resolves the plugin at its
new marketplace commit, verifies and caches its content, and atomically rewrites
`kbrd.plugins.lock`. Review and commit the lock-file diff so other board clones
receive the same revisions. If the marketplace is not registered on the current
machine, update restores its registration from the URL and ref recorded in the
board lock before resolving the new revision.

`kbrd plugin sync` is deliberately different: it installs exactly what the
current lock specifies and never selects a newer revision. After pulling an
updated lock on another machine, run `kbrd plugin sync` to populate its cache.

## Marketplace layout

```text
marketplace-repository/
├── marketplace.json
└── plugins/
    └── date-tools/
        ├── plugin.json
        ├── init.lua
        ├── util.lua
        ├── README.md
        └── CHANGELOG.md
```

`marketplace.json` is the catalog:

```json
{
  "apiVersion": 1,
  "name": "acme",
  "description": "Acme kbrd plugins",
  "owner": {
    "name": "Acme Tools",
    "url": "https://example.com"
  },
  "plugins": [
    {
      "name": "date-tools",
      "source": "plugins/date-tools"
    }
  ]
}
```

Each catalog directory has its own `plugin.json`:

```json
{
  "apiVersion": 1,
  "name": "date-tools",
  "version": "1.2.0",
  "description": "Date parsing and due-date commands",
  "entrypoint": "init.lua",
  "author": {
    "name": "Acme Tools",
    "url": "https://example.com"
  },
  "license": "MIT",
  "homepage": "https://example.com/date-tools",
  "commands": ["set-due-date", "plan-day"],
  "hooks": ["item_saved"],
  "layers": ["planning"],
  "timers": ["daily rollover"],
  "networkAccess": false,
  "shellAccess": false,
  "readme": "README.md",
  "changelog": "CHANGELOG.md"
}
```

Names use lowercase kebab-case. Sources and entrypoints must stay within their
own directories. The commands, hooks, layers, and timers arrays describe what
the entrypoint will register; `networkAccess` and `shellAccess` declare whether
it uses outbound HTTP or shell execution. These fields are metadata for review,
not an enforcement sandbox. README and changelog paths are relative to the
plugin directory and must name regular files. Symlinks, special files, nested
`.git` directories, unknown manifest fields, duplicate names, and plugins larger
than 64 MiB are rejected. Submodules are not initialized.

Inspect a registered marketplace plugin before adding it to a board:

```bash
kbrd plugin info acme/date-tools
```

The command reads `marketplace.json`, `plugin.json`, and the current board lock;
it does not load the Lua entrypoint. It reports both the installed board-lock
version (if any) and the version available in the local marketplace checkout.

Validate a repository before publishing it:

```bash
kbrd plugin validate ./marketplace-repository
```

## Writing Lua plugins

The entrypoint runs before global `init.lua` and board-local `.kbrd.lua`. Require
plugin modules by their fully qualified marketplace and plugin name:

```lua
local util = require("acme.date-tools.util")

kbrd.command("set-due-date", "Set due date", function(ctx)
  util.set_due(ctx.path)
end)

kbrd.layer{
  id = "planning",
  name = "Planning",
  default = true,
  setup = function()
    kbrd.command("plan-day", "Plan day", util.plan_day)
  end,
}
```

kbrd namespaces plugin command IDs automatically. The example is exposed as
`acme/date-tools:set-due-date`, preventing another marketplace plugin from
silently replacing it. Virtual-column IDs receive the same namespace. Registered
editor functions use a Lua-safe `plugin__<marketplace>__<plugin>__<name>` prefix,
with hyphens converted to underscores; for example, `layer_value` from this
plugin is called as `plugin__acme__date_tools__layer_value()`. Plugins may
declare layers directly; layer IDs and resources registered by their setup
callbacks receive the same plugin namespace. Across the plugin and board
declarations, exactly one layer must set `default = true`.

## Lock and cache behavior

The generated lock records the canonical marketplace URL, exact Git commit,
plugin source path, entrypoint, descriptive version, and SHA-256 content digest.
Synchronization checks out that exact commit and refuses content that does not
match the digest.

Marketplace URLs containing credentials are rejected; use normal Git credential
helpers for private repositories. Local marketplace paths are converted to
absolute paths before they are stored.

Removing a plugin changes only the board lock. Old cache entries may remain and
can be garbage-collected later; they are never loaded unless referenced by the
current board's lock.
