package ginhandler_test

import (
	"fmt"
	"net/http"
	"testing"

	"github.com/google/uuid"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func TestRenameAccount(t *testing.T) {
	ownerID := uuid.New()

	t.Run("should answer 200 with the trimmed name", func(t *testing.T) {
		target := newManualAccount(t, ownerID)
		accountID := target.ID()
		server := newTestServer(t, func(server *testServer) {
			server.accounts = newFakeAccounts(target)
		})

		recorder := server.request(t, http.MethodPatch, "/v1/accounts/"+accountID.String(), ownerID.String(),
			map[string]any{"name": "  Nubank Business  "})

		require.Equal(t, http.StatusOK, recorder.Code, recorder.Body.String())
		body := decode(t, recorder)
		assert.Equal(t, "Nubank Business", body["name"], "the aggregate trims the name")
	})

	t.Run("should answer 422 for a blank name refused by the domain", func(t *testing.T) {
		target := newManualAccount(t, ownerID)
		accountID := target.ID()
		server := newTestServer(t, func(server *testServer) {
			server.accounts = newFakeAccounts(target)
		})

		recorder := server.request(t, http.MethodPatch, "/v1/accounts/"+accountID.String(), ownerID.String(),
			map[string]any{"name": "   "})

		require.Equal(t, http.StatusUnprocessableEntity, recorder.Code)
		assert.Equal(t, "invalid_account_name", errorCode(t, recorder))
		assert.Equal(t, "Checking Account", target.Name(), "a refusal does not mutate the aggregate")
	})

	t.Run("should answer 403 for an account of another user", func(t *testing.T) {
		target := newManualAccount(t, ownerID)
		accountID := target.ID()
		server := newTestServer(t, func(server *testServer) {
			server.accounts = newFakeAccounts(target)
		})

		recorder := server.request(t, http.MethodPatch, "/v1/accounts/"+accountID.String(), uuid.New().String(),
			map[string]any{"name": "Hijacked"})

		require.Equal(t, http.StatusForbidden, recorder.Code)
		assert.Equal(t, "Checking Account", target.Name())
	})
}

func TestCategoryRules(t *testing.T) {
	t.Run("should add, refuse and remove rules through the API, exposing behavior the domain already had", func(t *testing.T) {
		ownerID := uuid.New()
		target := newCategoryFor(t, ownerID)
		categoryID := target.ID()
		server := newTestServer(t, func(server *testServer) {
			server.categories = newFakeCategories(target)
		})

		rulesPath := fmt.Sprintf("/v1/categories/%s/rules", categoryID.String())

		first := server.request(t, http.MethodPost, rulesPath, ownerID.String(), map[string]any{"keyword": "ifood"})
		require.Equal(t, http.StatusCreated, first.Code, first.Body.String())

		second := server.request(t, http.MethodPost, rulesPath, ownerID.String(), map[string]any{"keyword": "market"})
		require.Equal(t, http.StatusCreated, second.Code)

		body := decode(t, second)
		rules := body["rules"].([]any)
		require.Len(t, rules, 2, "the response returns the whole category, not only the created rule")

		duplicate := server.request(t, http.MethodPost, rulesPath, ownerID.String(), map[string]any{"keyword": "ifood"})
		require.Equal(t, http.StatusConflict, duplicate.Code, "a duplicate keyword is a conflict")
		assert.Equal(t, "duplicate_category_rule", errorCode(t, duplicate))

		blank := server.request(t, http.MethodPost, rulesPath, ownerID.String(), map[string]any{"keyword": "   "})
		require.Equal(t, http.StatusUnprocessableEntity, blank.Code, "a blank keyword dies in the value object")
		assert.Equal(t, "invalid_category_keyword", errorCode(t, blank))

		firstRule := rules[0].(map[string]any)
		ruleID := firstRule["id"].(string)
		removed := server.request(t, http.MethodDelete, rulesPath+"/"+ruleID, ownerID.String(), nil)
		require.Equal(t, http.StatusOK, removed.Code, removed.Body.String())
		assert.Len(t, decode(t, removed)["rules"].([]any), 1, "the response shows the remaining rules")

		again := server.request(t, http.MethodDelete, rulesPath+"/"+ruleID, ownerID.String(), nil)
		require.Equal(t, http.StatusNotFound, again.Code, "removing the same rule again is 404")
		assert.Equal(t, "category_rule_not_found", errorCode(t, again))

		forbidden := server.request(t, http.MethodPost, rulesPath, uuid.New().String(), map[string]any{"keyword": "uber"})
		assert.Equal(t, http.StatusForbidden, forbidden.Code, "another user cannot touch the category rules")
	})
}

func TestRenameCategory(t *testing.T) {
	t.Run("should answer 200 with the new name", func(t *testing.T) {
		ownerID := uuid.New()
		target := newCategoryFor(t, ownerID)
		categoryID := target.ID()
		server := newTestServer(t, func(server *testServer) {
			server.categories = newFakeCategories(target)
		})

		recorder := server.request(t, http.MethodPatch, "/v1/categories/"+categoryID.String(), ownerID.String(),
			map[string]any{"name": "Meals"})

		require.Equal(t, http.StatusOK, recorder.Code, recorder.Body.String())
		assert.Equal(t, "Meals", decode(t, recorder)["name"])
	})
}

func TestMoveCategory(t *testing.T) {
	ownerID := uuid.New()

	t.Run("should turn a root category into a subcategory", func(t *testing.T) {
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

	t.Run("should turn a subcategory back into a root category, since a null parent_id is an explicit request", func(t *testing.T) {
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

	t.Run("should answer 422 for a category as its own parent", func(t *testing.T) {
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
