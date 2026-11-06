package ginhandler_test

import (
	"errors"
	"net/http"
	"testing"

	"github.com/google/uuid"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func TestHealthz(t *testing.T) {
	t.Run("should answer 200 healthy when the database is reachable", func(t *testing.T) {
		server := newTestServer(t)

		recorder := server.request(t, http.MethodGet, "/healthz", "", nil)

		require.Equal(t, http.StatusOK, recorder.Code)
		body := decode(t, recorder)
		assert.Equal(t, "healthy", body["status"])
		assert.Equal(t, "reachable", body["database"])
	})

	t.Run("should answer 503 unhealthy when the database is unreachable, since a 200 would keep alive a process that serves no one", func(t *testing.T) {
		server := newTestServer(t, func(server *testServer) {
			server.pinger = &fakePinger{err: errors.New("connection refused")}
		})

		recorder := server.request(t, http.MethodGet, "/healthz", "", nil)

		require.Equal(t, http.StatusServiceUnavailable, recorder.Code)
		body := decode(t, recorder)
		assert.Equal(t, "unhealthy", body["status"])
	})

	t.Run("should not require authentication, since the orchestrator sends no user header and a 401 would keep the pod from ever being ready", func(t *testing.T) {
		server := newTestServer(t)

		recorder := server.request(t, http.MethodGet, "/healthz", "", nil)

		assert.Equal(t, http.StatusOK, recorder.Code)
	})
}

func TestEveryV1RouteRequiresAuthentication(t *testing.T) {
	routes := []struct {
		method string
		path   string
	}{
		{http.MethodPost, "/v1/accounts"},
		{http.MethodGet, "/v1/accounts"},
		{http.MethodGet, "/v1/accounts/" + uuid.New().String()},
		{http.MethodPost, "/v1/categories"},
		{http.MethodGet, "/v1/categories"},
		{http.MethodPost, "/v1/transactions"},
		{http.MethodGet, "/v1/transactions"},
		{http.MethodGet, "/v1/transactions/" + uuid.New().String()},
		{http.MethodPut, "/v1/transactions/" + uuid.New().String() + "/category"},
		{http.MethodPost, "/v1/transactions/import"},
		{http.MethodPatch, "/v1/accounts/" + uuid.New().String()},
		{http.MethodPatch, "/v1/categories/" + uuid.New().String()},
		{http.MethodPut, "/v1/categories/" + uuid.New().String() + "/parent"},
		{http.MethodPost, "/v1/categories/" + uuid.New().String() + "/rules"},
		{http.MethodDelete, "/v1/categories/" + uuid.New().String() + "/rules/" + uuid.New().String()},
	}

	for _, route := range routes {
		t.Run("should answer 401 without identity on "+route.method+" "+route.path+", so a route registered outside the authenticated group is caught", func(t *testing.T) {
			server := newTestServer(t)

			recorder := server.request(t, route.method, route.path, "", map[string]any{})

			assert.Equal(t, http.StatusUnauthorized, recorder.Code,
				"route without the identity middleware: %s %s", route.method, route.path)
		})
	}
}
