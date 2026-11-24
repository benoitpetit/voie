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

func columnExists(ctx context.Context, tx *sql.Tx, table, column string) (bool, error) {
	rows, err := tx.QueryContext(ctx, "PRAGMA table_info("+table+")")
	if err != nil {
		return false, err
	}
	defer rows.Close()
	for rows.Next() {
		var cid, notNull, pk int
		var name, columnType string
		var defaultValue any
		if err := rows.Scan(&cid, &name, &columnType, &notNull, &defaultValue, &pk); err != nil {
			return false, err
		}
		if name == column {
			return true, nil
		}
	}
	return false, rows.Err()
}

func migrate(ctx context.Context, db *sql.DB) error {
	if _, err := db.ExecContext(ctx, schema); err != nil {
		return err
	}
	tx, err := db.BeginTx(ctx, nil)
	if err != nil {
		return err
	}
	defer tx.Rollback()
	for _, table := range []string{"conversations", "conversation_tombstones"} {
		exists, err := columnExists(ctx, tx, table, "tenant_id")
		if err != nil {
			return err
		}
		if exists {
			continue
		}
		if _, err := tx.ExecContext(ctx, "ALTER TABLE "+table+" ADD COLUMN tenant_id TEXT NOT NULL DEFAULT ''"); err != nil {
			return err
		}
	}
	var version int
	if err := tx.QueryRowContext(ctx, "PRAGMA user_version").Scan(&version); err != nil {
		return err
	}
	if version < 3 {
		for _, statement := range []string{
			`ALTER TABLE conversation_tombstones RENAME TO conversation_tombstones_old`,
			`CREATE TABLE conversation_tombstones (id TEXT NOT NULL, tenant_id TEXT NOT NULL DEFAULT '', expires_at INTEGER NOT NULL, PRIMARY KEY (tenant_id, id))`,
			`CREATE INDEX conversation_tombstones_expiry ON conversation_tombstones(expires_at)`,
			`INSERT OR IGNORE INTO conversation_tombstones(id, tenant_id, expires_at) SELECT id, tenant_id, expires_at FROM conversation_tombstones_old`,
			`DROP TABLE conversation_tombstones_old`,
		} {
			if _, err := tx.ExecContext(ctx, statement); err != nil {
				return err
			}
		}
	}
	if _, err := tx.ExecContext(ctx, "PRAGMA user_version = 3"); err != nil {
		return err
	}
	return tx.Commit()
}
