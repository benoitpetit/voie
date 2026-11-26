package a2a

import (
	"context"
	"sort"
	"sync"
	"time"
)

const taskTTL = time.Hour

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

// find resolves a task by its public id and optional owner. An empty owner is
// a wildcard for internal maintenance calls, not for cross-tenant reads.
func (s *taskStore) find(id, owner string) *taskEntry {
	if entry := s.tasks[s.key(id, owner)]; entry != nil {
		return entry
	}
	for _, entry := range s.tasks {
		if entry.task.ID == id && (owner == "" || entry.owner == "" || entry.owner == owner) {
			return entry
		}
	}
	return nil
}

func (s *taskStore) create(id, owner string, ctx context.Context, cancel context.CancelFunc) *taskEntry {
	s.mu.Lock()
	defer s.mu.Unlock()
	entry := &taskEntry{
		task: Task{
			ID:        id,
			ContextID: id,
			Status:    TaskStatus{State: "TASK_STATE_SUBMITTED", Timestamp: timestamp()},
		},
		owner:   owner,
		cancel:  cancel,
		updated: time.Now(),
	}
	_ = ctx
	s.tasks[s.key(id, owner)] = entry
	return entry
}

func (s *taskStore) setStatus(id, state string) {
	s.mu.Lock()
	defer s.mu.Unlock()
	if entry := s.find(id, ""); entry != nil {
		entry.task.Status = TaskStatus{State: state, Timestamp: timestamp()}
		entry.updated = time.Now()
	}
}

func (s *taskStore) setTask(id string, task Task) {
	s.mu.Lock()
	defer s.mu.Unlock()
	if entry := s.find(id, ""); entry != nil {
		entry.task = task
		entry.updated = time.Now()
	}
}

func (s *taskStore) appendArtifact(id string, artifact Artifact) {
	s.mu.Lock()
	defer s.mu.Unlock()
	if entry := s.find(id, ""); entry != nil {
		entry.task.Artifacts = append(entry.task.Artifacts, artifact)
		entry.updated = time.Now()
	}
}

func (s *taskStore) appendHistory(id string, message Message) {
	s.mu.Lock()
	defer s.mu.Unlock()
	if entry := s.find(id, ""); entry != nil {
		entry.task.History = append(entry.task.History, message)
		entry.updated = time.Now()
	}
}

func (s *taskStore) get(id, owner string) (Task, bool) {
	s.mu.Lock()
	defer s.mu.Unlock()
	entry := s.find(id, owner)
	if entry == nil || (entry.owner != "" && owner != "" && entry.owner != owner) {
		return Task{}, false
	}
	if time.Since(entry.updated) > taskTTL {
		delete(s.tasks, s.key(entry.task.ID, entry.owner))
		return Task{}, false
	}
	return entry.task, true
}

func (s *taskStore) list(owner, contextID, state string) []Task {
	s.mu.Lock()
	defer s.mu.Unlock()
	out := make([]Task, 0, len(s.tasks))
	for key, entry := range s.tasks {
		if entry.owner != "" && owner != "" && entry.owner != owner {
			continue
		}
		if time.Since(entry.updated) > taskTTL {
			delete(s.tasks, key)
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
	entry := s.find(id, owner)
	if entry == nil || (entry.owner != "" && owner != "" && entry.owner != owner) {
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
