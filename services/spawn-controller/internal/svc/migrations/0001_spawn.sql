-- spawn-controller domain schema. State lives in fsm.machines.
CREATE TABLE IF NOT EXISTS spaces (
    id       text PRIMARY KEY,
    owner    text NOT NULL,
    name     text NOT NULL,
    template text NOT NULL,
    spec     jsonb NOT NULL,
    digest   text NOT NULL,
    created  timestamptz NOT NULL DEFAULT now(),
    UNIQUE (owner, name)
);

CREATE TABLE IF NOT EXISTS runtimes (
    id         text PRIMARY KEY,
    space      text NOT NULL REFERENCES spaces(id),
    digest     text NOT NULL,
    container  text NOT NULL DEFAULT '',
    endpoint   text NOT NULL DEFAULT '',
    created    timestamptz NOT NULL DEFAULT now()
);

-- The intent registry: a row exists before docker create is called
-- (sys.worker-intent).
CREATE TABLE IF NOT EXISTS intents (
    id        text PRIMARY KEY,
    runtime   text NOT NULL,
    space     text NOT NULL,
    digest    text NOT NULL,
    image     text NOT NULL,
    kind      text NOT NULL DEFAULT 'runtime',
    created   timestamptz NOT NULL DEFAULT now()
);
