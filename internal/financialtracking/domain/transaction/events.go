package transaction

import (
	"time"

	"github.com/luigimenezes13/financial-manager/internal/financialtracking/domain/account"
	"github.com/luigimenezes13/financial-manager/internal/financialtracking/domain/category"
	"github.com/luigimenezes13/financial-manager/internal/financialtracking/domain/shared"
)

// Nomes dos eventos no dispatcher.
const (
	EventTypeImported    = "financialtracking.transaction.imported"
	EventTypeCategorized = "financialtracking.transaction.categorized"
	EventTypeReconciled  = "financialtracking.transaction.reconciled"
)

// Os três eventos seguem o mesmo padrão do account.BalanceUpdated:
// Event Mapper dedicado + EventName/OccurredAt satisfazendo events.Event
// + getters. Nos diagramas: TransactionImported, TransactionCategorized,
// TransactionReconciled (o prefixo cai pra evitar gagueira).

// Imported é emitido quando uma transação entra pelo fluxo de provider
// (NewFromProvider). Lançamento manual NÃO emite.
//
// DDD: Domain Event — imutável, nomeado no passado, emitido apenas pelo
// Aggregate Root.
type Imported struct {
	transactionID TransactionID
	userID        shared.UserID
	accountID     account.AccountID
	ref           ExternalRef
	occurredAt    time.Time
}

// NewImported é o Event Mapper deste evento, chamado apenas pelo root.
// Sem validação: o root já validou tudo.
func NewImported(transactionID TransactionID, userID shared.UserID, accountID account.AccountID, ref ExternalRef) Imported {
	return Imported{
		transactionID: transactionID,
		userID:        userID,
		accountID:     accountID,
		ref:           ref,
		occurredAt:    time.Now(),
	}
}

// EventName satisfaz events.Event.
func (e Imported) EventName() string {
	return EventTypeImported
}

// OccurredAt satisfaz events.Event.
func (e Imported) OccurredAt() time.Time {
	return e.occurredAt
}

// TransactionID retorna a transação importada.
func (e Imported) TransactionID() TransactionID {
	return e.transactionID
}

// UserID retorna o dono da transação.
func (e Imported) UserID() shared.UserID {
	return e.userID
}

// AccountID retorna a conta da transação.
func (e Imported) AccountID() account.AccountID {
	return e.accountID
}

// Ref retorna a referência externa da importação.
func (e Imported) Ref() ExternalRef {
	return e.ref
}

// Categorized é emitido quando a transação ganha (ou troca de) categoria.
// Consumidor futuro: Budgets (PR5).
//
// DDD: Domain Event — imutável, nomeado no passado, emitido apenas pelo
// Aggregate Root.
type Categorized struct {
	transactionID TransactionID
	userID        shared.UserID
	categoryID    category.CategoryID
	by            AssignedBy
	occurredAt    time.Time
}

// NewCategorized é o Event Mapper deste evento, chamado apenas pelo root.
func NewCategorized(transactionID TransactionID, userID shared.UserID, categoryID category.CategoryID, by AssignedBy) Categorized {
	return Categorized{
		transactionID: transactionID,
		userID:        userID,
		categoryID:    categoryID,
		by:            by,
		occurredAt:    time.Now(),
	}
}

// EventName satisfaz events.Event.
func (e Categorized) EventName() string {
	return EventTypeCategorized
}

// OccurredAt satisfaz events.Event.
func (e Categorized) OccurredAt() time.Time {
	return e.occurredAt
}

// TransactionID retorna a transação categorizada.
func (e Categorized) TransactionID() TransactionID {
	return e.transactionID
}

// UserID retorna o dono da transação.
func (e Categorized) UserID() shared.UserID {
	return e.userID
}

// CategoryID retorna a categoria atribuída.
func (e Categorized) CategoryID() category.CategoryID {
	return e.categoryID
}

// By retorna quem atribuiu.
func (e Categorized) By() AssignedBy {
	return e.by
}

// Reconciled é emitido quando a transação é conciliada com o registro do
// provider. Só dispara em fluxo Open Finance real (PR4) — modelado desde
// já pra interface não mudar.
//
// DDD: Domain Event — imutável, nomeado no passado, emitido apenas pelo
// Aggregate Root.
type Reconciled struct {
	transactionID TransactionID
	userID        shared.UserID
	occurredAt    time.Time
}

// NewReconciled é o Event Mapper deste evento, chamado apenas pelo root.
func NewReconciled(transactionID TransactionID, userID shared.UserID) Reconciled {
	return Reconciled{
		transactionID: transactionID,
		userID:        userID,
		occurredAt:    time.Now(),
	}
}

// EventName satisfaz events.Event.
func (e Reconciled) EventName() string {
	return EventTypeReconciled
}

// OccurredAt satisfaz events.Event.
func (e Reconciled) OccurredAt() time.Time {
	return e.occurredAt
}

// TransactionID retorna a transação conciliada.
func (e Reconciled) TransactionID() TransactionID {
	return e.transactionID
}

// UserID retorna o dono da transação.
func (e Reconciled) UserID() shared.UserID {
	return e.userID
}
