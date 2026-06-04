-- examples/tasks/tasks.lua — a kbrd virtual column listing open tasks.
--
-- Drop this into ~/.config/kbrd/init.lua (global) or <board>/.kbrd.lua
-- (folder-local). It adds a script-driven "Tasks" column to the right of your
-- filesystem columns, listing every open `- [ ]` checkbox found by ripgrep.
-- TAB onto it; press `c` to mark complete, `e` to edit the task text.
--
-- It stays in sync automatically: a fresh scan runs on board_load (open/switch)
-- and on board_refresh (the file watcher fires this whenever a .md file is
-- added or edited), so newly added tasks show up without any manual refresh.
--
-- Notes:
--   * `rg` runs with the active board as its working directory, so `.` scans
--     the current board. Use `~/boards` (or any path) to go cross-board.
--   * Virtual columns are read-only to file moves — all actions are the
--     declared commands below.
--   * kbrd.async.run can't be called from a timer, so refresh is event-driven.

local COLUMN_ID = "tasks"

-- Apply `transform` to one line of a file, rewriting it in place.
local function rewrite_line(path, line_no, transform)
  local body = kbrd.fs.read(path)
  if not body then return false, "cannot read " .. path end
  local out, i, hit = {}, 0, false
  for line in (body .. "\n"):gmatch("(.-)\n") do
    i = i + 1
    if i == line_no then line = transform(line); hit = true end
    out[#out + 1] = line
  end
  if not hit then return false, "task line not found (file changed?)" end
  kbrd.fs.write(path, table.concat(out, "\n"))
  return true
end

-- refresh() coalesces overlapping requests: at most one rg scan runs at a time,
-- and if a change arrives mid-scan, exactly one more scan runs afterward so the
-- column always reflects the latest on-disk state.
local refresh
local scanning, pending = false, false

local function scan()
  scanning = true
  kbrd.async.run([[rg --line-number --no-heading -e '^\s*- \[ *\]\s' . --glob '*.md']],
    function(r)
      scanning = false
      if r.exitCode > 1 then
        kbrd.notify("task scan failed", "error")
      else
        local items = {}
        for entry in (r.out .. "\n"):gmatch("(.-)\n") do
          local path, lno, text = entry:match("^(.-):(%d+):%s*%- %[ *%]%s*(.*)$")
          if path then
            items[#items + 1] = {
              id    = path .. ":" .. lno,            -- stable key → cursor survives a re-scan
              title = text,
              icon  = "☐",
              meta  = path:match("([^/]+)/[^/]+$"),  -- source folder, shown on line 3
              data  = { path = path, line = tonumber(lno) },
            }
          end
        end
        kbrd.column.set(COLUMN_ID, {
          name  = "Tasks",
          empty = (#items == 0) and "no open tasks 🎉" or nil,
          items = items,
          commands = {
            { id = "complete", name = "Mark complete", key = "c", default = true,
              run = function(ctx)
                if not kbrd.ui.confirm('Complete "' .. ctx.title .. '"?') then return end
                local ok, err = rewrite_line(ctx.data.path, ctx.data.line,
                  function(l) return (l:gsub("%[ *%]", "[x]", 1)) end)
                if ok then kbrd.notify("done: " .. ctx.title, "success"); refresh()
                else kbrd.notify(err, "error") end
              end },
            { id = "edit", name = "Edit text", key = "e",
              run = function(ctx)
                local new = kbrd.ui.prompt("Task text:", ctx.title)
                if not new or new == "" or new == ctx.title then return end
                local ok, err = rewrite_line(ctx.data.path, ctx.data.line,
                  function(l) return l:gsub("(%- %[ *%]%s*).*", "%1" .. new, 1) end)
                if ok then refresh() else kbrd.notify(err, "error") end
              end },
          },
        })
      end
      if pending then pending = false; refresh() end
    end)
end

refresh = function()
  if scanning then pending = true; return end
  scan()
end

kbrd.on("board_load", refresh)     -- open / board switch
kbrd.on("board_refresh", refresh)  -- file watcher: picks up added / edited / completed tasks
