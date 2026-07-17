package kernel

import (
	"slices"

	"github.com/luigimenezes13/financial-manager/internal/kernel/events"
)

// EventRecorder é o building block embedável que centraliza o acúmulo de
// eventos de domínio de um aggregate root. Elimina a repetição de
// events []events.Event + Events() + ClearEvents() em cada root: o root só
// embeda o EventRecorder e ganha os três métodos.
//
// DDD: infraestrutura do Aggregate Root — não é regra de negócio, é a
// mecânica de guardar os fatos que o root decidiu emitir.
type EventRecorder struct {
	events []events.Event
}

// RecordEvent anexa um evento ao histórico do aggregate. Por convenção é
// chamado APENAS de dentro do root (nos métodos de mutação). O embedding
// promove este método como público — a invariante "só o root emite
// eventos" fica protegida pela barreira internal/ + convenção, não pelo
// compilador (decisão de design consciente, ver spec 2026-07-17).
func (r *EventRecorder) RecordEvent(event events.Event) {
	r.events = append(r.events, event)
}

// Events retorna CÓPIA dos eventos acumulados — expor o slice interno
// deixaria o chamador mutar o histórico por fora do root. O use case lê
// após persistir: Save → Dispatch → ClearEvents.
func (r *EventRecorder) Events() []events.Event {
	return slices.Clone(r.events)
}

// ClearEvents descarta os eventos já despachados.
func (r *EventRecorder) ClearEvents() {
	r.events = nil
}
