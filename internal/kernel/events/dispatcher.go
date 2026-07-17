package events

import "context"

// Handler reage a um evento despachado.
type Handler func(ctx context.Context, event Event) error

// Dispatcher é a porta de publicação de eventos. A implementação v1
// (in-process síncrona) vive na infraestrutura; PR2 troca por outbox sem
// tocar neste contrato.
//
// DDD: Port — contrato no domínio, implementação na infraestrutura.
type Dispatcher interface {
	// Register associa um handler a um nome de evento. Vários handlers
	// podem observar o mesmo evento.
	Register(eventName string, handler Handler)

	// Dispatch entrega os eventos aos handlers registrados. Na v1 é
	// síncrono: erro de handler interrompe e retorna na hora.
	Dispatch(ctx context.Context, events ...Event) error
}
