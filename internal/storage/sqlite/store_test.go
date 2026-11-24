package sqlite

import (
	"context"
	"database/sql"
	"encoding/json"
	"errors"
	"os"
	"path/filepath"
	"runtime"
	"strings"
	"testing"
	"time"

	"github.com/benoitpetit/voie/internal/app"
)

func TestStorePersistsAcrossReopen(t *testing.T) {
	path := filepath.Join(t.TempDir(), "sessions.db")
	store, err := NewSQLiteStore(path, time.Hour)
	if err != nil {
		t.Fatal(err)
	}
	c, err := store.Create(context.Background())
	if err != nil {
		t.Fatal(err)
	}
	_ = store.Close()
	store, err = NewSQLiteStore(path, time.Hour)
	if err != nil {
		t.Fatal(err)
	}
	defer store.Close()
	got, err := store.Get(context.Background(), c.ID)
	if err != nil || got.ID != c.ID {
		t.Fatalf("get=%+v err=%v", got, err)
	}
}

func TestStoreMigrationKeepsLegacyConversationsOutOfProjectScopes(t *testing.T) {
	path := filepath.Join(t.TempDir(), "legacy.db")
	legacyDB, err := sql.Open("sqlite", path)
	if err != nil {
		t.Fatal(err)
	}
	_, err = legacyDB.Exec(`
CREATE TABLE conversations (
 id TEXT PRIMARY KEY, payload BLOB NOT NULL, version INTEGER NOT NULL,
 expires_at INTEGER NOT NULL, created_at INTEGER NOT NULL, updated_at INTEGER NOT NULL
);
CREATE TABLE conversation_tombstones (id TEXT PRIMARY KEY, expires_at INTEGER NOT NULL);
CREATE INDEX conversations_expiry ON conversations(expires_at);
CREATE INDEX tombstones_expiry ON conversation_tombstones(expires_at);
PRAGMA user_version = 1;
`)
	if err != nil {
		t.Fatal(err)
	}
	now := time.Now().UTC()
	legacy := app.Conversation{ID: "legacy-id", CreatedAt: now, UpdatedAt: now, ExpiresAt: now.Add(time.Hour), Turns: []app.ConversationTurn{}}
	payload, err := json.Marshal(legacy)
	if err != nil {
		t.Fatal(err)
	}
	_, err = legacyDB.Exec("INSERT INTO conversations(id,payload,version,expires_at,created_at,updated_at) VALUES(?,?,?,?,?,?)", legacy.ID, payload, legacy.Version, legacy.ExpiresAt.UnixNano(), now.UnixNano(), now.UnixNano())
	if err != nil {
		t.Fatal(err)
	}
	if err := legacyDB.Close(); err != nil {
		t.Fatal(err)
	}

	store, err := NewSQLiteStore(path, time.Hour)
	if err != nil {
		t.Fatal(err)
	}
	defer store.Close()
	if _, err = store.Get(context.Background(), legacy.ID); err != nil {
		t.Fatalf("legacy local read failed after migration: %v", err)
	}
	projectContext := app.WithConversationScope(context.Background(), "fac9ccbc84f381515806747e5fa5aac8625bdab35cb4264ef9ec360ee121a470")
	if _, err = store.Get(projectContext, legacy.ID); !errors.Is(err, app.ErrConversationNotFound) {
		t.Fatalf("project-scoped read of legacy conversation error = %v, want not found", err)
	}
}

func TestStoreKeepsExistingDirectoryModeAndSecuresDatabase(t *testing.T) {
	dir := t.TempDir()
	if err := os.Chmod(dir, 0755); err != nil {
		t.Fatal(err)
	}
	before, err := os.Stat(dir)
	if err != nil {
		t.Fatal(err)
	}
	store, err := NewSQLiteStore(filepath.Join(dir, "sessions.db"), time.Hour)
	if err != nil {
		t.Fatal(err)
	}
	defer store.Close()
	if _, err = store.Create(context.Background()); err != nil {
		t.Fatal(err)
	}
	after, err := os.Stat(dir)
	if err != nil {
		t.Fatal(err)
	}
	if before.Mode().Perm() != after.Mode().Perm() {
		t.Fatalf("parent mode changed from %o to %o", before.Mode().Perm(), after.Mode().Perm())
	}
	db, err := os.Stat(filepath.Join(dir, "sessions.db"))
	if err != nil {
		t.Fatal(err)
	}
	// Windows applies file access through ACLs and does not expose POSIX
	// permission bits through FileMode.Perm.
	if runtime.GOOS != "windows" && db.Mode().Perm() != 0600 {
		t.Fatalf("database mode=%o, want 0600", db.Mode().Perm())
	}
}

func TestStoreAppendRejectsStaleVersion(t *testing.T) {
	store, err := NewSQLiteStore(filepath.Join(t.TempDir(), "sessions.db"), time.Hour)
	if err != nil {
		t.Fatal(err)
	}
	defer store.Close()
	c, err := store.Create(context.Background())
	if err != nil {
		t.Fatal(err)
	}
	_, err = store.AppendTurn(context.Background(), c.ID, 0, []app.Message{{Role: "user", Content: "hi"}}, app.Message{Role: "assistant", Content: "ok"}, app.RoutingInfo{})
	if err != nil {
		t.Fatal(err)
	}
	_, err = store.AppendTurn(context.Background(), c.ID, 0, []app.Message{{Role: "user", Content: "again"}}, app.Message{Role: "assistant", Content: "no"}, app.RoutingInfo{})
	if !errors.Is(err, app.ErrConversationConflict) {
		t.Fatalf("error=%v", err)
	}
}

