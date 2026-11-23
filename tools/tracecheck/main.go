package main

import (
	"fmt"
	"os"

	"github.com/letya999/h-hub/tools/gates"
)

func main() {
	if len(os.Args) != 2 {
		fmt.Fprintln(os.Stderr, "usage: tracecheck <go-test-json>")
		os.Exit(2)
	}
	f, err := os.Open(os.Args[1])
	if err == nil {
		defer f.Close() //nolint:errcheck // process exit closes the file
		err = gates.TraceExecuted(".", f)
	}
	if err != nil {
		fmt.Fprintln(os.Stderr, err)
		os.Exit(1)
	}
}
