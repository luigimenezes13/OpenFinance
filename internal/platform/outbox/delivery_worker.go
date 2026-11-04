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

// Work cancels a delivery whose subscriber no longer exists instead of retrying
// it: no retry can bring back a subscriber removed from the code.
func (w *DeliveryWorker) Work(ctx context.Context, job *river.Job[DeliveryArgs]) error {
	subscriber, registered := w.subscriptions.subscriberNamed(job.Args.Subscriber)
	if !registered {
		return river.JobCancel(fmt.Errorf("%w: %s", ErrUnknownSubscriber, job.Args.Subscriber))
	}
	return subscriber.Handle(ctx, job.Args.envelope())
}
