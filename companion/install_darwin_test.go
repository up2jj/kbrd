//go:build darwin

package companion

import (
	"bytes"
	"strings"
	"testing"
)

func TestLaunchAgentPlist(t *testing.T) {
	plist := launchAgentPlist(`/Users/A & B/Applications/kbrd Companion.app`)
	for _, want := range [][]byte{
		[]byte(`<string>dev.kbrd.companion</string>`),
		[]byte(`<string>/usr/bin/open</string>`),
		[]byte(`<string>/Users/A &amp; B/Applications/kbrd Companion.app</string>`),
		[]byte(`<key>RunAtLoad</key>`),
	} {
		if !bytes.Contains(plist, want) {
			t.Fatalf("launch agent plist missing %q:\n%s", want, plist)
		}
	}
}

func TestRunRequiresInstalledCompanion(t *testing.T) {
	t.Setenv("HOME", t.TempDir())
	_, err := Run()
	if err == nil || !strings.Contains(err.Error(), "kbrd companion install") {
		t.Fatalf("Run() error = %v, want install guidance", err)
	}
}
