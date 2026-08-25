package application

import (
	"context"

	"github.com/google/uuid"

	"github.com/luigimenezes13/financial-manager/internal/financialtracking/domain/account"
	"github.com/luigimenezes13/financial-manager/internal/financialtracking/domain/shared"
)

// CreateAccountInput é a entrada do use case. UserID chega como uuid.UUID já
// parseado na borda. Kind e Currency chegam como TEXTO — são VOs de
// vocabulário (a palavra "checking"/"BRL" É a linguagem do domínio), então
// o construtor do VO valida o texto (diferente da identidade, que a borda
// parseia). Origem é sempre manual: conta de provider nasce no use case de
// import, não aqui.
type CreateAccountInput struct {
	UserID   uuid.UUID
	Name     string
	Kind     string // "checking" | "savings" | "credit_card"
	Currency string // "BRL" | "USD"
}

// CreateAccountOutput é a projeção serializável do resultado. Saldo sai em
// CENTAVOS (int64) + moeda — o mundo externo não conhece o VO Money. Conta
// nova nasce zerada.
type CreateAccountOutput struct {
	ID       string
	UserID   string
	Name     string
	Kind     string
	Balance  int64
	Currency string
}

// CreateAccountUseCase cria uma conta MANUAL para um usuário. Depende só da
// porta account.Repository — sem dispatcher: account.New não emite evento
// (nascer zerada não é mudança de saldo), então não haveria nada a
// despachar. Injetar um dispatcher aqui seria dependência morta.
type CreateAccountUseCase struct {
	accounts account.Repository
}

// NewCreateAccountUseCase injeta as dependências por construtor.
func NewCreateAccountUseCase(accounts account.Repository) *CreateAccountUseCase {
	return &CreateAccountUseCase{accounts: accounts}
}

// Execute orquestra a criação da conta manual. Mesmo esqueleto do
// CreateCategoryUseCase: constrói VOs, delega a invariante ao aggregate,
// persiste, projeta a saída.
func (u *CreateAccountUseCase) Execute(ctx context.Context, input CreateAccountInput) (CreateAccountOutput, error) {
	// Identidade: a borda já parseou o uuid; aqui só viramos VO. O erro
	// (ErrInvalidUserID) nasce no domínio e sobe sem tradução — quem traduz
	// pra HTTP é o adapter, não o use case.
	userID, err := shared.NewUserID(input.UserID)
	if err != nil {
		return CreateAccountOutput{}, err
	}

	// Vocabulário: aqui o VO valida o TEXTO. "poupança inventada" não existe
	// no domínio, então NewKind recusa com ErrInvalidKind. Note o padrão Go
	// se repetindo — cada construção é uma linha + guard clause de 3 linhas.
	kind, err := account.NewKind(input.Kind)
	if err != nil {
		return CreateAccountOutput{}, err
	}

	currency, err := shared.NewCurrency(input.Currency)
	if err != nil {
		return CreateAccountOutput{}, err
	}

	// Único construtor da lista SEM erro: não existe origem manual inválida,
	// então a assinatura não mente inventando um `error` que nunca vem.
	source := account.NewManualSource()

	// O aggregate é a fonte da verdade das invariantes (nome vazio, kind
	// zerado, source zerado, saldo inicial na moeda). O use case não
	// revalida nada — se New recusar, só repassamos.
	acc, err := account.New(userID, input.Name, kind, currency, source)
	if err != nil {
		return CreateAccountOutput{}, err
	}

	if err := u.accounts.Save(ctx, acc); err != nil {
		return CreateAccountOutput{}, err
	}

	return toAccountOutput(acc), nil
}

// toAccountOutput projeta o aggregate no DTO de saída.
func toAccountOutput(acc *account.Account) CreateAccountOutput {
	// Saldo em centavos + moeda: quebramos a cadeia
	// acc.Balance().Money().Amount() em variáveis locais — uma chamada por
	// linha (Demeter). Balance e Money são VOs (valores, não ponteiros), então
	// atribuir a uma local é cópia — não há aliasing pro estado do aggregate.
	balance := acc.Balance()
	money := balance.Money()
	currency := balance.Currency()

	return CreateAccountOutput{
		ID:       acc.ID().String(),
		UserID:   acc.UserID().String(),
		Name:     acc.Name(),
		Kind:     acc.Kind().String(),
		Balance:  money.Amount(),
		Currency: currency.Code(),
	}
}
