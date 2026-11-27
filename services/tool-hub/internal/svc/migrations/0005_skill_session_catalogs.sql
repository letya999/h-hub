CREATE TABLE skill_session_catalogs (
    session_key text PRIMARY KEY,
    revision text NOT NULL,
    payload jsonb NOT NULL
);
