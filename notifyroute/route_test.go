package notifyroute

import (
	"testing"
	"time"
)

func TestRouteRoundTrip(t *testing.T) {
	server, err := Listen()
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = server.Close() })
	want := Command{Action: OpenCard, BoardPath: "/board", CardPath: "/board/Todo/card.md"}
	if err := Send(server.Path(), want); err != nil {
		t.Fatal(err)
	}
	select {
	case got := <-server.Commands():
		if got != want {
			t.Fatalf("command = %#v, want %#v", got, want)
		}
	case <-time.After(time.Second):
		t.Fatal("timed out waiting for routed action")
	}
}

func TestCommandValidation(t *testing.T) {
	for _, command := range []Command{
		{Action: OpenCard, BoardPath: "/board", CardPath: "/card"},
		{Action: MarkDone, BoardPath: "/board", CardPath: "/card"},
		{Action: SnoozeDue, BoardPath: "/board", CardPath: "/card"},
		{Action: RetrySync, BoardPath: "/board", SyncKind: "git"},
		{Action: RetrySync, BoardPath: "/board", SyncKind: "reminders"},
	} {
		if !command.Valid() {
			t.Errorf("expected valid command: %#v", command)
		}
	}
	if (Command{Action: RetrySync, BoardPath: "/board", SyncKind: "other"}).Valid() {
		t.Fatal("unknown sync kind is valid")
	}
}
