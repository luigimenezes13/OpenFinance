package shared

import (
	"errors"
	"fmt"
)

// ErrCurrencyMismatch indica tentativa de operação entre Money de moedas diferentes.
var ErrCurrencyMismatch = errors.New("currency mismatch")

// Money é o VO de quantia monetária em CENTAVOS (int64).
// Floats são proibidos em domínio financeiro (precisão).
// Composição: Money contém Currency por valor — imutabilidade aninhada.
//
// DDD: Value Object — imutável, sem identidade, auto-validado.
type Money struct {
	amount   int64
	currency Currency
}

// NewMoney constrói um Money válido. Amount aceita qualquer int64
// (positivo=crédito, negativo=débito, zero=válido); quem decide se zero
// é problema é o aggregate que usa o Money, não o Money em si.
func NewMoney(amount int64, currency Currency) (Money, error) {
	if currency.IsZero() {
		return Money{}, ErrInvalidCurrency
	}
	return Money{amount: amount, currency: currency}, nil
}

// Amount retorna o valor em centavos.
func (m Money) Amount() int64 {
	return m.amount
}

// Currency retorna a moeda. Receiver de valor + retorno por valor = ninguém muta.
func (m Money) Currency() Currency {
	return m.currency
}

// Add devolve uma NOVA Money = m + other. Nunca muta m nem other.
// Moedas diferentes → ErrCurrencyMismatch.
// TODO: overflow de int64 não é tratado na v1 (centavos cabem em int64
// até ~92 quatrilhões de reais).
func (m Money) Add(other Money) (Money, error) {
	if !m.currency.Equals(other.currency) {
		return Money{}, ErrCurrencyMismatch
	}
	return Money{amount: m.amount + other.amount, currency: m.currency}, nil
}

// Subtract devolve uma NOVA Money = m - other. Mesma regra de currency do Add.
func (m Money) Subtract(other Money) (Money, error) {
	if !m.currency.Equals(other.currency) {
		return Money{}, ErrCurrencyMismatch
	}
	return Money{amount: m.amount - other.amount, currency: m.currency}, nil
}

// IsZero retorna true se a quantia é zero (independente da moeda).
func (m Money) IsZero() bool {
	return m.amount == 0
}

// Equals compara duas Money por valor (amount + currency).
func (m Money) Equals(other Money) bool {
	return m.amount == other.amount && m.currency.Equals(other.currency)
}

// String formata pra leitura humana: "BRL 1234.56". Amount negativo leva o
// sinal no número ("BRL -12.34"), normalizado antes da divisão pra o sinal
// não vazar duplicado nos centavos (-1234%100 == -34 em Go).
func (m Money) String() string {
	sign := ""
	amount := m.amount
	if amount < 0 {
		sign = "-"
		amount = -amount
	}
	return fmt.Sprintf("%s %s%d.%02d", m.currency.Code(), sign, amount/100, amount%100)
}
