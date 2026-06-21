package agent

// BudgetChecker allows the agent loop to enforce per-role token budgets.
//
// The concrete implementation lives in cmd/vikram (agentBudget struct).
// This interface decouples pkg/agent from the command layer so the agent
// package doesn't need to import cmd/.
//
// When nil is passed to NewAgentLoop, budget enforcement is silently
// skipped (backward-compatible default).
type BudgetChecker interface {
	// Check returns a non-nil error if the given role has exceeded its
	// daily token budget and the configured action is "stop".
	// For "notify" action, it triggers a notification but returns nil.
	Check(role string) error

	// Record adds the given token count to the daily accumulator for the role.
	Record(role string, tokens int)
}
