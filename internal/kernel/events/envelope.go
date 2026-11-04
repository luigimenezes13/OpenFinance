package events

import (
	"encoding/json"
	"time"

	"github.com/google/uuid"
)

// Envelope is the public contract a subscriber receives, decoupled from the
// producer's domain types so another bounded context never imports them.
//
// ID identifies the fact, not the delivery: redeliveries carry the same ID, and
// subscribers deduplicate on it. Version changes only when Payload changes shape.
type Envelope struct {
	ID         uuid.UUID
	Name       string
	Version    int
	OccurredAt time.Time
	Payload    json.RawMessage
}
