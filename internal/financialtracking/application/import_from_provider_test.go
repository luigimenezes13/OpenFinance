package application_test

import (
	"context"
	"testing"
	"time"

	"github.com/google/uuid"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/luigimenezes13/financial-manager/internal/financialtracking/application"
	"github.com/luigimenezes13/financial-manager/internal/financialtracking/domain/account"
	"github.com/luigimenezes13/financial-manager/internal/financialtracking/domain/openfinance"
	"github.com/luigimenezes13/financial-manager/internal/financialtracking/domain/shared"
	"github.com/luigimenezes13/financial-manager/internal/financialtracking/domain/transaction"
)

// providerTransaction monta o DTO de fronteira já normalizado (é o que o
// adapter entrega: centavos, sinal patrimônio, UTC).
func providerTransaction(providerID string, amountInCents int64, description string) openfinance.ProviderTransaction {
	return openfinance.ProviderTransaction{
		ProviderTransactionID: providerID,
		Description:           description,
		AmountInCents:         amountInCents,
		CurrencyCode:          "BRL",
		OccurredAt:            time.Now().Add(-24 * time.Hour).UTC(),
	}
}

// TestImportFromProviderUseCase_Execute cobre o caminho feliz: duas
// transações importadas, cada uma emitindo Imported, e o provider consultado
// com a referência que veio do VO Source (não do input).
func TestImportFromProviderUseCase_Execute(t *testing.T) {
	t.Parallel()

	ownerID := uuid.New()
	connected := newConnectedAccount(t, ownerID, "pluggy", "pluggy-acc-77", "BRL")
	accountID := connected.ID()

	provider := &fakeProvider{
		name: "pluggy",
		transactions: []openfinance.ProviderTransaction{
			providerTransaction("tx-1", -8990, "Mercado Livre"),
			providerTransaction("tx-2", 350000, "Salário"),
		},
	}
	accounts := newFakeAccounts(connected)
	transactions := newFakeTransactions()
	dispatcher := &fakeDispatcher{}
	useCase := application.NewImportFromProviderUseCase(transactions, accounts, provider, dispatcher)

	since := time.Now().Add(-30 * 24 * time.Hour)
	output, err := useCase.Execute(context.Background(), application.ImportFromProviderInput{
		UserID:    ownerID,
		AccountID: accountID.UUID(),
		Since:     since,
	})

	require.NoError(t, err)
	assert.Equal(t, accountID.String(), output.AccountID)
	assert.Equal(t, "pluggy", output.Provider)
	require.Len(t, output.TransactionIDs, 2)

	assert.Equal(t, "pluggy-acc-77", provider.calledAccountID, "a referência do provider sai do VO Source da conta")
	assert.WithinDuration(t, since, provider.calledSince, time.Second)
	assert.Equal(t, 1, provider.balanceRequests, "o saldo é consultado uma vez por importação")

	require.Len(t, transactions.saved, 2)
	assert.Equal(t,
		[]string{transaction.EventTypeImported, transaction.EventTypeImported, account.EventTypeBalanceUpdated},
		dispatcher.eventNames(),
		"cada transação emite Imported, e o saldo do provedor emite BalanceUpdated no fim",
	)

	imported := transactions.saved[0]
	assert.Empty(t, imported.Events(), "ClearEvents roda depois do dispatch")
	importedRef, ok := imported.ExternalRef()
	require.True(t, ok, "transação de provider nasce com referência externa")
	assert.Equal(t, "pluggy", importedRef.Provider())
	assert.Equal(t, "tx-1", importedRef.ProviderTransactionID())
}

// TestImportFromProviderUseCase_Execute_SemNovidade: lista vazia é resultado
// legítimo, não erro.
func TestImportFromProviderUseCase_Execute_SemNovidade(t *testing.T) {
	t.Parallel()

	ownerID := uuid.New()
	connected := newConnectedAccount(t, ownerID, "pluggy", "pluggy-acc-77", "BRL")
	accountID := connected.ID()

	provider := &fakeProvider{name: "pluggy"}
	transactions := newFakeTransactions()
	dispatcher := &fakeDispatcher{}
	useCase := application.NewImportFromProviderUseCase(transactions, newFakeAccounts(connected), provider, dispatcher)

	output, err := useCase.Execute(context.Background(), application.ImportFromProviderInput{
		UserID:    ownerID,
		AccountID: accountID.UUID(),
	})

	require.NoError(t, err)
	assert.Empty(t, output.TransactionIDs)
	assert.Empty(t, transactions.saved)
	assert.True(t, provider.calledSince.IsZero(), "Since zero = todo o histórico")

	// Sem transação nova, MAS o saldo é atualizado: são duas perguntas
	// independentes ao provedor, e o saldo pode mudar sem lançamento novo na
	// janela consultada (uma transação antiga liquidada, por exemplo).
	assert.Equal(t, []string{account.EventTypeBalanceUpdated}, dispatcher.eventNames())
	assert.True(t, output.BalanceApplied)
}

