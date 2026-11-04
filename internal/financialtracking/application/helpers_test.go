// Package application_test tests the use cases through their public API and
// holds the port fakes and aggregate builders shared by every test file.
//
// The fakes are hand written in-memory maps instead of mocks: the ports are
// small interfaces, and a map-backed fake tests the real orchestration (was it
// saved? found? which event was recorded?) instead of mock expectations.
package application_test

import (
	"context"
	"errors"
	"sort"
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

// errInfra simulates an infrastructure failure. The use case must neither
// translate nor wrap it: a boundary error goes up as it came.
var errInfra = errors.New("fake: infrastructure failure")

// recordedEvents mirrors what the real repositories do inside Save: the
// aggregate's pending events go to the outbox and are cleared afterwards.
type recordedEvents struct {
	savedEvents []events.Event
}

func (r *recordedEvents) record(pending []events.Event) {
	r.savedEvents = append(r.savedEvents, pending...)
}

func (r *recordedEvents) savedEventNames() []string {
	names := make([]string, 0, len(r.savedEvents))
	for _, saved := range r.savedEvents {
		names = append(names, saved.EventName())
	}
	return names
}

type fakeAccounts struct {
	recordedEvents
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
	f.record(target.Events())
	target.ClearEvents()
	return nil
}

func (f *fakeAccounts) FindByID(_ context.Context, id account.AccountID) (*account.Account, error) {
	if f.findErr != nil {
		return nil, f.findErr
	}
	found, ok := f.stored[id.String()]
	if !ok {
		return nil, account.ErrNotFound
	}
	return found, nil
}

// ListByUser sorts by name, like the real repository, so the use case test
// exercises the real projection.
func (f *fakeAccounts) ListByUser(_ context.Context, userID shared.UserID) ([]*account.Account, error) {
	if f.findErr != nil {
		return nil, f.findErr
	}

	found := make([]*account.Account, 0, len(f.stored))
	for _, existing := range f.stored {
		owner := existing.UserID()
		if owner.Equals(userID) {
			found = append(found, existing)
		}
	}
	sort.Slice(found, func(first, second int) bool {
		return found[first].Name() < found[second].Name()
	})
	return found, nil
}

type fakeTransactions struct {
	recordedEvents
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
	f.record(target.Events())
	target.ClearEvents()
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

// List applies the criteria filters in memory and returns the newest first,
// like the real repository, so the use case test proves it translated the
// input into Criteria correctly.
func (f *fakeTransactions) List(_ context.Context, criteria transaction.Criteria) ([]*transaction.Transaction, error) {
	if f.findErr != nil {
		return nil, f.findErr
	}

	found := make([]*transaction.Transaction, 0, len(f.stored))
	for _, existing := range f.stored {
		if !f.matches(existing, criteria) {
			continue
		}
		found = append(found, existing)
	}

	sort.Slice(found, func(first, second int) bool {
		return found[first].OccurredAt().After(found[second].OccurredAt())
	})

	page := criteria.Page()
	if page.Offset() >= len(found) {
		return []*transaction.Transaction{}, nil
	}
	end := page.Offset() + page.Limit()
	if end > len(found) {
		end = len(found)
	}
	return found[page.Offset():end], nil
}

func (f *fakeTransactions) matches(candidate *transaction.Transaction, criteria transaction.Criteria) bool {
	owner := candidate.UserID()
	if !owner.Equals(criteria.UserID()) {
		return false
	}
	if accountID, ok := criteria.AccountID(); ok {
		if !candidate.AccountID().Equals(accountID) {
			return false
		}
	}
	if categoryID, ok := criteria.CategoryID(); ok {
		assignment, assigned := candidate.Category()
		if !assigned || !assignment.CategoryID().Equals(categoryID) {
			return false
		}
	}
	if from, ok := criteria.From(); ok && candidate.OccurredAt().Before(from) {
		return false
	}
	if to, ok := criteria.To(); ok && candidate.OccurredAt().After(to) {
		return false
	}
	return true
}

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

func (f *fakeCategories) ListByUser(_ context.Context, userID shared.UserID) ([]*category.Category, error) {
	if f.findErr != nil {
		return nil, f.findErr
	}

	found := make([]*category.Category, 0, len(f.stored))
	for _, existing := range f.stored {
		owner := existing.UserID()
		if owner.Equals(userID) {
			found = append(found, existing)
		}
	}
	sort.Slice(found, func(first, second int) bool {
		return found[first].Name() < found[second].Name()
	})
	return found, nil
}

// callOrder records the order of calls across several fakes, so a test can
// prove the provider is queried before the unit of work opens. A nil
// callOrder records nothing.
type callOrder struct {
	calls []string
}

func (c *callOrder) record(call string) {
	if c == nil {
		return
	}
	c.calls = append(c.calls, call)
}

// fakeUnitOfWork runs the work in place and counts the runs. When err is set
// it returns that error without running the work, like a unit that fails to
// open or to commit.
type fakeUnitOfWork struct {
	runs  int
	err   error
	order *callOrder
}

func (f *fakeUnitOfWork) Run(ctx context.Context, work func(ctx context.Context) error) error {
	f.runs++
	f.order.record("unitOfWork.Run")
	if f.err != nil {
		return f.err
	}
	return work(ctx)
}

// fakeProvider records the arguments it was called with: that is how a test
// proves the use case used the reference from the Source value object and
// not something from the input. A nil balance answers a zero BRL balance as
// of now, so tests that do not care about the balance skip configuring it.
type fakeProvider struct {
	name         string
	transactions []openfinance.ProviderTransaction
	fetchErr     error

	balance         *openfinance.ProviderBalance
	balanceErr      error
	balanceRequests int

	order *callOrder

	calledAccountID string
	calledSince     time.Time
}

func (f *fakeProvider) Name() string {
	return f.name
}

func (f *fakeProvider) FetchTransactions(_ context.Context, providerAccountID string, since time.Time) ([]openfinance.ProviderTransaction, error) {
	f.order.record("provider.FetchTransactions")
	f.calledAccountID = providerAccountID
	f.calledSince = since
	if f.fetchErr != nil {
		return nil, f.fetchErr
	}
	return f.transactions, nil
}

func (f *fakeProvider) FetchBalance(_ context.Context, providerAccountID string) (openfinance.ProviderBalance, error) {
	f.order.record("provider.FetchBalance")
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

func newManualAccount(t *testing.T, ownerID uuid.UUID, currencyCode string) *account.Account {
	t.Helper()
	kind, err := account.NewKind("checking")
	require.NoError(t, err)

	manual, err := account.New(mustUserID(t, ownerID), "Conta Corrente", kind, mustCurrency(t, currencyCode), account.NewManualSource())
	require.NoError(t, err)
	return manual
}

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

func newManualTransaction(t *testing.T, ownerID uuid.UUID, accountID account.AccountID, amount int64, currencyCode string) *transaction.Transaction {
	t.Helper()
	recorded, err := transaction.NewManual(
		mustUserID(t, ownerID),
		accountID,
		mustMoney(t, amount, currencyCode),
		time.Now().Add(-time.Hour),
		"Groceries",
	)
	require.NoError(t, err)
	return recorded
}

// newManualTransactionAt pins the instant so ordering and period tests do
// not depend on the clock.
func newManualTransactionAt(t *testing.T, ownerID uuid.UUID, accountID account.AccountID, amount int64, occurredAt time.Time) *transaction.Transaction {
	t.Helper()
	recorded, err := transaction.NewManual(
		mustUserID(t, ownerID),
		accountID,
		mustMoney(t, amount, "BRL"),
		occurredAt,
		"Groceries",
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
