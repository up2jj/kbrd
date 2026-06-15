package script

import "testing"

func TestFireFrontmatterSuggestionsNoHook(t *testing.T) {
	dir := writeInit(t, `kbrd.on("board_load", function() end)`)
	h, err := New(defaultCfg(), &fakeAPI{}, nil, dir, "")
	if err != nil {
		t.Fatalf("load: %v", err)
	}
	defer h.Close()

	res := h.FireFrontmatterSuggestions("TODO", "card")
	if res.Skipped || len(res.Suggestions) != 0 {
		t.Fatalf("expected empty result, got %+v", res)
	}
}

func TestFireFrontmatterSuggestionsMapShape(t *testing.T) {
	dir := writeInit(t, `
		kbrd.on("frontmatter_suggestions", function(ev)
			if ev.item ~= "card" then return nil end
			return { status = "todo", priority = "1" }
		end)`)
	h, err := New(defaultCfg(), &fakeAPI{}, nil, dir, "")
	if err != nil {
		t.Fatalf("load: %v", err)
	}
	defer h.Close()

	res := h.FireFrontmatterSuggestions("TODO", "card")
	got := map[string]string{}
	for _, s := range res.Suggestions {
		got[s.Key] = s.Default
	}
	if got["status"] != "todo" || got["priority"] != "1" {
		t.Fatalf("expected status=todo priority=1, got %+v", got)
	}

	// A different item: the hook declines (returns nil).
	if res := h.FireFrontmatterSuggestions("TODO", "other"); len(res.Suggestions) != 0 {
		t.Fatalf("expected no suggestions for declined item, got %+v", res.Suggestions)
	}
}

func TestFireFrontmatterSuggestionsArrayShapePreservesOrder(t *testing.T) {
	dir := writeInit(t, `
		kbrd.on("frontmatter_suggestions", function(ev)
			return {
				{ key = "status", default = "todo" },
				{ key = "owner", default = "" },
			}
		end)`)
	h, err := New(defaultCfg(), &fakeAPI{}, nil, dir, "")
	if err != nil {
		t.Fatalf("load: %v", err)
	}
	defer h.Close()

	res := h.FireFrontmatterSuggestions("TODO", "card")
	if len(res.Suggestions) != 2 {
		t.Fatalf("expected 2 suggestions, got %+v", res.Suggestions)
	}
	if res.Suggestions[0].Key != "status" || res.Suggestions[0].Default != "todo" {
		t.Fatalf("first suggestion wrong: %+v", res.Suggestions[0])
	}
	if res.Suggestions[1].Key != "owner" {
		t.Fatalf("second suggestion wrong: %+v", res.Suggestions[1])
	}
}
