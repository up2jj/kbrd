package commands

import (
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"testing"

	"github.com/spf13/pflag"

	fsutil "kbrd/fs"
)

func TestResolveOpt(t *testing.T) {
	const envKey = "KBRD_TEST_RESOLVE"

	cases := []struct {
		name    string
		flagVal string // set via fl.Set when non-nil
		setFlag bool
		env     string
		toml    string
		def     string
		want    string
	}{
		{name: "default only", def: ":8080", want: ":8080"},
		{name: "toml beats default", toml: ":9090", def: ":8080", want: ":9090"},
		{name: "env beats toml", env: ":7070", toml: ":9090", def: ":8080", want: ":7070"},
		{name: "flag beats env", setFlag: true, flagVal: ":6060", env: ":7070", toml: ":9090", def: ":8080", want: ":6060"},
		{name: "explicit empty flag beats all", setFlag: true, flagVal: "", env: ":7070", toml: ":9090", def: ":8080", want: ""},
		{name: "empty env falls through to toml", env: "", toml: ":9090", def: ":8080", want: ":9090"},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			t.Setenv(envKey, tc.env)
			fl := pflag.NewFlagSet("test", pflag.ContinueOnError)
			val := fl.String("addr", "", "")
			if tc.setFlag {
				if err := fl.Set("addr", tc.flagVal); err != nil {
					t.Fatalf("set flag: %v", err)
				}
			}
			got := resolveOpt(fl, "addr", *val, envKey, tc.toml, tc.def)
			if got != tc.want {
				t.Fatalf("resolveOpt: got %q want %q", got, tc.want)
			}
		})
	}
}

func TestServe_RejectsMCPFlags(t *testing.T) {
	for _, args := range [][]string{
		{"serve", "--mcp"},
		{"serve", "--mcp-addr", "127.0.0.1:7777"},
	} {
		root := NewRootCmd()
		root.SetArgs(args)
		if err := root.Execute(); err == nil {
			t.Errorf("%v: expected error", args)
		} else if !strings.Contains(err.Error(), "not supported with serve") {
			t.Errorf("%v: error = %q, want it to mention 'not supported with serve'", args, err)
		}
	}
}

func TestInitBoardPushesBootConflictCopy(t *testing.T) {
	requireGit(t)
	isolateConfig(t)
	root := t.TempDir()
	bare := filepath.Join(root, "remote.git")
	local := filepath.Join(root, "local")
	other := filepath.Join(root, "other")
	gitRun(t, root, "init", "--bare", bare)
	gitRun(t, root, "clone", bare, local)
	if err := os.WriteFile(filepath.Join(local, "seed.md"), []byte("base\n"), 0o644); err != nil {
		t.Fatal(err)
	}
	if _, err := fsutil.GitCommitAll(local, "seed", "test", "test@example.com"); err != nil {
		t.Fatal(err)
	}
	if err := fsutil.GitPush(local); err != nil {
		t.Fatal(err)
	}
	gitRun(t, root, "clone", bare, other)
	if err := os.WriteFile(filepath.Join(other, "seed.md"), []byte("theirs\n"), 0o644); err != nil {
		t.Fatal(err)
	}
	if _, err := fsutil.GitCommitAll(other, "their edit", "test", "test@example.com"); err != nil {
		t.Fatal(err)
	}
	if err := fsutil.GitPush(other); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(local, "seed.md"), []byte("ours\n"), 0o644); err != nil {
		t.Fatal(err)
	}
	if _, err := fsutil.GitCommitAll(local, "our edit", "test", "test@example.com"); err != nil {
		t.Fatal(err)
	}

	if _, err := initBoard(local, "", "server-1", false, func(string) {}); err != nil {
		t.Fatalf("initBoard: %v", err)
	}
	if _, err := os.Stat(filepath.Join(local, "seed (conflict server-1).md")); err != nil {
		t.Fatalf("local conflict copy missing: %v", err)
	}
	out, err := exec.Command("git", "-C", bare, "ls-tree", "--name-only", "HEAD").Output()
	if err != nil || !strings.Contains(string(out), "seed (conflict server-1).md") {
		t.Fatalf("boot conflict copy was not pushed: out=%q err=%v", out, err)
	}
}
