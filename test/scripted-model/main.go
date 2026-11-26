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
	if input == "recall my markers" {
		input = recallMarkers(req.Messages)
	}
	if strings.HasPrefix(input, "scenario:") {
		m.scriptCompletion(w, r, input)
		return
	}
	defaultCompletion(w, r, input)
}

func recallMarkers(history []message) string {
	var markers []string
	for _, m := range history {
		if value, ok := strings.CutPrefix(m.Content, "remember marker "); m.Role == "user" && ok {
			markers = append(markers, value)
		}
	}
	return "markers " + strings.Join(markers, "/")
}

func defaultCompletion(w http.ResponseWriter, r *http.Request, input string) {
	// "slow:<ms> <text>" holds the turn open for kill/cancel tests;
	// "burn:<n> <text>" reports n spent tokens for ceiling tests;
	// "nousage: <text>" omits the usage block (estimate path).
	var ok bool
	input, ok = slowInput(r, input)
	if !ok {
		return
	}
	answer, burn, nousage := scriptedAnswer(input)
	resp := map[string]any{
		"choices": []map[string]any{{
			"index":         0,
			"message":       map[string]string{"role": "assistant", "content": answer},
			"finish_reason": "stop",
		}},
	}
	if burn > 0 {
		resp["usage"] = usageOf(burn)
	} else if !nousage {
		prompt := int64(len(input)+3) / 4
		resp["usage"] = usageOf(prompt + int64(len(answer)+3)/4)
	}
	writeJSON(w, resp)
}

// scriptedAnswer strips the test directives and returns the answer
// text, the forced token spend and the omit-usage flag.
func scriptedAnswer(input string) (answer string, burn int64, nousage bool) {
	text := input
	if rest, cut := strings.CutPrefix(text, "burn:"); cut {
		if num, tail, found := strings.Cut(rest, " "); found {
			if n, err := strconv.ParseInt(num, 10, 64); err == nil && n > 0 {
				burn = n
				text = tail
			}
		}
	}
	if tail, cut := strings.CutPrefix(text, "nousage:"); cut {
		nousage = true
		text = strings.TrimSpace(tail)
	}
	return "answer: " + text, burn, nousage
}

func usageOf(total int64) map[string]int64 {
	return map[string]int64{
		"prompt_tokens":     total / 2,
		"completion_tokens": total - total/2,
		"total_tokens":      total,
	}
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
