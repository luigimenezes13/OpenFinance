// Package openfinance é a PORTA de integração com provedores Open Finance
// (Pluggy, Belvo, ...) e o contrato de dados que atravessa essa fronteira.
//
// Fica no domain de propósito (Evans-style: a porta pertence ao domínio,
// a implementação à infraestrutura). O domínio DITA o que precisa saber de
// um provider; nenhum campo aqui é jargão de Pluggy — o adapter é que
// traduz `providerCode`, `DEBIT/CREDIT`, decimal em string e paginação por
// cursor para este contrato. É a Anti-Corruption Layer na prática.
package openfinance

import (
	"context"
	"time"
)

// ProviderTransaction é o dado bruto-mas-já-traduzido que o adapter entrega
// ao use case de importação. Campos exportados porque é um contrato de
// TRANSPORTE na fronteira, não modelo de domínio: quem tem invariante é o
// aggregate Transaction, e o use case só constrói VOs a partir daqui.
//
// Duas normalizações são responsabilidade do ADAPTER, não do use case:
//
//   - AmountInCents já vem na convenção patrimônio (positivo = entrada,
//     negativo = saída). A pegadinha DEBIT/CREDIT da Pluggy (doc §5.2:
//     o sinal depende do tipo da conta) morre no adapter.
//   - AmountInCents é int64 em CENTAVOS. O decimal-em-string da API não
//     cruza esta fronteira (doc §6.2).
type ProviderTransaction struct {
	// ProviderTransactionID é o id da transação no provider. ATENÇÃO
	// (doc §5.5): na Pluggy esse id NÃO é estável — muda se data, descrição
	// ou valor mudarem materialmente. Serve como referência, não como chave
	// de idempotência confiável.
	ProviderTransactionID string

	// Description é a descrição da transação na instituição.
	Description string

	// AmountInCents é a quantia em centavos, sinal na convenção patrimônio.
	AmountInCents int64

	// CurrencyCode é o código ISO 4217 (ex: "BRL"). O use case compara com
	// a moeda da conta local antes de construir Money.
	CurrencyCode string

	// OccurredAt é quando a transação aconteceu na instituição, em UTC.
	OccurredAt time.Time
}

// Provider é a porta de leitura de um provedor Open Finance. Interface
// PEQUENA de propósito (ISP): o v1 só importa transações de uma conta já
// conectada. Consent flow, health da conexão e descoberta de contas entram
// como métodos/portas próprias no PR4 — interface "Deus" de provider seria
// obrigar o mock a implementar o que ninguém chama.
//
// DDD: Port (integração externa) — contrato no domain, adapter na
// infraestrutura.
type Provider interface {
	// Name identifica o provider na forma que o VO account.Source guarda
	// (ex: "pluggy"). É o que permite recusar importar uma conta da Pluggy
	// usando o adapter da Belvo.
	Name() string

	// FetchTransactions devolve as transações da conta NO PROVIDER a partir
	// de `since` (inclusive). since zero = todo o histórico disponível.
	// Erros de transporte/credencial chegam traduzidos nos sentinels deste
	// package — pgx, http.Response e afins não vazam.
	FetchTransactions(ctx context.Context, providerAccountID string, since time.Time) ([]ProviderTransaction, error)
}
