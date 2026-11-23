// Package config reads credential-broker's declared env (rule 14).
package config

import (
	"fmt"
	"os"
)

type Cfg struct {
	Sub     string // HHUB_SUB
	Keys    string // HHUB_KEYS: service keys, pubs and edges
	DBPath  string // HHUB_DB_PATH: the SQLCipher file inside the volume
	DBKey   string // HHUB_DB_KEY: path to the hex database key (outside volumes)
	KEKPath string // HHUB_KEK_PATH: envelope KEK path (outside volumes)
}

func Env() (Cfg, error) {
	var c Cfg
	missing := ""
	for _, e := range []struct {
		name string
		dst  *string
	}{
		{"HHUB_SUB", &c.Sub}, {"HHUB_KEYS", &c.Keys},
		{"HHUB_DB_PATH", &c.DBPath}, {"HHUB_DB_KEY", &c.DBKey},
		{"HHUB_KEK_PATH", &c.KEKPath},
	} {
		*e.dst = os.Getenv(e.name)
		if *e.dst == "" && missing == "" {
			missing = e.name
		}
	}
	if missing != "" {
		return c, fmt.Errorf("config: missing %s", missing)
	}
	return c, nil
}
