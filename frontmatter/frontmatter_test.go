package frontmatter

import (
	"reflect"
	"strings"
	"testing"
)

func TestParse(t *testing.T) {
	t.Parallel()

	tests := []struct {
		name    string
		block   string
		want    Parsed
		wantErr bool
	}{
		{
			name:  "typed keys",
			block: "accent: \"#e06c75\"\nicon: \"🔥\"\nmeta: due tomorrow\ntags: [urgent, backend]\n",
			want: Parsed{
				Accent: "#e06c75",
				Icon:   "🔥",
				Meta:   "due tomorrow",
				Tags:   []string{"urgent", "backend"},
			},
		},
		{
			name:  "single string tag",
			block: "tags: urgent\n",
			want:  Parsed{Tags: []string{"urgent"}},
		},
		{
			name:  "render list of keys",
			block: "priority: high\nrender: [priority, assignee]\n",
			want:  Parsed{Render: []string{"priority", "assignee"}},
		},
		{
			name:  "render single key as string",
			block: "render: priority\n",
			want:  Parsed{Render: []string{"priority"}},
		},
		{
			name:  "scalar tags coerced",
			block: "tags: [1, true, x]\n",
			want:  Parsed{Tags: []string{"1", "true", "x"}},
		},
		{
			name:  "unknown keys land in Data only",
			block: "assignee: kuba\npriority: 2\n",
			want:  Parsed{},
		},
		{
			name:  "wrongly typed known keys ignored",
			block: "accent: [a, b]\nicon: 7\nmeta: {x: 1}\ntags: {a: 1}\n",
			want:  Parsed{},
		},
		{
			name:  "empty block",
			block: "",
			want:  Parsed{},
		},
		{
			name:  "comment-only block",
			block: "# just a comment\n",
			want:  Parsed{},
		},
		{
			name:    "malformed YAML",
			block:   "tags: [unclosed\n",
			wantErr: true,
		},
		{
			name:    "non-mapping top level",
			block:   "- a\n- b\n",
			wantErr: true,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()
			got, err := Parse([]byte(tt.block))
			if tt.wantErr {
				if err == nil {
					t.Fatalf("Parse(%q) error = nil, want non-nil", tt.block)
				}
				if !reflect.DeepEqual(got, Parsed{}) {
					t.Fatalf("Parse(%q) on error = %+v, want zero Parsed", tt.block, got)
				}
				return
			}
			if err != nil {
				t.Fatalf("Parse(%q) error = %v", tt.block, err)
			}
			// Compare typed fields; Data is checked separately below.
			got.Data = nil
			if !reflect.DeepEqual(got, tt.want) {
				t.Fatalf("Parse(%q) = %+v, want %+v", tt.block, got, tt.want)
			}
		})
	}

	t.Run("Data holds known and unknown keys", func(t *testing.T) {
		t.Parallel()
		got, err := Parse([]byte("accent: red\nassignee: kuba\n"))
		if err != nil {
			t.Fatal(err)
		}
		if got.Data["accent"] != "red" || got.Data["assignee"] != "kuba" {
			t.Fatalf("Data = %+v, want accent and assignee present", got.Data)
		}
	})

	t.Run("Data nil for empty block", func(t *testing.T) {
		t.Parallel()
		got, err := Parse(nil)
		if err != nil {
			t.Fatal(err)
		}
		if got.Data != nil {
			t.Fatalf("Data = %+v, want nil", got.Data)
		}
	})

	t.Run("large block within cap parses", func(t *testing.T) {
		t.Parallel()
		block := "accent: red\n" + strings.Repeat("# pad\n", 100)
		got, err := Parse([]byte(block))
		if err != nil {
			t.Fatal(err)
		}
		if got.Accent != "red" {
			t.Fatalf("Accent = %q, want red", got.Accent)
		}
	})
}

func TestBool(t *testing.T) {
	t.Parallel()
	cases := []struct {
		v    any
		want bool
	}{
		{true, true},
		{false, false},
		{"true", true},
		{"True", true},
		{"TRUE", true},
		{"yes", true},
		{"Yes", true},
		{"y", true},
		{"on", true},
		{"1", true},
		{"  yes  ", true},
		{"false", false},
		{"no", false},
		{"off", false},
		{"0", false},
		{"", false},
		{"banana", false},
		{nil, false},
		{42, false},
		{[]any{"yes"}, false},
	}
	for _, c := range cases {
		if got := Bool(c.v); got != c.want {
			t.Errorf("Bool(%#v) = %v, want %v", c.v, got, c.want)
		}
	}
}

