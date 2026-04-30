package queue

import (
	"context"
	"errors"
	"testing"
	"time"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func newTestQueue(t *testing.T) *Queue {
	q, err := NewQueue(t.TempDir())
	require.NoError(t, err)
	return q
}

func TestEnqueue(t *testing.T) {
	q := newTestQueue(t)

	id := q.Enqueue("email", PriorityNormal, map[string]interface{}{"to": "bob"}, time.Time{}, 3)
	assert.NotEmpty(t, id)
	assert.Equal(t, 1, q.PendingCount())
	assert.Equal(t, 1, q.JobCount())
}

func TestProcessJob_Success(t *testing.T) {
	q := newTestQueue(t)

	processed := false
	q.RegisterHandler("test", func(ctx context.Context, job *Job) error {
		processed = true
		return nil
	})

	q.Enqueue("test", PriorityNormal, nil, time.Time{}, 3)

	ctx := context.Background()
	q.processNext(ctx)

	assert.True(t, processed)
	assert.Equal(t, 0, q.PendingCount())
}

func TestProcessJob_Retry(t *testing.T) {
	q := newTestQueue(t)
	attempts := 0

	q.RegisterHandler("flaky", func(ctx context.Context, job *Job) error {
		attempts++
		if attempts < 3 {
			return errors.New("temporary failure")
		}
		return nil
	})

	q.Enqueue("flaky", PriorityNormal, nil, time.Time{}, 5)

	ctx := context.Background()
	// First attempt: fails, gets rescheduled
	q.processNext(ctx)
	assert.Equal(t, 1, attempts)

	job := q.jobs[0]
	assert.Equal(t, StatusPending, job.Status)
	assert.NotEmpty(t, job.LastError)
}

func TestProcessJob_DeadLetter(t *testing.T) {
	q := newTestQueue(t)

	q.RegisterHandler("bad", func(ctx context.Context, job *Job) error {
		return errors.New("always fails")
	})

	q.Enqueue("bad", PriorityNormal, nil, time.Time{}, 1)

	ctx := context.Background()
	q.processNext(ctx)

	assert.Equal(t, 1, q.DeadCount())
	assert.Equal(t, 0, q.PendingCount())
}

func TestPriorityOrder(t *testing.T) {
	q := newTestQueue(t)

	var order []string
	q.RegisterHandler("task", func(ctx context.Context, job *Job) error {
		name, _ := job.Payload["name"].(string)
		order = append(order, name)
		return nil
	})

	q.Enqueue("task", PriorityLow, map[string]interface{}{"name": "low"}, time.Time{}, 1)
	q.Enqueue("task", PriorityCritical, map[string]interface{}{"name": "critical"}, time.Time{}, 1)
	q.Enqueue("task", PriorityHigh, map[string]interface{}{"name": "high"}, time.Time{}, 1)

	ctx := context.Background()
	q.processNext(ctx)
	q.processNext(ctx)
	q.processNext(ctx)

	assert.Equal(t, []string{"critical", "high", "low"}, order)
}

func TestScheduledJob(t *testing.T) {
	q := newTestQueue(t)

	q.RegisterHandler("later", func(ctx context.Context, job *Job) error {
		return nil
	})

	// Schedule 10 seconds in the future
	q.Enqueue("later", PriorityNormal, nil, time.Now().Add(10*time.Second), 1)

	ctx := context.Background()
	q.processNext(ctx) // Should not run

	assert.Equal(t, 1, q.PendingCount())
}

func TestGetJob(t *testing.T) {
	q := newTestQueue(t)

	id := q.Enqueue("test", PriorityHigh, map[string]interface{}{"x": 1}, time.Time{}, 3)

	job := q.GetJob(id)
	require.NotNil(t, job)
	assert.Equal(t, "test", job.Kind)
	assert.Equal(t, PriorityHigh, job.Priority)
}

func TestGetJob_NotFound(t *testing.T) {
	q := newTestQueue(t)
	assert.Nil(t, q.GetJob("nonexistent"))
}

func TestPurge(t *testing.T) {
	q := newTestQueue(t)

	q.RegisterHandler("test", func(ctx context.Context, job *Job) error {
		return nil
	})

	q.Enqueue("test", PriorityNormal, nil, time.Time{}, 1)
	q.processNext(context.Background())

	assert.Equal(t, 1, q.JobCount())

	// Purge with 0 maxAge removes all completed
	purged := q.Purge(0)
	assert.Equal(t, 1, purged)
	assert.Equal(t, 0, q.JobCount())
}

func TestPersistence(t *testing.T) {
	dir := t.TempDir()

	// Create queue and add a job.
	q1, err := NewQueue(dir)
	require.NoError(t, err)
	q1.Enqueue("persist", PriorityNormal, map[string]interface{}{"data": "hello"}, time.Time{}, 3)
	assert.Equal(t, 1, q1.PendingCount())

	// Load a new queue from same dir.
	q2, err := NewQueue(dir)
	require.NoError(t, err)
	assert.Equal(t, 1, q2.PendingCount())

	job := q2.jobs[0]
	assert.Equal(t, "persist", job.Kind)
}

func TestRecoverRunningJobs(t *testing.T) {
	dir := t.TempDir()

	q1, _ := NewQueue(dir)
	q1.Enqueue("task", PriorityNormal, nil, time.Time{}, 3)
	// Simulate crash: mark as running and save.
	q1.mu.Lock()
	q1.jobs[0].Status = StatusRunning
	q1.save()
	q1.mu.Unlock()

	// Reload: should recover to pending.
	q2, _ := NewQueue(dir)
	assert.Equal(t, 1, q2.PendingCount())
	assert.Equal(t, StatusPending, q2.jobs[0].Status)
}

func TestStartStop(t *testing.T) {
	q := newTestQueue(t)

	processed := make(chan bool, 1)
	q.RegisterHandler("test", func(ctx context.Context, job *Job) error {
		processed <- true
		return nil
	})

	q.Enqueue("test", PriorityNormal, nil, time.Time{}, 1)

	ctx, cancel := context.WithCancel(context.Background())
	q.Start(ctx, 50*time.Millisecond)

	select {
	case <-processed:
		// Job was processed.
	case <-time.After(2 * time.Second):
		t.Fatal("job not processed in time")
	}

	cancel()
	time.Sleep(100 * time.Millisecond)
}

func TestStopSignalsStopChannel(t *testing.T) {
	q := newTestQueue(t)

	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()

	q.Start(ctx, time.Hour)
	q.Stop()

	select {
	case <-q.stopCh:
		// stop signal delivered
	case <-time.After(500 * time.Millisecond):
		t.Fatal("stop channel was not signaled")
	}
}

func TestStartAfterStop_RecreatesStopChannel(t *testing.T) {
	q := newTestQueue(t)

	ctx1, cancel1 := context.WithCancel(context.Background())
	q.Start(ctx1, time.Hour)
	firstStopCh := q.stopCh
	q.Stop()
	cancel1()

	select {
	case <-firstStopCh:
		// expected closed
	default:
		t.Fatal("first stop channel should be closed after Stop")
	}

	ctx2, cancel2 := context.WithCancel(context.Background())
	defer cancel2()

	q.Start(ctx2, time.Millisecond)
	secondStopCh := q.stopCh
	if firstStopCh == secondStopCh {
		t.Fatal("start should recreate a fresh stop channel")
	}

	select {
	case <-secondStopCh:
		t.Fatal("new stop channel must be open while queue is running")
	default:
	}

	q.Stop()
}

func TestNoHandler(t *testing.T) {
	q := newTestQueue(t)

	q.Enqueue("unhandled", PriorityNormal, nil, time.Time{}, 1)
	q.processNext(context.Background())

	// Job remains pending — no handler to process it.
	assert.Equal(t, 1, q.PendingCount())
}
