package mcp

import (
	"context"
	"errors"
	"fmt"
	"io"
	"net"
	"net/http"
	"os"
	"strings"
	"time"

	"github.com/modelcontextprotocol/go-sdk/mcp"
)

// Version is the server version advertised to MCP clients. Kept in sync with
// the app version by main.go via SetVersion.
var version = "dev"

// SetVersion overrides the version string advertised to MCP clients.
func SetVersion(v string) {
	if v != "" {
		version = v
	}
}

// Policy carries the trust decisions made by the CLI/config layer into the
// headless MCP server. The zero value is conservative.
type Policy struct {
	AllowCommands       bool
	AllowFolderCommands bool
	AllowCardReads      bool
}

// newServer builds the MCP server with all kbrd tools and permitted resources
// registered.
func newServer(policy Policy) *mcp.Server {
	s := mcp.NewServer(
		&mcp.Implementation{Name: "kbrd", Version: version},
		&mcp.ServerOptions{
			Instructions: ServerInstructions(),
			Capabilities: mcpAppServerCapabilities(),
			CompletionHandler: func(ctx context.Context, req *mcp.CompleteRequest) (*mcp.CompleteResult, error) {
				return completeResourceArgument(ctx, req, policy)
			},
		},
	)
	registerResources(s, policy)
	falsePtr := false
	truePtr := true

	mcp.AddTool(s, &mcp.Tool{
		Name:        "add_file_to_board",
		Description: "Create a markdown item (card) in a kbrd board, identified by its friendly name. Optionally place it in a named folder (column); defaults to the board's first folder. Set create_folder to make a missing folder.",
		Annotations: &mcp.ToolAnnotations{DestructiveHint: &falsePtr, OpenWorldHint: &falsePtr},
	}, addFileToBoard)

	mcp.AddTool(s, &mcp.Tool{
		Name:        "list_boards",
		Description: "List the kbrd boards known to this machine (name, path, pinned). Use this to discover valid board names.",
		Annotations: &mcp.ToolAnnotations{ReadOnlyHint: true, OpenWorldHint: &falsePtr},
	}, listBoards)

	mcp.AddTool(s, &mcp.Tool{
		Name:        "list_folders",
		Description: "List the folders (columns) of a kbrd board, identified by its friendly name.",
		Annotations: &mcp.ToolAnnotations{ReadOnlyHint: true, OpenWorldHint: &falsePtr},
	}, listFolders)

	mcp.AddTool(s, &mcp.Tool{
		Name:        "list_files",
		Description: "List the markdown items in a kbrd board folder. The folder defaults to the board's first folder.",
		Annotations: &mcp.ToolAnnotations{ReadOnlyHint: true, OpenWorldHint: &falsePtr},
	}, listFiles)

	mcp.AddTool(s, &mcp.Tool{
		Meta: mcp.Meta{
			"ui": map[string]any{
				"resourceUri": boardAppResourceURI,
				"visibility":  []string{"model", "app"},
			},
		},
		Name:        "show_board",
		Title:       "Show board",
		Description: "Show a read-only snapshot of a kbrd board's columns and card names. MCP Apps clients render an interactive board; other clients receive structured data.",
		Annotations: &mcp.ToolAnnotations{ReadOnlyHint: true, OpenWorldHint: &falsePtr},
	}, showBoard)

	mcp.AddTool(s, &mcp.Tool{
		Meta:        mcp.Meta{"ui": map[string]any{"visibility": []string{"model", "app"}}},
		Name:        "get_card",
		Description: "Read a card's raw Markdown, body, parsed frontmatter, column, and SHA-256 revision. Requires [mcp] allow_card_reads = true.",
		Annotations: &mcp.ToolAnnotations{ReadOnlyHint: true, OpenWorldHint: &falsePtr},
	}, func(ctx context.Context, req *mcp.CallToolRequest, in GetCardInput) (*mcp.CallToolResult, CardOutput, error) {
		return getCard(ctx, req, in, policy.AllowCardReads)
	})

	mcp.AddTool(s, &mcp.Tool{
		Name:        "search_cards",
		Description: "Search card names, bodies, tags, and frontmatter across all or selected columns. Requires [mcp] allow_card_reads = true.",
		Annotations: &mcp.ToolAnnotations{ReadOnlyHint: true, OpenWorldHint: &falsePtr},
	}, func(ctx context.Context, req *mcp.CallToolRequest, in SearchCardsInput) (*mcp.CallToolResult, SearchCardsOutput, error) {
		return searchCards(ctx, req, in, policy.AllowCardReads)
	})

	mcp.AddTool(s, &mcp.Tool{
		Name:        "update_card",
		Description: "Replace a card's complete Markdown only when expected_revision matches its current revision.",
		Annotations: &mcp.ToolAnnotations{DestructiveHint: &truePtr, IdempotentHint: true, OpenWorldHint: &falsePtr},
	}, updateCard)

	mcp.AddTool(s, &mcp.Tool{
		Name:        "move_card",
		Description: "Move a card to an existing destination column without overwriting a card of the same name.",
		Annotations: &mcp.ToolAnnotations{DestructiveHint: &truePtr, OpenWorldHint: &falsePtr},
	}, moveCard)

	mcp.AddTool(s, &mcp.Tool{
		Name:        "rename_card",
		Description: "Rename a card within its column without overwriting another card.",
		Annotations: &mcp.ToolAnnotations{DestructiveHint: &truePtr, OpenWorldHint: &falsePtr},
	}, renameCard)

	mcp.AddTool(s, &mcp.Tool{
		Name:        "delete_card",
		Description: "Delete a card only when expected_revision matches its current revision. This cannot be undone by kbrd.",
		Annotations: &mcp.ToolAnnotations{DestructiveHint: &truePtr, OpenWorldHint: &falsePtr},
	}, deleteCard)

	mcp.AddTool(s, &mcp.Tool{
		Name:        "create_column",
		Description: "Create a durable empty column that can be committed to version control.",
		Annotations: &mcp.ToolAnnotations{DestructiveHint: &falsePtr, OpenWorldHint: &falsePtr},
	}, createColumn)

	mcp.AddTool(s, &mcp.Tool{
		Name:        "list_custom_commands",
		Description: "List shell custom commands available for a kbrd board, optionally filtered for a folder or item context. Folder-local .kbrd_commands.yml commands are included only when MCP policy allows board-local commands. Lua and editor-line commands are not included.",
		Annotations: &mcp.ToolAnnotations{ReadOnlyHint: true, OpenWorldHint: &falsePtr},
	}, func(ctx context.Context, req *mcp.CallToolRequest, in ListCommandsInput) (*mcp.CallToolResult, ListCommandsOutput, error) {
		return listCustomCommandsWithPolicy(ctx, req, in, policy)
	})

	mcp.AddTool(s, &mcp.Tool{
		Name:        "run_custom_command",
		Description: "DANGEROUS: run a shell custom command (by id) for a kbrd board. Requires [mcp] allow_commands = true and is disabled by --safe. Output is truncated to a bounded size.",
		Annotations: &mcp.ToolAnnotations{DestructiveHint: &truePtr},
	}, func(ctx context.Context, req *mcp.CallToolRequest, in RunCommandInput) (*mcp.CallToolResult, RunCommandOutput, error) {
		return runCustomCommandWithPolicy(ctx, req, in, policy)
	})

	return s
}

