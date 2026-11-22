CREATE SCHEMA IF NOT EXISTS {{schema}};

CREATE TABLE IF NOT EXISTS {{schema}}.machines (
	kind text NOT NULL,
	id text NOT NULL,
	state text NOT NULL,
	deadline_at timestamptz,
	deadline_event text,
	deadline_actor text,
	actor text NOT NULL DEFAULT '',
	version bigint NOT NULL DEFAULT 1,
	updated_at timestamptz NOT NULL DEFAULT now(),
	PRIMARY KEY (kind, id)
);

CREATE INDEX IF NOT EXISTS machines_due
	ON {{schema}}.machines (deadline_at) WHERE deadline_at IS NOT NULL;

CREATE TABLE IF NOT EXISTS {{schema}}.history (
	seq bigserial PRIMARY KEY,
	kind text NOT NULL,
	id text NOT NULL,
	from_state text NOT NULL,
	event text NOT NULL,
	to_state text NOT NULL,
	actor text NOT NULL,
	at timestamptz NOT NULL DEFAULT now()
);

CREATE INDEX IF NOT EXISTS history_machine ON {{schema}}.history (kind, id, seq);

CREATE TABLE IF NOT EXISTS {{schema}}.outbox (
	seq bigserial PRIMARY KEY,
	kind text NOT NULL,
	machine_id text NOT NULL,
	topic text NOT NULL,
	payload bytea NOT NULL DEFAULT '',
	due_at timestamptz NOT NULL DEFAULT now(),
	done_at timestamptz,
	created_at timestamptz NOT NULL DEFAULT now()
);

CREATE INDEX IF NOT EXISTS outbox_due ON {{schema}}.outbox (due_at) WHERE done_at IS NULL;

CREATE TABLE IF NOT EXISTS {{schema}}.sweeper_leases (
	name text PRIMARY KEY,
	owner text NOT NULL,
	expires_at timestamptz NOT NULL
);

CREATE TABLE IF NOT EXISTS {{schema}}.watchdog_ticks (
	name text PRIMARY KEY,
	at timestamptz NOT NULL
);
