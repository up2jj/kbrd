# Browser extension

kbrd includes a Manifest V3 browser extension for capturing pages into local boards. The extension
is bundled inside the `kbrd` binary and installed through the browser's **Load unpacked** workflow;
it is not distributed through a Web Store.

## Install

Extract the extension and register the current `kbrd` executable as its Native Messaging host:

```bash
kbrd extension install
```

The extension is extracted to `~/kbrd-chrome-extension` by default. To choose another stable
location, pass `--dir`:

```bash
kbrd extension install --dir /path/to/kbrd-extension
```

Open the browser's extension management page, enable **Developer mode**, choose **Load unpacked**,
and select the directory printed by the installer. For Chrome, the management page is
`chrome://extensions`; other Chromium-based browsers expose an equivalent page. On macOS, press
**Command-Shift-G** in the folder chooser and paste the printed path if navigating to it is
inconvenient.

The extension has a fixed ID across installations. Its displayed version is derived from the kbrd
binary version.

## Capture a page

Open **kbrd Capture** from the browser toolbar. The form lets you:

- select a known board and column;
- review or edit the page-derived card name;
- include the current text selection;
- add notes; and
- include or omit the page URL.

The last selected board and column are remembered. Page titles are automatically converted to safe
card filenames when saved, so browser punctuation and path separators do not need to be removed
manually.

Captured cards receive `source`, `captured_at`, and optional `url` frontmatter.

## Capture selected text

Highlight text on a page, right-click it, and choose **Capture selection to kbrd…**. The extension
opens the same confirmation form with the selection, page title, and URL prefilled. No card is
created until **Save card** is pressed. The context-menu entry appears only when text is selected.

## How it connects

The extension invokes the installed `kbrd` binary directly through Chromium Native Messaging. It
does not require a running TUI, web server, or MCP server.

The native host supports only:

- listing known boards;
- listing a board's columns; and
- creating a card.

It cannot read existing card contents or run custom commands. The native-host registration accepts
only the bundled extension's fixed origin. See [SECURITY.md](./SECURITY.md) for the broader trust
model.

## Update or move kbrd

Re-run the installer after upgrading or moving the `kbrd` executable:

```bash
kbrd extension install
```

The command atomically refreshes files in a directory previously managed by kbrd and refuses to
take over an unrelated non-empty directory. It also updates the Native Messaging registration to
point at the current executable. It does not reload an unpacked extension inside the browser.

After installation, open the browser's extension management page and click **Reload** on **kbrd
Capture**.

## Troubleshooting

- If the extension cannot reach kbrd, re-run `kbrd extension install` using the current binary and
  reload the extension.
- If the right-click action is missing, make sure text is selected and reload the extension from
  the browser's extension management page.
- If updated behavior is missing, confirm that the binary used to run `kbrd extension install`
  contains the new extension assets; the installer extracts the version embedded in that binary.
