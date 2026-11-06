package main

import (
	"context"
	"database/sql"
	"errors"
	"log/slog"
	"time"

	"github.com/riverqueue/river"
	"github.com/riverqueue/river/riverdriver/riverdatabasesql"

	financialtrackingeventmapper "github.com/luigimenezes13/financial-manager/internal/financialtracking/adapter/eventmapper"
	identityeventmapper "github.com/luigimenezes13/financial-manager/internal/identity/adapter/eventmapper"
	"github.com/luigimenezes13/financial-manager/internal/kernel"
	"github.com/luigimenezes13/financial-manager/internal/platform/config"
	"github.com/luigimenezes13/financial-manager/internal/platform/outbox"
)

type outboxRuntime struct {
	writer *outbox.AggregateWriter
	queue  *river.Client[*sql.Tx]
}

func newOutbox(database *sql.DB, unitOfWork kernel.UnitOfWork, configuration config.Config, logger *slog.Logger) (outboxRuntime, error) {
	mappers, err := outbox.NewMappers(append(financialtrackingeventmapper.All(), identityeventmapper.All()...)...)
	if err != nil {
		return outboxRuntime{}, err
	}

	subscriptions := outbox.NewSubscriptions()
	if err := subscriptions.Subscribe(outbox.NewDomainEventLog(logger), mappers.EventNames()...); err != nil {
		return outboxRuntime{}, err
	}

	workers := river.NewWorkers()
	river.AddWorker(workers, outbox.NewDeliveryWorker(subscriptions))
	queue, err := river.NewClient(riverdatabasesql.New(database), &river.Config{
		PollOnly:   true,
		Queues:     map[string]river.QueueConfig{river.QueueDefault: {MaxWorkers: configuration.OutboxMaxWorkers}},
		Workers:    workers,
		JobTimeout: configuration.OutboxJobTimeout,
		Logger:     logger.With(slog.String("component", "river")),
	})
	if err != nil {
		return outboxRuntime{}, err
	}

	recorder := outbox.NewRecorder(mappers, subscriptions, queue)
	return outboxRuntime{writer: outbox.NewAggregateWriter(unitOfWork, recorder), queue: queue}, nil
}

// start detaches the queue from ctx on purpose: cancelling the context River
// was started with is a hard stop that cancels running deliveries, while the
// signal should only begin the graceful stop in stop.
func (o outboxRuntime) start(ctx context.Context) error {
	return o.queue.Start(context.WithoutCancel(ctx))
}

// stop lets running deliveries finish until ctx expires, then cancels them
// with the same grace period. A cancelled delivery is not lost: River's rescuer
// makes it available again after the job timeout.
func (o outboxRuntime) stop(ctx context.Context, grace time.Duration) error {
	err := o.queue.Stop(ctx)
	if !errors.Is(err, context.DeadlineExceeded) {
		return err
	}

	hardStopCtx, cancel := context.WithTimeout(context.Background(), grace)
	defer cancel()
	return o.queue.StopAndCancel(hardStopCtx)
}
