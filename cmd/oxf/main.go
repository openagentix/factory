// Command oxf is the command line entry point of the factory.
package main

import (
	"fmt"
	"io"
	"os"
)

// Version is the release version. It is set at build time with -ldflags.
var Version = "0.0.0-dev"

func run(args []string, stdout, stderr io.Writer) int {
	if len(args) == 0 {
		fmt.Fprintln(stderr, "usage: oxf <command>\ncommands: version")
		return 2
	}
	switch args[0] {
	case "version":
		fmt.Fprintf(stdout, "oxf %s\n", Version)
		return 0
	default:
		fmt.Fprintf(stderr, "unknown command %q\n", args[0])
		return 2
	}
}

func main() {
	os.Exit(run(os.Args[1:], os.Stdout, os.Stderr))
}
