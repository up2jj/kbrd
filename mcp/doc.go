// Package mcp exposes kbrd's board operations, read-only resources, and prompts
// to MCP (Model Context Protocol) clients over a Streamable HTTP transport. It
// runs in-process alongside the TUI but is fully decoupled from model/ and
// operates directly on the filesystem rather than the live Bubble Tea model.
// The TUI's fsnotify watcher surfaces any files the server creates.
package mcp
