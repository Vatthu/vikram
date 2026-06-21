package epistemology

import (
	"database/sql"
	"fmt"
	"os"
	"path/filepath"
	"sync"
	"time"

	"github.com/Vatthu/vikram/pkg/logger"
	_ "modernc.org/sqlite"
)

// SQLiteGraphStore implements GraphStore using a local SQLite database.
type SQLiteGraphStore struct {
	mu   sync.Mutex
	db   *sql.DB
	path string
}

// NewSQLiteGraphStore creates or opens an SQLite-backed knowledge graph.
func NewSQLiteGraphStore(workspaceDir string) (*SQLiteGraphStore, error) {
	dir := filepath.Join(workspaceDir, "epistemology")
	if err := os.MkdirAll(dir, 0755); err != nil {
		return nil, fmt.Errorf("failed to create epistemology dir: %w", err)
	}

	path := filepath.Join(dir, "graph.db")
	db, err := sql.Open("sqlite", path)
	if err != nil {
		return nil, fmt.Errorf("failed to open sqlite graph: %w", err)
	}

	store := &SQLiteGraphStore{
		db:   db,
		path: path,
	}

	if err := store.init(); err != nil {
		db.Close()
		return nil, err
	}

	return store, nil
}

func (s *SQLiteGraphStore) init() error {
	// Enable foreign-key enforcement (SQLite disables it by default).
	if _, err := s.db.Exec("PRAGMA foreign_keys = ON"); err != nil {
		return fmt.Errorf("failed to enable foreign keys: %w", err)
	}

	query := `
	CREATE TABLE IF NOT EXISTS facts (
		id TEXT PRIMARY KEY,
		subject TEXT NOT NULL,
		predicate TEXT NOT NULL,
		object TEXT NOT NULL,
		confidence REAL DEFAULT 1.0,
		source TEXT,
		created_at DATETIME,
		updated_at DATETIME,
		refuted_by TEXT,
		FOREIGN KEY(refuted_by) REFERENCES facts(id)
	);
	CREATE INDEX IF NOT EXISTS idx_facts_subject   ON facts(subject);
	CREATE INDEX IF NOT EXISTS idx_facts_predicate ON facts(predicate);
	CREATE INDEX IF NOT EXISTS idx_facts_object    ON facts(object);
	`
	_, err := s.db.Exec(query)
	return err
}

func (s *SQLiteGraphStore) AssertFact(subject, predicate, object, source string, confidence float64) (string, error) {
	s.mu.Lock()
	defer s.mu.Unlock()

	// Check for existing fact to update confidence/source
	var id string
	err := s.db.QueryRow(`
		SELECT id FROM facts
		WHERE subject = ? AND predicate = ? AND object = ? COLLATE NOCASE`,
		subject, predicate, object).Scan(&id)

	now := time.Now()
	if err == nil {
		// Update
		_, err = s.db.Exec(`
			UPDATE facts SET confidence = ?, source = ?, updated_at = ?
			WHERE id = ?`,
			confidence, source, now, id)
		return id, err
	}

	// Insert new
	id = fmt.Sprintf("fact_%d", now.UnixNano())
	_, err = s.db.Exec(`
		INSERT INTO facts (id, subject, predicate, object, confidence, source, created_at, updated_at)
		VALUES (?, ?, ?, ?, ?, ?, ?, ?)`,
		id, subject, predicate, object, confidence, source, now, now)

	if err == nil {
		logger.DebugCF("epistemology", "Fact asserted (sqlite)", map[string]interface{}{
			"id": id, "subject": subject, "predicate": predicate,
		})
	}

	return id, err
}

func (s *SQLiteGraphStore) Query(q Query) ([]Fact, error) {
	query := "SELECT id, subject, predicate, object, confidence, source, created_at, updated_at, refuted_by FROM facts WHERE refuted_by IS NULL"
	args := []interface{}{}

	if q.Subject != "" {
		query += " AND subject = ? COLLATE NOCASE"
		args = append(args, q.Subject)
	}
	if q.Predicate != "" {
		query += " AND predicate = ? COLLATE NOCASE"
		args = append(args, q.Predicate)
	}
	if q.Object != "" {
		query += " AND object LIKE ? COLLATE NOCASE"
		args = append(args, "%"+q.Object+"%")
	}
	if q.MinConf > 0 {
		query += " AND confidence >= ?"
		args = append(args, q.MinConf)
	}

	rows, err := s.db.Query(query, args...)
	if err != nil {
		return nil, err
	}
	defer rows.Close()

	var facts []Fact
	for rows.Next() {
		var f Fact
		var refutedBy sql.NullString
		err := rows.Scan(&f.ID, &f.Subject, &f.Predicate, &f.Object, &f.Confidence, &f.Source, &f.CreatedAt, &f.UpdatedAt, &refutedBy)
		if err != nil {
			return nil, err
		}
		if refutedBy.Valid {
			f.RefutedBy = refutedBy.String
		}
		facts = append(facts, f)
	}

	return facts, nil
}

func (s *SQLiteGraphStore) UpdateConfidence(id string, confidence float64) error {
	_, err := s.db.Exec("UPDATE facts SET confidence = ?, updated_at = ? WHERE id = ?", confidence, time.Now(), id)
	return err
}

func (s *SQLiteGraphStore) RefuteFact(targetID, refutingID string) error {
	_, err := s.db.Exec("UPDATE facts SET refuted_by = ?, updated_at = ? WHERE id = ?", refutingID, time.Now(), targetID)
	return err
}

func (s *SQLiteGraphStore) Close() error {
	return s.db.Close()
}
