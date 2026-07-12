package mcp

import (
	"context"
	"errors"
	"fmt"
	"io"
	"net"
	"net/http"
	"os"
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

// newServer builds the MCP server with all kbrd tools registered.
func newServer() *mcp.Server {
	s := mcp.NewServer(&mcp.Implementation{Name: "kbrd", Version: version}, nil)

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
		Description: "List the shell custom commands available for a kbrd board (from commands.yml and the board's .kbrd_commands.yml). Lua commands are not included.",
	}, listCustomCommands)

	mcp.AddTool(s, &mcp.Tool{
		Name:        "run_custom_command",
		Description: "Run a shell custom command (by id) for a kbrd board. Provide a folder/item if the command uses file variables. Returns the combined output and exit code.",
	}, runCustomCommand)

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
	mcpWriteTimeout      = 30 * time.Second
	mcpIdleTimeout       = 120 * time.Second
	mcpShutdownTimeout   = 5 * time.Second
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
func Serve(addr string) (io.Closer, error) {
	ln, err := net.Listen("tcp", addr)
	if err != nil {
		return nil, err
	}
	return serveListener(ln), nil
}

// serveListener starts the MCP server on an already-bound listener. Keeping
// the bind separate lets tests request an OS-assigned port without a race.
func serveListener(ln net.Listener) io.Closer {
	srv := newServer()
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
func Start(version, addr string) (io.Closer, bool) {
	SetVersion(version)
	c, err := Serve(addr)
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
	c, err := Serve(addr)
	if err != nil {
		return err
	}
	<-ctx.Done()
	return c.Close()
}
