package ginhandler_test

import (
	"fmt"
	"net/http"
	"testing"
	"time"

	"github.com/google/uuid"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/luigimenezes13/financial-manager/internal/financialtracking/domain/account"
	"github.com/luigimenezes13/financial-manager/internal/financialtracking/domain/openfinance"
	"github.com/luigimenezes13/financial-manager/internal/financialtracking/domain/transaction"
)

// TestRecordTransaction cobre o caminho feliz e a decisão de a transação
// HERDAR a moeda da conta — o corpo não tem campo de moeda e a resposta
// mesmo assim traz "BRL".
func TestRecordTransaction(t *testing.T) {
	ownerID := uuid.New()
	targetAccount := newManualAccount(t, ownerID)
	accountID := targetAccount.ID()

	server := newTestServer(t, func(server *testServer) {
		server.accounts = newFakeAccounts(targetAccount)
	})

	recorder := server.request(t, http.MethodPost, "/v1/transactions", ownerID.String(), map[string]any{
		"account_id":  accountID.String(),
		"amount":      -4550,
		"occurred_at": "2026-08-20T10:00:00Z",
		"description": "  Padaria  ",
	})

	require.Equal(t, http.StatusCreated, recorder.Code, recorder.Body.String())
	body := decode(t, recorder)
	assert.Equal(t, float64(-4550), body["amount"])
	assert.Equal(t, "BRL", body["currency"], "a moeda vem da conta, não do corpo")
	assert.Equal(t, "Padaria", body["description"])
	assert.Equal(t, accountID.String(), body["account_id"])
	assert.Len(t, server.transactions.stored, 1)
}

// TestRecordTransactionRecusa cobre autorização, invariantes e sintaxe no
// mesmo endpoint.
func TestRecordTransactionRecusa(t *testing.T) {
	ownerID := uuid.New()
	otherUserID := uuid.New()

	cases := []struct {
		name       string
		userID     uuid.UUID
		mutateBody func(body map[string]any)
		wantStatus int
		wantCode   string
	}{
		{
			name:       "valor zero",
			userID:     ownerID,
			mutateBody: func(body map[string]any) { body["amount"] = 0 },
			wantStatus: http.StatusUnprocessableEntity,
			wantCode:   "zero_amount",
		},
		{
			name:       "conta de outro usuário",
			userID:     otherUserID,
			mutateBody: func(map[string]any) {},
			wantStatus: http.StatusForbidden,
			wantCode:   "forbidden",
		},
		{
			name:       "conta inexistente",
			userID:     ownerID,
			mutateBody: func(body map[string]any) { body["account_id"] = uuid.New().String() },
			wantStatus: http.StatusNotFound,
			wantCode:   "account_not_found",
		},
		{
			name:       "account_id não é uuid",
			userID:     ownerID,
			mutateBody: func(body map[string]any) { body["account_id"] = "abc" },
			wantStatus: http.StatusBadRequest,
			wantCode:   "bad_request",
		},
		{
			name:       "data no futuro",
			userID:     ownerID,
			mutateBody: func(body map[string]any) { body["occurred_at"] = "2099-01-01T10:00:00Z" },
			wantStatus: http.StatusUnprocessableEntity,
			wantCode:   "invalid_occurred_at",
		},
		{
			name:       "descrição em branco",
			userID:     ownerID,
			mutateBody: func(body map[string]any) { body["description"] = "   " },
			wantStatus: http.StatusUnprocessableEntity,
			wantCode:   "invalid_description",
		},
		{
			name:       "occurred_at ausente",
			userID:     ownerID,
			mutateBody: func(body map[string]any) { delete(body, "occurred_at") },
			wantStatus: http.StatusBadRequest,
			wantCode:   "bad_request",
		},
	}

	for _, testCase := range cases {
		t.Run(testCase.name, func(t *testing.T) {
			targetAccount := newManualAccount(t, ownerID)
			accountID := targetAccount.ID()
			server := newTestServer(t, func(server *testServer) {
				server.accounts = newFakeAccounts(targetAccount)
			})

			body := map[string]any{
				"account_id":  accountID.String(),
				"amount":      -1000,
				"occurred_at": "2026-08-20T10:00:00Z",
				"description": "Mercado",
			}
			testCase.mutateBody(body)

			recorder := server.request(t, http.MethodPost, "/v1/transactions", testCase.userID.String(), body)

			require.Equal(t, testCase.wantStatus, recorder.Code, recorder.Body.String())
			assert.Equal(t, testCase.wantCode, errorCode(t, recorder))
			assert.Empty(t, server.transactions.stored)
		})
	}
}

