// Package application reúne os casos de uso do Financial Tracking BC. Layout
// Go idiomático: UM package coeso, UM arquivo por use case (create_category.go,
// create_account.go, ...), com o Input/Output e o mapeamento de saída
// COLOCADOS junto do use case que os define — são parte da API dele.
//
// Clean Architecture: cada use case ORQUESTRA o domínio via as portas
// (interfaces do domain) e NÃO contém regra de negócio — a regra mora no
// aggregate. As dependências apontam pra dentro: application importa domain,
// nunca o contrário.
package application

import (
	"context"

	"github.com/google/uuid"

	"github.com/luigimenezes13/financial-manager/internal/financialtracking/domain/category"
	"github.com/luigimenezes13/financial-manager/internal/financialtracking/domain/shared"
)

// CreateCategoryInput é a entrada do use case. UserID e ParentID chegam como
// uuid.UUID JÁ parseados na borda (handler/middleware) — o domínio não
// parseia representação externa (decisão de fronteira do spec, 2026-07-04);
// o use case só CONVERTE esses uuids nos VOs de identidade. ParentID nil =
// categoria raiz.
type CreateCategoryInput struct {
	UserID   uuid.UUID
	Name     string
	ParentID *uuid.UUID
}

// CreateCategoryOutput é a projeção serializável do resultado — o aggregate
// nunca vaza pra fora da application layer. IDs como string (forma canônica
// do UUID); ParentID nil = categoria raiz.
type CreateCategoryOutput struct {
	ID       string
	UserID   string
	Name     string
	ParentID *string
}

// CreateCategoryUseCase cria uma categoria nova para um usuário. Depende só
// da PORTA category.Repository (DIP): o concreto (pgx) é injetado no main,
// nunca instanciado aqui.
type CreateCategoryUseCase struct {
	categories category.Repository
}

// NewCreateCategoryUseCase injeta as dependências por construtor (o único
// papel do construtor: atribuir dependências).
func NewCreateCategoryUseCase(categories category.Repository) *CreateCategoryUseCase {
	return &CreateCategoryUseCase{categories: categories}
}

// Execute orquestra a criação: converte os uuids (já parseados na borda) nos
// VOs de identidade, delega a invariante ao aggregate (category.New),
// persiste e projeta a saída. Sem regra de negócio aqui.
func (u *CreateCategoryUseCase) Execute(ctx context.Context, input CreateCategoryInput) (CreateCategoryOutput, error) {
	// NewUserID retorna (UserID, error): o VO valida o uuid e devolve um
	// sentinel de domínio se for inválido. Em Go o erro é um segundo retorno,
	// não uma exception — o padrão é checar e repassar na hora.
	userID, err := shared.NewUserID(input.UserID)
	if err != nil {
		return CreateCategoryOutput{}, err
	}

	// Pai opcional: nil = categoria raiz. Só converte o uuid no VO quando há
	// pai. parent começa nil (zero value de ponteiro) e o aggregate aceita
	// assim; quando há pai, guardamos o ENDEREÇO do CategoryID convertido.
	var parent *category.CategoryID
	if input.ParentID != nil {
		parentID, err := category.CategoryIDFromUUID(*input.ParentID)
		if err != nil {
			return CreateCategoryOutput{}, err
		}
		parent = &parentID
	}

	// O aggregate é a fonte da verdade das invariantes (nome, parent). O use
	// case não revalida nada — só repassa o erro se a criação recusar.
	cat, err := category.New(userID, input.Name, parent)
	if err != nil {
		return CreateCategoryOutput{}, err
	}

	if err := u.categories.Save(ctx, cat); err != nil {
		return CreateCategoryOutput{}, err
	}

	return toCategoryOutput(cat), nil
}

// toCategoryOutput projeta o aggregate no DTO de saída. Função privada no
// mesmo arquivo (idioma Go: o mapeamento é parte da API deste use case, não
// um package "mappers" separado).
func toCategoryOutput(cat *category.Category) CreateCategoryOutput {
	// ParentID vira *string: nil continua nil (categoria raiz); com pai,
	// copiamos a String() pra uma variável local pra ter um endereço estável
	// pra apontar (não dá pra pegar &cat.ParentID().String()).
	var parentID *string
	if parent := cat.ParentID(); parent != nil {
		value := parent.String()
		parentID = &value
	}

	return CreateCategoryOutput{
		ID:       cat.ID().String(),
		UserID:   cat.UserID().String(),
		Name:     cat.Name(),
		ParentID: parentID,
	}
}
