package queue

import (
	"context"
	"testing"
)

func TestClaimedJobContextOutlivesPollingContext(t *testing.T) {
	pollContext, cancel := context.WithCancel(context.Background())
	jobContext := claimedJobContext(pollContext)

	cancel()

	if pollContext.Err() == nil {
		t.Fatal("expected polling context to be canceled")
	}

	if jobContext.Err() != nil {
		t.Fatalf("expected claimed job context to remain active, got %v", jobContext.Err())
	}
}
