package mcp

import (
	"encoding/json"
	"os"
	"path/filepath"
	"strings"
	"testing"

	sdkmcp "github.com/modelcontextprotocol/go-sdk/mcp"

	"kbrd/recents"
)

func resourceRequest(uri string) *sdkmcp.ReadResourceRequest {
	return &sdkmcp.ReadResourceRequest{Params: &sdkmcp.ReadResourceParams{URI: uri}}
}

func TestResourceURIRoundTrip(t *testing.T) {
	boardName := "Work / R&D"
	column := "1. TODO #?"
	card := "Release 100% #1?"

	boardURI := boardResourceURI(boardName)
	parts, err := parseResourceURI(boardURI, "board", 1)
	if err != nil {
		t.Fatal(err)
	}
	if len(parts) != 1 || parts[0] != boardName {
		t.Fatalf("board URI parts = %#v", parts)
	}

	cardURI := cardResourceURI(boardName, column, card)
	parts, err = parseResourceURI(cardURI, "card", 3)
	if err != nil {
		t.Fatal(err)
	}
	if parts[0] != boardName || parts[1] != column || parts[2] != card {
		t.Fatalf("card URI parts = %#v", parts)
	}

	for _, invalid := range []string{
		"kbrd://card/Work/todo",
		"kbrd://card/Work/todo/card/extra",
		"kbrd://card/Work/todo/card?view=raw",
		"kbrd://card/Work/todo/card#fragment",
		"kbrd://other/Work/todo/card",
	} {
		if _, err := parseResourceURI(invalid, "card", 3); err == nil {
			t.Errorf("parseResourceURI(%q) succeeded", invalid)
		}
	}
}

func TestReadBoardsAndBoardResources(t *testing.T) {
	root := makeBoardDir(t, "1. TODO", "2. Done", "_archive")
	if err := os.WriteFile(filepath.Join(root, "1. TODO", "a card.md"), []byte("body\n"), 0o644); err != nil {
		t.Fatal(err)
	}
	stale := filepath.Join(t.TempDir(), "missing")
	seedRecents(t, []recents.Entry{
		{Path: root, Name: "Work / R&D", Pinned: true},
		{Path: stale, Name: "Old"},
	})

	res, err := readBoardsResource(t.Context(), resourceRequest(boardsResourceURI))
	if err != nil {
		t.Fatal(err)
	}
	var index boardsResource
	if err := json.Unmarshal([]byte(res.Contents[0].Text), &index); err != nil {
		t.Fatal(err)
	}
	if index.SchemaVersion != resourceSchemaV1 || len(index.Boards) != 2 {
		t.Fatalf("boards resource = %+v", index)
	}
	if !index.Boards[0].Available || !index.Boards[0].Pinned || index.Boards[1].Available {
		t.Fatalf("board availability = %+v", index.Boards)
	}

	uri := boardResourceURI("Work / R&D")
	if _, err := readBoardResource(t.Context(), resourceRequest(strings.Replace(uri, "%2F", "%2f", 1)), true); err == nil {
		t.Fatal("non-canonical board URI resolved")
	}
	res, err = readBoardResource(t.Context(), resourceRequest(uri), true)
	if err != nil {
		t.Fatal(err)
	}
	var snapshot boardResource
	if err := json.Unmarshal([]byte(res.Contents[0].Text), &snapshot); err != nil {
		t.Fatal(err)
	}
	if len(snapshot.Columns) != 2 || snapshot.Columns[0].Name != "1. TODO" {
		t.Fatalf("board resource = %+v", snapshot)
	}
	if len(snapshot.Columns[0].Cards) != 1 || snapshot.Columns[0].Cards[0].URI != cardResourceURI("Work / R&D", "1. TODO", "a card") {
		t.Fatalf("cards = %+v", snapshot.Columns[0].Cards)
	}

	res, err = readBoardResource(t.Context(), resourceRequest(uri), false)
	if err != nil {
		t.Fatal(err)
	}
	snapshot = boardResource{}
	if err := json.Unmarshal([]byte(res.Contents[0].Text), &snapshot); err != nil {
		t.Fatal(err)
	}
	if snapshot.Columns[0].Cards[0].URI != "" {
		t.Fatalf("disabled card URI = %q", snapshot.Columns[0].Cards[0].URI)
	}
}

