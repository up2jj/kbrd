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

// fromLValue converts a Lua value back into a plain Go value, used to snapshot a
// script-provided `data` table at kbrd.column.set time so it can round-trip into
// a command ctx later without holding a live Lua reference. Tables become
// map[string]interface{} (string keys) or []interface{} (1..n integer keys);
// everything else maps to its Go scalar. Nested tables recurse. Functions,
// userdata, and other non-data values become nil.
func fromLValue(v lua.LValue) interface{} {
	switch x := v.(type) {
	case lua.LBool:
		return bool(x)
	case lua.LNumber:
		return float64(x)
	case lua.LString:
		return string(x)
	case *lua.LTable:
		// Decide array vs map: a table is treated as an array when its only keys
		// are 1..N contiguous integers.
		maxN := x.Len()
		isArray := maxN > 0
		count := 0
		x.ForEach(func(k, _ lua.LValue) {
			count++
			if n, ok := k.(lua.LNumber); !ok || float64(n) != float64(int(n)) || int(n) < 1 || int(n) > maxN {
				isArray = false
			}
		})
		if isArray && count == maxN {
			arr := make([]interface{}, 0, maxN)
			for i := 1; i <= maxN; i++ {
				arr = append(arr, fromLValue(x.RawGetInt(i)))
			}
			return arr
		}
		m := make(map[string]interface{})
		x.ForEach(func(k, val lua.LValue) {
			if ks, ok := k.(lua.LString); ok {
				m[string(ks)] = fromLValue(val)
			}
		})
		return m
	}
	return nil
}
