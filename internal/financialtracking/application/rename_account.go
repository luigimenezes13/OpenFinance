package application

import (
	"context"

	"github.com/google/uuid"

	"github.com/luigimenezes13/financial-manager/internal/financialtracking/domain/account"
	"github.com/luigimenezes13/financial-manager/internal/financialtracking/domain/shared"
	"github.com/luigimenezes13/financial-manager/internal/kernel"
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
	accounts          account.Repository
	transactionRunner *kernel.TransactionRunner
}

func NewRenameAccountUseCase(accounts account.Repository, transactionRunner *kernel.TransactionRunner) *RenameAccountUseCase {
	return &RenameAccountUseCase{accounts: accounts, transactionRunner: transactionRunner}
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

	if err := u.save(ctx, target); err != nil {
		return AccountSummary{}, err
	}

	return toAccountSummary(target), nil
}

func (u *RenameAccountUseCase) save(ctx context.Context, target *account.Account) error {
	return u.transactionRunner.Begin(ctx, func(ctx context.Context, transactionContext kernel.TransactionContext) error {
		transactionContext.RecordEventsOf(target)
		return u.accounts.Save(ctx, target)
	})
}
