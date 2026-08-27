package application

import (
	"context"
	"time"

	"github.com/google/uuid"

	"github.com/luigimenezes13/financial-manager/internal/financialtracking/domain/account"
	"github.com/luigimenezes13/financial-manager/internal/financialtracking/domain/category"
	"github.com/luigimenezes13/financial-manager/internal/financialtracking/domain/shared"
	"github.com/luigimenezes13/financial-manager/internal/financialtracking/domain/transaction"
	"github.com/luigimenezes13/financial-manager/internal/kernel"
)

// ListTransactionsInput é a entrada do extrato. Os filtros são opcionais
// (ponteiro nil / instante zero = sem filtro); a paginação tem default no VO
// Page, então Limit zero significa "não pedi nada".
type ListTransactionsInput struct {
	UserID     uuid.UUID
	AccountID  *uuid.UUID
	CategoryID *uuid.UUID
	From       time.Time
	To         time.Time
	Limit      int
	Offset     int
}

// TransactionSummary é a projeção de uma transação para leitura. Serve à
// lista e ao detalhe.
type TransactionSummary struct {
	ID          string
	AccountID   string
	Amount      int64
	Currency    string
	OccurredAt  time.Time
	Description string
	Reconciled  bool

	// Bloco de categoria: os três andam juntos ou nenhum vem, igual ao VO
	// CategoryAssignment que os originou.
	CategoryID *string
	AssignedBy *string
	AssignedAt *time.Time

	// Provider vazio = lançamento manual.
	Provider string
}

// ListTransactionsOutput é a projeção do extrato. Devolve a janela aplicada
// junto: o cliente pediu Limit zero e precisa saber qual default entrou pra
// montar a próxima página.
type ListTransactionsOutput struct {
	Transactions []TransactionSummary
	Limit        int
	Offset       int
}

// ListTransactionsUseCase devolve o extrato do usuário.
type ListTransactionsUseCase struct {
	transactions transaction.Repository
}

// NewListTransactionsUseCase injeta as dependências por construtor.
func NewListTransactionsUseCase(transactions transaction.Repository) *ListTransactionsUseCase {
	return &ListTransactionsUseCase{transactions: transactions}
}

// Execute monta o critério de domínio a partir do input e consulta.
//
// Note que o use case não filtra nada por conta própria: ele CONVERTE input
// em Criteria e deixa o VO validar. Filtro inválido (período invertido, conta
// zerada) morre na construção do critério, antes de virar SQL.
func (u *ListTransactionsUseCase) Execute(ctx context.Context, input ListTransactionsInput) (ListTransactionsOutput, error) {
	userID, err := shared.NewUserID(input.UserID)
	if err != nil {
		return ListTransactionsOutput{}, err
	}

	page, err := kernel.NewPage(input.Limit, input.Offset)
	if err != nil {
		return ListTransactionsOutput{}, err
	}

	options, err := criteriaOptionsFrom(input)
	if err != nil {
		return ListTransactionsOutput{}, err
	}

	criteria, err := transaction.NewCriteria(userID, page, options...)
	if err != nil {
		return ListTransactionsOutput{}, err
	}

	found, err := u.transactions.List(ctx, criteria)
	if err != nil {
		return ListTransactionsOutput{}, err
	}

	summaries := make([]TransactionSummary, 0, len(found))
	for _, current := range found {
		summaries = append(summaries, toTransactionSummary(current))
	}

	return ListTransactionsOutput{
		Transactions: summaries,
		Limit:        page.Limit(),
		Offset:       page.Offset(),
	}, nil
}

// criteriaOptionsFrom traduz os filtros opcionais do input em options do
// domínio, convertendo uuid em VO de identidade.
func criteriaOptionsFrom(input ListTransactionsInput) ([]transaction.CriteriaOption, error) {
	options := make([]transaction.CriteriaOption, 0, 3)

	if input.AccountID != nil {
		accountID, err := account.AccountIDFromUUID(*input.AccountID)
		if err != nil {
			return nil, err
		}
		options = append(options, transaction.ForAccount(accountID))
	}

	if input.CategoryID != nil {
		categoryID, err := category.CategoryIDFromUUID(*input.CategoryID)
		if err != nil {
			return nil, err
		}
		options = append(options, transaction.ForCategory(categoryID))
	}

	if !input.From.IsZero() || !input.To.IsZero() {
		options = append(options, transaction.InPeriod(input.From, input.To))
	}

	return options, nil
}

// toTransactionSummary projeta o aggregate. Compartilhada com o detalhe.
func toTransactionSummary(current *transaction.Transaction) TransactionSummary {
	money := current.Money()
	currency := money.Currency()

	summary := TransactionSummary{
		ID:          current.ID().String(),
		AccountID:   current.AccountID().String(),
		Amount:      money.Amount(),
		Currency:    currency.Code(),
		OccurredAt:  current.OccurredAt(),
		Description: current.Description(),
		Reconciled:  current.IsReconciled(),
	}

	if assignment, ok := current.Category(); ok {
		categoryID := assignment.CategoryID()
		assignedBy := assignment.By()
		assignedAt := assignment.AssignedAt()

		categoryValue := categoryID.String()
		assignedByValue := assignedBy.String()
		summary.CategoryID = &categoryValue
		summary.AssignedBy = &assignedByValue
		summary.AssignedAt = &assignedAt
	}

	if ref, ok := current.ExternalRef(); ok {
		summary.Provider = ref.Provider()
	}

	return summary
}
