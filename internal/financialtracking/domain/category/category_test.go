package category_test

import (
	"testing"

	"github.com/google/uuid"
	"github.com/luigimenezes13/financial-manager/internal/financialtracking/domain/category"
	"github.com/luigimenezes13/financial-manager/internal/financialtracking/domain/shared"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

// Helpers compartilhados por todos os _test.go deste package.

const validUUID = "550e8400-e29b-41d4-a716-446655440000"

func mustUserID(t *testing.T, raw string) shared.UserID {
	t.Helper()
	userID, err := shared.NewUserID(uuid.MustParse(raw))
	require.NoError(t, err)
	return userID
}

func newValidCategory(t *testing.T) *category.Category {
	t.Helper()
	validCategory, err := category.New(mustUserID(t, validUUID), "Transporte", nil)
	require.NoError(t, err)
	return validCategory
}

func mustRule(t *testing.T, keyword string) category.CategoryRule {
	t.Helper()
	rule, err := category.NewCategoryRule(keyword)
	require.NoError(t, err)
	return rule
}

// TestNew cobre as invariantes do construtor do root.
func TestNew(t *testing.T) {
	t.Parallel()

	validUser := mustUserID(t, validUUID)
	validParent := category.NewCategoryID()

	cases := []struct {
		name         string
		userID       shared.UserID
		categoryName string
		parentID     *category.CategoryID
		wantErr      error
	}{
		{"happy path raiz", validUser, "Transporte", nil, nil},
		{"happy path subcategoria", validUser, "Uber", &validParent, nil},
		{"userID zero value", shared.UserID{}, "Transporte", nil, shared.ErrInvalidUserID},
		{"nome vazio", validUser, "", nil, category.ErrInvalidName},
		{"nome só com espaços", validUser, "   ", nil, category.ErrInvalidName},
		{"parent zero value", validUser, "Uber", &category.CategoryID{}, category.ErrInvalidParent},
	}

	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			t.Parallel()

			got, err := category.New(tc.userID, tc.categoryName, tc.parentID)

			if tc.wantErr != nil {
				require.ErrorIs(t, err, tc.wantErr)
				return
			}
			require.NoError(t, err)
			assert.False(t, got.ID().IsZero(), "New deve gerar identidade")
			assert.True(t, got.UserID().Equals(tc.userID))
			assert.Empty(t, got.Rules(), "categoria nasce sem regras")

			if tc.parentID == nil {
				assert.Nil(t, got.ParentID(), "categoria raiz não tem pai")
				return
			}
			require.NotNil(t, got.ParentID())
			assert.True(t, got.ParentID().Equals(*tc.parentID))
		})
	}

	t.Run("nome é normalizado (trim)", func(t *testing.T) {
		t.Parallel()

		got, err := category.New(validUser, "  Transporte  ", nil)

		require.NoError(t, err)
		assert.Equal(t, "Transporte", got.Name())
	})

	t.Run("cada categoria nasce com identidade própria", func(t *testing.T) {
		t.Parallel()

		assert.False(t, newValidCategory(t).ID().Equals(newValidCategory(t).ID()))
	})
}

// TestRename — validação reaproveitada do New.
func TestRename(t *testing.T) {
	t.Parallel()

	t.Run("nome válido troca e normaliza", func(t *testing.T) {
		t.Parallel()

		validCategory := newValidCategory(t)

		require.NoError(t, validCategory.Rename("  Alimentação  "))

		assert.Equal(t, "Alimentação", validCategory.Name())
	})

	t.Run("nome vazio é recusado e mantém o atual", func(t *testing.T) {
		t.Parallel()

		validCategory := newValidCategory(t)

		err := validCategory.Rename("   ")

		require.ErrorIs(t, err, category.ErrInvalidName)
		assert.Equal(t, "Transporte", validCategory.Name())
	})
}

