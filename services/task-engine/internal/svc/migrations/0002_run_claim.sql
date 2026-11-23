ALTER TABLE runs ADD COLUMN claim_released_at timestamptz;

UPDATE runs r SET claim_released_at=now()
WHERE NOT EXISTS (
    SELECT 1 FROM conversations c
    WHERE c.owner=r.owner AND c.space=r.space
      AND c.conversation=r.conversation AND c.active_run=r.id
);

CREATE UNIQUE INDEX runs_one_claim_per_conversation
ON runs (owner, space, conversation)
WHERE claim_released_at IS NULL;
