package account_test

import (
	"testing"
	"time"

	"github.com/luigimenezes13/financial-manager/internal/financialtracking/domain/account"
	"github.com/luigimenezes13/financial-manager/internal/financialtracking/domain/shared"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

// TestNewBalance — saldo R$ 0,00 é válido; o que se recusa é VO malformado.
func TestNewBalance(t *testing.T) {
	t.Parallel()

	now := time.Now()

	cases := []struct {
		name    string
		money   shared.Money
		asOf    time.Time
		wantErr error
	}{
		{"saldo positivo", mustMoney(t, 150_00, "BRL"), now, nil},
		{"saldo zero é válido", mustMoney(t, 0, "BRL"), now, nil},
		{"dívida (negativo, convenção patrimônio)", mustMoney(t, -300_00, "BRL"), now, nil},
		{"Money zero value", shared.Money{}, now, account.ErrInvalidBalance},
		{"asOf zero", mustMoney(t, 150_00, "BRL"), time.Time{}, account.ErrInvalidBalance},
	}

	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			t.Parallel()

			got, err := account.NewBalance(tc.money, tc.asOf)

			if tc.wantErr != nil {
				require.ErrorIs(t, err, tc.wantErr)
				return
			}
			require.NoError(t, err)
			assert.True(t, got.Money().Equals(tc.money))
			assert.True(t, got.AsOf().Equal(tc.asOf))
		})
	}
}

// TestBalanceCurrency — o atalho de Demeter devolve a moeda do Money interno.
func TestBalanceCurrency(t *testing.T) {
	t.Parallel()

	balance := mustBalance(t, 100_00, "USD", time.Now())

	assert.Equal(t, "USD", balance.Currency().Code())
}

// TestBalanceIsZero — zero value vs Balance construído.
func TestBalanceIsZero(t *testing.T) {
	t.Parallel()

	t.Run("zero value", func(t *testing.T) {
		t.Parallel()
		assert.True(t, account.Balance{}.IsZero())
	})

	t.Run("saldo construído com quantia zero NÃO é zero value", func(t *testing.T) {
		t.Parallel()
		assert.False(t, mustBalance(t, 0, "BRL", time.Now()).IsZero())
	})
}
