//go:build darwin

package reminders

import (
	"strings"
	"testing"
)

func TestRemindersScriptParsesWithoutOpeningReminders(t *testing.T) {
	err := runRemindersScript(t.Context(), scriptRequest{Op: "syntax-check", List: "unused"}, nil)
	if err == nil || !strings.Contains(err.Error(), "Unsupported operation: syntax-check") {
		t.Fatalf("unexpected osascript result: %v", err)
	}
}
