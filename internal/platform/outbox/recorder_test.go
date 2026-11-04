package outbox_test

import (
	"context"
	"errors"
	"testing"

	"github.com/luigimenezes13/financial-manager/internal/kernel/events"
	"github.com/luigimenezes13/financial-manager/internal/platform/outbox"
)

var errPersistFailed = errors.New("persist failed")

type inlineUnitOfWork struct{}

func (inlineUnitOfWork) Run(ctx context.Context, work func(ctx context.Context) error) error {
	return work(ctx)
}

type pendingEvents struct {
	recorded []events.Event
}

func (p *pendingEvents) Events() []events.Event {
	return p.recorded
}

func (p *pendingEvents) ClearEvents() {
	p.recorded = nil
}

func newRecorder(t *testing.T) *outbox.Recorder {
	t.Helper()

	mappers, err := outbox.NewMappers(probeMapper())
	if err != nil {
		t.Fatalf("new mappers: %v", err)
	}
	subscriptions := outbox.NewSubscriptions()
	_ = subscriptions.Subscribe(&spySubscriber{name: "audit"}, "probe.happened")
	return outbox.NewRecorder(mappers, subscriptions, nil)
}

func TestRecorder(t *testing.T) {
	t.Run("should refuse to record outside a transaction", func(t *testing.T) {
		err := newRecorder(t).Record(context.Background(), probeHappened{label: "first"})

		if !errors.Is(err, outbox.ErrNoTransaction) {
			t.Fatalf("error = %v, want %v", err, outbox.ErrNoTransaction)
		}
	})
}

func TestAggregateWriter(t *testing.T) {
	t.Run("should keep the pending events when persisting fails", func(t *testing.T) {
		aggregate := &pendingEvents{recorded: []events.Event{probeHappened{label: "first"}}}
		writer := outbox.NewAggregateWriter(inlineUnitOfWork{}, newRecorder(t))

		err := writer.Write(context.Background(), aggregate, func(context.Context) error {
			return errPersistFailed
		})

		if !errors.Is(err, errPersistFailed) {
			t.Fatalf("error = %v, want %v", err, errPersistFailed)
		}
		if len(aggregate.Events()) != 1 {
			t.Errorf("pending events = %d, want 1", len(aggregate.Events()))
		}
	})

	t.Run("should keep the pending events when recording them fails", func(t *testing.T) {
		aggregate := &pendingEvents{recorded: []events.Event{probeHappened{label: "first"}}}
		writer := outbox.NewAggregateWriter(inlineUnitOfWork{}, newRecorder(t))

		err := writer.Write(context.Background(), aggregate, func(context.Context) error {
			return nil
		})

		if !errors.Is(err, outbox.ErrNoTransaction) {
			t.Fatalf("error = %v, want %v", err, outbox.ErrNoTransaction)
		}
		if len(aggregate.Events()) != 1 {
			t.Errorf("pending events = %d, want 1", len(aggregate.Events()))
		}
	})
}
