package server

import (
	"sync"
	"testing"
	"time"
)

func TestRelayStatsConcurrentSnapshots(t *testing.T) {
	s := &relayStats{}
	var wg sync.WaitGroup
	for i := 0; i < 10; i++ {
		wg.Add(1)
		go func() {
			defer wg.Done()
			for j := 0; j < 100; j++ {
				s.recordVideo(time.Second, j%60 == 0)
				s.recordAudio(2 * time.Second)
				s.snapshot()
			}
		}()
	}
	wg.Wait()
	values := s.snapshot()
	if values[1] != uint64(1000) || values[3] != uint64(1000) || values[5] != time.Second || values[7] != 2*time.Second {
		t.Fatalf("unexpected snapshot: %v", values)
	}
}
