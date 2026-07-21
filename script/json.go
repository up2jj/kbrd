package script

import (
	"bytes"
	"encoding/json"
	"fmt"
	"io"
	"math"
	"strconv"

	lua "github.com/yuin/gopher-lua"
)

const maxJSONDepth = 100

// installJSONAPI installs kbrd.json and preloads the same table as
// require("json"). Container constructors tag otherwise-ambiguous Lua tables
// so empty arrays and objects survive a decode/encode round trip.
func (h *Host) installJSONAPI(kbrd *lua.LTable) {
	L := h.L
	h.jsonArrayMeta = L.NewTable()
	h.jsonObjectMeta = L.NewTable()
	h.jsonNull = L.NewUserData()
	h.jsonNull.Value = jsonNullMarker{}
	nullMeta := L.NewTable()
	nullMeta.RawSetString("__tostring", L.NewFunction(func(L *lua.LState) int {
		L.Push(lua.LString("null"))
		return 1
	}))
	L.SetMetatable(h.jsonNull, nullMeta)

	module := L.NewTable()
	module.RawSetString("encode", L.NewFunction(h.luaJSONEncode))
	module.RawSetString("decode", L.NewFunction(h.luaJSONDecode))
	module.RawSetString("array", L.NewFunction(h.luaJSONArray))
	module.RawSetString("object", L.NewFunction(h.luaJSONObject))
	module.RawSetString("null", h.jsonNull)
	kbrd.RawSetString("json", module)
	L.PreloadModule("json", func(L *lua.LState) int {
		L.Push(module)
		return 1
	})
}

type jsonNullMarker struct{}

func (h *Host) luaJSONEncode(L *lua.LState) int {
	value, err := h.luaToJSON(L.CheckAny(1), make(map[*lua.LTable]bool), 0)
	if err != nil {
		return errResult(L, fmt.Errorf("kbrd.json.encode: %w", err))
	}
	body, err := json.Marshal(value)
	if err != nil {
		return errResult(L, fmt.Errorf("kbrd.json.encode: %w", err))
	}
	L.Push(lua.LString(body))
	return 1
}

func (h *Host) luaJSONDecode(L *lua.LState) int {
	value, err := decodeJSON([]byte(L.CheckString(1)))
	if err != nil {
		return errResult(L, fmt.Errorf("kbrd.json.decode: %w", err))
	}
	decoded, err := h.jsonToLua(value, 0)
	if err != nil {
		return errResult(L, fmt.Errorf("kbrd.json.decode: %w", err))
	}
	L.Push(decoded)
	return 1
}

func (h *Host) luaJSONArray(L *lua.LState) int {
	t := L.OptTable(1, L.NewTable())
	L.SetMetatable(t, h.jsonArrayMeta)
	L.Push(t)
	return 1
}

func (h *Host) luaJSONObject(L *lua.LState) int {
	t := L.OptTable(1, L.NewTable())
	L.SetMetatable(t, h.jsonObjectMeta)
	L.Push(t)
	return 1
}

func (h *Host) luaToJSON(v lua.LValue, visiting map[*lua.LTable]bool, depth int) (any, error) {
	if depth > maxJSONDepth {
		return nil, fmt.Errorf("maximum nesting depth of %d exceeded", maxJSONDepth)
	}
	switch x := v.(type) {
	case *lua.LNilType:
		return nil, nil
	case lua.LBool:
		return bool(x), nil
	case lua.LString:
		return string(x), nil
	case lua.LNumber:
		n := float64(x)
		if math.IsNaN(n) || math.IsInf(n, 0) {
			return nil, fmt.Errorf("cannot encode non-finite number %s", x.String())
		}
		return n, nil
	case *lua.LUserData:
		if x == h.jsonNull {
			return nil, nil
		}
		return nil, fmt.Errorf("unsupported userdata value")
	case *lua.LTable:
		if visiting[x] {
			return nil, fmt.Errorf("cyclic table")
		}
		visiting[x] = true
		defer delete(visiting, x)
		return h.luaTableToJSON(x, visiting, depth+1)
	default:
		return nil, fmt.Errorf("unsupported %s value", v.Type())
	}
}

