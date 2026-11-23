// hubctl is the thin operator CLI (rule 10). Only the install package
// commands open Postgres; everything else is a contract client.
package main

import (
	"context"
	"fmt"
	"os"

	"github.com/letya999/h-hub/cmd/hubctl/chat"
	"github.com/letya999/h-hub/cmd/hubctl/install"
	"github.com/letya999/h-hub/cmd/hubctl/job"
	"github.com/letya999/h-hub/cmd/hubctl/user"
)

func main() {
	os.Exit(run(os.Args))
}

func stderr(a ...any) {
	fmt.Fprintln(os.Stderr, a...)
}

func run(args []string) int {
	if len(args) < 2 {
		stderr("usage: hubctl init|doctor|job|user|chat ...")
		return 2
	}
	ctx := context.Background()
	var err error
	switch args[1] {
	case "init", "doctor":
		return runInstall(ctx, args)
	case "job":
		err = job.Run(ctx, args[2:], os.Stdout)
	case "user":
		err = user.Run(ctx, args[2:], os.Stdout)
	case "chat":
		err = chat.Run(ctx, args[2:], os.Stdout)
	default:
		stderr("usage: hubctl init|doctor|job|user|chat ...")
		return 2
	}
	if err != nil {
		stderr(err)
		return 1
	}
	return 0
}

func runInstall(ctx context.Context, args []string) int {
	if len(args) != 2 {
		stderr("usage: hubctl " + args[1])
		return 2
	}
	cfg, err := install.Env()
	if err != nil {
		stderr(err)
		return 2
	}
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
