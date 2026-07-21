package mcp

import (
	"context"
	"fmt"
	"net"
	"net/http"
	"os"
	"path/filepath"
	"strings"
	"sync/atomic"
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

	initResult := session.InitializeResult()
	if initResult == nil {
		t.Fatal("initialize result is nil")
	}
	if got, want := initResult.Instructions, ServerInstructions(); got != want {
		t.Errorf("server instructions = %q, want %q", got, want)
	}

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

func TestServeRoundTripElicitsBoardAndFolderChoices(t *testing.T) {
	firstBoard := makeBoardDir(t, "1. todo")
	secondBoard := makeBoardDir(t, "1. backlog", "2. doing")
	seedRecents(t, []recents.Entry{
		{Path: firstBoard, Name: "Demo"},
		{Path: secondBoard, Name: "Demo"},
	})

	var calls atomic.Int32
	ctx, session := connectTestClient(t, &mcp.ClientOptions{
		Capabilities: &mcp.ClientCapabilities{
			Elicitation: &mcp.ElicitationCapabilities{Form: &mcp.FormElicitationCapabilities{}},
		},
		ElicitationHandler: func(_ context.Context, req *mcp.ElicitRequest) (*mcp.ElicitResult, error) {
			calls.Add(1)
			switch {
			case strings.Contains(req.Params.Message, "Several boards"):
				return &mcp.ElicitResult{Action: "accept", Content: map[string]any{"choice": secondBoard}}, nil
			case strings.Contains(req.Params.Message, "does not exist"):
				return &mcp.ElicitResult{Action: "accept", Content: map[string]any{"choice": "2. doing"}}, nil
			default:
				return nil, fmt.Errorf("unexpected elicitation message %q", req.Params.Message)
			}
		},
	})

	res, err := session.CallTool(ctx, &mcp.CallToolParams{
		Name: "add_file_to_board",
		Arguments: AddFileInput{
			Board: "Demo", Name: "elicited", Folder: "missing", Content: "chosen",
		},
	})
	if err != nil {
		t.Fatalf("call: %v", err)
	}
	if res.IsError {
		t.Fatalf("tool error: %+v", res.Content)
	}
	if got := calls.Load(); got != 2 {
		t.Fatalf("elicitation calls = %d, want 2", got)
	}
	if _, err := os.Stat(filepath.Join(secondBoard, "2. doing", "elicited.md")); err != nil {
		t.Fatalf("item not created in elicited board and folder: %v", err)
	}
}

func TestServeRoundTripElicitationFallbackAndUserActions(t *testing.T) {
	for _, action := range []string{"unsupported", "decline", "cancel"} {
		t.Run(action, func(t *testing.T) {
			firstBoard := makeBoardDir(t, "todo")
			secondBoard := makeBoardDir(t, "doing")
			seedRecents(t, []recents.Entry{
				{Path: firstBoard, Name: "Demo"},
				{Path: secondBoard, Name: "Demo"},
			})

			var opts *mcp.ClientOptions
			if action != "unsupported" {
				opts = &mcp.ClientOptions{
					ElicitationHandler: func(context.Context, *mcp.ElicitRequest) (*mcp.ElicitResult, error) {
						return &mcp.ElicitResult{Action: action}, nil
					},
				}
			}
			ctx, session := connectTestClient(t, opts)
			res, err := session.CallTool(ctx, &mcp.CallToolParams{
				Name:      "list_folders",
				Arguments: ListFoldersInput{Board: "Demo"},
			})
			if err != nil {
				t.Fatalf("call: %v", err)
			}
			if !res.IsError {
				t.Fatalf("result IsError = false, content = %+v", res.Content)
			}
			text := res.Content[0].(*mcp.TextContent).Text
			want := "board name is ambiguous"
			if action != "unsupported" {
				want = "user " + action + "d elicitation"
				if action == "cancel" {
					want = "user canceled elicitation"
				}
			}
			if !strings.Contains(text, want) {
				t.Fatalf("tool error = %q, want containing %q", text, want)
			}
		})
	}
}

func connectTestClient(t *testing.T, opts *mcp.ClientOptions) (context.Context, *mcp.ClientSession) {
	t.Helper()
	ln, err := net.Listen("tcp", "127.0.0.1:0")
	if err != nil {
		t.Fatalf("listen: %v", err)
	}
	closer := serveListener(ln, Policy{})
	t.Cleanup(func() {
		if err := closer.Close(); err != nil {
			t.Errorf("shutdown: %v", err)
		}
	})

	ctx, cancel := context.WithTimeout(t.Context(), 5*time.Second)
	t.Cleanup(cancel)
	client := mcp.NewClient(&mcp.Implementation{Name: "test", Version: "0"}, opts)
	session, err := client.Connect(ctx, &mcp.StreamableClientTransport{Endpoint: "http://" + ln.Addr().String()}, nil)
	if err != nil {
		t.Fatalf("connect: %v", err)
	}
	t.Cleanup(func() {
		if err := session.Close(); err != nil {
			t.Errorf("close session: %v", err)
		}
	})
	return ctx, session
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
