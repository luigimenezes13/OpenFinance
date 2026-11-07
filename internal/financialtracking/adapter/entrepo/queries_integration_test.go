//go:build integration

package entrepo_test

import (
	"context"
	"testing"
	"time"

	"github.com/google/uuid"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/luigimenezes13/financial-manager/internal/financialtracking/adapter/entrepo"
	"github.com/luigimenezes13/financial-manager/internal/financialtracking/domain/account"
	"github.com/luigimenezes13/financial-manager/internal/financialtracking/domain/category"
	"github.com/luigimenezes13/financial-manager/internal/financialtracking/domain/transaction"
	"github.com/luigimenezes13/financial-manager/internal/kernel"
)

func mustSavedTransactionAt(t *testing.T, repository *entrepo.TransactionRepository, ownerID uuid.UUID, accountID account.AccountID, amount int64, occurredAt time.Time) *transaction.Transaction {
	t.Helper()

	created, err := transaction.NewManual(
		mustUserID(t, ownerID), accountID, mustMoneyBRL(t, amount), occurredAt, "Grocery")
	require.NoError(t, err)
	require.NoError(t, repository.Save(context.Background(), created))
	return created
}

func moneyAmount(current *transaction.Transaction) int64 {
	money := current.Money()
	return money.Amount()
}

func mustCategoryFor(t *testing.T, ownerID uuid.UUID) *category.Category {
	t.Helper()
	created, err := category.New(mustUserID(t, ownerID), "Someone Else's", nil)
	require.NoError(t, err)
	return created
}

func TestAccountRepositoryListByUserIntegration(t *testing.T) {
	ctx := context.Background()

	t.Run("should list only the owner's accounts ordered by name so a statement never shows up for someone else", func(t *testing.T) {
		truncateAll(t)
		repository := newAccountRepository()
		ownerID := uuid.New()
		otherID := uuid.New()

		zebra, err := account.New(mustUserID(t, ownerID), "Zebra", account.KindChecking, mustCurrency(t, "BRL"), account.NewManualSource())
		require.NoError(t, err)
		alpha, err := account.New(mustUserID(t, ownerID), "Alpha", account.KindSavings, mustCurrency(t, "BRL"), account.NewManualSource())
		require.NoError(t, err)
		require.NoError(t, repository.Save(ctx, zebra))
		require.NoError(t, repository.Save(ctx, alpha))
		require.NoError(t, repository.Save(ctx, mustManualAccount(t, otherID)))

		found, err := repository.ListByUser(ctx, mustUserID(t, ownerID))

		require.NoError(t, err)
		require.Len(t, found, 2, "the other user's account is not listed")
		assert.Equal(t, "Alpha", found[0].Name(), "ordered by name")
		assert.Equal(t, "Zebra", found[1].Name())
	})
}

func TestCategoryRepositoryListByUserIntegration(t *testing.T) {
	ctx := context.Background()

	t.Run("should eager load the rules of every listed category instead of querying once per category", func(t *testing.T) {
		truncateAll(t)
		repository := newCategoryRepository()
		ownerID := uuid.New()

		food, err := category.New(mustUserID(t, ownerID), "Food", nil)
		require.NoError(t, err)
		require.NoError(t, food.AddRule(mustRule(t, "ifood")))
		require.NoError(t, food.AddRule(mustRule(t, "grocery")))
		require.NoError(t, repository.Save(ctx, food))

		transport, err := category.New(mustUserID(t, ownerID), "Transport", nil)
		require.NoError(t, err)
		require.NoError(t, transport.AddRule(mustRule(t, "uber")))
		require.NoError(t, repository.Save(ctx, transport))

		require.NoError(t, repository.Save(ctx, mustCategoryFor(t, uuid.New())))

		found, err := repository.ListByUser(ctx, mustUserID(t, ownerID))

		require.NoError(t, err)
		require.Len(t, found, 2)
		assert.Equal(t, "Food", found[0].Name())
		assert.Len(t, found[0].Rules(), 2, "the rules are loaded together")
		assert.Equal(t, "Transport", found[1].Name())
		assert.Len(t, found[1].Rules(), 1)
	})
}

