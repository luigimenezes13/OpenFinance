package ginhandler_test

import (
	"net/http"
	"testing"

	"github.com/google/uuid"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

// TestCreateAccount cobre o caminho feliz e o CONTRATO do corpo de
// resposta: chaves em snake_case. Se alguém trocar o DTO de saída pelo
// Output do use case, este teste quebra — que é o ponto.
func TestCreateAccount(t *testing.T) {
	server := newTestServer(t)
	ownerID := uuid.New()

	recorder := server.request(t, http.MethodPost, "/v1/accounts", ownerID.String(), map[string]any{
		"name": "  Nubank  ", "kind": "checking", "currency": "BRL",
	})

	require.Equal(t, http.StatusCreated, recorder.Code, recorder.Body.String())
	body := decode(t, recorder)
	assert.Equal(t, ownerID.String(), body["user_id"])
	assert.Equal(t, "Nubank", body["name"], "o aggregate normaliza o nome")
	assert.Equal(t, "checking", body["kind"])
	assert.Equal(t, float64(0), body["balance"], "conta nova nasce zerada")
	assert.Equal(t, "BRL", body["currency"])
	assert.NotEmpty(t, body["id"])

	assert.Len(t, server.accounts.stored, 1, "a conta chegou ao repositório")
}

// TestCreateAccountIgnoraUserIDDoCorpo prova que o dono NUNCA vem do
// payload: mandar user_id de outra pessoa no corpo não muda nada, porque a
// identidade sai do header.
func TestCreateAccountIgnoraUserIDDoCorpo(t *testing.T) {
	server := newTestServer(t)
	ownerID := uuid.New()
	attackerTarget := uuid.New()

	recorder := server.request(t, http.MethodPost, "/v1/accounts", ownerID.String(), map[string]any{
		"name": "Conta", "kind": "checking", "currency": "BRL",
		"user_id": attackerTarget.String(),
	})

	require.Equal(t, http.StatusCreated, recorder.Code)
	body := decode(t, recorder)
	assert.Equal(t, ownerID.String(), body["user_id"], "o dono é o do header, não o do corpo")
}

// TestAutenticacao cobre o middleware: sem header e com header inválido
// morrem antes de qualquer use case.
func TestAutenticacao(t *testing.T) {
	cases := []struct {
		name   string
		userID string
	}{
		{name: "sem header", userID: ""},
		{name: "header que não é uuid", userID: "nao-e-uuid"},
		{name: "uuid nil", userID: uuid.Nil.String()},
	}

	for _, testCase := range cases {
		t.Run(testCase.name, func(t *testing.T) {
			server := newTestServer(t)

			recorder := server.request(t, http.MethodPost, "/v1/accounts", testCase.userID, map[string]any{
				"name": "Conta", "kind": "checking", "currency": "BRL",
			})

			// uuid.Nil passa pelo middleware (é uuid válido) e morre no
			// domínio com 422 — a distinção é proposital: o middleware
			// cuida de FORMA, o domínio de significado.
			if testCase.userID == uuid.Nil.String() {
				require.Equal(t, http.StatusUnprocessableEntity, recorder.Code)
				assert.Equal(t, "invalid_user_id", errorCode(t, recorder))
				return
			}

			require.Equal(t, http.StatusUnauthorized, recorder.Code)
			assert.Empty(t, server.accounts.stored, "nada foi criado")
		})
	}
}

// TestCreateAccountRecusa cobre a distinção 400 (sintaxe) vs 422 (domínio),
// que é o contrato mais importante da borda.
func TestCreateAccountRecusa(t *testing.T) {
	cases := []struct {
		name       string
		body       any
		wantStatus int
		wantCode   string
	}{
		{
			name:       "campo obrigatório ausente",
			body:       map[string]any{"name": "Conta", "kind": "checking"},
			wantStatus: http.StatusBadRequest,
			wantCode:   "bad_request",
		},
		{
			name:       "JSON malformado",
			body:       `{"name":`,
			wantStatus: http.StatusBadRequest,
			wantCode:   "bad_request",
		},
		{
			name:       "kind fora do vocabulário",
			body:       map[string]any{"name": "Conta", "kind": "cripto", "currency": "BRL"},
			wantStatus: http.StatusUnprocessableEntity,
			wantCode:   "invalid_account_kind",
		},
		{
			name:       "moeda não suportada",
			body:       map[string]any{"name": "Conta", "kind": "checking", "currency": "JPY"},
			wantStatus: http.StatusUnprocessableEntity,
			wantCode:   "invalid_currency",
		},
		{
			name:       "nome só com espaços",
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

// TestErroDeInfraNaoVaza é o teste de segurança da borda: falha de
// infraestrutura vira 500 com mensagem GENÉRICA. Se a mensagem do erro
// original vazasse, ela poderia carregar host, credencial ou nome de coluna.
func TestErroDeInfraNaoVaza(t *testing.T) {
	server := newTestServer(t, func(server *testServer) {
		server.accounts.saveErr = errInfra
	})

	recorder := server.request(t, http.MethodPost, "/v1/accounts", uuid.New().String(), map[string]any{
		"name": "Conta", "kind": "checking", "currency": "BRL",
	})

	require.Equal(t, http.StatusInternalServerError, recorder.Code)
	assert.Equal(t, "internal_error", errorCode(t, recorder))
	assert.NotContains(t, recorder.Body.String(), "infrastructure failure",
		"a mensagem original não pode chegar ao cliente")
}
