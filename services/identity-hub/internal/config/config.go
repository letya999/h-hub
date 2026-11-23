// Package config is the single place this service reads its declared
// environment (rule 14).
package config

import (
	"fmt"
	"os"
)

// C is the service configuration: the declared env list.
type C struct {
	Sub  string // HHUB_SUB: this service's subject name
	DSN  string // HHUB_DSN: its database
	Keys string // HHUB_KEYS: public keys and edges
	KEK  string // HHUB_KEK_PATH: host bind, outside Docker volumes
}

// Env loads C from the environment; every declared variable is
// required (missing input is a refusal).
func Env() (C, error) {
	c := C{
		Sub:  os.Getenv("HHUB_SUB"),
		DSN:  os.Getenv("HHUB_DSN"),
		Keys: os.Getenv("HHUB_KEYS"),
		KEK:  os.Getenv("HHUB_KEK_PATH"),
	}
	if c.Sub == "" || c.DSN == "" || c.Keys == "" || c.KEK == "" {
		return c, fmt.Errorf("config: HHUB_SUB, HHUB_DSN, HHUB_KEYS and HHUB_KEK_PATH are required")
	}
	return c, nil
}
