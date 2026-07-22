package mcp

import (
	"context"
	"encoding/json"
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
	uiExtension, ok := initResult.Capabilities.Extensions[mcpAppsExtension].(map[string]any)
	if !ok {
		t.Fatalf("MCP Apps capability = %#v", initResult.Capabilities.Extensions[mcpAppsExtension])
	}
	if mimeTypes, ok := uiExtension["mimeTypes"].([]any); !ok || len(mimeTypes) != 1 || mimeTypes[0] != mcpAppHTMLMIME {
		t.Fatalf("MCP Apps MIME types = %#v", uiExtension["mimeTypes"])
	}

	tools, err := session.ListTools(ctx, nil)
	if err != nil {
		t.Fatalf("list tools: %v", err)
	}
	want := map[string]bool{
		"add_file_to_board": false, "list_boards": false, "list_folders": false, "list_files": false, "show_board": false,
		"get_card": false, "search_cards": false, "update_card": false, "move_card": false,
		"rename_card": false, "delete_card": false, "create_column": false,
	}
	var listBoardsTool *mcp.Tool
	var showBoardTool *mcp.Tool
	for _, tl := range tools.Tools {
		if _, ok := want[tl.Name]; ok {
			want[tl.Name] = true
		}
		if tl.Name == "show_board" {
			showBoardTool = tl
		}
		if tl.Name == "list_boards" {
			listBoardsTool = tl
		}
	}
	for name, found := range want {
		if !found {
			t.Errorf("tool %q not advertised", name)
		}
	}
	if showBoardTool == nil {
		t.Fatal("show_board tool not advertised")
	}
	if listBoardsTool == nil {
		t.Fatal("list_boards tool not advertised")
	}
	uiMeta, ok := listBoardsTool.Meta["ui"].(map[string]any)
	if !ok || uiMeta["resourceUri"] != boardAppResourceURI {
		t.Fatalf("list_boards UI metadata = %#v", listBoardsTool.Meta)
	}
	uiMeta, ok = showBoardTool.Meta["ui"].(map[string]any)
	if !ok || uiMeta["resourceUri"] != boardAppResourceURI {
		t.Fatalf("show_board UI metadata = %#v", showBoardTool.Meta)
	}

	listResult, err := session.CallTool(ctx, &mcp.CallToolParams{Name: "list_boards"})
	if err != nil {
		t.Fatalf("list_boards without MCP Apps capability: %v", err)
	}
	if listResult.IsError || listResult.StructuredContent == nil {
		t.Fatalf("list_boards fallback result = %+v", listResult)
	}

	boardResult, err := session.CallTool(ctx, &mcp.CallToolParams{
		Name:      "show_board",
		Arguments: ShowBoardInput{Board: "Demo"},
	})
	if err != nil {
		t.Fatalf("show_board: %v", err)
	}
	if boardResult.IsError || boardResult.StructuredContent == nil {
		t.Fatalf("show_board result = %+v", boardResult)
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

func TestServeRoundTripSearchCardsWithoutFrontmatter(t *testing.T) {
	boardPath := makeBoardDir(t, "todo")
	if err := os.WriteFile(filepath.Join(boardPath, "todo", "plain.md"), []byte("plain body\n"), 0o644); err != nil {
		t.Fatal(err)
	}
	seedRecents(t, []recents.Entry{{Path: boardPath, Name: "Demo"}})

	ctx, session := connectTestClientWithPolicy(t, nil, Policy{AllowCardReads: true})
	res, err := session.CallTool(ctx, &mcp.CallToolParams{
		Name:      "search_cards",
		Arguments: SearchCardsInput{Board: "Demo", Query: "plain"},
	})
	if err != nil {
		t.Fatalf("search_cards protocol validation: %v", err)
	}
	if res.IsError {
		t.Fatalf("search_cards tool error: %+v", res.Content)
	}
}

func TestToolAnnotations(t *testing.T) {
	ctx, session := connectTestClient(t, nil)
	result, err := session.ListTools(ctx, nil)
	if err != nil {
		t.Fatalf("list tools: %v", err)
	}

	tools := make(map[string]*mcp.Tool, len(result.Tools))
	for _, tool := range result.Tools {
		tools[tool.Name] = tool
	}

	readOnly := []string{
		"list_boards", "list_folders", "list_files", "show_board",
		"get_card", "search_cards", "list_custom_commands",
	}
	for _, name := range readOnly {
		tool := tools[name]
		if tool == nil || tool.Annotations == nil || !tool.Annotations.ReadOnlyHint {
			t.Errorf("tool %q is not annotated read-only: %#v", name, tool)
		}
	}

	destructive := []string{"delete_card", "run_custom_command"}
	for _, name := range destructive {
		tool := tools[name]
		if tool == nil || tool.Annotations == nil || tool.Annotations.DestructiveHint == nil || !*tool.Annotations.DestructiveHint {
			t.Errorf("tool %q is not annotated destructive: %#v", name, tool)
		}
	}

	closedWorld := []string{
		"add_file_to_board", "list_boards", "list_folders", "list_files",
		"show_board", "get_card", "search_cards", "update_card", "move_card",
		"rename_card", "delete_card", "create_column", "list_custom_commands",
	}
	for _, name := range closedWorld {
		tool := tools[name]
		if tool == nil || tool.Annotations == nil || tool.Annotations.OpenWorldHint == nil || *tool.Annotations.OpenWorldHint {
			t.Errorf("tool %q is not annotated closed-world: %#v", name, tool)
		}
	}
}

func TestServeRoundTripListCustomCommandsForItem(t *testing.T) {
	boardPath := makeBoardDir(t, "todo")
	if err := os.WriteFile(filepath.Join(boardPath, "todo", "card.md"), []byte("body\n"), 0o644); err != nil {
		t.Fatal(err)
	}
	writeCommands(t, boardPath, `commands:
  - name: Item command
    id: item
    command: echo "{{.fileName}}"
`)
	seedRecents(t, []recents.Entry{{Path: boardPath, Name: "Demo"}})

	ctx, session := connectTestClientWithPolicy(t, nil, Policy{AllowFolderCommands: true})
	res, err := session.CallTool(ctx, &mcp.CallToolParams{
		Name: "list_custom_commands",
		Arguments: ListCommandsInput{
			Board: "Demo", Folder: "todo", Item: "card",
		},
	})
	if err != nil {
		t.Fatalf("list_custom_commands protocol validation: %v", err)
	}
	if res.IsError {
		t.Fatalf("list_custom_commands tool error: %+v", res.Content)
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
				return &mcp.ElicitResult{Action: "accept", Content: map[string]any{"choice": "folder:1"}}, nil
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

func TestServeRoundTripElicitsMissingFolderCreation(t *testing.T) {
	boardPath := makeBoardDir(t, "todo")
	seedRecents(t, []recents.Entry{{Path: boardPath, Name: "Demo"}})

	ctx, session := connectTestClient(t, &mcp.ClientOptions{
		ElicitationHandler: func(_ context.Context, req *mcp.ElicitRequest) (*mcp.ElicitResult, error) {
			if !strings.Contains(req.Params.Message, "Create it or choose") {
				return nil, fmt.Errorf("unexpected elicitation message %q", req.Params.Message)
			}
			return &mcp.ElicitResult{Action: "accept", Content: map[string]any{"choice": "create"}}, nil
		},
	})

	res, err := session.CallTool(ctx, &mcp.CallToolParams{
		Name: "add_file_to_board",
		Arguments: AddFileInput{
			Board: "Demo", Name: "new", Folder: "doing", Content: "created interactively",
		},
	})
	if err != nil {
		t.Fatalf("call: %v", err)
	}
	if res.IsError {
		t.Fatalf("tool error: %+v", res.Content)
	}
	if _, err := os.Stat(filepath.Join(boardPath, "doing", "new.md")); err != nil {
		t.Fatalf("item not created in elicited new folder: %v", err)
	}
}

func TestServeRoundTripReadOnlyFolderChoiceDoesNotOfferCreation(t *testing.T) {
	boardPath := makeBoardDir(t, "todo")
	if err := os.WriteFile(filepath.Join(boardPath, "todo", "card.md"), nil, 0o644); err != nil {
		t.Fatal(err)
	}
	seedRecents(t, []recents.Entry{{Path: boardPath, Name: "Demo"}})

	ctx, session := connectTestClient(t, &mcp.ClientOptions{
		ElicitationHandler: func(_ context.Context, req *mcp.ElicitRequest) (*mcp.ElicitResult, error) {
			schema, err := json.Marshal(req.Params.RequestedSchema)
			if err != nil {
				return nil, err
			}
			if strings.Contains(string(schema), `"const":"create"`) {
				return nil, fmt.Errorf("read-only folder choice offered creation: %s", schema)
			}
			return &mcp.ElicitResult{Action: "accept", Content: map[string]any{"choice": "folder:0"}}, nil
		},
	})

	res, err := session.CallTool(ctx, &mcp.CallToolParams{
		Name:      "list_files",
		Arguments: ListFilesInput{Board: "Demo", Folder: "missing"},
	})
	if err != nil {
		t.Fatalf("call: %v", err)
	}
	if res.IsError {
		t.Fatalf("tool error: %+v", res.Content)
	}
	if _, err := os.Stat(filepath.Join(boardPath, "missing")); !os.IsNotExist(err) {
		t.Fatalf("read-only tool created missing folder: %v", err)
	}
}

func TestServeRoundTripElicitsUnknownCommand(t *testing.T) {
	boardPath := makeBoardDir(t, "todo")
	seedRecents(t, []recents.Entry{{Path: boardPath, Name: "Demo"}})
	writeCommands(t, boardPath, `commands:
  - name: Selected command
    id: selected
    description: print a marker
    command: printf selected-via-elicitation
`)

	ctx, session := connectTestClientWithPolicy(t, &mcp.ClientOptions{
		ElicitationHandler: func(_ context.Context, req *mcp.ElicitRequest) (*mcp.ElicitResult, error) {
			if !strings.Contains(req.Params.Message, "Command \"missing\"") {
				return nil, fmt.Errorf("unexpected elicitation message %q", req.Params.Message)
			}
			return &mcp.ElicitResult{Action: "accept", Content: map[string]any{"choice": "selected"}}, nil
		},
	}, Policy{AllowCommands: true, AllowFolderCommands: true})

	res, err := session.CallTool(ctx, &mcp.CallToolParams{
		Name:      "run_custom_command",
		Arguments: RunCommandInput{Board: "Demo", Command: "missing"},
	})
	if err != nil {
		t.Fatalf("call: %v", err)
	}
	if res.IsError {
		t.Fatalf("tool error: %+v", res.Content)
	}
	text := res.Content[0].(*mcp.TextContent).Text
	if !strings.Contains(text, "selected-via-elicitation") {
		t.Fatalf("command output = %q", text)
	}
}

func TestServeRoundTripCommandElicitationCannotBypassPolicy(t *testing.T) {
	boardPath := makeBoardDir(t, "todo")
	seedRecents(t, []recents.Entry{{Path: boardPath, Name: "Demo"}})
	writeCommands(t, boardPath, `commands:
  - name: Must not run
    id: blocked
    command: printf should-not-run
`)

	var calls atomic.Int32
	ctx, session := connectTestClientWithPolicy(t, &mcp.ClientOptions{
		ElicitationHandler: func(context.Context, *mcp.ElicitRequest) (*mcp.ElicitResult, error) {
			calls.Add(1)
			return &mcp.ElicitResult{Action: "accept", Content: map[string]any{"choice": "blocked"}}, nil
		},
	}, Policy{AllowFolderCommands: true})

	res, err := session.CallTool(ctx, &mcp.CallToolParams{
		Name:      "run_custom_command",
		Arguments: RunCommandInput{Board: "Demo", Command: "missing"},
	})
	if err != nil {
		t.Fatalf("call: %v", err)
	}
	if !res.IsError {
		t.Fatalf("result IsError = false, content = %+v", res.Content)
	}
	if got := calls.Load(); got != 0 {
		t.Fatalf("elicitation calls = %d, want 0 when commands are disabled", got)
	}
}

func TestServeRoundTripElicitationFallbackAndUserActions(t *testing.T) {
	for _, scenario := range []string{"unsupported", "url-only", "decline", "cancel", "invalid", "client-error"} {
		t.Run(scenario, func(t *testing.T) {
			firstBoard := makeBoardDir(t, "todo")
			secondBoard := makeBoardDir(t, "doing")
			seedRecents(t, []recents.Entry{
				{Path: firstBoard, Name: "Demo"},
				{Path: secondBoard, Name: "Demo"},
			})

			var calls atomic.Int32
			var opts *mcp.ClientOptions
			switch scenario {
			case "url-only":
				opts = &mcp.ClientOptions{
					Capabilities: &mcp.ClientCapabilities{
						Elicitation: &mcp.ElicitationCapabilities{URL: &mcp.URLElicitationCapabilities{}},
					},
					ElicitationHandler: func(context.Context, *mcp.ElicitRequest) (*mcp.ElicitResult, error) {
						calls.Add(1)
						return &mcp.ElicitResult{Action: "accept"}, nil
					},
				}
			case "decline", "cancel":
				opts = &mcp.ClientOptions{ElicitationHandler: func(context.Context, *mcp.ElicitRequest) (*mcp.ElicitResult, error) {
					return &mcp.ElicitResult{Action: scenario}, nil
				}}
			case "invalid":
				opts = &mcp.ClientOptions{ElicitationHandler: func(context.Context, *mcp.ElicitRequest) (*mcp.ElicitResult, error) {
					return &mcp.ElicitResult{Action: "accept", Content: map[string]any{"choice": "not-an-option"}}, nil
				}}
			case "client-error":
				opts = &mcp.ClientOptions{ElicitationHandler: func(context.Context, *mcp.ElicitRequest) (*mcp.ElicitResult, error) {
					return nil, fmt.Errorf("client form failed")
				}}
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
			switch scenario {
			case "decline":
				want = "user declined elicitation"
			case "cancel":
				want = "user canceled elicitation"
			case "invalid":
				want = "does not match requested schema"
			case "client-error":
				want = "client form failed"
			}
			if !strings.Contains(text, want) {
				t.Fatalf("tool error = %q, want containing %q", text, want)
			}
			if scenario == "url-only" && calls.Load() != 0 {
				t.Fatal("form elicitation was sent to a URL-only client")
			}
		})
	}
}

func connectTestClient(t *testing.T, opts *mcp.ClientOptions) (context.Context, *mcp.ClientSession) {
	return connectTestClientWithPolicy(t, opts, Policy{})
}

func connectTestClientWithPolicy(t *testing.T, opts *mcp.ClientOptions, policy Policy) (context.Context, *mcp.ClientSession) {
	t.Helper()
	ln, err := net.Listen("tcp", "127.0.0.1:0")
	if err != nil {
		t.Fatalf("listen: %v", err)
	}
	closer := serveListener(ln, policy)
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

func TestCloserForcesStuckConnectionsClosed(t *testing.T) {
	started := make(chan struct{})
	httpSrv := newHTTPServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		close(started)
		<-r.Context().Done()
	}))
	ln, err := net.Listen("tcp", "127.0.0.1:0")
	if err != nil {
		t.Fatalf("listen: %v", err)
	}
	go func() {
		_ = httpSrv.Serve(ln)
	}()

	requestDone := make(chan error, 1)
	go func() {
		_, err := http.Get("http://" + ln.Addr().String())
		requestDone <- err
	}()
	select {
	case <-started:
	case <-time.After(time.Second):
		t.Fatal("request did not reach server")
	}

	c := closer{srv: httpSrv, shutdownTimeout: 10 * time.Millisecond}
	if err := c.Close(); err != nil {
		t.Fatalf("close: %v", err)
	}
	select {
	case <-requestDone:
	case <-time.After(time.Second):
		t.Fatal("active request remained after close")
	}
}
