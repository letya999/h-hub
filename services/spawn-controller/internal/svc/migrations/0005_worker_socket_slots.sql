ALTER TABLE intents ADD COLUMN IF NOT EXISTS worker_slot integer CHECK (worker_slot > 0);

CREATE UNIQUE INDEX IF NOT EXISTS worker_socket_slot ON intents (worker_slot)
    WHERE kind = 'worker' AND worker_slot IS NOT NULL;
