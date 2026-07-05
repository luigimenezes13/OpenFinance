package account_test

import (
	"testing"

	"github.com/luigimenezes13/financial-manager/internal/financialtracking/domain/account"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

// TestNewKind — reconstrução do enum a partir da forma textual.
func TestNewKind(t *testing.T) {
	t.Parallel()

	cases := []struct {
		name    string
		raw     string
		want    account.Kind
		wantErr error
	}{
		{"conta corrente", "checking", account.KindChecking, nil},
		{"poupança", "savings", account.KindSavings, nil},
		{"cartão de crédito", "credit_card", account.KindCreditCard, nil},
		{"valor desconhecido", "bitcoin_wallet", account.Kind{}, account.ErrInvalidKind},
		{"string vazia", "", account.Kind{}, account.ErrInvalidKind},
		{"case sensitive", "CHECKING", account.Kind{}, account.ErrInvalidKind},
	}

	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			t.Parallel()

			got, err := account.NewKind(tc.raw)

			if tc.wantErr != nil {
				require.ErrorIs(t, err, tc.wantErr)
				return
			}
			require.NoError(t, err)
			assert.Equal(t, tc.want, got)
		})
	}
}

// TestKindString — round-trip com o NewKind: o que String() devolve, NewKind
// aceita de volta (contrato de persistência).
func TestKindString(t *testing.T) {
	t.Parallel()

	kinds := []account.Kind{account.KindChecking, account.KindSavings, account.KindCreditCard}

	for _, kind := range kinds {
		t.Run(kind.String(), func(t *testing.T) {
			t.Parallel()

			reconstructed, err := account.NewKind(kind.String())

			require.NoError(t, err)
			assert.Equal(t, kind, reconstructed)
		})
	}
}

// TestKindIsZero — zero value vs instâncias válidas.
func TestKindIsZero(t *testing.T) {
	t.Parallel()

	t.Run("zero value", func(t *testing.T) {
		t.Parallel()
		assert.True(t, account.Kind{}.IsZero())
	})

	t.Run("instância válida", func(t *testing.T) {
		t.Parallel()
		assert.False(t, account.KindChecking.IsZero())
	})
}
