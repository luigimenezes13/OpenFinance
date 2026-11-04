//go:build integration

package entrepo_test

import (
	"context"
	"database/sql"
	"testing"

	"github.com/riverqueue/river"
	"github.com/riverqueue/river/rivertype"

	"github.com/luigimenezes13/financial-manager/internal/identity/adapter/entrepo"
	"github.com/luigimenezes13/financial-manager/internal/identity/adapter/eventmapper"
	"github.com/luigimenezes13/financial-manager/internal/kernel/events"
	"github.com/luigimenezes13/financial-manager/internal/platform/db"
	"github.com/luigimenezes13/financial-manager/internal/platform/outbox"
)

const probeDeliveriesTable = "identity_probe_deliveries"

type probeQueue struct{}

func (probeQueue) InsertManyTx(ctx context.Context, tx *sql.Tx, params []river.InsertManyParams) ([]*rivertype.JobInsertResult, error) {
	for _, param := range params {
		delivery := param.Args.(outbox.DeliveryArgs)
		_, err := tx.ExecContext(ctx,
			`INSERT INTO `+probeDeliveriesTable+` (subscriber, event_name) VALUES ($1, $2)`,
			delivery.Subscriber, delivery.EventName)
		if err != nil {
			return nil, err
		}
	}
	return nil, nil
}

type probeSubscriber struct{}

func (probeSubscriber) SubscriberName() string { return "probe" }

func (probeSubscriber) Handle(context.Context, events.Envelope) error { return nil }

func createProbeDeliveries(ctx context.Context, database *sql.DB) error {
	_, err := database.ExecContext(ctx,
		`CREATE TABLE IF NOT EXISTS `+probeDeliveriesTable+` (subscriber text NOT NULL, event_name text NOT NULL)`)
	return err
}

func newTestWriter(t *testing.T) *outbox.AggregateWriter {
	t.Helper()

	mappers, err := outbox.NewMappers(eventmapper.All()...)
	if err != nil {
		t.Fatalf("new mappers: %v", err)
	}
	subscriptions := outbox.NewSubscriptions()
	if err := subscriptions.Subscribe(probeSubscriber{}, mappers.EventNames()...); err != nil {
		t.Fatalf("subscribe probe: %v", err)
	}
	recorder := outbox.NewRecorder(mappers, subscriptions, probeQueue{})
	return outbox.NewAggregateWriter(db.NewUnitOfWork(testDB), recorder)
}

func queuedEventNames(t *testing.T) []string {
	t.Helper()

	rows, err := testDB.QueryContext(context.Background(),
		`SELECT event_name FROM `+probeDeliveriesTable+` ORDER BY event_name`)
	if err != nil {
		t.Fatalf("query probe deliveries: %v", err)
	}
	defer rows.Close()

	names := []string{}
	for rows.Next() {
		var name string
		if err := rows.Scan(&name); err != nil {
			t.Fatalf("scan probe delivery: %v", err)
		}
		names = append(names, name)
	}
	return names
}

func newUserRepository(t *testing.T) *entrepo.UserRepository {
	return entrepo.NewUserRepository(testClient, newTestWriter(t))
}
