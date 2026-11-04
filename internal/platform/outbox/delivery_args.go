package outbox

import (
	"encoding/json"
	"time"

	"github.com/google/uuid"
	"github.com/riverqueue/river"

	"github.com/luigimenezes13/financial-manager/internal/kernel/events"
)

// DeliveryArgs is one envelope addressed to one subscriber. Uniqueness covers
// only the subscriber and the envelope ID, so the same fact is never queued
// twice for the same subscriber. Args are stored as jsonb, so the delivered
// Payload keeps its content but not its key order or whitespace.
type DeliveryArgs struct {
	Subscriber   string          `json:"subscriber" river:"unique"`
	EventID      uuid.UUID       `json:"event_id" river:"unique"`
	EventName    string          `json:"event_name"`
	EventVersion int             `json:"event_version"`
	OccurredAt   time.Time       `json:"occurred_at"`
	Payload      json.RawMessage `json:"payload"`
}

func newDeliveryArgs(subscriber string, envelope events.Envelope) DeliveryArgs {
	return DeliveryArgs{
		Subscriber:   subscriber,
		EventID:      envelope.ID,
		EventName:    envelope.Name,
		EventVersion: envelope.Version,
		OccurredAt:   envelope.OccurredAt,
		Payload:      envelope.Payload,
	}
}

func (DeliveryArgs) Kind() string {
	return "outbox_delivery"
}

func (DeliveryArgs) InsertOpts() river.InsertOpts {
	return river.InsertOpts{UniqueOpts: river.UniqueOpts{ByArgs: true}}
}

func (a DeliveryArgs) envelope() events.Envelope {
	return events.Envelope{
		ID:         a.EventID,
		Name:       a.EventName,
		Version:    a.EventVersion,
		OccurredAt: a.OccurredAt,
		Payload:    a.Payload,
	}
}
