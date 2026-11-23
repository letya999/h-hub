CREATE TABLE principals (
    id text PRIMARY KEY,
    space text NOT NULL UNIQUE
);
CREATE TABLE link_codes (
    hash bytea PRIMARY KEY,
    owner text NOT NULL REFERENCES principals(id),
    sealed bytea NOT NULL,
    expires_at timestamptz NOT NULL
);
CREATE UNIQUE INDEX link_codes_one_pending ON link_codes(owner);
CREATE TABLE channel_links (
    channel text NOT NULL,
    external_id text NOT NULL,
    owner text NOT NULL REFERENCES principals(id),
    PRIMARY KEY(channel, external_id)
);
