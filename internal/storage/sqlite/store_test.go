package sqlite

import (
	"context"
	"database/sql"
	"encoding/json"
	"errors"
	"os"
	"path/filepath"
	"runtime"
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
