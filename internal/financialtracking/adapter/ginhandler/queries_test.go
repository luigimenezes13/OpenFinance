package ginhandler_test

import (
	"fmt"
	"net/http"
	"testing"
	"time"

	"github.com/google/uuid"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

// TestListAccounts cobre o envelope, a projeção e o isolamento por usuário.
func TestListAccounts(t *testing.T) {
	ownerID := uuid.New()
	manual := newManualAccount(t, ownerID)
	connected := newConnectedAccount(t, ownerID, "mock")
	fromOther := newManualAccount(t, uuid.New())

	server := newTestServer(t, func(server *testServer) {
		server.accounts = newFakeAccounts(manual, connected, fromOther)
	})

	recorder := server.request(t, http.MethodGet, "/v1/accounts", ownerID.String(), nil)

	require.Equal(t, http.StatusOK, recorder.Code, recorder.Body.String())
	body := decode(t, recorder)

	accounts, ok := body["accounts"].([]any)
	require.True(t, ok, "resposta vem em envelope, não array na raiz: %s", recorder.Body.String())
	require.Len(t, accounts, 2, "conta de outro usuário não aparece")

	first, ok := accounts[0].(map[string]any)
	require.True(t, ok)
	assert.Equal(t, "Conta Conectada", first["name"])
	assert.Equal(t, "mock", first["provider"], "conta conectada informa o provedor")
	assert.NotEmpty(t, first["balance_as_of"])

	second := accounts[1].(map[string]any)
	assert.Nil(t, second["provider"], "conta manual sai com provider null")
}

// TestGetAccount cobre detalhe, dono errado e id inválido.
func TestGetAccount(t *testing.T) {
	ownerID := uuid.New()
	target := newManualAccount(t, ownerID)
	accountID := target.ID()

	server := newTestServer(t, func(server *testServer) {
		server.accounts = newFakeAccounts(target)
	})

	t.Run("caminho feliz", func(t *testing.T) {
		recorder := server.request(t, http.MethodGet, "/v1/accounts/"+accountID.String(), ownerID.String(), nil)

		require.Equal(t, http.StatusOK, recorder.Code, recorder.Body.String())
		body := decode(t, recorder)
		assert.Equal(t, accountID.String(), body["id"])
		assert.Equal(t, "Conta Corrente", body["name"])
	})

	t.Run("conta de outro usuário responde 403", func(t *testing.T) {
		recorder := server.request(t, http.MethodGet, "/v1/accounts/"+accountID.String(), uuid.New().String(), nil)

		require.Equal(t, http.StatusForbidden, recorder.Code)
		assert.Equal(t, "forbidden", errorCode(t, recorder))
	})

	t.Run("conta inexistente responde 404", func(t *testing.T) {
		recorder := server.request(t, http.MethodGet, "/v1/accounts/"+uuid.New().String(), ownerID.String(), nil)

		require.Equal(t, http.StatusNotFound, recorder.Code)
		assert.Equal(t, "account_not_found", errorCode(t, recorder))
	})

	t.Run("id mal formado responde 400", func(t *testing.T) {
		recorder := server.request(t, http.MethodGet, "/v1/accounts/abc", ownerID.String(), nil)

		require.Equal(t, http.StatusBadRequest, recorder.Code)
	})
}

// TestListCategories cobre a projeção com regras dentro.
func TestListCategories(t *testing.T) {
	ownerID := uuid.New()
	target := newCategoryFor(t, ownerID)

	server := newTestServer(t, func(server *testServer) {
		server.categories = newFakeCategories(target)
	})

	recorder := server.request(t, http.MethodGet, "/v1/categories", ownerID.String(), nil)

	require.Equal(t, http.StatusOK, recorder.Code, recorder.Body.String())
	body := decode(t, recorder)
	categories := body["categories"].([]any)
	require.Len(t, categories, 1)

	first := categories[0].(map[string]any)
	assert.Equal(t, "Alimentação", first["name"])
	assert.Nil(t, first["parent_id"])
	assert.NotNil(t, first["rules"], "rules sai como array vazio, não null: o cliente itera sem checar")
}

// TestListTransactions cobre extrato, ordem, janela e filtros por query
// string.
func TestListTransactions(t *testing.T) {
	ownerID := uuid.New()
	targetAccount := newManualAccount(t, ownerID)
	accountID := targetAccount.ID()

	older := newTransactionAt(t, ownerID, accountID, -100, time.Now().Add(-72*time.Hour))
	middle := newTransactionAt(t, ownerID, accountID, -200, time.Now().Add(-48*time.Hour))
	newest := newTransactionAt(t, ownerID, accountID, -300, time.Now().Add(-24*time.Hour))

	server := newTestServer(t, func(server *testServer) {
		server.accounts = newFakeAccounts(targetAccount)
		server.transactions = newFakeTransactions(older, middle, newest)
	})

	t.Run("ordem e janela default", func(t *testing.T) {
		recorder := server.request(t, http.MethodGet, "/v1/transactions", ownerID.String(), nil)

		require.Equal(t, http.StatusOK, recorder.Code, recorder.Body.String())
		body := decode(t, recorder)
		transactions := body["transactions"].([]any)
		require.Len(t, transactions, 3)

		first := transactions[0].(map[string]any)
		assert.Equal(t, float64(-300), first["amount"], "extrato começa pela mais recente")
		assert.Equal(t, float64(50), body["limit"], "o default aplicado volta na resposta")
		assert.Equal(t, float64(0), body["offset"])
		assert.Nil(t, first["category_id"], "não categorizada sai com o bloco null")
		assert.Nil(t, first["provider"], "lançamento manual sai com provider null")
	})

	t.Run("paginação", func(t *testing.T) {
		recorder := server.request(t, http.MethodGet, "/v1/transactions?limit=2&offset=2", ownerID.String(), nil)

		require.Equal(t, http.StatusOK, recorder.Code)
		body := decode(t, recorder)
		assert.Len(t, body["transactions"].([]any), 1)
		assert.Equal(t, float64(2), body["limit"])
		assert.Equal(t, float64(2), body["offset"])
	})

	t.Run("filtro por período", func(t *testing.T) {
		from := time.Now().Add(-50 * time.Hour).UTC().Format(time.RFC3339)
		path := fmt.Sprintf("/v1/transactions?from=%s", from)

		recorder := server.request(t, http.MethodGet, path, ownerID.String(), nil)

		require.Equal(t, http.StatusOK, recorder.Code, recorder.Body.String())
		body := decode(t, recorder)
		assert.Len(t, body["transactions"].([]any), 2, "a de 72h atrás fica fora")
	})

	t.Run("filtro por conta", func(t *testing.T) {
		path := "/v1/transactions?account_id=" + accountID.String()

		recorder := server.request(t, http.MethodGet, path, ownerID.String(), nil)

		require.Equal(t, http.StatusOK, recorder.Code)
		body := decode(t, recorder)
		assert.Len(t, body["transactions"].([]any), 3)
	})
}

// TestListTransactionsQueryInvalida cobre a fronteira 400 (sintaxe da query)
// vs 422 (regra do domínio) — a mesma distinção do corpo, agora na query
// string.
func TestListTransactionsQueryInvalida(t *testing.T) {
	ownerID := uuid.New()

	cases := []struct {
		name       string
		query      string
		wantStatus int
		wantCode   string
	}{
		{name: "account_id não é uuid", query: "?account_id=abc", wantStatus: http.StatusBadRequest, wantCode: "bad_request"},
		{name: "from fora do RFC 3339", query: "?from=ontem", wantStatus: http.StatusBadRequest, wantCode: "bad_request"},
		{name: "limit não é número", query: "?limit=muitos", wantStatus: http.StatusBadRequest, wantCode: "bad_request"},
		{name: "limit acima do teto", query: "?limit=201", wantStatus: http.StatusUnprocessableEntity, wantCode: "invalid_page"},
		{name: "offset negativo", query: "?offset=-1", wantStatus: http.StatusUnprocessableEntity, wantCode: "invalid_page"},
		{
			name:       "período invertido",
			query:      "?from=2026-08-10T00:00:00Z&to=2026-08-01T00:00:00Z",
			wantStatus: http.StatusUnprocessableEntity,
			wantCode:   "invalid_period",
		},
	}

	for _, testCase := range cases {
		t.Run(testCase.name, func(t *testing.T) {
			server := newTestServer(t)

			recorder := server.request(t, http.MethodGet, "/v1/transactions"+testCase.query, ownerID.String(), nil)

			require.Equal(t, testCase.wantStatus, recorder.Code, recorder.Body.String())
			assert.Equal(t, testCase.wantCode, errorCode(t, recorder))
		})
	}
}

// TestGetTransaction cobre o detalhe com categoria atribuída.
func TestGetTransaction(t *testing.T) {
	ownerID := uuid.New()
	targetAccount := newManualAccount(t, ownerID)
	accountID := targetAccount.ID()
	target := newTransactionFor(t, ownerID, accountID)
	targetCategory := newCategoryFor(t, ownerID)
	transactionID := target.ID()
	categoryID := targetCategory.ID()

	server := newTestServer(t, func(server *testServer) {
		server.accounts = newFakeAccounts(targetAccount)
		server.transactions = newFakeTransactions(target)
		server.categories = newFakeCategories(targetCategory)
	})

	// Categoriza pela API, e depois lê o detalhe.
	categorize := server.request(t, http.MethodPut,
		fmt.Sprintf("/v1/transactions/%s/category", transactionID.String()),
		ownerID.String(), map[string]any{"category_id": categoryID.String()})
	require.Equal(t, http.StatusOK, categorize.Code)

	recorder := server.request(t, http.MethodGet, "/v1/transactions/"+transactionID.String(), ownerID.String(), nil)

	require.Equal(t, http.StatusOK, recorder.Code, recorder.Body.String())
	body := decode(t, recorder)
	assert.Equal(t, transactionID.String(), body["id"])
	assert.Equal(t, categoryID.String(), body["category_id"])
	assert.Equal(t, "user", body["assigned_by"])
	assert.NotEmpty(t, body["assigned_at"])
}
