package kernel

import (
	"context"
	"errors"
	"slices"

	"github.com/luigimenezes13/financial-manager/internal/kernel/events"
)

var ErrTransactionAlreadyStarted = errors.New("kernel: a transaction runner is already running in this context")

type EventSource interface {
	Events() []events.Event
	ClearEvents()
}

type TransactionContext interface {
	RecordEventsOf(aggregate EventSource)
}

type runningTransactionKey struct{}

type TransactionRunner struct {
	unitOfWork UnitOfWork
	outbox     events.Outbox
}

func NewTransactionRunner(unitOfWork UnitOfWork, outbox events.Outbox) *TransactionRunner {
	return &TransactionRunner{unitOfWork: unitOfWork, outbox: outbox}
}

// Begin runs work as one transaction owned by the calling use case.
//
// The events of every aggregate handed to RecordEventsOf are read when work
// returns, so events raised after the call are included, and go to the outbox
// before the commit: they are delivered if and only if the writes commit. They
// are cleared from the aggregates only after the commit, so a failed run leaves
// them in place for a retry.
//
// Begin refuses to run inside another Begin with ErrTransactionAlreadyStarted:
// the inner run would clear its aggregates' events before the outer commit.
func (r *TransactionRunner) Begin(ctx context.Context, work func(ctx context.Context, transaction TransactionContext) error) error {
	if ctx.Value(runningTransactionKey{}) != nil {
		return ErrTransactionAlreadyStarted
	}

	pending := &pendingAggregates{}
	err := r.unitOfWork.Run(context.WithValue(ctx, runningTransactionKey{}, pending), func(ctx context.Context) error {
		if err := work(ctx, pending); err != nil {
			return err
		}
		return r.outbox.Record(ctx, pending.events()...)
	})
	if err != nil {
		return err
	}

	pending.clearEvents()
	return nil
}

type pendingAggregates struct {
	aggregates []EventSource
}

func (p *pendingAggregates) RecordEventsOf(aggregate EventSource) {
	if slices.Contains(p.aggregates, aggregate) {
		return
	}
	p.aggregates = append(p.aggregates, aggregate)
}

func (p *pendingAggregates) events() []events.Event {
	recorded := []events.Event{}
	for _, aggregate := range p.aggregates {
		recorded = append(recorded, aggregate.Events()...)
	}
	return recorded
}

func (p *pendingAggregates) clearEvents() {
	for _, aggregate := range p.aggregates {
		aggregate.ClearEvents()
	}
}
