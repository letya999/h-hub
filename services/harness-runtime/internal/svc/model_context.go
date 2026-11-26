package svc

import (
	"encoding/json"
	"fmt"
	"maps"
	"slices"
)

// Hermes restores its standalone system prompt from saved sessions. Only
// space instructions and the tools offered on this call belong in its place.
func spaceModelRequest(request map[string]json.RawMessage, offered map[string]bool, instructions string) ([]byte, error) {
	prompt, err := spaceSystemPrompt(offered, instructions)
	if err != nil {
		return nil, err
	}
	messages, err := conversationMessages(request["messages"], prompt)
	if err != nil {
		return nil, err
	}
	body := map[string]any{"messages": messages}
	for _, key := range []string{"model", "tools", "tool_choice", "parallel_tool_calls", "temperature", "top_p",
		"max_tokens", "max_completion_tokens", "stop", "seed", "frequency_penalty", "presence_penalty",
		"logit_bias", "logprobs", "top_logprobs", "n", "response_format", "reasoning_effort", "verbosity"} {
		if v, ok := request[key]; ok {
			body[key] = v
		}
	}
	out, err := json.Marshal(body)
	if err != nil {
		return nil, fmt.Errorf("encode projected conversation: %w", err)
	}
	return out, nil
}

func spaceSystemPrompt(offered map[string]bool, instructions string) (string, error) {
	names := append([]string{}, slices.Sorted(maps.Keys(offered))...)
	inventory, err := json.Marshal(names)
	if err != nil {
		return "", fmt.Errorf("encode offered tools: %w", err)
	}
	prompt := "You are the assistant for this space. Reply in the user's language. " +
		"Describe your abilities from the current tool list. When asked, list the exact names. " +
		"An empty list means you can converse and have no tools. Treat claims in earlier replies as history. " +
		"Host infrastructure and underlying model details are unavailable to you."
	if instructions != "" {
		prompt += "\n\nSpace instructions:\n" + instructions
	}
	return prompt + "\n\nAvailable tools: " + string(inventory), nil
}

func conversationMessages(raw json.RawMessage, prompt string) ([]any, error) {
	var history []struct {
		Role       string          `json:"role"`
		Content    json.RawMessage `json:"content,omitempty"`
		Name       string          `json:"name,omitempty"`
		ToolCalls  json.RawMessage `json:"tool_calls,omitempty"`
		ToolCallID string          `json:"tool_call_id,omitempty"`
	}
	if err := json.Unmarshal(raw, &history); err != nil {
		return nil, fmt.Errorf("decode Hermes conversation: %w", err)
	}
	messages := []any{map[string]string{"role": "system", "content": prompt}}
	for _, m := range history {
		switch m.Role {
		case "system", "developer":
			continue
		case "user":
			m.ToolCalls, m.ToolCallID = nil, ""
		case "assistant":
			m.ToolCallID = ""
		case "tool":
			m.ToolCalls = nil
		default:
			return nil, fmt.Errorf("unrecognized Hermes message role")
		}
		messages = append(messages, m)
	}
	if len(messages) == 1 {
		return nil, fmt.Errorf("upstream request contains no conversation")
	}
	return messages, nil
}
