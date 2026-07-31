package frontmatter

import "testing"

func TestSplitExact(t *testing.T) {
	tests := []struct {
		name                     string
		raw, prefix, block, body string
		fenced, apparent         bool
	}{
		{name: "plain", raw: "hello\n---\n", body: "hello\n---\n"},
		{name: "lf", raw: "---\n# c\na: 1\n---\nbody\n", prefix: "---\n# c\na: 1\n---\n", block: "# c\na: 1\n", body: "body\n", fenced: true, apparent: true},
		{name: "crlf and dots", raw: "---\r\na: 1\r\n...\r\nbody\r\n", prefix: "---\r\na: 1\r\n...\r\n", block: "a: 1\r\n", body: "body\r\n", fenced: true, apparent: true},
		{name: "unterminated", raw: "---\nsecret: yes\nbody", body: "---\nsecret: yes\nbody", apparent: true},
		{name: "opening without newline", raw: "---", body: "---", apparent: true},
		{name: "closing without newline", raw: "---\na: 1\n---", body: "---\na: 1\n---", apparent: true},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			prefix, block, body, fenced, apparent := SplitExact(tt.raw)
			if prefix != tt.prefix || block != tt.block || body != tt.body || fenced != tt.fenced || apparent != tt.apparent {
				t.Fatalf("SplitExact() = (%q, %q, %q, %v, %v)", prefix, block, body, fenced, apparent)
			}
			oldBlock, oldBody, oldFenced := Split(tt.raw)
			if oldBlock != block || oldBody != body || oldFenced != fenced {
				t.Fatalf("Split disagrees: (%q, %q, %v)", oldBlock, oldBody, oldFenced)
			}
		})
	}
}
