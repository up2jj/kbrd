package mcp

import (
	"context"
	"io"
	"net"
	"net/http"

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

func (c closer) Close() error { return c.srv.Close() }

// Serve starts the kbrd MCP server on a Streamable HTTP listener at addr and
// returns immediately. The returned io.Closer stops the server. A bind error
// (e.g. the port is already in use by another kbrd instance) is returned so
// the caller can warn and continue without an MCP server.
func Serve(addr string) (io.Closer, error) {
	srv := newServer()
	handler := mcp.NewStreamableHTTPHandler(func(*http.Request) *mcp.Server { return srv }, nil)

	httpSrv := &http.Server{Addr: addr, Handler: handler}
	ln, err := net.Listen("tcp", addr)
	if err != nil {
		return nil, err
	}
	go func() {
		// http.ErrServerClosed is the normal result of Close on shutdown.
		_ = httpSrv.Serve(ln)
	}()
	return closer{srv: httpSrv}, nil
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
