package application

import (
	"context"

	"github.com/google/uuid"

	"github.com/luigimenezes13/financial-manager/internal/financialtracking/domain/category"
	"github.com/luigimenezes13/financial-manager/internal/financialtracking/domain/shared"
)

// ListCategoriesInput é a entrada: só o usuário autenticado.
type ListCategoriesInput struct {
	UserID uuid.UUID
}

// CategorySummary é a projeção de uma categoria, com suas regras.
//
// As regras vão junto porque estão DENTRO da fronteira do aggregate: a
// leitura de uma categoria sem suas regras devolveria um aggregate pela
// metade, e a UI precisaria de uma segunda chamada por categoria.
type CategorySummary struct {
	ID       string
	Name     string
	ParentID *string
	Rules    []RuleSummary
}

// RuleSummary é a projeção de uma regra de auto-categorização.
type RuleSummary struct {
	ID      string
	Keyword string
}

// ListCategoriesOutput é a projeção da lista.
type ListCategoriesOutput struct {
	Categories []CategorySummary
}

// ListCategoriesUseCase lista as categorias do usuário.
type ListCategoriesUseCase struct {
	categories category.Repository
}

// NewListCategoriesUseCase injeta as dependências por construtor.
func NewListCategoriesUseCase(categories category.Repository) *ListCategoriesUseCase {
	return &ListCategoriesUseCase{categories: categories}
}

// Execute lista as categorias do usuário.
func (u *ListCategoriesUseCase) Execute(ctx context.Context, input ListCategoriesInput) (ListCategoriesOutput, error) {
	userID, err := shared.NewUserID(input.UserID)
	if err != nil {
		return ListCategoriesOutput{}, err
	}

	found, err := u.categories.ListByUser(ctx, userID)
	if err != nil {
		return ListCategoriesOutput{}, err
	}

	summaries := make([]CategorySummary, 0, len(found))
	for _, current := range found {
		summaries = append(summaries, toCategorySummary(current))
	}

	return ListCategoriesOutput{Categories: summaries}, nil
}

// toCategorySummary projeta o aggregate e suas regras.
func toCategorySummary(current *category.Category) CategorySummary {
	summary := CategorySummary{
		ID:    current.ID().String(),
		Name:  current.Name(),
		Rules: make([]RuleSummary, 0, len(current.Rules())),
	}

	if parent := current.ParentID(); parent != nil {
		value := parent.String()
		summary.ParentID = &value
	}

	for _, rule := range current.Rules() {
		ruleID := rule.ID()
		summary.Rules = append(summary.Rules, RuleSummary{
			ID:      ruleID.String(),
			Keyword: rule.Keyword(),
		})
	}

	return summary
}
