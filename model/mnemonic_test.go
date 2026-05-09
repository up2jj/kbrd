package model

import (
	"fmt"
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

func TestGenerateMnemonics_Boundaries(t *testing.T) {
	t.Parallel()
	// Cases close to the algorithmic boundaries: k=9, k+1, k^2, just over k^2.
	for _, n := range []int{2, 8, 9, 10, 81, 82, 100, 200} {
		t.Run(fmt.Sprintf("n=%d", n), func(t *testing.T) {
			got := GenerateMnemonics(n)
			if len(got) != n {
				t.Fatalf("len = %d, want %d", len(got), n)
			}
			seen := make(map[string]bool, n)
			for _, tag := range got {
				if tag == "" {
					t.Fatalf("empty tag in %v", got)
				}
				if seen[tag] {
					t.Fatalf("duplicate tag %q", tag)
				}
				seen[tag] = true
			}
			for a := range seen {
				for b := range seen {
					if a != b && strings.HasPrefix(b, a) {
						t.Fatalf("tag %q is prefix of %q", a, b)
					}
				}
			}
		})
	}
}

// TestGenerateMnemonics_SingleCharFirst confirms the vimium-style property:
// when capacity exceeds n, a contiguous block of high-priority items receives
// length-1 tags (so they're hit with one keystroke).
func TestGenerateMnemonics_SingleCharFirst(t *testing.T) {
	t.Parallel()
	cases := []struct {
		n              int
		minSingleChars int
	}{
		// n <= 9: every tag is single char.
		{1, 1},
		{5, 5},
		{9, 9},
		// n=10: 8 short + 1 prefix exploded into 2.
		{10, 8},
		// n=17: 8 short + 1 prefix exploded into 9.
		{17, 8},
		// n=18: 7 short + 2 prefixes splitting 11.
		{18, 7},
	}
	for _, c := range cases {
		t.Run(fmt.Sprintf("n=%d", c.n), func(t *testing.T) {
			got := GenerateMnemonics(c.n)
			singleCount := 0
			for _, tag := range got {
				if len(tag) == 1 {
					singleCount++
				} else {
					break
				}
			}
			if singleCount < c.minSingleChars {
				t.Errorf("leading single-char tags = %d, want >= %d (got %v)", singleCount, c.minSingleChars, got)
			}
		})
	}
}

func TestGenerateMnemonics_ZeroAndNegative(t *testing.T) {
	t.Parallel()
	if got := GenerateMnemonics(0); got != nil {
		t.Errorf("GenerateMnemonics(0) = %v, want nil", got)
	}
	if got := GenerateMnemonics(-1); got != nil {
		t.Errorf("GenerateMnemonics(-1) = %v, want nil", got)
	}
}

func TestGenerateMnemonics_UsesHomeRow(t *testing.T) {
	t.Parallel()
	// Every character used must come from the home-row alphabet.
	allowed := map[byte]bool{}
	for i := 0; i < len("asdfghjkl"); i++ {
		allowed["asdfghjkl"[i]] = true
	}
	for _, tag := range GenerateMnemonics(50) {
		for i := 0; i < len(tag); i++ {
			if !allowed[tag[i]] {
				t.Errorf("tag %q contains non-home-row char %q", tag, tag[i])
			}
		}
	}
}

func TestCeilDiv(t *testing.T) {
	t.Parallel()
	cases := []struct {
		a, b, want int
	}{
		{0, 3, 0},
		{-5, 3, 0},
		{-1, 1, 0},
		{1, 3, 1},
		{3, 3, 1},
		{4, 3, 2},
		{9, 3, 3},
		{10, 3, 4},
		{1, 1, 1},
	}
	for _, c := range cases {
		t.Run(fmt.Sprintf("ceilDiv(%d,%d)", c.a, c.b), func(t *testing.T) {
			if got := ceilDiv(c.a, c.b); got != c.want {
				t.Errorf("got %d, want %d", got, c.want)
			}
		})
	}
}