func TestReadCardResourceIsExactAndPreservesMarkdown(t *testing.T) {
	root := makeBoardDir(t, "TODO")
	raw := "---\ntags: [release]\n---\n# Ship it\n\nDetails.\n"
	if err := os.WriteFile(filepath.Join(root, "TODO", "Release.md"), []byte(raw), 0o644); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(root, "TODO", "_hidden.md"), []byte("secret\n"), 0o644); err != nil {
		t.Fatal(err)
	}
	seedRecents(t, []recents.Entry{{Path: root, Name: "Work"}})

	uri := cardResourceURI("Work", "TODO", "Release")
	res, err := readCardResource(t.Context(), resourceRequest(uri))
	if err != nil {
		t.Fatal(err)
	}
	if got := res.Contents[0]; got.Text != raw || got.MIMEType != markdownResourceMIME || got.URI != uri {
		t.Fatalf("card resource = %+v", got)
	}

	for _, missing := range []string{
		cardResourceURI("Wor", "TODO", "Release"),
		cardResourceURI("Work", "todo", "Release"),
		cardResourceURI("Work", "TODO", "release"),
		cardResourceURI("Work", "TODO", "_hidden"),
	} {
		if _, err := readCardResource(t.Context(), resourceRequest(missing)); err == nil {
			t.Errorf("readCardResource(%q) succeeded", missing)
		}
	}
}

func TestDuplicateBoardLabelsAreNotResourceIdentity(t *testing.T) {
	seedRecents(t, []recents.Entry{
		{Path: makeBoardDir(t, "todo"), Name: "Work"},
		{Path: makeBoardDir(t, "todo"), Name: "Work"},
	})
	if _, err := readBoardResource(t.Context(), resourceRequest(boardResourceURI("Work")), true); err == nil {
		t.Fatal("duplicate board label resolved")
	}
	res, err := readBoardsResource(t.Context(), resourceRequest(boardsResourceURI))
	if err != nil {
		t.Fatal(err)
	}
	var index boardsResource
	if err := json.Unmarshal([]byte(res.Contents[0].Text), &index); err != nil {
		t.Fatal(err)
	}
	if !index.Boards[0].Ambiguous || index.Boards[0].Available {
		t.Fatalf("duplicate board entry = %+v", index.Boards[0])
	}
}

func TestResourceRegistrationHonorsCardReadPolicy(t *testing.T) {
	for _, tc := range []struct {
		name     string
		policy   Policy
		wantCard bool
	}{
		{name: "disabled", policy: Policy{}},
		{name: "enabled", policy: Policy{AllowCardReads: true}, wantCard: true},
	} {
		t.Run(tc.name, func(t *testing.T) {
			root := makeBoardDir(t, "todo")
			if err := os.WriteFile(filepath.Join(root, "todo", "card.md"), []byte("content\n"), 0o644); err != nil {
				t.Fatal(err)
			}
			seedRecents(t, []recents.Entry{{Path: root, Name: "Work"}})
			server := newServer(tc.policy)
			client := sdkmcp.NewClient(&sdkmcp.Implementation{Name: "test", Version: "0"}, nil)
			serverTransport, clientTransport := sdkmcp.NewInMemoryTransports()
			serverSession, err := server.Connect(t.Context(), serverTransport, nil)
			if err != nil {
				t.Fatal(err)
			}
			defer serverSession.Close()
			session, err := client.Connect(t.Context(), clientTransport, nil)
			if err != nil {
				t.Fatal(err)
			}
			defer session.Close()

			resources, err := session.ListResources(t.Context(), nil)
			if err != nil {
				t.Fatal(err)
			}
			if len(resources.Resources) != 1 || resources.Resources[0].URI != boardsResourceURI {
				t.Fatalf("resources = %+v", resources.Resources)
			}
			templates, err := session.ListResourceTemplates(t.Context(), nil)
			if err != nil {
				t.Fatal(err)
			}
			gotCard := false
			gotBoard := false
			for _, tmpl := range templates.ResourceTemplates {
				gotBoard = gotBoard || tmpl.URITemplate == boardResourceTmpl
				gotCard = gotCard || tmpl.URITemplate == cardResourceTmpl
			}
			if !gotBoard || gotCard != tc.wantCard {
				t.Fatalf("templates = %+v", templates.ResourceTemplates)
			}
			if tc.wantCard {
				res, err := session.ReadResource(t.Context(), &sdkmcp.ReadResourceParams{URI: cardResourceURI("Work", "todo", "card")})
				if err != nil {
					t.Fatal(err)
				}
				if len(res.Contents) != 1 || res.Contents[0].Text != "content\n" {
					t.Fatalf("card read = %+v", res.Contents)
				}
			}
		})
	}
}

func TestReadMissingResourceReturnsNotFound(t *testing.T) {
	seedRecents(t, nil)
	_, err := readCardResource(t.Context(), resourceRequest(cardResourceURI("none", "todo", "card")))
	if err == nil {
		t.Fatal("missing resource succeeded")
	}
	if !strings.Contains(err.Error(), "Resource not found") {
		t.Fatalf("missing resource error = %v", err)
	}
}
