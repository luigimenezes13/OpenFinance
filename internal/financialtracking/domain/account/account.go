// Package account contém o aggregate Account do Financial Tracking BC:
// root, VOs (Balance, Kind, Source), evento BalanceUpdated e a porta
// Repository.
package account

import (
	"strings"
	"time"

	"github.com/google/uuid"

	"github.com/luigimenezes13/financial-manager/internal/financialtracking/domain/shared"
	"github.com/luigimenezes13/financial-manager/internal/kernel"
)

// accountIdentity é o phantom type que marca identidades de conta.
type accountIdentity struct{}

// AccountID é o VO de identidade da conta. Mesma mecânica do shared.UserID,
// com uma diferença: o domínio GERA AccountID (identidade nasce aqui),
// enquanto UserID sempre chega de fora.
//
// DDD: Value Object (identidade) — imutável, auto-validado no construtor.
type AccountID = kernel.TypedID[accountIdentity]

// NewAccountID gera uma identidade nova.
func NewAccountID() AccountID {
	return kernel.NewTypedID[accountIdentity]()
}

// AccountIDFromUUID constrói o AccountID a partir de um uuid.UUID já
// convertido pela borda, traduzindo a invariante pro sentinel do aggregate.
func AccountIDFromUUID(value uuid.UUID) (AccountID, error) {
	accountID, err := kernel.TypedIDFromUUID[accountIdentity](value)
	if err != nil {
		return AccountID{}, ErrInvalidID
	}
	return accountID, nil
}

// Account é o aggregate root. Todos os campos não-exportados: mutação só
// pelos métodos abaixo — o compilador garante a fronteira de consistência.
//
// DDD: Aggregate Root — fronteira de consistência; toda mutação e toda
// emissão de eventos passa por aqui.
type Account struct {
	kernel.EventRecorder // embed: promove Events(), ClearEvents(), RecordEvent()

	id      AccountID
	userID  shared.UserID
	name    string
	kind    Kind
	balance Balance
	source  Source
}

// New valida invariantes e cria uma conta nova (caminho dos use cases).
// O saldo inicial é zero na moeda informada, datado de agora. Criar conta
// não emite BalanceUpdated — nascer zerada não é mudança de saldo.
func New(userID shared.UserID, name string, kind Kind, currency shared.Currency, source Source) (*Account, error) {
	if userID.IsZero() {
		return nil, shared.ErrInvalidUserID
	}
	canonicalName, err := normalizeName(name)
	if err != nil {
		return nil, err
	}
	if kind.IsZero() {
		return nil, ErrInvalidKind
	}
	if source.IsZero() {
		return nil, ErrInvalidSource
	}

	initialMoney, err := shared.NewMoney(0, currency)
	if err != nil {
		return nil, err
	}
	initialBalance, err := NewBalance(initialMoney, time.Now())
	if err != nil {
		return nil, err
	}

	return &Account{
		id:      NewAccountID(),
		userID:  userID,
		name:    canonicalName,
		kind:    kind,
		balance: initialBalance,
		source:  source,
	}, nil
}

// normalizeName é a única casa da regra "o que é um nome de conta válido":
// valida e devolve a forma canônica. New e Rename usam o retorno.
func normalizeName(raw string) (string, error) {
	name := strings.TrimSpace(raw)
	if name == "" {
		return "", ErrInvalidName
	}
	return name, nil
}

// ID retorna a identidade da conta.
func (a *Account) ID() AccountID {
	return a.id
}

// UserID retorna o dono da conta.
func (a *Account) UserID() shared.UserID {
	return a.userID
}

// Name retorna o nome da conta.
func (a *Account) Name() string {
	return a.name
}

// Kind retorna a natureza da conta.
func (a *Account) Kind() Kind {
	return a.kind
}

// Balance retorna o saldo atual.
func (a *Account) Balance() Balance {
	return a.balance
}

// Source retorna a origem da conta.
func (a *Account) Source() Source {
	return a.source
}

// UpdateBalance troca o saldo e emite BalanceUpdated — a ÚNICA porta de
// mutação de saldo do sistema.
//
// Decisão de domínio (2026-07-04): saldo não volta no tempo. asOf anterior
// ao atual → ErrStaleBalance — webhooks/syncs chegam fora de ordem e o use
// case decide o que fazer (em geral, ignorar). Mesmo instante é permitido
// (correção de valor).
func (a *Account) UpdateBalance(newBalance Balance) error {
	if newBalance.IsZero() {
		return ErrInvalidBalance
	}
	newCurrency := newBalance.Currency()
	if !newCurrency.Equals(a.balance.Currency()) {
		return ErrCurrencyMismatch
	}
	newAsOf := newBalance.AsOf()
	if newAsOf.Before(a.balance.AsOf()) {
		return ErrStaleBalance
	}

	previous := a.balance
	a.balance = newBalance
	a.RecordEvent(NewBalanceUpdated(a.id, a.userID, previous, newBalance))
	return nil
}

// Rename troca o nome da conta (mesma validação do New, via validateName).
// Sem evento: nenhum consumidor se importa com renomeação no v1.
func (a *Account) Rename(newName string) error {
	canonicalName, err := normalizeName(newName)
	if err != nil {
		return err
	}
	a.name = canonicalName
	return nil
}
