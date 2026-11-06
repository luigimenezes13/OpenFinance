package ginhandler_test

import (
	"net/http"
	"testing"

	"github.com/google/uuid"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func TestCreateCategory(t *testing.T) {
	t.Run("should answer 201 for a root category with parent_id as null, not omitted, so the client can tell a root from a missing field", func(t *testing.T) {
		server := newTestServer(t)
		ownerID := uuid.New()

		recorder := server.request(t, http.MethodPost, "/v1/categories", ownerID.String(), map[string]any{
			"name": "  Food  ",
		})

		require.Equal(t, http.StatusCreated, recorder.Code, recorder.Body.String())
		body := decode(t, recorder)
		assert.Equal(t, "Food", body["name"])
		assert.Equal(t, ownerID.String(), body["user_id"])
		assert.Contains(t, body, "parent_id")
		assert.Nil(t, body["parent_id"])
		assert.Len(t, server.categories.stored, 1)
	})

	t.Run("should answer 201 for a subcategory with the optional parent_id", func(t *testing.T) {
		server := newTestServer(t)
		ownerID := uuid.New()
		parentID := uuid.New()

		recorder := server.request(t, http.MethodPost, "/v1/categories", ownerID.String(), map[string]any{
			"name": "Restaurants", "parent_id": parentID.String(),
		})

		require.Equal(t, http.StatusCreated, recorder.Code, recorder.Body.String())
		body := decode(t, recorder)
		assert.Equal(t, parentID.String(), body["parent_id"])
	})
}

func TestCreateCategoryRefusal(t *testing.T) {
	cases := []struct {
		name       string
		body       any
		wantStatus int
		wantCode   string
	}{
		{
			name:       "should answer 400 when the name is missing",
			body:       map[string]any{},
			wantStatus: http.StatusBadRequest,
			wantCode:   "bad_request",
		},
		{
			name:       "should answer 422 for a blank name",
			body:       map[string]any{"name": "   "},
			wantStatus: http.StatusUnprocessableEntity,
			wantCode:   "invalid_category_name",
		},
		{
			name:       "should answer 400 when parent_id is not a uuid, since that is syntax",
			body:       map[string]any{"name": "Restaurants", "parent_id": "abc"},
			wantStatus: http.StatusBadRequest,
			wantCode:   "bad_request",
		},
		{
			name:       "should answer 422 for a nil parent_id, since it is a valid uuid the domain refuses",
			body:       map[string]any{"name": "Restaurants", "parent_id": uuid.Nil.String()},
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
