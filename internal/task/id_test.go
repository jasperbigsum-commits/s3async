package task

import (
	"strings"
	"sync"
	"testing"
	"time"
)

func TestTaskIDsUniqueAtSameInstant(t *testing.T) {
	// Simulate a coarse clock returning the same value to all callers.
	now := time.Unix(1700000000, 0)
	const workers, perWorker = 16, 128
	ids := make(chan string, workers*perWorker)
	var wg sync.WaitGroup
	for i := 0; i < workers; i++ {
		wg.Add(1)
		go func() {
			defer wg.Done()
			for j := 0; j < perWorker; j++ {
				id, err := newTaskID(now)
				if err != nil {
					t.Error(err)
					return
				}
				ids <- id
			}
		}()
	}
	wg.Wait()
	close(ids)
	seen := make(map[string]bool)
	for id := range ids {
		if !strings.HasPrefix(id, "task_1700000000000000000_") {
			t.Errorf("unexpected ID: %s", id)
		}
		if seen[id] {
			t.Fatalf("duplicate ID at same instant: %s", id)
		}
		seen[id] = true
	}
	if len(seen) != workers*perWorker {
		t.Fatalf("got %d IDs", len(seen))
	}
}
