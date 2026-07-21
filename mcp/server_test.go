package mcp

import (
	"context"
	"net"
	"net/http"
	"os"
	"path/filepath"
	"testing"
	"time"

	"github.com/modelcontextprotocol/go-sdk/mcp"

	"kbrd/recents"
)

// TestServeRoundTrip starts the real Streamable HTTP server and drives it with
// the SDK client: list tools, then call add_file_to_board and confirm the file
// lands on disk.
func TestServeRoundTrip(t *testing.T) {
	boardPath := makeBoardDir(t, "1. todo")
	seedRecents(t, []recents.Entry{{Path: boardPath, Name: "Demo"}})

	ln, err := net.Listen("tcp", "127.0.0.1:0")
	if err != nil {
		t.Fatalf("listen: %v", err)
	}
	c := serveListener(ln, Policy{})
	defer func() {
		if err := c.Close(); err != nil {
			t.Errorf("shutdown: %v", err)
		}
	}()
	addr := ln.Addr().String()

	ctx, cancel := context.WithTimeout(t.Context(), 5*time.Second)
	defer cancel()

	client := mcp.NewClient(&mcp.Implementation{Name: "test", Version: "0"}, nil)
	session, err := client.Connect(ctx, &mcp.StreamableClientTransport{Endpoint: "http://" + addr}, nil)
	if err != nil {
		t.Fatalf("connect: %v", err)
	}
	defer session.Close()

	tools, err := session.ListTools(ctx, nil)
	if err != nil {
		t.Fatalf("list tools: %v", err)
	}
	want := map[string]bool{"add_file_to_board": false, "list_boards": false, "list_folders": false, "list_files": false}
	for _, tl := range tools.Tools {
		if _, ok := want[tl.Name]; ok {
			want[tl.Name] = true
		}
	}
	for name, found := range want {
		if !found {
			t.Errorf("tool %q not advertised", name)
		}
	}

	res, err := session.CallTool(ctx, &mcp.CallToolParams{
		Name:      "add_file_to_board",
		Arguments: AddFileInput{Board: "Demo", Name: "hello", Content: "hi there"},
	})
	if err != nil {
		t.Fatalf("call: %v", err)
	}
	if res.IsError {
		t.Fatalf("tool error: %+v", res.Content)
	}
	if _, err := os.Stat(filepath.Join(boardPath, "1. todo", "hello.md")); err != nil {
		t.Fatalf("item not created: %v", err)
	}
}

func TestServeRefusesNonLoopbackAddr(t *testing.T) {
	if _, err := Serve("0.0.0.0:0", Policy{}); err == nil {
		t.Fatal("expected non-loopback address to be refused")
	}
}

func TestNewHTTPServer_Timeouts(t *testing.T) {
	srv := newHTTPServer(http.NotFoundHandler())
	if srv.ReadHeaderTimeout != mcpReadHeaderTimeout {
		t.Errorf("ReadHeaderTimeout = %v, want %v", srv.ReadHeaderTimeout, mcpReadHeaderTimeout)
	}
	if srv.ReadTimeout != mcpReadTimeout {
		t.Errorf("ReadTimeout = %v, want %v", srv.ReadTimeout, mcpReadTimeout)
	}
	if srv.WriteTimeout != mcpWriteTimeout {
		t.Errorf("WriteTimeout = %v, want %v", srv.WriteTimeout, mcpWriteTimeout)
	}
	if srv.IdleTimeout != mcpIdleTimeout {
		t.Errorf("IdleTimeout = %v, want %v", srv.IdleTimeout, mcpIdleTimeout)
	}
}
