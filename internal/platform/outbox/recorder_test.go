package outbox_test

import (
	"context"
	"errors"
	"testing"

	"github.com/luigimenezes13/financial-manager/internal/platform/outbox"
)

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
