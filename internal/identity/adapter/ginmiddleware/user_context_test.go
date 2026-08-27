// Testes do middleware de identidade: extração do Bearer token, tradução de
// erro e injeção da identidade no context.
//
// O resolvedor é uma closure de teste — a verificação de token de verdade é
// do adapter do Google (que tem os seus) e o use case tem os dele. Aqui o
// alvo é só a BORDA.
package ginmiddleware_test

import (
	"context"
	"encoding/json"
	"errors"
	"net/http"
	"net/http/httptest"
	"testing"

	"github.com/gin-gonic/gin"
	"github.com/google/uuid"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/luigimenezes13/financial-manager/internal/identity/adapter/ginmiddleware"
	identity "github.com/luigimenezes13/financial-manager/internal/identity/domain"
)

func TestMain(main *testing.M) {
	gin.SetMode(gin.TestMode)
	main.Run()
}

// newRouter monta uma rota protegida que ecoa o uuid resolvido, pra o teste
// poder afirmar que a identidade certa chegou ao handler.
func newRouter(resolve ginmiddleware.ResolveUser) *gin.Engine {
	router := gin.New()
	router.GET("/protegido", ginmiddleware.Authenticate(resolve), func(ginContext *gin.Context) {
		userID, ok := ginmiddleware.UserIDFrom(ginContext.Request.Context())
		if !ok {
			ginContext.JSON(http.StatusInternalServerError, gin.H{"error": "sem identidade no context"})
			return
		}
		ginContext.JSON(http.StatusOK, gin.H{"user_id": userID.String()})
	})
	return router
}

func do(t *testing.T, router *gin.Engine, authorization string) *httptest.ResponseRecorder {
	t.Helper()

	request := httptest.NewRequest(http.MethodGet, "/protegido", nil)
	if authorization != "" {
		request.Header.Set("Authorization", authorization)
	}
	recorder := httptest.NewRecorder()
	router.ServeHTTP(recorder, request)
	return recorder
}

func errorCode(t *testing.T, recorder *httptest.ResponseRecorder) string {
	t.Helper()

	var body map[string]map[string]string
	require.NoError(t, json.Unmarshal(recorder.Body.Bytes(), &body))
	return body["error"]["code"]
}

// TestInjetaIdentidadeResolvida: o token chega ao resolvedor e o uuid chega
// ao handler pelo context da REQUEST (não pelo gin.Context) — é isso que
// permite o use case receber context.Context puro.
func TestInjetaIdentidadeResolvida(t *testing.T) {
	expectedID := uuid.New()
	var receivedToken string

	router := newRouter(func(_ context.Context, rawToken string) (uuid.UUID, error) {
		receivedToken = rawToken
		return expectedID, nil
	})

	recorder := do(t, router, "Bearer token-abc")

	require.Equal(t, http.StatusOK, recorder.Code, recorder.Body.String())
	assert.Equal(t, "token-abc", receivedToken)

	var body map[string]string
	require.NoError(t, json.Unmarshal(recorder.Body.Bytes(), &body))
	assert.Equal(t, expectedID.String(), body["user_id"])
}

// TestEsquemaBearerCaseInsensitive: a RFC 7235 define o esquema como
// case-insensitive, e cliente mandando "bearer" minúsculo é comum o
// bastante pra não merecer um 401 misterioso.
func TestEsquemaBearerCaseInsensitive(t *testing.T) {
	for _, header := range []string{"Bearer token-abc", "bearer token-abc", "BEARER token-abc"} {
		t.Run(header, func(t *testing.T) {
			router := newRouter(func(context.Context, string) (uuid.UUID, error) {
				return uuid.New(), nil
			})

			recorder := do(t, router, header)

			assert.Equal(t, http.StatusOK, recorder.Code)
		})
	}
}

// TestCredencialAusenteOuMalFormada: todas viram o mesmo 401/invalid_token.
// Distinguir "não mandou" de "mandou errado" não muda a ação do cliente e só
// dá informação a quem sonda.
func TestCredencialAusenteOuMalFormada(t *testing.T) {
	cases := []struct {
		name          string
		authorization string
	}{
		{name: "header ausente", authorization: ""},
		{name: "sem esquema", authorization: "token-abc"},
		{name: "esquema errado", authorization: "Basic dXNlcjpwYXNz"},
		{name: "bearer sem token", authorization: "Bearer "},
		{name: "só espaços", authorization: "   "},
	}

	for _, testCase := range cases {
		t.Run(testCase.name, func(t *testing.T) {
			resolverCalled := false
			router := newRouter(func(context.Context, string) (uuid.UUID, error) {
				resolverCalled = true
				return uuid.New(), nil
			})

			recorder := do(t, router, testCase.authorization)

			require.Equal(t, http.StatusUnauthorized, recorder.Code)
			assert.Equal(t, "invalid_token", errorCode(t, recorder))
			assert.False(t, resolverCalled, "nem chega a tentar resolver")
		})
	}
}

// TestTraduzErroDoResolvedor: o middleware usa a MESMA tabela de tradução do
// resto da API. Se ele escolhesse status por conta própria, token expirado
// responderia diferente aqui e no resto do sistema.
func TestTraduzErroDoResolvedor(t *testing.T) {
	cases := []struct {
		name         string
		fromResolver error
		wantStatus   int
		wantCode     string
	}{
		{name: "token inválido", fromResolver: identity.ErrInvalidToken, wantStatus: http.StatusUnauthorized, wantCode: "invalid_token"},
		{name: "token expirado", fromResolver: identity.ErrTokenExpired, wantStatus: http.StatusUnauthorized, wantCode: "token_expired"},
		{name: "e-mail não verificado", fromResolver: identity.ErrEmailNotVerified, wantStatus: http.StatusForbidden, wantCode: "email_not_verified"},
		{name: "e-mail inválido no perfil", fromResolver: identity.ErrInvalidEmail, wantStatus: http.StatusUnprocessableEntity, wantCode: "invalid_email"},
		{name: "falha de infraestrutura", fromResolver: errors.New("banco fora"), wantStatus: http.StatusInternalServerError, wantCode: "internal_error"},
	}

	for _, testCase := range cases {
		t.Run(testCase.name, func(t *testing.T) {
			router := newRouter(func(context.Context, string) (uuid.UUID, error) {
				return uuid.Nil, testCase.fromResolver
			})

			recorder := do(t, router, "Bearer token-abc")

			require.Equal(t, testCase.wantStatus, recorder.Code, recorder.Body.String())
			assert.Equal(t, testCase.wantCode, errorCode(t, recorder))
		})
	}
}

// TestErroDeInfraNaoVaza: falha inesperada na resolução vira 500 genérico —
// a mensagem original poderia carregar host ou credencial.
func TestErroDeInfraNaoVaza(t *testing.T) {
	router := newRouter(func(context.Context, string) (uuid.UUID, error) {
		return uuid.Nil, errors.New("dial tcp 10.0.0.5:5432: connection refused")
	})

	recorder := do(t, router, "Bearer token-abc")

	require.Equal(t, http.StatusInternalServerError, recorder.Code)
	assert.NotContains(t, recorder.Body.String(), "10.0.0.5")
}

// TestUserIDFromSemMiddleware: context sem identidade devolve ok=false, e é
// por isso que o handler consegue distinguir "não autenticado" de "bug de
// fiação".
func TestUserIDFromSemMiddleware(t *testing.T) {
	userID, ok := ginmiddleware.UserIDFrom(context.Background())

	assert.False(t, ok)
	assert.Equal(t, uuid.Nil, userID)
}
