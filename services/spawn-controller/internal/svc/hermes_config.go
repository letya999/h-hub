package svc

import (
	"fmt"
	"os"
	"path/filepath"
)

// Hermes accepts JSON as YAML. Its model transport terminates at the
// adapter's private listener, where the admitted run supplies the credential.
func writeHermesConfig(dir string) error {
	const config = `{
  "model":{"provider":"custom","default":"hhub","base_url":"http://127.0.0.1:8643/v1","api_key":"local-adapter","context_length":262144},
  "platform_toolsets":{"api_server":[]},
  "tools":{"tool_search":{"enabled":"off"}},
  "plugins":{"enabled":[]}
}`
	if err := os.WriteFile(filepath.Join(dir, "config.yaml"), []byte(config), 0o644); err != nil {
		return fmt.Errorf("render Hermes config: %w", err)
	}
	return nil
}
