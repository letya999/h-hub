-- task-engine domain schema. State lives only in fsm.machines; these
-- tables carry domain data, ordering and the run journal.
CREATE TABLE IF NOT EXISTS jobs (
    id           text PRIMARY KEY,
    owner        text NOT NULL,
    space        text NOT NULL,
    conversation text NOT NULL,
    input        text NOT NULL,
    idem         text NOT NULL,
    conv_seq     bigint NOT NULL,
    result       text NOT NULL DEFAULT '',
    created      timestamptz NOT NULL DEFAULT now(),
    UNIQUE (owner, idem),
    UNIQUE (owner, space, conversation, conv_seq)
);

CREATE TABLE IF NOT EXISTS conversations (
    owner        text NOT NULL,
    space        text NOT NULL,
    conversation text NOT NULL,
    next_seq     bigint NOT NULL DEFAULT 1,
    active_run   text NOT NULL DEFAULT '',
    hold_epoch   bigint NOT NULL DEFAULT 0,
    PRIMARY KEY (owner, space, conversation)
);

CREATE TABLE IF NOT EXISTS runs (
    id           text PRIMARY KEY,
    job          text NOT NULL REFERENCES jobs(id),
    owner        text NOT NULL,
    space        text NOT NULL,
    conversation text NOT NULL,
    runtime      text NOT NULL DEFAULT '',
    endpoint     text NOT NULL DEFAULT '',
    digest       text NOT NULL DEFAULT '',
    created      timestamptz NOT NULL DEFAULT now()
);

CREATE TABLE IF NOT EXISTS run_events (
    run  text NOT NULL REFERENCES runs(id),
    seq  bigint NOT NULL,
    kind text NOT NULL,
    data text NOT NULL DEFAULT '',
    at   timestamptz NOT NULL DEFAULT now(),
    PRIMARY KEY (run, seq)
);

CREATE TABLE IF NOT EXISTS holds (
    space  text PRIMARY KEY,
    active boolean NOT NULL,
    note   text NOT NULL DEFAULT '',
    epoch  bigint NOT NULL DEFAULT 0
);
