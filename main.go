package main

import (
	"fmt"
	"os"

	"kbrd/commands"
	"kbrd/extension"
)

func main() {
	if extension.IsNativeHostInvocation(os.Args[1:]) {
		if err := extension.RunNativeHost(os.Stdin, os.Stdout); err != nil {
			fmt.Fprintf(os.Stderr, "kbrd native host: %v\n", err)
			os.Exit(1)
		}
		return
	}
	if err := commands.NewRootCmd().Execute(); err != nil {
		fmt.Fprintf(os.Stderr, "error: %v\n", err)
		os.Exit(1)
	}
}
