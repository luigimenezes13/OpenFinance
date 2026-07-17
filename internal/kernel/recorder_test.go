package kernel_test

import (
	"testing"
	"time"

	"github.com/luigimenezes13/financial-manager/internal/kernel"
	"github.com/luigimenezes13/financial-manager/internal/kernel/events"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

// stubEvent satisfaz events.Event pra exercitar o EventRecorder sem depender
// de um evento concreto de nenhum bounded context.
type stubEvent struct {
	name string
}

func (e stubEvent) EventName() string     { return e.name }
func (e stubEvent) OccurredAt() time.Time { return time.Now() }

// TestEventRecorderZeroValueIsUsable — nada precisa inicializar o recorder;
// o zero value já grava e lê sem panic.
func TestEventRecorderZeroValueIsUsable(t *testing.T) {
	t.Parallel()

	var recorder kernel.EventRecorder

	assert.Empty(t, recorder.Events(), "recorder recém-criado não tem eventos")

	recorder.RecordEvent(stubEvent{name: "a"})

	require.Len(t, recorder.Events(), 1)
}

// TestEventRecorderRecordsInOrder — os eventos saem na ordem em que entraram.
func TestEventRecorderRecordsInOrder(t *testing.T) {
	t.Parallel()

	var recorder kernel.EventRecorder
	recorder.RecordEvent(stubEvent{name: "first"})
	recorder.RecordEvent(stubEvent{name: "second"})

	got := recorder.Events()

	require.Len(t, got, 2)
	assert.Equal(t, "first", got[0].EventName())
	assert.Equal(t, "second", got[1].EventName())
}

// TestEventRecorderEventsReturnsCopy — mutar o slice retornado não afeta o
// histórico interno (contrato do slices.Clone).
func TestEventRecorderEventsReturnsCopy(t *testing.T) {
	t.Parallel()

	var recorder kernel.EventRecorder
	recorder.RecordEvent(stubEvent{name: "original"})

	leaked := recorder.Events()
	leaked[0] = nil

	fresh := recorder.Events()
	require.Len(t, fresh, 1)
	assert.NotNil(t, fresh[0], "Events() deve devolver cópia, não o slice interno")
}

// TestEventRecorderClearEvents — descarta o que já foi despachado.
func TestEventRecorderClearEvents(t *testing.T) {
	t.Parallel()

	var recorder kernel.EventRecorder
	recorder.RecordEvent(stubEvent{name: "a"})
	require.Len(t, recorder.Events(), 1)

	recorder.ClearEvents()

	assert.Empty(t, recorder.Events())
}

// stubEvent é usada como events.Event — trava em compile-time se o contrato
// mudar.
var _ events.Event = stubEvent{}
