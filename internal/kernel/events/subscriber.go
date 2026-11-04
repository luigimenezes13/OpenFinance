package events

import "context"

// Subscriber reacts to envelopes delivered by the outbox.
//
// Delivery is at least once: Handle may receive the same envelope ID again after
// a crash or a retry, so it must deduplicate on it. SubscriberName is stored with
// every pending delivery; renaming a subscriber orphans the deliveries queued
// under the old name.
type Subscriber interface {
	SubscriberName() string
	Handle(ctx context.Context, envelope Envelope) error
}
