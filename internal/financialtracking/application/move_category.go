package application

import (
	"context"

	"github.com/google/uuid"

	"github.com/luigimenezes13/financial-manager/internal/financialtracking/domain/category"
)

// MoveCategoryInput é a entrada da re-parentagem. ParentID nil = a categoria
// passa a ser raiz.
type MoveCategoryInput struct {
	UserID     uuid.UUID
	CategoryID uuid.UUID
	ParentID   *uuid.UUID
}

// MoveCategoryUseCase move uma categoria na hierarquia.
//
// Separado do renomear, e não um "update" com os dois campos: são duas
// intenções diferentes do usuário ("corrigi o nome" vs "reorganizei minha
// árvore"), e juntá-las esconderia qual delas aconteceu no log e no futuro
// evento.
type MoveCategoryUseCase struct {
	categories category.Repository
}

// NewMoveCategoryUseCase injeta as dependências por construtor.
func NewMoveCategoryUseCase(categories category.Repository) *MoveCategoryUseCase {
	return &MoveCategoryUseCase{categories: categories}
}

// Execute carrega a categoria, checa o dono e delega o movimento.
//
// O use case NÃO verifica se o pai existe nem se o movimento cria ciclo. O
// aggregate recusa auto-parentesco, e validar existência aqui seria uma
// consulta a mais numa checagem que o banco não garante entre aggregates
// (decisão de fronteira: sem FK entre aggregates). Ciclo de mais de um nível
// fica como limite conhecido do v1.
func (u *MoveCategoryUseCase) Execute(ctx context.Context, input MoveCategoryInput) (CategorySummary, error) {
	target, err := loadOwnedCategory(ctx, u.categories, input.UserID, input.CategoryID)
	if err != nil {
		return CategorySummary{}, err
	}

	var parentID *category.CategoryID
	if input.ParentID != nil {
		parsed, err := category.CategoryIDFromUUID(*input.ParentID)
		if err != nil {
			return CategorySummary{}, err
		}
		parentID = &parsed
	}

	if err := target.MoveTo(parentID); err != nil {
		return CategorySummary{}, err
	}

	if err := u.categories.Save(ctx, target); err != nil {
		return CategorySummary{}, err
	}

	return toCategorySummary(target), nil
}
