package application_test

import (
	"context"
	"testing"

	"github.com/google/uuid"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/luigimenezes13/financial-manager/internal/financialtracking/application"
	"github.com/luigimenezes13/financial-manager/internal/financialtracking/domain/account"
	"github.com/luigimenezes13/financial-manager/internal/financialtracking/domain/category"
	"github.com/luigimenezes13/financial-manager/internal/financialtracking/domain/shared"
)

func TestRenameAccount(t *testing.T) {
	t.Parallel()

	ownerID := uuid.New()

	t.Run("should rename the account and save it", func(t *testing.T) {
		t.Parallel()

		target := newManualAccount(t, ownerID, "BRL")
		accountID := target.ID()
		accounts := newFakeAccounts(target)
		useCase := application.NewRenameAccountUseCase(accounts)

		summary, err := useCase.Execute(context.Background(), application.RenameAccountInput{
			UserID: ownerID, AccountID: accountID.UUID(), Name: "  Nubank Business  ",
		})

		require.NoError(t, err)
		assert.Equal(t, "Nubank Business", summary.Name)
		require.Len(t, accounts.saved, 1)
	})

	t.Run("should neither mutate nor save the account when the name is invalid", func(t *testing.T) {
		t.Parallel()

		target := newManualAccount(t, ownerID, "BRL")
		accountID := target.ID()
		accounts := newFakeAccounts(target)
		useCase := application.NewRenameAccountUseCase(accounts)

		_, err := useCase.Execute(context.Background(), application.RenameAccountInput{
			UserID: ownerID, AccountID: accountID.UUID(), Name: "   ",
		})

		require.ErrorIs(t, err, account.ErrInvalidName)
		assert.Equal(t, "Checking Account", target.Name())
		assert.Empty(t, accounts.saved)
	})

	t.Run("should refuse with ErrForbidden an account of another user", func(t *testing.T) {
		t.Parallel()

		target := newManualAccount(t, ownerID, "BRL")
		accountID := target.ID()
		accounts := newFakeAccounts(target)
		useCase := application.NewRenameAccountUseCase(accounts)

		_, err := useCase.Execute(context.Background(), application.RenameAccountInput{
			UserID: uuid.New(), AccountID: accountID.UUID(), Name: "Hijacked",
		})

		require.ErrorIs(t, err, shared.ErrForbidden)
		assert.Empty(t, accounts.saved)
	})
}

func TestRenameCategory(t *testing.T) {
	t.Parallel()

	t.Run("should rename the category and save it", func(t *testing.T) {
		t.Parallel()

		ownerID := uuid.New()
		target := newCategory(t, ownerID, "Food")
		categoryID := target.ID()
		categories := newFakeCategories(target)
		useCase := application.NewRenameCategoryUseCase(categories)

		summary, err := useCase.Execute(context.Background(), application.RenameCategoryInput{
			UserID: ownerID, CategoryID: categoryID.UUID(), Name: "Meals",
		})

		require.NoError(t, err)
		assert.Equal(t, "Meals", summary.Name)
		require.Len(t, categories.saved, 1)
	})
}

func TestMoveCategory(t *testing.T) {
	t.Parallel()

	ownerID := uuid.New()

	t.Run("should turn a root category into a subcategory", func(t *testing.T) {
		t.Parallel()

		parent := newCategory(t, ownerID, "Food")
		child := newCategory(t, ownerID, "Restaurants")
		parentID := parent.ID()
		childID := child.ID()
		rawParent := parentID.UUID()
		useCase := application.NewMoveCategoryUseCase(newFakeCategories(parent, child))

		summary, err := useCase.Execute(context.Background(), application.MoveCategoryInput{
			UserID: ownerID, CategoryID: childID.UUID(), ParentID: &rawParent,
		})

		require.NoError(t, err)
		require.NotNil(t, summary.ParentID)
		assert.Equal(t, parentID.String(), *summary.ParentID)
	})

	t.Run("should turn a subcategory back into a root category when the parent is nil", func(t *testing.T) {
		t.Parallel()

		parent := newCategory(t, ownerID, "Food")
		parentID := parent.ID()
		child := newCategory(t, ownerID, "Restaurants")
		require.NoError(t, child.MoveTo(&parentID))
		childID := child.ID()
		useCase := application.NewMoveCategoryUseCase(newFakeCategories(parent, child))

		summary, err := useCase.Execute(context.Background(), application.MoveCategoryInput{
			UserID: ownerID, CategoryID: childID.UUID(), ParentID: nil,
		})

		require.NoError(t, err)
		assert.Nil(t, summary.ParentID)
	})

	t.Run("should refuse a category as its own parent without saving", func(t *testing.T) {
		t.Parallel()

		target := newCategory(t, ownerID, "Food")
		categoryID := target.ID()
		raw := categoryID.UUID()
		categories := newFakeCategories(target)
		useCase := application.NewMoveCategoryUseCase(categories)

		_, err := useCase.Execute(context.Background(), application.MoveCategoryInput{
			UserID: ownerID, CategoryID: categoryID.UUID(), ParentID: &raw,
		})

		require.ErrorIs(t, err, category.ErrInvalidParent)
		assert.Empty(t, categories.saved)
	})
}

