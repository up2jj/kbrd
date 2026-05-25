// Package mcp exposes kbrd's board operations to MCP (Model Context Protocol)
// clients over a Streamable HTTP transport. It runs in-process alongside the
// TUI but is fully decoupled: it depends only on kbrd/board (and the stdlib) —
// never on model/ — and operates directly on the filesystem rather than the
// live Bubble Tea model. The TUI's fsnotify watcher surfaces any files the
// server creates. The whole subsystem can be removed by deleting its wire-up
// in main.go.
package mcp