// TestMoveTo — re-parentesco e a invariante de auto-parentesco.
func TestMoveTo(t *testing.T) {
	t.Parallel()

	t.Run("move para um pai válido", func(t *testing.T) {
		t.Parallel()

		validCategory := newValidCategory(t)
		newParent := category.NewCategoryID()

		require.NoError(t, validCategory.MoveTo(&newParent))

		require.NotNil(t, validCategory.ParentID())
		assert.True(t, validCategory.ParentID().Equals(newParent))
	})

	t.Run("mover para nil vira raiz", func(t *testing.T) {
		t.Parallel()

		validCategory := newValidCategory(t)
		parent := category.NewCategoryID()
		require.NoError(t, validCategory.MoveTo(&parent))

		require.NoError(t, validCategory.MoveTo(nil))

		assert.Nil(t, validCategory.ParentID())
	})

	t.Run("pai zero value é recusado", func(t *testing.T) {
		t.Parallel()

		validCategory := newValidCategory(t)

		err := validCategory.MoveTo(&category.CategoryID{})

		require.ErrorIs(t, err, category.ErrInvalidParent)
	})

	t.Run("auto-parentesco é recusado", func(t *testing.T) {
		t.Parallel()

		validCategory := newValidCategory(t)
		selfID := validCategory.ID()

		err := validCategory.MoveTo(&selfID)

		require.ErrorIs(t, err, category.ErrInvalidParent)
	})
}

// TestAddRule — incorporação de regra e as invariantes de fronteira.
func TestAddRule(t *testing.T) {
	t.Parallel()

	t.Run("adiciona regra válida", func(t *testing.T) {
		t.Parallel()

		validCategory := newValidCategory(t)

		require.NoError(t, validCategory.AddRule(mustRule(t, "uber")))

		require.Len(t, validCategory.Rules(), 1)
		assert.Equal(t, "uber", validCategory.Rules()[0].Keyword())
	})

	t.Run("regra zero value é recusada", func(t *testing.T) {
		t.Parallel()

		validCategory := newValidCategory(t)

		err := validCategory.AddRule(category.CategoryRule{})

		require.ErrorIs(t, err, category.ErrInvalidRule)
		assert.Empty(t, validCategory.Rules())
	})

	t.Run("keyword duplicada é recusada", func(t *testing.T) {
		t.Parallel()

		validCategory := newValidCategory(t)
		require.NoError(t, validCategory.AddRule(mustRule(t, "uber")))

		err := validCategory.AddRule(mustRule(t, "uber"))

		require.ErrorIs(t, err, category.ErrDuplicateRule)
		assert.Len(t, validCategory.Rules(), 1, "duplicata não entra")
	})

	t.Run("Rules() devolve cópia, não o slice interno", func(t *testing.T) {
		t.Parallel()

		validCategory := newValidCategory(t)
		require.NoError(t, validCategory.AddRule(mustRule(t, "uber")))

		leaked := validCategory.Rules()
		leaked[0] = category.CategoryRule{}

		fresh := validCategory.Rules()
		require.Len(t, fresh, 1)
		assert.False(t, fresh[0].IsZero(), "mutar o retorno não pode afetar o aggregate")
	})
}

// TestRemoveRule — remoção por ID.
func TestRemoveRule(t *testing.T) {
	t.Parallel()

	t.Run("remove regra existente", func(t *testing.T) {
		t.Parallel()

		validCategory := newValidCategory(t)
		rule := mustRule(t, "uber")
		require.NoError(t, validCategory.AddRule(rule))

		require.NoError(t, validCategory.RemoveRule(rule.ID()))

		assert.Empty(t, validCategory.Rules())
	})

	t.Run("regra inexistente é recusada", func(t *testing.T) {
		t.Parallel()

		validCategory := newValidCategory(t)

		err := validCategory.RemoveRule(category.NewRuleID())

		require.ErrorIs(t, err, category.ErrRuleNotFound)
	})
}
