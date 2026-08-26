// Package events implementa o Dispatcher declarado no domínio
// (kernel/events). A interface fica no domínio, a implementação aqui — DIP
// na prática.
package events

import (
	"context"
	"fmt"
	"log/slog"
	"sync"

	domainevents "github.com/luigimenezes13/financial-manager/internal/kernel/events"
)

// InProcessDispatcher entrega eventos aos handlers registrados no MESMO
// processo, de forma síncrona.
//
// É o suficiente pro v1 e a interface não muda quando virar outbox (PR2):
// o aggregate continua acumulando evento igual, o use case continua
// chamando Dispatch. O que muda é só o tipo instanciado no main.
//
// Limitações conscientes: sem retry, sem persistência, sem ordenação entre
// processos. Se o handler falhar, o erro sobe e o use case falha junto —
// que é o comportamento honesto quando não há garantia de entrega.
type InProcessDispatcher struct {
	// O mutex protege o mapa: Register roda no boot (uma goroutine) e
	// Dispatch roda por request (várias). Sem ele, o race detector acusa —
	// e em produção o mapa corrompe.
	mutex    sync.RWMutex
	handlers map[string][]domainevents.Handler
	logger   *slog.Logger
}

// Garante em tempo de compilação que a implementação satisfaz a porta.
var _ domainevents.Dispatcher = (*InProcessDispatcher)(nil)

// NewInProcessDispatcher cria o dispatcher.
func NewInProcessDispatcher(logger *slog.Logger) *InProcessDispatcher {
	return &InProcessDispatcher{
		handlers: make(map[string][]domainevents.Handler),
		logger:   logger,
	}
}

// Register associa um handler a um nome de evento.
func (d *InProcessDispatcher) Register(eventName string, handler domainevents.Handler) {
	d.mutex.Lock()
	defer d.mutex.Unlock()

	d.handlers[eventName] = append(d.handlers[eventName], handler)
}

// Dispatch entrega os eventos aos handlers registrados, na ordem recebida.
//
// Evento sem handler NÃO é erro: no v1 os quatro eventos existem pro
// consumidor futuro (Budgets, Goals), e exigir handler transformaria
// modelagem antecipada em falha de runtime. Fica o log em nível debug pra
// dar rastro sem poluir.
func (d *InProcessDispatcher) Dispatch(ctx context.Context, events ...domainevents.Event) error {
	for _, event := range events {
		if err := d.dispatchOne(ctx, event); err != nil {
			return err
		}
	}
	return nil
}

// dispatchOne entrega um evento. Separado pra manter um nível de
// indentação por método.
func (d *InProcessDispatcher) dispatchOne(ctx context.Context, event domainevents.Event) error {
	name := event.EventName()

	d.mutex.RLock()
	// Cópia da fatia sob lock de leitura: chamar handler com o lock preso
	// travaria qualquer Register e convidaria deadlock se um handler
	// despachasse outro evento.
	handlers := make([]domainevents.Handler, len(d.handlers[name]))
	copy(handlers, d.handlers[name])
	d.mutex.RUnlock()

	if len(handlers) == 0 {
		d.logger.DebugContext(ctx, "evento sem handler registrado", slog.String("event", name))
		return nil
	}

	for _, handler := range handlers {
		if err := handler(ctx, event); err != nil {
			return fmt.Errorf("events: handler de %s falhou: %w", name, err)
		}
	}
	return nil
}
