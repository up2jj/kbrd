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
			want: `field "choices" item 2 must be a string`,
		},
		{
			name: "choice sequence",
			body: `kbrd.command("x", "Bad", function()
			  coroutine.yield({_uiReq=true, kind="pick", title="Pick", choices={[2]="gap"}})
			end)`,
			want: `field "choices" must be a contiguous sequence`,
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

func TestUIStructuredResultConversion(t *testing.T) {
	dir := writeInit(t, `
kbrd.command("x", "Structured", function()
  local result = coroutine.yield({_uiReq=true, kind="pick", title="Pick", choices={"a"}})
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
