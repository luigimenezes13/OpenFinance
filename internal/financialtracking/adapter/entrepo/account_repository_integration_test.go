//go:build integration

package entrepo_test

import (
	"context"
	"testing"
	"time"

	"github.com/google/uuid"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/luigimenezes13/financial-manager/internal/financialtracking/domain/account"
	"github.com/luigimenezes13/financial-manager/internal/financialtracking/domain/shared"
)

func mustUserID(t *testing.T, raw uuid.UUID) shared.UserID {
	t.Helper()
	userID, err := shared.NewUserID(raw)
	require.NoError(t, err)
	return userID
}

func mustCurrency(t *testing.T, code string) shared.Currency {
	t.Helper()
	currency, err := shared.NewCurrency(code)
	require.NoError(t, err)
	return currency
}

func mustMoneyBRL(t *testing.T, amount int64) shared.Money {
	t.Helper()
	money, err := shared.NewMoney(amount, mustCurrency(t, "BRL"))
	require.NoError(t, err)
	return money
}

func mustManualAccount(t *testing.T, ownerID uuid.UUID) *account.Account {
	t.Helper()
	created, err := account.New(mustUserID(t, ownerID), "Checking Account", account.KindChecking, mustCurrency(t, "BRL"), account.NewManualSource())
	require.NoError(t, err)
	return created
}

func TestAccountRepositoryIntegration(t *testing.T) {
	ctx := context.Background()

	t.Run("should round-trip the aggregate with the balance instant truncated to Postgres microsecond precision", func(t *testing.T) {
		truncateAll(t)
		repository := newAccountRepository()
		original := mustManualAccount(t, uuid.New())

		require.NoError(t, repository.Save(ctx, original))

		found, err := repository.FindByID(ctx, original.ID())
		require.NoError(t, err)

		originalSnapshot := original.Snapshot()
		foundSnapshot := found.Snapshot()

		assert.Equal(t, originalSnapshot.ID, foundSnapshot.ID)
		assert.Equal(t, originalSnapshot.UserID, foundSnapshot.UserID)
		assert.Equal(t, originalSnapshot.Name, foundSnapshot.Name)
		assert.Equal(t, originalSnapshot.Kind, foundSnapshot.Kind)
		assert.Equal(t, originalSnapshot.BalanceAmount, foundSnapshot.BalanceAmount)
		assert.Equal(t, originalSnapshot.BalanceCurrency, foundSnapshot.BalanceCurrency)
		assert.Empty(t, foundSnapshot.SourceProvider, "a manual account carries no provider reference")
		assert.WithinDuration(t, originalSnapshot.BalanceAsOf, foundSnapshot.BalanceAsOf, time.Millisecond)
	})

	t.Run("should upsert the same row and keep created_at immutable", func(t *testing.T) {
		truncateAll(t)
		repository := newAccountRepository()
		target := mustManualAccount(t, uuid.New())
		require.NoError(t, repository.Save(ctx, target))

		var createdAtBefore time.Time
		targetID := target.ID()
		require.NoError(t, testDB.QueryRowContext(ctx,
			`SELECT created_at FROM accounts WHERE id = $1`, targetID.UUID()).Scan(&createdAtBefore))

		newBalance, err := account.NewBalance(mustMoneyBRL(t, -1_250_00), time.Now())
		require.NoError(t, err)
		require.NoError(t, target.UpdateBalance(newBalance))
		require.NoError(t, target.Rename("Nubank Checking Account"))
		require.NoError(t, repository.Save(ctx, target))

		found, err := repository.FindByID(ctx, target.ID())
		require.NoError(t, err)

		foundSnapshot := found.Snapshot()
		assert.Equal(t, int64(-1_250_00), foundSnapshot.BalanceAmount)
		assert.Equal(t, "Nubank Checking Account", foundSnapshot.Name)

		var rowCount int
		require.NoError(t, testDB.QueryRowContext(ctx, `SELECT count(*) FROM accounts`).Scan(&rowCount))
		assert.Equal(t, 1, rowCount, "the upsert updates the row instead of inserting another")

		var createdAtAfter, updatedAtAfter time.Time
		require.NoError(t, testDB.QueryRowContext(ctx,
			`SELECT created_at, updated_at FROM accounts WHERE id = $1`, targetID.UUID()).
			Scan(&createdAtAfter, &updatedAtAfter))
		assert.WithinDuration(t, createdAtBefore, createdAtAfter, time.Microsecond, "created_at does not change on update")
		assert.True(t, updatedAtAfter.After(createdAtAfter) || updatedAtAfter.Equal(createdAtAfter), "updated_at follows the write")
	})

	t.Run("should round-trip an open finance source and reject a second account for the same provider account", func(t *testing.T) {
		truncateAll(t)
		repository := newAccountRepository()
		source, err := account.NewOpenFinanceSource("pluggy", "pluggy-acc-77")
		require.NoError(t, err)
		connected, err := account.New(mustUserID(t, uuid.New()), "Connected Account", account.KindCreditCard, mustCurrency(t, "BRL"), source)
		require.NoError(t, err)

		require.NoError(t, repository.Save(ctx, connected))

		found, err := repository.FindByID(ctx, connected.ID())
		require.NoError(t, err)

		foundSource := found.Source()
		require.True(t, foundSource.IsOpenFinance())
		provider, ok := foundSource.Provider()
		require.True(t, ok)
		assert.Equal(t, "pluggy", provider)
		providerAccountID, ok := foundSource.ProviderAccountID()
		require.True(t, ok)
		assert.Equal(t, "pluggy-acc-77", providerAccountID)

		duplicate, err := account.New(mustUserID(t, uuid.New()), "Duplicate", account.KindChecking, mustCurrency(t, "BRL"), source)
		require.NoError(t, err)
		require.Error(t, repository.Save(ctx, duplicate),
			"two local accounts for the same provider account must fail")
	})

	t.Run("should let manual accounts coexist because the provider unique index is partial", func(t *testing.T) {
		truncateAll(t)
		repository := newAccountRepository()
		ownerID := uuid.New()

		require.NoError(t, repository.Save(ctx, mustManualAccount(t, ownerID)))
		require.NoError(t, repository.Save(ctx, mustManualAccount(t, ownerID)))

		var rowCount int
		require.NoError(t, testDB.QueryRowContext(ctx, `SELECT count(*) FROM accounts`).Scan(&rowCount))
		assert.Equal(t, 2, rowCount)
	})

	t.Run("should translate a missing row into the aggregate not found sentinel", func(t *testing.T) {
		truncateAll(t)
		repository := newAccountRepository()
		missingID, err := account.AccountIDFromUUID(uuid.New())
		require.NoError(t, err)

		found, err := repository.FindByID(ctx, missingID)

		require.ErrorIs(t, err, account.ErrNotFound)
		assert.Nil(t, found)
	})
}
