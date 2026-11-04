// Package ginhandler_test tests the HTTP edge end to end INSIDE the process:
// httptest, the real gin router, real middleware and REAL use cases, with only
// the repositories and the provider replaced by in-memory fakes.
//
// The use cases are real, not mocked, because these tests prove the
// translation at both ends: request JSON to Input, and domain sentinel to
// HTTP status. With a mocked use case the test would pass even with a wrong
// error mapping, since the error would never be born in the domain.
package ginhandler_test

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"io"
	"log/slog"
	"net/http"
	"net/http/httptest"
	"sort"
	"testing"
	"time"

	"github.com/gin-gonic/gin"
	"github.com/google/uuid"
	"github.com/stretchr/testify/require"

	"github.com/luigimenezes13/financial-manager/internal/financialtracking/adapter/ginhandler"
	"github.com/luigimenezes13/financial-manager/internal/financialtracking/application"
	"github.com/luigimenezes13/financial-manager/internal/financialtracking/domain/account"
	"github.com/luigimenezes13/financial-manager/internal/financialtracking/domain/category"
	"github.com/luigimenezes13/financial-manager/internal/financialtracking/domain/openfinance"
	"github.com/luigimenezes13/financial-manager/internal/financialtracking/domain/shared"
	"github.com/luigimenezes13/financial-manager/internal/financialtracking/domain/transaction"
	"github.com/luigimenezes13/financial-manager/internal/identity/adapter/ginmiddleware"
	"github.com/luigimenezes13/financial-manager/internal/kernel/events"
	"github.com/luigimenezes13/financial-manager/internal/platform/httperror"
)

var errInfra = errors.New("fake: infrastructure failure")

func TestMain(main *testing.M) {
	gin.SetMode(gin.TestMode)
	main.Run()
}

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
	sort.Slice(found, func(first, second int) bool { return found[first].Name() < found[second].Name() })
	return found, nil
}

type fakeTransactions struct {
	recordedEvents
	stored  map[string]*transaction.Transaction
	saveErr error
	listErr error
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
	f.record(target.Events())
	target.ClearEvents()
	return nil
}

func (f *fakeTransactions) FindByID(_ context.Context, id transaction.TransactionID) (*transaction.Transaction, error) {
	found, ok := f.stored[id.String()]
	if !ok {
		return nil, transaction.ErrNotFound
	}
	return found, nil
}

