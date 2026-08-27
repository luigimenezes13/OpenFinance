package entrepo

import (
	"context"
	"fmt"

	"github.com/luigimenezes13/financial-manager/internal/financialtracking/adapter/entrepo/ent"
	enttransaction "github.com/luigimenezes13/financial-manager/internal/financialtracking/adapter/entrepo/ent/transaction"
	domaintransaction "github.com/luigimenezes13/financial-manager/internal/financialtracking/domain/transaction"
)

// TransactionRepository implementa transaction.Repository.
type TransactionRepository struct {
	client *ent.Client
}

var _ domaintransaction.Repository = (*TransactionRepository)(nil)

// NewTransactionRepository injeta o client por construtor.
func NewTransactionRepository(client *ent.Client) *TransactionRepository {
	return &TransactionRepository{client: client}
}

// Save persiste a transação (upsert por id).
func (r *TransactionRepository) Save(ctx context.Context, target *domaintransaction.Transaction) error {
	snapshot := target.Snapshot()

	builder := r.client.Transaction.Create().
		SetID(snapshot.ID).
		SetUserID(snapshot.UserID).
		SetAccountID(snapshot.AccountID).
		SetAmount(snapshot.Amount).
		SetCurrency(snapshot.Currency).
		SetOccurredAt(snapshot.OccurredAt).
		SetDescription(snapshot.Description).
		SetReconciled(snapshot.Reconciled).
		SetCategoryAssignedBy(snapshot.CategoryAssignedBy).
		SetExternalRefProvider(snapshot.ExternalRefProvider).
		SetExternalRefProviderTransactionID(snapshot.ExternalRefProviderTransactionID)

	// Categoria ausente fica NULL nas DUAS colunas. Sem este if, o
	// time.Time zero gravaria "ano 1" em vez de NULL — e o CHECK
	// transactions_category_assignment_complete recusaria a linha
	// incoerente. Setter tipado não protege de zero value: quem sabe que
	// "ausente" existe é o mapper.
	if snapshot.CategoryID != nil {
		builder = builder.
			SetCategoryID(*snapshot.CategoryID).
			SetCategoryAssignedAt(snapshot.CategoryAssignedAt)
	}

	err := builder.
		OnConflictColumns(enttransaction.FieldID).
		UpdateNewValues().
		Exec(ctx)
	if err != nil {
		return fmt.Errorf("entrepo: falha salvando transação: %w", err)
	}
	return nil
}

// FindByID hidrata a transação pelo ID. Ausência → transaction.ErrNotFound.
func (r *TransactionRepository) FindByID(ctx context.Context, id domaintransaction.TransactionID) (*domaintransaction.Transaction, error) {
	row, err := r.client.Transaction.Get(ctx, id.UUID())
	if ent.IsNotFound(err) {
		return nil, domaintransaction.ErrNotFound
	}
	if err != nil {
		return nil, fmt.Errorf("entrepo: falha lendo transação: %w", err)
	}

	return domaintransaction.FromSnapshot(toTransactionSnapshot(row))
}

// toTransactionSnapshot traduz entity → Snapshot. As duas convenções de
// "ausente" se encontram aqui: NULL no banco (ponteiro nil no Ent) vira
// ponteiro nil / time zero no Snapshot.
func toTransactionSnapshot(row *ent.Transaction) domaintransaction.TransactionSnapshot {
	snapshot := domaintransaction.TransactionSnapshot{
		ID:                               row.ID,
		UserID:                           row.UserID,
		AccountID:                        row.AccountID,
		Amount:                           row.Amount,
		Currency:                         row.Currency,
		OccurredAt:                       row.OccurredAt,
		Description:                      row.Description,
		Reconciled:                       row.Reconciled,
		CategoryID:                       row.CategoryID,
		CategoryAssignedBy:               row.CategoryAssignedBy,
		ExternalRefProvider:              row.ExternalRefProvider,
		ExternalRefProviderTransactionID: row.ExternalRefProviderTransactionID,
	}

	if row.CategoryAssignedAt != nil {
		snapshot.CategoryAssignedAt = *row.CategoryAssignedAt
	}

	return snapshot
}

// List traduz o Criteria do domínio em predicados do Ent.
//
// A ordenação é occurred_at DESC + id DESC: extrato se lê do recente pro
// antigo, e o id como desempate é o que impede duas transações do MESMO
// instante de trocarem de lugar entre páginas — sem ele, a paginação por
// offset repetiria ou perderia registros no limite das páginas.
func (r *TransactionRepository) List(ctx context.Context, criteria domaintransaction.Criteria) ([]*domaintransaction.Transaction, error) {
	userID := criteria.UserID()
	query := r.client.Transaction.Query().Where(enttransaction.UserIDEQ(userID.UUID()))

	if accountID, ok := criteria.AccountID(); ok {
		query = query.Where(enttransaction.AccountIDEQ(accountID.UUID()))
	}
	if categoryID, ok := criteria.CategoryID(); ok {
		query = query.Where(enttransaction.CategoryIDEQ(categoryID.UUID()))
	}
	if from, ok := criteria.From(); ok {
		query = query.Where(enttransaction.OccurredAtGTE(from))
	}
	if to, ok := criteria.To(); ok {
		query = query.Where(enttransaction.OccurredAtLTE(to))
	}

	page := criteria.Page()
	rows, err := query.
		Order(ent.Desc(enttransaction.FieldOccurredAt), ent.Desc(enttransaction.FieldID)).
		Offset(page.Offset()).
		Limit(page.Limit()).
		All(ctx)
	if err != nil {
		return nil, fmt.Errorf("entrepo: falha listando transações: %w", err)
	}

	transactions := make([]*domaintransaction.Transaction, 0, len(rows))
	for _, row := range rows {
		rebuilt, err := domaintransaction.FromSnapshot(toTransactionSnapshot(row))
		if err != nil {
			return nil, err
		}
		transactions = append(transactions, rebuilt)
	}

	return transactions, nil
}
