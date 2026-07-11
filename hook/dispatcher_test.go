package hook

import (
	"context"
	"os"
	"path/filepath"
	"testing"

	"kbrd/config"
	"kbrd/events"
)

func TestDispatcherDispatchesInOrderAndContinuesAfterFailure(t *testing.T) {
	dir := t.TempDir()
	output := filepath.Join(dir, "order")
	d := New(config.Config{Path: dir}, []config.Hook{
		{Name: "first", Event: events.NameItemCreated, Template: "printf first > " + output},
		{Name: "fails", Event: events.NameItemCreated, Template: "false"},
		{Name: "last", Event: events.NameItemCreated, Template: "printf last >> " + output},
	})

	results := d.Dispatch(context.Background(), events.ItemCreated{Item: events.ItemRef{Column: "Todo", Name: "card"}})
	if len(results) != 3 {
		t.Fatalf("results = %+v, want three", results)
	}
	if results[0].Err != nil || results[0].ExitCode != 0 {
		t.Fatalf("first result = %+v", results[0])
	}
	if results[1].Err != nil || results[1].ExitCode == 0 {
		t.Fatalf("failing result = %+v", results[1])
	}
	if results[2].Err != nil || results[2].ExitCode != 0 {
		t.Fatalf("last result = %+v", results[2])
	}
	data, err := os.ReadFile(output)
	if err != nil {
		t.Fatal(err)
	}
	if got, want := string(data), "firstlast"; got != want {
		t.Fatalf("execution order = %q, want %q", got, want)
	}
}
