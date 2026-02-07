package sync

import (
	"sync"
	"testing"
)

func TestEngine_ConfigRace(t *testing.T) {
	// Setup engine with existing structure
	e := NewEngine(SyncConfig{ID: "race-test"})

	// Create channels to coordinate start for maximum contention
	start := make(chan struct{})
	var wg sync.WaitGroup

	// Writer goroutine
	wg.Add(1)
	go func() {
		defer wg.Done()
		<-start
		for i := 0; i < 1000; i++ {
			e.SetAutoApproveDeletions(i%2 == 0)
			e.SetAlias("Alias")
		}
	}()

	// Reader goroutine
	wg.Add(1)
	go func() {
		defer wg.Done()
		<-start
		for i := 0; i < 1000; i++ {
			_ = e.GetConfig()
			_ = e.GetAlias()
		}
	}()

	close(start)
	wg.Wait()
}
