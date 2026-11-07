package kernel_test

import (
	"context"
	"errors"
	"testing"
	"time"

	"github.com/luigimenezes13/financial-manager/internal/kernel"
	"github.com/luigimenezes13/financial-manager/internal/kernel/events"
)

var (
	errWorkFailed   = errors.New("work failed")
	errOutboxFailed = errors.New("outbox failed")
	errCommitFailed = errors.New("commit failed")
)

type happened struct{ label string }

func (happened) EventName() string     { return "probe.happened" }
func (happened) OccurredAt() time.Time { return time.Date(2026, 11, 6, 12, 0, 0, 0, time.UTC) }

type probeAggregate struct {
	kernel.EventRecorder
}

func newProbeAggregate(labels ...string) *probeAggregate {
	aggregate := &probeAggregate{}
	for _, label := range labels {
		aggregate.RecordEvent(happened{label: label})
	}
	return aggregate
}

type fakeUnitOfWork struct {
	commitErr   error
	alreadyOpen bool
	runs        int
}

func (f *fakeUnitOfWork) InTransaction(context.Context) bool {
	return f.alreadyOpen
}

func (f *fakeUnitOfWork) Run(ctx context.Context, work func(ctx context.Context) error) error {
	f.runs++
	if err := work(ctx); err != nil {
		return err
	}
	return f.commitErr
}

type fakeOutbox struct {
	failWith error
	recorded []events.Event
	calls    int
}

func (f *fakeOutbox) Record(_ context.Context, recorded ...events.Event) error {
	f.calls++
	if f.failWith != nil {
		return f.failWith
	}
	f.recorded = append(f.recorded, recorded...)
	return nil
}

func labelsOf(recorded []events.Event) []string {
	labels := make([]string, 0, len(recorded))
	for _, event := range recorded {
		labels = append(labels, event.(happened).label)
	}
	return labels
}

func assertLabels(t *testing.T, got []events.Event, want ...string) {
	t.Helper()

	labels := labelsOf(got)
	if len(labels) != len(want) {
		t.Fatalf("events = %v, want %v", labels, want)
	}
	for index := range want {
		if labels[index] != want[index] {
			t.Fatalf("events = %v, want %v", labels, want)
		}
	}
}

