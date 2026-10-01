package sqlite

import (
	"context"
	"database/sql"
	"encoding/json"
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"sync"
	"time"

	"github.com/benoitpetit/voie/internal/app"
	"github.com/google/uuid"
	_ "modernc.org/sqlite"
)

const (
	maxMessages  = 200
	maxBytes     = 2 << 20
	tombstoneTTL = 30 * 24 * time.Hour
)

type Store struct {
	path      string
	ttl       time.Duration
	mu        sync.Mutex
	db        *sql.DB
	closed    bool
	stop      chan struct{}
	closeOnce sync.Once
}

func NewSQLiteStore(path string, ttl time.Duration) (*Store, error) {
	if path == "" || ttl <= 0 {
		return nil, app.ErrInvalidInput
	}
	return &Store{path: path, ttl: ttl, stop: make(chan struct{})}, nil
}

func (s *Store) open(ctx context.Context) (*sql.DB, error) {
	s.mu.Lock()
	defer s.mu.Unlock()
	if s.closed {
		return nil, appError(app.ErrConversationStore, "conversation store is closed", nil)
	}
	if s.db != nil {
		return s.db, nil
	}
	dir := filepath.Dir(s.path)
	_, dirErr := os.Stat(dir)
	dirCreated := errors.Is(dirErr, os.ErrNotExist)
	if dirErr != nil && !dirCreated {
		return nil, appError(app.ErrConversationStore, "inspect conversation directory", dirErr)
	}
	if err := os.MkdirAll(dir, 0700); err != nil {
		return nil, appError(app.ErrConversationStore, "create conversation directory", err)
	}
	if dirCreated {
		if err := os.Chmod(dir, 0700); err != nil {
			return nil, appError(app.ErrConversationStore, "secure conversation directory", err)
		}
	}
	if _, err := os.Stat(s.path); errors.Is(err, os.ErrNotExist) {
		f, e := os.OpenFile(s.path, os.O_CREATE|os.O_EXCL|os.O_RDWR, 0600)
		if e != nil {
			return nil, appError(app.ErrConversationStore, "create conversation database", e)
		}
		_ = f.Close()
	} else if err != nil {
		return nil, appError(app.ErrConversationStore, "inspect conversation database", err)
	}
	_ = os.Chmod(s.path, 0600)
	db, err := sql.Open("sqlite", s.path)
	if err != nil {
		return nil, appError(app.ErrConversationStore, "open conversation database", err)
	}
	db.SetMaxOpenConns(1)
	for _, pragma := range []string{"PRAGMA busy_timeout=5000", "PRAGMA foreign_keys=ON"} {
		if _, err = db.ExecContext(ctx, pragma); err != nil {
			_ = db.Close()
			return nil, appError(app.ErrConversationStore, "configure conversation database", err)
		}
	}
	if _, err = db.ExecContext(ctx, schema); err != nil {
		_ = db.Close()
		return nil, appError(app.ErrConversationStore, "migrate conversation database", err)
	}
	s.db = db
	if _, err := s.cleanup(ctx, db, time.Now()); err != nil {
		s.db = nil
		_ = db.Close()
		return nil, err
	}
	go s.cleanupLoop()
	return db, nil
}

func (s *Store) cleanupLoop() {
	ticker := time.NewTicker(time.Hour)
	defer ticker.Stop()
	for {
		select {
		case <-s.stop:
			return
		case <-ticker.C:
			ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
			_, _ = s.CleanupExpired(ctx, time.Now())
			cancel()
		}
	}
}
func (s *Store) Close() error {
	var err error
	s.closeOnce.Do(func() {
		s.mu.Lock()
		s.closed = true
		close(s.stop)
		db := s.db
		s.db = nil
		s.mu.Unlock()
		if db != nil {
			err = db.Close()
		}
	})
	return err
}

