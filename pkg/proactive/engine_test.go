package proactive

import (
	"context"
	"testing"
	"time"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func newTestEngine(t *testing.T) *Engine {
	e, err := NewEngine(t.TempDir())
	require.NoError(t, err)
	return e
}

func TestEngine_RecordActivity(t *testing.T) {
	e := newTestEngine(t)
	e.RecordActivity("check_email", "telegram")
	e.RecordActivity("check_news", "cli")
	assert.Equal(t, 2, e.ActivityCount())
}

func TestEngine_AddRoutine(t *testing.T) {
	e := newTestEngine(t)
	id := e.AddRoutine(Routine{
		Name:      "Morning briefing",
		TimeOfDay: "08:00",
		Action:    "morning_briefing",
	})
	assert.NotEmpty(t, id)
	assert.Len(t, e.GetRoutines(), 1)
}

func TestEngine_RemoveRoutine(t *testing.T) {
	e := newTestEngine(t)
	id := e.AddRoutine(Routine{Name: "Test"})
	assert.True(t, e.RemoveRoutine(id))
	assert.Len(t, e.GetRoutines(), 0)
	assert.False(t, e.RemoveRoutine("nonexistent"))
}

func TestEngine_CheckRoutines(t *testing.T) {
	e := newTestEngine(t)
	now := time.Now()

	e.AddRoutine(Routine{
		Name:      "Now routine",
		TimeOfDay: now.Format("15:04"),
		Action:    "test_action",
	})
	e.AddRoutine(Routine{
		Name:      "Later routine",
		TimeOfDay: "23:59",
		Action:    "later_action",
	})

	triggered := e.CheckRoutines()
	assert.Len(t, triggered, 1)
	assert.Equal(t, "Now routine", triggered[0].Name)
}

func TestEngine_CheckRoutines_DayFilter(t *testing.T) {
	e := newTestEngine(t)
	now := time.Now()

	// Add routine for a different day.
	tomorrow := now.Weekday() + 1
	if tomorrow > time.Saturday {
		tomorrow = time.Sunday
	}
	e.AddRoutine(Routine{
		Name:       "Wrong day",
		TimeOfDay:  now.Format("15:04"),
		Action:     "test",
		DaysOfWeek: []time.Weekday{tomorrow},
	})

	triggered := e.CheckRoutines()
	assert.Len(t, triggered, 0)
}

func TestEngine_CheckRoutines_AlreadyTriggered(t *testing.T) {
	e := newTestEngine(t)
	now := time.Now()

	id := e.AddRoutine(Routine{
		Name:      "Already done",
		TimeOfDay: now.Format("15:04"),
		Action:    "test",
	})
	e.MarkRoutineTriggered(id)

	triggered := e.CheckRoutines()
	assert.Len(t, triggered, 0)
}

func TestEngine_DetectPatterns(t *testing.T) {
	e := newTestEngine(t)

	// Record same action multiple times at same hour.
	for i := 0; i < 5; i++ {
		e.RecordActivity("check_email", "telegram")
	}

	patterns := e.DetectPatterns(3)
	assert.True(t, len(patterns) >= 1)
	assert.Equal(t, "check_email", patterns[0].Action)
	assert.Equal(t, 5, patterns[0].Observations)
}

func TestEngine_DetectPatterns_BelowThreshold(t *testing.T) {
	e := newTestEngine(t)
	e.RecordActivity("rare_action", "cli")

	patterns := e.DetectPatterns(3)
	assert.Len(t, patterns, 0)
}

func TestEngine_Suggestions(t *testing.T) {
	e := newTestEngine(t)

	receivedCh := make(chan Suggestion, 1)
	e.SetHandler(func(ctx context.Context, s Suggestion) {
		receivedCh <- s
	})

	e.Suggest("reminder", "You have a meeting in 15 minutes", 1, 30*time.Minute)

	var received Suggestion
	select {
	case received = <-receivedCh:
	case <-time.After(2 * time.Second):
		t.Fatal("timeout waiting for suggestion callback")
	}

	assert.Equal(t, "reminder", received.Type)
	assert.Contains(t, received.Message, "meeting")

	pending := e.PendingSuggestions()
	assert.Len(t, pending, 1)
}

func TestEngine_Suggestions_Expired(t *testing.T) {
	e := newTestEngine(t)
	e.Suggest("test", "expired", 2, -1*time.Second) // Already expired

	pending := e.PendingSuggestions()
	assert.Len(t, pending, 0)
}

func TestEngine_DismissSuggestion(t *testing.T) {
	e := newTestEngine(t)
	e.Suggest("test", "dismiss me", 2, 1*time.Hour)

	pending := e.PendingSuggestions()
	require.Len(t, pending, 1)

	e.DismissSuggestion(pending[0].ID)
	assert.Len(t, e.PendingSuggestions(), 0)
}

func TestEngine_Persistence(t *testing.T) {
	dir := t.TempDir()

	e1, _ := NewEngine(dir)
	e1.AddRoutine(Routine{Name: "Persistent routine", TimeOfDay: "09:00"})
	e1.RecordActivity("test", "cli")

	e2, _ := NewEngine(dir)
	assert.Len(t, e2.GetRoutines(), 1)
	assert.Equal(t, 1, e2.ActivityCount())
}

func TestEngine_ActivityCap(t *testing.T) {
	e := newTestEngine(t)
	for i := 0; i < 1050; i++ {
		e.RecordActivity("action", "cli")
	}
	assert.Equal(t, 1000, e.ActivityCount())
}
