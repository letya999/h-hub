// scripted-model is the phase-3 test model: an OpenAI-compatible
// endpoint with one built-in scenario — answer the user text back
// with a fixed prefix. No LLM, no state, test infrastructure only.
package main

import (
	"encoding/json"
	"log"
	"net/http"
	"strconv"
	"strings"
	"time"
)

type message struct {
	Role    string `json:"role"`
	Content string `json:"content"`
}

func main() {
	mux := http.NewServeMux()
	mux.HandleFunc("/healthz", func(w http.ResponseWriter, _ *http.Request) {
		w.WriteHeader(http.StatusOK)
	})
	mux.HandleFunc("/v1/chat/completions", completions)
	log.Fatal(http.ListenAndServe(":9000", mux)) //nolint:gocritic // process exit
}

func completions(w http.ResponseWriter, r *http.Request) {
	var req struct {
		Model    string    `json:"model"`
		Messages []message `json:"messages"`
	}
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
		http.Error(w, "bad request", http.StatusBadRequest)
		return
	}
	input := ""
	for _, m := range req.Messages {
		if m.Role == "user" {
			input = m.Content
		}
	}
	// "slow:<ms> <text>" holds the turn open for kill/cancel tests;
	// every other input answers immediately.
	if rest, ok := strings.CutPrefix(input, "slow:"); ok {
		if num, tail, found := strings.Cut(rest, " "); found {
			if d, err := strconv.Atoi(num); err == nil && d > 0 && d <= 300000 {
				select {
				case <-time.After(time.Duration(d) * time.Millisecond):
				case <-r.Context().Done():
					return
				}
				input = tail
			}
		}
	}
	writeJSON(w, map[string]any{
		"choices": []map[string]any{{
			"index": 0,
			"message": map[string]string{
				"role":    "assistant",
				"content": "answer: " + input,
			},
			"finish_reason": "stop",
		}},
	})
}

func writeJSON(w http.ResponseWriter, v any) {
	w.Header().Set("Content-Type", "application/json")
	if err := json.NewEncoder(w).Encode(v); err != nil {
		log.Printf("encode: %v", err)
	}
}