// TestImportFromProviderUseCase_Execute_Recusa cobre as recusas que são
// específicas deste use case: conta manual, conta de outro provider, conta
// de outro usuário e moeda divergente na fronteira.
func TestImportFromProviderUseCase_Execute_Recusa(t *testing.T) {
	t.Parallel()

	t.Run("conta manual não importa", func(t *testing.T) {
		t.Parallel()

		ownerID := uuid.New()
		manual := newManualAccount(t, ownerID, "BRL")
		accountID := manual.ID()
		transactions := newFakeTransactions()
		provider := &fakeProvider{name: "pluggy"}
		useCase := application.NewImportFromProviderUseCase(transactions, newFakeAccounts(manual), provider, &fakeDispatcher{})

		_, err := useCase.Execute(context.Background(), application.ImportFromProviderInput{
			UserID: ownerID, AccountID: accountID.UUID(),
		})

		require.ErrorIs(t, err, openfinance.ErrAccountNotConnected)
		assert.Empty(t, provider.calledAccountID, "nem chega a consultar o provider")
	})

	t.Run("conta de outro provider", func(t *testing.T) {
		t.Parallel()

		ownerID := uuid.New()
		connected := newConnectedAccount(t, ownerID, "belvo", "belvo-acc-1", "BRL")
		accountID := connected.ID()
		provider := &fakeProvider{name: "pluggy"}
		useCase := application.NewImportFromProviderUseCase(newFakeTransactions(), newFakeAccounts(connected), provider, &fakeDispatcher{})

		_, err := useCase.Execute(context.Background(), application.ImportFromProviderInput{
			UserID: ownerID, AccountID: accountID.UUID(),
		})

		require.ErrorIs(t, err, openfinance.ErrProviderMismatch)
	})

	t.Run("conta de outro usuário", func(t *testing.T) {
		t.Parallel()

		connected := newConnectedAccount(t, uuid.New(), "pluggy", "pluggy-acc-77", "BRL")
		accountID := connected.ID()
		useCase := application.NewImportFromProviderUseCase(newFakeTransactions(), newFakeAccounts(connected), &fakeProvider{name: "pluggy"}, &fakeDispatcher{})

		_, err := useCase.Execute(context.Background(), application.ImportFromProviderInput{
			UserID: uuid.New(), AccountID: accountID.UUID(),
		})

		require.ErrorIs(t, err, shared.ErrForbidden)
	})

	t.Run("moeda divergente na fronteira", func(t *testing.T) {
		t.Parallel()

		ownerID := uuid.New()
		connected := newConnectedAccount(t, ownerID, "pluggy", "pluggy-acc-77", "BRL")
		accountID := connected.ID()

		dollarTransaction := providerTransaction("tx-usd", -1000, "Steam")
		dollarTransaction.CurrencyCode = "USD"
		provider := &fakeProvider{name: "pluggy", transactions: []openfinance.ProviderTransaction{dollarTransaction}}
		transactions := newFakeTransactions()
		useCase := application.NewImportFromProviderUseCase(transactions, newFakeAccounts(connected), provider, &fakeDispatcher{})

		_, err := useCase.Execute(context.Background(), application.ImportFromProviderInput{
			UserID: ownerID, AccountID: accountID.UUID(),
		})

		require.ErrorIs(t, err, account.ErrCurrencyMismatch)
		assert.Empty(t, transactions.saved)
	})

	t.Run("conta inexistente", func(t *testing.T) {
		t.Parallel()

		useCase := application.NewImportFromProviderUseCase(newFakeTransactions(), newFakeAccounts(), &fakeProvider{name: "pluggy"}, &fakeDispatcher{})

		_, err := useCase.Execute(context.Background(), application.ImportFromProviderInput{
			UserID: uuid.New(), AccountID: uuid.New(),
		})

		require.ErrorIs(t, err, account.ErrNotFound)
	})
}

// TestImportFromProviderUseCase_Execute_AbortaNoPrimeiroErro documenta a
// decisão do v1: falha no meio da importação aborta tudo. O que já foi
// salvo FICA salvo (não há transação de banco no v1) — e é justamente isso
// que o PR4 precisa resolver junto com a idempotência.
func TestImportFromProviderUseCase_Execute_AbortaNoPrimeiroErro(t *testing.T) {
	t.Parallel()

	ownerID := uuid.New()
	connected := newConnectedAccount(t, ownerID, "pluggy", "pluggy-acc-77", "BRL")
	accountID := connected.ID()

	invalid := providerTransaction("", -500, "Sem referência externa")
	provider := &fakeProvider{
		name: "pluggy",
		transactions: []openfinance.ProviderTransaction{
			providerTransaction("tx-ok", -1500, "Uber"),
			invalid,
			providerTransaction("tx-nunca-chega", -2500, "iFood"),
		},
	}
	transactions := newFakeTransactions()
	dispatcher := &fakeDispatcher{}
	useCase := application.NewImportFromProviderUseCase(transactions, newFakeAccounts(connected), provider, dispatcher)

	_, err := useCase.Execute(context.Background(), application.ImportFromProviderInput{
		UserID: ownerID, AccountID: accountID.UUID(),
	})

	require.ErrorIs(t, err, transaction.ErrInvalidRef)
	assert.Len(t, transactions.saved, 1, "a primeira já tinha sido persistida")
	assert.Len(t, dispatcher.dispatched, 1)
}

