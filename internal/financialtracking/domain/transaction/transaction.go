// Package transaction contém o aggregate Transaction do Financial Tracking
// BC: root, VOs (ExternalRef, CategoryAssignment, AssignedBy), os três
// eventos e a porta Repository.
package transaction

import (
	"strings"
	"time"

	"github.com/google/uuid"

	"github.com/luigimenezes13/financial-manager/internal/financialtracking/domain/account"
	"github.com/luigimenezes13/financial-manager/internal/financialtracking/domain/category"
	"github.com/luigimenezes13/financial-manager/internal/financialtracking/domain/shared"
	"github.com/luigimenezes13/financial-manager/internal/kernel"
)

// transactionIdentity é o phantom type que marca identidades de transação.
type transactionIdentity struct{}

// TransactionID é o VO de identidade da transação.
//
// DDD: Value Object (identidade) — imutável, auto-validado no construtor.
type TransactionID = kernel.TypedID[transactionIdentity]

// NewTransactionID gera uma identidade nova.
func NewTransactionID() TransactionID {
	return kernel.NewTypedID[transactionIdentity]()
}

// TransactionIDFromUUID constrói o TransactionID a partir de um uuid.UUID
// já convertido pela borda, traduzindo a invariante pro sentinel do
// aggregate.
func TransactionIDFromUUID(value uuid.UUID) (TransactionID, error) {
	transactionID, err := kernel.TypedIDFromUUID[transactionIdentity](value)
	if err != nil {
		return TransactionID{}, ErrInvalidID
	}
	return transactionID, nil
}

// Transaction é o aggregate root. Referencia Account e Category por ID.
// category e externalRef são ponteiros COMO CAMPOS internos: nil = ausente
// (não categorizada / manual).
//
// DDD: Aggregate Root — fronteira de consistência; toda mutação e toda
// emissão de eventos passa por aqui.
type Transaction struct {
	kernel.EventRecorder // embed: promove Events(), ClearEvents(), RecordEvent()

	id          TransactionID
	userID      shared.UserID
	accountID   account.AccountID
	money       shared.Money
	occurredAt  time.Time
	description string
	category    *CategoryAssignment
	externalRef *ExternalRef
	reconciled  bool
}

// normalizeDescription é a única casa da regra "o que é uma descrição
// válida": valida e devolve a forma canônica. Decisão de domínio
// (2026-07-07): descrição é obrigatória — lançamento sem descrição é
// ilegível num gestor de gastos.
func normalizeDescription(raw string) (string, error) {
	description := strings.TrimSpace(raw)
	if description == "" {
		return "", ErrInvalidDescription
	}
	return description, nil
}

// validateOccurredAt é a única casa da regra do instante da transação.
// Decisão de domínio (2026-07-07): futuro é recusado — transação registra
// fato que JÁ aconteceu; lançamento agendado é conceito próprio (backlog),
// não um hack de data.
func validateOccurredAt(occurredAt time.Time) error {
	if occurredAt.IsZero() {
		return ErrInvalidOccurredAt
	}
	if occurredAt.After(time.Now()) {
		return ErrInvalidOccurredAt
	}
	return nil
}

// newTransaction concentra as invariantes comuns aos dois caminhos de
// criação. accountID zero usa o sentinel do account: a identidade é regra
// daquele aggregate (decisão de 2026-07-07).
func newTransaction(userID shared.UserID, accountID account.AccountID, money shared.Money, occurredAt time.Time, description string) (*Transaction, error) {
	if userID.IsZero() {
		return nil, shared.ErrInvalidUserID
	}
	if accountID.IsZero() {
		return nil, account.ErrInvalidID
	}
	if money.IsZero() {
		return nil, ErrInvalidMoney
	}
	if money.IsZeroAmount() {
		return nil, ErrZeroMoney
	}
	canonicalDescription, err := normalizeDescription(description)
	if err != nil {
		return nil, err
	}
	if err := validateOccurredAt(occurredAt); err != nil {
		return nil, err
	}

	return &Transaction{
		id:          NewTransactionID(),
		userID:      userID,
		accountID:   accountID,
		money:       money,
		occurredAt:  occurredAt,
		description: canonicalDescription,
	}, nil
}

