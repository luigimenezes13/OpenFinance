package shared_test

import (
	"fmt"
	"testing"

	"github.com/luigimenezes13/financial-manager/internal/financialtracking/domain/shared"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

// mustCurrency constrói uma Currency válida ou falha o teste na hora.
// Helper de setup: erro aqui é bug do TESTE, não do código sob teste —
// por isso require (aborta) em vez de assert (continua).
func mustCurrency(t *testing.T, code string) shared.Currency {
	t.Helper()
	currency, err := shared.NewCurrency(code)
	require.NoError(t, err)
	return currency
}

// mustMoney constrói um Money válido ou falha o teste na hora.
func mustMoney(t *testing.T, amount int64, code string) shared.Money {
	t.Helper()
	money, err := shared.NewMoney(amount, mustCurrency(t, code))
	require.NoError(t, err)
	return money
}

// TestNewMoney cobre os caminhos do construtor: qualquer int64 é aceito
// (crédito, débito, zero); só a Currency zero value é recusada.
func TestNewMoney(t *testing.T) {
	t.Parallel()

	brl := mustCurrency(t, "BRL")

	cases := []struct {
		name     string
		amount   int64
		currency shared.Currency
		wantErr  error // nil = espera sucesso
	}{
		{"crédito positivo", 2000, brl, nil},
		{"débito negativo é válido", -2000, brl, nil},
		{"quantia zero é válida", 0, brl, nil},
		{"currency zero value recusada", 100, shared.Currency{}, shared.ErrInvalidCurrency},
	}

	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			t.Parallel()

			got, err := shared.NewMoney(tc.amount, tc.currency)

			if tc.wantErr != nil {
				require.ErrorIs(t, err, tc.wantErr)
				return
			}
			require.NoError(t, err)
			assert.Equal(t, tc.amount, got.Amount())
			assert.True(t, got.Currency().Equals(tc.currency))
		})
	}
}

// TestMoneyAdd — soma preserva a moeda e recusa moedas diferentes.
func TestMoneyAdd(t *testing.T) {
	t.Parallel()

	cases := []struct {
		name    string
		a       shared.Money
		b       shared.Money
		want    int64
		wantErr error
	}{
		{"soma créditos", mustMoney(t, 1000, "BRL"), mustMoney(t, 500, "BRL"), 1500, nil},
		{"crédito com débito", mustMoney(t, 1000, "BRL"), mustMoney(t, -300, "BRL"), 700, nil},
		{"moedas diferentes", mustMoney(t, 1000, "BRL"), mustMoney(t, 500, "USD"), 0, shared.ErrCurrencyMismatch},
	}

	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			t.Parallel()

			got, err := tc.a.Add(tc.b)

			if tc.wantErr != nil {
				require.ErrorIs(t, err, tc.wantErr)
				return
			}
			require.NoError(t, err)
			assert.Equal(t, tc.want, got.Amount())
			assert.True(t, got.Currency().Equals(tc.a.Currency()))
		})
	}

	// Contrato de imutabilidade: Add devolve uma NOVA Money. Se alguém
	// trocar o receiver pra ponteiro e mutar, este teste quebra.
	t.Run("não muta os operandos", func(t *testing.T) {
		t.Parallel()

		dez := mustMoney(t, 1000, "BRL")
		cinco := mustMoney(t, 500, "BRL")

		_, err := dez.Add(cinco)

		require.NoError(t, err)
		assert.Equal(t, int64(1000), dez.Amount())
		assert.Equal(t, int64(500), cinco.Amount())
	})
}

// TestMoneySubtract — análogo ao Add; resultado pode cruzar o zero.
func TestMoneySubtract(t *testing.T) {
	t.Parallel()

	cases := []struct {
		name    string
		a       shared.Money
		b       shared.Money
		want    int64
		wantErr error
	}{
		{"resultado positivo", mustMoney(t, 1000, "BRL"), mustMoney(t, 300, "BRL"), 700, nil},
		{"resultado negativo cruza o zero", mustMoney(t, 300, "BRL"), mustMoney(t, 1000, "BRL"), -700, nil},
		{"moedas diferentes", mustMoney(t, 1000, "BRL"), mustMoney(t, 500, "USD"), 0, shared.ErrCurrencyMismatch},
	}

	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			t.Parallel()

			got, err := tc.a.Subtract(tc.b)

			if tc.wantErr != nil {
				require.ErrorIs(t, err, tc.wantErr)
				return
			}
			require.NoError(t, err)
			assert.Equal(t, tc.want, got.Amount())
			assert.True(t, got.Currency().Equals(tc.a.Currency()))
		})
	}
}

// TestMoneyIsZero — predicado binário, cenários discretos. Sem tabela.
func TestMoneyIsZero(t *testing.T) {
	t.Parallel()

	t.Run("quantia zero", func(t *testing.T) {
		t.Parallel()
		assert.True(t, mustMoney(t, 0, "BRL").IsZero())
	})

	t.Run("quantia positiva", func(t *testing.T) {
		t.Parallel()
		assert.False(t, mustMoney(t, 1, "BRL").IsZero())
	})

	t.Run("quantia negativa", func(t *testing.T) {
		t.Parallel()
		assert.False(t, mustMoney(t, -1, "BRL").IsZero())
	})
}

// TestMoneyEquals — igualdade por valor exige amount E currency iguais.
func TestMoneyEquals(t *testing.T) {
	t.Parallel()

	t.Run("mesmo amount e mesma moeda", func(t *testing.T) {
		t.Parallel()
		assert.True(t, mustMoney(t, 1000, "BRL").Equals(mustMoney(t, 1000, "BRL")))
	})

	t.Run("amount diferente", func(t *testing.T) {
		t.Parallel()
		assert.False(t, mustMoney(t, 1000, "BRL").Equals(mustMoney(t, 999, "BRL")))
	})

	t.Run("mesmo amount em moeda diferente", func(t *testing.T) {
		t.Parallel()
		assert.False(t, mustMoney(t, 1000, "BRL").Equals(mustMoney(t, 1000, "USD")))
	})
}

// TestMoneyString — os casos de sinal e padding são o coração deste teste:
// módulo de negativo em Go é negativo, então formatação ingênua produziria
// "BRL -12.-34" e perderia o sinal em "-0.50".
func TestMoneyString(t *testing.T) {
	t.Parallel()

	cases := []struct {
		name   string
		amount int64
		code   string
		want   string
	}{
		{"quantia com centavos", 1234, "BRL", "BRL 12.34"},
		{"centavos com zero à esquerda", 1205, "BRL", "BRL 12.05"},
		{"apenas centavos", 5, "BRL", "BRL 0.05"},
		{"zero", 0, "BRL", "BRL 0.00"},
		{"negativo leva o sinal no número", -1234, "BRL", "BRL -12.34"},
		{"negativo menor que um real preserva o sinal", -50, "BRL", "BRL -0.50"},
		{"moeda aparece no prefixo", 100000, "USD", "USD 1000.00"},
	}

	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			t.Parallel()
			assert.Equal(t, tc.want, mustMoney(t, tc.amount, tc.code).String())
		})
	}

	t.Run("satisfaz fmt.Stringer via Sprintf", func(t *testing.T) {
		t.Parallel()
		assert.Equal(t, "BRL 12.34", fmt.Sprintf("%s", mustMoney(t, 1234, "BRL")))
	})
}
