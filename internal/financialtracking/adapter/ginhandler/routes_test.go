package ginhandler_test

import (
	"errors"
	"net/http"
	"testing"

	"github.com/google/uuid"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

// TestHealthz cobre os DOIS caminhos. O de falha é o que importa: um
// /healthz que devolve 200 com o banco fora mantém no ar um processo que não
// atende ninguém — e era intestável enquanto a rota recebia *sql.DB
// concreto.
func TestHealthz(t *testing.T) {
	t.Run("banco acessível", func(t *testing.T) {
		server := newTestServer(t)

		recorder := server.request(t, http.MethodGet, "/healthz", "", nil)

		require.Equal(t, http.StatusOK, recorder.Code)
		body := decode(t, recorder)
		assert.Equal(t, "healthy", body["status"])
		assert.Equal(t, "reachable", body["database"])
	})

	t.Run("banco inacessível", func(t *testing.T) {
		server := newTestServer(t, func(server *testServer) {
			server.pinger = &fakePinger{err: errors.New("connection refused")}
		})

		recorder := server.request(t, http.MethodGet, "/healthz", "", nil)

		require.Equal(t, http.StatusServiceUnavailable, recorder.Code)
		body := decode(t, recorder)
		assert.Equal(t, "unhealthy", body["status"])
	})
}

// TestHealthzNaoExigeAutenticacao: o orquestrador (k8s, compose) não manda
// header de usuário. Se a rota tivesse entrado no grupo /v1, o health check
// responderia 401 e o pod nunca ficaria pronto.
func TestHealthzNaoExigeAutenticacao(t *testing.T) {
	server := newTestServer(t)

	recorder := server.request(t, http.MethodGet, "/healthz", "", nil)

	assert.Equal(t, http.StatusOK, recorder.Code)
}

// TestTodasAsRotasV1ExigemAutenticacao é o teste de REGRESSÃO do
// agrupamento: se alguém registrar uma rota nova fora do grupo autenticado,
// a lista abaixo denuncia. É o furo de autorização mais fácil de cometer e
// o mais silencioso.
func TestTodasAsRotasV1ExigemAutenticacao(t *testing.T) {
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
		t.Run(route.method+" "+route.path, func(t *testing.T) {
			server := newTestServer(t)

			recorder := server.request(t, route.method, route.path, "", map[string]any{})

			assert.Equal(t, http.StatusUnauthorized, recorder.Code,
				"rota sem middleware de identidade: %s %s", route.method, route.path)
		})
	}
}
