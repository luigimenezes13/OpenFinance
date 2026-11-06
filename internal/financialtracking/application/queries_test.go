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
	"github.com/luigimenezes13/financial-manager/internal/financialtracking/domain/category"
	"github.com/luigimenezes13/financial-manager/internal/financialtracking/domain/shared"
	"github.com/luigimenezes13/financial-manager/internal/financialtracking/domain/transaction"
	"github.com/luigimenezes13/financial-manager/internal/kernel"
)

func TestListAccounts(t *testing.T) {
	t.Parallel()

	t.Run("should list only the user's accounts sorted by name, since without the owner filter one user's statement would leak to another", func(t *testing.T) {
		t.Parallel()

		ownerID := uuid.New()
		otherID := uuid.New()

		manual := newManualAccount(t, ownerID, "BRL")
		connected := newConnectedAccount(t, ownerID, "pluggy", "pluggy-acc-1", "BRL")
		fromOther := newManualAccount(t, otherID, "BRL")

		accounts := newFakeAccounts(manual, connected, fromOther)
		useCase := application.NewListAccountsUseCase(accounts)

		output, err := useCase.Execute(context.Background(), application.ListAccountsInput{UserID: ownerID})

		require.NoError(t, err)
		require.Len(t, output.Accounts, 2, "the other user's account is not listed")

		assert.Equal(t, "Checking Account", output.Accounts[0].Name)
		assert.Empty(t, output.Accounts[0].Provider, "a manual account has no provider")
		assert.Equal(t, "Connected Account", output.Accounts[1].Name)
		assert.Equal(t, "pluggy", output.Accounts[1].Provider, "a connected account reports its provider")
		assert.False(t, output.Accounts[1].BalanceAsOf.IsZero(), "a balance without a date lies")
	})
}

func TestViewAccount(t *testing.T) {
	t.Parallel()

	ownerID := uuid.New()
	target := newManualAccount(t, ownerID, "BRL")
	accountID := target.ID()
	useCase := application.NewViewAccountUseCase(newFakeAccounts(target))

	t.Run("should return the account summary", func(t *testing.T) {
		t.Parallel()

		summary, err := useCase.Execute(context.Background(), application.ViewAccountInput{
			UserID: ownerID, AccountID: accountID.UUID(),
		})

		require.NoError(t, err)
		assert.Equal(t, accountID.String(), summary.ID)
		assert.Equal(t, "Checking Account", summary.Name)
	})

	t.Run("should refuse with ErrForbidden an account of another user", func(t *testing.T) {
		t.Parallel()

		_, err := useCase.Execute(context.Background(), application.ViewAccountInput{
			UserID: uuid.New(), AccountID: accountID.UUID(),
		})

		require.ErrorIs(t, err, shared.ErrForbidden)
	})

	t.Run("should refuse a missing account", func(t *testing.T) {
		t.Parallel()

		_, err := useCase.Execute(context.Background(), application.ViewAccountInput{
			UserID: ownerID, AccountID: uuid.New(),
		})

		require.ErrorIs(t, err, account.ErrNotFound)
	})
}

func TestListCategories(t *testing.T) {
	t.Parallel()

	t.Run("should list only the user's categories with their rules, since rules live inside the aggregate", func(t *testing.T) {
		t.Parallel()

		ownerID := uuid.New()
		food := newCategory(t, ownerID, "Food")
		rule, err := category.NewCategoryRule("ifood")
		require.NoError(t, err)
		require.NoError(t, food.AddRule(rule))

		parentID := food.ID()
		restaurants, err := category.New(mustUserID(t, ownerID), "Restaurants", &parentID)
		require.NoError(t, err)

		fromOther := newCategory(t, uuid.New(), "Someone Else's")

		useCase := application.NewListCategoriesUseCase(newFakeCategories(food, restaurants, fromOther))

		output, err := useCase.Execute(context.Background(), application.ListCategoriesInput{UserID: ownerID})

		require.NoError(t, err)
		require.Len(t, output.Categories, 2)

		assert.Equal(t, "Food", output.Categories[0].Name)
		assert.Nil(t, output.Categories[0].ParentID)
		require.Len(t, output.Categories[0].Rules, 1, "the rules come along: they live inside the aggregate")
		assert.Equal(t, "ifood", output.Categories[0].Rules[0].Keyword)

		require.NotNil(t, output.Categories[1].ParentID)
		assert.Equal(t, parentID.String(), *output.Categories[1].ParentID)
	})
}

