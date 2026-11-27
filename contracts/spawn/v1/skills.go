package v1

import "context"

// Skill namespaces keep layer skills and agent-authored files distinct.
const (
	LayerSkillPrefix = "layer/"
	OwnSkillPrefix   = "own/"
)

// SkillShowcaseFile is the layer skill catalog on the read-only render mount.
const SkillShowcaseFile = "skills.json"

type SkillInventoryReq struct {
	Owner  string `json:"owner"`
	Space  string `json:"space"`
	Digest string `json:"digest"`
}

type SkillInventoryResp struct {
	Entries      map[string]string `json:"entries"`
	Capabilities map[string]string `json:"capabilities"`
}

// SkillInventory captures the skills in one effective workspace.
func (c *Client) SkillInventory(ctx context.Context, req SkillInventoryReq) (SkillInventoryResp, error) {
	var out SkillInventoryResp
	err := c.call(ctx, "SkillInventory", req, &out)
	return out, err
}
