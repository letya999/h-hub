package v1

import "slices"

// Selection is the tools passport's selection and executor policy.
type Selection struct {
	Core      []string                `json:"core"`
	Extended  []string                `json:"extended"`
	Executors map[string]ToolExecutor `json:"executors,omitempty"`
}

// ToolExecutor names the executor and the owner's acceptance of
// delayed native revocation and the absence of per-call audit.
type ToolExecutor struct {
	Executor           string `json:"executor"`
	AcceptNativeLimits bool   `json:"accept_native_limits,omitempty"`
}

func (s Selection) Names() []string { return slices.Concat(s.Core, s.Extended) }

func (s Selection) Executor(name string) string {
	if executor, exists := s.Executors[name]; exists {
		return executor.Executor
	}
	return "toolhub"
}
