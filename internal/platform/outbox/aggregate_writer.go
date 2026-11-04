package outbox

import (
	"context"

	"github.com/luigimenezes13/financial-manager/internal/kernel"
	"github.com/luigimenezes13/financial-manager/internal/kernel/events"
)

type EventSource interface {
	Events() []events.Event
	ClearEvents()
}

type AggregateWriter struct {
	unitOfWork kernel.UnitOfWork
	recorder   *Recorder
}

func NewAggregateWriter(unitOfWork kernel.UnitOfWork, recorder *Recorder) *AggregateWriter {
	return &AggregateWriter{unitOfWork: unitOfWork, recorder: recorder}
}

// Write runs persist and records the aggregate's pending events in the same
// transaction, joining the caller's unit of work when there is one. Events are
// cleared only after the write succeeds, so a failed write can be retried with
// its events intact.
func (w *AggregateWriter) Write(ctx context.Context, aggregate EventSource, persist func(ctx context.Context) error) error {
	err := w.unitOfWork.Run(ctx, func(ctx context.Context) error {
		if err := persist(ctx); err != nil {
			return err
		}
		return w.recorder.Record(ctx, aggregate.Events()...)
	})
	if err != nil {
		return err
	}

	aggregate.ClearEvents()
	return nil
}
