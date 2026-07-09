package mcpsetup

import (
	"os"
	"os/exec"
	"strings"

	"kbrd/config"
	"kbrd/mcp"
)

const (
	ClientCodex  = "codex"
	ClientClaude = "claude"

	StatusConfigured = "configured"
	StatusEnabled    = "enabled"
	StatusFound      = "found"
	StatusSkipped    = "skipped"
)

type Options struct {
	Addr       string
	Clients    []string
	DryRun     bool
	Force      bool
	EnableKBRD bool

	LookPath func(string) (string, error)
	Run      func(string, ...string) error
}

type Result struct {
	Target string
	Status string
	Path   string
	Detail string
}

type runContext struct {
	addr string
	opts Options
}

type clientAdapter interface {
	Name() string
	Executable() string
	Configure(runContext, string) (Result, error)
}

var clientAdapters = []clientAdapter{
	codexAdapter{},
	claudeAdapter{},
}

func Run(opts Options) ([]Result, error) {
	opts = normalizeOptions(opts)
	addr, err := resolveAddr(opts.Addr)
	if err != nil {
		return nil, err
	}
	ctx := runContext{addr: addr, opts: opts}

	var results []Result
	if opts.EnableKBRD {
		res, err := configureKBRD(addr, opts.DryRun)
		if err != nil {
			return results, err
		}
		results = append(results, res)
	}

	clients := opts.Clients
	if len(clients) == 0 {
		clients = SupportedClients()
	}
	for _, name := range dedupeClients(clients) {
		adapter, ok := findClientAdapter(name)
		if !ok {
			results = append(results, Result{Target: name, Status: StatusSkipped, Detail: "unsupported client"})
			continue
		}
		binary, lookErr := opts.LookPath(adapter.Executable())
		if lookErr != nil {
			results = append(results, Result{Target: adapter.Name(), Status: StatusSkipped, Detail: adapter.Executable() + " not found on PATH"})
			continue
		}
		res, err := adapter.Configure(ctx, binary)
		if err != nil {
			return results, err
		}
		results = append(results, res)
	}
	return results, nil
}

func SupportedClients() []string {
	clients := make([]string, 0, len(clientAdapters))
	for _, adapter := range clientAdapters {
		clients = append(clients, adapter.Name())
	}
	return clients
}

func normalizeOptions(opts Options) Options {
	if opts.LookPath == nil {
		opts.LookPath = exec.LookPath
	}
	if opts.Run == nil {
		opts.Run = func(name string, args ...string) error {
			cmd := exec.Command(name, args...)
			cmd.Stdout = os.Stdout
			cmd.Stderr = os.Stderr
			return cmd.Run()
		}
	}
	return opts
}

func resolveAddr(addr string) (string, error) {
	if addr != "" {
		return addr, nil
	}
	cfg, err := config.Load("")
	if err != nil {
		return "", err
	}
	if cfg.MCP.Addr != "" {
		return cfg.MCP.Addr, nil
	}
	return mcp.DefaultAddr, nil
}

func findClientAdapter(name string) (clientAdapter, bool) {
	for _, adapter := range clientAdapters {
		if adapter.Name() == name {
			return adapter, true
		}
	}
	return nil, false
}

func dedupeClients(clients []string) []string {
	seen := make(map[string]bool, len(clients))
	out := make([]string, 0, len(clients))
	for _, client := range clients {
		client = strings.ToLower(strings.TrimSpace(client))
		if client == "" || seen[client] {
			continue
		}
		seen[client] = true
		out = append(out, client)
	}
	return out
}