// NewManual cria uma transação lançada à mão pelo usuário. NÃO emite
// evento — só o fluxo de provider emite Imported.
func NewManual(userID shared.UserID, accountID account.AccountID, money shared.Money, occurredAt time.Time, description string) (*Transaction, error) {
	return newTransaction(userID, accountID, money, occurredAt, description)
}

// NewFromProvider cria uma transação vinda de um provider Open Finance e
// emite Imported.
func NewFromProvider(userID shared.UserID, accountID account.AccountID, money shared.Money, occurredAt time.Time, description string, ref ExternalRef) (*Transaction, error) {
	if ref.IsZero() {
		return nil, ErrInvalidRef
	}
	imported, err := newTransaction(userID, accountID, money, occurredAt, description)
	if err != nil {
		return nil, err
	}

	imported.externalRef = &ref
	imported.RecordEvent(NewImported(imported.id, imported.userID, imported.accountID, ref))
	return imported, nil
}

// ID retorna a identidade da transação.
func (t *Transaction) ID() TransactionID {
	return t.id
}

// UserID retorna o dono da transação.
func (t *Transaction) UserID() shared.UserID {
	return t.userID
}

// AccountID retorna a conta da transação.
func (t *Transaction) AccountID() account.AccountID {
	return t.accountID
}

// Money retorna a quantia (sinal na convenção patrimônio: positivo=entrada,
// negativo=saída — o ACL normaliza a pegadinha DEBIT/CREDIT da Pluggy).
func (t *Transaction) Money() shared.Money {
	return t.money
}

// OccurredAt retorna quando a transação aconteceu na instituição.
func (t *Transaction) OccurredAt() time.Time {
	return t.occurredAt
}

// Description retorna a descrição da transação.
func (t *Transaction) Description() string {
	return t.description
}

// IsReconciled informa se a transação já foi conciliada com o provider.
func (t *Transaction) IsReconciled() bool {
	return t.reconciled
}

// Category retorna a atribuição de categoria, com ok=false se ainda não
// categorizada. Comma-ok (convenção do codebase, firmada no Source): o
// ponteiro fica como detalhe interno; desreferenciar devolve CÓPIA do VO.
func (t *Transaction) Category() (CategoryAssignment, bool) {
	if t.category == nil {
		return CategoryAssignment{}, false
	}
	return *t.category, true
}

// ExternalRef retorna a referência externa, com ok=false se a transação é
// manual.
func (t *Transaction) ExternalRef() (ExternalRef, bool) {
	if t.externalRef == nil {
		return ExternalRef{}, false
	}
	return *t.externalRef, true
}

// Categorize atribui a categoria da transação e emite Categorized.
//
// Decisão de domínio (2026-07-07): recategorizar SUBSTITUI a atribuição e
// re-emite o evento — correção do usuário e re-enriquecimento do sync são
// fluxos legítimos, e os consumidores (Budgets) precisam saber da troca.
func (t *Transaction) Categorize(categoryID category.CategoryID, by AssignedBy) error {
	assignment, err := NewCategoryAssignment(categoryID, by)
	if err != nil {
		return err
	}

	t.category = &assignment
	t.RecordEvent(NewCategorized(t.id, t.userID, categoryID, by))
	return nil
}

// MarkReconciled marca a transação como conciliada com o registro do
// provider e emite Reconciled.
//
// Decisões de domínio (2026-07-07): transação manual não concilia — não
// tem contraparte no provider (ErrNotReconcilable). Conciliar de novo é
// no-op idempotente SEM evento novo: syncs reprocessam, e repetição não é
// fato de domínio.
func (t *Transaction) MarkReconciled() error {
	if t.externalRef == nil {
		return ErrNotReconcilable
	}
	if t.reconciled {
		return nil
	}

	t.reconciled = true
	t.RecordEvent(NewReconciled(t.id, t.userID))
	return nil
}
