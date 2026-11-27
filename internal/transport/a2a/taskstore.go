package a2a

import (
	"context"
	"sort"
	"sync"
	"time"
)

const (
	taskTTL        = time.Hour
	maxStoredTasks = 256
)

type taskEntry struct {
	task    Task
	owner   string
	cancel  context.CancelFunc
	updated time.Time
}

type taskStore struct {
	mu    sync.Mutex
	tasks map[string]*taskEntry
}

func newTaskStore() *taskStore {
	return &taskStore{tasks: make(map[string]*taskEntry)}
}

func (s *taskStore) key(id, owner string) string { return owner + "|" + id }

// find resolves a task by its public id and owner.
func (s *taskStore) find(id, owner string) *taskEntry {
	return s.tasks[s.key(id, owner)]
}

func (s *taskStore) create(id, owner string, ctx context.Context, cancel context.CancelFunc) (*taskEntry, bool) {
	s.mu.Lock()
	defer s.mu.Unlock()
	now := time.Now()
	s.removeExpired(now)
	key := s.key(id, owner)
	if s.tasks[key] != nil {
		return nil, false
	}
	if len(s.tasks) >= maxStoredTasks {
		var oldestKey string
		var oldest *taskEntry
		for key, entry := range s.tasks {
			if !terminalTaskState(entry.task.Status.State) {
				continue
			}
			if oldest == nil || entry.updated.Before(oldest.updated) {
				oldestKey, oldest = key, entry
			}
		}
		if oldest == nil {
			return nil, false
		}
		delete(s.tasks, oldestKey)
	}
	entry := &taskEntry{
		task: Task{
			ID:        id,
			ContextID: id,
			Status:    TaskStatus{State: "TASK_STATE_SUBMITTED", Timestamp: timestamp()},
		},
		owner:   owner,
		cancel:  cancel,
		updated: now,
	}
	_ = ctx
	s.tasks[key] = entry
	return entry, true
}

func (s *taskStore) removeExpired(now time.Time) {
	for key, entry := range s.tasks {
		if now.Sub(entry.updated) <= taskTTL {
			continue
		}
		if entry.cancel != nil {
			entry.cancel()
		}
		delete(s.tasks, key)
	}
}

func terminalTaskState(state string) bool {
	switch state {
	case "TASK_STATE_COMPLETED", "TASK_STATE_FAILED", "TASK_STATE_CANCELED":
		return true
	default:
		return false
	}
}

func (s *taskStore) setStatus(id, owner, state string) {
	s.mu.Lock()
	defer s.mu.Unlock()
	if entry := s.find(id, owner); entry != nil {
		entry.task.Status = TaskStatus{State: state, Timestamp: timestamp()}
		entry.updated = time.Now()
	}
}

func (s *taskStore) setTask(id, owner string, task Task) {
	s.mu.Lock()
	defer s.mu.Unlock()
	if entry := s.find(id, owner); entry != nil {
		entry.task = task
		entry.updated = time.Now()
	}
}

func (s *taskStore) appendHistory(id, owner string, message Message) {
	s.mu.Lock()
	defer s.mu.Unlock()
	if entry := s.find(id, owner); entry != nil {
		entry.task.History = append(entry.task.History, message)
		entry.updated = time.Now()
	}
}

func (s *taskStore) get(id, owner string) (Task, bool) {
	s.mu.Lock()
	defer s.mu.Unlock()
	s.removeExpired(time.Now())
	entry := s.find(id, owner)
	if entry == nil {
		return Task{}, false
	}
	return entry.task, true
}

func (s *taskStore) list(owner, contextID, state string) []Task {
	s.mu.Lock()
	defer s.mu.Unlock()
	s.removeExpired(time.Now())
	out := make([]Task, 0, len(s.tasks))
	for _, entry := range s.tasks {
		if entry.owner != owner {
			continue
		}
		if contextID != "" && entry.task.ContextID != contextID {
			continue
		}
		if state != "" && entry.task.Status.State != state {
			continue
		}
		out = append(out, entry.task)
	}
	sort.Slice(out, func(i, j int) bool { return out[i].ID < out[j].ID })
	return out
}

func (s *taskStore) cancel(id, owner string) (Task, bool) {
	s.mu.Lock()
	defer s.mu.Unlock()
	s.removeExpired(time.Now())
	entry := s.find(id, owner)
	if entry == nil {
		return Task{}, false
	}
	if entry.cancel != nil {
		entry.cancel()
	}
	entry.task.Status = TaskStatus{State: "TASK_STATE_CANCELED", Timestamp: timestamp()}
	entry.updated = time.Now()
	task := entry.task
	return task, true
}

func timestamp() string {
	return time.Now().UTC().Format("2006-01-02T15:04:05.000Z")
}
