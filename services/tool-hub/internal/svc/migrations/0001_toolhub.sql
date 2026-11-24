-- tool-hub admission: the receipt ledger of every allow/deny
-- decision and the per-digest projection cache (phase 9, blueprint
-- 2.5). Receipts are append-only facts; the cache is a pure function
-- of one spec digest and can be rebuilt any time.
CREATE TABLE receipts (
    seq         bigserial   PRIMARY KEY,
    space       text        NOT NULL,
    run         text        NOT NULL,
    job         text        NOT NULL,
    req_digest  text        NOT NULL,
    spec_digest text        NOT NULL,
    toolset     text        NOT NULL,
    tool        text        NOT NULL,
    caps        jsonb       NOT NULL,
    allowed     boolean     NOT NULL,
    reason      text        NOT NULL DEFAULT '',
    created     timestamptz NOT NULL DEFAULT now()
);

CREATE TABLE projection_cache (
    digest     text        PRIMARY KEY,
    space      text        NOT NULL,
    projection jsonb       NOT NULL,
    created    timestamptz NOT NULL DEFAULT now()
);
