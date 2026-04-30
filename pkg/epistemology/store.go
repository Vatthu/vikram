package epistemology

import (
	"encoding/json"
	"fmt"
	"os"
	"path/filepath"
	"strings"
	"sync"
	"time"

	"github.com/v1claw/levik/pkg/logger"
)

// JSONGraphStore implements GraphStore using a local JSON file.
// It is designed to be lightweight and strictly logical.
type JSONGraphStore struct {
	mu       sync.RWMutex
	filePath string
	facts    map[string]Fact
}

// NewJSONGraphStore creates or loads a JSON-backed knowledge graph.
func NewJSONGraphStore(workspaceDir string) (*JSONGraphStore, error) {
	dir := filepath.Join(workspaceDir, "epistemology")
	if err := os.MkdirAll(dir, 0700); err != nil {
		return nil, fmt.Errorf("failed to create epistemology dir: %w", err)
	}

	filePath := filepath.Join(dir, "graph.json")
	store := &JSONGraphStore{
		filePath: filePath,
		facts:    make(map[string]Fact),
	}

	if err := store.load(); err != nil {
		return nil, err
	}

	return store, nil
}

// AssertFact adds a new fact to the graph or updates an existing identical one.
func (s *JSONGraphStore) AssertFact(subject, predicate, object, source string, confidence float64) (string, error) {
	s.mu.Lock()
	defer s.mu.Unlock()

	// Check for exact duplicate
	for id, fact := range s.facts {
		if strings.EqualFold(fact.Subject, subject) &&
			strings.EqualFold(fact.Predicate, predicate) &&
			strings.EqualFold(fact.Object, object) {

			// Update confidence and source if needed
			fact.Confidence = confidence
			fact.Source = source
			fact.UpdatedAt = time.Now()
			s.facts[id] = fact
			s.save()
			return id, nil
		}
	}

	// Create new fact
	id := fmt.Sprintf("fact_%d", time.Now().UnixNano())
	now := time.Now()

	newFact := Fact{
		ID:         id,
		Subject:    subject,
		Predicate:  predicate,
		Object:     object,
		Confidence: confidence,
		Source:     source,
		CreatedAt:  now,
		UpdatedAt:  now,
	}

	s.facts[id] = newFact
	s.save()

	logger.DebugCF("epistemology", "Fact asserted", map[string]interface{}{
		"id": id, "subject": subject, "predicate": predicate, "object": object,
	})

	return id, nil
}

// Query searches the graph for facts matching the criteria.
func (s *JSONGraphStore) Query(q Query) ([]Fact, error) {
	s.mu.RLock()
	defer s.mu.RUnlock()

	var results []Fact
	for _, fact := range s.facts {
		// If refuted, skip it (unless specifically querying for refuted facts, which we don't support yet)
		if fact.RefutedBy != "" {
			continue
		}

		if fact.Confidence < q.MinConf {
			continue
		}

		if q.Subject != "" && !strings.EqualFold(fact.Subject, q.Subject) {
			continue
		}
		if q.Predicate != "" && !strings.EqualFold(fact.Predicate, q.Predicate) {
			continue
		}
		if q.Object != "" && !strings.Contains(strings.ToLower(fact.Object), strings.ToLower(q.Object)) {
			continue
		}

		results = append(results, fact)
	}

	return results, nil
}

// UpdateConfidence allows the agent to strengthen or weaken its belief in a fact.
func (s *JSONGraphStore) UpdateConfidence(id string, confidence float64) error {
	s.mu.Lock()
	defer s.mu.Unlock()

	fact, exists := s.facts[id]
	if !exists {
		return fmt.Errorf("fact %s not found", id)
	}

	fact.Confidence = confidence
	fact.UpdatedAt = time.Now()
	s.facts[id] = fact
	s.save()

	return nil
}

// RefuteFact marks a fact as contradicted by another fact.
func (s *JSONGraphStore) RefuteFact(targetID, refutingID string) error {
	s.mu.Lock()
	defer s.mu.Unlock()

	fact, exists := s.facts[targetID]
	if !exists {
		return fmt.Errorf("target fact %s not found", targetID)
	}

	if _, exists := s.facts[refutingID]; !exists {
		return fmt.Errorf("refuting fact %s not found", refutingID)
	}

	fact.RefutedBy = refutingID
	fact.UpdatedAt = time.Now()
	s.facts[targetID] = fact
	s.save()

	return nil
}

func (s *JSONGraphStore) Close() error {
	return nil
}

// --- Internal persistence methods ---

func (s *JSONGraphStore) load() error {
	data, err := os.ReadFile(s.filePath)
	if err != nil {
		if os.IsNotExist(err) {
			s.facts = make(map[string]Fact)
			return nil
		}
		return fmt.Errorf("read graph file: %w", err)
	}

	if len(data) == 0 {
		return nil
	}

	return json.Unmarshal(data, &s.facts)
}

func (s *JSONGraphStore) save() {
	data, err := json.MarshalIndent(s.facts, "", "  ")
	if err != nil {
		logger.ErrorC("epistemology", fmt.Sprintf("failed to marshal graph: %v", err))
		return
	}

	tmpPath := s.filePath + ".tmp"
	if err := os.WriteFile(tmpPath, data, 0600); err != nil {
		logger.ErrorC("epistemology", fmt.Sprintf("failed to write tmp graph file: %v", err))
		return
	}

	if err := os.Rename(tmpPath, s.filePath); err != nil {
		logger.ErrorC("epistemology", fmt.Sprintf("failed to rename graph file: %v", err))
	}
}
