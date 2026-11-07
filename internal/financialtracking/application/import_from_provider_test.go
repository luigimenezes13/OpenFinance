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

// providerTransaction builds the boundary DTO already normalized, as the
// adapter delivers it: cents, net-worth sign, UTC.
func providerTransaction(providerID string, amountInCents int64, description string) openfinance.ProviderTransaction {
	return openfinance.ProviderTransaction{
		ProviderTransactionID: providerID,
		Description:           description,
		AmountInCents:         amountInCents,
		CurrencyCode:          "BRL",
		OccurredAt:            time.Now().Add(-24 * time.Hour).UTC(),
	}
}

type importScenario struct {
	ownerID      uuid.UUID
	connected    *account.Account
	accounts     *fakeAccounts
	transactions *fakeTransactions
	provider     *fakeProvider
	unitOfWork   *fakeUnitOfWork
	outbox       *fakeOutbox
}

func newImportScenario(t *testing.T) importScenario {
	t.Helper()

	ownerID := uuid.New()
	connected := newConnectedAccount(t, ownerID, "pluggy", "pluggy-acc-77", "BRL")
	return importScenario{
		ownerID:      ownerID,
		connected:    connected,
		accounts:     newFakeAccounts(connected),
		transactions: newFakeTransactions(),
		provider:     &fakeProvider{name: "pluggy"},
		unitOfWork:   &fakeUnitOfWork{},
		outbox:       &fakeOutbox{},
	}
}

func (s importScenario) execute() (application.ImportFromProviderOutput, error) {
	accountID := s.connected.ID()
	return s.executeFor(application.ImportFromProviderInput{UserID: s.ownerID, AccountID: accountID.UUID()})
}

func (s importScenario) executeFor(input application.ImportFromProviderInput) (application.ImportFromProviderOutput, error) {
	useCase := application.NewImportFromProviderUseCase(s.transactions, s.accounts, s.provider, newTransactionRunner(s.unitOfWork, s.outbox))
	return useCase.Execute(context.Background(), input)
}

func (s importScenario) assertNothingSaved(t *testing.T) {
	t.Helper()
	assert.Empty(t, s.transactions.saved, "no transaction is saved")
	assert.Empty(t, s.accounts.saved, "the account is not saved")
	assert.Empty(t, s.outbox.recorded, "no event is recorded")
}

