CREATE TABLE inbound_updates (
    id text PRIMARY KEY,
    channel text NOT NULL,
    update_id text NOT NULL,
    external_id text NOT NULL,
    body text NOT NULL,
    job_id text NOT NULL DEFAULT '',
    created_at timestamptz NOT NULL DEFAULT now(),
    UNIQUE(channel, update_id)
);
CREATE TABLE deliveries (
    id text PRIMARY KEY,
    external_id text NOT NULL,
    kind text NOT NULL,
    body text NOT NULL,
    created_at timestamptz NOT NULL DEFAULT now()
);
CREATE TABLE mock_outbox (
    seq bigint GENERATED ALWAYS AS IDENTITY PRIMARY KEY,
    delivery_id text NOT NULL UNIQUE REFERENCES deliveries(id),
    external_id text NOT NULL,
    kind text NOT NULL,
    body text NOT NULL,
    sent_at timestamptz NOT NULL DEFAULT now()
);