func TestSplit(t *testing.T) {
	t.Parallel()
	cases := []struct {
		name        string
		raw         string
		block, body string
		fenced      bool
	}{
		{"simple block", "---\ntags: [a]\n---\nbody\n", "tags: [a]\n", "body\n", true},
		{"no frontmatter", "no frontmatter\n", "", "no frontmatter\n", false},
		{"unclosed", "---\nunclosed\n", "", "---\nunclosed\n", false},
		{"empty input", "", "", "", false},
		{"empty block", "---\n---\nbody", "", "body", true},
		{"dot-dot-dot close", "---\nk: v\n...\nbody", "k: v\n", "body", true},
		{"mid-body rule not hijacked", "para\n\n---\n\nmore\n", "", "para\n\n---\n\nmore\n", false},
		{"large block not capped", "---\n" + strings.Repeat("# pad\n", 5000) + "---\nb", strings.Repeat("# pad\n", 5000), "b", true},
	}
	for _, c := range cases {
		block, body, fenced := Split(c.raw)
		if block != c.block || body != c.body || fenced != c.fenced {
			t.Errorf("%s: Split(%q) = (%q, %q, %v), want (%q, %q, %v)",
				c.name, c.raw, block, body, fenced, c.block, c.body, c.fenced)
		}
	}
}

func TestSet(t *testing.T) {
	t.Parallel()
	cases := []struct {
		name string
		raw  string
		want string
	}{
		{"create block when no frontmatter", "body text\n", "---\npinned: true\n---\n\nbody text\n"},
		{"create block on empty file", "", "---\npinned: true\n---\n\n"},
		{"append to existing block", "---\naccent: red\n---\nbody\n", "---\naccent: red\npinned: true\n---\nbody\n"},
		{"replace existing value", "---\npinned: false\n---\nbody\n", "---\npinned: true\n---\nbody\n"},
		{"idempotent on already true", "---\npinned: true\n---\nbody\n", "---\npinned: true\n---\nbody\n"},
		{"unterminated falls back to new block", "---\nbroken\nbody\n", "---\npinned: true\n---\n\n---\nbroken\nbody\n"},
	}
	for _, c := range cases {
		if got := Set(c.raw, "pinned", "true"); got != c.want {
			t.Errorf("%s: Set(%q) = %q, want %q", c.name, c.raw, got, c.want)
		}
	}
}

func TestSet_PreservesSiblingsAndNesting(t *testing.T) {
	t.Parallel()
	raw := "---\n# a comment\naccent: red\ntags:\n  - urgent\n  - backend\nnested:\n  pinned: false\n---\nbody\n"
	got := Set(raw, "pinned", "true")
	// The nested pinned: and the multi-line tags: value must be untouched; the
	// new top-level key is appended; comment and order preserved.
	want := "---\n# a comment\naccent: red\ntags:\n  - urgent\n  - backend\nnested:\n  pinned: false\npinned: true\n---\nbody\n"
	if got != want {
		t.Errorf("Set = %q, want %q", got, want)
	}
	// Round-trips: the top-level key parses true, nested mapping intact.
	p, err := Parse([]byte(mustBlock(t, got)))
	if err != nil {
		t.Fatal(err)
	}
	if !Bool(p.Data["pinned"]) {
		t.Errorf("parsed pinned = %v, want true", p.Data["pinned"])
	}
}

func TestDelete(t *testing.T) {
	t.Parallel()
	cases := []struct {
		name string
		raw  string
		want string
	}{
		{"remove sole key drops block", "---\npinned: true\n---\nbody\n", "body\n"},
		{"remove key keeps other keys", "---\naccent: red\npinned: true\n---\nbody\n", "---\naccent: red\n---\nbody\n"},
		{"no key is no-op", "---\naccent: red\n---\nbody\n", "---\naccent: red\n---\nbody\n"},
		{"no block is no-op", "just body\n", "just body\n"},
		{"unterminated is no-op", "---\nbroken\nbody\n", "---\nbroken\nbody\n"},
	}
	for _, c := range cases {
		if got := Delete(c.raw, "pinned"); got != c.want {
			t.Errorf("%s: Delete(%q) = %q, want %q", c.name, c.raw, got, c.want)
		}
	}
}

func TestSetDelete_RoundTrip(t *testing.T) {
	t.Parallel()
	inputs := []string{
		"body only\n",
		"",
		"---\naccent: red\n---\nbody\n",
		"---\npinned: false\n---\nbody\n",
		"---\n# c\naccent: red\ntags:\n  - a\n---\nbody\n",
	}
	for _, raw := range inputs {
		set := Set(raw, "pinned", "true")
		p, err := Parse([]byte(mustBlock(t, set)))
		if err != nil {
			t.Fatalf("Parse after Set(%q): %v", raw, err)
		}
		if !Bool(p.Data["pinned"]) {
			t.Errorf("after Set(%q): pinned = %v, want true", raw, p.Data["pinned"])
		}
		del := Delete(set, "pinned")
		block, _, _ := Split(del)
		p2, err := Parse([]byte(block))
		if err != nil {
			t.Fatalf("Parse after Delete(%q): %v", raw, err)
		}
		if _, ok := p2.Data["pinned"]; ok {
			t.Errorf("after Delete(%q): pinned still present in %q", raw, del)
		}
	}
}

// mustBlock returns the frontmatter block of raw, failing if none is present.
func mustBlock(t *testing.T, raw string) string {
	t.Helper()
	block, _, fenced := Split(raw)
	if !fenced {
		t.Fatalf("expected a frontmatter block in %q", raw)
	}
	return block
}
