package plugin

import (
	"strings"
	"testing"
)

func TestNormalizePatchPathsOnlyRewritesHeaders(t *testing.T) {
	before := "/tmp/preview-old/plugin.lua"
	after := "/tmp/preview-new/plugin.lua"
	patch := strings.Join([]string{
		"diff --git a" + before + " b" + after,
		"--- a" + before,
		"+++ b" + after,
		"@@ -1 +1 @@",
		"-return " + before,
		"+return " + after,
	}, "\n")

	got := normalizePatchPaths(patch, before, after, "plugin.lua")
	for _, want := range []string{
		"diff --git a/plugin.lua b/plugin.lua",
		"--- a/plugin.lua",
		"+++ b/plugin.lua",
		"-return " + before,
		"+return " + after,
	} {
		if !strings.Contains(got, want) {
			t.Errorf("normalized patch missing %q:\n%s", want, got)
		}
	}
	if strings.Contains(got, "aa/plugin.lua") || strings.Contains(got, "bb/plugin.lua") {
		t.Fatalf("normalized patch has duplicate prefixes:\n%s", got)
	}
}
