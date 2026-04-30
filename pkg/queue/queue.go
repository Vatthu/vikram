package queue

import (
	"context"
	"encoding/json"
	"fmt"
	"os"
	"path/filepath"
	"sort"
	"sync"
	"sync/atomic"
	"time"

	"github.com/v1claw/levik/pkg/logger"
)

// jobCounter provides collision-free job IDs within a process lifetime.
var jobCounter atomic.Int64

// Priority levels for jobs.
const (
	PriorityCritical = 0
	PriorityHigh     = 1
	PriorityNormal   = 2
	PriorityLow      = 3
)

// Job status values.
const (
	StatusPending = "pending"
	StatusRunning = "running"
	StatusDone    = "done"
	StatusFailed  = "failed"
	StatusDead    = "dead" // Exhausted retries.
)

// Job represents a unit of work in the queue.
type Job struct {
	ID          string                 `json:"id"`
	Kind        string                 `json:"kind"`
	Priority    int                    `json:"priority"`
	Payload     map[string]interface{} `json:"payload,omitempty"`
	Status      string                 `json:"status"`
	Attempts    int                    `json:"attempts"`
	MaxRetries  int                    `json:"max_retries"`
	CreatedAt   time.Time              `json:"created_at"`
	ScheduledAt time.Time              `json:"scheduled_at"`
	StartedAt   *time.Time             `json:"started_at,omitempty"`
	FinishedAt  *time.Time             `json:"finished_at,omitempty"`
	LastError   string                 `json:"last_error,omitempty"`
}

// JobHandler processes a job.
type JobHandler func(ctx context.Context, job *Job) error

// Queue is a persistent, priority-based job queue backed by a JSON file.
type Queue struct {
	mu       sync.Mutex
	path     string
	jobs     []*Job
	handlers map[string]JobHandler
	ticker   *time.Ticker
	stopCh   chan struct{}
	running  bool
	runID    uint64
}

// NewQueue creates a new persistent queue stored at the given directory.
func NewQueue(dir string) (*Queue, error) {
	path := filepath.Join(dir, "job_queue.json")
	q := &Queue{
		path:     path,
		handlers: make(map[string]JobHandler),
		stopCh:   make(chan struct{}),
	}
	if err := q.load(); err != nil {
		return nil, fmt.Errorf("queue load: %w", err)
	}
	// Reset any jobs that were running when we last shut down.
	for _, j := range q.jobs {
		if j.Status == StatusRunning {
			j.Status = StatusPending
		}
	}
	return q, nil
}

// RegisterHandler registers a handler for a job kind.
func (q *Queue) RegisterHandler(kind string, handler JobHandler) {
	q.mu.Lock()
	defer q.mu.Unlock()
	q.handlers[kind] = handler
}

// Enqueue adds a job to the queue. If scheduledAt is zero, it runs immediately.
func (q *Queue) Enqueue(kind string, priority int, payload map[string]interface{}, scheduledAt time.Time, maxRetries int) string {
	q.mu.Lock()
	defer q.mu.Unlock()

	if scheduledAt.IsZero() {
		scheduledAt = time.Now()
	}
	if maxRetries <= 0 {
		maxRetries = 3
	}

	job := &Job{
		ID:          fmt.Sprintf("job_%d", jobCounter.Add(1)),
		Kind:        kind,
		Priority:    priority,
		Payload:     payload,
		Status:      StatusPending,
		MaxRetries:  maxRetries,
		CreatedAt:   time.Now(),
		ScheduledAt: scheduledAt,
	}
	q.jobs = append(q.jobs, job)
	q.save()

	logger.DebugC("queue", fmt.Sprintf("Enqueued job %s (kind=%s, priority=%d)", job.ID, kind, priority))
	return job.ID
}

// Start begins processing jobs at the given interval.
func (q *Queue) Start(ctx context.Context, interval time.Duration) {
	q.mu.Lock()
	if q.running {
		q.mu.Unlock()
		return
	}
	q.running = true
	q.runID++
	currentRunID := q.runID
	q.stopCh = make(chan struct{})
	stopCh := q.stopCh
	q.ticker = time.NewTicker(interval)
	ticker := q.ticker
	q.mu.Unlock()

	logger.InfoC("queue", fmt.Sprintf("Job queue started (poll interval: %s, pending: %d)", interval, q.PendingCount()))

	go func() {
		for {
			select {
			case <-ctx.Done():
				q.stopWithRunID(currentRunID)
				return
			case <-stopCh:
				return
			case <-ticker.C:
				q.processNext(ctx)
			}
		}
	}()
}

// Stop halts job processing.
func (q *Queue) Stop() {
	q.stopWithRunID(0)
}

func (q *Queue) stopWithRunID(runID uint64) {
	q.mu.Lock()
	defer q.mu.Unlock()
	if !q.running {
		return
	}
	if runID != 0 && q.runID != runID {
		return
	}
	q.running = false
	if q.ticker != nil {
		q.ticker.Stop()
		q.ticker = nil
	}
	if q.stopCh != nil {
		select {
		case <-q.stopCh:
		default:
			close(q.stopCh)
		}
	}
	logger.InfoC("queue", "Job queue stopped")
}

