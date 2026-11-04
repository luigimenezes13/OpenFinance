package outbox

import (
	"errors"
	"fmt"

	"github.com/luigimenezes13/financial-manager/internal/kernel/events"
)

var ErrDuplicateSubscriber = errors.New("outbox: subscriber name already registered")

type Subscriptions struct {
	byName      map[string]events.Subscriber
	byEventName map[string][]events.Subscriber
}

func NewSubscriptions() *Subscriptions {
	return &Subscriptions{
		byName:      map[string]events.Subscriber{},
		byEventName: map[string][]events.Subscriber{},
	}
}

func (s *Subscriptions) Subscribe(subscriber events.Subscriber, eventNames ...string) error {
	name := subscriber.SubscriberName()
	if _, taken := s.byName[name]; taken {
		return fmt.Errorf("%w: %s", ErrDuplicateSubscriber, name)
	}

	s.byName[name] = subscriber
	for _, eventName := range eventNames {
		s.byEventName[eventName] = append(s.byEventName[eventName], subscriber)
	}
	return nil
}

func (s *Subscriptions) subscribersOf(eventName string) []events.Subscriber {
	return s.byEventName[eventName]
}

func (s *Subscriptions) subscriberNamed(name string) (events.Subscriber, bool) {
	subscriber, found := s.byName[name]
	return subscriber, found
}
