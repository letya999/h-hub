// hubctl is the thin operator CLI (rule 10). Only the install package
// commands open Postgres; everything else is a contract client.
package main

import (
	"context"
	"fmt"
	"os"

	"github.com/letya999/h-hub/cmd/hubctl/install"
)

func main() {
	os.Exit(run(os.Args))
}

func stderr(a ...any) {
	fmt.Fprintln(os.Stderr, a...)
}

func run(args []string) int {
	if len(args) != 2 || (args[1] != "init" && args[1] != "doctor") {
		stderr("usage: hubctl init|doctor")
		return 2
	}
	cfg, err := install.Env()
	if err != nil {
		stderr(err)
		return 2
	}
	ctx := context.Background()
	if args[1] == "init" {
		err = install.Init(ctx, cfg, os.Stdout)
	} else {
		err = install.Doctor(ctx, cfg, os.Stdout)
	}
	if err != nil {
		stderr(err)
		return 1
	}
	return 0
}
