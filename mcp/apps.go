package mcp

import (
	"context"
	_ "embed"

	"github.com/modelcontextprotocol/go-sdk/mcp"
)

const (
	mcpAppsExtension    = "io.modelcontextprotocol/ui"
	mcpAppHTMLMIME      = "text/html;profile=mcp-app"
	boardAppResourceURI = "ui://kbrd/board-v4.html"
)

//go:embed board_app.html
var boardAppHTML string

func mcpAppServerCapabilities() *mcp.ServerCapabilities {
	return &mcp.ServerCapabilities{
		// Preserve the SDK's default logging capability while adding MCP Apps.
		Logging: &mcp.LoggingCapabilities{},
		Extensions: map[string]any{
			mcpAppsExtension: map[string]any{
				"mimeTypes": []string{mcpAppHTMLMIME},
			},
		},
	}
}

func registerAppResources(s *mcp.Server) {
	uiMeta := map[string]any{
		"csp": map[string]any{
			"connectDomains":  []string{},
			"resourceDomains": []string{},
			"frameDomains":    []string{},
		},
		"prefersBorder": true,
	}
	s.AddResource(&mcp.Resource{
		Meta:        mcp.Meta{"ui": uiMeta},
		URI:         boardAppResourceURI,
		Name:        "kbrd_boards",
		Title:       "kbrd boards",
		Description: "Board picker for list_boards and read-only Kanban view for show_board.",
		MIMEType:    mcpAppHTMLMIME,
	}, readBoardAppResource)
}

func readBoardAppResource(_ context.Context, req *mcp.ReadResourceRequest) (*mcp.ReadResourceResult, error) {
	if req == nil || req.Params == nil || req.Params.URI != boardAppResourceURI {
		return nil, resourceNotFound(req)
	}
	return &mcp.ReadResourceResult{Contents: []*mcp.ResourceContents{{
		URI:      boardAppResourceURI,
		MIMEType: mcpAppHTMLMIME,
		Text:     boardAppHTML,
		Meta: mcp.Meta{"ui": map[string]any{
			"csp": map[string]any{
				"connectDomains":  []string{},
				"resourceDomains": []string{},
				"frameDomains":    []string{},
			},
			"prefersBorder": true,
		}},
	}}}, nil
}
