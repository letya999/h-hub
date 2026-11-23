CREATE TABLE operator_alerts (
    key text PRIMARY KEY,
    code text NOT NULL,
    created_at timestamptz NOT NULL DEFAULT now()
);
