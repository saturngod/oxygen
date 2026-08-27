package queue

import (
	"context"
	"encoding/json"
	"testing"
)

func TestJobGenerateThumbnailJSONContract(t *testing.T) {
	tests := []struct {
		name string
		raw  string
		want bool
	}{
		{name: "enabled", raw: `{"id":"media-1","organization_id":"org-1","generate_thumbnail":true}`, want: true},
		{name: "disabled", raw: `{"id":"media-1","organization_id":"org-1","generate_thumbnail":false}`, want: false},
		{name: "legacy payload", raw: `{"id":"media-1","organization_id":"org-1"}`, want: false},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			var job Job
			if err := json.Unmarshal([]byte(tt.raw), &job); err != nil {
				t.Fatal(err)
			}
			if job.GenerateThumbnail != tt.want {
				t.Fatalf("GenerateThumbnail = %t, want %t", job.GenerateThumbnail, tt.want)
			}
			if job.ID == "" || job.OrganizationID == "" {
				t.Fatal("required IDs were not decoded")
			}
		})
	}
}

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
