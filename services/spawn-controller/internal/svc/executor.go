package svc

import (
	"maps"
	"slices"

	herrors "github.com/letya999/h-hub/shared/errors"
)

func validateExecutors(tools ToolsPassport) error {
	for _, name := range slices.Sorted(maps.Keys(tools.Executors)) {
		choice := tools.Executors[name]
		switch choice.Executor {
		case "toolhub":
			if !choice.AcceptNativeLimits {
				continue
			}
		case "harness":
			i := slices.IndexFunc(hermesTools, func(tool hermesTool) bool { return tool.Name == name })
			if i >= 0 {
				effect := hermesTools[i].Effect
				if effect == "nothing" || effect == "workspace" || choice.AcceptNativeLimits {
					continue
				}
				return herrors.New(herrors.CodeInvalid, "err.native-limits"). //nolint:wrapcheck // shared error form
												With("tool", name).With("effect", effect)
			}
		}
		return herrors.New(herrors.CodeInvalid, "err.invalid-arg"). //nolint:wrapcheck // shared error form
										With("tool", name).With("executor", choice.Executor)
	}
	return nil
}