func TestImportFromProvider(t *testing.T) {
	t.Parallel()

	t.Run("should import every provider transaction using the reference from the account Source, not from the input", func(t *testing.T) {
		t.Parallel()

		scenario := newImportScenario(t)
		scenario.provider.transactions = []openfinance.ProviderTransaction{
			providerTransaction("tx-1", -8990, "Online store"),
			providerTransaction("tx-2", 350000, "Salary"),
		}
		accountID := scenario.connected.ID()
		since := time.Now().Add(-30 * 24 * time.Hour)

		output, err := scenario.executeFor(application.ImportFromProviderInput{
			UserID:    scenario.ownerID,
			AccountID: accountID.UUID(),
			Since:     since,
		})

		require.NoError(t, err)
		assert.Equal(t, accountID.String(), output.AccountID)
		assert.Equal(t, "pluggy", output.Provider)
		require.Len(t, output.TransactionIDs, 2)
		assert.Equal(t, "pluggy-acc-77", scenario.provider.calledAccountID, "the provider reference comes from the account Source")
		assert.WithinDuration(t, since, scenario.provider.calledSince, time.Second)
		assert.Equal(t, 1, scenario.provider.balanceRequests, "the balance is fetched once per import")
		assert.Equal(t, 1, scenario.unitOfWork.runs, "everything is saved in a single unit of work")
		require.Len(t, scenario.transactions.saved, 2)
		assert.Equal(t,
			[]string{transaction.EventTypeImported, transaction.EventTypeImported, account.EventTypeBalanceUpdated},
			scenario.outbox.recordedEventNames(),
			"each imported transaction records its Imported event and the provider balance records BalanceUpdated",
		)
		imported := scenario.transactions.saved[0]
		assert.Empty(t, imported.Events(), "the commit clears the recorded events")
		assert.Empty(t, scenario.connected.Events(), "the commit clears the account events")
		importedRef, ok := imported.ExternalRef()
		require.True(t, ok, "a provider transaction is born with an external reference")
		assert.Equal(t, "pluggy", importedRef.Provider())
		assert.Equal(t, "tx-1", importedRef.ProviderTransactionID())
	})

	t.Run("should fetch transactions and balance from the provider before opening the unit of work, so no lock is held during the network call", func(t *testing.T) {
		t.Parallel()

		scenario := newImportScenario(t)
		order := &callOrder{}
		scenario.provider.order = order
		scenario.unitOfWork.order = order
		scenario.provider.transactions = []openfinance.ProviderTransaction{providerTransaction("tx-1", -1000, "Taxi")}

		_, err := scenario.execute()

		require.NoError(t, err)
		assert.Equal(t,
			[]string{"provider.FetchTransactions", "provider.FetchBalance", "unitOfWork.Run"},
			order.calls,
		)
	})

	t.Run("should update the balance even with no new transaction, since the balance can change without a new entry in the window", func(t *testing.T) {
		t.Parallel()

		scenario := newImportScenario(t)

		output, err := scenario.execute()

		require.NoError(t, err)
		assert.Empty(t, output.TransactionIDs, "an empty list is a legitimate result, not an error")
		assert.Empty(t, scenario.transactions.saved)
		assert.True(t, scenario.provider.calledSince.IsZero(), "a zero Since means the whole history")
		assert.Equal(t, []string{account.EventTypeBalanceUpdated}, scenario.outbox.recordedEventNames())
		assert.True(t, output.BalanceApplied)
	})

	t.Run("should change the account balance through this path, the only one allowed to", func(t *testing.T) {
		t.Parallel()

		scenario := newImportScenario(t)
		scenario.provider.balance = &openfinance.ProviderBalance{AmountInCents: 1_234_56, CurrencyCode: "BRL", AsOf: time.Now()}

		output, err := scenario.execute()

		require.NoError(t, err)
		assert.Equal(t, int64(1_234_56), output.Balance)
		assert.Equal(t, "BRL", output.Currency)
		assert.True(t, output.BalanceApplied)
		balance := scenario.connected.Balance()
		money := balance.Money()
		assert.Equal(t, int64(1_234_56), money.Amount())
		require.Len(t, scenario.accounts.saved, 1, "an account with a new balance is persisted")
		assert.Equal(t, []string{account.EventTypeBalanceUpdated}, scenario.outbox.recordedEventNames())
	})

	t.Run("should accept a negative balance because card debt is negative in the net-worth convention", func(t *testing.T) {
		t.Parallel()

		scenario := newImportScenario(t)
		scenario.provider.balance = &openfinance.ProviderBalance{AmountInCents: -4_500_00, CurrencyCode: "BRL", AsOf: time.Now()}

		output, err := scenario.execute()

		require.NoError(t, err)
		assert.Equal(t, int64(-4_500_00), output.Balance)
	})

	t.Run("should ignore a balance older than the recorded one without failing, since syncs arrive out of order", func(t *testing.T) {
		t.Parallel()

		scenario := newImportScenario(t)
		recent, err := account.NewBalance(mustMoney(t, 500_00, "BRL"), time.Now())
		require.NoError(t, err)
		require.NoError(t, scenario.connected.UpdateBalance(recent))
		scenario.connected.ClearEvents()
		scenario.provider.balance = &openfinance.ProviderBalance{
			AmountInCents: 1_00,
			CurrencyCode:  "BRL",
			AsOf:          time.Now().Add(-24 * time.Hour),
		}

		output, err := scenario.execute()

		require.NoError(t, err, "an out-of-order balance is not an error")
		assert.False(t, output.BalanceApplied)
		balance := scenario.connected.Balance()
		money := balance.Money()
		assert.Equal(t, int64(500_00), money.Amount(), "the most recent balance is preserved")
		assert.Empty(t, scenario.accounts.saved, "nothing to persist")
		assert.Empty(t, scenario.outbox.recorded, "nothing happened, no event")
	})
}