// TestCategorizeTransaction cobre o 200 (não 201 — muta recurso existente),
// o default de assigned_by e o DESPACHO do evento.
func TestCategorizeTransaction(t *testing.T) {
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

	path := fmt.Sprintf("/v1/transactions/%s/category", transactionID.String())
	recorder := server.request(t, http.MethodPut, path, ownerID.String(), map[string]any{
		"category_id": categoryID.String(),
	})

	require.Equal(t, http.StatusOK, recorder.Code, recorder.Body.String())
	body := decode(t, recorder)
	assert.Equal(t, transactionID.String(), body["transaction_id"])
	assert.Equal(t, categoryID.String(), body["category_id"])
	assert.Equal(t, "user", body["assigned_by"], "assigned_by ausente assume 'user'")
	assert.NotEmpty(t, body["assigned_at"])

	require.Len(t, server.dispatcher.dispatched, 1, "categorizar publica o evento")
	assert.Equal(t, transaction.EventTypeCategorized, server.dispatcher.dispatched[0].EventName())
}

// TestCategorizeTransactionRecusa cobre os dois donos que precisam bater e
// o vocabulário de assigned_by.
func TestCategorizeTransactionRecusa(t *testing.T) {
	ownerID := uuid.New()
	otherUserID := uuid.New()

	cases := []struct {
		name       string
		userID     uuid.UUID
		path       func(transactionID string) string
		body       any
		wantStatus int
		wantCode   string
	}{
		{
			name:       "transação de outro usuário",
			userID:     otherUserID,
			wantStatus: http.StatusForbidden,
			wantCode:   "forbidden",
		},
		{
			name:       "assigned_by fora do vocabulário",
			userID:     ownerID,
			body:       map[string]any{"assigned_by": "robot"},
			wantStatus: http.StatusUnprocessableEntity,
			wantCode:   "invalid_category_assignment",
		},
		{
			name:       "categoria inexistente",
			userID:     ownerID,
			body:       map[string]any{"category_id": uuid.New().String()},
			wantStatus: http.StatusNotFound,
			wantCode:   "category_not_found",
		},
		{
			name:       "id da transação não é uuid",
			userID:     ownerID,
			path:       func(string) string { return "/v1/transactions/abc/category" },
			wantStatus: http.StatusBadRequest,
			wantCode:   "bad_request",
		},
		{
			name:       "category_id ausente",
			userID:     ownerID,
			body:       map[string]any{"category_id": ""},
			wantStatus: http.StatusBadRequest,
			wantCode:   "bad_request",
		},
	}

	for _, testCase := range cases {
		t.Run(testCase.name, func(t *testing.T) {
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

			body := map[string]any{"category_id": categoryID.String()}
			if override, ok := testCase.body.(map[string]any); ok {
				for key, value := range override {
					body[key] = value
				}
			}

			path := fmt.Sprintf("/v1/transactions/%s/category", transactionID.String())
			if testCase.path != nil {
				path = testCase.path(transactionID.String())
			}

			recorder := server.request(t, http.MethodPut, path, testCase.userID.String(), body)

			require.Equal(t, testCase.wantStatus, recorder.Code, recorder.Body.String())
			assert.Equal(t, testCase.wantCode, errorCode(t, recorder))
			assert.Empty(t, server.dispatcher.dispatched, "erro não publica evento")
		})
	}
}

