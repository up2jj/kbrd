package script

import (
	"fmt"
	"regexp"
	"strings"

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

	kindValue, err := uiString(t, "kind", true)
	if err != nil {
		return nil, true, err
	}
	req := &UIRequest{Kind: UIKind(kindValue)}
	if err := decodeCommonSpec(t, &req.Spec); err != nil {
		return nil, true, err
	}

	switch req.Kind {
	case UIKindInput:
		err = decodeInputSpec(t, &req.Spec)
	case UIKindSelect:
		err = decodeSelectSpec(t, &req.Spec)
	case UIKindConfirm:
		err = decodeConfirmSpec(t, &req.Spec)
	case UIKindActions:
		err = decodeActionsSpec(t, &req.Spec)
	default:
		err = req.validate()
	}
	if err != nil {
		return nil, true, err
	}
	return req, true, nil
}

func decodeCommonSpec(t *lua.LTable, spec *UISpec) error {
	var err error
	if spec.Title, err = uiString(t, "title", false); err != nil {
		return err
	}
	if spec.Label, err = uiString(t, "label", false); err != nil {
		return err
	}
	return nil
}

func decodeInputSpec(t *lua.LTable, spec *UISpec) error {
	initial, err := uiString(t, "initial", false)
	if err != nil {
		return err
	}
	spec.Initial = initial
	if spec.Placeholder, err = uiString(t, "placeholder", false); err != nil {
		return err
	}
	if spec.Required, err = uiBool(t, "required", false); err != nil {
		return err
	}
	if spec.MinLength, err = uiNonNegativeInt(t, "min_length"); err != nil {
		return err
	}
	if spec.MaxLength, err = uiNonNegativeInt(t, "max_length"); err != nil {
		return err
	}
	if spec.MaxLength > 0 && spec.MinLength > spec.MaxLength {
		return fmt.Errorf("kbrd.ui input min_length must not exceed max_length")
	}
	if spec.Pattern, err = uiString(t, "pattern", false); err != nil {
		return err
	}
	if spec.PatternHint, err = uiString(t, "pattern_hint", false); err != nil {
		return err
	}
	if spec.Pattern != "" {
		if _, err := regexp.Compile(spec.Pattern); err != nil {
			return fmt.Errorf("kbrd.ui input pattern is not valid RE2: %w", err)
		}
	}
	return nil
}

func decodeSelectSpec(t *lua.LTable, spec *UISpec) error {
	var err error
	if spec.Items, err = uiItems(t, "items"); err != nil {
		return err
	}
	if spec.Searchable, err = uiBool(t, "searchable", false); err != nil {
		return err
	}
	if spec.InitialID, err = uiString(t, "initial_id", false); err != nil {
		return err
	}
	if spec.InitialID != "" && !hasItemID(spec.Items, spec.InitialID) {
		return fmt.Errorf("kbrd.ui select initial_id %q does not match an item", spec.InitialID)
	}
	return nil
}

func decodeConfirmSpec(t *lua.LTable, spec *UISpec) error {
	var err error
	if spec.Message, err = uiString(t, "message", false); err != nil {
		return err
	}
	if spec.Detail, err = uiStringList(t, "detail"); err != nil {
		return err
	}
	if spec.ConfirmLabel, err = uiString(t, "confirm_label", false); err != nil {
		return err
	}
	if spec.RejectLabel, err = uiString(t, "reject_label", false); err != nil {
		return err
	}
	if spec.Default, err = uiBool(t, "default", false); err != nil {
		return err
	}
	if spec.Destructive, err = uiBool(t, "destructive", false); err != nil {
		return err
	}
	return nil
}

