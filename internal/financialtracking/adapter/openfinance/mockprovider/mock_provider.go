// Package mockprovider implementa a porta openfinance.Provider com dados
// SINTÉTICOS. Existe pra exercitar o fluxo de importação de ponta a ponta
// sem credencial de banco — o adapter real (Pluggy) entra no PR4 e implementa
// a mesma interface, então trocar é uma linha no composition root.
package mockprovider

import (
	"context"
	"fmt"
	"time"

	"github.com/luigimenezes13/financial-manager/internal/financialtracking/domain/openfinance"
)

// Provider gera transações determinísticas.
type Provider struct {
	name  string
	count int
}

// Garante em tempo de compilação que o mock satisfaz a porta.
var _ openfinance.Provider = (*Provider)(nil)

// New cria o mock. O nome "mock" é o que precisa estar no VO Source da
// conta pra importação aceitar — mesma checagem que recusaria uma conta da
// Pluggy sendo importada pelo adapter da Belvo.
func New() *Provider {
	return &Provider{name: "mock", count: 5}
}

// Name identifica o provider.
func (p *Provider) Name() string {
	return p.name
}

// FetchTransactions devolve transações sintéticas já normalizadas: centavos,
// sinal na convenção patrimônio, UTC — exatamente o contrato que o adapter
// real terá que cumprir.
//
// Os ids são DETERMINÍSTICOS (derivados da conta + índice). Isso é
// deliberado: reimportar a mesma janela devolve os mesmos ids e deixa
// visível que o v1 não é idempotente — a duplicação aparece em teste em vez
// de virar surpresa em produção.
func (p *Provider) FetchTransactions(_ context.Context, providerAccountID string, since time.Time) ([]openfinance.ProviderTransaction, error) {
	now := time.Now().UTC()
	transactions := make([]openfinance.ProviderTransaction, 0, p.count)

	for index := 0; index < p.count; index++ {
		occurredAt := now.Add(-time.Duration(index+1) * 24 * time.Hour)
		if occurredAt.Before(since) {
			// Respeita a janela pedida: transação anterior ao `since` não
			// entra. Sem isso, o mock mentiria sobre o contrato da porta.
			continue
		}

		transactions = append(transactions, openfinance.ProviderTransaction{
			ProviderTransactionID: fmt.Sprintf("%s-tx-%d", providerAccountID, index),
			Description:           descriptions[index%len(descriptions)],
			AmountInCents:         amounts[index%len(amounts)],
			CurrencyCode:          "BRL",
			OccurredAt:            occurredAt,
		})
	}

	return transactions, nil
}

// Dados sintéticos: uma entrada (salário) e várias saídas, pra o extrato
// importado ter as duas direções.
var (
	descriptions = []string{"Mercado Livre", "Uber", "Salário", "iFood", "Farmácia"}
	amounts      = []int64{-89_90, -23_50, 350_000, -67_80, -45_20}
)