// TestImportFromProviderUseCase_Execute_PropagaErroDeInfra cobre as três
// fronteiras que podem falhar: ler a conta, consultar o provider e gravar.
func TestImportFromProviderUseCase_Execute_PropagaErroDeInfra(t *testing.T) {
	t.Parallel()

	cases := []struct {
		name    string
		arrange func(accounts *fakeAccounts, transactions *fakeTransactions, provider *fakeProvider)
		wantErr error
	}{
		{
			name: "falha lendo a conta",
			arrange: func(accounts *fakeAccounts, _ *fakeTransactions, _ *fakeProvider) {
				accounts.findErr = errInfra
			},
			wantErr: errInfra,
		},
		{
			name: "provider indisponível",
			arrange: func(_ *fakeAccounts, _ *fakeTransactions, provider *fakeProvider) {
				provider.fetchErr = openfinance.ErrProviderUnavailable
			},
			wantErr: openfinance.ErrProviderUnavailable,
		},
		{
			name: "falha gravando a transação",
			arrange: func(_ *fakeAccounts, transactions *fakeTransactions, _ *fakeProvider) {
				transactions.saveErr = errInfra
			},
			wantErr: errInfra,
		},
	}

	for _, testCase := range cases {
		t.Run(testCase.name, func(t *testing.T) {
			t.Parallel()

			ownerID := uuid.New()
			connected := newConnectedAccount(t, ownerID, "pluggy", "pluggy-acc-77", "BRL")
			accountID := connected.ID()

			accounts := newFakeAccounts(connected)
			transactions := newFakeTransactions()
			provider := &fakeProvider{
				name:         "pluggy",
				transactions: []openfinance.ProviderTransaction{providerTransaction("tx-1", -1000, "Uber")},
			}
			testCase.arrange(accounts, transactions, provider)

			useCase := application.NewImportFromProviderUseCase(transactions, accounts, provider, &fakeDispatcher{})
			_, err := useCase.Execute(context.Background(), application.ImportFromProviderInput{
				UserID: ownerID, AccountID: accountID.UUID(),
			})

			require.ErrorIs(t, err, testCase.wantErr)
		})
	}
}

// TestImportFromProviderUseCase_SaldoVemDoProvedor é o teste da REGRA
// central: o saldo da conta muda por este caminho e só por ele.
func TestImportFromProviderUseCase_SaldoVemDoProvedor(t *testing.T) {
	t.Parallel()

	ownerID := uuid.New()
	connected := newConnectedAccount(t, ownerID, "pluggy", "pluggy-acc-77", "BRL")
	accountID := connected.ID()

	provider := &fakeProvider{
		name: "pluggy",
		balance: &openfinance.ProviderBalance{
			AmountInCents: 1_234_56,
			CurrencyCode:  "BRL",
			AsOf:          time.Now(),
		},
	}
	accounts := newFakeAccounts(connected)
	dispatcher := &fakeDispatcher{}
	useCase := application.NewImportFromProviderUseCase(newFakeTransactions(), accounts, provider, dispatcher)

	output, err := useCase.Execute(context.Background(), application.ImportFromProviderInput{
		UserID: ownerID, AccountID: accountID.UUID(),
	})

	require.NoError(t, err)
	assert.Equal(t, int64(1_234_56), output.Balance)
	assert.Equal(t, "BRL", output.Currency)
	assert.True(t, output.BalanceApplied)

	// O saldo foi para o aggregate E para o repositório.
	balance := connected.Balance()
	money := balance.Money()
	assert.Equal(t, int64(1_234_56), money.Amount())
	require.Len(t, accounts.saved, 1, "conta com saldo novo é persistida")

	// E o evento que existia só no papel agora dispara de verdade.
	require.Len(t, dispatcher.dispatched, 1)
	assert.Equal(t, account.EventTypeBalanceUpdated, dispatcher.dispatched[0].EventName())
}

