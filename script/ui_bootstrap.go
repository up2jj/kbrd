package script

// uiBootstrap keeps the positional API stable while using the structured
// request/result protocol internally. New table-based widgets are added in
// later phases without changing this coroutine bridge.
const uiBootstrap = `
kbrd.ui = {}

	local function request(spec)
	  kbrd._uiGuard(spec.kind)
	  return coroutine.yield(spec)
	end

	function kbrd.ui.pick(title, choices)
	  local result = request({_uiReq = true, kind = "pick", title = title or "", choices = choices or {}})
	  if result == nil or result.cancelled then return nil end
	  return result.value
	end

	function kbrd.ui.prompt(title, default)
	  local result = request({_uiReq = true, kind = "prompt", title = title or "", default = default or ""})
	  if result == nil or result.cancelled then return nil end
	  return result.value
	end

	function kbrd.ui.confirm(title)
	  local result = request({_uiReq = true, kind = "confirm", title = title or ""})
	  if result == nil or result.cancelled then return false end
	  return not not result.value
	end
	`
