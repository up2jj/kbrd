package script

import (
	"strings"
	"testing"
)

func TestUIRequestValidationErrors(t *testing.T) {
	tests := []struct {
		name string
		body string
		want string
	}{
		{
			name: "choice type",
			body: `kbrd.command("x", "Bad", function() kbrd.ui.pick("Pick", {"ok", 2}) end)`,
			want: `field "label" must be a string`,
		},
		{
			name: "item sequence",
			body: `kbrd.command("x", "Bad", function()
			  coroutine.yield({_uiReq=true, kind="select", title="Pick", items={[2]={id="x", label="gap"}}})
			end)`,
			want: `field "items" must be a contiguous sequence`,
		},
		{
			name: "legacy choice sequence",
			body: `kbrd.command("x", "Bad", function()
			  kbrd.ui.pick("Pick", {[1]="one", [3]="gap"})
			end)`,
			want: `choices must be a contiguous sequence`,
		},
		{
			name: "unknown kind",
			body: `kbrd.command("x", "Bad", function()
			  coroutine.yield({_uiReq=true, kind="mystery", title="Nope"})
			end)`,
			want: `unsupported kbrd.ui kind "mystery"`,
		},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			dir := writeInit(t, tt.body)
			h, err := New(defaultCfg(), &fakeAPI{}, nil, dir, "")
			if err != nil {
				t.Fatalf("load: %v", err)
			}
			defer h.Close()
			req, err := h.RunCommand(h.Commands()[0].LuaRef, nil)
			if req != nil || err == nil || !strings.Contains(err.Error(), tt.want) {
				t.Fatalf("RunCommand = (%+v, %v), want error containing %q", req, err, tt.want)
			}
		})
	}
}

func TestLegacyPromptPreservesCharacterLimit(t *testing.T) {
	dir := writeInit(t, `kbrd.command("x", "Prompt", function()
	  kbrd.ui.prompt("Name", "default")
	end)`)
	h, err := New(defaultCfg(), &fakeAPI{}, nil, dir, "")
	if err != nil {
		t.Fatalf("load: %v", err)
	}
	defer h.Close()
	req, err := h.RunCommand(h.Commands()[0].LuaRef, nil)
	if err != nil || req == nil {
		t.Fatalf("RunCommand = (%+v, %v)", req, err)
	}
	if req.Kind != UIKindInput || req.Spec.MaxLength != 256 {
		t.Fatalf("legacy prompt request = %+v", req)
	}
}

func TestUIStructuredResultConversion(t *testing.T) {
	dir := writeInit(t, `
kbrd.command("x", "Structured", function()
  local result = coroutine.yield({_uiReq=true, kind="select", title="Pick", items={{id="a", label="A"}}})
  kbrd.notify(tostring(result.submitted) .. ":" .. result.action .. ":" .. result.value)
end)`)
	api := &fakeAPI{}
	h, err := New(defaultCfg(), api, nil, dir, "")
	if err != nil {
		t.Fatalf("load: %v", err)
	}
	defer h.Close()
	req, err := h.RunCommand(h.Commands()[0].LuaRef, nil)
	if err != nil || req == nil {
		t.Fatalf("run = (%+v, %v)", req, err)
	}
	if _, err := h.ResumeWith(req.Token, UIResult{Submitted: true, Action: "submit", Value: "a"}); err != nil {
		t.Fatalf("resume: %v", err)
	}
	if len(api.notifies) != 1 || !strings.Contains(api.notifies[0], "true:submit:a") {
		t.Fatalf("notifications = %v", api.notifies)
	}
}

func TestUIRequestExclusivityAndCancellation(t *testing.T) {
	dir := writeInit(t, `
kbrd.command("x", "First", function() kbrd.ui.prompt("One", "") end)
kbrd.command("y", "Second", function() kbrd.ui.prompt("Two", "") end)`)
	h, err := New(defaultCfg(), &fakeAPI{}, nil, dir, "")
	if err != nil {
		t.Fatalf("load: %v", err)
	}
	defer h.Close()
	first, err := h.RunCommand(h.Commands()[0].LuaRef, nil)
	if err != nil || first == nil {
		t.Fatalf("first = (%+v, %v)", first, err)
	}
	if req, err := h.RunCommand(h.Commands()[1].LuaRef, nil); req != nil || err == nil {
		t.Fatalf("second = (%+v, %v), want active-request error", req, err)
	}
	h.CancelPending()
	if req, err := h.ResumeWith(first.Token, UIResult{Cancelled: true}); req != nil || err == nil {
		t.Fatalf("stale resume = (%+v, %v), want unknown token", req, err)
	}
	second, err := h.RunCommand(h.Commands()[1].LuaRef, nil)
	if err != nil || second == nil {
		t.Fatalf("second after cancel = (%+v, %v)", second, err)
	}
	if first.Token == second.Token {
		t.Fatalf("tokens reused after cancellation: %q", first.Token)
	}
}

func TestUIErrorAfterResumeLeavesHostUsable(t *testing.T) {
	dir := writeInit(t, `
kbrd.command("x", "Fails", function()
  kbrd.ui.prompt("Continue", "")
  error("boom after resume")
end)
kbrd.command("y", "Next", function()
  kbrd.notify("recovered")
end)`)
	api := &fakeAPI{}
	h, err := New(defaultCfg(), api, nil, dir, "")
	if err != nil {
		t.Fatalf("load: %v", err)
	}
	defer h.Close()

	req, err := h.RunCommand(h.Commands()[0].LuaRef, nil)
	if err != nil || req == nil {
		t.Fatalf("run = (%+v, %v), want UI request", req, err)
	}
	if next, err := h.ResumeWith(req.Token, UIResult{Submitted: true, Action: "submit", Value: "yes"}); next != nil || err == nil || !strings.Contains(err.Error(), "boom after resume") {
		t.Fatalf("resume = (%+v, %v), want post-resume script error", next, err)
	}

	if req, err := h.RunCommand(h.Commands()[1].LuaRef, nil); err != nil || req != nil {
		t.Fatalf("next command = (%+v, %v), want successful completion", req, err)
	}
	if !contains(api.notifies, "recovered") {
		t.Fatalf("next command did not run after resume error: %v", api.notifies)
	}
}

