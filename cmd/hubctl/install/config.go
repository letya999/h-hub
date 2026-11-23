// Package install holds the only hubctl commands that may touch
// Postgres directly (rules 10, 14): `init` provisions and `doctor`
// verifies an installation, both under the install role.
package install

import (
	"fmt"
	"os"
	"strings"
)

// Config is the declared env list of the install commands (rule 14).
// This is the only package of hubctl that reads the environment.
type Config struct {
	AdminDSN   string // HHUB_ADMIN_DSN: install-role postgres DSN
	ConfigDir  string // HHUB_CONFIG_DIR: install config dir outside volumes
	Descriptor string // HHUB_DESCRIPTOR: path to deploy/descriptor.yaml
	KEKDir     string // HHUB_KEK_DIR: host bind outside Docker volumes
}

// Env loads Config from the environment.
func Env() (Config, error) {
	c := Config{
		AdminDSN:   os.Getenv("HHUB_ADMIN_DSN"),
		ConfigDir:  os.Getenv("HHUB_CONFIG_DIR"),
		Descriptor: os.Getenv("HHUB_DESCRIPTOR"),
		KEKDir:     os.Getenv("HHUB_KEK_DIR"),
	}
	if c.ConfigDir == "" {
		c.ConfigDir = "/etc/hhub"
	}
	if c.Descriptor == "" {
		c.Descriptor = "deploy/descriptor.yaml"
	}
	if c.KEKDir == "" {
		c.KEKDir = "/run/hhub-secrets"
	}
	if c.AdminDSN == "" {
		return c, fmt.Errorf("install: HHUB_ADMIN_DSN is required")
	}
	return c, nil
}

// dbName maps a service name to its database/role identifier.
func dbName(service string) string {
	return strings.ReplaceAll(service, "-", "_")
}
