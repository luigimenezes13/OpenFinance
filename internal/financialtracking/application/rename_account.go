package application

import (
	"context"

	"github.com/google/uuid"

	"github.com/luigimenezes13/financial-manager/internal/financialtracking/domain/account"
	"github.com/luigimenezes13/financial-manager/internal/financialtracking/domain/shared"
)

// RenameAccountInput é a entrada da renomeação.
type RenameAccountInput struct {
	UserID    uuid.UUID
	AccountID uuid.UUID
	Name      string
}

// RenameAccountUseCase renomeia uma conta do usuário.
//
// Use case próprio, e não um "UpdateAccount" genérico: renomear é a única
// coisa que o dono pode mudar numa conta. Kind e moeda são decisões de
// abertura (mudar a moeda de uma conta com lançamentos reinterpretaria todo
// o histórico), e saldo vem do provedor. Um endpoint de update genérico
// convidaria justamente essas mudanças.
type RenameAccountUseCase struct {
	accounts account.Repository
}

// NewRenameAccountUseCase injeta as dependências por construtor.
func NewRenameAccountUseCase(accounts account.Repository) *RenameAccountUseCase {
	return &RenameAccountUseCase{accounts: accounts}
}

// Execute carrega a conta, checa o dono e delega a renomeação ao aggregate.
func (u *RenameAccountUseCase) Execute(ctx context.Context, input RenameAccountInput) (AccountSummary, error) {
	userID, err := shared.NewUserID(input.UserID)
	if err != nil {
		return AccountSummary{}, err
	}

	accountID, err := account.AccountIDFromUUID(input.AccountID)
	if err != nil {
		return AccountSummary{}, err
	}

	target, err := u.accounts.FindByID(ctx, accountID)
	if err != nil {
		return AccountSummary{}, err
	}
	if err := assertOwnership(target.UserID(), userID); err != nil {
		return AccountSummary{}, err
	}

	// A regra "o que é um nome válido" mora no aggregate; nome inválido
	// mantém o atual (Rename não muta em caso de recusa).
	if err := target.Rename(input.Name); err != nil {
		return AccountSummary{}, err
	}

	if err := u.accounts.Save(ctx, target); err != nil {
		return AccountSummary{}, err
	}

	return toAccountSummary(target), nil
}
