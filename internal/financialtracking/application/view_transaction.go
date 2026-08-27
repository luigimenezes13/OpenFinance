package application

import (
	"context"

	"github.com/google/uuid"

	"github.com/luigimenezes13/financial-manager/internal/financialtracking/domain/shared"
	"github.com/luigimenezes13/financial-manager/internal/financialtracking/domain/transaction"
)

// ViewTransactionInput é a entrada do detalhe de uma transação.
type ViewTransactionInput struct {
	UserID        uuid.UUID
	TransactionID uuid.UUID
}

// ViewTransactionUseCase devolve uma transação do usuário.
type ViewTransactionUseCase struct {
	transactions transaction.Repository
}

// NewViewTransactionUseCase injeta as dependências por construtor.
func NewViewTransactionUseCase(transactions transaction.Repository) *ViewTransactionUseCase {
	return &ViewTransactionUseCase{transactions: transactions}
}

// Execute carrega a transação, checa o dono e projeta.
func (u *ViewTransactionUseCase) Execute(ctx context.Context, input ViewTransactionInput) (TransactionSummary, error) {
	userID, err := shared.NewUserID(input.UserID)
	if err != nil {
		return TransactionSummary{}, err
	}

	transactionID, err := transaction.TransactionIDFromUUID(input.TransactionID)
	if err != nil {
		return TransactionSummary{}, err
	}

	found, err := u.transactions.FindByID(ctx, transactionID)
	if err != nil {
		return TransactionSummary{}, err
	}

	if err := assertOwnership(found.UserID(), userID); err != nil {
		return TransactionSummary{}, err
	}

	return toTransactionSummary(found), nil
}
