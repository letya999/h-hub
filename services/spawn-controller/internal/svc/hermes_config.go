package svc

import (
	"context"
	"encoding/json"
	"fmt"
	"os"
	"path/filepath"

	idv1 "github.com/letya999/h-hub/contracts/identity/v1"
	thv1 "github.com/letya999/h-hub/contracts/toolhub/v1"
	"github.com/letya999/h-hub/shared/httpapi"
)

func (s *Server) projectTools(ctx context.Context, sp spaceRow, p Passports) (thv1.ProjectionResp, error) {
	if len(p.Tools.Core)+len(p.Tools.Extended) == 0 {
		return thv1.ProjectionResp{Digest: sp.digest}, nil
	}
	issuer := &idv1.Client{T: idv1.HTTP{C: httpapi.NewClient(s.deps.Identity, "")}}
	token, err := issuer.IssueServiceToken(ctx, s.sub, "tool-hub", newID("projection"), s.deps.SelfKey)
	if err != nil {
		return thv1.ProjectionResp{}, fmt.Errorf("authorize tool projection: %w", err)
	}
	hub := &thv1.Client{T: thv1.HTTP{C: httpapi.NewClient(p.Config.ToolURL, token.Token)}}
	out, err := hub.Projection(ctx, thv1.ProjectionReq{Space: sp.id, Digest: sp.digest})
	if err != nil {
		return thv1.ProjectionResp{}, fmt.Errorf("project launch tools: %w", err)
	}
	if sp.digest != out.Digest {
		return thv1.ProjectionResp{}, fmt.Errorf("launch tool projection digest changed")
	}
	return out, nil
}

// Hermes accepts JSON as YAML. Its model transport terminates at the
// adapter's private listener, where the admitted run supplies the credential.
func writeHermesConfig(dir string, projection thv1.ProjectionResp) error {
	sets := []string{}
	if len(projection.Catalog) > 0 {
		sets = append(sets, "hhub")
	}
	if err := writeJSONFile(filepath.Join(dir, "tools.json"), projection); err != nil {
		return err
	}
	return writeJSONFile(filepath.Join(dir, "config.yaml"), map[string]any{
		"model": map[string]any{"provider": "custom", "default": "hhub", "base_url": "http://127.0.0.1:8643/v1",
			"api_key": "local-adapter", "context_length": 262144},
		"gateway":           map[string]bool{"multiplex_profiles": true},
		"auxiliary":         map[string]any{"title_generation": map[string]bool{"enabled": false}},
		"platform_toolsets": map[string][]string{"api_server": sets},
		"tools":             map[string]any{"tool_search": map[string]string{"enabled": "off"}},
		"plugins":           map[string][]string{"enabled": {"hhub"}},
	})
}

func writeJSONFile(path string, value any) error {
	data, err := json.Marshal(value)
	if err != nil {
		return fmt.Errorf("encode %s: %w", filepath.Base(path), err)
	}
	if err := os.WriteFile(path, data, 0o644); err != nil {
		return fmt.Errorf("write %s: %w", filepath.Base(path), err)
	}
	return nil
}
