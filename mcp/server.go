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
		&mcp.ServerOptions{Instructions: ServerInstructions()},
	)
	registerResources(s, policy)
	falsePtr := false

	mcp.AddTool(s, &mcp.Tool{
		Name:        "add_file_to_board",
		Description: "Create a markdown item (card) in a kbrd board, identified by its friendly name. Optionally place it in a named folder (column); defaults to the board's first folder. Set create_folder to make a missing folder.",
	}, addFileToBoard)

	mcp.AddTool(s, &mcp.Tool{
		Name:        "list_boards",
		Description: "List the kbrd boards known to this machine (name, path, pinned). Use this to discover valid board names.",
	}, listBoards)

	mcp.AddTool(s, &mcp.Tool{
		Name:        "list_folders",
		Description: "List the folders (columns) of a kbrd board, identified by its friendly name.",
	}, listFolders)

	mcp.AddTool(s, &mcp.Tool{
		Name:        "list_files",
		Description: "List the markdown items in a kbrd board folder. The folder defaults to the board's first folder.",
	}, listFiles)

	mcp.AddTool(s, &mcp.Tool{
		Name:        "list_custom_commands",
		Description: "List the shell custom commands available for a kbrd board. Folder-local .kbrd_commands.yml commands are included only when MCP policy allows board-local commands. Lua commands are not included.",
		Annotations: &mcp.ToolAnnotations{ReadOnlyHint: true},
	}, func(ctx context.Context, req *mcp.CallToolRequest, in ListCommandsInput) (*mcp.CallToolResult, ListCommandsOutput, error) {
		return listCustomCommandsWithPolicy(ctx, req, in, policy)
	})

	mcp.AddTool(s, &mcp.Tool{
		Name:        "run_custom_command",
		Description: "DANGEROUS: run a shell custom command (by id) for a kbrd board. Requires [mcp] allow_commands = true and is disabled by --safe. Output is truncated to a bounded size.",
		Annotations: &mcp.ToolAnnotations{ReadOnlyHint: false, DestructiveHint: &falsePtr},
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
