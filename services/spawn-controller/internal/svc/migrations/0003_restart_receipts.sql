CREATE UNIQUE INDEX IF NOT EXISTS restart_request_receipt ON fsm.outbox (machine_id, payload)
    WHERE kind='space' AND topic='restart-idle';
