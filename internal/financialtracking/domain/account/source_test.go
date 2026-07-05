package account_test

import (
	"testing"

	"github.com/luigimenezes13/financial-manager/internal/financialtracking/domain/account"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

// TestNewManualSource — origem manual: sem provider, sem ref.
func TestNewManualSource(t *testing.T) {
	t.Parallel()

	manual := account.NewManualSource()

	assert.False(t, manual.IsZero(), "manual é origem válida, não zero value")
	assert.False(t, manual.IsOpenFinance())

	provider, ok := manual.Provider()
	assert.False(t, ok)
	assert.Empty(t, provider)

	providerAccountID, ok := manual.ProviderAccountID()
	assert.False(t, ok)
	assert.Empty(t, providerAccountID)
}

// TestNewOpenFinanceSource — a invariante do VO: openfinance SEMPRE tem ref.
func TestNewOpenFinanceSource(t *testing.T) {
	t.Parallel()

	cases := []struct {
		name              string
		provider          string
		providerAccountID string
		wantErr           error
	}{
		{"happy path", "pluggy", "a1b2c3d4", nil},
		{"provider vazio", "", "a1b2c3d4", account.ErrInvalidSource},
		{"provider só espaços", "   ", "a1b2c3d4", account.ErrInvalidSource},
		{"ref vazia", "pluggy", "", account.ErrInvalidSource},
		{"ref só espaços", "pluggy", "   ", account.ErrInvalidSource},
	}

	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			t.Parallel()

			got, err := account.NewOpenFinanceSource(tc.provider, tc.providerAccountID)

			if tc.wantErr != nil {
				require.ErrorIs(t, err, tc.wantErr)
				return
			}
			require.NoError(t, err)
			assert.True(t, got.IsOpenFinance())

			provider, ok := got.Provider()
			require.True(t, ok)
			assert.Equal(t, tc.provider, provider)

			providerAccountID, ok := got.ProviderAccountID()
			require.True(t, ok)
			assert.Equal(t, tc.providerAccountID, providerAccountID)
		})
	}
}

// TestSourceIsZero — zero value vs origens construídas.
func TestSourceIsZero(t *testing.T) {
	t.Parallel()

	t.Run("zero value", func(t *testing.T) {
		t.Parallel()
		assert.True(t, account.Source{}.IsZero())
	})

	t.Run("origem manual", func(t *testing.T) {
		t.Parallel()
		assert.False(t, account.NewManualSource().IsZero())
	})
}
