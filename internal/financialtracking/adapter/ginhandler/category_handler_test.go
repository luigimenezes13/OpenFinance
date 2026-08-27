package ginhandler_test

import (
	"net/http"
	"testing"

	"github.com/google/uuid"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

// TestCreateCategory cobre a categoria raiz: parent_id sai null, não
// omitido — o cliente precisa distinguir "é raiz" de "a API parou de
// mandar o campo".
func TestCreateCategory(t *testing.T) {
	server := newTestServer(t)
	ownerID := uuid.New()

	recorder := server.request(t, http.MethodPost, "/v1/categories", ownerID.String(), map[string]any{
		"name": "  Alimentação  ",
	})

	require.Equal(t, http.StatusCreated, recorder.Code, recorder.Body.String())
	body := decode(t, recorder)
	assert.Equal(t, "Alimentação", body["name"])
	assert.Equal(t, ownerID.String(), body["user_id"])
	assert.Contains(t, body, "parent_id")
	assert.Nil(t, body["parent_id"])
	assert.Len(t, server.categories.stored, 1)
}

// TestCreateSubcategoria cobre o pai opcional.
func TestCreateSubcategoria(t *testing.T) {
	server := newTestServer(t)
	ownerID := uuid.New()
	parentID := uuid.New()

	recorder := server.request(t, http.MethodPost, "/v1/categories", ownerID.String(), map[string]any{
		"name": "Restaurantes", "parent_id": parentID.String(),
	})

	require.Equal(t, http.StatusCreated, recorder.Code, recorder.Body.String())
	body := decode(t, recorder)
	assert.Equal(t, parentID.String(), body["parent_id"])
}

// TestCreateCategoryRecusa cobre a fronteira 400/422 no parent_id: string
// que não é uuid é SINTAXE (400); uuid nil é uuid válido que o domínio
// recusa (422).
func TestCreateCategoryRecusa(t *testing.T) {
	cases := []struct {
		name       string
		body       any
		wantStatus int
		wantCode   string
	}{
		{
			name:       "nome ausente",
			body:       map[string]any{},
			wantStatus: http.StatusBadRequest,
			wantCode:   "bad_request",
		},
		{
			name:       "nome só com espaços",
			body:       map[string]any{"name": "   "},
			wantStatus: http.StatusUnprocessableEntity,
			wantCode:   "invalid_category_name",
		},
		{
			name:       "parent_id não é uuid",
			body:       map[string]any{"name": "Restaurantes", "parent_id": "abc"},
			wantStatus: http.StatusBadRequest,
			wantCode:   "bad_request",
		},
		{
			name:       "parent_id nil",
			body:       map[string]any{"name": "Restaurantes", "parent_id": uuid.Nil.String()},
			wantStatus: http.StatusUnprocessableEntity,
			wantCode:   "invalid_category_id",
		},
	}

	for _, testCase := range cases {
		t.Run(testCase.name, func(t *testing.T) {
			server := newTestServer(t)

			recorder := server.request(t, http.MethodPost, "/v1/categories", uuid.New().String(), testCase.body)

			require.Equal(t, testCase.wantStatus, recorder.Code, recorder.Body.String())
			assert.Equal(t, testCase.wantCode, errorCode(t, recorder))
			assert.Empty(t, server.categories.stored)
		})
	}
}
