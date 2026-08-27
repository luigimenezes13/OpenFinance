package application

import (
	"context"

	"github.com/google/uuid"

	"github.com/luigimenezes13/financial-manager/internal/financialtracking/domain/account"
	"github.com/luigimenezes13/financial-manager/internal/financialtracking/domain/shared"
)

// ViewAccountInput é a entrada do detalhe de uma conta.
type ViewAccountInput struct {
	UserID    uuid.UUID
	AccountID uuid.UUID
}

// ViewAccountUseCase devolve uma conta do usuário.
//
// Diferente da lista, aqui o id vem do cliente — então a checagem de dono é
// obrigatória, e é ela que faz "conta de outro" responder 403 em vez de
// entregar o saldo alheio.
type ViewAccountUseCase struct {
	accounts account.Repository
}

// NewViewAccountUseCase injeta as dependências por construtor.
func NewViewAccountUseCase(accounts account.Repository) *ViewAccountUseCase {
	return &ViewAccountUseCase{accounts: accounts}
}

// Execute carrega a conta, checa o dono e projeta.
//
// Devolve AccountSummary direto, sem struct de Output própria: um wrapper de
// um campo só não acrescenta informação nenhuma, e a projeção é a mesma da
// lista.
func (u *ViewAccountUseCase) Execute(ctx context.Context, input ViewAccountInput) (AccountSummary, error) {
	userID, err := shared.NewUserID(input.UserID)
	if err != nil {
		return AccountSummary{}, err
	}

	accountID, err := account.AccountIDFromUUID(input.AccountID)
	if err != nil {
		return AccountSummary{}, err
	}

	found, err := u.accounts.FindByID(ctx, accountID)
	if err != nil {
		return AccountSummary{}, err
	}

	if err := assertOwnership(found.UserID(), userID); err != nil {
		return AccountSummary{}, err
	}

	return toAccountSummary(found), nil
}
