package script

import (
	"fmt"
	"sort"
	"strconv"
	"strings"
	"unicode/utf8"

	lua "github.com/yuin/gopher-lua"
)

const (
	inspectMaxDepth = 5
	inspectMaxItems = 100
	inspectMaxBytes = 4096
)

func (h *Host) luaDebug(L *lua.LState) int {
	parts := make([]string, 0, L.GetTop())
	for i := 1; i <= L.GetTop(); i++ {
		v := L.Get(i)
		if s, ok := v.(lua.LString); ok {
			parts = append(parts, string(s))
		} else {
			parts = append(parts, inspectLua(v))
		}
	}
	source := luaCaller(L)
	h.logger.Log("debug", source, strings.Join(parts, "\t"))
	return 0
}

func (h *Host) luaInspect(L *lua.LState) int {
	L.Push(lua.LString(inspectLua(L.CheckAny(1))))
	return 1
}

func luaCaller(L *lua.LState) string {
	for level := 1; level < 8; level++ {
		dbg, ok := L.GetStack(level)
		if !ok {
			break
		}
		if _, err := L.GetInfo("Sl", dbg, lua.LNil); err != nil || dbg.What == "G" {
			continue
		}
		source := strings.TrimPrefix(dbg.Source, "@")
		if source == "" {
			source = FolderInitFile
		}
		if dbg.CurrentLine > 0 {
			return fmt.Sprintf("%s:%d", source, dbg.CurrentLine)
		}
		return source
	}
	return FolderInitFile
}

func inspectLua(v lua.LValue) string {
	s := inspectValue(v, 0, make(map[*lua.LTable]bool), new(int))
	if len(s) > inspectMaxBytes {
		end := inspectMaxBytes
		for end > 0 && !utf8.ValidString(s[:end]) {
			end--
		}
		return s[:end] + "…"
	}
	return s
}

func inspectValue(v lua.LValue, depth int, visiting map[*lua.LTable]bool, items *int) string {
	switch v := v.(type) {
	case *lua.LNilType:
		return "nil"
	case lua.LBool:
		return strconv.FormatBool(bool(v))
	case lua.LNumber:
		return strconv.FormatFloat(float64(v), 'g', -1, 64)
	case lua.LString:
		return strconv.Quote(string(v))
	case *lua.LTable:
		if visiting[v] {
			return "<cycle>"
		}
		if depth >= inspectMaxDepth {
			return "{…}"
		}
		visiting[v] = true
		defer delete(visiting, v)
		type pair struct{ key, value string }
		pairs := make([]pair, 0, v.Len())
		truncated := false
		v.ForEach(func(k, value lua.LValue) {
			if *items >= inspectMaxItems {
				truncated = true
				return
			}
			*items++
			pairs = append(pairs, pair{
				key:   inspectValue(k, depth+1, visiting, items),
				value: inspectValue(value, depth+1, visiting, items),
			})
		})
		sort.Slice(pairs, func(i, j int) bool { return pairs[i].key < pairs[j].key })
		parts := make([]string, 0, len(pairs)+1)
		for _, p := range pairs {
			parts = append(parts, p.key+" = "+p.value)
		}
		if truncated {
			parts = append(parts, "…")
		}
		return "{" + strings.Join(parts, ", ") + "}"
	default:
		return v.String()
	}
}
