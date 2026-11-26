package svc

import (
	"encoding/json"
	"fmt"
	"net/http"
)

// The model contract returns an accounted completion. Hermes's streaming client
// consumes the same provider message in the OpenAI chunk envelope.
func writeCompletion(w http.ResponseWriter, response json.RawMessage, stream bool) error {
	if !stream {
		w.Header().Set("Content-Type", "application/json")
		if _, err := w.Write(response); err != nil {
			return fmt.Errorf("write model response: %w", err)
		}
		return nil
	}
	var completion struct {
		ID      string          `json:"id"`
		Model   string          `json:"model"`
		Created int64           `json:"created"`
		Usage   json.RawMessage `json:"usage"`
		Choices []struct {
			Index   int                        `json:"index"`
			Message map[string]json.RawMessage `json:"message"`
			Finish  json.RawMessage            `json:"finish_reason"`
		} `json:"choices"`
	}
	if err := json.Unmarshal(response, &completion); err != nil {
		return fmt.Errorf("decode provider completion: %w", err)
	}
	if len(completion.Choices) == 0 {
		return fmt.Errorf("provider completion has no choices")
	}
	chunk := map[string]any{"id": completion.ID, "object": "chat.completion.chunk", "model": completion.Model, "created": completion.Created}
	var deltas, finishes []map[string]any
	for _, choice := range completion.Choices {
		if err := indexToolCalls(choice.Message); err != nil {
			return err
		}
		deltas = append(deltas, map[string]any{"index": choice.Index, "delta": choice.Message, "finish_reason": nil})
		finishes = append(finishes, map[string]any{"index": choice.Index, "delta": struct{}{}, "finish_reason": choice.Finish})
	}
	w.Header().Set("Content-Type", "text/event-stream")
	for _, choices := range [][]map[string]any{deltas, finishes} {
		chunk["choices"] = choices
		if err := writeChunk(w, chunk); err != nil {
			return err
		}
	}
	if len(completion.Usage) != 0 {
		chunk["choices"], chunk["usage"] = []any{}, completion.Usage
		if err := writeChunk(w, chunk); err != nil {
			return err
		}
	}
	if _, err := fmt.Fprint(w, "data: [DONE]\n\n"); err != nil {
		return fmt.Errorf("finish model stream: %w", err)
	}
	return nil
}

func indexToolCalls(message map[string]json.RawMessage) error {
	raw, ok := message["tool_calls"]
	if !ok {
		return nil
	}
	var calls []map[string]any
	if err := json.Unmarshal(raw, &calls); err != nil {
		return fmt.Errorf("decode provider tool calls: %w", err)
	}
	for i, call := range calls {
		call["index"] = i
	}
	indexed, err := json.Marshal(calls)
	if err != nil {
		return fmt.Errorf("encode provider tool calls: %w", err)
	}
	message["tool_calls"] = indexed
	return nil
}

func writeChunk(w http.ResponseWriter, chunk map[string]any) error {
	raw, err := json.Marshal(chunk)
	if err != nil {
		return fmt.Errorf("encode provider chunk: %w", err)
	}
	if _, err := fmt.Fprintf(w, "data: %s\n\n", raw); err != nil {
		return fmt.Errorf("write provider chunk: %w", err)
	}
	return nil
}
