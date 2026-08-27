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

// ProviderBalance é o saldo que o provedor informa para uma conta.
//
// O saldo do sistema vem EXCLUSIVAMENTE daqui (decisão de 2026-08-27):
// lançamento manual não mexe em saldo. A razão é que o banco é a autoridade
// sobre quanto existe na conta — derivar saldo dos lançamentos que o usuário
// digitou produziria um número que discorda do extrato bancário, e o usuário
// confiaria no errado.
type ProviderBalance struct {
	// AmountInCents na convenção patrimônio (positivo = a conta soma ao
	// patrimônio; dívida de cartão é NEGATIVA). É o ADAPTER que normaliza a
	// fatura positiva da Pluggy na entrada.
	AmountInCents int64

	// CurrencyCode ISO 4217. O use case compara com a moeda da conta local
	// antes de construir Money.
	CurrencyCode string

	// AsOf é o instante a que o saldo se refere, em UTC. Obrigatório: sem
	// ele não há como recusar um saldo que chegou fora de ordem, e syncs
	// concorrentes fariam o saldo voltar no tempo.
	AsOf time.Time
}

// Provider é a porta de leitura de um provedor Open Finance. Interface
// PEQUENA de propósito (ISP): o v1 só importa transações e saldo de uma
// conta já conectada. Consent flow, health da conexão e descoberta de contas entram
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

	// FetchBalance devolve o saldo atual da conta no provedor.
	//
	// Método separado do FetchTransactions, e não um campo no retorno dele,
	// porque são duas perguntas independentes: o saldo é o retrato de agora
	// e não depende da janela `since` pedida para as transações. Juntá-los
	// obrigaria a buscar histórico só pra saber o saldo.
	FetchBalance(ctx context.Context, providerAccountID string) (ProviderBalance, error)
}