// closer shuts down the HTTP server it wraps.
type closer struct{ srv *http.Server }

func (c closer) Close() error {
	ctx, cancel := context.WithTimeout(context.Background(), mcpShutdownTimeout)
	defer cancel()
	return c.srv.Shutdown(ctx)
}

const (
	mcpReadHeaderTimeout = 5 * time.Second
	mcpReadTimeout       = 30 * time.Second
	// Streamable HTTP may keep an SSE response open while a tool waits for a
	// user's elicitation response. Per-operation contexts bound that work; a
	// server-wide write deadline would terminate otherwise healthy sessions.
	mcpWriteTimeout    time.Duration = 0
	mcpIdleTimeout                   = 120 * time.Second
	mcpShutdownTimeout               = 5 * time.Second
)

func newHTTPServer(handler http.Handler) *http.Server {
	return &http.Server{
		Handler:           handler,
		ReadHeaderTimeout: mcpReadHeaderTimeout,
		ReadTimeout:       mcpReadTimeout,
		WriteTimeout:      mcpWriteTimeout,
		IdleTimeout:       mcpIdleTimeout,
	}
}

// Serve starts the kbrd MCP server on a Streamable HTTP listener at addr and
// returns immediately. The returned io.Closer stops the server. A bind error
// (e.g. the port is already in use by another kbrd instance) is returned so
// the caller can warn and continue without an MCP server.
func Serve(addr string, policy Policy) (io.Closer, error) {
	if err := requireLoopbackAddr(addr); err != nil {
		return nil, err
	}
	ln, err := net.Listen("tcp", addr)
	if err != nil {
		return nil, err
	}
	return serveListener(ln, policy), nil
}

// serveListener starts the MCP server on an already-bound listener. Keeping
// the bind separate lets tests request an OS-assigned port without a race.
func serveListener(ln net.Listener, policy Policy) io.Closer {
	srv := newServer(policy)
	handler := mcp.NewStreamableHTTPHandler(func(*http.Request) *mcp.Server { return srv }, nil)

	httpSrv := newHTTPServer(handler)
	go func() {
		if err := httpSrv.Serve(ln); err != nil && !errors.Is(err, http.ErrServerClosed) {
			fmt.Fprintf(os.Stderr, "warning: MCP server stopped: %v\n", err)
		}
	}()
	return closer{srv: httpSrv}
}

// Start brings up the MCP server on addr and returns the closer plus whether
// the listener actually bound. It owns the version handshake and the
// bind-failure warning so callers (main) stay thin. A bind error — most often
// the port is already held by another kbrd instance, which already serves every
// board via recents — is warned about and reported as running=false, never
// fatal. A nil closer is safe to ignore; callers should still guard Close.
func Start(version, addr string, policy Policy) (io.Closer, bool) {
	SetVersion(version)
	c, err := Serve(addr, policy)
	if err != nil {
		fmt.Fprintf(os.Stderr, "warning: MCP server not started on %s: %v\n", addr, err)
		return nil, false
	}
	return c, true
}

// Run starts the server on addr and blocks until ctx is cancelled. Useful for
// running the MCP server standalone (not currently wired, but handy for tests
// and tooling).
func Run(ctx context.Context, addr string) error {
	c, err := Serve(addr, Policy{})
	if err != nil {
		return err
	}
	<-ctx.Done()
	return c.Close()
}

func requireLoopbackAddr(addr string) error {
	host, _, err := net.SplitHostPort(addr)
	if err != nil {
		return fmt.Errorf("parse listen address: %w", err)
	}
	host = strings.Trim(host, "[]")
	if host == "localhost" {
		return nil
	}
	ip := net.ParseIP(host)
	if ip != nil && ip.IsLoopback() {
		return nil
	}
	return fmt.Errorf("refusing non-loopback MCP listen address %q; authentication is not configured", addr)
}
