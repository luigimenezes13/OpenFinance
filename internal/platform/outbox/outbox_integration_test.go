//go:build integration

package outbox_test

import (
	"context"
	"database/sql"
	"encoding/json"
	"errors"
	"net/url"
	"os"
	"sync"
	"testing"
	"time"

	"github.com/google/uuid"
	"github.com/riverqueue/river"
	"github.com/riverqueue/river/riverdriver/riverdatabasesql"

	"github.com/luigimenezes13/financial-manager/internal/kernel/events"
	"github.com/luigimenezes13/financial-manager/internal/platform/db"
	"github.com/luigimenezes13/financial-manager/internal/platform/outbox"
)

const (
	outboxSchema       = "outbox_integration"
	riverMigrationPath = "../../../migrations/river/20261104120000_add_river_job_queue.sql"
	deliveryTimeout    = 20 * time.Second
)

type outboxHarness struct {
	database      *sql.DB
	unitOfWork    *db.SQLUnitOfWork
	subscriptions *outbox.Subscriptions
	queue         *river.Client[*sql.Tx]
	recorder      *outbox.Recorder
}

func newOutboxHarness(t *testing.T, subscribers ...events.Subscriber) outboxHarness {
	t.Helper()

	database := connectToOutboxSchema(t)
	mappers, err := outbox.NewMappers(probeMapper())
	if err != nil {
		t.Fatalf("new mappers: %v", err)
	}
	subscriptions := outbox.NewSubscriptions()
	for _, subscriber := range subscribers {
		if err := subscriptions.Subscribe(subscriber, mappers.EventNames()...); err != nil {
			t.Fatalf("subscribe: %v", err)
		}
	}

	workers := river.NewWorkers()
	river.AddWorker(workers, outbox.NewDeliveryWorker(subscriptions))
	queue, err := river.NewClient(riverdatabasesql.New(database), &river.Config{
		PollOnly:          true,
		FetchCooldown:     10 * time.Millisecond,
		FetchPollInterval: 50 * time.Millisecond,
		Queues:            map[string]river.QueueConfig{river.QueueDefault: {MaxWorkers: 2}},
		Workers:           workers,
		TestOnly:          true,
	})
	if err != nil {
		t.Fatalf("new river client: %v", err)
	}

	return outboxHarness{
		database:      database,
		unitOfWork:    db.NewUnitOfWork(database),
		subscriptions: subscriptions,
		queue:         queue,
		recorder:      outbox.NewRecorder(mappers, subscriptions, queue),
	}
}

func connectToOutboxSchema(t *testing.T) *sql.DB {
	t.Helper()

	databaseURL := os.Getenv("TEST_DATABASE_URL")
	if databaseURL == "" {
		t.Fatal("TEST_DATABASE_URL is empty; run make test-integration")
	}
	ctx := context.Background()

	admin, err := db.NewSQLDB(ctx, databaseURL)
	if err != nil {
		t.Fatalf("connect: %v", err)
	}
	defer admin.Close()
	if _, err := admin.ExecContext(ctx, "DROP SCHEMA IF EXISTS "+outboxSchema+" CASCADE; CREATE SCHEMA "+outboxSchema); err != nil {
		t.Fatalf("recreate schema: %v", err)
	}

	database, err := db.NewSQLDB(ctx, withSearchPath(t, databaseURL))
	if err != nil {
		t.Fatalf("connect to schema: %v", err)
	}
	t.Cleanup(func() { _ = database.Close() })

	migration, err := os.ReadFile(riverMigrationPath)
	if err != nil {
		t.Fatalf("read river migration: %v", err)
	}
	if _, err := database.ExecContext(ctx, string(migration)); err != nil {
		t.Fatalf("apply river migration: %v", err)
	}
	if _, err := database.ExecContext(ctx, "CREATE TABLE ledger (label text NOT NULL)"); err != nil {
		t.Fatalf("create ledger: %v", err)
	}
	return database
}

func withSearchPath(t *testing.T, databaseURL string) string {
	t.Helper()

	parsed, err := url.Parse(databaseURL)
	if err != nil {
		t.Fatalf("parse database url: %v", err)
	}
	query := parsed.Query()
	query.Set("search_path", outboxSchema)
	parsed.RawQuery = query.Encode()
	return parsed.String()
}

func (h outboxHarness) writeLedgerAndRecord(ctx context.Context, label string, afterRecord error) error {
	return h.unitOfWork.Run(ctx, func(ctx context.Context) error {
		transaction, _ := db.TransactionFrom(ctx)
		if _, err := transaction.ExecContext(ctx, "INSERT INTO ledger (label) VALUES ($1)", label); err != nil {
			return err
		}
		if err := h.recorder.Record(ctx, probeHappened{label: label}); err != nil {
			return err
		}
		return afterRecord
	})
}

func (h outboxHarness) count(t *testing.T, query string) int {
	t.Helper()

	var total int
	if err := h.database.QueryRowContext(context.Background(), query).Scan(&total); err != nil {
		t.Fatalf("count: %v", err)
	}
	return total
}

func (h outboxHarness) start(t *testing.T) {
	t.Helper()

	ctx := context.Background()
	if err := h.queue.Start(ctx); err != nil {
		t.Fatalf("start river: %v", err)
	}
	t.Cleanup(func() {
		stopCtx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
		defer cancel()
		_ = h.queue.Stop(stopCtx)
	})
}

type countingSubscriber struct {
	name      string
	failFirst bool

	mutex     sync.Mutex
	delivered []events.Envelope
	arrived   chan struct{}
}

func newCountingSubscriber(name string, failFirst bool) *countingSubscriber {
	return &countingSubscriber{name: name, failFirst: failFirst, arrived: make(chan struct{}, 10)}
}

