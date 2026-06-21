package proactive

import (
	"context"
	"encoding/json"
	"fmt"
	"os"
	"path/filepath"
	"sort"
	"sync"
	"time"

	"github.com/Vatthu/vikram/pkg/logger"
)

// Routine represents a learned user behavior pattern.
type Routine struct {
	ID            string         `json:"id"`
	Name          string         `json:"name"`
	Description   string         `json:"description"`
	TimeOfDay     string         `json:"time_of_day"`  // "HH:MM" in local time
	DaysOfWeek    []time.Weekday `json:"days_of_week"` // Empty = every day
	Action        string         `json:"action"`       // Action to suggest/execute
	Confidence    float64        `json:"confidence"`   // 0.0-1.0 based on observation count
	Observations  int            `json:"observations"` // Times this pattern was observed
	LastTriggered time.Time      `json:"last_triggered"`
	Enabled       bool           `json:"enabled"`
}

// Suggestion represents a proactive suggestion V1 can make.
type Suggestion struct {
	ID        string    `json:"id"`
	Type      string    `json:"type"` // "reminder", "routine", "anomaly", "insight"
	Message   string    `json:"message"`
	Priority  int       `json:"priority"`
	ExpiresAt time.Time `json:"expires_at"`
	Action    string    `json:"action,omitempty"` // Optional action to execute
}

// ActivityLog records user activities for pattern detection.
type ActivityLog struct {
	Timestamp time.Time `json:"timestamp"`
	Action    string    `json:"action"`
	Channel   string    `json:"channel"`
	DayOfWeek int       `json:"day_of_week"`
	HourOfDay int       `json:"hour_of_day"`
}

// Engine manages proactive intelligence — routines, suggestions, anomalies.
type Engine struct {
	mu           sync.RWMutex
	dataDir      string
	routines     []Routine
	activities   []ActivityLog
	suggestions  []Suggestion
	handler      func(ctx context.Context, suggestion Suggestion)
	engineCtx    context.Context
	engineCancel context.CancelFunc
}

// NewEngine creates a proactive intelligence engine.
func NewEngine(dataDir string) (*Engine, error) {
	ctx, cancel := context.WithCancel(context.Background())
	e := &Engine{dataDir: dataDir, engineCtx: ctx, engineCancel: cancel}
	if err := e.load(); err != nil {
		cancel()
		return nil, fmt.Errorf("proactive engine load: %w", err)
	}
	logger.InfoCF("proactive", "Proactive engine loaded", map[string]interface{}{
		"routines":   len(e.routines),
		"activities": len(e.activities),
	})
	return e, nil
}

// Close cancels the engine's lifecycle context, stopping all handler goroutines.
func (e *Engine) Close() {
	e.engineCancel()
}

// SetHandler sets the callback for proactive suggestions.
func (e *Engine) SetHandler(handler func(ctx context.Context, suggestion Suggestion)) {
	e.mu.Lock()
	defer e.mu.Unlock()
	e.handler = handler
}

// RecordActivity logs a user activity for pattern learning.
func (e *Engine) RecordActivity(action, channel string) {
	now := time.Now()
	e.mu.Lock()
	defer e.mu.Unlock()

	e.activities = append(e.activities, ActivityLog{
		Timestamp: now,
		Action:    action,
		Channel:   channel,
		DayOfWeek: int(now.Weekday()),
		HourOfDay: now.Hour(),
	})

	// Keep last 1000 activities.
	if len(e.activities) > 1000 {
		e.activities = e.activities[len(e.activities)-1000:]
	}
	e.save()
}

// AddRoutine manually adds a routine.
func (e *Engine) AddRoutine(routine Routine) string {
	e.mu.Lock()
	defer e.mu.Unlock()

	if routine.ID == "" {
		routine.ID = fmt.Sprintf("routine_%d", time.Now().UnixNano())
	}
	routine.Enabled = true
	e.routines = append(e.routines, routine)
	e.save()
	return routine.ID
}

// GetRoutines returns all routines.
func (e *Engine) GetRoutines() []Routine {
	e.mu.RLock()
	defer e.mu.RUnlock()
	result := make([]Routine, len(e.routines))
	copy(result, e.routines)
	return result
}

// RemoveRoutine removes a routine by ID.
func (e *Engine) RemoveRoutine(id string) bool {
	e.mu.Lock()
	defer e.mu.Unlock()
	for i, r := range e.routines {
		if r.ID == id {
			e.routines = append(e.routines[:i], e.routines[i+1:]...)
			e.save()
			return true
		}
	}
	return false
}

