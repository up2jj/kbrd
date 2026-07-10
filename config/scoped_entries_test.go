package config

import "testing"

type scopedTestEntry struct {
	ID   string
	Name string
}

func TestLoadScopedEntriesOverridesAndWarns(t *testing.T) {
	entries := map[string][]scopedTestEntry{
		"global/global.yml": {{ID: "shared", Name: "global shared"}, {ID: "global", Name: "global only"}},
		"board/local.yml":   {{ID: "shared", Name: "local shared"}, {ID: "dup", Name: "first"}, {ID: "dup", Name: "second"}},
	}
	read := func(path string) ([]scopedTestEntry, []CommandLoadWarning, error) {
		return entries[path], nil, nil
	}

	got, warnings, err := loadScopedEntries("global", "board", "global.yml", "local.yml", read,
		func(entry scopedTestEntry) string { return entry.ID },
		func(entry scopedTestEntry) string { return entry.Name },
	)
	if err != nil {
		t.Fatalf("loadScopedEntries: %v", err)
	}
	if len(got) != 4 || got[0].ID != "global" || got[1].Name != "local shared" {
		t.Fatalf("merged entries = %+v", got)
	}
	if len(warnings) != 1 || warnings[0].Source != "local.yml" || warnings[0].Message != `duplicate id "dup": "second" shadowed by "first"` {
		t.Fatalf("warnings = %+v", warnings)
	}
}
