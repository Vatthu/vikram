package main

import (
	"fmt"
	"sync"
	"time"

	"github.com/Vatthu/vikram/pkg/config"
)

// agentBudget tracks daily token usage per agent role and notifies (or
// optionally stops) when a budget threshold is crossed.
type agentBudget struct {
	mu           sync.Mutex
	dailyTokens  map[string]int
	dailyLimits  map[string]int
	dailyActions map[string]string // "", "notify", "stop"
	lastReset    string
	notifyFn     func(role string, used, limit int) // called when budget exceeded
	notified     map[string]bool                    // only notify once per day per role
}

func newAgentBudget(cfg *config.Config) *agentBudget {
	b := &agentBudget{
		dailyTokens:  make(map[string]int),
		dailyLimits:  make(map[string]int),
		dailyActions: make(map[string]string),
		notified:     make(map[string]bool),
	}
	for _, a := range cfg.Agents.List {
		if a.MaxTokensPerDay > 0 {
			role := a.Role
			if role == "" {
				role = a.ID
			}
			b.dailyLimits[role] = a.MaxTokensPerDay
			action := a.BudgetAction
			if action == "" {
				action = "notify"
			}
			b.dailyActions[role] = action
		}
	}
	return b
}

func (b *agentBudget) setNotifier(fn func(role string, used, limit int)) {
	b.notifyFn = fn
}

func (b *agentBudget) check(role string) error {
	b.mu.Lock()
	defer b.mu.Unlock()

	today := time.Now().Format("2006-01-02")
	if b.lastReset != today {
		b.dailyTokens = make(map[string]int)
		b.notified = make(map[string]bool)
		b.lastReset = today
	}

	limit, ok := b.dailyLimits[role]
	if !ok || limit <= 0 {
		return nil
	}

	used := b.dailyTokens[role]
	if used >= limit {
		if b.notifyFn != nil && !b.notified[role] {
			b.notifyFn(role, used, limit)
			b.notified[role] = true
		}
		if b.dailyActions[role] == "stop" {
			return fmt.Errorf("agent role %q exceeded daily budget (%d/%d tokens)", role, used, limit)
		}
	}
	return nil
}

func (b *agentBudget) record(role string, tokens int) {
	b.mu.Lock()
	defer b.mu.Unlock()
	b.dailyTokens[role] += tokens
}

// Check implements agent.BudgetChecker.
func (b *agentBudget) Check(role string) error { return b.check(role) }

// Record implements agent.BudgetChecker.
func (b *agentBudget) Record(role string, tokens int) { b.record(role, tokens) }
