package application_test

import (
	"context"
	"testing"

	"github.com/google/uuid"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/luigimenezes13/financial-manager/internal/financialtracking/application"
	"github.com/luigimenezes13/financial-manager/internal/financialtracking/domain/category"
	"github.com/luigimenezes13/financial-manager/internal/financialtracking/domain/shared"
	"github.com/luigimenezes13/financial-manager/internal/financialtracking/domain/transaction"
)

type categorizeScenario struct {
	ownerID        uuid.UUID
	target         *transaction.Transaction
	targetCategory *category.Category
	transactions   *fakeTransactions
	categories     *fakeCategories
	useCase        *application.CategorizeTransactionUseCase
}

func newCategorizeScenario(t *testing.T, transactionOwner uuid.UUID, categoryOwner uuid.UUID) categorizeScenario {
	t.Helper()

	targetAccount := newManualAccount(t, transactionOwner, "BRL")
	target := newManualTransaction(t, transactionOwner, targetAccount.ID(), -3200, "BRL")
	targetCategory := newCategory(t, categoryOwner, "Food")
	transactions := newFakeTransactions(target)
	categories := newFakeCategories(targetCategory)

	return categorizeScenario{
		ownerID:        transactionOwner,
		target:         target,
		targetCategory: targetCategory,
		transactions:   transactions,
		categories:     categories,
		useCase:        application.NewCategorizeTransactionUseCase(transactions, categories),
	}
}

func (s categorizeScenario) validInput() application.CategorizeTransactionInput {
	transactionID := s.target.ID()
	categoryID := s.targetCategory.ID()
	return application.CategorizeTransactionInput{
		UserID:        s.ownerID,
		TransactionID: transactionID.UUID(),
		CategoryID:    categoryID.UUID(),
		AssignedBy:    "user",
	}
}

func TestCategorizeTransaction(t *testing.T) {
	t.Parallel()

	t.Run("should save the categorized transaction with the Categorized event and only read the category", func(t *testing.T) {
		t.Parallel()

		ownerID := uuid.New()
		scenario := newCategorizeScenario(t, ownerID, ownerID)
		transactionID := scenario.target.ID()
		categoryID := scenario.targetCategory.ID()

		output, err := scenario.useCase.Execute(context.Background(), scenario.validInput())

		require.NoError(t, err)
		assert.Equal(t, transactionID.String(), output.TransactionID)
		assert.Equal(t, categoryID.String(), output.CategoryID)
		assert.Equal(t, "user", output.AssignedBy)
		assert.False(t, output.AssignedAt.IsZero())
		require.Len(t, scenario.transactions.saved, 1, "the categorized transaction must be persisted")
		assert.Equal(t, []string{transaction.EventTypeCategorized}, scenario.transactions.savedEventNames())
		assert.Empty(t, scenario.target.Events(), "the save clears the recorded events")
		assert.Empty(t, scenario.categories.saved, "the category is only READ by this use case")
	})

	t.Run("should replace the assignment and save the Categorized event again when recategorizing so budgets learn about the change", func(t *testing.T) {
		t.Parallel()

		ownerID := uuid.New()
		scenario := newCategorizeScenario(t, ownerID, ownerID)
		second := newCategory(t, ownerID, "Restaurants")
		scenario.categories.stored[second.ID().String()] = second
		secondID := second.ID()

		_, err := scenario.useCase.Execute(context.Background(), scenario.validInput())
		require.NoError(t, err)
		recategorize := scenario.validInput()
		recategorize.CategoryID = secondID.UUID()
		recategorize.AssignedBy = "rule"
		output, err := scenario.useCase.Execute(context.Background(), recategorize)

		require.NoError(t, err)
		assert.Equal(t, secondID.String(), output.CategoryID)
		assert.Equal(t, "rule", output.AssignedBy)
		assert.Equal(t,
			[]string{transaction.EventTypeCategorized, transaction.EventTypeCategorized},
			scenario.transactions.savedEventNames(),
			"recategorizing saves the event again",
		)
	})
}

func TestCategorizeTransactionOwnership(t *testing.T) {
	t.Parallel()

	cases := []struct {
		name                     string
		transactionOfAnotherUser bool
		categoryOfAnotherUser    bool
	}{
		{name: "should refuse with ErrForbidden a transaction of another user", transactionOfAnotherUser: true},
		{name: "should refuse with ErrForbidden a category of another user, since ErrNotFound would leak that it exists", categoryOfAnotherUser: true},
	}

	for _, testCase := range cases {
		t.Run(testCase.name, func(t *testing.T) {
			t.Parallel()

			requesterID := uuid.New()
			transactionOwner := ownerOrRequester(requesterID, testCase.transactionOfAnotherUser)
			categoryOwner := ownerOrRequester(requesterID, testCase.categoryOfAnotherUser)
			scenario := newCategorizeScenario(t, transactionOwner, categoryOwner)
			input := scenario.validInput()
			input.UserID = requesterID

			_, err := scenario.useCase.Execute(context.Background(), input)

			require.ErrorIs(t, err, shared.ErrForbidden)
			assert.Empty(t, scenario.transactions.saved)
			assert.Empty(t, scenario.transactions.savedEvents)
		})
	}
}

