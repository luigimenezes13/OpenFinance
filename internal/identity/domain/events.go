package identity

import (
	"time"
)

// EventTypeRegistered identifica o evento no dispatcher.
const EventTypeRegistered = "identity.user.registered"

// Registered é emitido quando um usuário se registra (primeiro login).
//
// É o primeiro evento do projeto com consumidor REAL previsto: o Financial
// Tracking pode reagir semeando as categorias padrão do usuário. Repare no
// que o evento NÃO carrega: o subject do provedor. Um evento é contrato
// público entre contextos, e vazar a referência externa por ele desfaria o
// encapsulamento que o VO ExternalIdentity existe pra garantir — quem
// consome precisa saber QUEM se registrou, não COMO se autenticou.
//
// DDD: Domain Event — imutável, nomeado no passado, emitido APENAS pelo
// Aggregate Root.
type Registered struct {
	userID     UserID
	email      Email
	provider   string
	occurredAt time.Time
}

// NewRegistered é o Event Mapper deste evento, chamado apenas pelo root.
func NewRegistered(userID UserID, email Email, external ExternalIdentity) Registered {
	return Registered{
		userID:     userID,
		email:      email,
		provider:   external.Provider(),
		occurredAt: time.Now(),
	}
}

// EventName satisfaz events.Event.
func (e Registered) EventName() string {
	return EventTypeRegistered
}

// OccurredAt satisfaz events.Event.
func (e Registered) OccurredAt() time.Time {
	return e.occurredAt
}

// UserID retorna o usuário registrado.
func (e Registered) UserID() UserID {
	return e.userID
}

// Email retorna o e-mail do usuário registrado.
func (e Registered) Email() Email {
	return e.email
}

// Provider retorna o provedor usado no registro (sem o subject).
func (e Registered) Provider() string {
	return e.provider
}
