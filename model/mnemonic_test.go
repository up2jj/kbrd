package model

import (
	"strings"
	"testing"
)

func TestGenerateMnemonics(t *testing.T) {
	cases := []int{0, 1, 5, 9, 10, 17, 50, 81, 90}
	for _, n := range cases {
		got := GenerateMnemonics(n)
		if len(got) != n {
			t.Fatalf("n=%d: got %d tags, want %d", n, len(got), n)
		}
		seen := map[string]bool{}
		for _, tag := range got {
			if tag == "" {
				t.Fatalf("n=%d: empty tag", n)
			}
			if seen[tag] {
				t.Fatalf("n=%d: duplicate tag %q", n, tag)
			}
			seen[tag] = true
		}
		// Prefix-free check: no tag is a strict prefix of any other.
		for a := range seen {
			for b := range seen {
				if a == b {
					continue
				}
				if strings.HasPrefix(b, a) {
					t.Fatalf("n=%d: tag %q is a prefix of %q", n, a, b)
				}
			}
		}
	}
}