func TestStoreCleanupLeavesThirtyDayTombstone(t *testing.T) {
	store, err := NewSQLiteStore(filepath.Join(t.TempDir(), "sessions.db"), time.Second)
	if err != nil {
		t.Fatal(err)
	}
	defer store.Close()
	c, err := store.Create(context.Background())
	if err != nil {
		t.Fatal(err)
	}
	_, err = store.CleanupExpired(context.Background(), time.Now().Add(2*time.Second))
	if err != nil {
		t.Fatal(err)
	}
	_, err = store.Get(context.Background(), c.ID)
	if !errors.Is(err, app.ErrConversationExpired) {
		t.Fatalf("error=%v", err)
	}
}

func TestMigrationIsIdempotentOnPartialUpgrade(t *testing.T) {
	path := filepath.Join(t.TempDir(), "partial.db")
	db, err := sql.Open("sqlite", path)
	if err != nil {
		t.Fatal(err)
	}
	defer db.Close()
	for _, statement := range []string{
		`CREATE TABLE conversations (id TEXT PRIMARY KEY, payload BLOB NOT NULL, version INTEGER NOT NULL, expires_at INTEGER NOT NULL, created_at INTEGER NOT NULL, updated_at INTEGER NOT NULL, tenant_id TEXT NOT NULL DEFAULT '')`,
		`CREATE TABLE conversation_tombstones (id TEXT PRIMARY KEY, expires_at INTEGER NOT NULL)`,
		`PRAGMA user_version = 1`,
	} {
		if _, err := db.Exec(statement); err != nil {
			t.Fatal(err)
		}
	}
	if err := migrate(context.Background(), db); err != nil {
		t.Fatal(err)
	}
	if err := migrate(context.Background(), db); err != nil {
		t.Fatalf("second migration failed: %v", err)
	}
	for _, table := range []string{"conversations", "conversation_tombstones"} {
		if !hasColumn(t, db, table, "tenant_id") {
			t.Errorf("%s is missing tenant_id after migration", table)
		}
	}
}

func TestFreshDatabaseHasTenantID(t *testing.T) {
	db, err := sql.Open("sqlite", filepath.Join(t.TempDir(), "fresh.db"))
	if err != nil {
		t.Fatal(err)
	}
	defer db.Close()
	if err := migrate(context.Background(), db); err != nil {
		t.Fatal(err)
	}
	for _, table := range []string{"conversations", "conversation_tombstones"} {
		if !hasColumn(t, db, table, "tenant_id") {
			t.Errorf("%s is missing tenant_id on a fresh database", table)
		}
	}
}

func hasColumn(t *testing.T, db *sql.DB, table, column string) bool {
	t.Helper()
	rows, err := db.Query(`PRAGMA table_info(` + table + `)`)
	if err != nil {
		t.Fatal(err)
	}
	defer rows.Close()
	for rows.Next() {
		var cid, notNull, pk int
		var name, columnType string
		var defaultValue any
		if err := rows.Scan(&cid, &name, &columnType, &notNull, &defaultValue, &pk); err != nil {
			t.Fatal(err)
		}
		if name == column {
			return true
		}
	}
	return false
}

func TestTombstonesAreScopedByTenant(t *testing.T) {
	ctx := context.Background()
	store, err := NewSQLiteStore(filepath.Join(t.TempDir(), "scoped.db"), time.Hour)
	if err != nil {
		t.Fatal(err)
	}
	defer store.Close()
	db, release, err := store.open(ctx)
	if err != nil {
		t.Fatal(err)
	}
	defer release()
	for _, tenant := range []string{"scope-a", "scope-b"} {
		if _, err := db.Exec(`INSERT INTO conversation_tombstones(id, tenant_id, expires_at) VALUES(?,?,?)`,
			"same-id", tenant, time.Now().Add(time.Hour).UnixNano()); err != nil {
			t.Fatal(err)
		}
	}
	rows, err := db.Query(`SELECT tenant_id FROM conversation_tombstones WHERE id='same-id'`)
	if err != nil {
		t.Fatal(err)
	}
	defer rows.Close()
	found := map[string]bool{}
	for rows.Next() {
		var tenant string
		if err := rows.Scan(&tenant); err != nil {
			t.Fatal(err)
		}
		found[tenant] = true
	}
	if len(found) != 2 || !found["scope-a"] || !found["scope-b"] {
		t.Fatalf("tombstones = %v, want one per scope", found)
	}
}

func TestCloseWaitsForInFlightOperations(t *testing.T) {
	store, err := NewSQLiteStore(filepath.Join(t.TempDir(), "close.db"), time.Hour)
	if err != nil {
		t.Fatal(err)
	}
	store.ops.Add(1)
	done := make(chan struct{})
	go func() {
		_ = store.Close()
		close(done)
	}()
	select {
	case <-done:
		t.Fatal("Close returned while an operation was in flight")
	case <-time.After(50 * time.Millisecond):
	}
	store.ops.Done()
	select {
	case <-done:
	case <-time.After(time.Second):
		t.Fatal("Close did not return after the operation finished")
	}
}

func TestStoreErrorsDoNotLeakSQL(t *testing.T) {
	err := storeErr(errors.New("SELECT * FROM conversations: syntax error"))
	if strings.Contains(err.Error(), "SELECT") || strings.Contains(err.Error(), "conversations") {
		t.Fatalf("store error leaks SQL: %v", err)
	}
}
