package mcp

import "encoding/json"

// ServerName is the key under which kbrd registers itself in an MCP client's
// configuration (.mcp.json).
const ServerName = "kbrd"

// DefaultAddr is the listen address used when none is configured.
const DefaultAddr = "127.0.0.1:7777"

// clientConfig is the .mcp.json shape understood by MCP clients (e.g. Claude
// Code): a map of server name to its connection details.
type clientConfig struct {
	MCPServers map[string]serverEntry `json:"mcpServers"`
}

type serverEntry struct {
	Type string `json:"type"`
	URL  string `json:"url"`
}

// ClientConfigJSON renders a .mcp.json that connects an MCP client to this
// server (Streamable HTTP) running at addr. A trailing newline is included.
func ClientConfigJSON(addr string) ([]byte, error) {
	if addr == "" {
		addr = DefaultAddr
	}
	cfg := clientConfig{MCPServers: map[string]serverEntry{
		ServerName: {Type: "http", URL: "http://" + addr},
	}}
	data, err := json.MarshalIndent(cfg, "", "  ")
	if err != nil {
		return nil, err
	}
	return append(data, '\n'), nil
}
