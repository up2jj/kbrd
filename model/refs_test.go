package model

import (
	"strings"
	"testing"
)

func TestDelayedResolversRejectIndexOnlyFallbacks(t *testing.T) {
	col := newTestColumn(t, map[string]string{"a": "alpha"})
	b := &Board{columns: []*Column{col}}

	if got, err := b.resolveColumnRef(columnRef{}, 0); err != nil || got != col {
		t.Fatalf("synchronous column fallback = (%v, %v), want column", got, err)
	}
	if gotCol, gotItem, err := b.resolveItemRef(itemRefStable{FileName: "a"}, 0); err != nil || gotCol != col || gotItem.Name != "a" {
		t.Fatalf("synchronous item fallback = (%v, %v, %v), want col/a", gotCol, gotItem, err)
	}

	if _, err := b.resolveDelayedColumnRef(columnRef{}); err == nil || !strings.Contains(err.Error(), "column no longer exists") {
		t.Fatalf("delayed column resolver err = %v, want missing-column error", err)
	}
	if _, _, err := b.resolveDelayedItemRef(itemRefStable{FileName: "a"}); err == nil || !strings.Contains(err.Error(), "item no longer exists") {
		t.Fatalf("delayed item resolver err = %v, want missing-item error", err)
	}
}

func TestDelayedItemResolverUsesStablePathAfterColumnReorder(t *testing.T) {
	colA := newTestColumn(t, map[string]string{"same": "alpha"})
	colB := newTestColumn(t, map[string]string{"same": "bravo"})
	ref := refForItem(colB, colB.ItemByName("same"))
	b := &Board{columns: []*Column{colA, colB}}

	b.columns = []*Column{colB, colA}
	col, item, err := b.resolveDelayedItemRef(ref)
	if err != nil {
		t.Fatalf("resolve delayed item after reorder: %v", err)
	}
	if col != colB || item == nil || item.FullPath != ref.ItemPath {
		t.Fatalf("resolved col/item = %v/%v, want original path %q", col, item, ref.ItemPath)
	}
}
