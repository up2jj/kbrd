package script

import (
	"strings"
	"testing"

	lua "github.com/yuin/gopher-lua"
)

func TestJSONEncodeDecodePreservesContainersAndNull(t *testing.T) {
	dir := writeInit(t, `
local json = require("json")
encoded, encode_err = json.encode({
  empty_object = json.object(),
  empty_array = json.array(),
  values = json.array({true, json.null, "żółw"}),
})
decoded, decode_err = json.decode(encoded)
roundtrip, roundtrip_err = json.encode(decoded)
null_matches = decoded.values[2] == json.null
`)
	h, err := New(defaultCfg(), &fakeAPI{}, nil, dir, "")
	if err != nil {
		t.Fatalf("load: %v", err)
	}
	defer h.Close()

	encoded := lua.LVAsString(h.L.GetGlobal("encoded"))
	if encoded != `{"empty_array":[],"empty_object":{},"values":[true,null,"żółw"]}` {
		t.Fatalf("encoded = %q", encoded)
	}
	if got := lua.LVAsString(h.L.GetGlobal("roundtrip")); got != encoded {
		t.Fatalf("roundtrip = %q, want %q", got, encoded)
	}
	if h.L.GetGlobal("encode_err") != lua.LNil || h.L.GetGlobal("decode_err") != lua.LNil || h.L.GetGlobal("roundtrip_err") != lua.LNil {
		t.Fatal("unexpected JSON error")
	}
	if !lua.LVAsBool(h.L.GetGlobal("null_matches")) {
		t.Fatal("decoded null must equal json.null")
	}
}

func TestJSONEncodeRejectsCyclesAndMixedKeys(t *testing.T) {
	dir := writeInit(t, `
cyclic = {}; cyclic.self = cyclic
cycle_value, cycle_err = kbrd.json.encode(cyclic)
mixed_value, mixed_err = kbrd.json.encode({[1]="a", name="b"})
`)
	h, err := New(defaultCfg(), &fakeAPI{}, nil, dir, "")
	if err != nil {
		t.Fatalf("load: %v", err)
	}
	defer h.Close()

	if h.L.GetGlobal("cycle_value") != lua.LNil || !strings.Contains(lua.LVAsString(h.L.GetGlobal("cycle_err")), "cyclic table") {
		t.Fatalf("cycle result = %v, %v", h.L.GetGlobal("cycle_value"), h.L.GetGlobal("cycle_err"))
	}
	if h.L.GetGlobal("mixed_value") != lua.LNil || !strings.Contains(lua.LVAsString(h.L.GetGlobal("mixed_err")), "mixed") {
		t.Fatalf("mixed result = %v, %v", h.L.GetGlobal("mixed_value"), h.L.GetGlobal("mixed_err"))
	}
}

func TestJSONDecodeRejectsTrailingData(t *testing.T) {
	dir := writeInit(t, `value, json_err = kbrd.json.decode("{} []")`)
	h, err := New(defaultCfg(), &fakeAPI{}, nil, dir, "")
	if err != nil {
		t.Fatalf("load: %v", err)
	}
	defer h.Close()
	if h.L.GetGlobal("value") != lua.LNil || !strings.Contains(lua.LVAsString(h.L.GetGlobal("json_err")), "multiple JSON values") {
		t.Fatalf("decode result = %v, %v", h.L.GetGlobal("value"), h.L.GetGlobal("json_err"))
	}
}

func TestJSONDecodeRejectsNumbersOutsideLuaRange(t *testing.T) {
	dir := writeInit(t, `
top_value, top_err = kbrd.json.decode("1e400")
nested_value, nested_err = kbrd.json.decode('{"number":1e400}')
`)
	h, err := New(defaultCfg(), &fakeAPI{}, nil, dir, "")
	if err != nil {
		t.Fatalf("load: %v", err)
	}
	defer h.Close()

	for _, name := range []string{"top", "nested"} {
		if h.L.GetGlobal(name+"_value") != lua.LNil {
			t.Fatalf("%s decode unexpectedly succeeded", name)
		}
		if got := lua.LVAsString(h.L.GetGlobal(name + "_err")); !strings.Contains(got, "outside Lua numeric range") {
			t.Fatalf("%s error = %q", name, got)
		}
	}
}
