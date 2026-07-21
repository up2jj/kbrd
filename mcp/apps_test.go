package mcp

import (
	"strings"
	"testing"
)

func TestReadBoardAppResource(t *testing.T) {
	res, err := readBoardAppResource(t.Context(), resourceRequest(boardAppResourceURI))
	if err != nil {
		t.Fatal(err)
	}
	if len(res.Contents) != 1 {
		t.Fatalf("contents = %+v", res.Contents)
	}
	content := res.Contents[0]
	if content.URI != boardAppResourceURI || content.MIMEType != mcpAppHTMLMIME {
		t.Fatalf("content metadata = %+v", content)
	}
	if !strings.Contains(content.Text, "ui/initialize") || !strings.Contains(content.Text, "ui/notifications/tool-result") {
		t.Fatal("embedded board app is missing MCP Apps lifecycle handlers")
	}
	if !strings.Contains(content.Text, "appInfo:") || strings.Contains(content.Text, "clientInfo:") {
		t.Fatal("embedded board app does not use the MCP Apps appInfo handshake")
	}
	if !strings.Contains(content.Text, `request("tools/call"`) || !strings.Contains(content.Text, `name: "get_card"`) {
		t.Fatal("embedded board app does not fetch selected cards with get_card")
	}
	uiMeta, ok := content.Meta["ui"].(map[string]any)
	if !ok || uiMeta["prefersBorder"] != true {
		t.Fatalf("UI metadata = %#v", content.Meta)
	}
}

func TestReadBoardAppResourceRejectsOtherURI(t *testing.T) {
	if _, err := readBoardAppResource(t.Context(), resourceRequest("ui://kbrd/missing")); err == nil {
		t.Fatal("missing UI resource succeeded")
	}
}
