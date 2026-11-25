package a2a

import (
	"context"
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
	s.tasks[id] = entry
	return entry
}

func (s *taskStore) setStatus(id, state string) {
	s.mu.Lock()
	defer s.mu.Unlock()
	if entry := s.tasks[id]; entry != nil {
		entry.task.Status = TaskStatus{State: state, Timestamp: timestamp()}
		entry.updated = time.Now()
	}
}

func (s *taskStore) setTask(id string, task Task) {
	s.mu.Lock()
	defer s.mu.Unlock()
	if entry := s.tasks[id]; entry != nil {
		entry.task = task
		entry.updated = time.Now()
	}
}

func (s *taskStore) appendArtifact(id string, artifact Artifact) {
	s.mu.Lock()
	defer s.mu.Unlock()
	if entry := s.tasks[id]; entry != nil {
		entry.task.Artifacts = append(entry.task.Artifacts, artifact)
		entry.updated = time.Now()
	}
}

func (s *taskStore) appendHistory(id string, message Message) {
	s.mu.Lock()
	defer s.mu.Unlock()
	if entry := s.tasks[id]; entry != nil {
		entry.task.History = append(entry.task.History, message)
		entry.updated = time.Now()
	}
}

func (s *taskStore) get(id, owner string) (Task, bool) {
	s.mu.Lock()
	defer s.mu.Unlock()
	entry := s.tasks[id]
	if entry == nil || (entry.owner != "" && owner != "" && entry.owner != owner) {
		return Task{}, false
	}
	if time.Since(entry.updated) > taskTTL {
		delete(s.tasks, id)
		return Task{}, false
	}
	return entry.task, true
}

func (s *taskStore) list(owner, contextID, state string) []Task {
	s.mu.Lock()
	defer s.mu.Unlock()
	out := make([]Task, 0, len(s.tasks))
	for id, entry := range s.tasks {
		if entry.owner != "" && owner != "" && entry.owner != owner {
			continue
		}
		if time.Since(entry.updated) > taskTTL {
			delete(s.tasks, id)
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
	return out
}

func (s *taskStore) cancel(id, owner string) (Task, bool) {
	s.mu.Lock()
	entry := s.tasks[id]
	if entry == nil || (entry.owner != "" && owner != "" && entry.owner != owner) {
		s.mu.Unlock()
		return Task{}, false
	}
	if entry.cancel != nil {
		entry.cancel()
	}
	entry.task.Status = TaskStatus{State: "TASK_STATE_CANCELED", Timestamp: timestamp()}
	entry.updated = time.Now()
	task := entry.task
	s.mu.Unlock()
	return task, true
}

func timestamp() string {
	return time.Now().UTC().Format("2006-01-02T15:04:05.000Z")
}
