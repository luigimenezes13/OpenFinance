package application

import (
	"context"

	"github.com/google/uuid"

	"github.com/luigimenezes13/financial-manager/internal/financialtracking/domain/category"
)

// AddCategoryRuleInput é a entrada da criação de regra.
type AddCategoryRuleInput struct {
	UserID     uuid.UUID
	CategoryID uuid.UUID
	Keyword    string
}

// AddCategoryRuleUseCase adiciona uma regra de auto-categorização à
// categoria.
//
// A regra é criada e guardada agora; quem a APLICA (a rules engine) é
// PR2/PR5. Isso é deliberado: o usuário pode cadastrar suas regras desde já,
// e quando o motor entrar ele encontra os dados prontos em vez de exigir que
// todo mundo recadastre.
type AddCategoryRuleUseCase struct {
	categories category.Repository
}

// NewAddCategoryRuleUseCase injeta as dependências por construtor.
func NewAddCategoryRuleUseCase(categories category.Repository) *AddCategoryRuleUseCase {
	return &AddCategoryRuleUseCase{categories: categories}
}

// Execute carrega a categoria, checa o dono e adiciona a regra.
func (u *AddCategoryRuleUseCase) Execute(ctx context.Context, input AddCategoryRuleInput) (CategorySummary, error) {
	target, err := loadOwnedCategory(ctx, u.categories, input.UserID, input.CategoryID)
	if err != nil {
		return CategorySummary{}, err
	}

	// O construtor valida o keyword; o AddRule do root recusa duplicata.
	// Nenhuma das duas regras é revalidada aqui.
	rule, err := category.NewCategoryRule(input.Keyword)
	if err != nil {
		return CategorySummary{}, err
	}

	if err := target.AddRule(rule); err != nil {
		return CategorySummary{}, err
	}

	if err := u.categories.Save(ctx, target); err != nil {
		return CategorySummary{}, err
	}

	return toCategorySummary(target), nil
}
