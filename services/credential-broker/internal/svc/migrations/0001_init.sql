CREATE TABLE IF NOT EXISTS meta(
  key   TEXT PRIMARY KEY,
  value TEXT NOT NULL
);
CREATE TABLE IF NOT EXISTS credentials(
  ref        TEXT PRIMARY KEY,
  name       TEXT NOT NULL,
  type       TEXT NOT NULL,
  created_at TEXT NOT NULL DEFAULT (datetime('now'))
);
CREATE TABLE IF NOT EXISTS versions(
  ref        TEXT NOT NULL REFERENCES credentials(ref),
  version    INTEGER NOT NULL,
  sealed     BLOB NOT NULL,
  kid        TEXT NOT NULL,
  deleted_at TEXT,
  created_at TEXT NOT NULL DEFAULT (datetime('now')),
  PRIMARY KEY (ref, version)
);
CREATE TABLE IF NOT EXISTS audit_events(
  seq    INTEGER PRIMARY KEY AUTOINCREMENT,
  at     TEXT NOT NULL DEFAULT (datetime('now')),
  actor  TEXT NOT NULL,
  event  TEXT NOT NULL,
  ref    TEXT NOT NULL DEFAULT '',
  detail TEXT NOT NULL DEFAULT ''
);
INSERT OR IGNORE INTO meta(key, value) VALUES ('schema_version', '0');
