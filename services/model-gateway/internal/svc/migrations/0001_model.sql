-- model-gateway accounting: daily counters, per-run counters and the
-- spend event ledger (phase 6, blueprint 2.14).
CREATE TABLE usage_daily (
    owner    text   NOT NULL,
    day      date   NOT NULL,
    spent    bigint NOT NULL DEFAULT 0,
    reserved bigint NOT NULL DEFAULT 0,
    PRIMARY KEY (owner, day)
);

CREATE TABLE usage_runs (
    run         text        PRIMARY KEY,
    owner       text        NOT NULL,
    day         date        NOT NULL DEFAULT current_date,
    spent       bigint      NOT NULL DEFAULT 0,
    reserved    bigint      NOT NULL DEFAULT 0,
    reserved_at timestamptz NOT NULL DEFAULT now()
);

CREATE TABLE spend_events (
    seq      bigserial    PRIMARY KEY,
    run      text         NOT NULL,
    owner    text         NOT NULL,
    route    text         NOT NULL,
    reserved bigint       NOT NULL,
    charged  bigint       NOT NULL,
    measured boolean      NOT NULL,
    created  timestamptz  NOT NULL DEFAULT now()
);
