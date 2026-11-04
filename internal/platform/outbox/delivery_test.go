package outbox_test

import (
	"context"
	"encoding/json"
	"errors"
	"testing"

	"github.com/google/uuid"
	"github.com/riverqueue/river"

	"github.com/luigimenezes13/financial-manager/internal/kernel/events"
	"github.com/luigimenezes13/financial-manager/internal/platform/outbox"
)

var errSubscriberFailed = errors.New("subscriber failed")

type spySubscriber struct {
	name      string
	failWith  error
	delivered []events.Envelope
}

func (s *spySubscriber) SubscriberName() string {
	return s.name
}

func (s *spySubscriber) Handle(_ context.Context, envelope events.Envelope) error {
	s.delivered = append(s.delivered, envelope)
	return s.failWith
}

func deliveryJob(subscriber string) *river.Job[outbox.DeliveryArgs] {
	return &river.Job[outbox.DeliveryArgs]{Args: outbox.DeliveryArgs{
		Subscriber:   subscriber,
		EventID:      uuid.MustParse("0193a3b0-0000-7000-8000-0000000000aa"),
		EventName:    "probe.happened",
		EventVersion: 3,
		OccurredAt:   probeOccurredAt,
		Payload:      json.RawMessage(`{"label":"first"}`),
	}}
}

func TestSubscriptions(t *testing.T) {
	t.Run("should refuse two subscribers with the same name", func(t *testing.T) {
		subscriptions := outbox.NewSubscriptions()
		if err := subscriptions.Subscribe(&spySubscriber{name: "audit"}, "probe.happened"); err != nil {
			t.Fatalf("first subscribe: %v", err)
		}

		err := subscriptions.Subscribe(&spySubscriber{name: "audit"}, "probe.vanished")

		if !errors.Is(err, outbox.ErrDuplicateSubscriber) {
			t.Fatalf("error = %v, want %v", err, outbox.ErrDuplicateSubscriber)
		}
	})
}

func TestDeliveryWorker(t *testing.T) {
	t.Run("should hand the envelope to the subscriber the delivery is addressed to", func(t *testing.T) {
		audit := &spySubscriber{name: "audit"}
		billing := &spySubscriber{name: "billing"}
		subscriptions := outbox.NewSubscriptions()
		_ = subscriptions.Subscribe(audit, "probe.happened")
		_ = subscriptions.Subscribe(billing, "probe.happened")

		err := outbox.NewDeliveryWorker(subscriptions).Work(context.Background(), deliveryJob("billing"))

		if err != nil {
			t.Fatalf("work: %v", err)
		}
		if len(audit.delivered) != 0 {
			t.Errorf("audit received %d envelopes, want 0", len(audit.delivered))
		}
		if len(billing.delivered) != 1 {
			t.Fatalf("billing received %d envelopes, want 1", len(billing.delivered))
		}
		envelope := billing.delivered[0]
		if envelope.Name != "probe.happened" || envelope.Version != 3 || string(envelope.Payload) != `{"label":"first"}` {
			t.Errorf("envelope = %+v", envelope)
		}
		if !envelope.OccurredAt.Equal(probeOccurredAt) {
			t.Errorf("occurred at = %v, want %v", envelope.OccurredAt, probeOccurredAt)
		}
	})

	t.Run("should return the subscriber failure so the delivery is retried", func(t *testing.T) {
		subscriptions := outbox.NewSubscriptions()
		_ = subscriptions.Subscribe(&spySubscriber{name: "audit", failWith: errSubscriberFailed}, "probe.happened")

		err := outbox.NewDeliveryWorker(subscriptions).Work(context.Background(), deliveryJob("audit"))

		if !errors.Is(err, errSubscriberFailed) {
			t.Fatalf("error = %v, want %v", err, errSubscriberFailed)
		}
		var cancelled *river.JobCancelError
		if errors.As(err, &cancelled) {
			t.Error("a failing subscriber cancelled the delivery instead of retrying it")
		}
	})

	t.Run("should cancel a delivery addressed to a subscriber that no longer exists", func(t *testing.T) {
		err := outbox.NewDeliveryWorker(outbox.NewSubscriptions()).Work(context.Background(), deliveryJob("removed"))

		var cancelled *river.JobCancelError
		if !errors.As(err, &cancelled) {
			t.Fatalf("error = %v, want a job cancellation", err)
		}
		if !errors.Is(err, outbox.ErrUnknownSubscriber) {
			t.Errorf("error = %v, want %v", err, outbox.ErrUnknownSubscriber)
		}
	})
}
