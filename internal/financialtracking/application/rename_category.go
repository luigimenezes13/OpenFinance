package application

import (
	"context"

	"github.com/google/uuid"

	"github.com/luigimenezes13/financial-manager/internal/financialtracking/domain/category"
	"github.com/luigimenezes13/financial-manager/internal/financialtracking/domain/shared"
	"github.com/luigimenezes13/financial-manager/internal/kernel"
)

// RenameCategoryInput é a entrada da renomeação.
type RenameCategoryInput struct {
	UserID     uuid.UUID
	CategoryID uuid.UUID
	Name       string
}

// RenameCategoryUseCase renomeia uma categoria do usuário.
type RenameCategoryUseCase struct {
	categories        category.Repository
	transactionRunner *kernel.TransactionRunner
}

func NewRenameCategoryUseCase(categories category.Repository, transactionRunner *kernel.TransactionRunner) *RenameCategoryUseCase {
	return &RenameCategoryUseCase{categories: categories, transactionRunner: transactionRunner}
}

// Execute carrega a categoria, checa o dono e delega ao aggregate.
func (u *RenameCategoryUseCase) Execute(ctx context.Context, input RenameCategoryInput) (CategorySummary, error) {
	target, err := loadOwnedCategory(ctx, u.categories, input.UserID, input.CategoryID)
	if err != nil {
		return CategorySummary{}, err
	}

	if err := target.Rename(input.Name); err != nil {
		return CategorySummary{}, err
	}

	if err := u.save(ctx, target); err != nil {
		return CategorySummary{}, err
	}

	return toCategorySummary(target), nil
}

// loadOwnedCategory resolve os VOs, carrega a categoria e checa o dono.
//
// Compartilhada pelos quatro use cases que mutam categoria (renomear, mover,
// adicionar e remover regra): sem ela, a checagem de ownership estaria
// copiada quatro vezes, e a quinta cópia é onde alguém esquece.
func loadOwnedCategory(
	ctx context.Context,
	categories category.Repository,
	rawUserID uuid.UUID,
	rawCategoryID uuid.UUID,
) (*category.Category, error) {
	userID, err := shared.NewUserID(rawUserID)
	if err != nil {
		return nil, err
	}

	categoryID, err := category.CategoryIDFromUUID(rawCategoryID)
	if err != nil {
		return nil, err
	}

	target, err := categories.FindByID(ctx, categoryID)
	if err != nil {
		return nil, err
	}
	if err := assertOwnership(target.UserID(), userID); err != nil {
		return nil, err
	}

	return target, nil
}

func (u *RenameCategoryUseCase) save(ctx context.Context, target *category.Category) error {
	return u.transactionRunner.Begin(ctx, func(ctx context.Context, _ kernel.TransactionContext) error {
		return u.categories.Save(ctx, target)
	})
}