func (q *Queue) processNext(ctx context.Context) {
	q.mu.Lock()
	job := q.nextPending()
	if job == nil {
		q.mu.Unlock()
		return
	}

	handler, ok := q.handlers[job.Kind]
	if !ok {
		q.mu.Unlock()
		return
	}

	now := time.Now()
	job.Status = StatusRunning
	job.StartedAt = &now
	job.Attempts++
	q.save()
	q.mu.Unlock()

	err := handler(ctx, job)

	q.mu.Lock()
	defer q.mu.Unlock()

	finished := time.Now()
	job.FinishedAt = &finished

	if err != nil {
		job.LastError = err.Error()
		if job.Attempts >= job.MaxRetries {
			job.Status = StatusDead
			logger.WarnC("queue", fmt.Sprintf("Job %s moved to dead letter (kind=%s, attempts=%d): %v", job.ID, job.Kind, job.Attempts, err))
		} else {
			// Exponential backoff: 2^attempts seconds.
			backoff := time.Duration(1<<uint(job.Attempts)) * time.Second
			job.ScheduledAt = time.Now().Add(backoff)
			job.Status = StatusPending
			logger.DebugC("queue", fmt.Sprintf("Job %s retry #%d in %s: %v", job.ID, job.Attempts, backoff, err))
		}
	} else {
		job.Status = StatusDone
		logger.DebugC("queue", fmt.Sprintf("Job %s completed (kind=%s)", job.ID, job.Kind))
	}

	q.save()
}

func (q *Queue) nextPending() *Job {
	now := time.Now()
	var candidates []*Job
	for _, j := range q.jobs {
		if j.Status == StatusPending && !j.ScheduledAt.After(now) {
			candidates = append(candidates, j)
		}
	}
	if len(candidates) == 0 {
		return nil
	}
	// Sort by priority (lower = higher priority), then by creation time.
	sort.Slice(candidates, func(i, j int) bool {
		if candidates[i].Priority != candidates[j].Priority {
			return candidates[i].Priority < candidates[j].Priority
		}
		return candidates[i].CreatedAt.Before(candidates[j].CreatedAt)
	})
	return candidates[0]
}

// PendingCount returns the number of pending jobs.
func (q *Queue) PendingCount() int {
	q.mu.Lock()
	defer q.mu.Unlock()
	count := 0
	for _, j := range q.jobs {
		if j.Status == StatusPending {
			count++
		}
	}
	return count
}

// DeadCount returns the number of dead-letter jobs.
func (q *Queue) DeadCount() int {
	q.mu.Lock()
	defer q.mu.Unlock()
	count := 0
	for _, j := range q.jobs {
		if j.Status == StatusDead {
			count++
		}
	}
	return count
}

// JobCount returns total number of jobs in all states.
func (q *Queue) JobCount() int {
	q.mu.Lock()
	defer q.mu.Unlock()
	return len(q.jobs)
}

// GetJob returns a job by ID.
func (q *Queue) GetJob(id string) *Job {
	q.mu.Lock()
	defer q.mu.Unlock()
	for _, j := range q.jobs {
		if j.ID == id {
			cp := *j
			return &cp
		}
	}
	return nil
}

// Purge removes completed and dead jobs older than maxAge.
func (q *Queue) Purge(maxAge time.Duration) int {
	q.mu.Lock()
	defer q.mu.Unlock()

	cutoff := time.Now().Add(-maxAge)
	var kept []*Job
	purged := 0
	for _, j := range q.jobs {
		if (j.Status == StatusDone || j.Status == StatusDead) && j.CreatedAt.Before(cutoff) {
			purged++
		} else {
			kept = append(kept, j)
		}
	}
	q.jobs = kept
	if purged > 0 {
		q.save()
	}
	return purged
}

// --- Persistence ---

func (q *Queue) load() error {
	data, err := os.ReadFile(q.path)
	if err != nil {
		if os.IsNotExist(err) {
			q.jobs = []*Job{}
			return nil
		}
		return err
	}
	return json.Unmarshal(data, &q.jobs)
}

func (q *Queue) save() {
	data, err := json.MarshalIndent(q.jobs, "", "  ")
	if err != nil {
		logger.ErrorC("queue", fmt.Sprintf("Failed to marshal queue: %v", err))
		return
	}
	tmpPath := q.path + ".tmp"
	if err := os.WriteFile(tmpPath, data, 0600); err != nil {
		logger.ErrorC("queue", fmt.Sprintf("Failed to write queue: %v", err))
		return
	}
	if err := os.Rename(tmpPath, q.path); err != nil {
		logger.ErrorC("queue", fmt.Sprintf("Failed to rename queue file: %v", err))
	}
}
