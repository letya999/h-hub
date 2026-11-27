package svc

import (
	"context"
	"fmt"
	"slices"
	"strings"

	sv1 "github.com/letya999/h-hub/contracts/spawn/v1"
)

func skillInventory(s *Server, ctx context.Context, r sv1.SkillInventoryReq) (sv1.SkillInventoryResp, error) {
	var out sv1.SkillInventoryResp
	dir, w, err := workspaceScope(s, ctx, r)
	if err != nil {
		return out, err
	}
	out.Entries = map[string]string{}
	if err := validateLayerSkills(w.Skills); err != nil {
		return out, err
	}
	for name, body := range w.Skills {
		out.Entries[sv1.LayerSkillPrefix+name] = body
	}
	out.Capabilities = make(map[string]string, len(catalog))
	for name, entry := range catalog {
		out.Capabilities[name] = entry.Summary
	}
	hasOwn := slices.ContainsFunc(w.mountNodes(), func(node sv1.WorkspaceNode) bool {
		return node.Path == ownSkillRoot
	})
	if hasOwn {
		if err := readOwnSkills(dir, out.Entries); err != nil {
			return out, err
		}
	}
	return out, nil
}

func readOwnSkills(dir string, out map[string]string) error {
	skillWrites.Lock()
	defer skillWrites.Unlock()
	root, err := openWorkspaceNode(dir, strings.TrimPrefix(ownSkillRoot, "/"))
	if err != nil {
		return err
	}
	defer root.Close() //nolint:errcheck // directory handle
	files, err := boundedDirectory(root, ".")
	if err != nil {
		return err
	}
	if len(files) > maxOwnSkills {
		return fileRefusal("err.skill-limit")
	}
	for _, file := range files {
		if strings.HasPrefix(file.Name(), ".hhub-write-") {
			continue
		}
		name, ok := ownSkillName(strings.TrimPrefix(ownSkillRoot, "/") + "/" + file.Name())
		if !ok || !file.Type().IsRegular() {
			return fileRefusal("err.skill-format")
		}
		body, err := workspaceText(root, file.Name())
		if err != nil {
			return fmt.Errorf("read own skill: %w", err)
		}
		if err := scanSkill(file.Name(), body); err != nil {
			return err
		}
		out[sv1.OwnSkillPrefix+name] = body
	}
	return nil
}
