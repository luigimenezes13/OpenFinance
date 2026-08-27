// Package application_test testa os use cases pela API pública (package
// externo _test, convenção do spec §8). Aqui vivem os FAKES das portas e os
// builders de aggregate compartilhados por todos os _test.go do package.
//
// Por que fakes escritos à mão e não uma lib de mock: as portas do domínio
// são interfaces pequenas (2 métodos), e um fake com map em memória testa a
// ORQUESTRAÇÃO de verdade (salvou? achou? qual evento saiu?) em vez de
// testar expectativas de mock. Mock que verifica "Save foi chamado 1 vez"
// testa o dublê, não o comportamento.
package application_test

import (
	"context"
	"errors"
	"testing"
	"time"

	"github.com/google/uuid"
	"github.com/stretchr/testify/require"

	"github.com/luigimenezes13/financial-manager/internal/financialtracking/domain/account"
	"github.com/luigimenezes13/financial-manager/internal/financialtracking/domain/category"
	"github.com/luigimenezes13/financial-manager/internal/financialtracking/domain/openfinance"
	"github.com/luigimenezes13/financial-manager/internal/financialtracking/domain/shared"
	"github.com/luigimenezes13/financial-manager/internal/financialtracking/domain/transaction"
	"github.com/luigimenezes13/financial-manager/internal/kernel/events"
)

// errInfra simula falha de infraestrutura (banco fora, rede caiu). O use
// case não deve traduzir nem embrulhar: erro de fronteira sobe como veio.
var errInfra = errors.New("fake: infrastructure failure")

// --- Fakes das portas de persistência -------------------------------------

// fakeAccounts implementa account.Repository com um map. Os campos *Err
// injetam falha pra exercitar os caminhos de erro.
type fakeAccounts struct {
	stored  map[string]*account.Account
	saved   []*account.Account
	saveErr error
	findErr error
}

func newFakeAccounts(stored ...*account.Account) *fakeAccounts {
	fake := &fakeAccounts{stored: make(map[string]*account.Account, len(stored))}
	for _, existing := range stored {
		fake.stored[existing.ID().String()] = existing
	}
	return fake
}

func (f *fakeAccounts) Save(_ context.Context, target *account.Account) error {
	if f.saveErr != nil {
		return f.saveErr
	}
	f.stored[target.ID().String()] = target
	f.saved = append(f.saved, target)
	return nil
}

func (f *fakeAccounts) FindByID(_ context.Context, id account.AccountID) (*account.Account, error) {
	if f.findErr != nil {
		return nil, f.findErr
	}
	found, ok := f.stored[id.String()]
	if !ok {
		// Mesmo contrato do repository real: ausência é ErrNotFound do
		// aggregate, não erro de driver.
		return nil, account.ErrNotFound
	}
	return found, nil
}

// fakeTransactions implementa transaction.Repository.
type fakeTransactions struct {
	stored  map[string]*transaction.Transaction
	saved   []*transaction.Transaction
	saveErr error
	findErr error
}

func newFakeTransactions(stored ...*transaction.Transaction) *fakeTransactions {
	fake := &fakeTransactions{stored: make(map[string]*transaction.Transaction, len(stored))}
	for _, existing := range stored {
		fake.stored[existing.ID().String()] = existing
	}
	return fake
}

func (f *fakeTransactions) Save(_ context.Context, target *transaction.Transaction) error {
	if f.saveErr != nil {
		return f.saveErr
	}
	f.stored[target.ID().String()] = target
	f.saved = append(f.saved, target)
	return nil
}

func (f *fakeTransactions) FindByID(_ context.Context, id transaction.TransactionID) (*transaction.Transaction, error) {
	if f.findErr != nil {
		return nil, f.findErr
	}
	found, ok := f.stored[id.String()]
	if !ok {
		return nil, transaction.ErrNotFound
	}
	return found, nil
}

// fakeCategories implementa category.Repository.
type fakeCategories struct {
	stored  map[string]*category.Category
	saved   []*category.Category
	saveErr error
	findErr error
}

func newFakeCategories(stored ...*category.Category) *fakeCategories {
	fake := &fakeCategories{stored: make(map[string]*category.Category, len(stored))}
	for _, existing := range stored {
		fake.stored[existing.ID().String()] = existing
	}
	return fake
}

func (f *fakeCategories) Save(_ context.Context, target *category.Category) error {
	if f.saveErr != nil {
		return f.saveErr
	}
	f.stored[target.ID().String()] = target
	f.saved = append(f.saved, target)
	return nil
}

func (f *fakeCategories) FindByID(_ context.Context, id category.CategoryID) (*category.Category, error) {
	if f.findErr != nil {
		return nil, f.findErr
	}
	found, ok := f.stored[id.String()]
	if !ok {
		return nil, category.ErrNotFound
	}
	return found, nil
}

// --- Fake do dispatcher ---------------------------------------------------

