package outbox

import (
	"context"
	"errors"
	"fmt"

	"github.com/riverqueue/river"
)

var ErrUnknownSubscriber = errors.New("outbox: delivery addressed to an unregistered subscriber")

type DeliveryWorker struct {
	river.WorkerDefaults[DeliveryArgs]
	subscriptions *Subscriptions
}

func NewDeliveryWorker(subscriptions *Subscriptions) *DeliveryWorker {
	return &DeliveryWorker{subscriptions: subscriptions}
}

// Work retries a delivery addressed to a subscriber this process does not know
// instead of cancelling it: during a rolling deploy an older pod can fetch a
// delivery for a subscriber only the newer version registers, and a cancelled
// job is never retried. A subscriber truly removed from the code exhausts the
// attempts and ends discarded.
func (w *DeliveryWorker) Work(ctx context.Context, job *river.Job[DeliveryArgs]) error {
	subscriber, registered := w.subscriptions.subscriberNamed(job.Args.Subscriber)
	if !registered {
		return fmt.Errorf("%w: %s", ErrUnknownSubscriber, job.Args.Subscriber)
	}
	return subscriber.Handle(ctx, job.Args.envelope())
}
