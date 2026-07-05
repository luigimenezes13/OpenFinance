package account

import (
	"time"

	"github.com/luigimenezes13/financial-manager/internal/financialtracking/domain/shared"
)

// Balance é o VO de saldo: uma quantia (Money) válida em um instante (asOf).
// Composição de VOs — Balance contém Money que contém Currency, tudo
// imutável por cópia.
//
// CONVENÇÃO DE DOMÍNIO (2026-07-04): Balance significa sempre "quanto esta
// conta SOMA AO PATRIMÔNIO". Conta corrente/poupança: saldo disponível
// (positivo). Cartão de crédito: dívida em aberto como valor NEGATIVO.
// O ACL normaliza na entrada (a Pluggy manda a fatura do cartão positiva) —
// assim nenhum consumidor precisa de "if kind == creditCard" pra interpretar
// o número, e patrimônio total = soma simples dos balances.
//
// DDD: Value Object — imutável, sem identidade, auto-validado.
type Balance struct {
	money shared.Money
	asOf  time.Time
}

// NewBalance valida e constrói um Balance. Quantia R$ 0,00 é VÁLIDA (saldo
// zerado existe); o que é recusado é Money zero value e instante zero.
func NewBalance(money shared.Money, asOf time.Time) (Balance, error) {
	if money.IsZero() {
		return Balance{}, ErrInvalidBalance
	}
	if asOf.IsZero() {
		return Balance{}, ErrInvalidBalance
	}
	return Balance{money: money, asOf: asOf}, nil
}

// Money retorna a quantia do saldo.
func (b Balance) Money() shared.Money {
	return b.money
}

// AsOf retorna o instante a que o saldo se refere.
func (b Balance) AsOf() time.Time {
	return b.asOf
}

// Currency retorna a moeda do saldo. Existe pra evitar a corrente
// balance.Money().Currency() nos chamadores (Lei de Demeter: quem conhece
// a estrutura interna é o próprio Balance).
func (b Balance) Currency() shared.Currency {
	return b.money.Currency()
}

// IsZero informa se este Balance é o zero value (criado fora do NewBalance).
// Um Balance válido sempre tem asOf — é o discriminador mais barato.
func (b Balance) IsZero() bool {
	return b.asOf.IsZero()
}