func TestImportFromProviderRefusal(t *testing.T) {
	t.Parallel()

	t.Run("should refuse a manual account without querying the provider", func(t *testing.T) {
		t.Parallel()

		scenario := newImportScenario(t)
		manual := newManualAccount(t, scenario.ownerID, "BRL")
		scenario.accounts = newFakeAccounts(manual)
		accountID := manual.ID()

		_, err := scenario.executeFor(application.ImportFromProviderInput{UserID: scenario.ownerID, AccountID: accountID.UUID()})

		require.ErrorIs(t, err, openfinance.ErrAccountNotConnected)
		assert.Empty(t, scenario.provider.calledAccountID, "the provider is never queried")
		assert.Zero(t, scenario.unitOfWork.runs)
	})

	t.Run("should refuse an account of another provider", func(t *testing.T) {
		t.Parallel()

		scenario := newImportScenario(t)
		otherProvider := newConnectedAccount(t, scenario.ownerID, "belvo", "belvo-acc-1", "BRL")
		scenario.accounts = newFakeAccounts(otherProvider)
		accountID := otherProvider.ID()

		_, err := scenario.executeFor(application.ImportFromProviderInput{UserID: scenario.ownerID, AccountID: accountID.UUID()})

		require.ErrorIs(t, err, openfinance.ErrProviderMismatch)
		assert.Zero(t, scenario.unitOfWork.runs)
	})

	t.Run("should refuse an account of another user", func(t *testing.T) {
		t.Parallel()

		scenario := newImportScenario(t)
		accountID := scenario.connected.ID()

		_, err := scenario.executeFor(application.ImportFromProviderInput{UserID: uuid.New(), AccountID: accountID.UUID()})

		require.ErrorIs(t, err, shared.ErrForbidden)
		assert.Zero(t, scenario.unitOfWork.runs)
	})

	t.Run("should refuse a missing account", func(t *testing.T) {
		t.Parallel()

		scenario := newImportScenario(t)

		_, err := scenario.executeFor(application.ImportFromProviderInput{UserID: uuid.New(), AccountID: uuid.New()})

		require.ErrorIs(t, err, account.ErrNotFound)
		assert.Zero(t, scenario.unitOfWork.runs)
	})
}

func TestImportFromProviderAllOrNothing(t *testing.T) {
	t.Parallel()

	t.Run("should save nothing when one provider transaction has another currency", func(t *testing.T) {
		t.Parallel()

		scenario := newImportScenario(t)
		dollarTransaction := providerTransaction("tx-usd", -1000, "Steam")
		dollarTransaction.CurrencyCode = "USD"
		scenario.provider.transactions = []openfinance.ProviderTransaction{
			providerTransaction("tx-brl", -1500, "Taxi"),
			dollarTransaction,
		}

		_, err := scenario.execute()

		require.ErrorIs(t, err, account.ErrCurrencyMismatch)
		assert.Zero(t, scenario.unitOfWork.runs, "the unit of work never opens")
		scenario.assertNothingSaved(t)
	})

	t.Run("should save nothing when one provider transaction is invalid, even the valid ones before it", func(t *testing.T) {
		t.Parallel()

		scenario := newImportScenario(t)
		scenario.provider.transactions = []openfinance.ProviderTransaction{
			providerTransaction("tx-ok", -1500, "Taxi"),
			providerTransaction("", -500, "No external reference"),
			providerTransaction("tx-never-arrives", -2500, "Food delivery"),
		}

		_, err := scenario.execute()

		require.ErrorIs(t, err, transaction.ErrInvalidRef)
		assert.Zero(t, scenario.unitOfWork.runs, "the unit of work never opens")
		scenario.assertNothingSaved(t)
	})

	t.Run("should save nothing when fetching the balance fails, not even the transactions already fetched", func(t *testing.T) {
		t.Parallel()

		scenario := newImportScenario(t)
		scenario.provider.transactions = []openfinance.ProviderTransaction{providerTransaction("tx-1", -1000, "Taxi")}
		scenario.provider.balanceErr = openfinance.ErrProviderUnavailable

		_, err := scenario.execute()

		require.ErrorIs(t, err, openfinance.ErrProviderUnavailable)
		assert.Zero(t, scenario.unitOfWork.runs, "the unit of work never opens")
		scenario.assertNothingSaved(t)
	})

	t.Run("should return the error and save the account only through the unit of work", func(t *testing.T) {
		t.Parallel()

		scenario := newImportScenario(t)
		scenario.provider.transactions = []openfinance.ProviderTransaction{providerTransaction("tx-1", -1000, "Taxi")}
		scenario.unitOfWork.err = errInfra

		_, err := scenario.execute()

		require.ErrorIs(t, err, errInfra)
		assert.Equal(t, 1, scenario.unitOfWork.runs)
		scenario.assertNothingSaved(t)
	})

	t.Run("should return the error and keep the events on the aggregates when the outbox fails", func(t *testing.T) {
		t.Parallel()

		scenario := newImportScenario(t)
		scenario.provider.transactions = []openfinance.ProviderTransaction{providerTransaction("tx-1", -1000, "Taxi")}
		scenario.outbox.recordErr = errInfra

		_, err := scenario.execute()

		require.ErrorIs(t, err, errInfra)
		assert.Empty(t, scenario.outbox.recorded)
		require.Len(t, scenario.transactions.saved, 1)
		imported := scenario.transactions.saved[0]
		assert.Len(t, imported.Events(), 1, "the Imported event stays on the transaction for a retry")
		assert.Len(t, scenario.connected.Events(), 1, "the BalanceUpdated event stays on the account for a retry")
	})
}

