package application

import (
	"context"

	"github.com/google/uuid"

	"github.com/luigimenezes13/financial-manager/internal/financialtracking/domain/category"
)

// RemoveCategoryRuleInput é a entrada da remoção de regra.
type RemoveCategoryRuleInput struct {
	UserID     uuid.UUID
	CategoryID uuid.UUID
	RuleID     uuid.UUID
}

// RemoveCategoryRuleUseCase remove uma regra da categoria.
type RemoveCategoryRuleUseCase struct {
	categories category.Repository
}

// NewRemoveCategoryRuleUseCase injeta as dependências por construtor.
func NewRemoveCategoryRuleUseCase(categories category.Repository) *RemoveCategoryRuleUseCase {
	return &RemoveCategoryRuleUseCase{categories: categories}
}

// Execute carrega a categoria, checa o dono e remove a regra.
//
// A regra é acessada pelo ROOT, nunca direto: é ele que sabe se aquela regra
// é dele (RemoveRule devolve ErrRuleNotFound se não for). Um repositório de
// CategoryRule permitiria remover a regra de outra categoria passando o id
// certo — e é por isso que ele não existe.
func (u *RemoveCategoryRuleUseCase) Execute(ctx context.Context, input RemoveCategoryRuleInput) (CategorySummary, error) {
	target, err := loadOwnedCategory(ctx, u.categories, input.UserID, input.CategoryID)
	if err != nil {
		return CategorySummary{}, err
	}

	ruleID, err := category.RuleIDFromUUID(input.RuleID)
	if err != nil {
		return CategorySummary{}, err
	}

	if err := target.RemoveRule(ruleID); err != nil {
		return CategorySummary{}, err
	}

	if err := u.categories.Save(ctx, target); err != nil {
		return CategorySummary{}, err
	}

	return toCategorySummary(target), nil
}