func (s *countingSubscriber) SubscriberName() string {
	return s.name
}

func (s *countingSubscriber) Handle(_ context.Context, envelope events.Envelope) error {
	s.mutex.Lock()
	s.delivered = append(s.delivered, envelope)
	attempt := len(s.delivered)
	s.mutex.Unlock()
	s.arrived <- struct{}{}

	if s.failFirst && attempt == 1 {
		return errSubscriberFailed
	}
	return nil
}

func (s *countingSubscriber) deliveries() []events.Envelope {
	s.mutex.Lock()
	defer s.mutex.Unlock()
	return append([]events.Envelope(nil), s.delivered...)
}

func (s *countingSubscriber) awaitDeliveries(t *testing.T, want int) {
	t.Helper()

	deadline := time.After(deliveryTimeout)
	for received := 0; received < want; received++ {
		select {
		case <-s.arrived:
		case <-deadline:
			t.Fatalf("%s received %d deliveries, want %d", s.name, received, want)
		}
	}
}

func TestOutboxIntegration(t *testing.T) {
	t.Run("should queue one delivery per subscriber in the caller's transaction", func(t *testing.T) {
		harness := newOutboxHarness(t, newCountingSubscriber("audit", false), newCountingSubscriber("billing", false))

		if err := harness.writeLedgerAndRecord(context.Background(), "first", nil); err != nil {
			t.Fatalf("write: %v", err)
		}

		if got := harness.count(t, "SELECT count(*) FROM ledger"); got != 1 {
			t.Errorf("ledger rows = %d, want 1", got)
		}
		queued := harness.count(t, `SELECT count(*) FROM river_job
			WHERE kind = 'outbox_delivery' AND args->>'event_name' = 'probe.happened'
			AND args->>'subscriber' IN ('audit', 'billing')`)
		if queued != 2 {
			t.Errorf("queued deliveries = %d, want 2", queued)
		}
	})

	t.Run("should queue nothing when the transaction rolls back", func(t *testing.T) {
		harness := newOutboxHarness(t, newCountingSubscriber("audit", false))

		err := harness.writeLedgerAndRecord(context.Background(), "first", errPersistFailed)

		if !errors.Is(err, errPersistFailed) {
			t.Fatalf("error = %v, want %v", err, errPersistFailed)
		}
		if got := harness.count(t, "SELECT count(*) FROM ledger"); got != 0 {
			t.Errorf("ledger rows = %d, want 0", got)
		}
		if got := harness.count(t, "SELECT count(*) FROM river_job"); got != 0 {
			t.Errorf("queued deliveries = %d, want 0", got)
		}
	})

	t.Run("should queue an event without subscribers as no delivery at all", func(t *testing.T) {
		harness := newOutboxHarness(t)

		if err := harness.writeLedgerAndRecord(context.Background(), "first", nil); err != nil {
			t.Fatalf("write: %v", err)
		}

		if got := harness.count(t, "SELECT count(*) FROM river_job"); got != 0 {
			t.Errorf("queued deliveries = %d, want 0", got)
		}
	})

	t.Run("should queue the same delivery for the same subscriber only once", func(t *testing.T) {
		harness := newOutboxHarness(t)
		delivery := outbox.DeliveryArgs{
			Subscriber: "audit",
			EventID:    uuid.MustParse("0193a3b0-0000-7000-8000-0000000000bb"),
			EventName:  "probe.happened",
			Payload:    []byte(`{"label":"first"}`),
		}

		for range 2 {
			if _, err := harness.queue.InsertMany(context.Background(), []river.InsertManyParams{{Args: delivery}}); err != nil {
				t.Fatalf("insert: %v", err)
			}
		}

		if got := harness.count(t, "SELECT count(*) FROM river_job"); got != 1 {
			t.Errorf("queued deliveries = %d, want 1", got)
		}
	})

	t.Run("should deliver a committed event to its subscriber", func(t *testing.T) {
		audit := newCountingSubscriber("audit", false)
		harness := newOutboxHarness(t, audit)
		harness.start(t)

		if err := harness.writeLedgerAndRecord(context.Background(), "first", nil); err != nil {
			t.Fatalf("write: %v", err)
		}
		audit.awaitDeliveries(t, 1)

		envelope := audit.deliveries()[0]
		if envelope.Name != "probe.happened" || envelope.Version != 3 {
			t.Errorf("name/version = %q/%d", envelope.Name, envelope.Version)
		}
		var payload probePayload
		if err := json.Unmarshal(envelope.Payload, &payload); err != nil || payload.Label != "first" {
			t.Errorf("payload = %s (%v)", envelope.Payload, err)
		}
		if envelope.ID == uuid.Nil {
			t.Error("envelope arrived without an id")
		}
	})

	t.Run("should retry a failing subscriber without redelivering to the one that succeeded", func(t *testing.T) {
		flaky := newCountingSubscriber("flaky", true)
		steady := newCountingSubscriber("steady", false)
		harness := newOutboxHarness(t, flaky, steady)
		harness.start(t)

		if err := harness.writeLedgerAndRecord(context.Background(), "first", nil); err != nil {
			t.Fatalf("write: %v", err)
		}
		flaky.awaitDeliveries(t, 2)
		steady.awaitDeliveries(t, 1)

		flakyDeliveries := flaky.deliveries()
		if flakyDeliveries[0].ID != flakyDeliveries[1].ID {
			t.Errorf("retry carried another envelope id: %s then %s", flakyDeliveries[0].ID, flakyDeliveries[1].ID)
		}
		if got := len(steady.deliveries()); got != 1 {
			t.Errorf("steady received %d deliveries, want 1", got)
		}
	})
}
