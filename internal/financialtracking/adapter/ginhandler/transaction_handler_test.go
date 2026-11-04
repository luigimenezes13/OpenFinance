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

func TestRecordTransactionEndpoint(t *testing.T) {
	t.Run("should create the transaction inheriting the account currency, since the body has no currency field", func(t *testing.T) {
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
			"description": "  Bakery  ",
		})

		require.Equal(t, http.StatusCreated, recorder.Code, recorder.Body.String())
		body := decode(t, recorder)
		assert.Equal(t, float64(-4550), body["amount"])
		assert.Equal(t, "BRL", body["currency"], "the currency comes from the account, not from the body")
		assert.Equal(t, "Bakery", body["description"])
		assert.Equal(t, accountID.String(), body["account_id"])
		assert.Len(t, server.transactions.stored, 1)
	})
}

func TestRecordTransactionEndpointRefusal(t *testing.T) {
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
			name:       "should answer 422 for a zero amount",
			userID:     ownerID,
			mutateBody: func(body map[string]any) { body["amount"] = 0 },
			wantStatus: http.StatusUnprocessableEntity,
			wantCode:   "zero_amount",
		},
		{
			name:       "should answer 403 for an account of another user",
			userID:     otherUserID,
			mutateBody: func(map[string]any) {},
			wantStatus: http.StatusForbidden,
			wantCode:   "forbidden",
		},
		{
			name:       "should answer 404 for a missing account",
			userID:     ownerID,
			mutateBody: func(body map[string]any) { body["account_id"] = uuid.New().String() },
			wantStatus: http.StatusNotFound,
			wantCode:   "account_not_found",
		},
		{
			name:       "should answer 400 when account_id is not a uuid",
			userID:     ownerID,
			mutateBody: func(body map[string]any) { body["account_id"] = "abc" },
			wantStatus: http.StatusBadRequest,
			wantCode:   "bad_request",
		},
		{
			name:       "should answer 422 for a date in the future",
			userID:     ownerID,
			mutateBody: func(body map[string]any) { body["occurred_at"] = "2099-01-01T10:00:00Z" },
			wantStatus: http.StatusUnprocessableEntity,
			wantCode:   "invalid_occurred_at",
		},
		{
			name:       "should answer 422 for a blank description",
			userID:     ownerID,
			mutateBody: func(body map[string]any) { body["description"] = "   " },
			wantStatus: http.StatusUnprocessableEntity,
			wantCode:   "invalid_description",
		},
		{
			name:       "should answer 400 when occurred_at is missing",
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
				"description": "Groceries",
			}
			testCase.mutateBody(body)

			recorder := server.request(t, http.MethodPost, "/v1/transactions", testCase.userID.String(), body)

			require.Equal(t, testCase.wantStatus, recorder.Code, recorder.Body.String())
			assert.Equal(t, testCase.wantCode, errorCode(t, recorder))
			assert.Empty(t, server.transactions.stored)
		})
	}
}

func TestCategorizeTransactionEndpoint(t *testing.T) {
	t.Run("should answer 200 because it mutates an existing resource, default assigned_by to user and save the Categorized event", func(t *testing.T) {
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
		assert.Equal(t, "user", body["assigned_by"], "a missing assigned_by defaults to 'user'")
		assert.NotEmpty(t, body["assigned_at"])
		assert.Equal(t, []string{transaction.EventTypeCategorized}, server.transactions.savedEventNames())
	})
}

func TestCategorizeTransactionEndpointRefusal(t *testing.T) {
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
			name:       "should answer 403 for a transaction of another user",
			userID:     otherUserID,
			wantStatus: http.StatusForbidden,
			wantCode:   "forbidden",
		},
		{
			name:       "should answer 422 for an assigned_by outside the vocabulary",
			userID:     ownerID,
			body:       map[string]any{"assigned_by": "robot"},
			wantStatus: http.StatusUnprocessableEntity,
			wantCode:   "invalid_category_assignment",
		},
		{
			name:       "should answer 404 for a missing category",
			userID:     ownerID,
			body:       map[string]any{"category_id": uuid.New().String()},
			wantStatus: http.StatusNotFound,
			wantCode:   "category_not_found",
		},
		{
			name:       "should answer 400 when the transaction id is not a uuid",
			userID:     ownerID,
			path:       func(string) string { return "/v1/transactions/abc/category" },
			wantStatus: http.StatusBadRequest,
			wantCode:   "bad_request",
		},
		{
			name:       "should answer 400 when category_id is missing",
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
			assert.Empty(t, server.transactions.savedEvents, "an error saves no event")
		})
	}
}

