-- owner_requests: the domain row of the owner-request machine
-- (phase 10). State and deadline live in fsm.machines; the row keeps
-- the addressee, the question and the recorded answer. delivery_id
-- pairs the request with its channel delivery (sys.seam-onesided).
CREATE TABLE owner_requests (
    id           text        PRIMARY KEY,
    owner        text        NOT NULL,
    space        text        NOT NULL,
    channel      text        NOT NULL DEFAULT '',
    external_id  text        NOT NULL DEFAULT '',
    chat_id      text        NOT NULL DEFAULT '',
    topic        text        NOT NULL DEFAULT '',
    conversation text        NOT NULL DEFAULT '',
    job          text        NOT NULL DEFAULT '',
    run          text        NOT NULL DEFAULT '',
    kind         text        NOT NULL,
    prompt       text        NOT NULL,
    options      jsonb       NOT NULL DEFAULT '[]',
    answer       text        NOT NULL DEFAULT '',
    answered_by  text        NOT NULL DEFAULT '',
    delivery_id  text        NOT NULL DEFAULT '',
    created_at   timestamptz NOT NULL DEFAULT now()
);
