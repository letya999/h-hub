// scripted-model is an OpenAI-compatible deterministic test endpoint.
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
	m := &model{scripts: newScriptStore()}
	mux.HandleFunc("/healthz", func(w http.ResponseWriter, _ *http.Request) {
		w.WriteHeader(http.StatusOK)
	})
	mux.HandleFunc("/v1/chat/completions", m.completions)
	mux.HandleFunc("POST /v1/scenarios", m.loadScenario)
	mux.HandleFunc("GET /v1/scenarios/{id}/verify", m.verifyScenario)
	log.Fatal(http.ListenAndServe(":9000", mux)) //nolint:gocritic // process exit
}

func (m *model) completions(w http.ResponseWriter, r *http.Request) {
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
	if strings.HasPrefix(input, "scenario:") {
		m.scriptCompletion(w, r, input)
		return
	}
	defaultCompletion(w, r, input)
}

func defaultCompletion(w http.ResponseWriter, r *http.Request, input string) {
	// "slow:<ms> <text>" holds the turn open for kill/cancel tests;
	// every other input answers immediately.
	var ok bool
	input, ok = slowInput(r, input)
	if !ok {
		return
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

func slowInput(r *http.Request, input string) (string, bool) {
	rest, slow := strings.CutPrefix(input, "slow:")
	if !slow {
		return input, true
	}
	num, tail, found := strings.Cut(rest, " ")
	if !found {
		return input, true
	}
	d, err := strconv.Atoi(num)
	if err != nil || d <= 0 || d > 300000 {
		return input, true
	}
	select {
	case <-time.After(time.Duration(d) * time.Millisecond):
		return tail, true
	case <-r.Context().Done():
		return "", false
	}
}

func writeJSON(w http.ResponseWriter, v any) {
	w.Header().Set("Content-Type", "application/json")
	if err := json.NewEncoder(w).Encode(v); err != nil {
		log.Printf("encode: %v", err)
	}
}
