CREATE TABLE terminal_calls (
    id         text PRIMARY KEY,
    digest     text NOT NULL,
    result     jsonb,
    created    timestamptz NOT NULL DEFAULT now()
);
