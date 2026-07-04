package shared_test

import (
	"fmt"
	"testing"

	"github.com/luigimenezes13/financial-manager/internal/financialtracking/domain/shared"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

// TestNewCurrency cobre os caminhos do construtor.
//
// Padrão "table-driven": uma slice de casos + loop com t.Run pra cada um.
// Adicionar caso = adicionar linha. Quando falha, aponta exatamente qual.
func TestNewCurrency(t *testing.T) {
	t.Parallel()

	cases := []struct {
		name    string
		input   string
		wantErr error // nil = espera sucesso
	}{
		{"happy path BRL", "BRL", nil},
		{"happy path USD", "USD", nil},
		{"string vazia", "", shared.ErrInvalidCurrency},
		{"tamanho 2", "BR", shared.ErrInvalidCurrency},
		{"tamanho 4", "BRLL", shared.ErrInvalidCurrency},
		{"lowercase recusado", "brl", shared.ErrInvalidCurrency},
		{"moeda não suportada", "EUR", shared.ErrInvalidCurrency},
		{"chars não-letra", "1@#", shared.ErrInvalidCurrency},
	}

	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			t.Parallel()

			got, err := shared.NewCurrency(tc.input)

			if tc.wantErr != nil {
				require.ErrorIs(t, err, tc.wantErr)
				return
			}
			require.NoError(t, err)
			assert.Equal(t, tc.input, got.Code())
		})
	}
}

// TestCurrencyEquals — comparação binária, 3 cenários discretos. Sem tabela.
func TestCurrencyEquals(t *testing.T) {
	t.Parallel()

	brl, err := shared.NewCurrency("BRL")
	require.NoError(t, err)
	outraBRL, err := shared.NewCurrency("BRL")
	require.NoError(t, err)
	usd, err := shared.NewCurrency("USD")
	require.NoError(t, err)

	t.Run("BRL igual a BRL", func(t *testing.T) {
		t.Parallel()
		assert.True(t, brl.Equals(outraBRL))
	})

	t.Run("BRL diferente de USD", func(t *testing.T) {
		t.Parallel()
		assert.False(t, brl.Equals(usd))
	})

	t.Run("BRL diferente de Currency zero value", func(t *testing.T) {
		t.Parallel()
		assert.False(t, brl.Equals(shared.Currency{}))
	})
}

// TestCurrencyString — String() retorna o código e satisfaz fmt.Stringer.
func TestCurrencyString(t *testing.T) {
	t.Parallel()

	brl, err := shared.NewCurrency("BRL")
	require.NoError(t, err)

	t.Run("String retorna o código", func(t *testing.T) {
		t.Parallel()
		assert.Equal(t, "BRL", brl.String())
	})

	t.Run("satisfaz fmt.Stringer via Sprintf", func(t *testing.T) {
		t.Parallel()
		assert.Equal(t, "BRL", fmt.Sprintf("%s", brl))
	})
}
