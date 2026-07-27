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

## Marketplace layout

```text
marketplace-repository/
├── marketplace.json
└── plugins/
    └── date-tools/
        ├── plugin.json
        ├── init.lua
        └── util.lua
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
    "name": "Acme Tools"
  },
  "license": "MIT",
  "homepage": "https://example.com/date-tools"
}
```

Names use lowercase kebab-case. Sources and entrypoints must stay within their
own directories. Symlinks, special files, nested `.git` directories, unknown
manifest fields, duplicate names, and plugins larger than 64 MiB are rejected.
Submodules are not initialized.

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
```

kbrd namespaces plugin command IDs automatically. The example is exposed as
`acme/date-tools:set-due-date`, preventing another marketplace plugin from
silently replacing it. Registered editor functions and virtual-column IDs are
also namespaced.

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
