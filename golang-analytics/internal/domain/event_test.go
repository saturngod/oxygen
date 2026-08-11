package domain

import (
	"testing"
	"time"

	"github.com/google/uuid"
)

func validEvent() Event {
	return Event{
		EventID:        uuid.New(),
		EventType:      EventViewerSample,
		SchemaVersion:  1,
		Sequence:       2,
		OccurredAt:     time.Now().UTC(),
		OrganizationID: uuid.New(),
		LiveStreamID:   uuid.New(),
		SessionID:      uuid.New(),
	}
}

func TestEventValidateRejectsNegativeCountersAndFutureEvents(t *testing.T) {
	now := time.Now().UTC()
	event := validEvent()
	event.IdentityAdditions = -1
	if err := event.Validate(time.Hour, now); err == nil {
		t.Fatal("expected negative counter error")
	}

	event = validEvent()
	event.OccurredAt = now.Add(6 * time.Minute)
	if err := event.Validate(time.Hour, now); err == nil {
		t.Fatal("expected future event error")
	}
}

func TestEventValidateAllowsTerminalEventOutsideRawRetention(t *testing.T) {
	event := validEvent()
	event.EventType = EventSessionEnded
	event.OccurredAt = time.Now().UTC().Add(-48 * time.Hour)
	if err := event.Validate(24*time.Hour, time.Now().UTC()); err != nil {
		t.Fatalf("terminal events should remain admissible: %v", err)
	}
}
