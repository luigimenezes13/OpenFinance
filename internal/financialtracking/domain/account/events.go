package account

import (
	"time"

	"github.com/luigimenezes13/financial-manager/internal/financialtracking/domain/shared"
)

// EventTypeBalanceUpdated identifica o evento no dispatcher (Register e
// Dispatch casam por este nome).
const EventTypeBalanceUpdated = "financialtracking.account.balance_updated"

// BalanceUpdated é o evento de domínio emitido quando o saldo da conta
// muda. Carrega o fato completo (quem, o quê, quando): o consumidor não
// precisa consultar o aggregate pra entender o que houve. Nos diagramas
// aparece como AccountBalanceUpdated; o prefixo cai pra evitar gagueira
// (account.AccountBalanceUpdated).
//
// DDD: Domain Event — imutável, nomeado no passado, emitido APENAS pelo
// Aggregate Root.
type BalanceUpdated struct {
	accountID  AccountID
	userID     shared.UserID
	previous   Balance
	current    Balance
	occurredAt time.Time
}

// NewBalanceUpdated é o Event Mapper deste evento, chamado apenas pelo
// root (Account.UpdateBalance). Sem validação: o root já validou tudo.
func NewBalanceUpdated(accountID AccountID, userID shared.UserID, previous Balance, current Balance) BalanceUpdated {
	return BalanceUpdated{
		accountID:  accountID,
		userID:     userID,
		previous:   previous,
		current:    current,
		occurredAt: time.Now(),
	}
}

// EventName satisfaz events.Event.
func (e BalanceUpdated) EventName() string {
	return EventTypeBalanceUpdated
}

// OccurredAt satisfaz events.Event.
func (e BalanceUpdated) OccurredAt() time.Time {
	return e.occurredAt
}

// AccountID retorna a conta cujo saldo mudou.
func (e BalanceUpdated) AccountID() AccountID {
	return e.accountID
}

// UserID retorna o dono da conta.
func (e BalanceUpdated) UserID() shared.UserID {
	return e.userID
}

// Previous retorna o saldo anterior à mudança.
func (e BalanceUpdated) Previous() Balance {
	return e.previous
}

// Current retorna o saldo após a mudança.
func (e BalanceUpdated) Current() Balance {
	return e.current
}
