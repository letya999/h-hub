// Package config reads the model gateway's declared environment.
package config

import (
	"fmt"
	"os"
)

type C struct{ Sub, DSN, Keys, IdentityURL, SpawnURL string }

func Env() (C, error) {
	c := C{Sub: os.Getenv("HHUB_SUB"), DSN: os.Getenv("HHUB_DSN"), Keys: os.Getenv("HHUB_KEYS"),
		IdentityURL: os.Getenv("HHUB_IDENTITY_URL"), SpawnURL: os.Getenv("HHUB_SPAWN_URL")}
	for _, v := range []string{c.Sub, c.DSN, c.Keys, c.IdentityURL, c.SpawnURL} {
		if v == "" {
			return c, fmt.Errorf("model config: HHUB_SUB, HHUB_DSN, HHUB_KEYS, HHUB_IDENTITY_URL, HHUB_SPAWN_URL required")
		}
	}
	return c, nil
}
