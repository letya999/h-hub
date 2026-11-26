ALTER TABLE owner_requests ADD COLUMN action_key text NOT NULL DEFAULT '';
ALTER TABLE owner_requests ADD COLUMN action_used_at timestamptz;
CREATE INDEX unused_tool_approval ON owner_requests(space,job,action_key)
    WHERE action_key <> '' AND action_used_at IS NULL;
