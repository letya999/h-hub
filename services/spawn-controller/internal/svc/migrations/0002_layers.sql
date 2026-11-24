-- Full-form passports: immutable layer revisions, content-addressed
-- effective specs with explain, and the activation log that anchors
-- rollback (phase 8).
CREATE TABLE layer_revisions (
    id      text PRIMARY KEY,
    space   text NOT NULL REFERENCES spaces(id),
    role    text NOT NULL,
    seq     integer NOT NULL,
    docs    jsonb NOT NULL,
    author  text NOT NULL,
    created timestamptz NOT NULL,
    UNIQUE (space, role, seq)
);
CREATE TABLE effective_specs (
    space   text NOT NULL REFERENCES spaces(id),
    digest  text NOT NULL,
    spec    jsonb NOT NULL,
    explain jsonb NOT NULL,
    created timestamptz NOT NULL,
    PRIMARY KEY (space, digest)
);
CREATE TABLE activations (
    id        text PRIMARY KEY,
    space     text NOT NULL REFERENCES spaces(id),
    seq       integer NOT NULL,
    digest    text NOT NULL,
    revisions jsonb NOT NULL,
    effect    text NOT NULL,
    author    text NOT NULL,
    created   timestamptz NOT NULL,
    UNIQUE (space, seq)
);
