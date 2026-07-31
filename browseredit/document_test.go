package browseredit

import "testing"

func TestDocumentRoundTrip(t *testing.T) {
	tests := []struct {
		name, raw, body, replacement, want string
		frontmatter, safe                  bool
	}{
		{name: "plain", raw: "hello\n", body: "hello\n", replacement: "new\n", want: "new\n", safe: true},
		{name: "empty", raw: "", body: "", replacement: "", want: "", safe: true},
		{name: "lf exact", raw: "---\n# comment\nnested:\n  x: y\n---\nold\n", body: "old\n", replacement: "new\n", want: "---\n# comment\nnested:\n  x: y\n---\nnew\n", frontmatter: true, safe: true},
		{name: "crlf exact", raw: "---\r\n# comment\r\nunknown:  1\r\n...\r\nold\r\n", body: "old\r\n", replacement: "new\n", want: "---\r\n# comment\r\nunknown:  1\r\n...\r\nnew\n", frontmatter: true, safe: true},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			doc := ParseDocument(tt.raw)
			if doc.Body != tt.body || doc.FrontmatterPresent != tt.frontmatter || doc.WYSIWYGSafe != tt.safe {
				t.Fatalf("ParseDocument() = %+v", doc)
			}
			got, err := MergeBody(tt.raw, tt.replacement)
			if err != nil {
				t.Fatalf("MergeBody: %v", err)
			}
			if got != tt.want {
				t.Fatalf("MergeBody() = %q, want %q", got, tt.want)
			}
		})
	}
}

func TestMalformedFrontmatterFailsClosed(t *testing.T) {
	raw := "---\nsecret: true\nbody"
	doc := ParseDocument(raw)
	if doc.WYSIWYGSafe || doc.Body != "" || doc.Warning == "" {
		t.Fatalf("ParseDocument() = %+v", doc)
	}
	if _, err := MergeBody(raw, "replacement"); err == nil {
		t.Fatal("MergeBody accepted malformed metadata")
	}
}

func TestRevisionCoversEveryByte(t *testing.T) {
	if Revision("a") == Revision("a\n") {
		t.Fatal("revision ignored raw byte change")
	}
	if got := Revision("a"); len(got) != 64 {
		t.Fatalf("revision length = %d", len(got))
	}
}

func TestMergeBodyUsesCurrentFrontmatter(t *testing.T) {
	got, err := MergeBody("---\nexternal: newest\n---\ndisk\n", "mine\n")
	if err != nil {
		t.Fatal(err)
	}
	if want := "---\nexternal: newest\n---\nmine\n"; got != want {
		t.Fatalf("MergeBody() = %q, want %q", got, want)
	}
}
