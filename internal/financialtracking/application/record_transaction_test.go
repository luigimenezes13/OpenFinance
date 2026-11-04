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
	"github.com/luigimenezes13/financial-manager/internal/financialtracking/domain/shared"
	"github.com/luigimenezes13/financial-manager/internal/financialtracking/domain/transaction"
)

func TestRecordTransaction(t *testing.T) {
	t.Parallel()

	t.Run("should save an outflow that inherits the account currency and saves no event", func(t *testing.T) {
		t.Parallel()

		ownerID := uuid.New()
		targetAccount := newManualAccount(t, ownerID, "BRL")
		accountID := targetAccount.ID()
		accounts := newFakeAccounts(targetAccount)
		transactions := newFakeTransactions()
		useCase := application.NewRecordTransactionUseCase(transactions, accounts)
		occurredAt := time.Now().Add(-2 * time.Hour)

		output, err := useCase.Execute(context.Background(), application.RecordTransactionInput{
			UserID:      ownerID,
			AccountID:   accountID.UUID(),
			Amount:      -4550,
			OccurredAt:  occurredAt,
			Description: "  Bakery  ",
		})

		require.NoError(t, err)
		assert.Equal(t, int64(-4550), output.Amount)
		assert.Equal(t, "BRL", output.Currency, "the currency comes from the account, not from the input")
		assert.Equal(t, "Bakery", output.Description)
		assert.Equal(t, accountID.String(), output.AccountID)
		assert.WithinDuration(t, occurredAt, output.OccurredAt, time.Second)
		require.Len(t, transactions.saved, 1)
		assert.Empty(t, transactions.savedEvents, "a manual entry saves no event, only the provider flow does")
		savedBalance := targetAccount.Balance()
		savedMoney := savedBalance.Money()
		assert.Zero(t, savedMoney.Amount(), "recording does not touch the balance")
		assert.Empty(t, accounts.saved, "the account is only READ by this use case")
	})

	t.Run("should refuse with ErrForbidden an account of another user before any write", func(t *testing.T) {
		t.Parallel()

		otherAccount := newManualAccount(t, uuid.New(), "BRL")
		accounts := newFakeAccounts(otherAccount)
		transactions := newFakeTransactions()
		useCase := application.NewRecordTransactionUseCase(transactions, accounts)
		accountID := otherAccount.ID()

		_, err := useCase.Execute(context.Background(), application.RecordTransactionInput{
			UserID:      uuid.New(),
			AccountID:   accountID.UUID(),
			Amount:      1000,
			OccurredAt:  time.Now().Add(-time.Minute),
			Description: "Attempt",
		})

		require.ErrorIs(t, err, shared.ErrForbidden)
		assert.Empty(t, transactions.saved)
	})
}

// TestRecordTransactionNeverTouchesBalance guards a product rule, not an
// implementation detail: the balance enters the system only through the Open
// Finance import, because the bank is the authority on how much the account
// holds. Summing entries into the balance would make it disagree with the
// bank statement, and this test exists to stop that well-meant "fix".
//
// The initial balance is non-zero so a wrong sum cannot land on zero by
// chance, and dated after the account creation because the aggregate refuses
// a balance that goes back in time.
func TestRecordTransactionNeverTouchesBalance(t *testing.T) {
	t.Parallel()

	t.Run("should keep a non-zero provider balance untouched after several entries", func(t *testing.T) {
		t.Parallel()

		ownerID := uuid.New()
		targetAccount := newManualAccount(t, ownerID, "BRL")
		accountID := targetAccount.ID()
		initial, err := account.NewBalance(mustMoney(t, 1_000_00, "BRL"), time.Now().Add(time.Second))
		require.NoError(t, err)
		require.NoError(t, targetAccount.UpdateBalance(initial))
		targetAccount.ClearEvents()
		accounts := newFakeAccounts(targetAccount)
		transactions := newFakeTransactions()
		useCase := application.NewRecordTransactionUseCase(transactions, accounts)

		for _, amount := range []int64{-45_50, -120_00, 300_00} {
			_, err := useCase.Execute(context.Background(), application.RecordTransactionInput{
				UserID:      ownerID,
				AccountID:   accountID.UUID(),
				Amount:      amount,
				OccurredAt:  time.Now().Add(-time.Minute),
				Description: "Entry",
			})
			require.NoError(t, err)
		}

		require.Len(t, transactions.saved, 3, "the transactions were recorded")
		balance := targetAccount.Balance()
		money := balance.Money()
		assert.Equal(t, int64(1_000_00), money.Amount(), "the balance stays exactly what the provider reported")
		assert.Empty(t, accounts.saved, "the account is not even saved again: this use case only READS it")
		assert.Empty(t, targetAccount.Events(), "no AccountBalanceUpdated was recorded")
		assert.Empty(t, accounts.savedEvents)
	})
}

