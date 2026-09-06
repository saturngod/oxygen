package server

import (
	"sync"
	"testing"
)

func TestHLSWriteErrorPolicyToleratesIsolatedFailures(t *testing.T) {
	policy := &hlsWriteErrorPolicy{}

	for i := int32(1); i < maxConsecutiveHLSWriteErrors; i++ {
		if n := policy.noteFailure(); n != i {
			t.Fatalf("expected run length %d, got %d", i, n)
		}
	}

	policy.noteSuccess()
	if n := policy.noteFailure(); n != 1 {
		t.Fatalf("expected run length reset to 1 after success, got %d", n)
	}
}

func TestHLSWriteErrorPolicyReachesThreshold(t *testing.T) {
	policy := &hlsWriteErrorPolicy{}

	var n int32
	for i := 0; i < maxConsecutiveHLSWriteErrors; i++ {
		n = policy.noteFailure()
	}

	if n != maxConsecutiveHLSWriteErrors {
		t.Fatalf("expected run length %d, got %d", maxConsecutiveHLSWriteErrors, n)
	}
}

func TestHLSWriteErrorPolicyConcurrentUse(t *testing.T) {
	policy := &hlsWriteErrorPolicy{}

	var wg sync.WaitGroup
	for i := 0; i < 10; i++ {
		wg.Add(1)
		go func() {
			defer wg.Done()
			for j := 0; j < 100; j++ {
				policy.noteFailure()
				policy.noteSuccess()
			}
		}()
	}
	wg.Wait()
}
