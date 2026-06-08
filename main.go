package main

import (
	"fmt"
	"os"

	"kbrd/commands"
)

func main() {
	if err := commands.NewRootCmd().Execute(); err != nil {
		fmt.Fprintf(os.Stderr, "error: %v\n", err)
		os.Exit(1)
	}
}