func TestRecordTransactionRefusal(t *testing.T) {
	t.Parallel()

	validOccurredAt := time.Now().Add(-time.Hour)

	cases := []struct {
		name        string
		mutateInput func(input *application.RecordTransactionInput)
		wantErr     error
	}{
		{
			name:        "should refuse a nil user",
			mutateInput: func(input *application.RecordTransactionInput) { input.UserID = uuid.Nil },
			wantErr:     shared.ErrInvalidUserID,
		},
		{
			name:        "should refuse a nil account",
			mutateInput: func(input *application.RecordTransactionInput) { input.AccountID = uuid.Nil },
			wantErr:     account.ErrInvalidID,
		},
		{
			name:        "should refuse a missing account",
			mutateInput: func(input *application.RecordTransactionInput) { input.AccountID = uuid.New() },
			wantErr:     account.ErrNotFound,
		},
		{
			name:        "should refuse a zero amount",
			mutateInput: func(input *application.RecordTransactionInput) { input.Amount = 0 },
			wantErr:     transaction.ErrZeroMoney,
		},
		{
			name:        "should refuse a blank description",
			mutateInput: func(input *application.RecordTransactionInput) { input.Description = "  " },
			wantErr:     transaction.ErrInvalidDescription,
		},
		{
			name:        "should refuse a date in the future",
			mutateInput: func(input *application.RecordTransactionInput) { input.OccurredAt = time.Now().Add(time.Hour) },
			wantErr:     transaction.ErrInvalidOccurredAt,
		},
		{
			name:        "should refuse a zero date",
			mutateInput: func(input *application.RecordTransactionInput) { input.OccurredAt = time.Time{} },
			wantErr:     transaction.ErrInvalidOccurredAt,
		},
	}

	for _, testCase := range cases {
		t.Run(testCase.name, func(t *testing.T) {
			t.Parallel()

			ownerID := uuid.New()
			targetAccount := newManualAccount(t, ownerID, "BRL")
			accountID := targetAccount.ID()
			accounts := newFakeAccounts(targetAccount)
			transactions := newFakeTransactions()
			useCase := application.NewRecordTransactionUseCase(transactions, accounts)
			input := application.RecordTransactionInput{
				UserID:      ownerID,
				AccountID:   accountID.UUID(),
				Amount:      -1000,
				OccurredAt:  validOccurredAt,
				Description: "Groceries",
			}
			testCase.mutateInput(&input)

			_, err := useCase.Execute(context.Background(), input)

			require.ErrorIs(t, err, testCase.wantErr)
			assert.Empty(t, transactions.saved)
		})
	}
}

func TestRecordTransactionInfrastructureFailure(t *testing.T) {
	t.Parallel()

	cases := []struct {
		name    string
		arrange func(accounts *fakeAccounts, transactions *fakeTransactions)
	}{
		{
			name:    "should return the error when reading the account fails",
			arrange: func(accounts *fakeAccounts, _ *fakeTransactions) { accounts.findErr = errInfra },
		},
		{
			name:    "should return the error when saving the transaction fails",
			arrange: func(_ *fakeAccounts, transactions *fakeTransactions) { transactions.saveErr = errInfra },
		},
	}

	for _, testCase := range cases {
		t.Run(testCase.name, func(t *testing.T) {
			t.Parallel()

			ownerID := uuid.New()
			targetAccount := newManualAccount(t, ownerID, "BRL")
			accountID := targetAccount.ID()
			accounts := newFakeAccounts(targetAccount)
			transactions := newFakeTransactions()
			testCase.arrange(accounts, transactions)
			useCase := application.NewRecordTransactionUseCase(transactions, accounts)

			_, err := useCase.Execute(context.Background(), application.RecordTransactionInput{
				UserID:      ownerID,
				AccountID:   accountID.UUID(),
				Amount:      2500,
				OccurredAt:  time.Now().Add(-time.Hour),
				Description: "Salary",
			})

			require.ErrorIs(t, err, errInfra)
		})
	}
}
