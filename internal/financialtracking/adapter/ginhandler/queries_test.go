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

func TestListAccounts(t *testing.T) {
	t.Run("should answer an envelope with only the user's accounts sorted by name", func(t *testing.T) {
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
		require.True(t, ok, "the response comes in an envelope, not a root array: %s", recorder.Body.String())
		require.Len(t, accounts, 2, "the other user's account is not listed")

		first, ok := accounts[0].(map[string]any)
		require.True(t, ok)
		assert.Equal(t, "Checking Account", first["name"])
		assert.Nil(t, first["provider"], "a manual account comes with a null provider")

		second, ok := accounts[1].(map[string]any)
		require.True(t, ok)
		assert.Equal(t, "Connected Account", second["name"])
		assert.Equal(t, "mock", second["provider"], "a connected account reports its provider")
		assert.NotEmpty(t, second["balance_as_of"])
	})
}

func TestGetAccount(t *testing.T) {
	ownerID := uuid.New()
	target := newManualAccount(t, ownerID)
	accountID := target.ID()

	server := newTestServer(t, func(server *testServer) {
		server.accounts = newFakeAccounts(target)
	})

	t.Run("should answer 200 with the account", func(t *testing.T) {
		recorder := server.request(t, http.MethodGet, "/v1/accounts/"+accountID.String(), ownerID.String(), nil)

		require.Equal(t, http.StatusOK, recorder.Code, recorder.Body.String())
		body := decode(t, recorder)
		assert.Equal(t, accountID.String(), body["id"])
		assert.Equal(t, "Checking Account", body["name"])
	})

	t.Run("should answer 403 for an account of another user", func(t *testing.T) {
		recorder := server.request(t, http.MethodGet, "/v1/accounts/"+accountID.String(), uuid.New().String(), nil)

		require.Equal(t, http.StatusForbidden, recorder.Code)
		assert.Equal(t, "forbidden", errorCode(t, recorder))
	})

	t.Run("should answer 404 for a missing account", func(t *testing.T) {
		recorder := server.request(t, http.MethodGet, "/v1/accounts/"+uuid.New().String(), ownerID.String(), nil)

		require.Equal(t, http.StatusNotFound, recorder.Code)
		assert.Equal(t, "account_not_found", errorCode(t, recorder))
	})

	t.Run("should answer 400 for a malformed id", func(t *testing.T) {
		recorder := server.request(t, http.MethodGet, "/v1/accounts/abc", ownerID.String(), nil)

		require.Equal(t, http.StatusBadRequest, recorder.Code)
	})
}

func TestListCategories(t *testing.T) {
	t.Run("should answer the categories with rules as an empty array, not null, so the client iterates without checking", func(t *testing.T) {
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
		assert.Equal(t, "Food", first["name"])
		assert.Nil(t, first["parent_id"])
		assert.NotNil(t, first["rules"], "rules comes as an empty array, not null")
	})
}

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

	t.Run("should answer newest first with the default window", func(t *testing.T) {
		recorder := server.request(t, http.MethodGet, "/v1/transactions", ownerID.String(), nil)

		require.Equal(t, http.StatusOK, recorder.Code, recorder.Body.String())
		body := decode(t, recorder)
		transactions := body["transactions"].([]any)
		require.Len(t, transactions, 3)

		first := transactions[0].(map[string]any)
		assert.Equal(t, float64(-300), first["amount"], "the statement starts with the newest")
		assert.Equal(t, float64(50), body["limit"], "the applied default comes back in the response")
		assert.Equal(t, float64(0), body["offset"])
		assert.Nil(t, first["category_id"], "an uncategorized transaction comes with a null category block")
		assert.Nil(t, first["provider"], "a manual transaction comes with a null provider")
	})

	t.Run("should paginate with limit and offset", func(t *testing.T) {
		recorder := server.request(t, http.MethodGet, "/v1/transactions?limit=2&offset=2", ownerID.String(), nil)

		require.Equal(t, http.StatusOK, recorder.Code)
		body := decode(t, recorder)
		assert.Len(t, body["transactions"].([]any), 1)
		assert.Equal(t, float64(2), body["limit"])
		assert.Equal(t, float64(2), body["offset"])
	})

	t.Run("should filter by period", func(t *testing.T) {
		from := time.Now().Add(-50 * time.Hour).UTC().Format(time.RFC3339)
		path := fmt.Sprintf("/v1/transactions?from=%s", from)

		recorder := server.request(t, http.MethodGet, path, ownerID.String(), nil)

		require.Equal(t, http.StatusOK, recorder.Code, recorder.Body.String())
		body := decode(t, recorder)
		assert.Len(t, body["transactions"].([]any), 2, "the one from 72h ago stays out")
	})

	t.Run("should filter by account", func(t *testing.T) {
		path := "/v1/transactions?account_id=" + accountID.String()

		recorder := server.request(t, http.MethodGet, path, ownerID.String(), nil)

		require.Equal(t, http.StatusOK, recorder.Code)
		body := decode(t, recorder)
		assert.Len(t, body["transactions"].([]any), 3)
	})
}

func TestListTransactionsInvalidQuery(t *testing.T) {
	ownerID := uuid.New()

	cases := []struct {
		name       string
		query      string
		wantStatus int
		wantCode   string
	}{
		{name: "should answer 400 when account_id is not a uuid", query: "?account_id=abc", wantStatus: http.StatusBadRequest, wantCode: "bad_request"},
		{name: "should answer 400 when from is not RFC 3339", query: "?from=yesterday", wantStatus: http.StatusBadRequest, wantCode: "bad_request"},
		{name: "should answer 400 when limit is not a number", query: "?limit=many", wantStatus: http.StatusBadRequest, wantCode: "bad_request"},
		{name: "should answer 422 for a limit above the ceiling", query: "?limit=201", wantStatus: http.StatusUnprocessableEntity, wantCode: "invalid_page"},
		{name: "should answer 422 for a negative offset", query: "?offset=-1", wantStatus: http.StatusUnprocessableEntity, wantCode: "invalid_page"},
		{
			name:       "should answer 422 for an inverted period",
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

func TestGetTransaction(t *testing.T) {
	t.Run("should answer the detail with the category assigned through the API", func(t *testing.T) {
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
	})
}
