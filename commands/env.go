package commands

import (
	"os"
	"strings"

	"github.com/spf13/pflag"
)

// isTruthyEnv reports whether the named env var is set to a truthy value
// (1/true/yes/on, case-insensitive). Used for boolean opt-ins that, unlike the
// string flags, have no flag-presence signal to distinguish "" from unset.
func isTruthyEnv(key string) bool {
	switch strings.ToLower(strings.TrimSpace(os.Getenv(key))) {
	case "1", "true", "yes", "on":
		return true
	}
	return false
}

// envDefault fills v from env when the flag was not set.
func envDefault(v, envKey, def string) string {
	if v != "" {
		return v
	}
	if e := os.Getenv(envKey); e != "" {
		return e
	}
	return def
}

// resolveOpt returns the first set value in the serve precedence chain:
// flag > env > toml > default. Flag presence is checked via Changed so an
// explicit `--addr ""` still beats the other layers (envDefault can't tell
// "empty flag" from "no flag", which is fine elsewhere but wrong once a TOML
// layer sits underneath).
func resolveOpt(fl *pflag.FlagSet, name, flagVal, envKey, tomlVal, def string) string {
	if fl.Changed(name) {
		return flagVal
	}
	if e := os.Getenv(envKey); e != "" {
		return e
	}
	if tomlVal != "" {
		return tomlVal
	}
	return def
}
