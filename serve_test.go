package main

import (
	"testing"

	"github.com/spf13/pflag"
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
