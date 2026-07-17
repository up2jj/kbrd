package script

// uiBootstrap exposes declarative widgets while keeping the original scalar
// pick, prompt, and confirm calls source-compatible.
const uiBootstrap = `
kbrd.ui = {}

	local function request(kind, spec)
	  if type(spec) ~= "table" then
	    error("kbrd.ui." .. kind .. ": expected an options table", 2)
	  end
	  spec._uiReq = true
	  spec.kind = kind
	  kbrd._uiGuard(kind)
	  return coroutine.yield(spec)
	end

	function kbrd.ui.input(spec)
	  return request("input", spec)
	end

	function kbrd.ui.textarea(spec)
	  return request("textarea", spec)
	end

	function kbrd.ui.select(spec)
	  return request("select", spec)
	end

	function kbrd.ui.multiselect(spec)
	  return request("multiselect", spec)
	end

	function kbrd.ui.form(spec)
	  return request("form", spec)
	end

	function kbrd.ui.actions(spec)
	  return request("actions", spec)
	end

	function kbrd.ui.viewer(spec)
	  return request("viewer", spec)
	end

	local function normalize_pick_choices(choices)
	  if choices == nil then choices = {} end
	  if type(choices) ~= "table" then
	    error("kbrd.ui.pick: choices must be a table", 3)
	  end
	  local count, max_index = 0, 0
	  for index in pairs(choices) do
	    if type(index) ~= "number" or index < 1 or index % 1 ~= 0 then
	      error("kbrd.ui.pick: choices must be a contiguous sequence", 3)
	    end
	    count = count + 1
	    if index > max_index then max_index = index end
	  end
	  if count ~= max_index then
	    error("kbrd.ui.pick: choices must be a contiguous sequence", 3)
	  end
	  local items = {}
	  for i = 1, max_index do
	    items[i] = {id = tostring(i), label = choices[i]}
	  end
	  return choices, items
	end

	function kbrd.ui.pick(title, choices)
	  local items
	  choices, items = normalize_pick_choices(choices)
	  local result = request("select", {title = title or "", items = items})
	  if result == nil or result.cancelled then return nil end
	  return choices[tonumber(result.value)]
	end

	function kbrd.ui.prompt(title, default)
	  local result = request("input", {title = title or "", initial = default or "", max_length = 256})
	  if result == nil or result.cancelled then return nil end
	  return result.value
	end

	function kbrd.ui.confirm(spec)
	  if type(spec) == "table" then
	    return request("confirm", spec)
	  end
	  local result = request("confirm", {title = spec or "", default = true})
	  if result == nil or result.cancelled then return false end
	  return not not result.value
	end

	function kbrd.ui.notify(spec)
	  if type(spec) ~= "table" then
	    error("kbrd.ui.notify: expected an options table", 2)
	  end
	  if type(spec.message) ~= "string" or spec.message == "" then
	    error("kbrd.ui.notify: field 'message' must be a non-empty string", 2)
	  end
	  if spec.level ~= nil and type(spec.level) ~= "string" then
	    error("kbrd.ui.notify: field 'level' must be a string", 2)
	  end
	  local level = spec.level or "info"
	  if level ~= "info" and level ~= "success" and level ~= "warning" and level ~= "error" then
	    error("kbrd.ui.notify: field 'level' must be info, success, warning, or error", 2)
	  end
	  kbrd.notify(spec.message, level)
	end
	`