func TestTransactionRunner(t *testing.T) {
	t.Run("should record the events of every aggregate in the unit and clear them after the commit", func(t *testing.T) {
		unitOfWork, outbox := &fakeUnitOfWork{}, &fakeOutbox{}
		first, second := newProbeAggregate("first"), newProbeAggregate("second", "third")

		err := kernel.NewTransactionRunner(unitOfWork, outbox).Begin(context.Background(), func(_ context.Context, transaction kernel.TransactionContext) error {
			transaction.RecordEventsOf(first)
			transaction.RecordEventsOf(second)
			return nil
		})

		if err != nil {
			t.Fatalf("begin: %v", err)
		}
		if unitOfWork.runs != 1 {
			t.Errorf("unit runs = %d, want 1", unitOfWork.runs)
		}
		assertLabels(t, outbox.recorded, "first", "second", "third")
		if len(first.Events()) != 0 || len(second.Events()) != 0 {
			t.Error("events survived a successful commit")
		}
	})

	t.Run("should include events raised after the aggregate was handed to the transaction", func(t *testing.T) {
		outbox := &fakeOutbox{}
		aggregate := newProbeAggregate()

		_ = kernel.NewTransactionRunner(&fakeUnitOfWork{}, outbox).Begin(context.Background(), func(_ context.Context, transaction kernel.TransactionContext) error {
			transaction.RecordEventsOf(aggregate)
			aggregate.RecordEvent(happened{label: "late"})
			return nil
		})

		assertLabels(t, outbox.recorded, "late")
	})

	t.Run("should record an aggregate handed twice only once", func(t *testing.T) {
		outbox := &fakeOutbox{}
		aggregate := newProbeAggregate("once")

		_ = kernel.NewTransactionRunner(&fakeUnitOfWork{}, outbox).Begin(context.Background(), func(_ context.Context, transaction kernel.TransactionContext) error {
			transaction.RecordEventsOf(aggregate)
			transaction.RecordEventsOf(aggregate)
			return nil
		})

		assertLabels(t, outbox.recorded, "once")
	})

	t.Run("should keep the events and skip the outbox when the work fails", func(t *testing.T) {
		outbox := &fakeOutbox{}
		aggregate := newProbeAggregate("kept")

		err := kernel.NewTransactionRunner(&fakeUnitOfWork{}, outbox).Begin(context.Background(), func(_ context.Context, transaction kernel.TransactionContext) error {
			transaction.RecordEventsOf(aggregate)
			return errWorkFailed
		})

		if !errors.Is(err, errWorkFailed) {
			t.Fatalf("error = %v, want %v", err, errWorkFailed)
		}
		if outbox.calls != 0 {
			t.Errorf("outbox calls = %d, want 0", outbox.calls)
		}
		assertLabels(t, aggregate.Events(), "kept")
	})

	t.Run("should keep the events when the outbox refuses them", func(t *testing.T) {
		aggregate := newProbeAggregate("kept")

		err := kernel.NewTransactionRunner(&fakeUnitOfWork{}, &fakeOutbox{failWith: errOutboxFailed}).Begin(context.Background(), func(_ context.Context, transaction kernel.TransactionContext) error {
			transaction.RecordEventsOf(aggregate)
			return nil
		})

		if !errors.Is(err, errOutboxFailed) {
			t.Fatalf("error = %v, want %v", err, errOutboxFailed)
		}
		assertLabels(t, aggregate.Events(), "kept")
	})

	t.Run("should keep the events when the commit fails after the outbox recorded them", func(t *testing.T) {
		aggregate := newProbeAggregate("kept")

		err := kernel.NewTransactionRunner(&fakeUnitOfWork{commitErr: errCommitFailed}, &fakeOutbox{}).Begin(context.Background(), func(_ context.Context, transaction kernel.TransactionContext) error {
			transaction.RecordEventsOf(aggregate)
			return nil
		})

		if !errors.Is(err, errCommitFailed) {
			t.Fatalf("error = %v, want %v", err, errCommitFailed)
		}
		assertLabels(t, aggregate.Events(), "kept")
	})

	t.Run("should refuse to begin inside another transaction runner", func(t *testing.T) {
		runner := kernel.NewTransactionRunner(&fakeUnitOfWork{}, &fakeOutbox{})
		aggregate := newProbeAggregate("outer")

		var nestedErr error
		err := runner.Begin(context.Background(), func(ctx context.Context, transaction kernel.TransactionContext) error {
			transaction.RecordEventsOf(aggregate)
			nestedErr = runner.Begin(ctx, func(context.Context, kernel.TransactionContext) error { return nil })
			return nestedErr
		})

		if !errors.Is(nestedErr, kernel.ErrTransactionAlreadyStarted) {
			t.Fatalf("nested error = %v, want %v", nestedErr, kernel.ErrTransactionAlreadyStarted)
		}
		if !errors.Is(err, kernel.ErrTransactionAlreadyStarted) {
			t.Errorf("outer error = %v, want the nested refusal", err)
		}
		assertLabels(t, aggregate.Events(), "outer")
	})

	t.Run("should refuse to begin inside a unit of work that is already open", func(t *testing.T) {
		unitOfWork, outbox := &fakeUnitOfWork{alreadyOpen: true}, &fakeOutbox{}
		aggregate := newProbeAggregate("kept")

		err := kernel.NewTransactionRunner(unitOfWork, outbox).Begin(context.Background(), func(_ context.Context, transaction kernel.TransactionContext) error {
			transaction.RecordEventsOf(aggregate)
			return nil
		})

		if !errors.Is(err, kernel.ErrTransactionAlreadyStarted) {
			t.Fatalf("error = %v, want %v", err, kernel.ErrTransactionAlreadyStarted)
		}
		if unitOfWork.runs != 0 || outbox.calls != 0 {
			t.Errorf("unit runs = %d, outbox calls = %d, want neither touched", unitOfWork.runs, outbox.calls)
		}
		assertLabels(t, aggregate.Events(), "kept")
	})
}