func decodeActionsSpec(t *lua.LTable, spec *UISpec) error {
	actions, err := uiActions(t, "actions")
	if err != nil {
		return err
	}
	spec.Actions = actions
	return nil
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

func uiBool(t *lua.LTable, key string, def bool) (bool, error) {
	v := t.RawGetString(key)
	if v == lua.LNil {
		return def, nil
	}
	b, ok := v.(lua.LBool)
	if !ok {
		return false, fmt.Errorf("kbrd.ui request field %q must be a boolean, got %s", key, v.Type())
	}
	return bool(b), nil
}

func uiNonNegativeInt(t *lua.LTable, key string) (int, error) {
	v := t.RawGetString(key)
	if v == lua.LNil {
		return 0, nil
	}
	n, ok := v.(lua.LNumber)
	value := int(n)
	if !ok || float64(n) != float64(value) || value < 0 {
		return 0, fmt.Errorf("kbrd.ui request field %q must be a non-negative integer", key)
	}
	return value, nil
}

func uiStringList(t *lua.LTable, key string) ([]string, error) {
	return uiSequence(t, key, func(index int, value lua.LValue) (string, error) {
		s, ok := value.(lua.LString)
		if !ok {
			return "", fmt.Errorf("kbrd.ui request field %q item %d must be a string, got %s", key, index, value.Type())
		}
		return string(s), nil
	})
}

func uiItems(t *lua.LTable, key string) ([]UIItem, error) {
	items, err := uiSequence(t, key, func(index int, value lua.LValue) (UIItem, error) {
		row, ok := value.(*lua.LTable)
		if !ok {
			return UIItem{}, fmt.Errorf("kbrd.ui request field %q item %d must be a table, got %s", key, index, value.Type())
		}
		id, err := uiString(row, "id", true)
		if err != nil {
			return UIItem{}, fmt.Errorf("%s item %d: %w", key, index, err)
		}
		label, err := uiString(row, "label", true)
		if err != nil {
			return UIItem{}, fmt.Errorf("%s item %d: %w", key, index, err)
		}
		description, err := uiString(row, "description", false)
		if err != nil {
			return UIItem{}, err
		}
		icon, err := uiString(row, "icon", false)
		if err != nil {
			return UIItem{}, err
		}
		disabled, err := uiBool(row, "disabled", false)
		if err != nil {
			return UIItem{}, err
		}
		disabledReason, err := uiString(row, "disabled_reason", false)
		if err != nil {
			return UIItem{}, err
		}
		group, err := uiString(row, "group", false)
		if err != nil {
			return UIItem{}, err
		}
		return UIItem{ID: id, Label: label, Description: description, Icon: icon, Disabled: disabled, DisabledReason: disabledReason, Group: group}, nil
	})
	if err != nil {
		return nil, err
	}
	if err := uniqueIDs(key, len(items), func(i int) string { return items[i].ID }); err != nil {
		return nil, err
	}
	return items, nil
}

func uiActions(t *lua.LTable, key string) ([]UIAction, error) {
	actions, err := uiSequence(t, key, func(index int, value lua.LValue) (UIAction, error) {
		row, ok := value.(*lua.LTable)
		if !ok {
			return UIAction{}, fmt.Errorf("kbrd.ui request field %q item %d must be a table, got %s", key, index, value.Type())
		}
		id, err := uiString(row, "id", true)
		if err != nil {
			return UIAction{}, fmt.Errorf("%s item %d: %w", key, index, err)
		}
		label, err := uiString(row, "label", true)
		if err != nil {
			return UIAction{}, fmt.Errorf("%s item %d: %w", key, index, err)
		}
		shortcut, err := uiString(row, "key", false)
		if err != nil {
			return UIAction{}, err
		}
		if isReservedActionKey(shortcut) {
			return UIAction{}, fmt.Errorf("kbrd.ui actions key %q is reserved for navigation, submission, or cancellation", shortcut)
		}
		primary, err := uiBool(row, "primary", false)
		if err != nil {
			return UIAction{}, err
		}
		destructive, err := uiBool(row, "destructive", false)
		if err != nil {
			return UIAction{}, err
		}
		disabled, err := uiBool(row, "disabled", false)
		if err != nil {
			return UIAction{}, err
		}
		disabledReason, err := uiString(row, "disabled_reason", false)
		if err != nil {
			return UIAction{}, err
		}
		return UIAction{ID: id, Label: label, Key: shortcut, Primary: primary, Destructive: destructive, Disabled: disabled, DisabledReason: disabledReason}, nil
	})
	if err != nil {
		return nil, err
	}
	if err := uniqueIDs(key, len(actions), func(i int) string { return actions[i].ID }); err != nil {
		return nil, err
	}
	keys := make(map[string]string)
	for _, action := range actions {
		key := strings.ToLower(strings.TrimSpace(action.Key))
		if key == "" {
			continue
		}
		if prior, ok := keys[key]; ok {
			return nil, fmt.Errorf("kbrd.ui actions key %q is used by both %q and %q", action.Key, prior, action.ID)
		}
		keys[key] = action.ID
	}
	return actions, nil
}

func uiSequence[T any](t *lua.LTable, key string, decode func(int, lua.LValue) (T, error)) ([]T, error) {
	v := t.RawGetString(key)
	if v == lua.LNil {
		return nil, nil
	}
	table, ok := v.(*lua.LTable)
	if !ok {
		return nil, fmt.Errorf("kbrd.ui request field %q must be a table, got %s", key, v.Type())
	}
	values := make(map[int]T)
	maxIndex := 0
	var decodeErr error
	table.ForEach(func(k, v lua.LValue) {
		if decodeErr != nil {
			return
		}
		n, ok := k.(lua.LNumber)
		index := int(n)
		if !ok || float64(n) != float64(index) || index < 1 {
			decodeErr = fmt.Errorf("kbrd.ui request field %q must be a sequence", key)
			return
		}
		value, err := decode(index, v)
		if err != nil {
			decodeErr = err
			return
		}
		values[index] = value
		maxIndex = max(maxIndex, index)
	})
	if decodeErr != nil {
		return nil, decodeErr
	}
	if len(values) != maxIndex {
		return nil, fmt.Errorf("kbrd.ui request field %q must be a contiguous sequence", key)
	}
	out := make([]T, maxIndex)
	for i := 1; i <= maxIndex; i++ {
		out[i-1] = values[i]
	}
	return out, nil
}

func uniqueIDs(kind string, count int, id func(int) string) error {
	seen := make(map[string]struct{}, count)
	for i := range count {
		value := id(i)
		if _, ok := seen[value]; ok {
			return fmt.Errorf("kbrd.ui %s contains duplicate id %q", kind, value)
		}
		seen[value] = struct{}{}
	}
	return nil
}

func hasItemID(items []UIItem, id string) bool {
	for _, item := range items {
		if item.ID == id {
			return true
		}
	}
	return false
}

func isReservedActionKey(value string) bool {
	switch strings.ToLower(strings.TrimSpace(value)) {
	case "esc", "escape", "enter", "return", "up", "down", "left", "right", "j", "k", "q", "ctrl+c", "ctrl+p":
		return true
	default:
		return false
	}
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
