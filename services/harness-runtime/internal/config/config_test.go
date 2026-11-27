package config

import (
	"os"
	"path/filepath"
	"strings"
	"testing"

	sv1 "github.com/letya999/h-hub/contracts/spawn/v1"
)

// A spec activated before tool calling existed renders no tool_url;
// Load must accept it so the runtime starts tool-less instead of
// refusing every turn (the N-1 restore smoke runs exactly such specs).
func TestLoadWithoutToolURL(t *testing.T) {
	t.Parallel()
	c := configFixture(t, `{"space":"s","sub":"harness-runtime","task_url":"http://task:9000",`+
		`"model_url":"http://model:9000","timeout_ms":5000}`)
	writeConfigFixture(t, filepath.Join(filepath.Dir(c.Config), sv1.SkillShowcaseFile), "{}")
	r, err := c.Load()
	if err != nil {
		t.Fatalf("spec without tool_url refused: %v", err)
	}
	if r.ToolURL != "" {
		t.Fatalf("tool_url = %q, want empty", r.ToolURL)
	}
}

func TestLoadRefusesMissingSkillShowcase(t *testing.T) {
	t.Parallel()
	c := configFixture(t, `{"space":"s","task_url":"http://task:9000","model_url":"http://model:9000","timeout_ms":5000}`)
	assertShowcaseRefused(t, c)
	for _, body := range []string{"null", "[]"} {
		writeConfigFixture(t, filepath.Join(filepath.Dir(c.Config), sv1.SkillShowcaseFile), body)
		assertShowcaseRefused(t, c)
	}
}

func assertShowcaseRefused(t *testing.T, c C) {
	t.Helper()
	if _, err := c.Load(); err == nil || !strings.Contains(err.Error(), "layer skill showcase") {
		t.Fatalf("malformed or missing skill showcase was accepted: %v", err)
	}
}

func TestLoadMissingRequired(t *testing.T) {
	t.Parallel()
	c := configFixture(t, `{"task_url":"http://task:9000"}`)
	if _, err := c.Load(); err == nil {
		t.Fatal("config without model_url and timeout_ms accepted")
	}
}

func configFixture(t *testing.T, body string) C {
	t.Helper()
	c := C{Config: filepath.Join(t.TempDir(), "config.json")}
	writeConfigFixture(t, c.Config, body)
	return c
}

func writeConfigFixture(t *testing.T, path, body string) {
	t.Helper()
	if err := os.WriteFile(path, []byte(body), 0o644); err != nil {
		t.Fatal(err)
	}
}
