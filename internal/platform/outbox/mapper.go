package outbox

import (
	"encoding/json"
	"errors"
	"fmt"
	"slices"

	"github.com/google/uuid"

	"github.com/luigimenezes13/financial-manager/internal/kernel/events"
)

var (
	ErrUnmappedEvent       = errors.New("outbox: event has no mapper")
	ErrUnexpectedEventType = errors.New("outbox: mapper received another event type")
	ErrDuplicateMapper     = errors.New("outbox: event already has a mapper")
)

type EventMapper interface {
	EventName() string
	Version() int
	ToPayload(event events.Event) (json.RawMessage, error)
}

type typedMapper[E events.Event, P any] struct {
	eventName string
	version   int
	toPayload func(event E) P
}

func MapperFor[E events.Event, P any](eventName string, version int, toPayload func(event E) P) EventMapper {
	return typedMapper[E, P]{eventName: eventName, version: version, toPayload: toPayload}
}

func (m typedMapper[E, P]) EventName() string {
	return m.eventName
}

func (m typedMapper[E, P]) Version() int {
	return m.version
}

func (m typedMapper[E, P]) ToPayload(event events.Event) (json.RawMessage, error) {
	typed, matches := event.(E)
	if !matches {
		return nil, fmt.Errorf("%w: %s got %T", ErrUnexpectedEventType, m.eventName, event)
	}

	payload, err := json.Marshal(m.toPayload(typed))
	if err != nil {
		return nil, fmt.Errorf("outbox: encode %s payload: %w", m.eventName, err)
	}
	return payload, nil
}

type Mappers struct {
	byEventName map[string]EventMapper
}

func NewMappers(mappers ...EventMapper) (Mappers, error) {
	byEventName := make(map[string]EventMapper, len(mappers))
	for _, mapper := range mappers {
		if _, registered := byEventName[mapper.EventName()]; registered {
			return Mappers{}, fmt.Errorf("%w: %s", ErrDuplicateMapper, mapper.EventName())
		}
		byEventName[mapper.EventName()] = mapper
	}
	return Mappers{byEventName: byEventName}, nil
}

func (m Mappers) Seal(event events.Event) (events.Envelope, error) {
	mapper, registered := m.byEventName[event.EventName()]
	if !registered {
		return events.Envelope{}, fmt.Errorf("%w: %s", ErrUnmappedEvent, event.EventName())
	}

	payload, err := mapper.ToPayload(event)
	if err != nil {
		return events.Envelope{}, err
	}

	id, err := uuid.NewV7()
	if err != nil {
		return events.Envelope{}, fmt.Errorf("outbox: generate envelope id: %w", err)
	}

	return events.Envelope{
		ID:         id,
		Name:       event.EventName(),
		Version:    mapper.Version(),
		OccurredAt: event.OccurredAt(),
		Payload:    payload,
	}, nil
}

func (m Mappers) EventNames() []string {
	names := make([]string, 0, len(m.byEventName))
	for name := range m.byEventName {
		names = append(names, name)
	}
	slices.Sort(names)
	return names
}
