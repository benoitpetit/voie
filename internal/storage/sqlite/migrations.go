package sqlite

import (
	"context"
	"database/sql"
)

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
`

func migrate(ctx context.Context, db *sql.DB) error {
	if _, err := db.ExecContext(ctx, schema); err != nil {
		return err
	}
	var version int
	if err := db.QueryRowContext(ctx, "PRAGMA user_version").Scan(&version); err != nil {
		return err
	}
	if version >= 2 {
		return nil
	}

	tx, err := db.BeginTx(ctx, nil)
	if err != nil {
		return err
	}
	defer tx.Rollback()
	if version < 1 {
		if _, err = tx.ExecContext(ctx, "PRAGMA user_version = 1"); err != nil {
			return err
		}
	}
	if _, err = tx.ExecContext(ctx, "ALTER TABLE conversations ADD COLUMN tenant_id TEXT NOT NULL DEFAULT ''"); err != nil {
		return err
	}
	if _, err = tx.ExecContext(ctx, "ALTER TABLE conversation_tombstones ADD COLUMN tenant_id TEXT NOT NULL DEFAULT ''"); err != nil {
		return err
	}
	if _, err = tx.ExecContext(ctx, "PRAGMA user_version = 2"); err != nil {
		return err
	}
	return tx.Commit()
}
