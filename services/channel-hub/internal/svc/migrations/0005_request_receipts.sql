ALTER TABLE deliveries ADD COLUMN telegram_message_id bigint;
CREATE UNIQUE INDEX telegram_request_receipt ON deliveries(chat_id, telegram_message_id)
    WHERE channel='telegram' AND kind='request' AND telegram_message_id IS NOT NULL;
