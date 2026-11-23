// Package config reads the channel service's declared environment.
package config

import (
	"fmt"
	"os"
)

type C struct{ Sub, DSN, Keys, IdentityURL, TaskURL, SpawnURL string }

func Env() (C, error) {
	c := C{Sub: os.Getenv("HHUB_SUB"), DSN: os.Getenv("HHUB_DSN"), Keys: os.Getenv("HHUB_KEYS"),
		IdentityURL: os.Getenv("HHUB_IDENTITY_URL"), TaskURL: os.Getenv("HHUB_TASK_URL"),
		SpawnURL: os.Getenv("HHUB_SPAWN_URL")}
	for _, v := range []string{c.Sub, c.DSN, c.Keys, c.IdentityURL, c.TaskURL, c.SpawnURL} {
		if v == "" {
			return c, fmt.Errorf("channel config: HHUB_SUB, HHUB_DSN, HHUB_KEYS, HHUB_IDENTITY_URL, HHUB_TASK_URL, HHUB_SPAWN_URL required")
		}
	}
	return c, nil
}
