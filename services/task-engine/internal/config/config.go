// Package config is the single place this service reads its declared
// environment (rule 14).
package config

import (
	"fmt"
	"os"
)

// C is the service configuration: the declared env list.
type C struct {
	Sub         string // HHUB_SUB
	DSN         string // HHUB_DSN
	Keys        string // HHUB_KEYS: service.key, pubs/, edges
	IdentityURL string // HHUB_IDENTITY_URL
	SpawnURL    string // HHUB_SPAWN_URL
	ChannelURL  string // HHUB_CHANNEL_URL
	ToolURL     string // HHUB_TOOL_URL
}

// Env loads C from the environment; every declared variable is
// required (missing input is a refusal).
func Env() (C, error) {
	c := C{
		Sub:         os.Getenv("HHUB_SUB"),
		DSN:         os.Getenv("HHUB_DSN"),
		Keys:        os.Getenv("HHUB_KEYS"),
		IdentityURL: os.Getenv("HHUB_IDENTITY_URL"),
		SpawnURL:    os.Getenv("HHUB_SPAWN_URL"),
		ChannelURL:  os.Getenv("HHUB_CHANNEL_URL"),
		ToolURL:     os.Getenv("HHUB_TOOL_URL"),
	}
	for _, v := range []string{c.Sub, c.DSN, c.Keys, c.IdentityURL, c.SpawnURL, c.ChannelURL, c.ToolURL} {
		if v == "" {
			return c, fmt.Errorf("config: HHUB_SUB, HHUB_DSN, HHUB_KEYS, " +
				"HHUB_IDENTITY_URL, HHUB_SPAWN_URL, HHUB_CHANNEL_URL and HHUB_TOOL_URL are required")
		}
	}
	return c, nil
}