// TestImportFromProviderUseCase_SaldoNegativo cobre a convenção patrimônio:
// dívida de cartão é NEGATIVA, e o adapter normaliza a fatura positiva da
// Pluggy antes de chegar aqui.
func TestImportFromProviderUseCase_SaldoNegativo(t *testing.T) {
	t.Parallel()

	ownerID := uuid.New()
	connected := newConnectedAccount(t, ownerID, "pluggy", "pluggy-acc-77", "BRL")
	accountID := connected.ID()

	provider := &fakeProvider{
		name:    "pluggy",
		balance: &openfinance.ProviderBalance{AmountInCents: -4_500_00, CurrencyCode: "BRL", AsOf: time.Now()},
	}
	useCase := application.NewImportFromProviderUseCase(newFakeTransactions(), newFakeAccounts(connected), provider, &fakeDispatcher{})

	output, err := useCase.Execute(context.Background(), application.ImportFromProviderInput{
		UserID: ownerID, AccountID: accountID.UUID(),
	})

	require.NoError(t, err)
	assert.Equal(t, int64(-4_500_00), output.Balance)
}

// TestImportFromProviderUseCase_SaldoAntigoEhIgnorado: webhooks e syncs
// chegam fora de ordem. Saldo mais velho que o registrado é recusado pelo
// aggregate e o use case SEGUE — não é falha, e o output diz que não aplicou.
func TestImportFromProviderUseCase_SaldoAntigoEhIgnorado(t *testing.T) {
	t.Parallel()

	ownerID := uuid.New()
	connected := newConnectedAccount(t, ownerID, "pluggy", "pluggy-acc-77", "BRL")
	accountID := connected.ID()

	// Primeiro, um saldo recente.
	recent, err := account.NewBalance(mustMoney(t, 500_00, "BRL"), time.Now())
	require.NoError(t, err)
	require.NoError(t, connected.UpdateBalance(recent))
	connected.ClearEvents()

	// Agora o provedor responde com um saldo de ontem.
	provider := &fakeProvider{
		name: "pluggy",
		balance: &openfinance.ProviderBalance{
			AmountInCents: 1_00,
			CurrencyCode:  "BRL",
			AsOf:          time.Now().Add(-24 * time.Hour),
		},
	}
	accounts := newFakeAccounts(connected)
	dispatcher := &fakeDispatcher{}
	useCase := application.NewImportFromProviderUseCase(newFakeTransactions(), accounts, provider, dispatcher)

	output, err := useCase.Execute(context.Background(), application.ImportFromProviderInput{
		UserID: ownerID, AccountID: accountID.UUID(),
	})

	require.NoError(t, err, "saldo fora de ordem não é erro")
	assert.False(t, output.BalanceApplied)

	balance := connected.Balance()
	money := balance.Money()
	assert.Equal(t, int64(500_00), money.Amount(), "o saldo mais recente é preservado")
	assert.Empty(t, accounts.saved, "nada a persistir")
	assert.Empty(t, dispatcher.dispatched, "nada aconteceu, nenhum evento")
}

// TestImportFromProviderUseCase_SaldoInvalido cobre as recusas do saldo.
func TestImportFromProviderUseCase_SaldoInvalido(t *testing.T) {
	t.Parallel()

	cases := []struct {
		name    string
		arrange func(provider *fakeProvider)
		wantErr error
	}{
		{
			name: "moeda divergente",
			arrange: func(provider *fakeProvider) {
				provider.balance = &openfinance.ProviderBalance{AmountInCents: 100, CurrencyCode: "USD", AsOf: time.Now()}
			},
			wantErr: account.ErrCurrencyMismatch,
		},
		{
			name: "instante ausente",
			arrange: func(provider *fakeProvider) {
				provider.balance = &openfinance.ProviderBalance{AmountInCents: 100, CurrencyCode: "BRL"}
			},
			wantErr: account.ErrInvalidBalance,
		},
		{
			name: "provider indisponível na consulta de saldo",
			arrange: func(provider *fakeProvider) {
				provider.balanceErr = openfinance.ErrProviderUnavailable
			},
			wantErr: openfinance.ErrProviderUnavailable,
		},
	}

	for _, testCase := range cases {
		t.Run(testCase.name, func(t *testing.T) {
			t.Parallel()

			ownerID := uuid.New()
			connected := newConnectedAccount(t, ownerID, "pluggy", "pluggy-acc-77", "BRL")
			accountID := connected.ID()
			provider := &fakeProvider{name: "pluggy"}
			testCase.arrange(provider)

			accounts := newFakeAccounts(connected)
			useCase := application.NewImportFromProviderUseCase(newFakeTransactions(), accounts, provider, &fakeDispatcher{})

			_, err := useCase.Execute(context.Background(), application.ImportFromProviderInput{
				UserID: ownerID, AccountID: accountID.UUID(),
			})

			require.ErrorIs(t, err, testCase.wantErr)
			assert.Empty(t, accounts.saved)
		})
	}
}
