package browseredit

import (
	"io/fs"
	"strings"
	"testing"
)

func TestEmbeddedAppRequiresRecoveryChoice(t *testing.T) {
	source, err := fs.ReadFile(assets, "static/app.js")
	if err != nil {
		t.Fatal(err)
	}
	app := string(source)
	contracts := []string{
		"saveButton.disabled = recoveryPending || !writer || !doc.wysiwygSafe",
		"if (!writer || recoveryPending || !documentState) return",
		"if (loading || applyingRemote || !writer || recoveryPending) return",
		"else if (recoveryPending)",
		"setEditorReadOnly(true)",
		"showRecoveryPrompt()",
	}
	for _, contract := range contracts {
		if !strings.Contains(app, contract) {
			t.Errorf("embedded app is missing recovery gate %q", contract)
		}
	}
}

func TestEmbeddedAppCanExitCurrentTab(t *testing.T) {
	page, err := fs.ReadFile(assets, "index.html")
	if err != nil {
		t.Fatal(err)
	}
	if !strings.Contains(string(page), `<button id="exit" type="button">Exit</button>`) {
		t.Fatal("embedded page is missing the Exit button")
	}

	source, err := fs.ReadFile(assets, "static/app.js")
	if err != nil {
		t.Fatal(err)
	}
	app := string(source)
	contracts := []string{
		"async function exitEditor()",
		"await persistDraft()",
		"await request('/close', {method:'POST'})",
		"sessionStorage.removeItem(leaseKey)",
		"window.close()",
		"exitButton.addEventListener('click', () => enqueue(exitEditor))",
	}
	for _, contract := range contracts {
		if !strings.Contains(app, contract) {
			t.Errorf("embedded app is missing exit behavior %q", contract)
		}
	}
}

func TestEmbeddedAppCompletesCardLinks(t *testing.T) {
	page, err := fs.ReadFile(assets, "index.html")
	if err != nil {
		t.Fatal(err)
	}
	if !strings.Contains(string(page), `id="link-completion"`) {
		t.Fatal("embedded page is missing the card-link completion popup")
	}

	source, err := fs.ReadFile(assets, "static/app.js")
	if err != nil {
		t.Fatal(err)
	}
	app := string(source)
	contracts := []string{
		"function syncLinkCompletion()",
		"function insertLinkTarget(index = linkSelected)",
		"editor.replaceSelection(`[[${target.name}]]`, start, end)",
		"case 'ArrowDown'",
		"case 'Enter'",
		"case 'Escape'",
	}
	for _, contract := range contracts {
		if !strings.Contains(app, contract) {
			t.Errorf("embedded app is missing card-link completion behavior %q", contract)
		}
	}
}
