-- The owner request a waiting job is bound to. Empty when the job
-- never waited; the request's state lives in tool-hub — this column
-- is only the pair's link, never a copy (sys.seam-onesided).
ALTER TABLE jobs ADD COLUMN owner_request TEXT NOT NULL DEFAULT '';
