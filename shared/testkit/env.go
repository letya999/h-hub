package testkit

import "os"

// Env reads a test knob with a default; tests never read the
// environment directly (rule 20) so overrides stay in one place.
func Env(key, def string) string {
	if v := os.Getenv(key); v != "" {
		return v
	}
	return def
}
