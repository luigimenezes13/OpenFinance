package application

import (
	"context"
	"time"

	"github.com/google/uuid"

	"github.com/luigimenezes13/financial-manager/internal/financialtracking/domain/account"
	"github.com/luigimenezes13/financial-manager/internal/financialtracking/domain/shared"
	"github.com/luigimenezes13/financial-manager/internal/financialtracking/domain/transaction"
)

// RecordTransactionInput é a entrada do lançamento MANUAL. Amount em
// CENTAVOS, sinal na convenção patrimônio (positivo = entrada, negativo =
// saída) — a mesma do resto do BC.
//
// Repare no que NÃO tem aqui: moeda. A transação herda a moeda da CONTA
// (decisão de fronteira, 2026-08-25). Deixar o cliente mandar a moeda
// criaria uma invariante cruzando dois aggregates ("a moeda da transação
// tem que ser a da conta") que não cabe em nenhum dos dois — e o único
// lugar sobrando pra ela seria este use case, que não pode ter regra.
// Herdando, a invariante deixa de existir.
type RecordTransactionInput struct {
	UserID      uuid.UUID
	AccountID   uuid.UUID
	Amount      int64
	OccurredAt  time.Time
	Description string
}

// RecordTransactionOutput é a projeção serializável do lançamento.
type RecordTransactionOutput struct {
	ID          string
	UserID      string
	AccountID   string
	Amount      int64
	Currency    string
	OccurredAt  time.Time
	Description string
}

// RecordTransactionUseCase registra um lançamento manual numa conta do
// usuário. Depende de DUAS portas: transaction.Repository (onde grava) e
// account.Repository (só pra LER a conta — ownership e moeda). Ler um
// segundo aggregate pra decidir é orquestração legítima; MUTAR os dois no
// mesmo use case não seria (fronteira de consistência é por aggregate).
//
// Sem dispatcher: transaction.NewManual não emite evento — só o fluxo de
// provider emite Imported. Injetar um dispatcher aqui seria dependência
// morta (o spec previa um; a decisão de não injetar é de 2026-08-25).
type RecordTransactionUseCase struct {
	transactions transaction.Repository
	accounts     account.Repository
}

// NewRecordTransactionUseCase injeta as dependências por construtor.
func NewRecordTransactionUseCase(transactions transaction.Repository, accounts account.Repository) *RecordTransactionUseCase {
	return &RecordTransactionUseCase{transactions: transactions, accounts: accounts}
}

// Execute orquestra o lançamento: resolve identidades, carrega a conta pra
// checar dono e moeda, delega as invariantes ao aggregate, persiste.
//
// O SALDO da conta NÃO é mexido aqui (decisão de 2026-08-25, seguindo o
// spec): saldo de conta conectada é fato do provider, e derivar saldo de
// lançamentos manuais é outro comportamento — entra como use case próprio
// no backlog, não como efeito colateral escondido deste.
func (u *RecordTransactionUseCase) Execute(ctx context.Context, input RecordTransactionInput) (RecordTransactionOutput, error) {
	userID, err := shared.NewUserID(input.UserID)
	if err != nil {
		return RecordTransactionOutput{}, err
	}

	accountID, err := account.AccountIDFromUUID(input.AccountID)
	if err != nil {
		return RecordTransactionOutput{}, err
	}

	// FindByID devolve ErrNotFound quando não existe (o repository traduz o
	// erro do driver — pgx.ErrNoRows não chega aqui).
	targetAccount, err := u.accounts.FindByID(ctx, accountID)
	if err != nil {
		return RecordTransactionOutput{}, err
	}

	if err := assertOwnership(targetAccount.UserID(), userID); err != nil {
		return RecordTransactionOutput{}, err
	}

	// Moeda herdada da conta: uma chamada por linha (Demeter) e Money
	// construído com a moeda que a conta já garante ser válida.
	balance := targetAccount.Balance()
	currency := balance.Currency()
	money, err := shared.NewMoney(input.Amount, currency)
	if err != nil {
		return RecordTransactionOutput{}, err
	}

	// Todas as invariantes do lançamento (valor não-zero, descrição
	// obrigatória, data não-futura) moram no aggregate.
	recorded, err := transaction.NewManual(userID, accountID, money, input.OccurredAt, input.Description)
	if err != nil {
		return RecordTransactionOutput{}, err
	}

	if err := u.transactions.Save(ctx, recorded); err != nil {
		return RecordTransactionOutput{}, err
	}

	return toRecordTransactionOutput(recorded, currency), nil
}

// assertOwnership recusa operar aggregate de outro usuário. Vive aqui (e
// não no aggregate) porque é regra de AUTORIZAÇÃO da aplicação: o
// aggregate sabe quem é o dono; quem decide que "não-dono não passa" é o
// caso de uso. Compartilhada pelos use cases que carregam aggregate por ID.
func assertOwnership(owner shared.UserID, requester shared.UserID) error {
	if !owner.Equals(requester) {
		return shared.ErrForbidden
	}
	return nil
}

// toRecordTransactionOutput projeta o aggregate no DTO de saída. Recebe a
// moeda pronta: ela vem da conta, não da transação (o VO Money carrega a
// dela, mas evitamos a cadeia recorded.Money().Currency().Code()).
func toRecordTransactionOutput(recorded *transaction.Transaction, currency shared.Currency) RecordTransactionOutput {
	money := recorded.Money()

	return RecordTransactionOutput{
		ID:          recorded.ID().String(),
		UserID:      recorded.UserID().String(),
		AccountID:   recorded.AccountID().String(),
		Amount:      money.Amount(),
		Currency:    currency.Code(),
		OccurredAt:  recorded.OccurredAt(),
		Description: recorded.Description(),
	}
}
