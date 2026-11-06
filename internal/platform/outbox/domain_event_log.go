package outbox

import (
	"context"
	"log/slog"

	"github.com/luigimenezes13/financial-manager/internal/kernel/events"
)

type DomainEventLog struct {
	logger *slog.Logger
}

func NewDomainEventLog(logger *slog.Logger) *DomainEventLog {
	return &DomainEventLog{logger: logger}
}

func (l *DomainEventLog) SubscriberName() string {
	return "log.domain_event"
}

func (l *DomainEventLog) Handle(ctx context.Context, envelope events.Envelope) error {
	l.logger.InfoContext(ctx, "domain event delivered",
		slog.String("event_id", envelope.ID.String()),
		slog.String("event", envelope.Name),
		slog.Int("version", envelope.Version),
		slog.Time("occurred_at", envelope.OccurredAt),
	)
	return nil
}
