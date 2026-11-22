CREATE SCHEMA IF NOT EXISTS fsm;

CREATE TABLE IF NOT EXISTS fsm.machines (
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

CREATE INDEX IF NOT EXISTS fsm_machines_due
	ON fsm.machines (deadline_at) WHERE deadline_at IS NOT NULL;

CREATE TABLE IF NOT EXISTS fsm.history (
	seq bigserial PRIMARY KEY,
	kind text NOT NULL,
	id text NOT NULL,
	from_state text NOT NULL,
	event text NOT NULL,
	to_state text NOT NULL,
	actor text NOT NULL,
	at timestamptz NOT NULL DEFAULT now()
);

CREATE INDEX IF NOT EXISTS fsm_history_machine ON fsm.history (kind, id, seq);

CREATE TABLE IF NOT EXISTS fsm.outbox (
	seq bigserial PRIMARY KEY,
	kind text NOT NULL,
	machine_id text NOT NULL,
	topic text NOT NULL,
	payload bytea NOT NULL DEFAULT '',
	due_at timestamptz NOT NULL DEFAULT now(),
	done_at timestamptz,
	created_at timestamptz NOT NULL DEFAULT now()
);

CREATE INDEX IF NOT EXISTS fsm_outbox_due ON fsm.outbox (due_at) WHERE done_at IS NULL;

CREATE TABLE IF NOT EXISTS fsm.sweeper_leases (
	name text PRIMARY KEY,
	owner text NOT NULL,
	expires_at timestamptz NOT NULL
);