func ownerOrRequester(requesterID uuid.UUID, ownedByAnotherUser bool) uuid.UUID {
	if ownedByAnotherUser {
		return uuid.New()
	}
	return requesterID
}

func TestCategorizeTransactionRefusal(t *testing.T) {
	t.Parallel()

	cases := []struct {
		name        string
		mutateInput func(input *application.CategorizeTransactionInput)
		wantErr     error
	}{
		{
			name:        "should refuse a nil user",
			mutateInput: func(input *application.CategorizeTransactionInput) { input.UserID = uuid.Nil },
			wantErr:     shared.ErrInvalidUserID,
		},
		{
			name:        "should refuse a nil transaction",
			mutateInput: func(input *application.CategorizeTransactionInput) { input.TransactionID = uuid.Nil },
			wantErr:     transaction.ErrInvalidID,
		},
		{
			name:        "should refuse a nil category",
			mutateInput: func(input *application.CategorizeTransactionInput) { input.CategoryID = uuid.Nil },
			wantErr:     category.ErrInvalidID,
		},
		{
			name:        "should refuse an assignedBy outside the vocabulary",
			mutateInput: func(input *application.CategorizeTransactionInput) { input.AssignedBy = "robot" },
			wantErr:     transaction.ErrInvalidAssignment,
		},
		{
			name:        "should refuse a missing transaction",
			mutateInput: func(input *application.CategorizeTransactionInput) { input.TransactionID = uuid.New() },
			wantErr:     transaction.ErrNotFound,
		},
		{
			name:        "should refuse a missing category",
			mutateInput: func(input *application.CategorizeTransactionInput) { input.CategoryID = uuid.New() },
			wantErr:     category.ErrNotFound,
		},
	}

	for _, testCase := range cases {
		t.Run(testCase.name, func(t *testing.T) {
			t.Parallel()

			ownerID := uuid.New()
			scenario := newCategorizeScenario(t, ownerID, ownerID)
			input := scenario.validInput()
			testCase.mutateInput(&input)

			_, err := scenario.useCase.Execute(context.Background(), input)

			require.ErrorIs(t, err, testCase.wantErr)
			assert.Empty(t, scenario.transactions.saved)
			assert.Empty(t, scenario.transactions.savedEvents)
		})
	}
}

func TestCategorizeTransactionInfrastructureFailure(t *testing.T) {
	t.Parallel()

	cases := []struct {
		name    string
		arrange func(transactions *fakeTransactions, categories *fakeCategories)
	}{
		{
			name:    "should return the error when reading the transaction fails",
			arrange: func(transactions *fakeTransactions, _ *fakeCategories) { transactions.findErr = errInfra },
		},
		{
			name:    "should return the error when reading the category fails",
			arrange: func(_ *fakeTransactions, categories *fakeCategories) { categories.findErr = errInfra },
		},
		{
			name:    "should return the error when saving the transaction fails",
			arrange: func(transactions *fakeTransactions, _ *fakeCategories) { transactions.saveErr = errInfra },
		},
	}

	for _, testCase := range cases {
		t.Run(testCase.name, func(t *testing.T) {
			t.Parallel()

			ownerID := uuid.New()
			scenario := newCategorizeScenario(t, ownerID, ownerID)
			testCase.arrange(scenario.transactions, scenario.categories)

			_, err := scenario.useCase.Execute(context.Background(), scenario.validInput())

			require.ErrorIs(t, err, errInfra)
			assert.Empty(t, scenario.transactions.savedEvents, "no event is saved when persistence fails")
		})
	}

	t.Run("should keep the Categorized event on the aggregate when saving fails", func(t *testing.T) {
		t.Parallel()

		ownerID := uuid.New()
		scenario := newCategorizeScenario(t, ownerID, ownerID)
		scenario.transactions.saveErr = errInfra

		_, err := scenario.useCase.Execute(context.Background(), scenario.validInput())

		require.ErrorIs(t, err, errInfra)
		assert.Len(t, scenario.target.Events(), 1, "an unsaved event stays on the aggregate")
	})
}