func TestTransactionRepositoryListIntegration(t *testing.T) {
	truncateAll(t)
	ctx := context.Background()

	repository := newTransactionRepository()
	ownerID := uuid.New()
	accountID := account.NewAccountID()
	otherAccountID := account.NewAccountID()

	now := time.Now()
	oldest := mustSavedTransactionAt(t, repository, ownerID, accountID, -100, now.Add(-72*time.Hour))
	middle := mustSavedTransactionAt(t, repository, ownerID, accountID, -200, now.Add(-48*time.Hour))
	newest := mustSavedTransactionAt(t, repository, ownerID, accountID, -300, now.Add(-24*time.Hour))
	mustSavedTransactionAt(t, repository, ownerID, otherAccountID, -400, now.Add(-12*time.Hour))
	mustSavedTransactionAt(t, repository, uuid.New(), accountID, -999, now.Add(-time.Hour))

	page, err := kernel.NewPage(kernel.DefaultLimit, 0)
	require.NoError(t, err)

	t.Run("should list only the owner's transactions from newest to oldest", func(t *testing.T) {
		criteria, err := transaction.NewCriteria(mustUserID(t, ownerID), page)
		require.NoError(t, err)

		found, err := repository.List(ctx, criteria)

		require.NoError(t, err)
		require.Len(t, found, 4, "the other user's transaction is not listed")
		assert.True(t, found[0].ID().Equals(newest.ID()) || found[0].OccurredAt().After(found[1].OccurredAt()),
			"from newest to oldest")
		assert.True(t, found[3].ID().Equals(oldest.ID()))
	})

	t.Run("should filter by account", func(t *testing.T) {
		criteria, err := transaction.NewCriteria(mustUserID(t, ownerID), page, transaction.ForAccount(otherAccountID))
		require.NoError(t, err)

		found, err := repository.List(ctx, criteria)

		require.NoError(t, err)
		require.Len(t, found, 1)
		assert.Equal(t, int64(-400), moneyAmount(found[0]))
	})

	t.Run("should filter by period", func(t *testing.T) {
		criteria, err := transaction.NewCriteria(mustUserID(t, ownerID), page,
			transaction.InPeriod(now.Add(-50*time.Hour), now.Add(-20*time.Hour)))
		require.NoError(t, err)

		found, err := repository.List(ctx, criteria)

		require.NoError(t, err)
		require.Len(t, found, 2, "only middle and newest fall inside the window")
		assert.True(t, found[1].ID().Equals(middle.ID()))
	})

	t.Run("should paginate without repeating or losing a record", func(t *testing.T) {
		firstPage, err := kernel.NewPage(2, 0)
		require.NoError(t, err)
		secondPage, err := kernel.NewPage(2, 2)
		require.NoError(t, err)

		firstCriteria, err := transaction.NewCriteria(mustUserID(t, ownerID), firstPage)
		require.NoError(t, err)
		secondCriteria, err := transaction.NewCriteria(mustUserID(t, ownerID), secondPage)
		require.NoError(t, err)

		first, err := repository.List(ctx, firstCriteria)
		require.NoError(t, err)
		second, err := repository.List(ctx, secondCriteria)
		require.NoError(t, err)

		require.Len(t, first, 2)
		require.Len(t, second, 2)

		seen := map[string]bool{}
		for _, current := range append(first, second...) {
			id := current.ID().String()
			assert.False(t, seen[id], "id repeated across pages: %s", id)
			seen[id] = true
		}
		assert.Len(t, seen, 4)
	})

	t.Run("should filter by category", func(t *testing.T) {
		categoryID := category.NewCategoryID()
		require.NoError(t, middle.Categorize(categoryID, transaction.AssignedByUser))
		require.NoError(t, repository.Save(ctx, middle))

		criteria, err := transaction.NewCriteria(mustUserID(t, ownerID), page, transaction.ForCategory(categoryID))
		require.NoError(t, err)

		found, err := repository.List(ctx, criteria)

		require.NoError(t, err)
		require.Len(t, found, 1)
		assert.True(t, found[0].ID().Equals(middle.ID()))
	})

	t.Run("should return an empty list, not an error, when nothing matches", func(t *testing.T) {
		criteria, err := transaction.NewCriteria(mustUserID(t, uuid.New()), page)
		require.NoError(t, err)

		found, err := repository.List(ctx, criteria)

		require.NoError(t, err)
		assert.Empty(t, found)
	})
}
