// Command descriptor validates deploy/descriptor.yaml and renders or
// checks deploy/compose.yaml. Usage: generate | check.
package main

import (
	"bytes"
	"fmt"
	"os"

	"github.com/letya999/h-hub/internal/descriptor"
)

const (
	descPath    = "deploy/descriptor.yaml"
	composePath = "deploy/compose.yaml"
)

func main() {
	os.Exit(run(os.Args))
}

func stderr(a ...any) {
	fmt.Fprintln(os.Stderr, a...)
}

func run(args []string) int {
	if len(args) != 2 || (args[1] != "generate" && args[1] != "check") {
		stderr("usage: descriptor generate|check")
		return 2
	}
	gen, err := build(descPath)
	if err != nil {
		stderr(err)
		return 1
	}
	if args[1] == "generate" {
		if err := os.WriteFile(composePath, gen, 0o644); err != nil {
			stderr(err)
			return 1
		}
		return 0
	}
	return compare(gen)
}

func build(path string) ([]byte, error) {
	f, err := descriptor.Load(path)
	if err != nil {
		return nil, fmt.Errorf("descriptor: %w", err)
	}
	if err := f.EdgeTargets(); err != nil {
		return nil, fmt.Errorf("descriptor: %w", err)
	}
	out, err := f.Compose()
	if err != nil {
		return nil, fmt.Errorf("descriptor: %w", err)
	}
	return out, nil
}

func compare(gen []byte) int {
	cur, err := os.ReadFile(composePath)
	if err != nil || !bytes.Equal(cur, gen) {
		stderr("deploy/compose.yaml is stale: run tools/descriptor generate")
		return 1
	}
	return 0
}