// TestImportFromProvider cobre a importação de conta conectada, incluindo o
// imported_count que a borda calcula pra UI não ter que contar array.
func TestImportFromProvider(t *testing.T) {
	ownerID := uuid.New()
	connected := newConnectedAccount(t, ownerID, "mock")
	accountID := connected.ID()

	server := newTestServer(t, func(server *testServer) {
		server.accounts = newFakeAccounts(connected)
		server.provider = &fakeProvider{
			name: "mock",
			transactions: []openfinance.ProviderTransaction{
				{ProviderTransactionID: "tx-1", Description: "Uber", AmountInCents: -2350, CurrencyCode: "BRL", OccurredAt: time.Now().Add(-24 * time.Hour)},
				{ProviderTransactionID: "tx-2", Description: "Salário", AmountInCents: 350000, CurrencyCode: "BRL", OccurredAt: time.Now().Add(-48 * time.Hour)},
			},
		}
	})

	recorder := server.request(t, http.MethodPost, "/v1/transactions/import", ownerID.String(), map[string]any{
		"account_id": accountID.String(),
	})

	require.Equal(t, http.StatusCreated, recorder.Code, recorder.Body.String())
	body := decode(t, recorder)
	assert.Equal(t, "mock", body["provider"])
	assert.Equal(t, float64(2), body["imported_count"])
	assert.Len(t, body["transaction_ids"], 2)
	assert.Len(t, server.transactions.stored, 2)

	require.Len(t, server.dispatcher.dispatched, 3, "2 Imported + 1 BalanceUpdated")
	assert.Equal(t, transaction.EventTypeImported, server.dispatcher.dispatched[0].EventName())
	assert.Equal(t, account.EventTypeBalanceUpdated, server.dispatcher.dispatched[2].EventName())
}

// TestImportFromProviderRecusa cobre as recusas próprias do import,
// incluindo a tradução de falha do PROVIDER para 5xx — é do provider, não do
// cliente, então tentar de novo mais tarde é a ação certa.
func TestImportFromProviderRecusa(t *testing.T) {
	ownerID := uuid.New()

	t.Run("conta manual não importa", func(t *testing.T) {
		manual := newManualAccount(t, ownerID)
		accountID := manual.ID()
		server := newTestServer(t, func(server *testServer) {
			server.accounts = newFakeAccounts(manual)
		})

		recorder := server.request(t, http.MethodPost, "/v1/transactions/import", ownerID.String(), map[string]any{
			"account_id": accountID.String(),
		})

		require.Equal(t, http.StatusUnprocessableEntity, recorder.Code)
		assert.Equal(t, "account_not_connected", errorCode(t, recorder))
	})

	t.Run("conta de outro provider", func(t *testing.T) {
		connected := newConnectedAccount(t, ownerID, "pluggy")
		accountID := connected.ID()
		server := newTestServer(t, func(server *testServer) {
			server.accounts = newFakeAccounts(connected)
			server.provider = &fakeProvider{name: "mock"}
		})

		recorder := server.request(t, http.MethodPost, "/v1/transactions/import", ownerID.String(), map[string]any{
			"account_id": accountID.String(),
		})

		require.Equal(t, http.StatusUnprocessableEntity, recorder.Code)
		assert.Equal(t, "provider_mismatch", errorCode(t, recorder))
	})

	t.Run("provider indisponível vira 502", func(t *testing.T) {
		connected := newConnectedAccount(t, ownerID, "mock")
		accountID := connected.ID()
		server := newTestServer(t, func(server *testServer) {
			server.accounts = newFakeAccounts(connected)
			server.provider = &fakeProvider{name: "mock", fetchErr: openfinance.ErrProviderUnavailable}
		})

		recorder := server.request(t, http.MethodPost, "/v1/transactions/import", ownerID.String(), map[string]any{
			"account_id": accountID.String(),
		})

		require.Equal(t, http.StatusBadGateway, recorder.Code)
		assert.Equal(t, "provider_unavailable", errorCode(t, recorder))
	})
}

