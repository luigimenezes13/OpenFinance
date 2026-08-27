// Package ginhandler_test testa a borda HTTP de ponta a ponta DENTRO do
// processo: httptest + gin real + middleware real + use cases REAIS, só com
// os repositórios e o provider substituídos por fakes em memória.
//
// Por que use case real e não mock do use case: o que estes testes precisam
// provar é a tradução das duas pontas — request JSON → Input, e sentinel de
// domínio → status HTTP. Com use case mockado, o teste passaria mesmo se o
// mapeamento de erro estivesse todo errado, porque o erro nunca nasceria no
// domínio.
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

// errInfra simula falha de infraestrutura, pra exercitar o 500.
var errInfra = errors.New("fake: infrastructure failure")

// TestMain silencia o gin: em TestMode ele não imprime as rotas nem loga
// cada request, e o teste fica legível.
func TestMain(main *testing.M) {
	gin.SetMode(gin.TestMode)
	main.Run()
}

// --- Fakes das portas ----------------------------------------------------

type fakeAccounts struct {
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

type fakeTransactions struct {
	stored  map[string]*transaction.Transaction
	saveErr error
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
	return nil
}

func (f *fakeTransactions) FindByID(_ context.Context, id transaction.TransactionID) (*transaction.Transaction, error) {
	found, ok := f.stored[id.String()]
	if !ok {
		return nil, transaction.ErrNotFound
	}
	return found, nil
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

type fakeDispatcher struct{ dispatched []events.Event }

func (f *fakeDispatcher) Register(_ string, _ events.Handler) {}

func (f *fakeDispatcher) Dispatch(_ context.Context, dispatched ...events.Event) error {
	f.dispatched = append(f.dispatched, dispatched...)
	return nil
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

// fakePinger permite testar /healthz sem Postgres — inclusive o caminho de
// falha, que é o que realmente importa num health check.
type fakePinger struct{ err error }

func (f *fakePinger) PingContext(_ context.Context) error { return f.err }

// --- Montagem do servidor de teste --------------------------------------

// testServer é o alvo dos testes: rotas reais, middleware real, use cases
// reais, fakes só nas portas.
type testServer struct {
	router       *gin.Engine
	accounts     *fakeAccounts
	transactions *fakeTransactions
	categories   *fakeCategories
	dispatcher   *fakeDispatcher
	provider     *fakeProvider
	pinger       *fakePinger
}

func newTestServer(t *testing.T, options ...func(*testServer)) *testServer {
	t.Helper()

	server := &testServer{
		accounts:     newFakeAccounts(),
		transactions: newFakeTransactions(),
		categories:   newFakeCategories(),
		dispatcher:   &fakeDispatcher{},
		provider:     &fakeProvider{name: "mock"},
		pinger:       &fakePinger{},
	}
	for _, option := range options {
		option(server)
	}

	// Logger descartado: o handler loga o 500 com o erro completo, e isso
	// poluiria a saída do teste sem provar nada.
	logger := slog.New(slog.NewTextHandler(io.Discard, nil))

	router := gin.New()
	ginhandler.RegisterRoutes(
		router,
		server.pinger,
		stubAuthenticate(),
		ginhandler.NewAccountHandler(application.NewCreateAccountUseCase(server.accounts), logger),
		ginhandler.NewTransactionHandler(
			application.NewRecordTransactionUseCase(server.transactions, server.accounts),
			application.NewCategorizeTransactionUseCase(server.transactions, server.categories, server.dispatcher),
			application.NewImportFromProviderUseCase(server.transactions, server.accounts, server.provider, server.dispatcher),
			logger,
		),
		ginhandler.NewCategoryHandler(application.NewCreateCategoryUseCase(server.categories), logger),
	)
	server.router = router

	return server
}

// stubAuthenticate substitui o middleware do BC Identity por um que lê o
// usuário direto do header X-User-Id.
//
// Por que stub e não o middleware real: estes testes são do Financial
// Tracking, e o que eles precisam saber sobre autenticação é apenas
// "resolvido ou não". Usar o middleware real obrigaria cada teste daqui a
// forjar um token do Google — acoplando os testes de conta e transação ao
// mecanismo de credencial, que é assunto de outro bounded context e tem
// testes próprios (identity/adapter/ginmiddleware).
//
// O contrato que este stub imita é o mesmo do real: injeta um uuid no
// context da request, ou responde 401.
func stubAuthenticate() gin.HandlerFunc {
	return func(ginContext *gin.Context) {
		raw := ginContext.GetHeader("X-User-Id")
		if raw == "" {
			ginContext.AbortWithStatusJSON(http.StatusUnauthorized, httperror.Body{
				Error: httperror.Detail{Code: "invalid_token", Message: "credencial ausente"},
			})
			return
		}
		userID, err := uuid.Parse(raw)
		if err != nil {
			ginContext.AbortWithStatusJSON(http.StatusUnauthorized, httperror.Body{
				Error: httperror.Detail{Code: "invalid_token", Message: "credencial mal formada"},
			})
			return
		}

		ginContext.Request = ginContext.Request.WithContext(
			ginmiddleware.ContextWithUserID(ginContext.Request.Context(), userID))
		ginContext.Next()
	}
}

// request dispara uma requisição contra o router em memória (sem socket) e
// devolve a resposta gravada.
func (s *testServer) request(t *testing.T, method string, path string, userID string, body any) *httptest.ResponseRecorder {
	t.Helper()

	var payload io.Reader
	if body != nil {
		if raw, ok := body.(string); ok {
			// String crua permite testar JSON malformado, que um
			// json.Marshal jamais produziria.
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

// decode lê o corpo JSON da resposta.
func decode(t *testing.T, recorder *httptest.ResponseRecorder) map[string]any {
	t.Helper()

	var body map[string]any
	require.NoError(t, json.Unmarshal(recorder.Body.Bytes(), &body), "corpo não é JSON: %s", recorder.Body.String())
	return body
}

// errorCode extrai o código estável do corpo de erro — é o contrato que o
// cliente consome, e por isso o que os testes afirmam (não a mensagem, que
// é pra humano e pode mudar).
func errorCode(t *testing.T, recorder *httptest.ResponseRecorder) string {
	t.Helper()

	body := decode(t, recorder)
	detail, ok := body["error"].(map[string]any)
	require.True(t, ok, "resposta sem objeto error: %s", recorder.Body.String())
	code, ok := detail["code"].(string)
	require.True(t, ok, "erro sem code: %s", recorder.Body.String())
	return code
}

// --- Builders de aggregate ----------------------------------------------

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
	created, err := transaction.NewManual(mustUserID(t, ownerID), accountID, money, time.Now().Add(-time.Hour), "Padaria")
	require.NoError(t, err)
	return created
}
