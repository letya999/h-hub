-- Job attachments: photo/binary parts the channel staged for the
-- model. JSONB holds [{mime,data(base64)}] rows; the engine forwards
-- them to the runtime verbatim.
ALTER TABLE jobs ADD COLUMN attachments jsonb;
