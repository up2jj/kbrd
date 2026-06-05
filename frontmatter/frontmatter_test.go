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
