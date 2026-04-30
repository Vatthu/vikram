package epistemology

import (
	"time"
)

// Fact represents a single, verifiable statement in the agent's memory.
// It follows a Subject-Predicate-Object structure.
type Fact struct {
	ID         string    `json:"id"`
	Subject    string    `json:"subject"`    // e.g., "User"
	Predicate  string    `json:"predicate"`  // e.g., "prefers"
	Object     string    `json:"object"`     // e.g., "concise answers"
	Confidence float64   `json:"confidence"` // 0.0 to 1.0
	Source     string    `json:"source"`     // How this was learned (e.g., "chat_inference", "user_explicit")
	CreatedAt  time.Time `json:"created_at"`
	UpdatedAt  time.Time `json:"updated_at"`
	RefutedBy  string    `json:"refuted_by,omitempty"` // ID of another fact that contradicts this one
}

// Query allows searching the epistemological graph.
type Query struct {
	Subject   string  `json:"subject,omitempty"`
	Predicate string  `json:"predicate,omitempty"`
	Object    string  `json:"object,omitempty"`
	MinConf   float64 `json:"min_confidence,omitempty"`
}

// GraphStore defines the interface for interacting with the structured memory.
type GraphStore interface {
	AssertFact(subject, predicate, object, source string, confidence float64) (string, error)
	Query(q Query) ([]Fact, error)
	UpdateConfidence(id string, confidence float64) error
	RefuteFact(targetID, refutingID string) error
	Close() error
}
