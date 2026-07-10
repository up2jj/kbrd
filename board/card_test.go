package board

import "testing"

func TestProjectCardContentMatchesPartsProjection(t *testing.T) {
	raw := "---\npinned: true\ntags: [home, urgent]\nicon: \"!\"\n---\n\n# Plan #\nfirst line\nsecond line\n"
	opts := CardProjectionOptions{PreviewLines: 1, TitleFromHeading: true}
	got := ProjectCardContent("plan", raw, opts)
	want := ProjectCardParts("plan", "Plan", []string{"first line"}, []byte("pinned: true\ntags: [home, urgent]\nicon: \"!\"\n"), opts)

	if got.Name != want.Name || got.Title != want.Title || got.Pinned != want.Pinned || got.Icon != want.Icon {
		t.Fatalf("projection metadata = %#v, want %#v", got, want)
	}
	if len(got.Tags) != 2 || got.Tags[0] != "home" || got.Preview[0] != "first line" {
		t.Fatalf("projection content = %#v", got)
	}
}

func TestProjectCardContentKeepsHeadingInPreviewWhenDisabled(t *testing.T) {
	got := ProjectCardContent("task", "# Visible title\nbody\n", CardProjectionOptions{PreviewLines: 2})
	if got.Title != "task" {
		t.Fatalf("title = %q, want filename", got.Title)
	}
	if len(got.Preview) != 2 || got.Preview[0] != "# Visible title" {
		t.Fatalf("preview = %#v, want heading retained", got.Preview)
	}
}
