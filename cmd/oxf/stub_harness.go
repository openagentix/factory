package main

import (
	"context"
	"flag"
	"fmt"
	"io"
	"os"

	"github.com/openagentix/factory/internal/harness"
)

// runStubHarness is the hidden "oxf stub-harness" subcommand. The local
// runner starts it as a process in place of a real harness, for tests and
// demo mode. It is not in the usage text.
func runStubHarness(args []string, stdout, stderr io.Writer) int {
	if os.Getenv("OXF_ALLOW_STUB") != "1" {
		fmt.Fprintln(stderr, "stub-harness: refused. It writes and deletes files in the directories it is given, so it runs only when OXF_ALLOW_STUB=1 is set by the local runner or a test.")
		return 2
	}
	fs := flag.NewFlagSet("stub-harness", flag.ContinueOnError)
	fs.SetOutput(stderr)
	script := fs.String("script", "", "path of the stub script")
	workspace := fs.String("workspace", "", "workspace directory")
	out := fs.String("out", "", "output directory")
	if err := fs.Parse(args); err != nil {
		return 2
	}
	if *script == "" || *workspace == "" || *out == "" {
		fmt.Fprintln(stderr, "stub-harness: --script, --workspace and --out are required")
		return 2
	}
	data, err := os.ReadFile(*script)
	if err != nil {
		fmt.Fprintf(stderr, "stub-harness: %v\n", err)
		return 2
	}
	code, err := harness.RunStub(context.Background(), harness.StubOptions{
		Script: data, Workspace: *workspace, OutDir: *out, Stdout: stdout,
	})
	if err != nil {
		fmt.Fprintf(stderr, "stub-harness: %v\n", err)
	}
	return code
}
