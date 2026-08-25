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

	require.Len(t, transactions.saved, 2)
	assert.Equal(t,
		[]string{transaction.EventTypeImported, transaction.EventTypeImported},
		dispatcher.eventNames(),
		"cada transação importada emite Imported",
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
	assert.Empty(t, dispatcher.dispatched)
	assert.True(t, provider.calledSince.IsZero(), "Since zero = todo o histórico")
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
