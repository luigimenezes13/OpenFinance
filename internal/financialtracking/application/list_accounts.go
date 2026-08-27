package application

import (
	"context"
	"time"

	"github.com/google/uuid"

	"github.com/luigimenezes13/financial-manager/internal/financialtracking/domain/account"
	"github.com/luigimenezes13/financial-manager/internal/financialtracking/domain/shared"
)

// ListAccountsInput é a entrada: só o usuário autenticado. Não existe
// parâmetro para "contas de quem" — a pergunta não pode ser feita, então a
// falha de autorização correspondente não existe.
type ListAccountsInput struct {
	UserID uuid.UUID
}

// AccountSummary é a projeção de uma conta para leitura. Serve à lista E ao
// detalhe: hoje a mesma informação atende os dois, e criar duas structs
// idênticas só para ter nomes diferentes seria duplicação esperando
// divergir.
type AccountSummary struct {
	ID       string
	Name     string
	Kind     string
	Balance  int64
	Currency string

	// BalanceAsOf é a que HORA aquele saldo se refere. Vai na projeção
	// porque saldo sem data mente: o número veio do provedor no último
	// sync, e a UI precisa poder dizer "atualizado às 14h".
	BalanceAsOf time.Time

	// Provider vazio = conta manual. É o que permite a UI marcar quais
	// contas têm saldo confiável (as conectadas) e quais são só registro
	// manual de lançamentos.
	Provider string
}

// ListAccountsOutput é a projeção da lista.
type ListAccountsOutput struct {
	Accounts []AccountSummary
}

// ListAccountsUseCase lista as contas do usuário.
type ListAccountsUseCase struct {
	accounts account.Repository
}

// NewListAccountsUseCase injeta as dependências por construtor.
func NewListAccountsUseCase(accounts account.Repository) *ListAccountsUseCase {
	return &ListAccountsUseCase{accounts: accounts}
}

// Execute lista as contas do usuário. Sem paginação: uma pessoa tem contas
// na casa das unidades.
func (u *ListAccountsUseCase) Execute(ctx context.Context, input ListAccountsInput) (ListAccountsOutput, error) {
	userID, err := shared.NewUserID(input.UserID)
	if err != nil {
		return ListAccountsOutput{}, err
	}

	found, err := u.accounts.ListByUser(ctx, userID)
	if err != nil {
		return ListAccountsOutput{}, err
	}

	summaries := make([]AccountSummary, 0, len(found))
	for _, current := range found {
		summaries = append(summaries, toAccountSummary(current))
	}

	return ListAccountsOutput{Accounts: summaries}, nil
}

// toAccountSummary projeta o aggregate. Compartilhada com o use case de
// detalhe.
func toAccountSummary(current *account.Account) AccountSummary {
	balance := current.Balance()
	money := balance.Money()
	currency := balance.Currency()
	source := current.Source()
	// Comma-ok descartado: conta manual devolve "", que é exatamente o que a
	// projeção usa para dizer "não é conectada".
	provider, _ := source.Provider()

	return AccountSummary{
		ID:          current.ID().String(),
		Name:        current.Name(),
		Kind:        current.Kind().String(),
		Balance:     money.Amount(),
		Currency:    currency.Code(),
		BalanceAsOf: balance.AsOf(),
		Provider:    provider,
	}
}