func TestImportFromProviderInvalidBalance(t *testing.T) {
	t.Parallel()

	cases := []struct {
		name    string
		arrange func(provider *fakeProvider)
		wantErr error
	}{
		{
			name: "should refuse a balance in another currency",
			arrange: func(provider *fakeProvider) {
				provider.balance = &openfinance.ProviderBalance{AmountInCents: 100, CurrencyCode: "USD", AsOf: time.Now()}
			},
			wantErr: account.ErrCurrencyMismatch,
		},
		{
			name: "should refuse a balance without an instant",
			arrange: func(provider *fakeProvider) {
				provider.balance = &openfinance.ProviderBalance{AmountInCents: 100, CurrencyCode: "BRL"}
			},
			wantErr: account.ErrInvalidBalance,
		},
	}

	for _, testCase := range cases {
		t.Run(testCase.name, func(t *testing.T) {
			t.Parallel()

			scenario := newImportScenario(t)
			testCase.arrange(scenario.provider)

			_, err := scenario.execute()

			require.ErrorIs(t, err, testCase.wantErr)
			assert.Zero(t, scenario.unitOfWork.runs)
			scenario.assertNothingSaved(t)
		})
	}
}

func TestImportFromProviderInfrastructureFailure(t *testing.T) {
	t.Parallel()

	cases := []struct {
		name    string
		arrange func(scenario importScenario)
		wantErr error
	}{
		{
			name:    "should return the error when reading the account fails",
			arrange: func(scenario importScenario) { scenario.accounts.findErr = errInfra },
			wantErr: errInfra,
		},
		{
			name:    "should return the error when the provider is unavailable",
			arrange: func(scenario importScenario) { scenario.provider.fetchErr = openfinance.ErrProviderUnavailable },
			wantErr: openfinance.ErrProviderUnavailable,
		},
		{
			name:    "should return the error when saving a transaction fails",
			arrange: func(scenario importScenario) { scenario.transactions.saveErr = errInfra },
			wantErr: errInfra,
		},
		{
			name:    "should return the error when saving the account fails",
			arrange: func(scenario importScenario) { scenario.accounts.saveErr = errInfra },
			wantErr: errInfra,
		},
	}

	for _, testCase := range cases {
		t.Run(testCase.name, func(t *testing.T) {
			t.Parallel()

			scenario := newImportScenario(t)
			scenario.provider.transactions = []openfinance.ProviderTransaction{providerTransaction("tx-1", -1000, "Taxi")}
			testCase.arrange(scenario)

			_, err := scenario.execute()

			require.ErrorIs(t, err, testCase.wantErr)
		})
	}
}
