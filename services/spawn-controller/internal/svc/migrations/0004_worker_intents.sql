ALTER TABLE intents
    ADD COLUMN IF NOT EXISTS owner text NOT NULL DEFAULT '',
    ADD COLUMN IF NOT EXISTS profile text NOT NULL DEFAULT '',
    ADD COLUMN IF NOT EXISTS lease text NOT NULL DEFAULT '',
    ADD COLUMN IF NOT EXISTS stop_reason text NOT NULL DEFAULT '';

CREATE UNIQUE INDEX IF NOT EXISTS worker_generation ON intents (runtime)
    WHERE kind = 'worker';
