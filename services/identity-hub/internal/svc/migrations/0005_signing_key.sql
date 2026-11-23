CREATE TABLE signing_keys (
    purpose text PRIMARY KEY,
    kid text NOT NULL,
    sealed bytea NOT NULL,
    pub bytea NOT NULL
);
