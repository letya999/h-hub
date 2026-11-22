CREATE TABLE IF NOT EXISTS nonces (
    sub   text NOT NULL,
    nonce text NOT NULL,
    exp   timestamptz NOT NULL,
    PRIMARY KEY (sub, nonce)
);
