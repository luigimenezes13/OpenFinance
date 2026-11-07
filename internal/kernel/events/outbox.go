package events

import "context"

// Outbox stores events in the same transaction as the writes that produced
// them, so they are delivered if and only if those writes commit. Record fails
// when ctx carries no open transaction.
type Outbox interface {
	Record(ctx context.Context, recorded ...Event) error
}
