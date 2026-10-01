package sqlite

import (
	"context"
	"errors"
	"path/filepath"
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
