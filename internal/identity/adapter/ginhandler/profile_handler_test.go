package ginhandler_test

import (
	"context"
	"encoding/json"
	"errors"
	"io"
	"log/slog"
	"net/http"
	"net/http/httptest"
	"testing"

	"github.com/gin-gonic/gin"
	"github.com/google/uuid"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/luigimenezes13/financial-manager/internal/identity/adapter/ginhandler"
	"github.com/luigimenezes13/financial-manager/internal/identity/adapter/ginmiddleware"
	"github.com/luigimenezes13/financial-manager/internal/identity/application"
	identity "github.com/luigimenezes13/financial-manager/internal/identity/domain"
	"github.com/luigimenezes13/financial-manager/internal/platform/httperror"
)

var errInfra = errors.New("fake: infrastructure failure")

func TestMain(main *testing.M) {
	gin.SetMode(gin.TestMode)
	main.Run()
}

type fakeUsers struct {
	stored  map[string]*identity.User
	findErr error
}

func newFakeUsers(stored ...*identity.User) *fakeUsers {
	fake := &fakeUsers{stored: make(map[string]*identity.User, len(stored))}
	for _, existing := range stored {
		fake.stored[existing.ID().String()] = existing
	}
	return fake
}

func (f *fakeUsers) Save(context.Context, *identity.User) error { return nil }

func (f *fakeUsers) FindByID(_ context.Context, id identity.UserID) (*identity.User, error) {
	if f.findErr != nil {
		return nil, f.findErr
	}
	found, ok := f.stored[id.String()]
	if !ok {
		return nil, identity.ErrNotFound
	}
	return found, nil
}

func (f *fakeUsers) FindByExternalIdentity(context.Context, identity.ExternalIdentity) (*identity.User, error) {
	return nil, identity.ErrNotFound
}

func mustUser(t *testing.T) *identity.User {
	t.Helper()

	email, err := identity.NewEmail("luigi@example.com")
	require.NoError(t, err)
	external, err := identity.NewExternalIdentity(identity.ProviderGoogle, "google-sub-123")
	require.NoError(t, err)
	registered, err := identity.Register(email, "Luigi Menezes", external)
	require.NoError(t, err)
	return registered
}

// stubAuthenticate imita o contrato do middleware real (injeta um uuid no
// context, ou 401) sem exigir que estes testes forjem um token do Google — a
// verificação de credencial tem testes próprios em ginmiddleware.
func stubAuthenticate() gin.HandlerFunc {
	return func(ginContext *gin.Context) {
		raw := ginContext.GetHeader("X-User-Id")
		userID, err := uuid.Parse(raw)
		if err != nil {
			ginContext.AbortWithStatusJSON(http.StatusUnauthorized, httperror.Body{
				Error: httperror.Detail{Code: "invalid_token", Message: "credencial ausente ou mal formada"},
			})
			return
		}
		ginContext.Request = ginContext.Request.WithContext(
			ginmiddleware.ContextWithUserID(ginContext.Request.Context(), userID))
		ginContext.Next()
	}
}

// newRouter monta o router com a rota real e o use case real.
func newRouter(users *fakeUsers) *gin.Engine {
	logger := slog.New(slog.NewTextHandler(io.Discard, nil))
	router := gin.New()
	ginhandler.RegisterRoutes(router, stubAuthenticate(),
		ginhandler.NewProfileHandler(application.NewViewProfileUseCase(users), logger))
	return router
}

func get(t *testing.T, router *gin.Engine, userID string) *httptest.ResponseRecorder {
	t.Helper()

	request := httptest.NewRequest(http.MethodGet, "/v1/me", nil)
	if userID != "" {
		request.Header.Set("X-User-Id", userID)
	}
	recorder := httptest.NewRecorder()
	router.ServeHTTP(recorder, request)
	return recorder
}

// TestMe cobre o caminho feliz e o CONTRATO do corpo: snake_case e, o mais
// importante, SEM a identidade externa.
func TestMe(t *testing.T) {
	existing := mustUser(t)
	router := newRouter(newFakeUsers(existing))

	recorder := get(t, router, existing.ID().String())

	require.Equal(t, http.StatusOK, recorder.Code, recorder.Body.String())

	var body map[string]any
	require.NoError(t, json.Unmarshal(recorder.Body.Bytes(), &body))
	assert.Equal(t, existing.ID().String(), body["user_id"])
	assert.Equal(t, "luigi@example.com", body["email"])
	assert.Equal(t, "Luigi Menezes", body["name"])
	assert.NotEmpty(t, body["registered_at"])

	// O subject do provedor não pode aparecer em campo nenhum: um frontend
	// registraria isso em log ou analytics sem pensar duas vezes.
	assert.NotContains(t, recorder.Body.String(), "google-sub-123")
	assert.NotContains(t, body, "external_provider")
	assert.NotContains(t, body, "external_subject")
}

// TestMeExigeAutenticacao: a rota está no grupo autenticado.
func TestMeExigeAutenticacao(t *testing.T) {
	cases := []struct {
		name   string
		userID string
	}{
		{name: "sem credencial", userID: ""},
		{name: "credencial mal formada", userID: "nao-e-uuid"},
	}

	for _, testCase := range cases {
		t.Run(testCase.name, func(t *testing.T) {
			router := newRouter(newFakeUsers())

			recorder := get(t, router, testCase.userID)

			assert.Equal(t, http.StatusUnauthorized, recorder.Code)
		})
	}
}

// TestMeUsuarioInexistente: só alcançável se o usuário for removido entre a
// resolução do token e a consulta. Vira 404, não 500.
func TestMeUsuarioInexistente(t *testing.T) {
	router := newRouter(newFakeUsers())

	recorder := get(t, router, uuid.New().String())

	require.Equal(t, http.StatusNotFound, recorder.Code)

	var body map[string]map[string]string
	require.NoError(t, json.Unmarshal(recorder.Body.Bytes(), &body))
	assert.Equal(t, "user_not_found", body["error"]["code"])
}

// TestMeErroDeInfraNaoVaza: 500 com mensagem genérica.
func TestMeErroDeInfraNaoVaza(t *testing.T) {
	users := newFakeUsers()
	users.findErr = errInfra
	router := newRouter(users)

	recorder := get(t, router, uuid.New().String())

	require.Equal(t, http.StatusInternalServerError, recorder.Code)
	assert.NotContains(t, recorder.Body.String(), "infrastructure failure")
}
