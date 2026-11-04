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
	"github.com/luigimenezes13/financial-manager/internal/financialtracking/domain/category"
	"github.com/luigimenezes13/financial-manager/internal/financialtracking/domain/transaction"
)

const rawTransactionInsert = `
INSERT INTO transactions (
    id, user_id, account_id, amount, currency, occurred_at, description, reconciled,
    category_id, category_assigned_by, category_assigned_at,
    external_ref_provider, external_ref_provider_transaction_id,
    created_at, updated_at
) VALUES ($1, $2, $3, $4, 'BRL', now(), $5, $6, $7, $8, $9, $10, $11, now(), now())`

type impossibleTransaction struct {
	name                  string
	amount                int64
	description           string
	reconciled            bool
	categoryID            *uuid.UUID
	categoryAssignedBy    string
	categoryAssignedAt    *time.Time
	externalRefProvider   string
	externalRefProviderTx string
}

func impossibleTransactions() []impossibleTransaction {
	return []impossibleTransaction{
		{name: "should reject a zero amount", amount: 0, description: "Nothing"},
		{name: "should reject a blank description", amount: -100, description: "   "},
		{name: "should reject a manual transaction marked as reconciled", amount: -100, description: "Uber", reconciled: true},
		{name: "should reject a category without who assigned it", amount: -100, description: "Uber", categoryID: pointerTo(uuid.New())},
		{name: "should reject an external reference without the provider transaction id", amount: -100, description: "Uber", externalRefProvider: "pluggy"},
	}
}

