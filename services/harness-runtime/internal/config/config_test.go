package config

import (
	"os"
	"path/filepath"
	"testing"
)

// A spec activated before tool calling existed renders no tool_url;
// Load must accept it so the runtime starts tool-less instead of
// refusing every turn (the N-1 restore smoke runs exactly such specs).
func TestLoadWithoutToolURL(t *testing.T) {
	t.Parallel()
	path := filepath.Join(t.TempDir(), "config.json")
	body := []byte(`{"sub":"harness-runtime","task_url":"http://task:9000",` +
		`"model_url":"http://model:9000","timeout_ms":5000}`)
	if err := os.WriteFile(path, body, 0o644); err != nil {
		t.Fatal(err)
	}
	r, err := (C{Config: path}).Load()
	if err != nil {
		t.Fatalf("spec without tool_url refused: %v", err)
	}
	if r.ToolURL != "" {
		t.Fatalf("tool_url = %q, want empty", r.ToolURL)
	}
}

func TestLoadMissingRequired(t *testing.T) {
	t.Parallel()
	path := filepath.Join(t.TempDir(), "config.json")
	if err := os.WriteFile(path, []byte(`{"task_url":"http://task:9000"}`), 0o644); err != nil {
		t.Fatal(err)
	}
	if _, err := (C{Config: path}).Load(); err == nil {
		t.Fatal("config without model_url and timeout_ms accepted")
	}
}