func (h *Host) luaTableToJSON(t *lua.LTable, visiting map[*lua.LTable]bool, depth int) (any, error) {
	meta := h.L.GetMetatable(t)
	forceArray := meta == h.jsonArrayMeta
	forceObject := meta == h.jsonObjectMeta

	count, maxIndex := 0, 0
	arrayKeys, stringKeys := true, true
	t.ForEach(func(k, _ lua.LValue) {
		count++
		if n, ok := exactPositiveIndex(k); ok {
			maxIndex = max(maxIndex, n)
		} else {
			arrayKeys = false
		}
		if _, ok := k.(lua.LString); !ok {
			stringKeys = false
		}
	})

	isArray := forceArray || (!forceObject && count > 0 && arrayKeys && maxIndex == count)
	if isArray {
		if !arrayKeys || maxIndex != count {
			return nil, fmt.Errorf("JSON array must use contiguous integer keys starting at 1")
		}
		out := make([]any, count)
		for i := 1; i <= count; i++ {
			value, err := h.luaToJSON(t.RawGetInt(i), visiting, depth)
			if err != nil {
				return nil, fmt.Errorf("array index %d: %w", i, err)
			}
			out[i-1] = value
		}
		return out, nil
	}
	if !stringKeys {
		return nil, fmt.Errorf("JSON object must use string keys; mixed and sparse tables are unsupported")
	}
	out := make(map[string]any, count)
	var conversionErr error
	t.ForEach(func(k, v lua.LValue) {
		if conversionErr != nil {
			return
		}
		key := string(k.(lua.LString))
		value, err := h.luaToJSON(v, visiting, depth)
		if err != nil {
			conversionErr = fmt.Errorf("object key %q: %w", key, err)
			return
		}
		out[key] = value
	})
	return out, conversionErr
}

func exactPositiveIndex(v lua.LValue) (int, bool) {
	n, ok := v.(lua.LNumber)
	if !ok || n < 1 || float64(n) != math.Trunc(float64(n)) || float64(n) > float64(math.MaxInt) {
		return 0, false
	}
	return int(n), true
}

func decodeJSON(body []byte) (any, error) {
	dec := json.NewDecoder(bytes.NewReader(body))
	dec.UseNumber()
	var value any
	if err := dec.Decode(&value); err != nil {
		return nil, err
	}
	if err := validateJSONDepth(value, 0); err != nil {
		return nil, err
	}
	var trailing any
	if err := dec.Decode(&trailing); err != io.EOF {
		if err == nil {
			return nil, fmt.Errorf("multiple JSON values")
		}
		return nil, fmt.Errorf("trailing data: %w", err)
	}
	return value, nil
}

func validateJSONDepth(v any, depth int) error {
	if depth > maxJSONDepth {
		return fmt.Errorf("maximum nesting depth of %d exceeded", maxJSONDepth)
	}
	switch x := v.(type) {
	case json.Number:
		if _, err := jsonNumberToLua(x); err != nil {
			return err
		}
	case []any:
		for _, value := range x {
			if err := validateJSONDepth(value, depth+1); err != nil {
				return err
			}
		}
	case map[string]any:
		for _, value := range x {
			if err := validateJSONDepth(value, depth+1); err != nil {
				return err
			}
		}
	}
	return nil
}

func jsonNumberToLua(n json.Number) (lua.LNumber, error) {
	value, err := strconv.ParseFloat(string(n), 64)
	if err != nil {
		return 0, fmt.Errorf("number %q is outside Lua numeric range: %w", n, err)
	}
	return lua.LNumber(value), nil
}

func (h *Host) jsonToLua(v any, depth int) (lua.LValue, error) {
	if depth > maxJSONDepth {
		return lua.LNil, fmt.Errorf("maximum nesting depth of %d exceeded", maxJSONDepth)
	}
	switch x := v.(type) {
	case nil:
		return h.jsonNull, nil
	case bool:
		return lua.LBool(x), nil
	case string:
		return lua.LString(x), nil
	case json.Number:
		return jsonNumberToLua(x)
	case []any:
		t := h.L.NewTable()
		h.L.SetMetatable(t, h.jsonArrayMeta)
		for i, value := range x {
			converted, err := h.jsonToLua(value, depth+1)
			if err != nil {
				return lua.LNil, fmt.Errorf("array index %d: %w", i+1, err)
			}
			t.RawSetInt(i+1, converted)
		}
		return t, nil
	case map[string]any:
		t := h.L.NewTable()
		h.L.SetMetatable(t, h.jsonObjectMeta)
		for key, value := range x {
			converted, err := h.jsonToLua(value, depth+1)
			if err != nil {
				return lua.LNil, fmt.Errorf("object key %q: %w", key, err)
			}
			t.RawSetString(key, converted)
		}
		return t, nil
	default:
		return lua.LNil, fmt.Errorf("unsupported decoded JSON value %T", v)
	}
}
