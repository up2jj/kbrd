package mcp

import (
	"encoding/json"
	"testing"
)

func TestClientConfigJSON(t *testing.T) {
	data, err := ClientConfigJSON("127.0.0.1:9999")
	if err != nil {
		t.Fatal(err)
	}
	var cfg clientConfig
	if err := json.Unmarshal(data, &cfg); err != nil {
		t.Fatalf("unmarshal: %v", err)
	}
	srv, ok := cfg.MCPServers[ServerName]
	if !ok || srv.Type != "http" || srv.URL != "http://127.0.0.1:9999" {
		t.Fatalf("server entry = %+v", cfg.MCPServers)
	}

	// Empty addr falls back to the default.
	data, err = ClientConfigJSON("")
	if err != nil {
		t.Fatal(err)
	}
	var cfg2 clientConfig
	if err := json.Unmarshal(data, &cfg2); err != nil {
		t.Fatal(err)
	}
	if cfg2.MCPServers[ServerName].URL != "http://"+DefaultAddr {
		t.Fatalf("default url = %q", cfg2.MCPServers[ServerName].URL)
	}
}
