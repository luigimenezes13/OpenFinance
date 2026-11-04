package outbox_test

import (
	"errors"
	"testing"
	"time"

	"github.com/luigimenezes13/financial-manager/internal/platform/outbox"
)

var probeOccurredAt = time.Date(2026, 11, 4, 12, 0, 0, 0, time.UTC)

type probeHappened struct{ label string }

func (probeHappened) EventName() string     { return "probe.happened" }
func (probeHappened) OccurredAt() time.Time { return probeOccurredAt }

type probeVanished struct{}

func (probeVanished) EventName() string     { return "probe.vanished" }
func (probeVanished) OccurredAt() time.Time { return probeOccurredAt }

type probeImpostor struct{}

func (probeImpostor) EventName() string     { return "probe.happened" }
func (probeImpostor) OccurredAt() time.Time { return probeOccurredAt }

type probePayload struct {
	Label string `json:"label"`
}

func probeMapper() outbox.EventMapper {
	return outbox.MapperFor("probe.happened", 3, func(event probeHappened) probePayload {
		return probePayload{Label: event.label}
	})
}

func TestMappers(t *testing.T) {
	t.Run("should seal an event into an envelope carrying its name, version, time and payload", func(t *testing.T) {
		mappers, err := outbox.NewMappers(probeMapper())
		if err != nil {
			t.Fatalf("new mappers: %v", err)
		}

		envelope, err := mappers.Seal(probeHappened{label: "first"})
		if err != nil {
			t.Fatalf("seal: %v", err)
		}

		if envelope.Name != "probe.happened" {
			t.Errorf("name = %q", envelope.Name)
		}
		if envelope.Version != 3 {
			t.Errorf("version = %d, want 3", envelope.Version)
		}
		if !envelope.OccurredAt.Equal(probeOccurredAt) {
			t.Errorf("occurred at = %v, want %v", envelope.OccurredAt, probeOccurredAt)
		}
		if string(envelope.Payload) != `{"label":"first"}` {
			t.Errorf("payload = %s", envelope.Payload)
		}
	})

	t.Run("should give every sealed envelope its own time-ordered id", func(t *testing.T) {
		mappers, _ := outbox.NewMappers(probeMapper())

		first, _ := mappers.Seal(probeHappened{label: "first"})
		second, _ := mappers.Seal(probeHappened{label: "second"})

		if first.ID == second.ID {
			t.Fatal("two envelopes share an id")
		}
		if first.ID.Version() != 7 {
			t.Errorf("id version = %d, want 7", first.ID.Version())
		}
		if first.ID.String() >= second.ID.String() {
			t.Errorf("ids are not time ordered: %s then %s", first.ID, second.ID)
		}
	})

	t.Run("should refuse an event nobody mapped", func(t *testing.T) {
		mappers, _ := outbox.NewMappers(probeMapper())

		_, err := mappers.Seal(probeVanished{})

		if !errors.Is(err, outbox.ErrUnmappedEvent) {
			t.Fatalf("error = %v, want %v", err, outbox.ErrUnmappedEvent)
		}
	})

	t.Run("should refuse an event whose type differs from the one the mapper was built for", func(t *testing.T) {
		mappers, _ := outbox.NewMappers(probeMapper())

		_, err := mappers.Seal(probeImpostor{})

		if !errors.Is(err, outbox.ErrUnexpectedEventType) {
			t.Fatalf("error = %v, want %v", err, outbox.ErrUnexpectedEventType)
		}
	})

	t.Run("should refuse two mappers for the same event", func(t *testing.T) {
		_, err := outbox.NewMappers(probeMapper(), probeMapper())

		if !errors.Is(err, outbox.ErrDuplicateMapper) {
			t.Fatalf("error = %v, want %v", err, outbox.ErrDuplicateMapper)
		}
	})
}
