package frontmatter

import (
	"strings"
	"testing"
)

func TestApplyPatchPreservesBodyAndMutatesTopLevelKeys(t *testing.T) {
	raw := "---\nstatus: todo\ntags: [bug]\nnotes:\n  - keep\n---\n\n# Card\n"
	got, err := Apply(raw, Patch{
		Set: map[string]string{
			"status": "doing",
			"tags":   "[bug, urgent]",
			"owner":  "alice",
		},
		Unset: []string{"notes"},
	})
	if err != nil {
		t.Fatalf("Apply: %v", err)
	}
	want := "---\nstatus: doing\ntags: [bug, urgent]\nowner: alice\n---\n\n# Card\n"
	if got != want {
		t.Fatalf("Apply = %q, want %q", got, want)
	}
}

func TestApplyPatchCreatesFrontmatterWhenSettingOnBody(t *testing.T) {
	got, err := Apply("# Card\n", Patch{Set: map[string]string{"status": "doing"}})
	if err != nil {
		t.Fatalf("Apply: %v", err)
	}
	want := "---\nstatus: doing\n---\n\n# Card\n"
	if got != want {
		t.Fatalf("Apply = %q, want %q", got, want)
	}
}

func TestApplyPatchNoOpUnsetDoesNotRewrite(t *testing.T) {
	raw := "---\nstatus: todo\n---\nbody\n"
	got, err := Apply(raw, Patch{Unset: []string{"missing"}})
	if err != nil {
		t.Fatalf("Apply: %v", err)
	}
	if got != raw {
		t.Fatalf("Apply changed a no-op patch: %q", got)
	}
}

func TestApplyPatchReplacesMultilineValue(t *testing.T) {
	raw := "---\ntags:\n  - old\n  - value\nstatus: todo\n---\nbody\n"
	got, err := Apply(raw, Patch{Set: map[string]string{"tags": "[new]"}})
	if err != nil {
		t.Fatalf("Apply: %v", err)
	}
	want := "---\ntags: [new]\nstatus: todo\n---\nbody\n"
	if got != want {
		t.Fatalf("Apply = %q, want %q", got, want)
	}
}

func TestApplyPatchRejectsMalformedFrontmatter(t *testing.T) {
	_, err := Apply("---\nstatus: [broken\nbody\n", Patch{Set: map[string]string{"owner": "alice"}})
	if err == nil || !strings.Contains(err.Error(), "unterminated") {
		t.Fatalf("Apply error = %v, want unterminated frontmatter error", err)
	}

	_, err = Apply("---\nstatus: [broken\n---\nbody\n", Patch{Set: map[string]string{"owner": "alice"}})
	if err == nil || !strings.Contains(err.Error(), "resulting frontmatter is invalid") {
		t.Fatalf("Apply error = %v, want invalid YAML error", err)
	}
}

func TestEncodeValue(t *testing.T) {
	for _, test := range []struct {
		name string
		in   any
		want string
	}{
		{name: "scalar", in: "doing", want: "doing"},
		{name: "string with colon", in: "owner: alice", want: "'owner: alice'"},
		{name: "sequence", in: []any{"bug", "urgent"}, want: "[bug, urgent]"},
		{name: "boolean", in: true, want: "true"},
	} {
		t.Run(test.name, func(t *testing.T) {
			got, err := EncodeValue(test.in)
			if err != nil {
				t.Fatalf("EncodeValue: %v", err)
			}
			if got != test.want {
				t.Fatalf("EncodeValue(%#v) = %q, want %q", test.in, got, test.want)
			}
		})
	}
}
