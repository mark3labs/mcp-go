package server

import (
	"sync"
	"testing"

	"github.com/stretchr/testify/assert"
)

// In-process session IDs are unique however fast they are generated: the
// wall clock alone is not, as it moves in microseconds on some platforms.
func TestGenerateInProcessSessionIDIsUnique(t *testing.T) {
	const workers, each = 4, 2500
	ids := make(chan string, workers*each)
	var wg sync.WaitGroup
	for range workers {
		wg.Go(func() {
			for range each {
				ids <- GenerateInProcessSessionID()
			}
		})
	}
	wg.Wait()
	close(ids)
	seen := make(map[string]bool, workers*each)
	for id := range ids {
		assert.False(t, seen[id], "session ID %q was generated twice", id)
		seen[id] = true
	}
}