func TestCategoryRules(t *testing.T) {
	t.Parallel()

	ownerID := uuid.New()

	t.Run("should add a rule, refuse a duplicate and remove it through the aggregate root", func(t *testing.T) {
		t.Parallel()

		target := newCategory(t, ownerID, "Food")
		categoryID := target.ID()
		categories := newFakeCategories(target)
		add := application.NewAddCategoryRuleUseCase(categories)
		remove := application.NewRemoveCategoryRuleUseCase(categories)

		summary, err := add.Execute(context.Background(), application.AddCategoryRuleInput{
			UserID: ownerID, CategoryID: categoryID.UUID(), Keyword: "ifood",
		})
		require.NoError(t, err)
		require.Len(t, summary.Rules, 1)
		ruleID := summary.Rules[0].ID

		_, err = add.Execute(context.Background(), application.AddCategoryRuleInput{
			UserID: ownerID, CategoryID: categoryID.UUID(), Keyword: "ifood",
		})
		require.ErrorIs(t, err, category.ErrDuplicateRule)

		afterRemoval, err := remove.Execute(context.Background(), application.RemoveCategoryRuleInput{
			UserID: ownerID, CategoryID: categoryID.UUID(), RuleID: uuid.MustParse(ruleID),
		})
		require.NoError(t, err)
		assert.Empty(t, afterRemoval.Rules)

		_, err = remove.Execute(context.Background(), application.RemoveCategoryRuleInput{
			UserID: ownerID, CategoryID: categoryID.UUID(), RuleID: uuid.MustParse(ruleID),
		})
		require.ErrorIs(t, err, category.ErrRuleNotFound, "removing twice is not idempotent, by decision of the aggregate")
	})

	t.Run("should refuse a blank keyword without saving", func(t *testing.T) {
		t.Parallel()

		target := newCategory(t, ownerID, "Food")
		categoryID := target.ID()
		categories := newFakeCategories(target)
		useCase := application.NewAddCategoryRuleUseCase(categories)

		_, err := useCase.Execute(context.Background(), application.AddCategoryRuleInput{
			UserID: ownerID, CategoryID: categoryID.UUID(), Keyword: "   ",
		})

		require.ErrorIs(t, err, category.ErrInvalidKeyword)
		assert.Empty(t, categories.saved)
	})

	t.Run("should refuse with ErrForbidden a category of another user", func(t *testing.T) {
		t.Parallel()

		target := newCategory(t, uuid.New(), "Someone Else's")
		categoryID := target.ID()
		categories := newFakeCategories(target)
		useCase := application.NewAddCategoryRuleUseCase(categories)

		_, err := useCase.Execute(context.Background(), application.AddCategoryRuleInput{
			UserID: ownerID, CategoryID: categoryID.UUID(), Keyword: "ifood",
		})

		require.ErrorIs(t, err, shared.ErrForbidden)
		assert.Empty(t, categories.saved)
	})

	t.Run("should refuse a missing category", func(t *testing.T) {
		t.Parallel()

		categories := newFakeCategories()
		useCase := application.NewAddCategoryRuleUseCase(categories)

		_, err := useCase.Execute(context.Background(), application.AddCategoryRuleInput{
			UserID: ownerID, CategoryID: uuid.New(), Keyword: "ifood",
		})

		require.ErrorIs(t, err, category.ErrNotFound)
	})
}
