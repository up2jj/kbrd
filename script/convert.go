package script

import (
	lua "github.com/yuin/gopher-lua"
)

// toLValue converts a Go value (typically the ctx map handed in from the
// model) into a Lua value for handoff to a script. Supported inputs:
//   - nil               → lua.LNil
//   - string / int / bool / float64
//   - map[string]string and map[string]interface{}
//   - []interface{} and []string
//
// Anything else is stringified.
func toLValue(L *lua.LState, v interface{}) lua.LValue {
	switch x := v.(type) {
	case nil:
		return lua.LNil
	case string:
		return lua.LString(x)
	case bool:
		return lua.LBool(x)
	case int:
		return lua.LNumber(x)
	case int64:
		return lua.LNumber(x)
	case float64:
		return lua.LNumber(x)
	case map[string]string:
		t := L.NewTable()
		for k, val := range x {
			t.RawSetString(k, lua.LString(val))
		}
		return t
	case map[string]interface{}:
		t := L.NewTable()
		for k, val := range x {
			t.RawSetString(k, toLValue(L, val))
		}
		return t
	case []interface{}:
		t := L.NewTable()
		for i, val := range x {
			t.RawSetInt(i+1, toLValue(L, val))
		}
		return t
	case []string:
		t := L.NewTable()
		for i, val := range x {
			t.RawSetInt(i+1, lua.LString(val))
		}
		return t
	}
	return lua.LNil
}
