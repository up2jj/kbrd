package script

import (
	"fmt"

	lua "github.com/yuin/gopher-lua"
)

// decodeUIRequest distinguishes ordinary coroutine yields from kbrd.ui
// requests, then strictly decodes and validates the latter.
func decodeUIRequest(vals []lua.LValue) (*UIRequest, bool, error) {
	if len(vals) == 0 {
		return nil, false, nil
	}
	t, ok := vals[0].(*lua.LTable)
	if !ok || !lua.LVAsBool(t.RawGetString("_uiReq")) {
		return nil, false, nil
	}

	kind, err := uiString(t, "kind", true)
	if err != nil {
		return nil, true, err
	}
	title, err := uiString(t, "title", false)
	if err != nil {
		return nil, true, err
	}
	def, err := uiString(t, "default", false)
	if err != nil {
		return nil, true, err
	}
	choices, err := uiStringList(t, "choices")
	if err != nil {
		return nil, true, err
	}

	req := &UIRequest{
		Kind: UIKind(kind),
		Spec: UISpec{Title: title, Default: def, Choices: choices},
	}
	if err := req.validate(); err != nil {
		return nil, true, err
	}
	return req, true, nil
}

func uiString(t *lua.LTable, key string, required bool) (string, error) {
	v := t.RawGetString(key)
	if v == lua.LNil {
		if required {
			return "", fmt.Errorf("kbrd.ui request field %q is required", key)
		}
		return "", nil
	}
	s, ok := v.(lua.LString)
	if !ok {
		return "", fmt.Errorf("kbrd.ui request field %q must be a string, got %s", key, v.Type())
	}
	if required && s == "" {
		return "", fmt.Errorf("kbrd.ui request field %q must not be empty", key)
	}
	return string(s), nil
}

func uiStringList(t *lua.LTable, key string) ([]string, error) {
	v := t.RawGetString(key)
	if v == lua.LNil {
		return nil, nil
	}
	items, ok := v.(*lua.LTable)
	if !ok {
		return nil, fmt.Errorf("kbrd.ui request field %q must be a table, got %s", key, v.Type())
	}
	values := make(map[int]string)
	maxIndex := 0
	var decodeErr error
	items.ForEach(func(k, v lua.LValue) {
		if decodeErr != nil {
			return
		}
		n, ok := k.(lua.LNumber)
		index := int(n)
		if !ok || float64(n) != float64(index) || index < 1 {
			decodeErr = fmt.Errorf("kbrd.ui request field %q must be a sequence", key)
			return
		}
		s, ok := v.(lua.LString)
		if !ok {
			decodeErr = fmt.Errorf("kbrd.ui request field %q item %d must be a string, got %s", key, index, v.Type())
			return
		}
		values[index] = string(s)
		maxIndex = max(maxIndex, index)
	})
	if decodeErr != nil {
		return nil, decodeErr
	}
	if len(values) != maxIndex {
		return nil, fmt.Errorf("kbrd.ui request field %q must be a contiguous sequence", key)
	}
	out := make([]string, maxIndex)
	for i := 1; i <= maxIndex; i++ {
		out[i-1] = values[i]
	}
	return out, nil
}

func uiResultValue(L *lua.LState, result UIResult) lua.LValue {
	t := L.NewTable()
	t.RawSetString("submitted", lua.LBool(result.Submitted))
	t.RawSetString("cancelled", lua.LBool(result.Cancelled))
	if result.Action != "" {
		t.RawSetString("action", lua.LString(result.Action))
	}
	if result.Value != nil {
		t.RawSetString("value", toLValue(L, result.Value))
	}
	if result.Values != nil {
		t.RawSetString("values", toLValue(L, result.Values))
	}
	if result.IDs != nil {
		t.RawSetString("ids", toLValue(L, result.IDs))
	}
	if result.Cursor != nil {
		t.RawSetString("cursor", toLValue(L, map[string]any{
			"line": result.Cursor.Line, "column": result.Cursor.Column, "offset": result.Cursor.Offset,
		}))
	}
	if result.Selection != nil {
		t.RawSetString("selection", toLValue(L, map[string]any{
			"start_offset": result.Selection.StartOffset,
			"end_offset":   result.Selection.EndOffset,
			"text":         result.Selection.Text,
		}))
	}
	if result.Reason != "" {
		t.RawSetString("reason", lua.LString(result.Reason))
	}
	return t
}
