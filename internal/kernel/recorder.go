package kernel

import (
	"slices"

	"github.com/luigimenezes13/financial-manager/internal/kernel/events"
)

// EventRecorder is the embeddable building block that accumulates the domain
// events of an aggregate root, so each root embeds it instead of repeating the
// slice, Events and ClearEvents. It is the mechanics of keeping the facts the
// root decided to raise, not a business rule.
type EventRecorder struct {
	events []events.Event
}

// RecordEvent appends an event to the aggregate's history. By convention only
// the root's own mutation methods call it: embedding promotes it as public, so
// the invariant "only the root raises events" rests on the internal/ boundary
// and convention, not on the compiler.
func (r *EventRecorder) RecordEvent(event events.Event) {
	r.events = append(r.events, event)
}

// Events returns a copy of the accumulated events, so a caller cannot rewrite
// the root's history through the internal slice. The TransactionRunner reads
// them and writes them to the outbox in the same transaction as the writes.
func (r *EventRecorder) Events() []events.Event {
	return slices.Clone(r.events)
}

// ClearEvents drops the events once the transaction that recorded them in the
// outbox has committed.
func (r *EventRecorder) ClearEvents() {
	r.events = nil
}
