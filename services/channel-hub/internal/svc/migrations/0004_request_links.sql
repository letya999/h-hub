-- request_links: the delivery half of an owner-request pair
-- (phase 10). The link records where a request was delivered and
-- until when a reply routes back; open is derived from timestamps
-- (answered_at IS NULL AND deadline_at > now()), never a flag —
-- retraction only brings the deadline forward (rule 16).
CREATE TABLE request_links (
    request_id  text        PRIMARY KEY,
    delivery_id text        NOT NULL REFERENCES deliveries(id),
    channel     text        NOT NULL,
    external_id text        NOT NULL DEFAULT '',
    chat_id     text        NOT NULL DEFAULT '',
    topic       text        NOT NULL DEFAULT '',
    owner       text        NOT NULL,
    options     jsonb       NOT NULL DEFAULT '[]',
    deadline_at timestamptz NOT NULL,
    answered_at timestamptz,
    created_at  timestamptz NOT NULL DEFAULT now()
);