func TestImportFromProviderEndpoint(t *testing.T) {
	t.Run("should import the connected account and answer the imported_count so the UI does not count the array", func(t *testing.T) {
		ownerID := uuid.New()
		connected := newConnectedAccount(t, ownerID, "mock")
		accountID := connected.ID()
		server := newTestServer(t, func(server *testServer) {
			server.accounts = newFakeAccounts(connected)
			server.provider = &fakeProvider{
				name: "mock",
				transactions: []openfinance.ProviderTransaction{
					{ProviderTransactionID: "tx-1", Description: "Taxi", AmountInCents: -2350, CurrencyCode: "BRL", OccurredAt: time.Now().Add(-24 * time.Hour)},
					{ProviderTransactionID: "tx-2", Description: "Salary", AmountInCents: 350000, CurrencyCode: "BRL", OccurredAt: time.Now().Add(-48 * time.Hour)},
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
		assert.Equal(t,
			[]string{transaction.EventTypeImported, transaction.EventTypeImported},
			server.transactions.savedEventNames(),
		)
		assert.Equal(t, []string{account.EventTypeBalanceUpdated}, server.accounts.savedEventNames())
	})

	t.Run("should pass the optional since window to the provider", func(t *testing.T) {
		ownerID := uuid.New()
		connected := newConnectedAccount(t, ownerID, "mock")
		accountID := connected.ID()
		server := newTestServer(t, func(server *testServer) {
			server.accounts = newFakeAccounts(connected)
			server.provider = &fakeProvider{
				name: "mock",
				transactions: []openfinance.ProviderTransaction{
					{ProviderTransactionID: "tx-1", Description: "Taxi", AmountInCents: -2350, CurrencyCode: "BRL", OccurredAt: time.Now().Add(-24 * time.Hour)},
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
	})
}

func TestImportFromProviderEndpointRefusal(t *testing.T) {
	ownerID := uuid.New()

	t.Run("should answer 422 for a manual account", func(t *testing.T) {
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

	t.Run("should answer 422 for an account of another provider", func(t *testing.T) {
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

	t.Run("should answer 502 when the provider is unavailable, since retrying later is the right action", func(t *testing.T) {
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

func TestImportFromProviderEndpointSyntax(t *testing.T) {
	cases := []struct {
		name string
		body any
	}{
		{name: "should answer 400 when account_id is not a uuid", body: map[string]any{"account_id": "abc"}},
		{name: "should answer 400 when account_id is missing", body: map[string]any{}},
		{name: "should answer 400 when since has an invalid format", body: map[string]any{"account_id": uuid.New().String(), "since": "yesterday"}},
		{name: "should answer 400 for malformed JSON", body: `{"account_id":`},
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

func TestBalanceChangesOnlyThroughImport(t *testing.T) {
	t.Run("should keep the balance through manual entries and apply the provider balance on import", func(t *testing.T) {
		ownerID := uuid.New()
		connected := newConnectedAccount(t, ownerID, "mock")
		accountID := connected.ID()
		server := newTestServer(t, func(server *testServer) {
			server.accounts = newFakeAccounts(connected)
			server.provider = &fakeProvider{
				name: "mock",
				transactions: []openfinance.ProviderTransaction{
					{ProviderTransactionID: "tx-1", Description: "Taxi", AmountInCents: -2350, CurrencyCode: "BRL", OccurredAt: time.Now().Add(-24 * time.Hour)},
				},
				balance: &openfinance.ProviderBalance{AmountInCents: 7_777_00, CurrencyCode: "BRL", AsOf: time.Now()},
			}
		})
		currentBalance := func() int64 {
			balance := connected.Balance()
			money := balance.Money()
			return money.Amount()
		}
		require.Zero(t, currentBalance(), "the account is born at zero")

		for _, amount := range []int64{-45_50, -120_00, 300_00} {
			recorder := server.request(t, http.MethodPost, "/v1/transactions", ownerID.String(), map[string]any{
				"account_id":  accountID.String(),
				"amount":      amount,
				"occurred_at": "2026-08-20T10:00:00Z",
				"description": "Entry",
			})
			require.Equal(t, http.StatusCreated, recorder.Code, recorder.Body.String())
		}
		require.Len(t, server.transactions.stored, 3)
		assert.Zero(t, currentBalance(), "a manual entry does NOT move the balance")

		recorder := server.request(t, http.MethodPost, "/v1/transactions/import", ownerID.String(), map[string]any{
			"account_id": accountID.String(),
		})

		require.Equal(t, http.StatusCreated, recorder.Code, recorder.Body.String())
		body := decode(t, recorder)
		assert.Equal(t, float64(7_777_00), body["balance"])
		assert.Equal(t, "BRL", body["currency"])
		assert.Equal(t, true, body["balance_applied"])
		assert.Equal(t, int64(7_777_00), currentBalance(), "the balance is what the provider reported, not the sum of entries")
	})
}