func (f *fakeTransactions) List(_ context.Context, criteria transaction.Criteria) ([]*transaction.Transaction, error) {
	if f.listErr != nil {
		return nil, f.listErr
	}

	found := make([]*transaction.Transaction, 0, len(f.stored))
	for _, existing := range f.stored {
		owner := existing.UserID()
		if !owner.Equals(criteria.UserID()) {
			continue
		}
		if accountID, ok := criteria.AccountID(); ok && !existing.AccountID().Equals(accountID) {
			continue
		}
		if from, ok := criteria.From(); ok && existing.OccurredAt().Before(from) {
			continue
		}
		if to, ok := criteria.To(); ok && existing.OccurredAt().After(to) {
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

type fakeCategories struct {
	stored  map[string]*category.Category
	saveErr error
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
	return nil
}

func (f *fakeCategories) FindByID(_ context.Context, id category.CategoryID) (*category.Category, error) {
	found, ok := f.stored[id.String()]
	if !ok {
		return nil, category.ErrNotFound
	}
	return found, nil
}

func (f *fakeCategories) ListByUser(_ context.Context, userID shared.UserID) ([]*category.Category, error) {
	found := make([]*category.Category, 0, len(f.stored))
	for _, existing := range f.stored {
		owner := existing.UserID()
		if owner.Equals(userID) {
			found = append(found, existing)
		}
	}
	sort.Slice(found, func(first, second int) bool { return found[first].Name() < found[second].Name() })
	return found, nil
}

// passThroughUnitOfWork runs the work in place: the HTTP edge tests care
// about translation, not about commit boundaries.
type passThroughUnitOfWork struct{}

func (passThroughUnitOfWork) Run(ctx context.Context, work func(ctx context.Context) error) error {
	return work(ctx)
}

type fakeProvider struct {
	name         string
	transactions []openfinance.ProviderTransaction
	fetchErr     error
	balance      *openfinance.ProviderBalance
}

func (f *fakeProvider) Name() string { return f.name }

func (f *fakeProvider) FetchTransactions(_ context.Context, _ string, _ time.Time) ([]openfinance.ProviderTransaction, error) {
	if f.fetchErr != nil {
		return nil, f.fetchErr
	}
	return f.transactions, nil
}

func (f *fakeProvider) FetchBalance(_ context.Context, _ string) (openfinance.ProviderBalance, error) {
	if f.balance != nil {
		return *f.balance, nil
	}
	return openfinance.ProviderBalance{AmountInCents: 0, CurrencyCode: "BRL", AsOf: time.Now()}, nil
}

// fakePinger tests /healthz without Postgres, including the failure path,
// which is what really matters in a health check.
type fakePinger struct{ err error }

func (f *fakePinger) PingContext(_ context.Context) error { return f.err }

type testServer struct {
	router       *gin.Engine
	accounts     *fakeAccounts
	transactions *fakeTransactions
	categories   *fakeCategories
	provider     *fakeProvider
	pinger       *fakePinger
}

func newTestServer(t *testing.T, options ...func(*testServer)) *testServer {
	t.Helper()

	server := &testServer{
		accounts:     newFakeAccounts(),
		transactions: newFakeTransactions(),
		categories:   newFakeCategories(),
		provider:     &fakeProvider{name: "mock"},
		pinger:       &fakePinger{},
	}
	for _, option := range options {
		option(server)
	}

	logger := slog.New(slog.NewTextHandler(io.Discard, nil))

	router := gin.New()
	ginhandler.RegisterRoutes(
		router,
		server.pinger,
		stubAuthenticate(),
		ginhandler.NewAccountHandler(
			application.NewCreateAccountUseCase(server.accounts),
			application.NewListAccountsUseCase(server.accounts),
			application.NewViewAccountUseCase(server.accounts),
			application.NewRenameAccountUseCase(server.accounts),
			logger,
		),
		ginhandler.NewTransactionHandler(
			application.NewRecordTransactionUseCase(server.transactions, server.accounts),
			application.NewCategorizeTransactionUseCase(server.transactions, server.categories),
			application.NewImportFromProviderUseCase(server.transactions, server.accounts, server.provider, passThroughUnitOfWork{}),
			application.NewListTransactionsUseCase(server.transactions),
			application.NewViewTransactionUseCase(server.transactions),
			logger,
		),
		ginhandler.NewCategoryHandler(
			application.NewCreateCategoryUseCase(server.categories),
			application.NewListCategoriesUseCase(server.categories),
			application.NewRenameCategoryUseCase(server.categories),
			application.NewMoveCategoryUseCase(server.categories),
			application.NewAddCategoryRuleUseCase(server.categories),
			application.NewRemoveCategoryRuleUseCase(server.categories),
			logger,
		),
	)
	server.router = router

	return server
}

// stubAuthenticate replaces the Identity middleware with one that reads the
// user straight from the X-User-Id header. These Financial Tracking tests only
// need "resolved or not"; the real middleware would force every test to forge
// a Google token, coupling them to a credential mechanism owned by another
// bounded context. The stub keeps the real contract: it puts a uuid in the
// request context, or answers 401.
func stubAuthenticate() gin.HandlerFunc {
	return func(ginContext *gin.Context) {
		raw := ginContext.GetHeader("X-User-Id")
		if raw == "" {
			ginContext.AbortWithStatusJSON(http.StatusUnauthorized, httperror.Body{
				Error: httperror.Detail{Code: "invalid_token", Message: "missing credential"},
			})
			return
		}
		userID, err := uuid.Parse(raw)
		if err != nil {
			ginContext.AbortWithStatusJSON(http.StatusUnauthorized, httperror.Body{
				Error: httperror.Detail{Code: "invalid_token", Message: "malformed credential"},
			})
			return
		}

		ginContext.Request = ginContext.Request.WithContext(
			ginmiddleware.ContextWithUserID(ginContext.Request.Context(), userID))
		ginContext.Next()
	}
}

// request sends a request to the in-memory router, without a socket. A raw
// string body is sent as is, so tests can send malformed JSON that
// json.Marshal would never produce.
func (s *testServer) request(t *testing.T, method string, path string, userID string, body any) *httptest.ResponseRecorder {
	t.Helper()

	var payload io.Reader
	if body != nil {
		if raw, ok := body.(string); ok {
			payload = bytes.NewBufferString(raw)
		} else {
			encoded, err := json.Marshal(body)
			require.NoError(t, err)
			payload = bytes.NewBuffer(encoded)
		}
	}

	request := httptest.NewRequest(method, path, payload)
	request.Header.Set("Content-Type", "application/json")
	if userID != "" {
		request.Header.Set("X-User-Id", userID)
	}

	recorder := httptest.NewRecorder()
	s.router.ServeHTTP(recorder, request)
	return recorder
}

func decode(t *testing.T, recorder *httptest.ResponseRecorder) map[string]any {
	t.Helper()

	var body map[string]any
	require.NoError(t, json.Unmarshal(recorder.Body.Bytes(), &body), "body is not JSON: %s", recorder.Body.String())
	return body
}

// errorCode extracts the stable code from the error body: it is the contract
// the client consumes, so tests assert it instead of the human message.
func errorCode(t *testing.T, recorder *httptest.ResponseRecorder) string {
	t.Helper()

	body := decode(t, recorder)
	detail, ok := body["error"].(map[string]any)
	require.True(t, ok, "response without an error object: %s", recorder.Body.String())
	code, ok := detail["code"].(string)
	require.True(t, ok, "error without a code: %s", recorder.Body.String())
	return code
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

func newManualAccount(t *testing.T, ownerID uuid.UUID) *account.Account {
	t.Helper()
	created, err := account.New(mustUserID(t, ownerID), "Conta Corrente", account.KindChecking, mustCurrency(t, "BRL"), account.NewManualSource())
	require.NoError(t, err)
	return created
}

func newConnectedAccount(t *testing.T, ownerID uuid.UUID, providerName string) *account.Account {
	t.Helper()
	source, err := account.NewOpenFinanceSource(providerName, "provider-acc-1")
	require.NoError(t, err)
	created, err := account.New(mustUserID(t, ownerID), "Conta Conectada", account.KindChecking, mustCurrency(t, "BRL"), source)
	require.NoError(t, err)
	return created
}

// newTransactionAt pins the instant so ordering and period tests do not
// depend on the clock.
func newTransactionAt(t *testing.T, ownerID uuid.UUID, accountID account.AccountID, amount int64, occurredAt time.Time) *transaction.Transaction {
	t.Helper()
	money, err := shared.NewMoney(amount, mustCurrency(t, "BRL"))
	require.NoError(t, err)
	created, err := transaction.NewManual(mustUserID(t, ownerID), accountID, money, occurredAt, "Groceries")
	require.NoError(t, err)
	return created
}

func newCategoryFor(t *testing.T, ownerID uuid.UUID) *category.Category {
	t.Helper()
	created, err := category.New(mustUserID(t, ownerID), "Alimentação", nil)
	require.NoError(t, err)
	return created
}

func newTransactionFor(t *testing.T, ownerID uuid.UUID, accountID account.AccountID) *transaction.Transaction {
	t.Helper()
	money, err := shared.NewMoney(-4550, mustCurrency(t, "BRL"))
	require.NoError(t, err)
	created, err := transaction.NewManual(mustUserID(t, ownerID), accountID, money, time.Now().Add(-time.Hour), "Bakery")
	require.NoError(t, err)
	return created
}
