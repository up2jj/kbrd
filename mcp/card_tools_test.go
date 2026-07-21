package mcp

import (
	"encoding/json"
	"errors"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"kbrd/recents"
)

func TestGetAndSearchCards(t *testing.T) {
	root := makeBoardDir(t, "todo", "done")
	writeCard(t, root, "todo", "alpha", "---\ntags: [urgent, api]\nowner: sam\n---\nFix parser\n")
	writeCard(t, root, "done", "beta", "Ship release\n")
	seedRecents(t, []recents.Entry{{Path: root, Name: "Work"}})

	_, got, err := getCard(t.Context(), nil, GetCardInput{Board: "Work", Column: "TODO", Name: "alpha.md"}, true)
	if err != nil {
		t.Fatalf("getCard: %v", err)
	}
	if got.Name != "alpha" || got.Column != "todo" || got.Body != "Fix parser\n" {
		t.Fatalf("card = %+v", got)
	}
	if got.Frontmatter["owner"] != "sam" || got.Revision == "" {
		t.Fatalf("frontmatter/revision = %+v / %q", got.Frontmatter, got.Revision)
	}

	for _, query := range []string{"ALPHA", "parser", "urgent", "owner: sam"} {
		t.Run(query, func(t *testing.T) {
			_, found, err := searchCards(t.Context(), nil, SearchCardsInput{Board: "Work", Query: query}, true)
			if err != nil {
				t.Fatal(err)
			}
			if len(found.Cards) != 1 || found.Cards[0].Name != "alpha" {
				t.Fatalf("cards = %+v", found.Cards)
			}
		})
	}

	_, found, err := searchCards(t.Context(), nil, SearchCardsInput{Board: "Work", Columns: []string{"DONE"}}, true)
	if err != nil {
		t.Fatal(err)
	}
	if len(found.Cards) != 1 || found.Cards[0].Name != "beta" {
		t.Fatalf("column-filtered cards = %+v", found.Cards)
	}
	if found.Cards[0].Frontmatter == nil || len(found.Cards[0].Frontmatter) != 0 {
		t.Fatalf("card without frontmatter returned %#v, want empty object", found.Cards[0].Frontmatter)
	}
	if _, _, err := searchCards(t.Context(), nil, SearchCardsInput{Board: "Work", Columns: []string{"missing"}}, true); err == nil {
		t.Fatal("expected unknown-column error")
	}
	if _, _, err := getCard(t.Context(), nil, GetCardInput{Board: "Work", Column: "todo", Name: "alpha"}, false); err == nil {
		t.Fatal("expected card-read policy error")
	}
	if _, _, err := searchCards(t.Context(), nil, SearchCardsInput{Board: "Work"}, false); err == nil {
		t.Fatal("expected card-search policy error")
	}
}

func TestUpdateAndDeleteCardRequireCurrentRevision(t *testing.T) {
	root := makeBoardDir(t, "todo")
	writeCard(t, root, "todo", "task", "old\n")
	seedRecents(t, []recents.Entry{{Path: root, Name: "Work"}})

	_, card, err := getCard(t.Context(), nil, GetCardInput{Board: "Work", Column: "todo", Name: "task"}, true)
	if err != nil {
		t.Fatal(err)
	}
	if _, _, err := updateCard(t.Context(), nil, UpdateCardInput{
		Board: "Work", Column: "todo", Name: "task", Content: "new", ExpectedRevision: "stale",
	}); err == nil || !strings.Contains(err.Error(), "revision conflict") {
		t.Fatalf("stale update error = %v", err)
	}

	_, updated, err := updateCard(t.Context(), nil, UpdateCardInput{
		Board: "Work", Column: "todo", Name: "task", Content: "new", ExpectedRevision: card.Revision,
	})
	if err != nil {
		t.Fatalf("updateCard: %v", err)
	}
	if updated.Revision == card.Revision || updated.Revision == "" {
		t.Fatalf("new revision = %q, old = %q", updated.Revision, card.Revision)
	}
	data, err := os.ReadFile(filepath.Join(root, "todo", "task.md"))
	if err != nil || string(data) != "new\n" {
		t.Fatalf("updated content = %q, err = %v", data, err)
	}

	if _, _, err := deleteCard(t.Context(), nil, DeleteCardInput{
		Board: "Work", Column: "todo", Name: "task", ExpectedRevision: card.Revision,
	}); err == nil || !strings.Contains(err.Error(), "revision conflict") {
		t.Fatalf("stale delete error = %v", err)
	}
	if _, _, err := deleteCard(t.Context(), nil, DeleteCardInput{
		Board: "Work", Column: "todo", Name: "task", ExpectedRevision: updated.Revision,
	}); err != nil {
		t.Fatalf("deleteCard: %v", err)
	}
	if _, err := os.Stat(filepath.Join(root, "todo", "task.md")); !errors.Is(err, os.ErrNotExist) {
		t.Fatalf("deleted card stat error = %v", err)
	}
}

func TestMoveRenameAndCreateColumn(t *testing.T) {
	root := makeBoardDir(t, "todo", "done")
	writeCard(t, root, "todo", "task", "body\n")
	writeCard(t, root, "done", "taken", "body\n")
	seedRecents(t, []recents.Entry{{Path: root, Name: "Work"}})

	if _, _, err := moveCard(t.Context(), nil, MoveCardInput{
		Board: "Work", Column: "todo", Name: "task", Destination: "done",
	}); err != nil {
		t.Fatalf("moveCard: %v", err)
	}
	if _, _, err := renameCard(t.Context(), nil, RenameCardInput{
		Board: "Work", Column: "done", Name: "task", NewName: "taken",
	}); !errors.Is(err, os.ErrExist) {
		t.Fatalf("rename collision error = %v", err)
	}
	_, renamed, err := renameCard(t.Context(), nil, RenameCardInput{
		Board: "Work", Column: "done", Name: "task", NewName: "renamed.md",
	})
	if err != nil {
		t.Fatalf("renameCard: %v", err)
	}
	if renamed.Name != "renamed" {
		t.Fatalf("renamed output = %+v", renamed)
	}

	_, created, err := createColumn(t.Context(), nil, CreateColumnInput{Board: "Work", Name: "review"})
	if err != nil {
		t.Fatalf("createColumn: %v", err)
	}
	if created.Column != "review" {
		t.Fatalf("created column = %+v", created)
	}
	if _, err := os.Stat(filepath.Join(root, "review", ".gitkeep")); err != nil {
		t.Fatalf("durable column marker: %v", err)
	}
}

func TestCardToolOutputsMarshal(t *testing.T) {
	for _, value := range []any{CardOutput{}, SearchCardsOutput{}, MutationOutput{}, CreateColumnOutput{}} {
		if _, err := json.Marshal(value); err != nil {
			t.Fatalf("marshal %T: %v", value, err)
		}
	}
}

func writeCard(t *testing.T, root, column, name, content string) {
	t.Helper()
	if err := os.WriteFile(filepath.Join(root, column, name+".md"), []byte(content), 0o644); err != nil {
		t.Fatal(err)
	}
}
