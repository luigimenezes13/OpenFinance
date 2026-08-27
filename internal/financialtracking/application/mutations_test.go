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

// TestRenameAccountUseCase cobre caminho feliz, recusa do domínio e
// autorização.
func TestRenameAccountUseCase(t *testing.T) {
	t.Parallel()

	ownerID := uuid.New()

	t.Run("renomeia e persiste", func(t *testing.T) {
		t.Parallel()

		target := newManualAccount(t, ownerID, "BRL")
		accountID := target.ID()
		accounts := newFakeAccounts(target)
		useCase := application.NewRenameAccountUseCase(accounts)

		summary, err := useCase.Execute(context.Background(), application.RenameAccountInput{
			UserID: ownerID, AccountID: accountID.UUID(), Name: "  Nubank PJ  ",
		})

		require.NoError(t, err)
		assert.Equal(t, "Nubank PJ", summary.Name)
		require.Len(t, accounts.saved, 1)
	})

	t.Run("nome inválido não muta nem persiste", func(t *testing.T) {
		t.Parallel()

		target := newManualAccount(t, ownerID, "BRL")
		accountID := target.ID()
		accounts := newFakeAccounts(target)
		useCase := application.NewRenameAccountUseCase(accounts)

		_, err := useCase.Execute(context.Background(), application.RenameAccountInput{
			UserID: ownerID, AccountID: accountID.UUID(), Name: "   ",
		})

		require.ErrorIs(t, err, account.ErrInvalidName)
		assert.Equal(t, "Conta Corrente", target.Name())
		assert.Empty(t, accounts.saved)
	})

	t.Run("conta de outro usuário", func(t *testing.T) {
		t.Parallel()

		target := newManualAccount(t, ownerID, "BRL")
		accountID := target.ID()
		accounts := newFakeAccounts(target)
		useCase := application.NewRenameAccountUseCase(accounts)

		_, err := useCase.Execute(context.Background(), application.RenameAccountInput{
			UserID: uuid.New(), AccountID: accountID.UUID(), Name: "Sequestrada",
		})

		require.ErrorIs(t, err, shared.ErrForbidden)
		assert.Empty(t, accounts.saved)
	})
}

// TestRenameCategoryUseCase cobre renomeação de categoria.
func TestRenameCategoryUseCase(t *testing.T) {
	t.Parallel()

	ownerID := uuid.New()
	target := newCategory(t, ownerID, "Alimentação")
	categoryID := target.ID()
	categories := newFakeCategories(target)
	useCase := application.NewRenameCategoryUseCase(categories)

	summary, err := useCase.Execute(context.Background(), application.RenameCategoryInput{
		UserID: ownerID, CategoryID: categoryID.UUID(), Name: "Comida",
	})

	require.NoError(t, err)
	assert.Equal(t, "Comida", summary.Name)
	require.Len(t, categories.saved, 1)
}

// TestMoveCategoryUseCase cobre as duas direções e o auto-parentesco.
func TestMoveCategoryUseCase(t *testing.T) {
	t.Parallel()

	ownerID := uuid.New()

	t.Run("vira subcategoria", func(t *testing.T) {
		t.Parallel()

		parent := newCategory(t, ownerID, "Alimentação")
		child := newCategory(t, ownerID, "Restaurantes")
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

	t.Run("nil volta a ser raiz", func(t *testing.T) {
		t.Parallel()

		parent := newCategory(t, ownerID, "Alimentação")
		parentID := parent.ID()
		child := newCategory(t, ownerID, "Restaurantes")
		require.NoError(t, child.MoveTo(&parentID))
		childID := child.ID()
		useCase := application.NewMoveCategoryUseCase(newFakeCategories(parent, child))

		summary, err := useCase.Execute(context.Background(), application.MoveCategoryInput{
			UserID: ownerID, CategoryID: childID.UUID(), ParentID: nil,
		})

		require.NoError(t, err)
		assert.Nil(t, summary.ParentID)
	})

	t.Run("pai de si mesma", func(t *testing.T) {
		t.Parallel()

		target := newCategory(t, ownerID, "Alimentação")
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

// TestCategoryRuleUseCases cobre o ciclo de vida da regra pelo root.
func TestCategoryRuleUseCases(t *testing.T) {
	t.Parallel()

	ownerID := uuid.New()

	t.Run("adiciona, recusa duplicata e remove", func(t *testing.T) {
		t.Parallel()

		target := newCategory(t, ownerID, "Alimentação")
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
		require.ErrorIs(t, err, category.ErrRuleNotFound, "remover duas vezes não é idempotente por decisão do aggregate")
	})

	t.Run("keyword em branco", func(t *testing.T) {
		t.Parallel()

		target := newCategory(t, ownerID, "Alimentação")
		categoryID := target.ID()
		categories := newFakeCategories(target)
		useCase := application.NewAddCategoryRuleUseCase(categories)

		_, err := useCase.Execute(context.Background(), application.AddCategoryRuleInput{
			UserID: ownerID, CategoryID: categoryID.UUID(), Keyword: "   ",
		})

		require.ErrorIs(t, err, category.ErrInvalidKeyword)
		assert.Empty(t, categories.saved)
	})

	t.Run("categoria de outro usuário", func(t *testing.T) {
		t.Parallel()

		target := newCategory(t, uuid.New(), "Alheia")
		categoryID := target.ID()
		categories := newFakeCategories(target)
		useCase := application.NewAddCategoryRuleUseCase(categories)

		_, err := useCase.Execute(context.Background(), application.AddCategoryRuleInput{
			UserID: ownerID, CategoryID: categoryID.UUID(), Keyword: "ifood",
		})

		require.ErrorIs(t, err, shared.ErrForbidden)
		assert.Empty(t, categories.saved)
	})

	t.Run("categoria inexistente", func(t *testing.T) {
		t.Parallel()

		categories := newFakeCategories()
		useCase := application.NewAddCategoryRuleUseCase(categories)

		_, err := useCase.Execute(context.Background(), application.AddCategoryRuleInput{
			UserID: ownerID, CategoryID: uuid.New(), Keyword: "ifood",
		})

		require.ErrorIs(t, err, category.ErrNotFound)
	})
}