// CheckRoutines returns routines that should trigger now.
func (e *Engine) CheckRoutines() []Routine {
	e.mu.RLock()
	defer e.mu.RUnlock()

	now := time.Now()
	currentTime := now.Format("15:04")
	currentDay := now.Weekday()

	var triggered []Routine
	for _, r := range e.routines {
		if !r.Enabled || r.TimeOfDay != currentTime {
			continue
		}
		// Check if already triggered today.
		if r.LastTriggered.Day() == now.Day() && r.LastTriggered.Month() == now.Month() {
			continue
		}
		// Check day of week filter.
		if len(r.DaysOfWeek) > 0 {
			found := false
			for _, d := range r.DaysOfWeek {
				if d == currentDay {
					found = true
					break
				}
			}
			if !found {
				continue
			}
		}
		triggered = append(triggered, r)
	}
	return triggered
}

// MarkRoutineTriggered updates the last triggered time.
func (e *Engine) MarkRoutineTriggered(id string) {
	e.mu.Lock()
	defer e.mu.Unlock()
	for i := range e.routines {
		if e.routines[i].ID == id {
			e.routines[i].LastTriggered = time.Now()
			break
		}
	}
	e.save()
}

// DetectPatterns analyzes activity logs to find recurring patterns.
func (e *Engine) DetectPatterns(minOccurrences int) []Routine {
	e.mu.RLock()
	defer e.mu.RUnlock()

	if minOccurrences <= 0 {
		minOccurrences = 3
	}

	// Group activities by hour and action.
	type patternKey struct {
		hour   int
		action string
	}
	counts := make(map[patternKey]int)
	for _, a := range e.activities {
		key := patternKey{hour: a.HourOfDay, action: a.Action}
		counts[key]++
	}

	var patterns []Routine
	for key, count := range counts {
		if count >= minOccurrences {
			patterns = append(patterns, Routine{
				Name:         fmt.Sprintf("Auto: %s", key.action),
				Description:  fmt.Sprintf("Detected %d occurrences at %02d:00", count, key.hour),
				TimeOfDay:    fmt.Sprintf("%02d:00", key.hour),
				Action:       key.action,
				Confidence:   float64(count) / float64(len(e.activities)),
				Observations: count,
			})
		}
	}

	// Sort by confidence descending.
	sort.Slice(patterns, func(i, j int) bool {
		return patterns[i].Confidence > patterns[j].Confidence
	})

	return patterns
}

// Suggest creates a proactive suggestion.
func (e *Engine) Suggest(sugType, message string, priority int, expiresIn time.Duration) {
	e.mu.Lock()
	defer e.mu.Unlock()

	s := Suggestion{
		ID:        fmt.Sprintf("sug_%d", time.Now().UnixNano()),
		Type:      sugType,
		Message:   message,
		Priority:  priority,
		ExpiresAt: time.Now().Add(expiresIn),
	}
	e.suggestions = append(e.suggestions, s)

	if e.handler != nil {
		go e.handler(e.engineCtx, s)
	}
}

// PendingSuggestions returns active (non-expired) suggestions.
func (e *Engine) PendingSuggestions() []Suggestion {
	e.mu.RLock()
	defer e.mu.RUnlock()

	now := time.Now()
	var active []Suggestion
	for _, s := range e.suggestions {
		if s.ExpiresAt.After(now) {
			active = append(active, s)
		}
	}
	return active
}

// DismissSuggestion removes a suggestion by ID.
func (e *Engine) DismissSuggestion(id string) {
	e.mu.Lock()
	defer e.mu.Unlock()
	for i, s := range e.suggestions {
		if s.ID == id {
			e.suggestions = append(e.suggestions[:i], e.suggestions[i+1:]...)
			return
		}
	}
}

// ActivityCount returns total recorded activities.
func (e *Engine) ActivityCount() int {
	e.mu.RLock()
	defer e.mu.RUnlock()
	return len(e.activities)
}

// --- Persistence ---

type persistData struct {
	Routines   []Routine     `json:"routines"`
	Activities []ActivityLog `json:"activities"`
}

func (e *Engine) load() error {
	path := filepath.Join(e.dataDir, "proactive.json")
	data, err := os.ReadFile(path)
	if err != nil {
		if os.IsNotExist(err) {
			e.routines = []Routine{}
			e.activities = []ActivityLog{}
			e.suggestions = []Suggestion{}
			return nil
		}
		return err
	}
	var pd persistData
	if err := json.Unmarshal(data, &pd); err != nil {
		return err
	}
	e.routines = pd.Routines
	e.activities = pd.Activities
	e.suggestions = []Suggestion{}
	return nil
}

func (e *Engine) save() {
	pd := persistData{Routines: e.routines, Activities: e.activities}
	data, err := json.Marshal(pd)
	if err != nil {
		return
	}
	path := filepath.Join(e.dataDir, "proactive.json")
	tmpPath := path + ".tmp"
	if err := os.WriteFile(tmpPath, data, 0600); err != nil {
		return
	}
	if err := os.Rename(tmpPath, path); err != nil {
		logger.ErrorC("proactive", fmt.Sprintf("Failed to rename proactive data file: %v", err))
	}
}
