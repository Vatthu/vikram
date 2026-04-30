package epistemology

import (
	"fmt"
	"os"
	"sync"
	"testing"
)

func TestSQLiteGraphStore_Concurrency(t *testing.T) {
	tmpDir, err := os.MkdirTemp("", "epistemology-test-*")
	if err != nil {
		t.Fatal(err)
	}
	defer os.RemoveAll(tmpDir)

	store, err := NewSQLiteGraphStore(tmpDir)
	if err != nil {
		t.Fatalf("Failed to create store: %v", err)
	}
	defer store.Close()

	var wg sync.WaitGroup
	// Spawn 100 goroutines trying to insert the EXACT same fact simultaneously
	// We want to test the TOCTOU check and see if duplicates slip through.
	for i := 0; i < 100; i++ {
		wg.Add(1)
		go func(idx int) {
			defer wg.Done()
			_, err := store.AssertFact("Sky", "is", "Blue", fmt.Sprintf("source_%d", idx), 0.9)
			if err != nil {
				t.Errorf("AssertFact failed simultaneously: %v", err)
			}
		}(i)
	}
	wg.Wait()

	// Now query to ensure only 1 fact was created despite 100 concurrent asserts
	results, err := store.Query(Query{Subject: "Sky", Predicate: "is", Object: "Blue"})
	if err != nil {
		t.Fatalf("Failed to query: %v", err)
	}

	if len(results) != 1 {
		t.Errorf("Concurrency TOCTOU failure! Expected exactly 1 fact (updated over time), got %d duplicate facts.", len(results))
	}
}

func TestJSONGraphStore_Concurrency(t *testing.T) {
	tmpDir, err := os.MkdirTemp("", "epistemology-test-json-*")
	if err != nil {
		t.Fatal(err)
	}
	defer os.RemoveAll(tmpDir)

	store, err := NewJSONGraphStore(tmpDir)
	if err != nil {
		t.Fatalf("Failed to create store: %v", err)
	}
	defer store.Close()

	var wg sync.WaitGroup
	for i := 0; i < 100; i++ {
		wg.Add(1)
		go func(idx int) {
			defer wg.Done()

			id, err := store.AssertFact("Sky", "is", "Blue", "source", 0.9)
			if err != nil {
				t.Errorf("AssertFact failed: %v", err)
			}

			// Concurrently attempt to update confidence on the newly minted ID or refute it
			store.UpdateConfidence(id, 0.95)
		}(i)
	}
	wg.Wait()

	results, err := store.Query(Query{Subject: "Sky", Predicate: "is", Object: "Blue"})
	if err != nil {
		t.Fatalf("Failed to query: %v", err)
	}

	if len(results) != 1 {
		t.Errorf("Concurrency TOCTOU failure in JSON! Expected exactly 1 fact, got %d", len(results))
	}
}
