package sqlite

const schema = `
CREATE TABLE IF NOT EXISTS conversations (
 id TEXT PRIMARY KEY, payload BLOB NOT NULL, version INTEGER NOT NULL,
 expires_at INTEGER NOT NULL, created_at INTEGER NOT NULL, updated_at INTEGER NOT NULL
);
CREATE TABLE IF NOT EXISTS conversation_tombstones (
 id TEXT PRIMARY KEY, expires_at INTEGER NOT NULL
);
CREATE INDEX IF NOT EXISTS conversations_expiry ON conversations(expires_at);
CREATE INDEX IF NOT EXISTS tombstones_expiry ON conversation_tombstones(expires_at);
PRAGMA user_version = 1;
`