// TestImportFromProviderComSince prova que a janela chega ao provider: o
// campo é opcional, e ausente significa "todo o histórico".
func TestImportFromProviderComSince(t *testing.T) {
	ownerID := uuid.New()
	connected := newConnectedAccount(t, ownerID, "mock")
	accountID := connected.ID()

	server := newTestServer(t, func(server *testServer) {
		server.accounts = newFakeAccounts(connected)
		server.provider = &fakeProvider{
			name: "mock",
			transactions: []openfinance.ProviderTransaction{
				{ProviderTransactionID: "tx-1", Description: "Uber", AmountInCents: -2350, CurrencyCode: "BRL", OccurredAt: time.Now().Add(-24 * time.Hour)},
			},
		}
	})

	recorder := server.request(t, http.MethodPost, "/v1/transactions/import", ownerID.String(), map[string]any{
		"account_id": accountID.String(),
		"since":      "2026-08-01T00:00:00Z",
	})

	require.Equal(t, http.StatusCreated, recorder.Code, recorder.Body.String())
	body := decode(t, recorder)
	assert.Equal(t, float64(1), body["imported_count"])
}

// TestImportFromProviderSintaxe cobre as recusas de FORMA (400), que não
// passam pela tabela de tradução de domínio.
func TestImportFromProviderSintaxe(t *testing.T) {
	cases := []struct {
		name string
		body any
	}{
		{name: "account_id não é uuid", body: map[string]any{"account_id": "abc"}},
		{name: "account_id ausente", body: map[string]any{}},
		{name: "since em formato inválido", body: map[string]any{"account_id": uuid.New().String(), "since": "ontem"}},
		{name: "JSON malformado", body: `{"account_id":`},
	}

	for _, testCase := range cases {
		t.Run(testCase.name, func(t *testing.T) {
			server := newTestServer(t)

			recorder := server.request(t, http.MethodPost, "/v1/transactions/import", uuid.New().String(), testCase.body)

			require.Equal(t, http.StatusBadRequest, recorder.Code, recorder.Body.String())
			assert.Equal(t, "bad_request", errorCode(t, recorder))
			assert.Empty(t, server.transactions.stored)
		})
	}
}

// TestSaldoSoMudaPorImportacao é o teste AUTOMÁTICO da regra, na borda HTTP:
// nenhuma quantidade de lançamentos manuais move o saldo, e a importação
// move. É o mesmo contrato do teste de regressão da application, verificado
// aqui pelo caminho que o cliente realmente usa.
func TestSaldoSoMudaPorImportacao(t *testing.T) {
	ownerID := uuid.New()
	connected := newConnectedAccount(t, ownerID, "mock")
	accountID := connected.ID()

	server := newTestServer(t, func(server *testServer) {
		server.accounts = newFakeAccounts(connected)
		server.provider = &fakeProvider{
			name: "mock",
			transactions: []openfinance.ProviderTransaction{
				{ProviderTransactionID: "tx-1", Description: "Uber", AmountInCents: -2350, CurrencyCode: "BRL", OccurredAt: time.Now().Add(-24 * time.Hour)},
			},
			balance: &openfinance.ProviderBalance{AmountInCents: 7_777_00, CurrencyCode: "BRL", AsOf: time.Now()},
		}
	})

	saldoAtual := func() int64 {
		balance := connected.Balance()
		money := balance.Money()
		return money.Amount()
	}

	require.Zero(t, saldoAtual(), "conta nasce zerada")

	// Três lançamentos manuais pela API.
	for _, amount := range []int64{-45_50, -120_00, 300_00} {
		recorder := server.request(t, http.MethodPost, "/v1/transactions", ownerID.String(), map[string]any{
			"account_id":  accountID.String(),
			"amount":      amount,
			"occurred_at": "2026-08-20T10:00:00Z",
			"description": "Lançamento",
		})
		require.Equal(t, http.StatusCreated, recorder.Code, recorder.Body.String())
	}

	require.Len(t, server.transactions.stored, 3)
	assert.Zero(t, saldoAtual(), "lançamento manual NÃO move o saldo")

	// A importação move.
	recorder := server.request(t, http.MethodPost, "/v1/transactions/import", ownerID.String(), map[string]any{
		"account_id": accountID.String(),
	})

	require.Equal(t, http.StatusCreated, recorder.Code, recorder.Body.String())
	body := decode(t, recorder)
	assert.Equal(t, float64(7_777_00), body["balance"])
	assert.Equal(t, "BRL", body["currency"])
	assert.Equal(t, true, body["balance_applied"])
	assert.Equal(t, int64(7_777_00), saldoAtual(), "o saldo é o que o provedor informou, não a soma dos lançamentos")
}
