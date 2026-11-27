package v1

import "context"

type SkillPromptReq struct {
	Run    string `json:"run"`
	Digest string `json:"digest"`
}

type SkillPromptResp struct {
	Text string `json:"text"`
}

// SkillPrompt resolves the budgeted catalog for the run's conversation.
func (c *Client) SkillPrompt(ctx context.Context, req SkillPromptReq) (SkillPromptResp, error) {
	var out SkillPromptResp
	err := c.call(ctx, "SkillPrompt", req, &out)
	return out, err
}
