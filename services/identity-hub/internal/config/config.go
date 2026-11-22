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
	Keys string // HHUB_KEYS: key material dir (service.key, pubs/, edges)
}

// Env loads C from the environment; every declared variable is
// required (missing input is a refusal).
func Env() (C, error) {
	c := C{
		Sub:  os.Getenv("HHUB_SUB"),
		DSN:  os.Getenv("HHUB_DSN"),
		Keys: os.Getenv("HHUB_KEYS"),
	}
	if c.Sub == "" || c.DSN == "" || c.Keys == "" {
		return c, fmt.Errorf("config: HHUB_SUB, HHUB_DSN and HHUB_KEYS are required")
	}
	return c, nil
}
