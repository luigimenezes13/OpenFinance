// Package events define os contratos de eventos de domínio compartilhados
// pelos bounded contexts. Aqui vivem só interfaces: os eventos concretos
// vivem no package de cada aggregate (quem emite é o root), e o dispatcher
// concreto vive na infraestrutura (platform).
package events

import "time"

// Event é o contrato mínimo que todo evento de domínio satisfaz.
//
// DDD: Domain Event (contrato) — os eventos concretos são VOs imutáveis
// nomeados no passado.
type Event interface {
	// EventName identifica o tipo do evento no formato
	// "<contexto>.<aggregate>.<fato_no_passado>".
	EventName() string

	// OccurredAt registra quando o fato aconteceu no domínio.
	OccurredAt() time.Time
}