func TestUITableWidgetsDecodeTypedSpecs(t *testing.T) {
	dir := writeInit(t, `
kbrd.command("input", "Input", function()
  kbrd.ui.input({title="Rename", label="Name", initial="old", placeholder="new", required=true,
    min_length=2, max_length=8, pattern="^[a-z]+$", pattern_hint="Lowercase only"})
end)
kbrd.command("select", "Select", function()
  kbrd.ui.select({title="Column", searchable=true, initial_id="doing", items={
    {id="todo", label="Todo", description="Backlog", icon="○", group="Board"},
    {id="doing", label="Doing", disabled=true, disabled_reason="Full", group="Board"},
  }})
end)
kbrd.command("confirm", "Confirm", function()
  kbrd.ui.confirm({title="Delete", message="Delete card?", detail={"Cannot be undone"},
    confirm_label="Delete", reject_label="Keep", default=false, destructive=true})
end)
kbrd.command("actions", "Actions", function()
  kbrd.ui.actions({title="Choose", actions={{id="save", label="Save", key="ctrl+s", primary=true}}})
end)`)
	h, err := New(defaultCfg(), &fakeAPI{}, nil, dir, "")
	if err != nil {
		t.Fatalf("load: %v", err)
	}
	defer h.Close()

	commands := h.Commands()
	input, err := h.RunCommand(commands[0].LuaRef, nil)
	if err != nil || input.Kind != UIKindInput || input.Spec.Initial != "old" || input.Spec.MinLength != 2 || input.Spec.PatternHint != "Lowercase only" {
		t.Fatalf("input request = (%+v, %v)", input, err)
	}
	h.CancelPending()
	selectReq, err := h.RunCommand(commands[1].LuaRef, nil)
	if err != nil || selectReq.Kind != UIKindSelect || !selectReq.Spec.Searchable || selectReq.Spec.Items[1].DisabledReason != "Full" {
		t.Fatalf("select request = (%+v, %v)", selectReq, err)
	}
	h.CancelPending()
	confirm, err := h.RunCommand(commands[2].LuaRef, nil)
	if err != nil || confirm.Kind != UIKindConfirm || !confirm.Spec.Destructive || confirm.Spec.ConfirmLabel != "Delete" {
		t.Fatalf("confirm request = (%+v, %v)", confirm, err)
	}
	h.CancelPending()
	actions, err := h.RunCommand(commands[3].LuaRef, nil)
	if err != nil || actions.Kind != UIKindActions || actions.Spec.Actions[0].Key != "ctrl+s" {
		t.Fatalf("actions request = (%+v, %v)", actions, err)
	}
}

func TestUITableWidgetValidationErrors(t *testing.T) {
	tests := []struct {
		name string
		body string
		want string
	}{
		{"duplicate item id", `kbrd.command("x", "x", function() kbrd.ui.select({items={{id="x",label="X"},{id="x",label="Y"}}}) end)`, `duplicate id "x"`},
		{"missing initial item", `kbrd.command("x", "x", function() kbrd.ui.select({initial_id="x",items={}}) end)`, `initial_id "x" does not match`},
		{"invalid pattern", `kbrd.command("x", "x", function() kbrd.ui.input({pattern="["}) end)`, `pattern is not valid RE2`},
		{"invalid range", `kbrd.command("x", "x", function() kbrd.ui.input({min_length=4,max_length=2}) end)`, `min_length must not exceed`},
		{"reserved action key", `kbrd.command("x", "x", function() kbrd.ui.actions({actions={{id="x",label="X",key="esc"}}}) end)`, `key "esc" is reserved`},
		{"duplicate action key", `kbrd.command("x", "x", function() kbrd.ui.actions({actions={{id="x",label="X",key="ctrl+x"},{id="y",label="Y",key="CTRL+X"}}}) end)`, `used by both`},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			dir := writeInit(t, tt.body)
			h, err := New(defaultCfg(), &fakeAPI{}, nil, dir, "")
			if err != nil {
				t.Fatalf("load: %v", err)
			}
			defer h.Close()
			req, err := h.RunCommand(h.Commands()[0].LuaRef, nil)
			if req != nil || err == nil || !strings.Contains(err.Error(), tt.want) {
				t.Fatalf("RunCommand = (%+v, %v), want %q", req, err, tt.want)
			}
		})
	}
}

func TestUINotifyIsNonBlocking(t *testing.T) {
	dir := writeInit(t, `kbrd.command("n", "Notify", function()
  local value = kbrd.ui.notify({message="Saved", level="success"})
  if value ~= nil then error("notify returned a value") end
end)`)
	api := &fakeAPI{}
	h, err := New(defaultCfg(), api, nil, dir, "")
	if err != nil {
		t.Fatalf("load: %v", err)
	}
	defer h.Close()
	if req, err := h.RunCommand(h.Commands()[0].LuaRef, nil); req != nil || err != nil {
		t.Fatalf("RunCommand = (%+v, %v), want immediate completion", req, err)
	}
	if len(api.notifies) != 1 || !strings.Contains(api.notifies[0], "success:Saved") {
		t.Fatalf("notifications = %v", api.notifies)
	}
}