func TestListTransactions(t *testing.T) {
	t.Parallel()

	ownerID := uuid.New()
	targetAccount := newManualAccount(t, ownerID, "BRL")
	accountID := targetAccount.ID()

	older := newManualTransactionAt(t, ownerID, accountID, -100, time.Now().Add(-72*time.Hour))
	middle := newManualTransactionAt(t, ownerID, accountID, -200, time.Now().Add(-48*time.Hour))
	newest := newManualTransactionAt(t, ownerID, accountID, -300, time.Now().Add(-24*time.Hour))
	fromOther := newManualTransactionAt(t, uuid.New(), account.NewAccountID(), -999, time.Now())

	transactions := newFakeTransactions(older, middle, newest, fromOther)
	useCase := application.NewListTransactionsUseCase(transactions)

	t.Run("should list only the user's transactions newest first with the default limit", func(t *testing.T) {
		t.Parallel()

		output, err := useCase.Execute(context.Background(), application.ListTransactionsInput{UserID: ownerID})

		require.NoError(t, err)
		require.Len(t, output.Transactions, 3, "a transaction of another user is not listed")
		assert.Equal(t, int64(-300), output.Transactions[0].Amount, "the statement starts with the newest")
		assert.Equal(t, int64(-100), output.Transactions[2].Amount)
		assert.Equal(t, kernel.DefaultLimit, output.Limit, "the default is returned so the client can build the next page")
	})

	t.Run("should paginate with limit and offset", func(t *testing.T) {
		t.Parallel()

		first, err := useCase.Execute(context.Background(), application.ListTransactionsInput{
			UserID: ownerID, Limit: 2, Offset: 0,
		})
		require.NoError(t, err)
		require.Len(t, first.Transactions, 2)

		second, err := useCase.Execute(context.Background(), application.ListTransactionsInput{
			UserID: ownerID, Limit: 2, Offset: 2,
		})
		require.NoError(t, err)
		require.Len(t, second.Transactions, 1)
		assert.NotEqual(t, first.Transactions[0].ID, second.Transactions[0].ID)
	})

	t.Run("should filter by period", func(t *testing.T) {
		t.Parallel()

		output, err := useCase.Execute(context.Background(), application.ListTransactionsInput{
			UserID: ownerID,
			From:   time.Now().Add(-50 * time.Hour),
			To:     time.Now(),
		})

		require.NoError(t, err)
		assert.Len(t, output.Transactions, 2, "the one from 72h ago stays out")
	})

	t.Run("should filter by account", func(t *testing.T) {
		t.Parallel()

		otherAccountID := account.NewAccountID()
		rawOther := otherAccountID.UUID()
		output, err := useCase.Execute(context.Background(), application.ListTransactionsInput{
			UserID: ownerID, AccountID: &rawOther,
		})

		require.NoError(t, err)
		assert.Empty(t, output.Transactions)
	})
}

func TestListTransactionsRefusal(t *testing.T) {
	t.Parallel()

	ownerID := uuid.New()
	nilUUID := uuid.Nil

	cases := []struct {
		name    string
		input   application.ListTransactionsInput
		wantErr error
	}{
		{
			name:    "should refuse a nil user",
			input:   application.ListTransactionsInput{UserID: uuid.Nil},
			wantErr: shared.ErrInvalidUserID,
		},
		{
			name:    "should refuse a limit above the ceiling",
			input:   application.ListTransactionsInput{UserID: ownerID, Limit: kernel.MaxLimit + 1},
			wantErr: kernel.ErrInvalidPage,
		},
		{
			name:    "should refuse a negative offset",
			input:   application.ListTransactionsInput{UserID: ownerID, Offset: -1},
			wantErr: kernel.ErrInvalidPage,
		},
		{
			name:    "should refuse a nil account in the filter",
			input:   application.ListTransactionsInput{UserID: ownerID, AccountID: &nilUUID},
			wantErr: account.ErrInvalidID,
		},
		{
			name:    "should refuse a nil category in the filter",
			input:   application.ListTransactionsInput{UserID: ownerID, CategoryID: &nilUUID},
			wantErr: category.ErrInvalidID,
		},
		{
			name: "should refuse an inverted period before it becomes a query",
			input: application.ListTransactionsInput{
				UserID: ownerID,
				From:   time.Now(),
				To:     time.Now().Add(-24 * time.Hour),
			},
			wantErr: transaction.ErrInvalidPeriod,
		},
	}

	for _, testCase := range cases {
		t.Run(testCase.name, func(t *testing.T) {
			t.Parallel()

			useCase := application.NewListTransactionsUseCase(newFakeTransactions())

			_, err := useCase.Execute(context.Background(), testCase.input)

			require.ErrorIs(t, err, testCase.wantErr)
		})
	}
}

func TestViewTransaction(t *testing.T) {
	t.Parallel()

	ownerID := uuid.New()
	targetAccount := newManualAccount(t, ownerID, "BRL")
	accountID := targetAccount.ID()
	target := newManualTransaction(t, ownerID, accountID, -4550, "BRL")
	categoryID := category.NewCategoryID()
	require.NoError(t, target.Categorize(categoryID, transaction.AssignedByUser))
	transactionID := target.ID()

	useCase := application.NewViewTransactionUseCase(newFakeTransactions(target))

	t.Run("should return the transaction summary with its category assignment", func(t *testing.T) {
		t.Parallel()

		summary, err := useCase.Execute(context.Background(), application.ViewTransactionInput{
			UserID: ownerID, TransactionID: transactionID.UUID(),
		})

		require.NoError(t, err)
		assert.Equal(t, int64(-4550), summary.Amount)
		require.NotNil(t, summary.CategoryID)
		assert.Equal(t, categoryID.String(), *summary.CategoryID)
		require.NotNil(t, summary.AssignedBy)
		assert.Equal(t, "user", *summary.AssignedBy)
		require.NotNil(t, summary.AssignedAt)
		assert.Empty(t, summary.Provider, "a manual transaction has no provider")
	})

	t.Run("should refuse with ErrForbidden a transaction of another user", func(t *testing.T) {
		t.Parallel()

		_, err := useCase.Execute(context.Background(), application.ViewTransactionInput{
			UserID: uuid.New(), TransactionID: transactionID.UUID(),
		})

		require.ErrorIs(t, err, shared.ErrForbidden)
	})
}