func (s *Store) Create(ctx context.Context) (app.Conversation, error) {
	db, err := s.open(ctx)
	if err != nil {
		return app.Conversation{}, err
	}
	now := time.Now().UTC()
	c := app.Conversation{ID: uuid.NewString(), CreatedAt: now, UpdatedAt: now, ExpiresAt: now.Add(s.ttl), Turns: []app.ConversationTurn{}}
	b, err := json.Marshal(c)
	if err != nil {
		return c, storeErr(err)
	}
	_, err = db.ExecContext(ctx, "INSERT INTO conversations(id,payload,version,expires_at,created_at,updated_at) VALUES(?,?,?,?,?,?)", c.ID, b, c.Version, c.ExpiresAt.UnixNano(), now.UnixNano(), now.UnixNano())
	if err != nil {
		return app.Conversation{}, storeErr(err)
	}
	return c, nil
}

func (s *Store) Get(ctx context.Context, id string) (app.Conversation, error) {
	db, err := s.open(ctx)
	if err != nil {
		return app.Conversation{}, err
	}
	var payload []byte
	var expiry int64
	err = db.QueryRowContext(ctx, "SELECT payload,expires_at FROM conversations WHERE id=?", id).Scan(&payload, &expiry)
	if errors.Is(err, sql.ErrNoRows) {
		return app.Conversation{}, s.missing(ctx, db, id)
	}
	if err != nil {
		return app.Conversation{}, storeErr(err)
	}
	if time.Now().UnixNano() >= expiry {
		return app.Conversation{}, app.ErrConversationExpired
	}
	var c app.Conversation
	if err = json.Unmarshal(payload, &c); err != nil {
		return c, storeErr(err)
	}
	return c, nil
}
func (s *Store) missing(ctx context.Context, db *sql.DB, id string) error {
	var expiry int64
	err := db.QueryRowContext(ctx, "SELECT expires_at FROM conversation_tombstones WHERE id=?", id).Scan(&expiry)
	if err == nil && time.Now().UnixNano() < expiry {
		return app.ErrConversationExpired
	}
	if err != nil && !errors.Is(err, sql.ErrNoRows) {
		return storeErr(err)
	}
	return app.ErrConversationNotFound
}

func (s *Store) List(ctx context.Context) ([]app.ConversationSummary, error) {
	db, err := s.open(ctx)
	if err != nil {
		return nil, err
	}
	rows, err := db.QueryContext(ctx, "SELECT payload FROM conversations WHERE expires_at>? ORDER BY updated_at DESC", time.Now().UnixNano())
	if err != nil {
		return nil, storeErr(err)
	}
	defer rows.Close()
	out := []app.ConversationSummary{}
	for rows.Next() {
		var b []byte
		if err = rows.Scan(&b); err != nil {
			return nil, storeErr(err)
		}
		var c app.Conversation
		if err = json.Unmarshal(b, &c); err != nil {
			return nil, storeErr(err)
		}
		count := 0
		for _, t := range c.Turns {
			count += len(t.UserMessages) + 1
		}
		out = append(out, app.ConversationSummary{ID: c.ID, CreatedAt: c.CreatedAt, UpdatedAt: c.UpdatedAt, ExpiresAt: c.ExpiresAt, Version: c.Version, MessageCount: count})
	}
	if err = rows.Err(); err != nil {
		return nil, storeErr(err)
	}
	return out, nil
}
func (s *Store) Delete(ctx context.Context, id string) error {
	db, err := s.open(ctx)
	if err != nil {
		return err
	}
	_, err = db.ExecContext(ctx, "DELETE FROM conversations WHERE id=?", id)
	if err != nil {
		return storeErr(err)
	}
	_, err = db.ExecContext(ctx, "DELETE FROM conversation_tombstones WHERE id=?", id)
	return storeErr(err)
}