func TestTransactionRepositoryIntegration(t *testing.T) {
	ctx := context.Background()

	t.Run("should round-trip a manual transaction with the optional columns absent and stored as NULL", func(t *testing.T) {
		truncateAll(t)
		repository := newTransactionRepository(t)
		occurredAt := time.Now().Add(-3 * time.Hour)
		original, err := transaction.NewManual(
			mustUserID(t, uuid.New()),
			account.NewAccountID(),
			mustMoneyBRL(t, -45_50),
			occurredAt,
			"Bakery",
		)
		require.NoError(t, err)

		require.NoError(t, repository.Save(ctx, original))

		found, err := repository.FindByID(ctx, original.ID())
		require.NoError(t, err)

		foundMoney := found.Money()
		assert.Equal(t, int64(-45_50), foundMoney.Amount())
		assert.Equal(t, "Bakery", found.Description())
		assert.WithinDuration(t, occurredAt, found.OccurredAt(), time.Millisecond)
		assert.False(t, found.IsReconciled())

		_, hasCategory := found.Category()
		assert.False(t, hasCategory, "NULL in the database becomes 'uncategorized', not a zeroed assignment")
		_, hasReference := found.ExternalRef()
		assert.False(t, hasReference, "a manual transaction has no external reference")
		assert.Empty(t, found.Events(), "rehydrating does not emit events")

		var categoryID *uuid.UUID
		var assignedAt *time.Time
		originalID := original.ID()
		require.NoError(t, testDB.QueryRowContext(ctx,
			`SELECT category_id, category_assigned_at FROM transactions WHERE id = $1`, originalID.UUID()).
			Scan(&categoryID, &assignedAt))
		assert.Nil(t, categoryID, "the category column is really NULL, not a year-one timestamp written by the mapper")
		assert.Nil(t, assignedAt)
	})

	t.Run("should round-trip an imported, categorized and reconciled transaction and queue its events", func(t *testing.T) {
		truncateAll(t)
		repository := newTransactionRepository(t)
		reference, err := transaction.NewExternalRef("pluggy", "tx-abc-123")
		require.NoError(t, err)
		original, err := transaction.NewFromProvider(
			mustUserID(t, uuid.New()),
			account.NewAccountID(),
			mustMoneyBRL(t, -89_90),
			time.Now().Add(-48*time.Hour),
			"Mercado Livre",
			reference,
		)
		require.NoError(t, err)

		categoryID := category.NewCategoryID()
		require.NoError(t, original.Categorize(categoryID, transaction.AssignedByRule))
		require.NoError(t, original.MarkReconciled())

		require.NoError(t, repository.Save(ctx, original))

		found, err := repository.FindByID(ctx, original.ID())
		require.NoError(t, err)

		assert.True(t, found.IsReconciled())

		foundReference, hasReference := found.ExternalRef()
		require.True(t, hasReference)
		assert.Equal(t, "pluggy", foundReference.Provider())
		assert.Equal(t, "tx-abc-123", foundReference.ProviderTransactionID())

		assignment, hasCategory := found.Category()
		require.True(t, hasCategory)
		foundCategoryID := assignment.CategoryID()
		assert.True(t, foundCategoryID.Equals(categoryID))
		assignedBy := assignment.By()
		assert.Equal(t, "rule", assignedBy.String())

		originalSnapshot := original.Snapshot()
		assert.WithinDuration(t, originalSnapshot.CategoryAssignedAt, assignment.AssignedAt(), time.Millisecond,
			"the assignment instant comes from the database, not from time.Now() on read")

		assert.Empty(t, original.Events(), "a successful save hands the pending events to the outbox")
		assert.Equal(t, []string{
			transaction.EventTypeCategorized,
			transaction.EventTypeImported,
			transaction.EventTypeReconciled,
		}, queuedEventNames(t))
	})

	t.Run("should update the category assignment instead of duplicating the transaction", func(t *testing.T) {
		truncateAll(t)
		repository := newTransactionRepository(t)
		target, err := transaction.NewManual(
			mustUserID(t, uuid.New()),
			account.NewAccountID(),
			mustMoneyBRL(t, -20_00),
			time.Now().Add(-time.Hour),
			"Uber",
		)
		require.NoError(t, err)
		require.NoError(t, repository.Save(ctx, target))

		secondCategoryID := category.NewCategoryID()
		require.NoError(t, target.Categorize(category.NewCategoryID(), transaction.AssignedByUser))
		require.NoError(t, repository.Save(ctx, target))
		require.NoError(t, target.Categorize(secondCategoryID, transaction.AssignedByUser))
		require.NoError(t, repository.Save(ctx, target))

		found, err := repository.FindByID(ctx, target.ID())
		require.NoError(t, err)

		assignment, hasCategory := found.Category()
		require.True(t, hasCategory)
		foundCategoryID := assignment.CategoryID()
		assert.True(t, foundCategoryID.Equals(secondCategoryID))

		var rowCount int
		require.NoError(t, testDB.QueryRowContext(ctx, `SELECT count(*) FROM transactions`).Scan(&rowCount))
		assert.Equal(t, 1, rowCount)
	})

	t.Run("should translate a missing row into the aggregate not found sentinel", func(t *testing.T) {
		truncateAll(t)
		repository := newTransactionRepository(t)
		missingID, err := transaction.TransactionIDFromUUID(uuid.New())
		require.NoError(t, err)

		found, err := repository.FindByID(ctx, missingID)

		require.ErrorIs(t, err, transaction.ErrNotFound)
		assert.Nil(t, found)
	})

	t.Run("should keep the schema CHECK constraints guarding raw SQL writes that bypass the domain", func(t *testing.T) {
		truncateAll(t)

		for _, testCase := range impossibleTransactions() {
			t.Run(testCase.name, func(t *testing.T) {
				_, err := testDB.ExecContext(ctx, rawTransactionInsert,
					uuid.New(), uuid.New(), uuid.New(),
					testCase.amount, testCase.description, testCase.reconciled,
					testCase.categoryID, testCase.categoryAssignedBy, testCase.categoryAssignedAt,
					testCase.externalRefProvider, testCase.externalRefProviderTx,
				)
				require.Error(t, err, "the schema must reject this state")
			})
		}
	})
}

func pointerTo[T any](value T) *T {
	return &value
}
