package application

import (
	"context"
	"time"

	"github.com/google/uuid"

	"github.com/luigimenezes13/financial-manager/internal/financialtracking/domain/category"
	"github.com/luigimenezes13/financial-manager/internal/financialtracking/domain/shared"
	"github.com/luigimenezes13/financial-manager/internal/financialtracking/domain/transaction"
	"github.com/luigimenezes13/financial-manager/internal/kernel"
)

// CategorizeTransactionInput é a entrada da categorização. AssignedBy chega
// como TEXTO ("user" | "rule") — é VO de vocabulário, igual account.Kind:
// a palavra É a linguagem do domínio, então o construtor do VO valida.
type CategorizeTransactionInput struct {
	UserID        uuid.UUID
	TransactionID uuid.UUID
	CategoryID    uuid.UUID
	AssignedBy    string // "user" | "rule"
}

// CategorizeTransactionOutput é a projeção serializável da atribuição.
type CategorizeTransactionOutput struct {
	TransactionID string
	CategoryID    string
	AssignedBy    string
	AssignedAt    time.Time
}

// CategorizeTransactionUseCase atribui (ou troca) a categoria de uma
// transação. Duas dependências: a transação (lê e grava) e a categoria (só
// LÊ — pra garantir que existe e é do usuário). O Categorized que o root
// emite vai para o outbox na mesma transação da escrita.
type CategorizeTransactionUseCase struct {
	transactions      transaction.Repository
	categories        category.Repository
	transactionRunner *kernel.TransactionRunner
}

func NewCategorizeTransactionUseCase(transactions transaction.Repository, categories category.Repository, transactionRunner *kernel.TransactionRunner) *CategorizeTransactionUseCase {
	return &CategorizeTransactionUseCase{transactions: transactions, categories: categories, transactionRunner: transactionRunner}
}

// Execute orquestra a categorização: resolve identidades, carrega os dois
// aggregates pra checar dono e delega a mutação ao root.
func (u *CategorizeTransactionUseCase) Execute(ctx context.Context, input CategorizeTransactionInput) (CategorizeTransactionOutput, error) {
	userID, err := shared.NewUserID(input.UserID)
	if err != nil {
		return CategorizeTransactionOutput{}, err
	}

	transactionID, err := transaction.TransactionIDFromUUID(input.TransactionID)
	if err != nil {
		return CategorizeTransactionOutput{}, err
	}

	categoryID, err := category.CategoryIDFromUUID(input.CategoryID)
	if err != nil {
		return CategorizeTransactionOutput{}, err
	}

	assignedBy, err := transaction.NewAssignedBy(input.AssignedBy)
	if err != nil {
		return CategorizeTransactionOutput{}, err
	}

	target, err := u.transactions.FindByID(ctx, transactionID)
	if err != nil {
		return CategorizeTransactionOutput{}, err
	}
	if err := assertOwnership(target.UserID(), userID); err != nil {
		return CategorizeTransactionOutput{}, err
	}

	// A categoria é carregada só pra VALIDAR: existe? é deste usuário?
	// Categorizar com o id de uma categoria de outra pessoa vazaria a
	// existência dela — daí o ErrForbidden vir antes de qualquer mutação.
	targetCategory, err := u.categories.FindByID(ctx, categoryID)
	if err != nil {
		return CategorizeTransactionOutput{}, err
	}
	if err := assertOwnership(targetCategory.UserID(), userID); err != nil {
		return CategorizeTransactionOutput{}, err
	}

	// Recategorizar SUBSTITUI a atribuição e re-emite o evento — decisão de
	// domínio já resolvida dentro do aggregate (2026-07-07).
	if err := target.Categorize(categoryID, assignedBy); err != nil {
		return CategorizeTransactionOutput{}, err
	}

	if err := u.save(ctx, target); err != nil {
		return CategorizeTransactionOutput{}, err
	}

	return toCategorizeTransactionOutput(target)
}

// toCategorizeTransactionOutput projeta a atribuição recém-feita. Retorna
// erro junto porque a leitura é comma-ok: se por absurdo a transação não
// estiver categorizada aqui, é bug de invariante — melhor um erro explícito
// que um output com campos zerados.
func toCategorizeTransactionOutput(target *transaction.Transaction) (CategorizeTransactionOutput, error) {
	assignment, ok := target.Category()
	if !ok {
		return CategorizeTransactionOutput{}, transaction.ErrInvalidAssignment
	}

	assignedCategory := assignment.CategoryID()
	assignedBy := assignment.By()

	return CategorizeTransactionOutput{
		TransactionID: target.ID().String(),
		CategoryID:    assignedCategory.String(),
		AssignedBy:    assignedBy.String(),
		AssignedAt:    assignment.AssignedAt(),
	}, nil
}

func (u *CategorizeTransactionUseCase) save(ctx context.Context, target *transaction.Transaction) error {
	return u.transactionRunner.Begin(ctx, func(ctx context.Context, transactionContext kernel.TransactionContext) error {
		transactionContext.RecordEventsOf(target)
		return u.transactions.Save(ctx, target)
	})
}
