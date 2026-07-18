package category_test

import (
	"testing"

	"github.com/luigimenezes13/financial-manager/internal/financialtracking/domain/category"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

// TestNewCategoryRule cobre as invariantes do construtor da entity.
func TestNewCategoryRule(t *testing.T) {
	t.Parallel()

	t.Run("keyword válido gera regra com identidade", func(t *testing.T) {
		t.Parallel()

		rule, err := category.NewCategoryRule("uber")

		require.NoError(t, err)
		assert.False(t, rule.ID().IsZero(), "regra nasce com identidade")
		assert.Equal(t, "uber", rule.Keyword())
		assert.False(t, rule.IsZero())
	})

	t.Run("keyword é normalizado (trim)", func(t *testing.T) {
		t.Parallel()

		rule, err := category.NewCategoryRule("  uber  ")

		require.NoError(t, err)
		assert.Equal(t, "uber", rule.Keyword())
	})

	cases := []struct {
		name    string
		keyword string
	}{
		{"vazio", ""},
		{"só espaços", "   "},
	}
	for _, tc := range cases {
		t.Run("keyword "+tc.name+" é recusado", func(t *testing.T) {
			t.Parallel()

			_, err := category.NewCategoryRule(tc.keyword)

			require.ErrorIs(t, err, category.ErrInvalidKeyword)
		})
	}
}

// TestCategoryRuleMatches — a regra de negócio: substring case-insensitive.
func TestCategoryRuleMatches(t *testing.T) {
	t.Parallel()

	rule, err := category.NewCategoryRule("uber")
	require.NoError(t, err)

	cases := []struct {
		name        string
		description string
		want        bool
	}{
		{"match exato", "uber", true},
		{"match como substring", "UBER *TRIP 12345", true},
		{"match ignorando caixa", "Uber Eats", true},
		{"não casa", "spotify premium", false},
		{"descrição vazia não casa", "", false},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			t.Parallel()

			assert.Equal(t, tc.want, rule.Matches(tc.description))
		})
	}
}
