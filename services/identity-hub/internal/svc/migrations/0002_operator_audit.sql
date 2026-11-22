CREATE TABLE IF NOT EXISTS operator_audit (
    id          bigint GENERATED ALWAYS AS IDENTITY PRIMARY KEY,
    owner       text NOT NULL,
    audience    text NOT NULL,
    operation   text NOT NULL,
    at          timestamptz NOT NULL DEFAULT now()
);
