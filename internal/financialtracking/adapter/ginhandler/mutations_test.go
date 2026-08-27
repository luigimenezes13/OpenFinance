package ginhandler_test

import (
	"fmt"
	"net/http"
	"testing"

	"github.com/google/uuid"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

// TestRenameAccount cobre a renomeação e as recusas.
func TestRenameAccount(t *testing.T) {
	ownerID := uuid.New()

	t.Run("caminho feliz", func(t *testing.T) {
		target := newManualAccount(t, ownerID)
		accountID := target.ID()
		server := newTestServer(t, func(server *testServer) {
			server.accounts = newFakeAccounts(target)
		})

		recorder := server.request(t, http.MethodPatch, "/v1/accounts/"+accountID.String(), ownerID.String(),
			map[string]any{"name": "  Nubank PJ  "})

		require.Equal(t, http.StatusOK, recorder.Code, recorder.Body.String())
		body := decode(t, recorder)
		assert.Equal(t, "Nubank PJ", body["name"], "o aggregate normaliza o nome")
	})

	t.Run("nome em branco é recusado pelo domínio", func(t *testing.T) {
		target := newManualAccount(t, ownerID)
		accountID := target.ID()
		server := newTestServer(t, func(server *testServer) {
			server.accounts = newFakeAccounts(target)
		})

		recorder := server.request(t, http.MethodPatch, "/v1/accounts/"+accountID.String(), ownerID.String(),
			map[string]any{"name": "   "})

		require.Equal(t, http.StatusUnprocessableEntity, recorder.Code)
		assert.Equal(t, "invalid_account_name", errorCode(t, recorder))
		assert.Equal(t, "Conta Corrente", target.Name(), "recusa não muta o aggregate")
	})

	t.Run("conta de outro usuário", func(t *testing.T) {
		target := newManualAccount(t, ownerID)
		accountID := target.ID()
		server := newTestServer(t, func(server *testServer) {
			server.accounts = newFakeAccounts(target)
		})

		recorder := server.request(t, http.MethodPatch, "/v1/accounts/"+accountID.String(), uuid.New().String(),
			map[string]any{"name": "Sequestrada"})

		require.Equal(t, http.StatusForbidden, recorder.Code)
		assert.Equal(t, "Conta Corrente", target.Name())
	})
}

// TestCategoryRules cobre o ciclo completo de regras pela API — o
// comportamento que existia no domínio e não tinha porta de entrada.
func TestCategoryRules(t *testing.T) {
	ownerID := uuid.New()
	target := newCategoryFor(t, ownerID)
	categoryID := target.ID()
	server := newTestServer(t, func(server *testServer) {
		server.categories = newFakeCategories(target)
	})

	rulesPath := fmt.Sprintf("/v1/categories/%s/rules", categoryID.String())

	// Cria duas regras.
	first := server.request(t, http.MethodPost, rulesPath, ownerID.String(), map[string]any{"keyword": "ifood"})
	require.Equal(t, http.StatusCreated, first.Code, first.Body.String())

	second := server.request(t, http.MethodPost, rulesPath, ownerID.String(), map[string]any{"keyword": "mercado"})
	require.Equal(t, http.StatusCreated, second.Code)

	body := decode(t, second)
	rules := body["rules"].([]any)
	require.Len(t, rules, 2, "a resposta devolve a categoria inteira, não só a regra criada")

	// Keyword duplicada é conflito.
	duplicate := server.request(t, http.MethodPost, rulesPath, ownerID.String(), map[string]any{"keyword": "ifood"})
	require.Equal(t, http.StatusConflict, duplicate.Code)
	assert.Equal(t, "duplicate_category_rule", errorCode(t, duplicate))

	// Keyword em branco morre no VO.
	blank := server.request(t, http.MethodPost, rulesPath, ownerID.String(), map[string]any{"keyword": "   "})
	require.Equal(t, http.StatusUnprocessableEntity, blank.Code)
	assert.Equal(t, "invalid_category_keyword", errorCode(t, blank))

	// Remove uma regra pelo id.
	firstRule := rules[0].(map[string]any)
	ruleID := firstRule["id"].(string)
	removed := server.request(t, http.MethodDelete, rulesPath+"/"+ruleID, ownerID.String(), nil)
	require.Equal(t, http.StatusOK, removed.Code, removed.Body.String())
	assert.Len(t, decode(t, removed)["rules"].([]any), 1, "a resposta mostra o conjunto que sobrou")

	// Remover de novo é 404.
	again := server.request(t, http.MethodDelete, rulesPath+"/"+ruleID, ownerID.String(), nil)
	require.Equal(t, http.StatusNotFound, again.Code)
	assert.Equal(t, "category_rule_not_found", errorCode(t, again))

	// Categoria de outro dono não deixa mexer nas regras.
	forbidden := server.request(t, http.MethodPost, rulesPath, uuid.New().String(), map[string]any{"keyword": "uber"})
	assert.Equal(t, http.StatusForbidden, forbidden.Code)
}

// TestRenameCategory cobre a renomeação.
func TestRenameCategory(t *testing.T) {
	ownerID := uuid.New()
	target := newCategoryFor(t, ownerID)
	categoryID := target.ID()
	server := newTestServer(t, func(server *testServer) {
		server.categories = newFakeCategories(target)
	})

	recorder := server.request(t, http.MethodPatch, "/v1/categories/"+categoryID.String(), ownerID.String(),
		map[string]any{"name": "Comida"})

	require.Equal(t, http.StatusOK, recorder.Code, recorder.Body.String())
	assert.Equal(t, "Comida", decode(t, recorder)["name"])
}

// TestMoveCategory cobre as duas direções do movimento, incluindo o
// `parent_id: null` que é PEDIDO EXPLÍCITO de virar raiz.
func TestMoveCategory(t *testing.T) {
	ownerID := uuid.New()

	t.Run("vira subcategoria", func(t *testing.T) {
		parent := newCategoryFor(t, ownerID)
		child := newCategoryFor(t, ownerID)
		parentID := parent.ID()
		childID := child.ID()
		server := newTestServer(t, func(server *testServer) {
			server.categories = newFakeCategories(parent, child)
		})

		recorder := server.request(t, http.MethodPut, "/v1/categories/"+childID.String()+"/parent", ownerID.String(),
			map[string]any{"parent_id": parentID.String()})

		require.Equal(t, http.StatusOK, recorder.Code, recorder.Body.String())
		assert.Equal(t, parentID.String(), decode(t, recorder)["parent_id"])
	})

	t.Run("parent_id null volta a ser raiz", func(t *testing.T) {
		parent := newCategoryFor(t, ownerID)
		parentID := parent.ID()
		child := newCategoryFor(t, ownerID)
		require.NoError(t, child.MoveTo(&parentID))
		childID := child.ID()

		server := newTestServer(t, func(server *testServer) {
			server.categories = newFakeCategories(parent, child)
		})

		recorder := server.request(t, http.MethodPut, "/v1/categories/"+childID.String()+"/parent", ownerID.String(),
			map[string]any{"parent_id": nil})

		require.Equal(t, http.StatusOK, recorder.Code, recorder.Body.String())
		assert.Nil(t, decode(t, recorder)["parent_id"])
	})

	t.Run("pai de si mesma é recusado", func(t *testing.T) {
		target := newCategoryFor(t, ownerID)
		categoryID := target.ID()
		server := newTestServer(t, func(server *testServer) {
			server.categories = newFakeCategories(target)
		})

		recorder := server.request(t, http.MethodPut, "/v1/categories/"+categoryID.String()+"/parent", ownerID.String(),
			map[string]any{"parent_id": categoryID.String()})

		require.Equal(t, http.StatusUnprocessableEntity, recorder.Code)
		assert.Equal(t, "invalid_category_parent", errorCode(t, recorder))
	})
}