func (s *Store) AppendTurn(ctx context.Context, id string, expected int64, user []app.Message, assistant app.Message, routing app.RoutingInfo) (app.Conversation, error) {
	db, err := s.open(ctx)
	if err != nil {
		return app.Conversation{}, err
	}
	tx, err := db.BeginTx(ctx, nil)
	if err != nil {
		return app.Conversation{}, storeErr(err)
	}
	defer tx.Rollback()
	var payload []byte
	var expiry int64
	err = tx.QueryRowContext(ctx, "SELECT payload,expires_at FROM conversations WHERE id=?", id).Scan(&payload, &expiry)
	if errors.Is(err, sql.ErrNoRows) {
		_ = tx.Rollback()
		return app.Conversation{}, s.missing(ctx, db, id)
	}
	if err != nil {
		return app.Conversation{}, storeErr(err)
	}
	if time.Now().UnixNano() >= expiry {
		return app.Conversation{}, app.ErrConversationExpired
	}
	var c app.Conversation
	if err = json.Unmarshal(payload, &c); err != nil {
		return c, storeErr(err)
	}
	if c.Version != expected {
		return c, app.ErrConversationConflict
	}
	now := time.Now().UTC()
	c.Turns = append(c.Turns, app.ConversationTurn{UserMessages: append([]app.Message(nil), user...), Assistant: assistant, Routing: routing, CreatedAt: now})
	c.UpdatedAt = now
	c.ExpiresAt = now.Add(s.ttl)
	c.Version++
	count := 0
	for _, turn := range c.Turns {
		count += len(turn.UserMessages) + 1
	}
	if count > maxMessages {
		return app.Conversation{}, appError(app.ErrInvalidInput, "conversation exceeds message limit", nil)
	}
	payload, err = json.Marshal(c)
	if err != nil {
		return c, storeErr(err)
	}
	if len(payload) > maxBytes {
		return app.Conversation{}, appError(app.ErrInvalidInput, "conversation exceeds storage limit", nil)
	}
	res, err := tx.ExecContext(ctx, "UPDATE conversations SET payload=?,version=?,expires_at=?,updated_at=? WHERE id=? AND version=?", payload, c.Version, c.ExpiresAt.UnixNano(), now.UnixNano(), id, expected)
	if err != nil {
		return app.Conversation{}, storeErr(err)
	}
	n, _ := res.RowsAffected()
	if n != 1 {
		return app.Conversation{}, app.ErrConversationConflict
	}
	if err = tx.Commit(); err != nil {
		return app.Conversation{}, storeErr(err)
	}
	return c, nil
}

func (s *Store) CleanupExpired(ctx context.Context, now time.Time) (int, error) {
	db, err := s.open(ctx)
	if err != nil {
		return 0, err
	}
	return s.cleanup(ctx, db, now)
}
func (s *Store) cleanup(ctx context.Context, db *sql.DB, now time.Time) (int, error) {
	tx, err := db.BeginTx(ctx, nil)
	if err != nil {
		return 0, storeErr(err)
	}
	defer tx.Rollback()
	rows, err := tx.QueryContext(ctx, "SELECT id,expires_at FROM conversations WHERE expires_at<=?", now.UnixNano())
	if err != nil {
		return 0, storeErr(err)
	}
	type tomb struct {
		id      string
		expires int64
	}
	var expired []tomb
	for rows.Next() {
		var t tomb
		if err = rows.Scan(&t.id, &t.expires); err != nil {
			rows.Close()
			return 0, storeErr(err)
		}
		t.expires = now.Add(tombstoneTTL).UnixNano()
		expired = append(expired, t)
	}
	if err = rows.Err(); err != nil {
		rows.Close()
		return 0, storeErr(err)
	}
	rows.Close()
	for _, t := range expired {
		if _, err = tx.ExecContext(ctx, "INSERT OR REPLACE INTO conversation_tombstones(id,expires_at) VALUES(?,?)", t.id, t.expires); err != nil {
			return 0, storeErr(err)
		}
	}
	if _, err = tx.ExecContext(ctx, "DELETE FROM conversations WHERE expires_at<=?", now.UnixNano()); err != nil {
		return 0, storeErr(err)
	}
	if _, err = tx.ExecContext(ctx, "DELETE FROM conversation_tombstones WHERE expires_at<=?", now.UnixNano()); err != nil {
		return 0, storeErr(err)
	}
	if err = tx.Commit(); err != nil {
		return 0, storeErr(err)
	}
	return len(expired), nil
}

func storeErr(err error) error {
	if err == nil {
		return nil
	}
	return appError(app.ErrConversationStore, fmt.Sprintf("conversation storage failed: %v", err), err)
}
func appError(kind error, msg string, cause error) error {
	return &app.Error{Kind: kind, Message: msg, Cause: cause}
}
