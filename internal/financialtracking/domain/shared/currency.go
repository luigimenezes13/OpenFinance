// Package shared contém Value Objects compartilhados entre os aggregates
// do Financial Tracking BC.
package shared

import "errors"

// ErrInvalidCurrency indica que o código informado não é uma moeda suportada
// ou não está no formato canônico (3 letras maiúsculas ISO 4217).
var ErrInvalidCurrency = errors.New("invalid currency")

var supportedCurrencies = map[string]struct{}{
	"BRL": {},
	"USD": {},
}

// Currency é o VO de moeda. Imutável, comparável por valor.
type Currency struct {
	code string
}

// NewCurrency valida e retorna um Currency. Único caminho de criação válido.
// Recusa códigos em lowercase: o chamador envia no formato canônico maiúsculo.
func NewCurrency(code string) (Currency, error) {
	if len(code) != 3 {
		return Currency{}, ErrInvalidCurrency
	}
	if _, ok := supportedCurrencies[code]; !ok {
		return Currency{}, ErrInvalidCurrency
	}
	return Currency{code: code}, nil
}

// Code retorna o código ISO 4217 da moeda.
func (c Currency) Code() string {
	return c.code
}

// String implementa fmt.Stringer.
func (c Currency) String() string {
	return c.code
}

// Equals compara dois Currency por valor.
func (c Currency) Equals(other Currency) bool {
	return c.code == other.code
}

// IsZero informa se este Currency é o zero value, ou seja, foi criado
// fora do NewCurrency e não representa moeda alguma.
func (c Currency) IsZero() bool {
	return c.code == ""
}
