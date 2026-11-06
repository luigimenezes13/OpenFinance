package ginhandler_test

import (
	"net/http"
	"testing"

	"github.com/google/uuid"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func TestCreateAccount(t *testing.T) {
	t.Run("should answer 201 with a snake_case body, so swapping the response DTO for the use case Output breaks this contract", func(t *testing.T) {
		server := newTestServer(t)
		ownerID := uuid.New()

		recorder := server.request(t, http.MethodPost, "/v1/accounts", ownerID.String(), map[string]any{
			"name": "  Nubank  ", "kind": "checking", "currency": "BRL",
		})

		require.Equal(t, http.StatusCreated, recorder.Code, recorder.Body.String())
		body := decode(t, recorder)
		assert.Equal(t, ownerID.String(), body["user_id"])
		assert.Equal(t, "Nubank", body["name"], "the aggregate trims the name")
		assert.Equal(t, "checking", body["kind"])
		assert.Equal(t, float64(0), body["balance"], "a new account is born at zero")
		assert.Equal(t, "BRL", body["currency"])
		assert.NotEmpty(t, body["id"])

		assert.Len(t, server.accounts.stored, 1, "the account reached the repository")
	})

	t.Run("should ignore a user_id sent in the body, since the owner always comes from the authenticated identity", func(t *testing.T) {
		server := newTestServer(t)
		ownerID := uuid.New()
		attackerTarget := uuid.New()

		recorder := server.request(t, http.MethodPost, "/v1/accounts", ownerID.String(), map[string]any{
			"name": "Account", "kind": "checking", "currency": "BRL",
			"user_id": attackerTarget.String(),
		})

		require.Equal(t, http.StatusCreated, recorder.Code)
		body := decode(t, recorder)
		assert.Equal(t, ownerID.String(), body["user_id"], "the owner is the one from the header, not from the body")
	})

	t.Run("should answer 500 with a generic message, since the original error could carry a host, a credential or a column name", func(t *testing.T) {
		server := newTestServer(t, func(server *testServer) {
			server.accounts.saveErr = errInfra
		})

		recorder := server.request(t, http.MethodPost, "/v1/accounts", uuid.New().String(), map[string]any{
			"name": "Account", "kind": "checking", "currency": "BRL",
		})

		require.Equal(t, http.StatusInternalServerError, recorder.Code)
		assert.Equal(t, "internal_error", errorCode(t, recorder))
		assert.NotContains(t, recorder.Body.String(), "infrastructure failure",
			"the original message must never reach the client")
	})
}

func TestAuthentication(t *testing.T) {
	cases := []struct {
		name   string
		userID string
	}{
		{name: "should answer 401 without the identity header", userID: ""},
		{name: "should answer 401 when the identity header is not a uuid", userID: "not-a-uuid"},
	}

	for _, testCase := range cases {
		t.Run(testCase.name, func(t *testing.T) {
			server := newTestServer(t)

			recorder := server.request(t, http.MethodPost, "/v1/accounts", testCase.userID, map[string]any{
				"name": "Account", "kind": "checking", "currency": "BRL",
			})

			require.Equal(t, http.StatusUnauthorized, recorder.Code)
			assert.Empty(t, server.accounts.stored, "nothing was created")
		})
	}

	t.Run("should let a nil uuid through the middleware and answer 422 from the domain, since the middleware checks form and the domain checks meaning", func(t *testing.T) {
		server := newTestServer(t)

		recorder := server.request(t, http.MethodPost, "/v1/accounts", uuid.Nil.String(), map[string]any{
			"name": "Account", "kind": "checking", "currency": "BRL",
		})

		require.Equal(t, http.StatusUnprocessableEntity, recorder.Code)
		assert.Equal(t, "invalid_user_id", errorCode(t, recorder))
	})
}

func TestCreateAccountRefusal(t *testing.T) {
	cases := []struct {
		name       string
		body       any
		wantStatus int
		wantCode   string
	}{
		{
			name:       "should answer 400 when a required field is missing",
			body:       map[string]any{"name": "Account", "kind": "checking"},
			wantStatus: http.StatusBadRequest,
			wantCode:   "bad_request",
		},
		{
			name:       "should answer 400 for malformed JSON",
			body:       `{"name":`,
			wantStatus: http.StatusBadRequest,
			wantCode:   "bad_request",
		},
		{
			name:       "should answer 422 for a kind outside the vocabulary",
			body:       map[string]any{"name": "Account", "kind": "crypto", "currency": "BRL"},
			wantStatus: http.StatusUnprocessableEntity,
			wantCode:   "invalid_account_kind",
		},
		{
			name:       "should answer 422 for an unsupported currency",
			body:       map[string]any{"name": "Account", "kind": "checking", "currency": "JPY"},
			wantStatus: http.StatusUnprocessableEntity,
			wantCode:   "invalid_currency",
		},
		{
			name:       "should answer 422 for a blank name",
			body:       map[string]any{"name": "   ", "kind": "checking", "currency": "BRL"},
			wantStatus: http.StatusUnprocessableEntity,
			wantCode:   "invalid_account_name",
		},
	}

	for _, testCase := range cases {
		t.Run(testCase.name, func(t *testing.T) {
			server := newTestServer(t)

			recorder := server.request(t, http.MethodPost, "/v1/accounts", uuid.New().String(), testCase.body)

			require.Equal(t, testCase.wantStatus, recorder.Code, recorder.Body.String())
			assert.Equal(t, testCase.wantCode, errorCode(t, recorder))
			assert.Empty(t, server.accounts.stored)
		})
	}
}
