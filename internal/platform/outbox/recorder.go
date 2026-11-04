package outbox

import (
	"context"
	"database/sql"
	"errors"
	"fmt"

	"github.com/riverqueue/river"
	"github.com/riverqueue/river/rivertype"

	"github.com/luigimenezes13/financial-manager/internal/kernel/events"
	"github.com/luigimenezes13/financial-manager/internal/platform/db"
)

var ErrNoTransaction = errors.New("outbox: recording events requires a transaction in the context")

type JobInserter interface {
	InsertManyTx(ctx context.Context, tx *sql.Tx, params []river.InsertManyParams) ([]*rivertype.JobInsertResult, error)
}

type Recorder struct {
	mappers       Mappers
	subscriptions *Subscriptions
	inserter      JobInserter
}

func NewRecorder(mappers Mappers, subscriptions *Subscriptions, inserter JobInserter) *Recorder {
	return &Recorder{mappers: mappers, subscriptions: subscriptions, inserter: inserter}
}

// Record queues one delivery per subscriber of each event, inside the
// transaction carried by ctx. Outside a transaction it returns ErrNoTransaction:
// an event committed apart from its aggregate is exactly what the outbox exists
// to prevent.
func (r *Recorder) Record(ctx context.Context, recorded ...events.Event) error {
	transaction, inTransaction := db.TransactionFrom(ctx)
	if !inTransaction {
		return ErrNoTransaction
	}

	deliveries, err := r.deliveriesOf(recorded)
	if err != nil {
		return err
	}
	if len(deliveries) == 0 {
		return nil
	}

	if _, err := r.inserter.InsertManyTx(ctx, transaction, deliveries); err != nil {
		return fmt.Errorf("outbox: queue deliveries: %w", err)
	}
	return nil
}

func (r *Recorder) deliveriesOf(recorded []events.Event) ([]river.InsertManyParams, error) {
	deliveries := []river.InsertManyParams{}
	for _, event := range recorded {
		envelope, err := r.mappers.Seal(event)
		if err != nil {
			return nil, err
		}
		deliveries = append(deliveries, r.deliveriesFor(envelope)...)
	}
	return deliveries, nil
}

func (r *Recorder) deliveriesFor(envelope events.Envelope) []river.InsertManyParams {
	subscribers := r.subscriptions.subscribersOf(envelope.Name)
	deliveries := make([]river.InsertManyParams, 0, len(subscribers))
	for _, subscriber := range subscribers {
		deliveries = append(deliveries, river.InsertManyParams{
			Args: newDeliveryArgs(subscriber.SubscriberName(), envelope),
		})
	}
	return deliveries
}
