-- Telegram adapter storage: conversation keys (chat, topic), the
-- durable getUpdates offset, the provider message_id registry for
-- status edits and secret deletes, and the delivery routing columns.
ALTER TABLE inbound_updates ADD COLUMN kind text NOT NULL DEFAULT 'text';
ALTER TABLE inbound_updates ADD COLUMN chat_id text NOT NULL DEFAULT '';
ALTER TABLE inbound_updates ADD COLUMN topic text NOT NULL DEFAULT '';
ALTER TABLE inbound_updates ADD COLUMN payload text NOT NULL DEFAULT '';
ALTER TABLE deliveries ADD COLUMN channel text NOT NULL DEFAULT 'mock';
ALTER TABLE deliveries ADD COLUMN chat_id text NOT NULL DEFAULT '';
ALTER TABLE deliveries ADD COLUMN topic text NOT NULL DEFAULT '';
ALTER TABLE deliveries ADD COLUMN intake_id text NOT NULL DEFAULT '';
CREATE TABLE tg_threads (
    chat_id text NOT NULL,
    topic text NOT NULL DEFAULT '',
    conversation_id text NOT NULL,
    PRIMARY KEY (chat_id, topic)
);
CREATE TABLE tg_offsets (
    channel text PRIMARY KEY,
    next_offset bigint NOT NULL
);
CREATE TABLE tg_status (
    intake_id text PRIMARY KEY,
    message_id bigint NOT NULL,
    phase text NOT NULL DEFAULT '',
    updated_at timestamptz NOT NULL DEFAULT now()
);
