# Lua Plugins and Marketplaces

kbrd can install Lua and static-content plugins from Git-backed marketplaces. Marketplace
registration is machine-local discovery state; plugin activation belongs only
to the board's committed `kbrd.plugins.lock`. The downloaded cache is disposable
and never decides which plugins load.

Plugins with a Lua entrypoint execute with the same privileges as `init.lua` and `.kbrd.lua`, including
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
kbrd plugin add acme/date-tools@1.4.2
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
kbrd plugin disable acme/date-tools
kbrd plugin enable acme/date-tools
kbrd plugin update acme/date-tools
kbrd plugin update acme/date-tools --channel beta
kbrd plugin rollback acme/date-tools
kbrd plugin remove acme/date-tools
```

`add` refreshes the selected catalog, copies and verifies the plugin, then
atomically writes `kbrd.plugins.lock`. Commit that lock with the board. After
cloning the board on another machine, run:

```bash
kbrd plugin sync
```

`disable` leaves the plugin and its exact revision in `kbrd.plugins.lock` but
skips its cache checks and Lua entrypoint during startup. This is useful for
isolating plugin startup failures without losing the pin. `enable` makes the
same locked revision active again. Commit either lock-file change so other
clones use the same enabled state.

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

Check updates before allowing new Lua code to become active:

```bash
kbrd plugin outdated
kbrd plugin update --dry-run
kbrd plugin diff acme/date-tools
```

These commands fetch and validate the tracked marketplace revision, then show
version changes, changed manifest fields, and added, modified, or removed
plugin files. `plugin diff` also prints unified content diffs. They do not
rewrite `kbrd.plugins.lock`, refresh the registered marketplace checkout, or
activate/cache the candidate plugin. A missing locked cache may be fetched to a
temporary staging directory for comparison and is removed afterward.

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
selected version or channel, verifies and caches its content, and atomically
rewrites `kbrd.plugins.lock`. An exact version selected with `@version` stays
exact on later updates; the command reports that pin without fetching content
or rewriting the lock. A channel selection stays on that channel. Pass
`--channel` to change the selection policy. Review and commit the lock-file
diff so other board clones receive the same revisions. If the marketplace is
not registered on the current machine, update restores its registration from
the URL and ref recorded in the board lock before resolving the new revision.

Every changed lock entry is retained in lock history. Roll back one step without
consulting the current catalog:

```bash
kbrd plugin rollback acme/date-tools
```

Rollback synchronizes and verifies the previous exact commit and digest before
changing activation. Repeating it walks farther back through that plugin's lock
history.

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
      "source": "plugins/date-tools",
      "versions": [
        {"version": "1.4.2", "ref": "date-tools/v1.4.2"},
        {"version": "1.5.0-beta.2", "ref": "date-tools/v1.5.0-beta.2"}
      ],
      "channels": {
        "stable": "1.4.2",
        "beta": "1.5.0-beta.2"
      }
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

### Static asset packs

A marketplace entry is still a plugin: it keeps one identity, version policy,
lock entry, Git commit, and SHA-256 digest whether it contains Lua, static
assets, or both. Static-only plugins omit `entrypoint` and declare at least one
asset path:

```json
{
  "apiVersion": 1,
  "name": "planning-kit",
  "version": "1.0.0",
  "description": "Planning templates, presets, and starter boards",
  "assets": {
    "cardTemplates": "templates",
    "themes": "themes",
    "frontmatterPresets": "presets.toml",
    "customCommands": "commands.yml",
    "boardStarters": "starters"
  }
}
```

Every asset path is relative to the plugin directory and may name a regular
file or directory. Paths cannot escape the plugin root or name symlinks or
special files. The declared categories are:

- `cardTemplates` for card-template packs
- `themes` for theme definitions
- `frontmatterPresets` for declarative frontmatter presets
- `customCommands` for custom-command packs
- `boardStarters` for board starter kits

Enabled assets load directly from the verified content-addressed cache; kbrd
does not copy generated package state into the board. The native loaders apply
these conventions:

- **Card templates:** the path may be one Markdown file or a directory tree of
  `.md` files. They appear in the template picker as
  `<marketplace>/<plugin>: <template name>`.
- **Frontmatter presets:** the path may be one TOML file or a directory tree of
  `.toml` files using the ordinary `[[frontmatter_presets]]` format. IDs become
  `<marketplace>/<plugin>:<id>`.
- **Custom commands:** the path may be one YAML file or a directory tree of
  `.yml`/`.yaml` files using the ordinary `commands:` format. IDs receive the
  same plugin namespace. Plugin command packs do not load in `--safe` mode.
- **Themes:** the path may be one TOML file or a directory tree. A theme is a
  dark/light palette overlay:

  ```toml
  name = "night"
  base = "dark" # dark or light; defaults to dark

  [palette]
  primary = "#7c3aed"
  primary_strong = "#6d28d9"
  border_active = "#8b5cf6"
  ```

  Select it in `kbrd.toml` with
  `display.theme = "<marketplace>/<plugin>/<name>"`. Palette keys use the
  snake_case names from `theme.Palette`; omitted keys inherit the base theme.
- **Board starters:** the declared path is a directory whose immediate child
  directories are named kits. Inspect and apply one explicitly:

  ```bash
  kbrd plugin starter list
  kbrd plugin starter apply acme/planning-kit simple
  ```

  Application copies files into the board without changing its lock or
  deleting content. Existing files cause an error; `--force` replaces files,
  but `.git` and `kbrd.plugins.lock` are always protected. Use `--target` to
  apply a kit somewhere other than the current board.

Plugins load in stable plugin-ID order. Plugin IDs namespace reusable entries so plugins do
not silently shadow one another. Board-local commands and frontmatter presets
have final precedence and can replace a plugin entry by declaring its fully
qualified ID.

A mixed plugin can declare the same `assets` object alongside `entrypoint`.
Capability and access metadata can describe either its Lua or static portions.
`kbrd plugin add`, `sync`, `update`,
`diff`, and `rollback` operate on the whole plugin tree, so code and assets
cannot move independently of the digest recorded in `kbrd.plugins.lock`.
Verified static paths are read only from the content-addressed plugin cache;
the cache remains disposable and the board lock remains the activation source
of truth.

Names use lowercase kebab-case. Sources and entrypoints must stay within their
own directories. The commands, hooks, layers, and timers arrays describe what
the entrypoint will register; `networkAccess` and `shellAccess` declare whether
it uses outbound HTTP or shell execution. These fields are metadata for review,
not an enforcement sandbox. README and changelog paths are relative to the
plugin directory and must name regular files. Symlinks, special files, nested
`.git` directories, unknown manifest fields, duplicate names, and plugins larger
than 64 MiB are rejected. Submodules are not initialized.

The optional `versions` list publishes semantic versions at Git refs. A version
may override `source`; otherwise it uses the entry's main source. `channels` maps
named channels to published versions. With version metadata present, an
unqualified add selects `stable` (or the highest non-prerelease version when no
explicit stable channel is declared). kbrd resolves each ref to a full commit
and verifies that the selected checkout's `plugin.json` declares the cataloged
version. Moving a tag or channel later cannot change an existing board lock.

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

The generated lock records the selection policy, canonical marketplace URL,
exact Git commit, plugin source path, entrypoint, descriptive version, enabled
state, and SHA-256 content digest. It also retains prior exact entries for
rollback. Synchronization checks out the recorded commit and refuses content
that does not match the digest; it never re-resolves the version or channel.

Marketplace URLs containing credentials are rejected; use normal Git credential
helpers for private repositories. Local marketplace paths are converted to
absolute paths before they are stored.

Removing a plugin changes only the board lock. Old cache entries may remain and
can be garbage-collected later; they are never loaded unless referenced by the
current board's lock.