// fakeDispatcher guarda o que foi despachado — é assim que o teste verifica
// que o use case publicou o fato certo DEPOIS de persistir.
type fakeDispatcher struct {
	dispatched  []events.Event
	dispatchErr error
}

func (f *fakeDispatcher) Register(_ string, _ events.Handler) {}

func (f *fakeDispatcher) Dispatch(_ context.Context, dispatched ...events.Event) error {
	if f.dispatchErr != nil {
		return f.dispatchErr
	}
	f.dispatched = append(f.dispatched, dispatched...)
	return nil
}

// eventNames devolve os nomes dos eventos despachados, na ordem.
func (f *fakeDispatcher) eventNames() []string {
	names := make([]string, 0, len(f.dispatched))
	for _, dispatched := range f.dispatched {
		names = append(names, dispatched.EventName())
	}
	return names
}

// --- Fake do provider Open Finance ---------------------------------------

// fakeProvider implementa openfinance.Provider e registra COM O QUE foi
// chamado — é como o teste prova que o use case usou a referência do VO
// Source, e não algo vindo do input.
type fakeProvider struct {
	name         string
	transactions []openfinance.ProviderTransaction
	fetchErr     error

	// Saldo devolvido pelo FetchBalance. Zero value = saldo zerado em BRL
	// datado de agora (montado no método), pra os testes que não se importam
	// com saldo não precisarem configurá-lo.
	balance         *openfinance.ProviderBalance
	balanceErr      error
	balanceRequests int

	calledAccountID string
	calledSince     time.Time
}

func (f *fakeProvider) Name() string {
	return f.name
}

func (f *fakeProvider) FetchTransactions(_ context.Context, providerAccountID string, since time.Time) ([]openfinance.ProviderTransaction, error) {
	f.calledAccountID = providerAccountID
	f.calledSince = since
	if f.fetchErr != nil {
		return nil, f.fetchErr
	}
	return f.transactions, nil
}

func (f *fakeProvider) FetchBalance(_ context.Context, providerAccountID string) (openfinance.ProviderBalance, error) {
	f.balanceRequests++
	f.calledAccountID = providerAccountID
	if f.balanceErr != nil {
		return openfinance.ProviderBalance{}, f.balanceErr
	}
	if f.balance != nil {
		return *f.balance, nil
	}
	return openfinance.ProviderBalance{AmountInCents: 0, CurrencyCode: "BRL", AsOf: time.Now()}, nil
}

// --- Builders de aggregate ------------------------------------------------

func mustUserID(t *testing.T, raw uuid.UUID) shared.UserID {
	t.Helper()
	userID, err := shared.NewUserID(raw)
	require.NoError(t, err)
	return userID
}

func mustCurrency(t *testing.T, code string) shared.Currency {
	t.Helper()
	currency, err := shared.NewCurrency(code)
	require.NoError(t, err)
	return currency
}

func mustMoney(t *testing.T, amount int64, code string) shared.Money {
	t.Helper()
	money, err := shared.NewMoney(amount, mustCurrency(t, code))
	require.NoError(t, err)
	return money
}

// newManualAccount cria uma conta manual pronta pra ser "achada" pelo fake.
func newManualAccount(t *testing.T, ownerID uuid.UUID, currencyCode string) *account.Account {
	t.Helper()
	kind, err := account.NewKind("checking")
	require.NoError(t, err)

	manual, err := account.New(mustUserID(t, ownerID), "Conta Corrente", kind, mustCurrency(t, currencyCode), account.NewManualSource())
	require.NoError(t, err)
	return manual
}

// newConnectedAccount cria uma conta com origem Open Finance — a única que
// o use case de importação aceita.
func newConnectedAccount(t *testing.T, ownerID uuid.UUID, providerName string, providerAccountID string, currencyCode string) *account.Account {
	t.Helper()
	kind, err := account.NewKind("checking")
	require.NoError(t, err)

	source, err := account.NewOpenFinanceSource(providerName, providerAccountID)
	require.NoError(t, err)

	connected, err := account.New(mustUserID(t, ownerID), "Conta Conectada", kind, mustCurrency(t, currencyCode), source)
	require.NoError(t, err)
	return connected
}

// newManualTransaction cria um lançamento manual já "persistido" no fake.
func newManualTransaction(t *testing.T, ownerID uuid.UUID, accountID account.AccountID, amount int64, currencyCode string) *transaction.Transaction {
	t.Helper()
	recorded, err := transaction.NewManual(
		mustUserID(t, ownerID),
		accountID,
		mustMoney(t, amount, currencyCode),
		time.Now().Add(-time.Hour),
		"Mercado",
	)
	require.NoError(t, err)
	return recorded
}

func newCategory(t *testing.T, ownerID uuid.UUID, name string) *category.Category {
	t.Helper()
	created, err := category.New(mustUserID(t, ownerID), name, nil)
	require.NoError(t, err)
	return created
}
